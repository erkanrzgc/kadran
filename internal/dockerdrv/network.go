package dockerdrv

import (
	"context"
	"errors"
	"net/http"
)

// networkBody, `POST /networks/create` gövdesidir.
//
// Sürücü `Driver` alanını GÖNDERMEZ; Docker varsayılanı `bridge`'tir.
// Sürücü seçilebilseydi `host` seçilebilirdi ve uygulama host ağına
// düşerdi — şemada `host_network` alanının olmamasıyla kapatılan sınıfın
// aynısı, bu katmanda tekrar açılırdı.
type networkBody struct {
	Name string `json:"Name"`
	// Internal, ağı dış dünyadan yalıtır. Uygulamalar birbirine ve
	// Caddy'ye iç ağdan ulaşır; dışarı çıkışa ihtiyaçları yok.
	//
	// ⚠ Bu alan ŞU AN false: derleme (Faz 1) ve servis kataloğu (Faz 2)
	// yerleşene kadar uygulamaların paket indirmesi gerekebiliyor.
	// Sınırı gizlemektense yazıyoruz — kapatılması Faz 2'nin işi.
	Internal bool              `json:"Internal"`
	Labels   map[string]string `json:"Labels"`
}

// NetworkEnsure, uygulamanın iç ağını oluşturur ve adını döndürür.
//
// Ad İSTEKTEN ALINMAZ, app_id'den türetilir (NetworkName). Seçilebilseydi
// mevcut bir ağa — örneğin bir veritabanının ağına — bağlanmak mümkün
// olurdu.
//
// İdempotan: ağ zaten varsa Docker HTTP 409 döndürür ve bu başarı sayılır.
func (c *Client) NetworkEnsure(ctx context.Context, appID string) (string, error) {
	name := NetworkName(appID)
	body := networkBody{
		Name:     name,
		Internal: false,
		// Öksüz ağları bulabilmek için Panely işareti.
		Labels: map[string]string{labelAppID: appID},
	}

	err := c.doJSON(ctx, http.MethodPost, "/networks/create", nil, body, nil)
	if err == nil {
		return name, nil
	}

	// 409 = zaten var. Bunu başarı saymak "ensure" semantiğinin kendisi.
	//
	// Yarış koşulu bilinçli olarak böyle çözülüyor: önce "var mı" diye
	// sorup sonra oluşturmak TOCTOU'dur — iki dağıtım aynı anda koşarsa
	// ikisi de "yok" görüp ikisi de oluşturmaya çalışır. Doğrudan
	// oluşturup çakışmayı kabul etmek yarışı tamamen ortadan kaldırır.
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict {
		return name, nil
	}
	return "", err
}
