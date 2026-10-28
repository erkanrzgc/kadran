package proxydrv

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Canlı geri okuma, aracının (proxyguard) son savunması: her yüklemeden ve
// her vekil denetiminden sonra SafeRoutes, Caddy'nin `GET /config/` cevabını
// FixedConfig'in kurabileceğiyle DeepEqual karşılaştırıyor. Bu karşılaştırma
// ancak cevaptaki HER alan bir Go alanına düşerse anlamlı: düz
// json.Unmarshal modellenmemiş alanı sessizce atıyordu ve `abort`,
// `transport`, dosya okuyan bir `handle_response` ya da dosyaya yazan bir
// log `writer`'ı taşıyan canlı yapılandırma "güvenli" sayılıyordu (K-154).

// canliKume, geri okuma testlerinin kümesi: bir domain rotası ve bir
// yönlendirme, yani FixedConfig'in kurabildiği üç işleyici türünün hepsi
// (encode, reverse_proxy, static_response).
func canliKume() RouteSet {
	return RouteSet{
		Routes:    []ProxyRoute{{Domain: "example.com", Upstreams: []ProxyUpstream{{IP: "172.18.0.3", Port: 8080}}}},
		Redirects: []ProxyRedirect{{From: "www.example.com", To: "example.com"}},
	}
}

// temizAgac, FixedConfig çıktısını genel bir JSON ağacı olarak verir. Caddy
// de geri okumayı sakladığı ağaçtan üretiyor (admin.go, unsyncedConfigAccess).
func temizAgac(t *testing.T) map[string]any {
	t.Helper()
	cfg, err := FixedConfig(canliKume())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var agac map[string]any
	if err := json.Unmarshal(raw, &agac); err != nil {
		t.Fatal(err)
	}
	return agac
}

// caddyGibi, ağacı Caddy'nin admin cevabı gibi kodlar: json.Encoder, yani
// sonda bir satır sonu.
func caddyGibi(t *testing.T, agac map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(agac)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

// ekle, ağaçta yolun gösterdiği nesneye yeni bir anahtar koyar. Yol yoksa
// ya da anahtar zaten varsa test düşer: yoksa kurcalama hiçbir şey
// eklemeden ölçülürdü.
func ekle(t *testing.T, agac map[string]any, yol []any, anahtar string, deger any) {
	t.Helper()
	m := bul(t, agac, yol)
	if _, varMi := m[anahtar]; varMi {
		t.Fatalf("yol %v: %q zaten var", yol, anahtar)
	}
	m[anahtar] = deger
}

// bul, ağaçta yolun gösterdiği nesneyi verir; yol yoksa test düşer.
func bul(t *testing.T, agac map[string]any, yol []any) map[string]any {
	t.Helper()
	var cur any = agac
	for _, adim := range yol {
		switch a := adim.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("yol %v: %q bir nesnenin içinde değil", yol, a)
			}
			if cur, ok = m[a]; !ok {
				t.Fatalf("yol %v: %q yok", yol, a)
			}
		case int:
			s, ok := cur.([]any)
			if !ok || a >= len(s) {
				t.Fatalf("yol %v: %d. öğe yok", yol, a)
			}
			cur = s[a]
		default:
			t.Fatalf("yol %v: geçersiz adım %v", yol, adim)
		}
	}
	m, ok := cur.(map[string]any)
	if !ok {
		t.Fatalf("yol %v bir nesneye varmıyor", yol)
	}
	return m
}

// TestLiveReadbackRejectsUnmodelledFields: yapılandırmanın HER yapı
// düzeyine (Config, Admin, Logging, LogConfig, Apps, HTTPApp, HTTPServer,
// Route, Match, Handler, Upstream) eklenen ve Go yapılarında karşılığı
// olmayan bir alan, geri okumayı düşürür. Tek bir düzeyin gevşemesi o
// düzeyin satırını kırmızıya çevirir.
func TestLiveReadbackRejectsUnmodelledFields(t *testing.T) {
	rota := []any{"apps", "http", "servers", serverName, "routes"}
	at := func(ek ...any) []any { return append(append([]any(nil), rota...), ek...) }
	cases := []struct {
		ad      string
		yol     []any
		anahtar string
		deger   any
	}{
		{"kök: depolama", nil, "storage", map[string]any{"module": "file_system", "root": "/tmp/x"}},
		{"admin: kalıcı yapılandırma", []any{"admin"}, "config", map[string]any{"persist": true}},
		{"logging: sink", []any{"logging"}, "sink", map[string]any{"writer": map[string]any{"output": "stderr"}}},
		{"log: dosyaya yazan writer", []any{"logging", "logs", "default"}, "writer",
			map[string]any{"output": "file", "filename": "/etc/cron.d/x"}},
		{"apps: tls", []any{"apps"}, "tls", map[string]any{"automation": map[string]any{}}},
		{"http: grace_period", []any{"apps", "http"}, "grace_period", "1s"},
		{"sunucu: automatic_https", []any{"apps", "http", "servers", serverName}, "automatic_https",
			map[string]any{"disable": true}},
		{"rota: group", at(0), "group", "g"},
		{"eşleşme: path", at(0, "match", 0), "path", []any{"/admin"}},
		{"encode: minimum_length", at(0, "handle", 0), "minimum_length", 1},
		{"reverse_proxy: transport", at(0, "handle", 1), "transport",
			map[string]any{"protocol": "http", "tls": map[string]any{}}},
		{"reverse_proxy: handle_response (dosya okur)", at(0, "handle", 1), "handle_response",
			[]any{map[string]any{"routes": []any{map[string]any{"handle": []any{
				map[string]any{"handler": "static_response", "body": "{file./etc/shadow}"}}}}}}},
		{"upstream: max_requests", at(0, "handle", 1, "upstreams", 0), "max_requests", 1},
		{"static_response: abort", at(1, "handle", 0), "abort", true},
		{"static_response: close", at(1, "handle", 0), "close", true},
		// Go'nun çözücüsü anahtarı büyük/küçük harf duyarsız eşliyor:
		// "Upstreams" bilinmeyen alan SAYILMAZ, `upstreams` alanına düşer ve
		// Marshal sırasıyla sonra gelen `upstreams` onu ezer. Canlıda iki
		// upstream listesi olur, karşılaştırma birini hiç görmez.
		{"reverse_proxy: harf varyantı Upstreams", at(0, "handle", 1), "Upstreams",
			[]any{map[string]any{"dial": "172.18.0.9:80"}}},
	}
	for _, tc := range cases {
		t.Run(tc.ad, func(t *testing.T) {
			agac := temizAgac(t)
			ekle(t, agac, tc.yol, tc.anahtar, tc.deger)
			_, err := decodeLive(caddyGibi(t, agac))
			if err == nil {
				t.Fatalf("%q eklenmiş canlı yapılandırma hatasız çözüldü", tc.anahtar)
			}
			// Bilinmeyen alanın reddi alanı ADLANDIRIR: vekil alarmının
			// ayrıntısında operatör neyin eklendiğini görür. Harf varyantı
			// bilinmeyen alan değil; onu ağaç karşılaştırması yakalıyor.
			if tc.anahtar == strings.ToLower(tc.anahtar) && !strings.Contains(err.Error(), `"`+tc.anahtar+`"`) {
				t.Fatalf("ret sebebi %q alanını adlandırmıyor: %v", tc.anahtar, err)
			}
		})
	}
}

// TestLiveReadbackRejectsCaseVariantKeys: modellenmiş bir alanın harf
// varyantıyla yazılmış anahtar ("Admin", "Dial") aynı Go alanına düşer ve
// DisallowUnknownFields onu görmez. Geri okuma yalnız alanların kendi
// yazımını kabul eder.
func TestLiveReadbackRejectsCaseVariantKeys(t *testing.T) {
	for _, tc := range []struct {
		ad         string
		yol        []any
		eski, yeni string
	}{
		{"kök: Admin", nil, "admin", "Admin"},
		{"upstream: Dial", []any{"apps", "http", "servers", serverName, "routes", 0, "handle", 1, "upstreams", 0}, "dial", "Dial"},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			agac := temizAgac(t)
			m := bul(t, agac, tc.yol)
			deger, ok := m[tc.eski]
			if !ok {
				t.Fatalf("%q yok", tc.eski)
			}
			delete(m, tc.eski)
			m[tc.yeni] = deger
			if _, err := decodeLive(caddyGibi(t, agac)); err == nil {
				t.Fatalf("%q yerine %q yazılmış canlı yapılandırma hatasız çözüldü", tc.eski, tc.yeni)
			}
		})
	}
}

// TestLiveReadbackOfFixedConfigIsAccepted, kontrol grubu: FixedConfig'in
// kendi çıktısı, Caddy'nin kodladığı biçimde geri okununca geçer ve aynı
// kümeyi verir. Bu olmasaydı "her şeyi reddet" de yukarıdaki testi geçerdi.
func TestLiveReadbackOfFixedConfigIsAccepted(t *testing.T) {
	cfg, err := decodeLive(caddyGibi(t, temizAgac(t)))
	if err != nil {
		t.Fatalf("temiz geri okuma çözülemedi: %v", err)
	}
	set, err := SafeRoutes(cfg)
	if err != nil {
		t.Fatalf("temiz geri okuma reddedildi: %v", err)
	}
	if !reflect.DeepEqual(set, canliKume()) {
		t.Fatalf("geri okunan küme farklı:\n got %+v\nwant %+v", set, canliKume())
	}
}

// TestStartupConfigPassesStrictReadback: açılışta Caddy rotasız
// `/etc/kadran/caddy.json` ile kalkıyor ve kadrand ilk okumayı o anda
// yapıyor (atlanan uygulamaların rotası taşınırken). Bu dosya geri okumada
// reddedilseydi her yeniden başlatma "vekil uzlaştırılamadı"ya düşerdi.
func TestStartupConfigPassesStrictReadback(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/caddy/config.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeLive(raw)
	if err != nil {
		t.Fatalf("açılış yapılandırması çözülemedi: %v", err)
	}
	set, err := SafeRoutes(cfg)
	if err != nil {
		t.Fatalf("açılış yapılandırması reddedildi: %v", err)
	}
	if len(set.Routes) != 0 || len(set.Redirects) != 0 {
		t.Fatalf("açılış yapılandırmasında rota olmamalı: %+v", set)
	}
}

// TestLiveReadbackRejectsTrailingData: cevapta ikinci bir JSON belgesi
// varsa hangisinin canlı olduğu belirsiz; reddedilir.
func TestLiveReadbackRejectsTrailingData(t *testing.T) {
	raw := append(caddyGibi(t, temizAgac(t)), []byte("{}\n")...)
	_, err := decodeLive(raw)
	if err == nil {
		t.Fatal("sonunda ikinci belge olan cevap kabul edildi")
	}
	// Sebep adıyla söylenir; yalnız "çözülemedi" operatöre neyin yanlış
	// olduğunu anlatmazdı.
	if !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("ret sebebi sondaki veriyi söylemiyor: %v", err)
	}
}

// TestClientCurrentRejectsUnmodelledLiveField: Current gerçekten bu katı
// yoldan geçiyor (aracının kullandığı uç). Yalnız Linux'ta koşar (adminKur).
func TestClientCurrentRejectsUnmodelledLiveField(t *testing.T) {
	agac := temizAgac(t)
	ekle(t, agac, []any{"apps", "http", "servers", serverName, "routes", 0, "handle", 1}, "transport",
		map[string]any{"protocol": "http"})
	c := adminKur(t, &sahteAdmin{canli: caddyGibi(t, agac)})
	if _, err := c.Current(context.Background()); err == nil {
		t.Fatal("transport eklenmiş canlı yapılandırma hatasız okundu")
	}
}
