package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Prober, bir replikanın CEVAP VERDİĞİNİ ölçer.
//
// Konteynerin ÇALIŞTIĞINI ölçmek ayrı bir sorudur ve onu `Lifecycle`
// cevaplıyor. İkisinin farkı Faz 1'in 4. kabul ölçütünün tamamı: bozuk
// bir commit çoğu zaman açılan ama 500 dönen bir uygulama üretir ve
// yalnızca konteyner durumuna bakan bir kapı onu canlıya alır.
type Prober interface {
	Probe(ctx context.Context, ip string, port uint32, path string) error
}

// probeBodyLimit, yanıt gövdesinden okunacak en fazla bayt.
//
// Gövdeyi hiç okumamak bağlantının yeniden kullanılmasını engeller;
// tamamını okumak ise sağlıksız bir uygulamanın sonsuz akışıyla panelyd'nin
// belleğini bitirmesine izin verir. Sağlık yanıtı için 4 KB fazlasıyla
// yeter.
const probeBodyLimit = 4 << 10

// DefaultProbeTimeout, tek bir yoklamanın toplam süre sınırı.
//
// Kapı aralığından (DefaultGate.Interval = 2sn) KISA tutuluyor: eşit ya
// da uzun olsaydı her tur yoklama süresi kadar uzar ve toplam süre
// sınırına sığan ölçüm sayısı sessizce düşerdi.
const DefaultProbeTimeout = 1500 * time.Millisecond

// HTTPProber, iç ağdaki konteynere düz HTTP isteği atar.
type HTTPProber struct{ client *http.Client }

// NewHTTPProber, tek atımlık bir sağlık yoklayıcısı kurar.
//
// ── Yönlendirme İZLENMİYOR ──────────────────────────────────────────
//
// İzlenseydi, dağıtılan uygulama 302 döndürerek panelyd'ye istediği
// adrese istek attırabilirdi: kontrol düzlemi, iş yükünün seçtiği bir
// hedefe bağlanan bir araca dönüşürdü. Birimdeki `IPAddressAllow` bunu
// ikinci katmanda da kapatıyor, ama savunma burada başlıyor.
//
// ⚠ `Timeout` GÖVDEYİ de kapsıyor (K-053'ün konusu). Sağlık yanıtları
// küçük olduğu ve gövde sınırlı okunduğu için burada doğru davranış bu:
// yanıt başlığını gönderip gövdeyi sürüncemede bırakan bir uygulama da
// sağlıksızdır.
func NewHTTPProber(timeout time.Duration) *HTTPProber {
	return &HTTPProber{client: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			// Yoklamalar seyrek ve kısa ömürlü; bağlantı havuzunu
			// büyütmenin anlamı yok. Konteyner öldüğünde havuzda kalan
			// ölü bir bağlantı, bir sonraki yoklamayı yanıltabilirdi.
			DisableKeepAlives:     true,
			DisableCompression:    true,
			ResponseHeaderTimeout: timeout,
		},
	}}
}

// Probe, tek bir replikanın sağlık yolunu yoklar.
//
// Sağlıklı = durum kodu 400'ün ALTINDA. 3xx dahil: bir uygulamanın kökten
// başka bir yola yönlendirmesi olağan ve o yanıtı verebiliyor olması,
// uygulamanın ayakta olduğunu kanıtlar.
//
// 404 SAĞLIKSIZ sayılıyor. Konteyner ayakta ve cevap veriyor olabilir ama
// sağlık yolunun bulunamaması, uygulamanın beklenen şeyi servis etmediğinin
// en yaygın belirtisi.
func (p *HTTPProber) Probe(ctx context.Context, ip string, port uint32, path string) error {
	if path == "" {
		return errors.New("sağlık yolu boş")
	}

	// URL bir YAPIDAN kuruluyor, dize birleştirmesinden değil: konak
	// alanı ayrı bir alan olduğu için hiçbir yol değeri hedefi
	// kaydıramaz.
	u := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(ip, strconv.FormatUint(uint64(port), 10)),
		Path:   path,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("sağlık isteği kurulamadı: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sağlık yoklaması başarısız (%s): %w", u.Host, err)
	}
	defer func() {
		// Sınırlı boşaltma: bağlantının düzgün kapanması için gövdeden
		// biraz okunması gerekiyor, hepsini okumak ise saldırı yüzeyi.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, probeBodyLimit))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("sağlık yoklaması %d döndü (%s%s)", resp.StatusCode, u.Host, path)
	}
	return nil
}
