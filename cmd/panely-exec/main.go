// panely-exec, Panely'nin ayrıcalıklı executor'ıdır.
//
// ════════════════════════════════════════════════════════════════════
//  BU BINARY ROOT ÇALIŞIR. EKLENEN HER SATIR AYRICALIKLI YÜZEYDİR.
// ════════════════════════════════════════════════════════════════════
//
// Ayrıcalık kaçınılmazdır: Docker soketine erişim pratikte root yetkisidir.
// Modelin özü bu ayrıcalığı, şema doğrulamalı ve denetlenebilir küçük bir
// yüzeye hapsetmektir. Değişmez: internal/exec 2000 satırı geçerse ne
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

	"google.golang.org/grpc"

	panelyexec "github.com/erkanrzgc/panely/internal/exec"
	"github.com/erkanrzgc/panely/internal/grpcserve"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/peercred"
	"github.com/erkanrzgc/panely/internal/sdnotify"
	"github.com/erkanrzgc/panely/internal/sockets"
	"github.com/erkanrzgc/panely/internal/version"
)

const (
	defaultSocket      = "/run/panely-exec/exec.sock"
	defaultJournal     = "/var/lib/panely/exec-audit.log"
	defaultAllowedUser = "panely"
	defaultOwnerGroup  = "panely"
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
		dockerSocket = flag.String("docker-socket", panelyexec.DefaultDockerSocket, "Docker Engine API soketi")
		allowUser    = flag.String("allow-user", defaultAllowedUser, "bağlanmasına izin verilen tek kullanıcı")
		ownerGroup   = flag.String("owner-group", defaultOwnerGroup, "soket ve günlük dosyasının grubu")
		showVersion  = flag.Bool("version", false, "sürümü yazdır ve çık")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("panely-exec %s (%s) protokol %d\n", version.Version, version.Commit, version.Protocol)
		return nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Executor'ın root çalıştığını doğrula. Root DEĞİLSE Docker'a
	// erişemez ve her ayrıcalıklı işlem anlaşılmaz hatalarla başarısız
	// olur. Baştan ve açıkça reddetmek daha iyidir.
	if euid := os.Geteuid(); euid != 0 {
		return fmt.Errorf(
			"executor root çalışmalı, efektif uid %d bulundu — systemd unit dosyasını kontrol edin", euid)
	}

	allowedUID, err := lookupUID(*allowUser)
	if err != nil {
		return err
	}
	groupGID, err := lookupGID(*ownerGroup)
	if err != nil {
		return err
	}

	// Yalnızca panelyd bağlanabilir. Root'a bile örtük izin yoktur:
	// executor'a ulaşabilen tek kimlik `panely` kullanıcısıdır.
	policy := peercred.Policy{AllowUIDs: []uint32{allowedUID}}
	creds, err := peercred.TransportCredentials(policy)
	if err != nil {
		return fmt.Errorf("çağıran doğrulaması kurulamadı: %w", err)
	}

	// Günlük açılışta zincirini doğrular. Bozuksa executor BAŞLAMAZ:
	// denetim bütünlüğü şüpheliyken ayrıcalıklı işlem yapmak, hizmet
	// vermemekten daha kötüdür.
	journal, err := panelyexec.OpenJournal(panelyexec.JournalOptions{
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

	service, err := panelyexec.NewServer(panelyexec.ServerOptions{
		Journal:      journal,
		DockerSocket: *dockerSocket,
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
	panelyv1.RegisterExecutorServiceServer(server, service)

	seq, _ := journal.Head()
	slog.Info("executor hazır",
		"surum", version.Version,
		"soket", *socketPath,
		"izinli_uid", allowedUID,
		"denetim_kaydi", seq,
	)

	if err := sdnotify.Ready(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
		slog.Warn("systemd bilgilendirilemedi", "hata", err)
	}

	return grpcserve.Run(server, listener)
}

func lookupUID(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf(
			"%q kullanıcısı bulunamadı — `panely bootstrap` çalıştırıldı mı?: %w", name, err)
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
			"%q grubu bulunamadı — `panely bootstrap` çalıştırıldı mı?: %w", name, err)
	}
	id, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, fmt.Errorf("%q grubunun gid'i çözümlenemedi: %w", name, err)
	}
	return id, nil
}
