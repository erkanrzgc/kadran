package bootstrap

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unitOku, deploy/systemd altındaki bir birim dosyasının YÖNERGELERİNİ
// döndürür — yorumlar ATILIR.
//
// ── Yorumları atmak neden ZORUNLU ────────────────────────────────────
//
// İlk hâli dosyanın tamamında düz metin araması yapıyordu ve testler
// kırmızıya döndü: `panely-offsite.service`'in AÇIKLAMA satırında
// "panelyd `IPAddressDeny=any` taşıyor" yazıyor. Yani test, birimin
// yapılandırmasını değil, kendi gerekçesini okumuştu.
//
// Bu projede tanıdık bir sınıf: yüzey denetçisi de bir dönem yorum
// satırlarını ayrıcalıklı kod sayıyordu. Bir dosyayı "içinde X geçiyor
// mu" diye sınamak, X'i ANLATAN bir yorumu da yakalar.
func unitOku(t *testing.T, ad string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", ad))
	if err != nil {
		t.Fatalf("%s okunamadı: %v", ad, err)
	}

	var yonergeler []string
	for _, satir := range strings.Split(string(b), "\n") {
		kirpik := strings.TrimSpace(satir)
		if kirpik == "" || strings.HasPrefix(kirpik, "#") || strings.HasPrefix(kirpik, ";") {
			continue
		}
		yonergeler = append(yonergeler, kirpik)
	}
	return strings.Join(yonergeler, "\n")
}

// TestOffsiteUploaderIsTheOnlyUnitWithNetwork, ağ yetkisinin YALNIZCA
// yükleyicide olduğunu doğrular.
//
// ── Kilitlenen mimari karar ──────────────────────────────────────────
//
// panelyd `IPAddressDeny=any` taşıyor: ele geçirilen bir kontrol
// düzlemi dışarı veri sızdıramasın diye (K-092'de kontrol gruplu
// ölçüldü — kısıtlı istek `status=6`, kısıtsız `302`).
//
// Uzak yedek yüklemesi ağ istiyor. Bu yetenek panelyd'ye EKLENMEDİ;
// ayrı bir birime verildi. İkisi aynı anda ağ görürse ayrım anlamını
// yitirir, ikisi de görmezse yedek dışarı çıkamaz.
//
// Hiçbir birim testi bunu göremezdi: kural iki AYRI dosya arasındaki
// ilişkide yaşıyor.
func TestOffsiteUploaderIsTheOnlyUnitWithNetwork(t *testing.T) {
	daemon := unitOku(t, "panelyd.service")
	if !strings.Contains(daemon, "IPAddressDeny=any") {
		t.Error("panelyd.service artık IPAddressDeny=any taşımıyor — " +
			"ele geçirilen kontrol düzlemi dışarı veri sızdırabilir")
	}

	yukleyici := unitOku(t, "panely-offsite.service")
	if strings.Contains(yukleyici, "IPAddressDeny=any") {
		t.Error("panely-offsite.service ağa çıkamıyor — uzak yedek " +
			"yüklenemez, birim her koşuda başarısız olur")
	}
	if !strings.Contains(yukleyici, "RestrictAddressFamilies=") {
		t.Error("yükleyicide RestrictAddressFamilies yok — adres ailesi sınırsız")
	}
}

// TestOffsiteUploaderCanResolveNamesButNotReachLocalhost, yerel ağ
// engelinin DNS çözücüsünü kapatmadığını — ve istisnanın çözücüden
// geniş olmadığını — doğrular.
//
// ── Neden var (K-107) ────────────────────────────────────────────────
//
// Birim ilk gerçek ağ koşusunda düştü: `IPAddressDeny=localhost`,
// systemd-resolved'ın 127.0.0.53'teki çözücüsünü de kapatıyordu ve
// rclone R2'nin adını çözemedi. K-098'in sınaması yerel bir rclone
// hedefiyle yapıldığı için ağ yolu hiç yürünmemişti.
//
// İki yön de kilitleniyor: istisna yoksa yedek hiç yüklenmez; istisna
// `localhost` ya da `127.0.0.0/8` kadar genişse yükleyici host'taki
// yerel servislere ulaşabilir — engelin tüm amacı buydu.
func TestOffsiteUploaderCanResolveNamesButNotReachLocalhost(t *testing.T) {
	var deny, allow []string
	for _, satir := range strings.Split(unitOku(t, "panely-offsite.service"), "\n") {
		if deger, ok := strings.CutPrefix(satir, "IPAddressDeny="); ok {
			deny = append(deny, strings.Fields(deger)...)
		}
		if deger, ok := strings.CutPrefix(satir, "IPAddressAllow="); ok {
			allow = append(allow, strings.Fields(deger)...)
		}
	}

	if !slices.Contains(deny, "localhost") {
		t.Fatalf("IPAddressDeny localhost'u engellemiyor (%q) — ölçüm geçersiz: "+
			"korunan bir şey yok", deny)
	}
	if !slices.Contains(allow, "127.0.0.53") {
		t.Errorf("IPAddressAllow 127.0.0.53'ü içermiyor (%q) — localhost engeli "+
			"DNS çözücüsünü de kapatır, R2'nin adı çözülemez ve her yükleme düşer", allow)
	}
	for _, a := range allow {
		if a != "127.0.0.53" {
			t.Errorf("IPAddressAllow %q içeriyor — istisna yalnızca DNS çözücüsü "+
				"olmalı; daha genişi yükleyiciyi host'un yerel servislerine açar", a)
		}
	}
}

// TestOffsiteUploaderCannotWriteToDisk, yükleyicinin yerel yedekleri
// BOZAMAYACAĞINI doğrular.
//
// Yükleyici yalnızca okuyup ağa yazıyor; dosya sistemine yazma ihtiyacı
// YOK. `ProtectSystem=strict` her şeyi salt okunur yapıyor ve
// `ReadWritePaths` VERİLMİYOR. Sonuç: bu süreç ele geçirilse bile
// yerel yedekleri silemez ya da değiştiremez.
//
// ⚠ Bu özellik canlıda KAZARA kanıtlandı. Boru hattı yerel bir rclone
// hedefiyle sınanırken 24 dosyanın hepsi şu hatayla düştü:
//
//	Failed to copy: open /var/backups/...: read-only file system
//
// Üretimde hedef ağ olduğu için sorun değil — ama kısıtın gerçekten
// uygulandığını göstermiş oldu.
func TestOffsiteUploaderCannotWriteToDisk(t *testing.T) {
	icerik := unitOku(t, "panely-offsite.service")

	if !strings.Contains(icerik, "ProtectSystem=strict") {
		t.Error("ProtectSystem=strict yok — yükleyici dosya sistemine yazabilir")
	}
	for _, satir := range strings.Split(icerik, "\n") {
		if strings.HasPrefix(strings.TrimSpace(satir), "ReadWritePaths=") {
			t.Errorf("yükleyiciye yazma izni verilmiş: %q — "+
				"ele geçirilirse YEREL YEDEKLERİ bozabilir", satir)
		}
	}
	if !strings.Contains(icerik, "User=panely") {
		t.Error("User=panely yok — yükleyici yanlış kimlikle koşuyor")
	}
	if !strings.Contains(icerik, "CapabilityBoundingSet=") {
		t.Error("CapabilityBoundingSet boşaltılmamış")
	}
}

// daemonYazilabilirYollar, panelyd.service'in ReadWritePaths
// yönergelerini döndürür — daemon'un yazabildiği, dolayısıyla içindeki
// HER dosyayı silip değiştirebildiği dizinler.
func daemonYazilabilirYollar(t *testing.T) []string {
	t.Helper()
	var yollar []string
	for _, satir := range strings.Split(unitOku(t, "panelyd.service"), "\n") {
		if deger, ok := strings.CutPrefix(satir, "ReadWritePaths="); ok {
			yollar = append(yollar, strings.Fields(deger)...)
		}
	}
	if len(yollar) == 0 {
		t.Fatal("panelyd.service'te ReadWritePaths yok — ölçüm geçersiz: " +
			"karşılaştırılacak bir şey bulunamadı")
	}
	return yollar
}

// altinda, yol'un dizin'in kendisi ya da altında olup olmadığını söyler.
func altinda(yol, dizin string) bool {
	dizin = strings.TrimSuffix(dizin, "/")
	return yol == dizin || strings.HasPrefix(yol, dizin+"/")
}

// TestOffsiteRcloneConfigOutsideDaemonDirs, yükleyicinin rclone
// yapılandırmasının daemon'un DEĞİŞTİREMEYECEĞİ bir yerde olduğunu
// doğrular.
//
// ── Kapatılan delik (K-100) ──────────────────────────────────────────
//
// Birim RCLONE_CONFIG tanımlamıyordu. rclone o zaman
// `$HOME/.config/rclone` altına bakar ve `panely`nin ev dizini
// /var/lib/panely — daemon'un kendi dizini. Dosya root'a ait olsa bile
// daemon onu silip yerine kendisininkini koyabilir; bu canlıda bir
// sınama dosyasıyla ölçüldü. rclone yapılandırması komut çalıştırabildiği
// için (webdav `bearer_token_command`) bu, ağı olmayan daemon'a ağ gören
// bir süreçte komut çalıştırma yolu açardı.
//
// İki dosya arasındaki bir ilişki: hiçbir birim testi tek başına göremez.
func TestOffsiteRcloneConfigOutsideDaemonDirs(t *testing.T) {
	var yol string
	for _, satir := range strings.Split(unitOku(t, "panely-offsite.service"), "\n") {
		if deger, ok := strings.CutPrefix(satir, "Environment=RCLONE_CONFIG="); ok {
			yol = strings.TrimSpace(deger)
		}
	}
	if yol == "" {
		t.Fatal("panely-offsite.service RCLONE_CONFIG tanımlamıyor — rclone " +
			"$HOME/.config/rclone'a, yani daemon'un dizinine bakar")
	}
	if !strings.HasPrefix(yol, "/") {
		t.Fatalf("RCLONE_CONFIG göreli: %q — hangi dizine düştüğü çalışma dizinine bağlı", yol)
	}

	for _, dizin := range daemonYazilabilirYollar(t) {
		if altinda(yol, dizin) {
			t.Errorf("rclone yapılandırması %q, daemon'un yazabildiği %q altında — "+
				"ele geçirilen panelyd yükleyicinin yapılandırmasını değiştirebilir", yol, dizin)
		}
	}
}

// TestOffsiteTimerSurvivesDowntime, kaçan bir koşunun telafi
// edildiğini doğrular.
//
// `Persistent=true` olmasaydı, bir gün kapalı kalan sunucu o günün
// yedeğini SESSİZCE atlardı — ve bu, tam olarak yedeğe en çok
// ihtiyaç duyulan senaryodur.
func TestOffsiteTimerSurvivesDowntime(t *testing.T) {
	icerik := unitOku(t, "panely-offsite.timer")

	if !strings.Contains(icerik, "Persistent=true") {
		t.Error("Persistent=true yok — kapalı kalan sunucu yedeği sessizce atlar")
	}
	if !strings.Contains(icerik, "OnCalendar=") {
		t.Error("OnCalendar yok — zamanlayıcı hiç tetiklenmez")
	}
}
