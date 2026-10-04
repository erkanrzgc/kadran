// kadran-exec, Kadran'ın ayrıcalıklı executor'ıdır.
//
// ════════════════════════════════════════════════════════════════════
//
//	BU BINARY ROOT ÇALIŞIR. EKLENEN HER SATIR AYRICALIKLI YÜZEYDİR.
//
// ════════════════════════════════════════════════════════════════════
//
// Ayrıcalık kaçınılmazdır: Docker soketine erişim pratikte root yetkisidir.
// Modelin özü bu ayrıcalığı, şema doğrulamalı ve denetlenebilir küçük bir
// yüzeye hapsetmektir. Değişmez: ayrıcalıklı kod 2500 satırı geçerse ne
// eklendiği sorgulanır (docs/decisions.md K-002).
package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"strconv"
	"strings"

	"google.golang.org/grpc"

	kadranexec "github.com/erkanrzgc/kadran/internal/exec"
	"github.com/erkanrzgc/kadran/internal/grpcserve"
	"github.com/erkanrzgc/kadran/internal/logutil"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/peercred"
	"github.com/erkanrzgc/kadran/internal/sdnotify"
	"github.com/erkanrzgc/kadran/internal/sockets"
	"github.com/erkanrzgc/kadran/internal/version"
)

const (
	defaultSocket  = "/run/kadran-exec/exec.sock"
	defaultJournal = "/var/lib/kadran-exec/exec-audit.log"
	// defaultVaultKey, kasa anahtarı (K-123). Yol bayrak VARSAYILANI, birim
	// dosyasında yazılı değil: ExecStart'ı yeniden yazan bir drop-in
	// (--allow-repo) yeni bir bayrağı düşürürdü.
	defaultVaultKey    = "/var/lib/kadran-exec/vault.key"
	defaultAllowedUser = "kadran"
	defaultOwnerGroup  = "kadran"
)

func main() {
	if err := run(); err != nil {
		slog.Error("executor başlatılamadı", "hata", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		socketPath   = flag.String("socket", defaultSocket, "dinlenecek unix soketi")
		journalPath  = flag.String("journal", defaultJournal, "denetim günlüğü dosyası")
		dockerSocket = flag.String("docker-socket", kadranexec.DefaultDockerSocket, "Docker Engine API soketi")
		allowUser    = flag.String("allow-user", defaultAllowedUser, "bağlanmasına izin verilen tek kullanıcı")
		ownerGroup   = flag.String("owner-group", defaultOwnerGroup, "soket ve günlük dosyasının grubu")
		showVersion  = flag.Bool("version", false, "sürümü yazdır ve çık")
		vaultKey     = flag.String("vault-key", defaultVaultKey, "kasa anahtarı (K-123)")
		debug        = flag.Bool("debug", false, "ayrıntılı günlük (KADRAN_DEBUG=1 ile de açılır)")
		gitHosts     = flag.String("allow-git-host", kadranexec.DefaultGitHost,
			"ImageBuild için izinli git sunucuları (virgülle ayrılmış)")
		gitRepos = flag.String("allow-repo", "",
			"derlenmesine izin verilen owner/repo çiftleri (virgülle ayrılmış; "+
				"boş = kısıt yok). Hostta git kimlik bilgisi varsa DOLDURULMALIDIR")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("kadran-exec %s (%s) protokol %d\n", version.Version, version.Commit, version.Protocol)
		return nil
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logutil.Level(*debug, os.Getenv),
	})))

	// Executor'ın root çalıştığını doğrula. Root DEĞİLSE Docker'a
	// erişemez ve her ayrıcalıklı işlem anlaşılmaz hatalarla başarısız
	// olur. Baştan ve açıkça reddetmek daha iyidir.
	if euid := os.Geteuid(); euid != 0 {
		return fmt.Errorf(
			"executor root çalışmalı, efektif uid %d bulundu — systemd unit dosyasını kontrol edin", euid)
	}

	vault, err := kadranexec.LoadVaultIdentity(*vaultKey)
	if err != nil {
		return err
	}

	allowedUID, err := lookupUID(*allowUser)
	if err != nil {
		return err
	}
	groupGID, err := lookupGID(*ownerGroup)
	if err != nil {
		return err
	}

	// Yalnızca kadrand bağlanabilir. Root'a bile örtük izin yoktur:
	// executor'a ulaşabilen tek kimlik `kadran` kullanıcısıdır.
	policy := peercred.Policy{AllowUIDs: []uint32{allowedUID}}
	creds, err := peercred.TransportCredentials(policy)
	if err != nil {
		return fmt.Errorf("çağıran doğrulaması kurulamadı: %w", err)
	}

	// Günlük açılışta zincirini doğrular. Bozuksa executor BAŞLAMAZ:
	// denetim bütünlüğü şüpheliyken ayrıcalıklı işlem yapmak, hizmet
	// vermemekten daha kötüdür.
	journal, err := kadranexec.OpenJournal(kadranexec.JournalOptions{
		Path:     *journalPath,
		GroupGID: groupGID,
	})
	if err != nil {
		return fmt.Errorf("denetim günlüğü açılamadı: %w", err)
	}
	defer func() {
		if err := journal.Close(); err != nil {
			slog.Error("denetim günlüğü kapatılamadı", "hata", err)
		}
	}()

	service, err := kadranexec.NewServer(kadranexec.ServerOptions{
		Journal:         journal,
		DockerSocket:    *dockerSocket,
		AllowedGitHosts: splitHosts(*gitHosts),
		AllowedRepos:    splitHosts(*gitRepos),
		Vault:           vault,
	})
	if err != nil {
		return err
	}

	if err := sockets.EnsureParentDir(*socketPath); err != nil {
		return err
	}
	listener, err := sockets.Listen(sockets.ListenOptions{
		Path: *socketPath,
		Mode: 0o660,
		GID:  groupGID,
	})
	if err != nil {
		return err
	}

	server := grpc.NewServer(grpc.Creds(creds))
	kadranv1.RegisterExecutorServiceServer(server, service)

	seq, _ := journal.Head()
	// ⚠ Beyaz listenin durumu KASTEN günlüğe yazılıyor.
	//
	// Hostta bir git kimlik bilgisi varken boş bir beyaz liste, token'ın
	// gördüğü HER özel deponun ImageBuild ile okunabilmesi demektir
	// (internal/exec/repoallow.go). Executor bu durumu güvenilir biçimde
	// TESPİT EDEMİYOR — kimlik bilgisi başka bir credential helper'da da
	// olabilir — ama GÖRÜNÜR kılabilir. Operatör "depo_kisiti=YOK"
	// satırını görmeli.
	repoLimit := "YOK (kısıt uygulanmıyor)"
	if n := len(splitHosts(*gitRepos)); n > 0 {
		repoLimit = fmt.Sprintf("%d depo", n)
	}
	slog.Info("executor hazır",
		"surum", version.Version,
		"soket", *socketPath,
		"izinli_uid", allowedUID,
		"denetim_kaydi", seq,
		"depo_kisiti", repoLimit,
		"kasa", vault.Recipient().String(),
	)

	if err := sdnotify.Ready(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
		slog.Warn("systemd bilgilendirilemedi", "hata", err)
	}

	return grpcserve.Run(server, listener)
}

// splitHosts, virgülle ayrılmış host listesini böler ve boşları atar.
func splitHosts(v string) []string {
	var out []string
	for _, h := range strings.Split(v, ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func lookupUID(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf(
			"%q kullanıcısı bulunamadı — `kadran bootstrap` çalıştırıldı mı?: %w", name, err)
	}
	id, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%q kullanıcısının uid'i çözümlenemedi: %w", name, err)
	}
	return uint32(id), nil
}

func lookupGID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, fmt.Errorf(
			"%q grubu bulunamadı — `kadran bootstrap` çalıştırıldı mı?: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("%q grubunun gid'i çözümlenemedi: %w", name, err)
	}
	return id, nil
}
