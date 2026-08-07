package execclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// fakeExecutor, yalnızca ImageBuild'i uygular.
//
// UnimplementedExecutorServiceServer gömülüyor: buf.gen.yaml
// `require_unimplemented_servers=false` ile üretiyor, yani ÜRETİM
// sunucuları bunu gömemez (yeni bir RPC derlemeyi kırsın diye). Testte
// gömmek o tripwire'ı zayıflatmaz — sınanan şey istemci tarafı.
type fakeExecutor struct {
	panelyv1.UnimplementedExecutorServiceServer

	// build, sunucunun akışa ne yazacağını belirler.
	build func(ctx context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error

	// seenDeadline, sunucunun GÖRDÜĞÜ bağlam son tarihidir. gRPC bunu
	// `grpc-timeout` başlığıyla taşır, yani istemcinin bağlamına konan
	// her sınır burada GÖZLENEBİLİR.
	seenDeadline time.Time
	seenHadLimit bool
}

func (f *fakeExecutor) ImageBuild(
	_ *panelyv1.ImageBuildRequest,
	stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse],
) error {
	f.seenDeadline, f.seenHadLimit = stream.Context().Deadline()
	return f.build(stream.Context(), stream)
}

// newFakeClient, bellek içi bir bağlantı üzerinden gerçek Client'ı kurar.
//
// Dial() ATLANIYOR çünkü o unix soketi ister ve bu test hem Windows iş
// istasyonunda hem Linux CI'da koşmalı. Sınanan şey Dial değil,
// ImageBuild'in GÖVDESİ — hatanın yaşayacağı yer tam olarak orası.
func newFakeClient(t *testing.T, srv *fakeExecutor) *Client {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	panelyv1.RegisterExecutorServiceServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatalf("sahte executor'a bağlanılamadı: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		gs.Stop()
	})

	return &Client{conn: conn, rpc: panelyv1.NewExecutorServiceClient(conn)}
}

func sendChunk(stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse], s string, stderr bool) error {
	return stream.Send(&panelyv1.ImageBuildResponse{Data: []byte(s), IsStderr: stderr})
}

// TestImageBuildImposesNoDeadlineOfItsOwn, K-044'ün bir katman yukarıda
// TEKRARLANMADIĞINI doğrular.
//
// ── Neden sunucunun gördüğü son tarihe bakılıyor? ──────────────────
//
// Doğrudan ölçüm mümkün değil: "60 saniye bekle, ölmedi mi" testi 60
// saniye sürerdi ve zamana bağlı testler kırılgandır. Ama gRPC, bağlam
// son tarihini `grpc-timeout` başlığıyla TELDEN taşıyor — yani istemci
// tarafında konan her sınır sunucuda GÖZLENEBİLİR bir olgu.
//
// Bu, hatanın YAŞADIĞI mekanizmaya doğrudan bakan bir ölçüt: birisi
// ImageBuild'e `context.WithTimeout(ctx, DefaultTimeout)` eklerse, bu
// test aynı saniyede kırmızıya döner.
func TestImageBuildImposesNoDeadlineOfItsOwn(t *testing.T) {
	srv := &fakeExecutor{
		build: func(_ context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			if err := sendChunk(stream, "Step 1/3\n", false); err != nil {
				return err
			}
			return stream.Send(&panelyv1.ImageBuildResponse{ImageId: "sha256:abc"})
		},
	}
	c := newFakeClient(t, srv)

	// Çağıran SINIRSIZ bir bağlam veriyor.
	_, err := c.ImageBuild(context.Background(), &panelyv1.ImageBuildRequest{},
		func([]byte, bool) error { return nil })
	if err != nil {
		t.Fatalf("derleme başarısız: %v", err)
	}

	if srv.seenHadLimit {
		t.Fatalf("ImageBuild kendi zaman aşımını dayattı (sunucu %v son tarihini gördü) — "+
			"uzun derlemeler ve akışlar bu yüzden bir kez öldü (K-044)",
			time.Until(srv.seenDeadline).Round(time.Second))
	}
}

// TestImageBuildHonoursCallerDeadline, sınırın TAMAMEN silinmediğini
// doğrular.
//
// Yukarıdaki testin tek başına yanlış yönlendirmesi mümkündü: "hiç sınır
// yok" da yanlış olurdu — asılı kalan bir executor'a bağlanan akış
// sonsuza kadar bekler. Doğru davranış "sınırı ÇAĞIRAN koyar".
func TestImageBuildHonoursCallerDeadline(t *testing.T) {
	srv := &fakeExecutor{
		build: func(_ context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			return stream.Send(&panelyv1.ImageBuildResponse{ImageId: "sha256:abc"})
		},
	}
	c := newFakeClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 42*time.Second)
	defer cancel()

	if _, err := c.ImageBuild(ctx, &panelyv1.ImageBuildRequest{},
		func([]byte, bool) error { return nil }); err != nil {
		t.Fatalf("derleme başarısız: %v", err)
	}

	if !srv.seenHadLimit {
		t.Fatal("çağıranın son tarihi executor'a HİÇ ulaşmadı — " +
			"asılı kalan bir executor akışı sonsuza kadar tutabilirdi")
	}
	if d := time.Until(srv.seenDeadline); d > 45*time.Second {
		t.Errorf("executor'a ulaşan sınır %v, çağıranınki 42s idi", d.Round(time.Second))
	}
}

// TestImageBuildNeedsImageIDToSucceed, K-042'nin istemci tarafında da
// zorlandığını doğrular: hatasız biten bir akış TEK BAŞINA başarı değil.
func TestImageBuildNeedsImageIDToSucceed(t *testing.T) {
	srv := &fakeExecutor{
		build: func(_ context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			// Çıktı akıyor, hata yok, ama kimlik karesi HİÇ gelmiyor.
			if err := sendChunk(stream, "Step 1/3\n", false); err != nil {
				return err
			}
			return sendChunk(stream, "Step 2/3\n", false)
		},
	}
	c := newFakeClient(t, srv)

	id, err := c.ImageBuild(context.Background(), &panelyv1.ImageBuildRequest{},
		func([]byte, bool) error { return nil })
	if err == nil {
		t.Fatalf("kimlik karesi olmadan başarı bildirildi (imageID=%q)", id)
	}
	if !strings.Contains(err.Error(), "kanıtlanamadı") {
		t.Errorf("hata mesajı sebebi açıklamıyor: %v", err)
	}
}

func TestImageBuildForwardsBothStreams(t *testing.T) {
	srv := &fakeExecutor{
		build: func(_ context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			if err := sendChunk(stream, "cikti\n", false); err != nil {
				return err
			}
			if err := sendChunk(stream, "hata\n", true); err != nil {
				return err
			}
			return stream.Send(&panelyv1.ImageBuildResponse{ImageId: "sha256:xyz"})
		},
	}
	c := newFakeClient(t, srv)

	type chunk struct {
		text   string
		stderr bool
	}
	var got []chunk

	id, err := c.ImageBuild(context.Background(), &panelyv1.ImageBuildRequest{},
		func(data []byte, stderr bool) error {
			got = append(got, chunk{string(data), stderr})
			return nil
		})
	if err != nil {
		t.Fatalf("derleme başarısız: %v", err)
	}
	if id != "sha256:xyz" {
		t.Errorf("imaj kimliği %q", id)
	}

	want := []chunk{{"cikti\n", false}, {"hata\n", true}}
	if len(got) != len(want) {
		t.Fatalf("%d parça alındı, %d bekleniyordu: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d. parça %v, beklenen %v", i, got[i], want[i])
		}
	}
}

// TestImageBuildDoesNotEmitTheIDFrameAsOutput, kimlik karesinin günlük
// olarak istemciye SIZMADIĞINI doğrular.
//
// Sızsaydı kullanıcının derleme çıktısının sonunda boş bir satır belirir
// ve daha kötüsü, `data` alanı boş bir kareyi "çıktı" sayan bir tüketici
// akışın bittiğini sanabilirdi.
func TestImageBuildDoesNotEmitTheIDFrameAsOutput(t *testing.T) {
	srv := &fakeExecutor{
		build: func(_ context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			return stream.Send(&panelyv1.ImageBuildResponse{ImageId: "sha256:abc"})
		},
	}
	c := newFakeClient(t, srv)

	emitted := 0
	if _, err := c.ImageBuild(context.Background(), &panelyv1.ImageBuildRequest{},
		func([]byte, bool) error { emitted++; return nil }); err != nil {
		t.Fatalf("derleme başarısız: %v", err)
	}
	if emitted != 0 {
		t.Errorf("kimlik karesi çıktı olarak yayıldı (%d kez)", emitted)
	}
}

// TestImageBuildStopsWhenSinkFails, tüketici yazamadığında akışın
// sürdürülmediğini doğrular.
func TestImageBuildStopsWhenSinkFails(t *testing.T) {
	srv := &fakeExecutor{
		build: func(ctx context.Context, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
			for range 100 {
				if err := sendChunk(stream, "satir\n", false); err != nil {
					return err
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
			// 100 parça da gitti: kimlik karesi HİÇ gönderilmiyor, yani
			// tüketici hatası yutulsaydı test yine de düşerdi.
			return nil
		},
	}
	c := newFakeClient(t, srv)

	sinkErr := errors.New("istemci gitti")
	_, err := c.ImageBuild(context.Background(), &panelyv1.ImageBuildRequest{},
		func([]byte, bool) error { return sinkErr })

	if !errors.Is(err, sinkErr) {
		t.Fatalf("tüketici hatası yukarı taşınmadı: %v", err)
	}
}
