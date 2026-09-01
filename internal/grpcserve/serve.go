// Package grpcserve, gRPC sunucularının yaşam döngüsünü yönetir.
//
// panelyd ve panely-exec aynı kapanış davranışına ihtiyaç duyar; tek yerde
// doğru yapmak iki yerde farklı yapmaktan iyidir.
package grpcserve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"github.com/erkanrzgc/panely/internal/sdnotify"
)

// Run, sunucuyu SIGINT veya SIGTERM gelene kadar çalıştırır, sonra açık
// istekleri tamamlayarak kapanır.
//
// # Neden zarif kapanış önemli?
//
// systemd yeniden başlatma veya güncelleme sırasında SIGTERM gönderir.
// Sert kapanış, o an süren bir ayrıcalıklı işlemi yarıda bırakabilir:
// oluşturulmuş ama kaydedilmemiş bir konteyner, yazılmamış bir denetim
// kaydı. GracefulStop açık istekleri bitirir, yeni istekleri reddeder.
func Run(server *grpc.Server, listener net.Listener) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return RunContext(ctx, server, listener)
}

// RunContext, kapanış sinyalini DIŞARIDAN gelen bağlamdan alır.
//
// ── Neden ayrı bir giriş noktası ────────────────────────────────────
//
// panelyd yalnızca gRPC sunmuyor: sağlık gözetmeni de arka planda
// koşuyor ve kapanışta o da durmalı. `Run` sinyal bağlamını kendi içinde
// kurup dışarı vermeseydi, gözetmen İKİNCİ bir bağlam kurardı — ikisi de
// çalışırdı ama kapanış sırası tanımsız kalırdı ve gözetmen, sunucu
// kapanırken hâlâ konteyner başlatmaya çalışabilirdi.
//
// `Run` korunuyor çünkü panely-exec'in arka plan işi yok ve kendi
// bağlamını kurmak zorunda kalmamalı.
func RunContext(ctx context.Context, server *grpc.Server, listener net.Listener) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		// Sunucu kendiliğinden durdu: dinleyici kapandı veya ölümcül hata.
		return normalize(err)

	case <-ctx.Done():
		slog.Info("kapanış sinyali alındı, açık istekler tamamlanıyor")
		if err := sdnotify.Stopping(); err != nil && !errors.Is(err, sdnotify.ErrNoSocket) {
			slog.Warn("systemd kapanış bildirimi başarısız", "hata", err)
		}
		server.GracefulStop()
		// GracefulStop döndüğünde Serve de dönmüştür.
		return normalize(<-serveErr)
	}
}

// normalize, beklenen kapanış hatalarını yok sayar.
func normalize(err error) error {
	switch {
	case err == nil, errors.Is(err, grpc.ErrServerStopped), errors.Is(err, net.ErrClosed):
		return nil
	default:
		return fmt.Errorf("gRPC hizmeti durdu: %w", err)
	}
}
