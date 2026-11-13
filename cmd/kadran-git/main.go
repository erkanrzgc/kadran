// kadran-git, GitHub bağlantısını ve özel depo arşivlerini yöneten ayrıcalıksız
// daemon'dır (K-167).
//
// Kendi kullanıcısıyla çalışır ve systemd tarafından iki unix soketinde
// etkinleştirilir:
//
//   - admin: yalnız `kadran` kullanıcısı (kadrand) çağırabilir; bağlanma,
//     durum, bağlantıyı kesme ve dal çözme.
//   - fetch: yalnız root (yürütücü) çağırabilir; kapsam kararı ve arşiv akışı.
//
// Jeton, cihaz kodu ve yanıt gövdeleri hiçbir günlüğe ya da gRPC durumuna girmez.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"sync"
	"syscall"

	"google.golang.org/grpc"

	"github.com/erkanrzgc/kadran/internal/github"
	"github.com/erkanrzgc/kadran/internal/grpcserve"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/peercred"
	"github.com/erkanrzgc/kadran/internal/sdnotify"
	"github.com/erkanrzgc/kadran/internal/version"
)

const (
	defaultState     = "/var/lib/kadran-git/github.json"
	defaultAdminUser = "kadran"
	// maxRecvBytes: istekler küçüktür (sahip, depo, dal).
	maxRecvBytes = 64 << 10
)

func main() {
	if err := run(); err != nil {
		slog.Error("kadran-git could not start", "error", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		stateFile   = flag.String("state", defaultState, "GitHub token state file")
		clientID    = flag.String("client-id", github.DefaultClientID, "GitHub App client ID")
		appSlug     = flag.String("app-slug", github.DefaultAppSlug, "GitHub App short name")
		adminUser   = flag.String("admin-user", defaultAdminUser, "the only user allowed on the admin socket")
		showVersion = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Printf("kadran-git %s (%s) protocol %d\n", version.Version, version.Commit, version.Protocol)
		return nil
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Ayrıcalıksız daemon root çalışırsa jeton dosyası ve soket politikası anlamsızlaşır.
	if os.Geteuid() == 0 {
		return errors.New("kadran-git must not run as root")
	}
	adminUID, err := lookupUID(*adminUser)
	if err != nil {
		return err
	}
	listeners, err := activated(os.Getenv, os.Getpid())
	if err != nil {
		return fmt.Errorf("socket activation: %w", err)
	}
	unsetActivationEnv()

	svc, err := github.New(github.Config{ClientID: *clientID, AppSlug: *appSlug, StateFile: *stateFile})
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	defer svc.Close()

	admin, err := newAdminServer(svc, adminUID)
	if err != nil {
		return err
	}
	fetch, err := newFetchServer(svc)
	if err != nil {
		return err
	}
	return serveBoth(admin, listeners[socketAdmin], fetch, listeners[socketFetch])
}

// newAdminServer yalnız `kadran` kullanıcısını kabul eden sunucuyu kurar.
func newAdminServer(svc gitService, adminUID uint32) (*grpc.Server, error) {
	creds, err := peercred.TransportCredentials(peercred.Policy{AllowUIDs: []uint32{adminUID}})
	if err != nil {
		return nil, fmt.Errorf("could not set up caller verification: %w", err)
	}
	s := grpc.NewServer(grpc.Creds(creds), grpc.MaxRecvMsgSize(maxRecvBytes))
	kadranv1.RegisterGitAdminServiceServer(s, &adminServer{svc: svc})
	return s, nil
}

// newFetchServer yalnız root'u (yürütücü) kabul eden sunucuyu kurar.
func newFetchServer(svc gitService) (*grpc.Server, error) {
	creds, err := peercred.TransportCredentials(peercred.Policy{AllowUIDs: []uint32{0}})
	if err != nil {
		return nil, fmt.Errorf("could not set up caller verification: %w", err)
	}
	s := grpc.NewServer(grpc.Creds(creds), grpc.MaxRecvMsgSize(maxRecvBytes))
	kadranv1.RegisterGitFetchServiceServer(s, &fetchServer{svc: svc})
	return s, nil
}

// serveBoth iki sunucuyu tek sinyal bağlamında çalıştırır; biri dönerse
// ötekini de durdurur ve ikisinin bitmesini bekler.
func serveBoth(admin *grpc.Server, adminLn net.Listener, fetch *grpc.Server, fetchLn net.Listener) error {
	sig, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(sig)
	defer cancel()

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, s := range []struct {
		srv *grpc.Server
		ln  net.Listener
	}{{admin, adminLn}, {fetch, fetchLn}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			errs[i] = grpcserve.RunContext(ctx, s.srv, s.ln)
		}()
	}
	slog.Info("kadran-git ready", "version", version.Version)
	if err := sdnotify.Ready(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
		slog.Warn("could not notify systemd")
	}
	wg.Wait()
	return errors.Join(errs...)
}

func lookupUID(name string) (uint32, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, fmt.Errorf("admin user %q not found: %w", name, err)
	}
	id, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("could not parse the uid of user %q: %w", name, err)
	}
	return uint32(id), nil
}
