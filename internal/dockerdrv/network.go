package dockerdrv

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
)

// appSubnetPool, uygulama ağlarının alt ağının içinde kalması gereken aralık.
//
// kadrand yalnız buraya sağlık yoklaması yapabiliyor (`IPAddressAllow`,
// K-070) ve vekil aracısı upstream'i yalnız buradan kabul ediyor (K-143).
// Docker'ın varsayılan havuzu ~15 ağdan sonra 192.168.0.0/16'ya geçer; o ağdaki
// uygulamanın yoklaması `permission denied` ile, rotası aracıda düşerdi.
var appSubnetPool = netip.MustParsePrefix("172.16.0.0/12")

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
//
// Alt ağ her çağrıda denetlenir (K-150): appSubnetPool dışındaysa hata. Bu
// çağrının oluşturduğu ağ silinir; önceden var olan ağa dokunulmaz (üzerinde
// konteyner olabilir), yalnız dağıtım durur. Çare dockerd'nin adres havuzunu
// sabitlemek: `kadran bootstrap` /etc/docker/daemon.json'u yoksa yazıyor.
func (c *Client) NetworkEnsure(ctx context.Context, appID string) (string, error) {
	name := NetworkName(appID)
	body := networkBody{
		Name:     name,
		Internal: false,
		// Öksüz ağları bulabilmek için Kadran işareti.
		Labels: map[string]string{labelAppID: appID},
	}

	created := true
	err := c.doJSON(ctx, http.MethodPost, "/networks/create", nil, body, nil)
	if err != nil {
		// 409 = zaten var. Bunu başarı saymak "ensure" semantiğinin kendisi.
		//
		// Yarış koşulu bilinçli olarak böyle çözülüyor: önce "var mı" diye
		// sorup sonra oluşturmak TOCTOU'dur — iki dağıtım aynı anda koşarsa
		// ikisi de "yok" görüp ikisi de oluşturmaya çalışır. Doğrudan
		// oluşturup çakışmayı kabul etmek yarışı tamamen ortadan kaldırır.
		var apiErr *apiError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			return "", err
		}
		created = false
	}

	if err := c.checkNetworkSubnet(ctx, name); err != nil {
		if created {
			// Silme hatası yutuluyor: asıl hata alt ağ; kalan ağ bir
			// sonraki çağrıda yine reddedilir.
			_ = c.doJSON(ctx, http.MethodDelete, "/networks/"+name, nil, nil, nil)
		}
		return "", err
	}
	return name, nil
}

// checkNetworkSubnet, ağın her IPv4 alt ağının appSubnetPool içinde olduğunu
// ve en az bir IPv4 alt ağ bulunduğunu doğrular. /16'dan küçük alt ağ kabul:
// bootstrap'ın yazdığı havuz /16 verir, ama operatörün kendi /24 havuzu da
// çalışır (aracının ağ geçidi kuralı /16 varsayıyor; SECURITY.md, K-148).
func (c *Client) checkNetworkSubnet(ctx context.Context, name string) error {
	// `GET /networks/{ad}` yanıtının okunan kısmı.
	var n struct {
		IPAM struct{ Config []struct{ Subnet string } }
	}
	if err := c.doJSON(ctx, http.MethodGet, "/networks/"+name, nil, nil, &n); err != nil {
		return err
	}
	v4 := 0
	for _, cfg := range n.IPAM.Config {
		p, err := netip.ParsePrefix(cfg.Subnet)
		if err == nil && p.Addr().Is6() {
			continue
		}
		if err != nil || p.Bits() < appSubnetPool.Bits() || !appSubnetPool.Contains(p.Addr()) {
			return fmt.Errorf("docker network %s got subnet %q outside %s; pin dockerd's "+
				"default-address-pools (docs/guides/operations.md)", name, cfg.Subnet, appSubnetPool)
		}
		v4++
	}
	if v4 == 0 {
		return fmt.Errorf("docker network %s has no IPv4 subnet", name)
	}
	return nil
}
