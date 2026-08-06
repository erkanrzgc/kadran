// panelyd, Panely'nin kontrol düzlemi daemon'ıdır.
//
// YETKİSİZ çalışır. Docker grubunda değildir, hiçbir yeteneği (capability)
// yoktur ve ayrıcalık gerektiren her şeyi executor'a tipli bir RPC olarak
// gönderir. `sudo -u panely docker ps` BAŞARISIZ OLMAK ZORUNDADIR — ürünün
// tamamı bu iddiaya dayanır.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"time"

	"google.golang.org/grpc"

	"github.com/erkanrzgc/panely/internal/api"
	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/grpcserve"
	"github.com/erkanrzgc/panely/internal/logutil"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/sdnotify"
	"github.com/erkanrzgc/panely/internal/sockets"
	"github.com/erkanrzgc/panely/internal/store"
	"github.com/erkanrzgc/panely/internal/version"
)

const (
	defaultSocket      = "/run/panely/api.sock"
	defaultExecSocket  = "/run/panely-exec/exec.sock"
	defaultDB          = "/var/lib/panely/panely.db"
	defaultClientGroup = "panely-client"
)

func main() {
	if err := run(); err != nil {
		slog.Error("daemon başlatılamadı", "hata", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		socketPath  = flag.String("socket", defaultSocket, "istemcilere açılan unix soketi")
		execSocket  = flag.String("exec-socket", defaultExecSocket, "executor soketi")
		dbPath      = flag.String("db", defaultDB, "SQLite veritabanı yolu")
		clientGroup = flag.String("client-group", defaultClientGroup, "api.sock'a erişebilecek grup")
		showVersion = flag.Bool("version", false, "sürümü yazdır ve çık")
		debug       = flag.Bool("debug", false, "ayrıntılı günlük (PANELY_DEBUG=1 ile de açılır)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("panelyd %s (%s) protokol %d\n", version.Version, version.Commit, version.Protocol)
		return nil
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logutil.Level(*debug, os.Getenv),
	})))

	// ── Ürünün merkezî iddiası ───────────────────────────────────────
	// panelyd root çalışıyorsa executor ayrımı dekoratiftir: ele geçirilen
	// daemon zaten her şeyi yapabilir. Bu yüzden root ile başlamayı
	// reddediyoruz. Executor'daki kontrolün tam aynası (o root OLMALI).
	if os.Geteuid() == 0 {
		return errors.New(
			"panelyd root çalışmamalı — executor ayrımının anlamı kalmaz. " +
				"systemd unit dosyasında User=panely olduğunu doğrulayın")
	}

	clientGID, err := lookupGID(*clientGroup)
	if err != nil {
		return err
	}

	db, err := store.Open(context.Background(), *dbPath)
	if err != nil {
		return fmt.Errorf("veritabanı açılamadı: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("veritabanı kapatılamadı", "hata", err)
		}
	}()

	exec, err := execclient.Dial(*execSocket)
	if err != nil {
		return err
	}
	defer func() {
		if err := exec.Close(); err != nil {
			slog.Error("executor bağlantısı kapatılamadı", "hata", err)
		}
	}()

	// Executor'a ulaşılamaması başlangıçta ÖLÜMCÜL DEĞİLDİR.
	//
	// Reddedip çıkmak systemd ile bir yeniden başlatma döngüsü yaratır ve
	// operatör hiçbir teşhis aracına erişemez. Uyarıp devam etmek, durum
	// ekranının "executor erişilemiyor" demesini sağlar — sorunu gizlemek
	// değil, görünür kılmak.
	probeExecutor(exec)

	service, err := api.NewServer(api.ServerOptions{Store: db, Executor: exec})
	if err != nil {
		return err
	}

	// İki aşamalı çağıran doğrulaması:
	//
	//  1. SO_PEERCRED — api.sock'a yalnızca panely-client grubundaki
	//     süreçler bağlanabilir. O grubun tek üyesi, zorlanmış komuta
	//     bağlanmış istemci SSH kullanıcısıdır.
	//  2. Kimlik önsözü — panely-connect, sshd'nin ortam değişkenlerinden
	//     türettiği SSH kimliğini bağlantı başında yazar.
	//
	// UYARI: SO_PEERCRED yalnızca BİRİNCİL grubu bildirir, bu yüzden
	// bootstrap istemci kullanıcısını `useradd -g panely-client` ile
	// oluşturmalıdır — `-G` ile değil.
	creds, err := api.TransportCredentials(uint32(clientGID)) //nolint:gosec // gid daima 32 bite sığar
	if err != nil {
		return fmt.Errorf("çağıran doğrulaması kurulamadı: %w", err)
	}

	if err := sockets.EnsureParentDir(*socketPath); err != nil {
		return err
	}
	listener, err := sockets.Listen(sockets.ListenOptions{
		Path: *socketPath,
		Mode: 0o660,
		GID:  clientGID,
	})
	if err != nil {
		return err
	}

	server := grpc.NewServer(
		grpc.Creds(creds),
		grpc.ChainUnaryInterceptor(api.LoggingInterceptor()),
	)
	panelyv1.RegisterPanelyServiceServer(server, service)

	recordStartup(db)

	slog.Info("daemon hazır",
		"surum", version.Version,
		"soket", *socketPath,
		"istemci_grubu", *clientGroup,
		"veritabani", *dbPath,
	)

	if err := sdnotify.Ready(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
		slog.Warn("systemd bilgilendirilemedi", "hata", err)
	}

	return grpcserve.Run(server, listener)
}

// probeExecutor, executor'a ulaşılıp ulaşılamadığını günlüğe yazar.
func probeExecutor(exec *execclient.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), execclient.DefaultTimeout)
	defer cancel()

	ping, err := exec.Ping(ctx)
	if err != nil {
		slog.Warn("executor'a ulaşılamıyor — ayrıcalıklı işlemler çalışmayacak", "hata", err)
		return
	}
	slog.Info("executor bağlandı", "surum", ping.Version, "protokol", ping.ProtocolVersion)
}

// recordStartup, daemon başlangıcını denetim zincirine yazar.
//
// Bu, zincire yazılan ilk gerçek olaydır ve iki işe yarar: yeniden
// başlatmalar denetim izinde görünür (beklenmedik bir yeniden başlatma
// araştırılması gereken bir olaydır), ve zincirin gerçekten çalıştığı
// kurulumdan hemen sonra doğrulanabilir olur.
func recordStartup(db *store.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rec := audit.Record{
		Actor:      audit.SystemActor("daemon"),
		Action:     "daemon.start",
		Target:     "panelyd",
		ParamsJSON: fmt.Sprintf(`{"version":%q,"protocol":%d}`, version.Version, version.Protocol),
		Outcome:    audit.OutcomeSuccess,
		Source:     audit.SourceDaemon,
	}

	// Denetim kaydı yazılamıyorsa bu ciddi bir sorundur ama daemon'ı
	// durdurmaz: kaydı olmayan bir servis, hiç olmayan bir servisten
	// iyidir ve sorun günlükte görünür.
	if _, err := db.AppendAudit(ctx, rec); err != nil {
		slog.Error("başlangıç denetim kaydı yazılamadı", "hata", err)
	}
}

func lookupGID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf(
			"%q grubu bulunamadı — `panely bootstrap` çalıştırıldı mı?: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("%q grubunun gid'i çözümlenemedi: %w", name, err)
	}
	return id, nil
}
