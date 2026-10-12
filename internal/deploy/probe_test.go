package deploy

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// serveOn, verilen işleyiciyi bir yerel sunucuda açar ve yoklayıcının
// beklediği (ip, port) çiftini döndürür.
func serveOn(t *testing.T, h http.Handler) (string, uint32) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("test sunucusunun adresi çözümlenemedi: %v", err)
	}
	port, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		t.Fatalf("port ayrıştırılamadı: %v", err)
	}
	return host, uint32(port)
}

func code(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
}

func TestProbeAcceptsSuccessAndRedirectStatuses(t *testing.T) {
	for _, status := range []int{200, 204, 301, 302, 308, 399} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			ip, port := serveOn(t, code(status))
			p := NewHTTPProber(time.Second)
			if err := p.Probe(context.Background(), ip, port, "/"); err != nil {
				t.Fatalf("%d sağlıklı sayılmalıydı: %v", status, err)
			}
		})
	}
}

// 4xx/5xx SAĞLIKSIZ. Özellikle 404 önemli: konteyner ayakta ve cevap
// veriyor ama sağlık yolu yok — uygulamanın yanlış derlendiğinin en
// yaygın belirtisi.
func TestProbeRejectsClientAndServerErrors(t *testing.T) {
	for _, status := range []int{400, 404, 500, 502, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			ip, port := serveOn(t, code(status))
			p := NewHTTPProber(time.Second)
			err := p.Probe(context.Background(), ip, port, "/")
			if err == nil {
				t.Fatalf("%d sağlıklı sayıldı — bozuk sürüm kapıdan geçerdi", status)
			}
			if !strings.Contains(err.Error(), strconv.Itoa(status)) {
				t.Fatalf("hata durum kodunu taşımıyor: %v", err)
			}
		})
	}
}

// ⚠ GÜVENLİK: yönlendirme İZLENMEMELİ.
//
// İzlenseydi, dağıtılan uygulama kadrand'ye istediği adrese istek
// attırabilirdi — kontrol düzlemi, iş yükünün seçtiği bir hedefe bağlanan
// bir araca dönüşürdü. Sağlık yoklaması tek atımlık bir ölçümdür.
func TestProbeDoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	t.Cleanup(target.Close)

	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/gizli", http.StatusFound)
	}))

	p := NewHTTPProber(time.Second)
	if err := p.Probe(context.Background(), ip, port, "/"); err != nil {
		t.Fatalf("302 sağlıklı sayılmalıydı: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("yönlendirme İZLENDİ (%d istek) — kadrand keyfi bir adrese bağlandı", n)
	}
}

// Gövde SINIRLI okunmalı: sağlıksız bir uygulama sonsuz akış üretebilir
// ve yoklayıcı onu yutmaya çalışırsa kadrand'nin belleğini bitirir. Ama
// sınırı aşan büyük bir gövde (ör. 4 KB'tan büyük bir HTML ana sayfası)
// SAĞLIKLIDIR: sınıra hatasız ulaşılınca bağlantı kapatılır, kalanı
// okunmaz.
func TestProbeLargeBodyIsHealthyAndNotFullyRead(t *testing.T) {
	var written atomic.Int64
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		chunk := strings.Repeat("x", 32*1024)
		for i := 0; i < 512; i++ { // 16 MB
			n, err := w.Write([]byte(chunk))
			written.Add(int64(n))
			if err != nil {
				return
			}
		}
	}))

	p := NewHTTPProber(2 * time.Second)
	done := make(chan error, 1)
	go func() { done <- p.Probe(context.Background(), ip, port, "/") }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("16 MB gövde sağlıklı sayılmalıydı: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("yoklama 16 MB gövdeyi yutmaya çalıştı")
	}
	// Gövdenin tamamı okunmuş olsaydı sunucu 16 MB'ı yazabilirdi;
	// bağlantı kapandığı için yazma bundan çok önce hata vermeli.
	time.Sleep(200 * time.Millisecond)
	if got := written.Load(); got >= 16<<20 {
		t.Fatalf("gövdenin tamamı okundu (%d bayt)", got)
	}
}

// Content-Length'ten kısa kesilen gövde (sınırın ALTINDA) sağlıksızdır.
func TestProbeTruncatedBodyIsUnhealthy(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("kisa"))
		// İşleyici dönünce net/http bağlantıyı Content-Length dolmadan
		// keser.
	}))

	p := NewHTTPProber(2 * time.Second)
	if err := p.Probe(context.Background(), ip, port, "/"); err == nil {
		t.Fatal("eksik gövde sağlıklı sayıldı")
	}
}

// Başlığı gönderip gövdeyi sürüncemede bırakan uygulama zaman aşımına
// düşer.
func TestProbeSlowBodyTimesOut(t *testing.T) {
	release := make(chan struct{})
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("bir"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// Temizlik sırası LIFO: sunucu kapanmadan ÖNCE işleyici serbest
	// kalmalı, yoksa srv.Close sonsuza dek bekler.
	t.Cleanup(func() { close(release) })

	p := NewHTTPProber(300 * time.Millisecond)
	start := time.Now()
	if err := p.Probe(context.Background(), ip, port, "/"); err == nil {
		t.Fatal("takılan gövde sağlıklı sayıldı")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("zaman aşımı geç devreye girdi")
	}
}

func TestProbeFailsWhenNothingIsListening(t *testing.T) {
	// Kapalı bir port bulmak için sunucuyu açıp hemen kapatıyoruz.
	srv := httptest.NewServer(code(200))
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	port, _ := strconv.ParseUint(portStr, 10, 32)
	srv.Close()

	p := NewHTTPProber(time.Second)
	if err := p.Probe(context.Background(), host, uint32(port), "/"); err == nil {
		t.Fatal("kimse dinlemezken yoklama BAŞARILI döndü")
	}
}

// Yol, konağı DEĞİŞTİREMEMELİ. URL bir yapıdan kuruluyor, dize
// birleştirmesinden değil; aksi hâlde "/..@evil" gibi bir yol hedefi
// kaydırabilirdi.
func TestProbeTargetHostCannotBeAlteredByThePath(t *testing.T) {
	var gotPath atomic.Value
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.WriteHeader(200)
	}))

	p := NewHTTPProber(time.Second)
	if err := p.Probe(context.Background(), ip, port, "/@example.com/saglik"); err != nil {
		t.Fatalf("yoklama başarısız: %v", err)
	}
	if got, _ := gotPath.Load().(string); got != "/@example.com/saglik" {
		t.Fatalf("yol bozuldu: %q", got)
	}
}

func TestProbeHonoursContextCancellation(t *testing.T) {
	block := make(chan struct{})
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(200)
	}))
	t.Cleanup(func() { close(block) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewHTTPProber(5 * time.Second)
	if err := p.Probe(ctx, ip, port, "/"); err == nil {
		t.Fatal("iptal edilmiş bağlamda yoklama BAŞARILI döndü")
	}
}

func TestProbeRejectsStalledSuccessBody(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test response writer cannot flush")
			return
		}
		flusher.Flush()
		<-r.Context().Done()
	}))
	err := NewHTTPProber(40*time.Millisecond).Probe(context.Background(), ip, port, "/health")
	if err == nil {
		t.Fatal("200 headers with an incomplete, timed-out body were accepted as healthy")
	}
}

func TestProbeRejectsTruncatedSuccessBody(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	}))
	if err := NewHTTPProber(time.Second).Probe(context.Background(), ip, port, "/health"); err == nil {
		t.Fatal("an incomplete successful response was accepted as healthy")
	}
}

func TestProbeAcceptsCompletedBodyAtLimit(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", probeBodyLimit)))
	}))
	if err := NewHTTPProber(time.Second).Probe(context.Background(), ip, port, "/health"); err != nil {
		t.Fatalf("completed body at the limit was rejected: %v", err)
	}
}

// Sınıra HATASIZ ulaşıldıktan sonra gövdenin takılması ya da kesilmesi
// artık önemsiz: kalanı okunmaz, bağlantı kapanır (K-144 düzeltmesi).
func TestProbeBodyStalledAfterLimitIsHealthy(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(probeBodyLimit+1))
		_, _ = w.Write([]byte(strings.Repeat("x", probeBodyLimit)))
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test response writer cannot flush")
			return
		}
		flusher.Flush()
		<-r.Context().Done()
	}))
	if err := NewHTTPProber(time.Second).Probe(context.Background(), ip, port, "/health"); err != nil {
		t.Fatalf("body that reached the limit cleanly was rejected: %v", err)
	}
}

func TestProbeBodyTruncatedAfterLimitIsHealthy(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(probeBodyLimit+1))
		_, _ = w.Write([]byte(strings.Repeat("x", probeBodyLimit)))
	}))
	if err := NewHTTPProber(time.Second).Probe(context.Background(), ip, port, "/health"); err != nil {
		t.Fatalf("body that reached the limit cleanly was rejected: %v", err)
	}
}
