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
// İzlenseydi, dağıtılan uygulama panelyd'ye istediği adrese istek
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
		t.Fatalf("yönlendirme İZLENDİ (%d istek) — panelyd keyfi bir adrese bağlandı", n)
	}
}

// Gövde SINIRLI okunmalı: sağlıksız bir uygulama sonsuz akış üretebilir
// ve yoklayıcı onu yutmaya çalışırsa panelyd'nin belleğini bitirir.
func TestProbeDoesNotDrainUnboundedBodies(t *testing.T) {
	ip, port := serveOn(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		chunk := strings.Repeat("x", 32*1024)
		for i := 0; i < 512; i++ { // 16 MB
			if _, err := w.Write([]byte(chunk)); err != nil {
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
			t.Fatalf("yoklama başarısız: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("yoklama 16 MB gövdeyi yutmaya çalıştı")
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
