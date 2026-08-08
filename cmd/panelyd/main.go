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
	"github.com/erkanrzgc/panely/internal/deploy"
	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/grpcserve"
	"github.com/erkanrzgc/panely/internal/logutil"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/proxydrv"
	"github.com/erkanrzgc/panely/internal/sdnotify"
	"github.com/erkanrzgc/panely/internal/sockets"
	"github.com/erkanrzgc/panely/internal/store"
	"github.com/erkanrzgc/panely/internal/version"
)

const (
	defaultSocket      = "/run/panely/api.sock"
	defaultExecSocket  = "/run/panely-exec/exec.sock"
	defaultCaddySocket = "/run/panely-caddy/admin.sock"
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
		caddySocket = flag.String("caddy-socket", defaultCaddySocket, "ters vekil admin soketi")
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

	// ── Ters vekil ───────────────────────────────────────────────────
	//
	// Yapılandırma SQLite'tan ÜRETİLİYOR ve bütün olarak yükleniyor;
	// Caddy'de kısmi güncelleme yok. Admin bloğu her yüklemede gitmek
	// zorunda: onsuz bir yükleme Caddy'yi varsayılan TCP :2019'a döndürür
	// ve panelyd unix soketinden bir daha ULAŞAMAZ.
	proxy := proxydrv.New(*caddySocket)
	reconciler, err := deploy.New(db, exec, proxy, proxydrv.Admin{
		// `fd/3`: soketi systemd yaratıyor, Caddy onu devralıyor.
		// Gerekçe deploy/systemd/panely-caddy-admin.socket dosyasında.
		Listen: "fd/3",
		// Boş bırakılırsa Caddy HER isteği 403 "host not allowed" ile
		// reddeder (gerçek sunucuda ölçüldü).
		Origins: []string{"localhost"},
	})
	if err != nil {
		return err
	}

	rollout, err := deploy.NewRollout(exec, db, reconciler, deploy.DefaultGate)
	if err != nil {
		return err
	}

	// ── K-055'in yükümlülüğü ─────────────────────────────────────────
	//
	// Ters vekil `--resume` KULLANMIYOR: her başlayışında rotasız bir
	// yapılandırmaya dönüyor (ölçüldü — reboot sonrası :80/:443 hiç
	// dinlenmiyor). Gerçeğin kaynağı SQLite olduğu için doğru davranış bu,
	// ama panelyd açılışta uzlaştırmak ZORUNDA: aksi hâlde bir reboot
	// bütün siteleri sessizce yayından kaldırırdı.
	//
	// Başarısızlık ÖLÜMCÜL DEĞİL: executor yoklamasındaki gerekçenin
	// aynısı — reddedip çıkmak systemd ile bir yeniden başlatma döngüsü
	// yaratır ve operatör hiçbir teşhis aracına erişemez.
	reconcileAtStartup(reconciler)

	service, err := api.NewServer(api.ServerOptions{
		Store: db, Executor: exec, Rollout: rollout,
	})
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

// startupReconcileTimeout, açılıştaki uzlaştırmanın süre sınırı.
//
// Sınırsız bırakmak, cevap vermeyen bir admin soketinde panelyd'nin hiç
// dinlemeye başlamamasına yol açardı: teşhis aracının kendisi kaybolurdu.
const startupReconcileTimeout = 30 * time.Second

// reconcileAtStartup, ters vekili SQLite'taki duruma getirir.
//
// Sonucu YUTMUYOR: rotalanamayan uygulamalar adlarıyla günlüğe yazılıyor.
// Sessiz bir başarı, bütün sitelerin düştüğü bir kurulumla aynı
// görünürdü.
func reconcileAtStartup(rc *deploy.Reconciler) {
	ctx, cancel := context.WithTimeout(context.Background(), startupReconcileTimeout)
	defer cancel()

	res, err := rc.Reconcile(ctx)
	if err != nil {
		slog.Error("ters vekil açılışta uzlaştırılamadı — TRAFİK AKMIYOR OLABİLİR",
			"hata", err)
		return
	}
	if len(res.Skipped) > 0 {
		slog.Warn("bazı uygulamalar rotalanamadı", "ayrinti", res.Error())
	}
	slog.Info("ters vekil uzlaştırıldı", "rotalanan", res.Routed)
}
