package bootstrap

import (
	"os"
	"path/filepath"
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
