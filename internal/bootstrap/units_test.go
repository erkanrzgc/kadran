package bootstrap

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// # Bu dosya neden var?
//
// systemd, ad alanı kurarken bir bind kaynağını bulamazsa birimi HİÇ
// başlatmaz:
//
//	Failed to set up mount namespacing: /run/docker.sock: No such file
//	or directory              → status=226/NAMESPACE
//
// Bu tam olarak gerçek sunucuda yaşandı: `BindReadOnlyPaths=` baştaki
// `-` olmadan yazılmıştı ve taze bir makinede (Docker kurulu değil)
// `panely bootstrap` kurulumu tamamlayamadı. Ne birim testleri ne WSL
// ne de CI yakalayabilirdi — hiçbiri systemd ad alanı yalıtımını bu
// biçimde kurmuyor.

// panelyOwnedPaths, kurulumun BAŞLATMADAN ÖNCE var ettiği yollar.
//
// install.sh ve tmpfiles bunları oluşturuyor, dolayısıyla zorunlu
// (öneksiz) olmaları doğru: yoksalar kurulum zaten bozuktur ve birimin
// sessizce başlaması yanıltıcı olurdu.
var panelyOwnedPaths = map[string]bool{
	"/var/lib/panely":  true,
	"/run/panely":      true,
	"/run/panely-exec": true,
}

// TestUnitsDoNotHardRequireForeignPaths, Panely'nin oluşturmadığı bir
// yola ZORUNLU bağımlılık kurulmadığını doğrular.
//
// Kural: kaynağını biz yaratmıyorsak `-` ile isteğe bağlı olmalı.
func TestUnitsDoNotHardRequireForeignPaths(t *testing.T) {
	dir := filepath.Join("..", "..", "deploy", "systemd")
	girisler, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("birim dizini okunamadı: %v", err)
	}

	sinanan := 0
	for _, giris := range girisler {
		if !strings.HasSuffix(giris.Name(), ".service") {
			continue
		}
		sinanan++
		yol := filepath.Join(dir, giris.Name())
		icerik, err := os.ReadFile(yol)
		if err != nil {
			t.Fatalf("%s okunamadı: %v", giris.Name(), err)
		}

		tarayici := bufio.NewScanner(strings.NewReader(string(icerik)))
		satirNo := 0
		for tarayici.Scan() {
			satirNo++
			satir := strings.TrimSpace(tarayici.Text())
			if strings.HasPrefix(satir, "#") {
				continue
			}
			anahtar, deger, ok := strings.Cut(satir, "=")
			if !ok || !yolDirektifi(anahtar) {
				continue
			}
			for _, ham := range strings.Fields(deger) {
				if strings.HasPrefix(ham, "-") {
					continue // isteğe bağlı, sorun yok
				}
				if panelyOwnedPaths[ham] {
					continue // kurulumun garanti ettiği yol
				}
				t.Errorf("%s:%d — %s=%s: Panely bu yolu oluşturmuyor, "+
					"`-` öneki şart. Öneksizken systemd kaynak yoksa "+
					"birimi hiç başlatmaz (226/NAMESPACE).",
					giris.Name(), satirNo, anahtar, ham)
			}
		}
	}

	// Testin gerçekten bir şeye baktığından emin ol: dizin taşınırsa ya
	// da dosyalar yeniden adlandırılırsa sessizce "geçti" demesin.
	if sinanan == 0 {
		t.Fatal("hiçbir .service dosyası sınanmadı — test bir şey ölçmüyor")
	}
}

// TestDockerSocketBindIsOptional, somut gerilemeyi doğrudan sabitler.
//
// Yukarıdaki genel kural yanlışlıkla gevşetilirse bu satır yine de
// düşer; gerçekten yaşanmış bir hatanın nöbetçisi.
func TestDockerSocketBindIsOptional(t *testing.T) {
	yol := filepath.Join("..", "..", "deploy", "systemd", "panely-exec.service")
	icerik, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("birim okunamadı: %v", err)
	}
	metin := string(icerik)

	if !strings.Contains(metin, "BindReadOnlyPaths=-/run/docker.sock") {
		t.Error("docker.sock bind'i isteğe bağlı değil. Taze sunucuda " +
			"Docker kurulu olmadığı için panely-exec hiç başlamaz ve " +
			"bootstrap kurulumu tamamlayamaz.")
	}
}

// yolDirektifi, değeri dosya sistemi yolu taşıyan systemd anahtarlarını
// tanır.
func yolDirektifi(anahtar string) bool {
	switch anahtar {
	case "BindPaths", "BindReadOnlyPaths",
		"ReadWritePaths", "ReadOnlyPaths", "InaccessiblePaths":
		return true
	}
	return false
}

// TestUnitExecStartFlagsExist, birim dosyalarının binary'de TANIMLI
// OLMAYAN bir bayrak geçmediğini doğrular.
//
// Gerçekten yaşanmış hata: panelyd.service uzun süre
// `--config /etc/panely/panelyd.toml` diyordu. Ne böyle bir bayrak ne
// böyle bir dosya vardı; birim tasarım oturmadan yazılmış ve öylece
// kalmıştı. Sonuç, taze sunucuda:
//
//	flag provided but not defined: -config
//	status=2/INVALIDARGUMENT      → sonsuz yeniden başlatma döngüsü
//
// Birim testleri binary'yi sınıyordu, birim dosyasını kimse sınamıyordu.
// Aradaki boşluk tam olarak burası.
func TestUnitExecStartFlagsExist(t *testing.T) {
	esler := map[string]string{
		"panelyd.service":     filepath.Join("..", "..", "cmd", "panelyd", "main.go"),
		"panely-exec.service": filepath.Join("..", "..", "cmd", "panely-exec", "main.go"),
	}

	for birim, kaynak := range esler {
		t.Run(birim, func(t *testing.T) {
			tanimli, err := bayrakTanimlari(kaynak)
			if err != nil {
				t.Fatalf("kaynak okunamadı: %v", err)
			}
			// Boş yere geçmeyi engelle: kod biçimi değişip regex
			// tutmazsa test sessizce "geçti" dememeli.
			if len(tanimli) == 0 {
				t.Fatalf("%s içinde hiç bayrak tanımı bulunamadı — "+
					"test bir şey ölçmüyor", kaynak)
			}

			kullanilan, err := execStartBayraklari(
				filepath.Join("..", "..", "deploy", "systemd", birim))
			if err != nil {
				t.Fatalf("birim okunamadı: %v", err)
			}
			if len(kullanilan) == 0 {
				t.Fatalf("%s içinde ExecStart bayrağı bulunamadı — "+
					"test bir şey ölçmüyor", birim)
			}

			for _, bayrak := range kullanilan {
				if !tanimli[bayrak] {
					t.Errorf("%s, binary'de tanımlı olmayan --%s bayrağını "+
						"geçiyor. Servis 2/INVALIDARGUMENT ile ölür ve "+
						"sonsuz yeniden başlar. Tanımlılar: %v",
						birim, bayrak, siraliAnahtarlar(tanimli))
				}
			}
		})
	}
}

// bayrakTanimlari, bir main.go içindeki flag.X("ad", ...) çağrılarından
// bayrak adlarını toplar.
func bayrakTanimlari(kaynak string) (map[string]bool, error) {
	icerik, err := os.ReadFile(kaynak)
	if err != nil {
		return nil, err
	}
	adlar := map[string]bool{}
	for _, satir := range strings.Split(string(icerik), "\n") {
		_, kalan, ok := strings.Cut(satir, "flag.")
		if !ok {
			continue
		}
		_, kalan, ok = strings.Cut(kalan, `("`)
		if !ok {
			continue
		}
		ad, _, ok := strings.Cut(kalan, `"`)
		if ok && ad != "" {
			adlar[ad] = true
		}
	}
	return adlar, nil
}

// execStartBayraklari, ExecStart satırlarındaki --bayrak adlarını toplar.
// Devam satırları (`\`) da izlenir.
func execStartBayraklari(birimYolu string) ([]string, error) {
	icerik, err := os.ReadFile(birimYolu)
	if err != nil {
		return nil, err
	}

	var komut strings.Builder
	devam := false
	for _, ham := range strings.Split(string(icerik), "\n") {
		satir := strings.TrimSpace(ham)
		if strings.HasPrefix(satir, "#") {
			continue
		}
		if !devam && !strings.HasPrefix(satir, "ExecStart=") {
			continue
		}
		devam = strings.HasSuffix(satir, `\`)
		komut.WriteString(" " + strings.TrimSuffix(satir, `\`))
	}

	var bayraklar []string
	for _, parca := range strings.Fields(komut.String()) {
		if !strings.HasPrefix(parca, "--") {
			continue
		}
		ad, _, _ := strings.Cut(strings.TrimPrefix(parca, "--"), "=")
		if ad != "" {
			bayraklar = append(bayraklar, ad)
		}
	}
	return bayraklar, nil
}

func siraliAnahtarlar(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── Ters vekilin yetki ayrımı ────────────────────────────────────────
//
// Bu testler tek bir cümleyi koruyor: internete bakan süreç, ayrıcalıklı
// executor'a ULAŞAMAMALI. Sınır iki ayrı yerde duruyor ve ikisi de
// gözden kaçmaya müsait.

func readUnit(t *testing.T, name string) string {
	t.Helper()
	yol := filepath.Join("..", "..", "deploy", "systemd", name)
	icerik, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", name, err)
	}
	return string(icerik)
}

// directive, bir birim dosyasından bir anahtarın DEĞERLERİNİ çıkarır.
//
// Yorum satırları atlanıyor: bu birimlerde yorumlar uzun ve içlerinde
// anahtar adları geçiyor ("Group= neden değiştirilmemeli" gibi). Ham
// strings.Contains bunları ayar sanır ve test yanlış yere yeşil verirdi.
func directive(unit, key string) []string {
	var out []string
	tarayici := bufio.NewScanner(strings.NewReader(unit))
	for tarayici.Scan() {
		satir := strings.TrimSpace(tarayici.Text())
		if satir == "" || strings.HasPrefix(satir, "#") || strings.HasPrefix(satir, ";") {
			continue
		}
		if ad, deger, ok := strings.Cut(satir, "="); ok &&
			strings.EqualFold(strings.TrimSpace(ad), key) {
			out = append(out, strings.TrimSpace(deger))
		}
	}
	return out
}

// TestReverseProxyIsNotInThePanelyGroup, ters vekilin panely grubuna
// GİRMEDİĞİNİ doğrular.
//
// Girseydi /run/panely-exec/exec.sock'a (0660 root:panely) ulaşırdı: yani
// internete bakan süreç ayrıcalıklı executor'a konuşabilirdi. Bu, tüm
// ayrıcalık ayrımının çöktüğü tek satır olurdu.
func TestReverseProxyIsNotInThePanelyGroup(t *testing.T) {
	unit := readUnit(t, "panely-caddy.service")

	gruplar := directive(unit, "Group")
	if len(gruplar) != 1 || gruplar[0] != "panely-caddy" {
		t.Fatalf("Group= beklenmedik: %v (yalnızca panely-caddy olmalı)", gruplar)
	}

	// SupplementaryGroups arka kapıyı yeniden açardı.
	for _, ek := range directive(unit, "SupplementaryGroups") {
		for _, ad := range strings.Fields(ek) {
			if ad == "panely" {
				t.Errorf("ters vekile panely ek grubu verilmiş: %q", ek)
			}
		}
	}

	if k := directive(unit, "User"); len(k) != 1 || k[0] != "panely-caddy" {
		t.Errorf("User= beklenmedik: %v", k)
	}
}

// TestAdminSocketCarriesGroupOwnershipNotMembership, panelyd'nin admin
// soketine nasıl ULAŞTIĞINI doğrular.
//
// Erişim grup ÜYELİĞİYLE değil, SOKETİN grup sahipliğiyle sağlanıyor.
// Fark tam da yukarıdaki testin koruduğu şey: üyelik verilseydi exec.sock
// da açılırdı.
func TestAdminSocketCarriesGroupOwnershipNotMembership(t *testing.T) {
	unit := readUnit(t, "panely-caddy-admin.socket")

	beklenenler := map[string]string{
		"SocketUser":  "panely-caddy",
		"SocketGroup": "panely",
		"SocketMode":  "0660",
		// .socket birimi varsayılan olarak AYNI ADLI .service'i tetikler;
		// bizimki farklı adda ve bu satır olmadan hiç başlamıyor
		// (gerçek sunucuda ölçüldü).
		"Service": "panely-caddy.service",
	}
	for anahtar, beklenen := range beklenenler {
		got := directive(unit, anahtar)
		if len(got) != 1 || got[0] != beklenen {
			t.Errorf("%s= %v, beklenen [%s]", anahtar, got, beklenen)
		}
	}
}

// TestReverseProxyKeepsOnlyThePortBindingCapability, ters vekile :80/:443
// dışında bir yetenek verilmediğini doğrular.
func TestReverseProxyKeepsOnlyThePortBindingCapability(t *testing.T) {
	unit := readUnit(t, "panely-caddy.service")

	for _, anahtar := range []string{"AmbientCapabilities", "CapabilityBoundingSet"} {
		got := directive(unit, anahtar)
		if len(got) != 1 || got[0] != "CAP_NET_BIND_SERVICE" {
			t.Errorf("%s= %v — yalnızca CAP_NET_BIND_SERVICE olmalı", anahtar, got)
		}
	}

	// Bir web sunucusu AF_INET'siz çalışamaz; ama NETLINK gibi fazlalıklar
	// sessizce eklenmemeli.
	aileler := directive(unit, "RestrictAddressFamilies")
	if len(aileler) != 1 {
		t.Fatalf("RestrictAddressFamilies= %v", aileler)
	}
	izinli := map[string]bool{"AF_INET": true, "AF_INET6": true, "AF_UNIX": true}
	for _, aile := range strings.Fields(aileler[0]) {
		if !izinli[aile] {
			t.Errorf("beklenmedik adres ailesi: %q", aile)
		}
	}
}
