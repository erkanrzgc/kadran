package bootstrap

import (
	"bufio"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// # Bu dosya neden var?
//
// Sağlık kapısı için panelyd'ye ağ verildi (AF_INET). Bu, en-az-yetki
// modelinde ölçülü bir taviz: yoklama panelyd'de yapılıyor çünkü
// ayrıcalıklı yüzey bütçesi (2500 satır) bir HTTP istemcisini kaldırmıyor.
//
// Tavizin dar kalmasını sağlayan tek şey birim dosyasındaki çit. Çit bir
// YORUMLA değil bir TESTLE korunmalı: "IPAddressAllow'u biraz genişletsem
// ne olur" diye düşünen gelecekteki birinin kırmızı bir test görmesi
// gerekiyor.

// birimAnahtarlari, bir birim dosyasındaki anahtarın tüm değerlerini
// döndürür (systemd bazı anahtarların tekrarlanmasına izin veriyor).
func birimAnahtarlari(t *testing.T, birim, anahtar string) []string {
	t.Helper()

	yol := filepath.Join("..", "..", "deploy", "systemd", birim)
	icerik, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", birim, err)
	}

	var out []string
	tarayici := bufio.NewScanner(strings.NewReader(string(icerik)))
	for tarayici.Scan() {
		satir := strings.TrimSpace(tarayici.Text())
		if strings.HasPrefix(satir, "#") {
			continue
		}
		ad, deger, bulundu := strings.Cut(satir, "=")
		if !bulundu || strings.TrimSpace(ad) != anahtar {
			continue
		}
		out = append(out, strings.TrimSpace(deger))
	}
	return out
}

// ⚠ EN ÖNEMLİ TEST: ayrıcalıklı binary ağa ÇIKAMAZ.
//
// panely-exec root çalışıyor ve Docker soketine erişiyor. Ona ağ vermek,
// "en az yetki" iddiasının kalan yarısını da silerdi: ele geçirilen bir
// executor hem konteyner çalıştırabilir hem dışarıyla konuşabilirdi.
func TestPrivilegedExecutorHasNoNetworkAtAll(t *testing.T) {
	degerler := birimAnahtarlari(t, "panely-exec.service", "RestrictAddressFamilies")
	if len(degerler) == 0 {
		t.Fatal("panely-exec.service içinde RestrictAddressFamilies YOK — " +
			"ayrıcalıklı süreç her adres ailesini açabilir")
	}
	for _, d := range degerler {
		for _, aile := range strings.Fields(d) {
			if aile != "AF_UNIX" {
				t.Errorf("ayrıcalıklı executor %s ailesini açıyor — "+
					"root + Docker soketi + ağ, en az yetki DEĞİLDİR", aile)
			}
		}
	}
}

// panelyd ağa çıkabiliyor ama YALNIZCA yoklama için gereken kadar.
func TestDaemonOpensOnlyTheAddressFamiliesTheProbeNeeds(t *testing.T) {
	degerler := birimAnahtarlari(t, "panelyd.service", "RestrictAddressFamilies")
	if len(degerler) == 0 {
		t.Fatal("panelyd.service içinde RestrictAddressFamilies YOK")
	}

	izinli := map[string]bool{"AF_UNIX": true, "AF_INET": true}
	gorulen := map[string]bool{}
	for _, d := range degerler {
		for _, aile := range strings.Fields(d) {
			gorulen[aile] = true
			if !izinli[aile] {
				t.Errorf("panelyd %s ailesini açıyor — sağlık yoklaması için "+
					"gerekmiyor (AF_NETLINK/AF_PACKET ham erişim demektir)", aile)
			}
		}
	}
	if !gorulen["AF_UNIX"] {
		t.Error("AF_UNIX kapalı — panelyd kendi soketini açamaz")
	}
	if !gorulen["AF_INET"] {
		t.Error("AF_INET kapalı — sağlık yoklaması hiç çalışamaz")
	}
}

// Çitin kendisi: varsayılan REDDET olmalı.
func TestDaemonDeniesAllAddressesByDefault(t *testing.T) {
	deny := birimAnahtarlari(t, "panelyd.service", "IPAddressDeny")
	if len(deny) == 0 {
		t.Fatal("IPAddressDeny YOK — AF_INET açıkken panelyd İNTERNETE çıkabilir")
	}
	var anyVar bool
	for _, d := range deny {
		if strings.EqualFold(strings.TrimSpace(d), "any") {
			anyVar = true
		}
	}
	if !anyVar {
		t.Errorf("IPAddressDeny=any yok (%v) — çit varsayılanı reddetmiyor", deny)
	}
}

// İzin verilen aralıklar dar ve ÖZEL olmalı.
//
// Somut yasaklar:
//   - 0.0.0.0/0 ya da "any": çitin tamamını iptal eder
//   - 169.254.0.0/16: bulut meta veri ucu (169.254.169.254) buradadır
//   - 192.168.0.0/16: kalıcı sunucunun EV AĞI orada
//   - loopback: hostun kendi servisleri
func TestDaemonAllowListStaysInsideDockersRange(t *testing.T) {
	allow := birimAnahtarlari(t, "panelyd.service", "IPAddressAllow")
	if len(allow) == 0 {
		t.Fatal("IPAddressAllow YOK — IPAddressDeny=any ile yoklama HİÇ çalışamaz")
	}

	// Yoklamanın gerçekten ulaşması gereken adres. ÖLÇÜLDÜ (gerçek
	// sunucu): uygulama ağları 172.18/16, 172.19/16, 172.20/16.
	hedef := netip.MustParseAddr("172.20.0.3")

	var hedefeUlasan bool
	for _, ham := range allow {
		for _, parca := range strings.Fields(ham) {
			if strings.EqualFold(parca, "any") || parca == "0.0.0.0/0" || parca == "::/0" {
				t.Fatalf("IPAddressAllow=%q çitin tamamını iptal ediyor", parca)
			}
			pre, err := netip.ParsePrefix(parca)
			if err != nil {
				t.Fatalf("IPAddressAllow=%q ayrıştırılamadı: %v", parca, err)
			}
			if !pre.Addr().IsPrivate() {
				t.Errorf("IPAddressAllow=%s ÖZEL bir aralık değil", parca)
			}
			if pre.Bits() < 12 {
				t.Errorf("IPAddressAllow=%s fazla geniş (/%d)", parca, pre.Bits())
			}
			// Ev ağı ve meta veri ucu açıkça yasak.
			for _, yasak := range []string{"192.168.0.0/16", "169.254.0.0/16", "127.0.0.0/8"} {
				y := netip.MustParsePrefix(yasak)
				if pre.Overlaps(y) {
					t.Errorf("IPAddressAllow=%s, %s ile ÇAKIŞIYOR", parca, yasak)
				}
			}
			if pre.Contains(hedef) {
				hedefeUlasan = true
			}
		}
	}

	// Pozitif kontrol: çit yalnızca dar değil, İŞE DE YARAMALI. Bu iddia
	// olmasaydı IPAddressAllow=10.255.255.255/32 gibi bir değer testi
	// geçer ama yoklamayı tamamen kırardı.
	if !hedefeUlasan {
		t.Errorf("izin listesi %v, ölçülen konteyner adresi %v'ye ULAŞMIYOR — "+
			"her dağıtım sağlık kapısında ölür", allow, hedef)
	}
}
