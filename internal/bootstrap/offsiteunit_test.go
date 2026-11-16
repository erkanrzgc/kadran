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
// kırmızıya döndü: `kadran-offsite.service`'in AÇIKLAMA satırında
// "kadrand `IPAddressDeny=any` taşıyor" yazıyor. Yani test, birimin
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
// kadrand `IPAddressDeny=any` taşıyor: ele geçirilen bir kontrol
// düzlemi dışarı veri sızdıramasın diye (K-092'de kontrol gruplu
// ölçüldü — kısıtlı istek `status=6`, kısıtsız `302`).
//
// Uzak yedek yüklemesi ağ istiyor. Bu yetenek kadrand'ye EKLENMEDİ;
// ayrı bir birime verildi. İkisi aynı anda ağ görürse ayrım anlamını
// yitirir, ikisi de görmezse yedek dışarı çıkamaz.
//
// Hiçbir birim testi bunu göremezdi: kural iki AYRI dosya arasındaki
// ilişkide yaşıyor.
func TestOffsiteUploaderIsTheOnlyUnitWithNetwork(t *testing.T) {
	daemon := unitOku(t, "kadrand.service")
	if !strings.Contains(daemon, "IPAddressDeny=any") {
		t.Error("kadrand.service artık IPAddressDeny=any taşımıyor — " +
			"ele geçirilen kontrol düzlemi dışarı veri sızdırabilir")
	}

	yukleyici := unitOku(t, "kadran-offsite.service")
	if strings.Contains(yukleyici, "IPAddressDeny=any") {
		t.Error("kadran-offsite.service ağa çıkamıyor — uzak yedek " +
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
	for _, satir := range strings.Split(unitOku(t, "kadran-offsite.service"), "\n") {
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

// TestOffsiteUploaderSeesTheStubResolver, yükleyicinin ad çözmek için
// süzgecinin İZİN VERDİĞİ çözücüye sorduğunu doğrular (K-168).
//
// GCP'de /etc/resolv.conf 169.254.169.254'ü gösteriyor ve birimin
// `link-local` engeli onu kapatıyor. Go'nun kendi çözücüsünü kullanan bir
// rclone orada her koşuda DNS'te düşer. Ölçüldü: `GODEBUG=netdns=go` ile
// çıkış 5, "write: operation not permitted"; bağla 181 kayıt. Dağıtımın
// cgo'lu rclone'u bugün düşmüyor, çünkü nsswitch'in `resolve` kaynağı onu
// resolved'a götürüyor. rclone.org'un ikilisi o yolu kullanmaz.
//
// Bağın kaynağı stub (127.0.0.53, süzgeç izin veriyor). Komşusu
// /run/systemd/resolve/resolv.conf DEĞİL: GCP'de o dosya tam da engellenen
// 169.254.169.254'ü taşıyor. `-` şart: kaynak Kadran'ın değil; öneksiz bağ
// resolved'sız sunucuda birimi hiç başlatmazdı (226/NAMESPACE). Çözücü o
// sunucuda süzgece takılıyorsa kurulum en başta durur (offsite_cozucu_kotu).
func TestOffsiteUploaderSeesTheStubResolver(t *testing.T) {
	const bag = "BindReadOnlyPaths=-/run/systemd/resolve/stub-resolv.conf:/etc/resolv.conf"
	yonergeler := strings.Split(unitOku(t, "kadran-offsite.service"), "\n")
	if !slices.Contains(yonergeler, bag) {
		t.Errorf("kadran-offsite.service %q taşımıyor — GCP'de resolv.conf "+
			"169.254.169.254'ü gösteriyor, süzgeç onu kapatıyor ve Go'nun "+
			"çözücüsünü kullanan bir rclone hiçbir adı çözemez", bag)
	}

	// Zamanlayıcı Persistent=true: kaçırılan koşu açılışta hemen başlar,
	// resolved stub'ı yazmadan önce de olabilir.
	var after []string
	for _, satir := range yonergeler {
		if deger, ok := strings.CutPrefix(satir, "After="); ok {
			after = append(after, strings.Fields(deger)...)
		}
	}
	if !slices.Contains(after, "systemd-resolved.service") {
		t.Errorf("After= systemd-resolved.service'i içermiyor (%q) — açılışta "+
			"yükleyici stub'dan önce koşabilir", after)
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
	icerik := unitOku(t, "kadran-offsite.service")

	if !strings.Contains(icerik, "ProtectSystem=strict") {
		t.Error("ProtectSystem=strict yok — yükleyici dosya sistemine yazabilir")
	}
	for _, satir := range strings.Split(icerik, "\n") {
		if strings.HasPrefix(strings.TrimSpace(satir), "ReadWritePaths=") {
			t.Errorf("yükleyiciye yazma izni verilmiş: %q — "+
				"ele geçirilirse YEREL YEDEKLERİ bozabilir", satir)
		}
	}
	if !strings.Contains(icerik, "User=kadran") {
		t.Error("User=kadran yok — yükleyici yanlış kimlikle koşuyor")
	}
	if !strings.Contains(icerik, "CapabilityBoundingSet=") {
		t.Error("CapabilityBoundingSet boşaltılmamış")
	}
}

// daemonYazilabilirYollar, kadrand.service'in ReadWritePaths
// yönergelerini döndürür — daemon'un yazabildiği, dolayısıyla içindeki
// HER dosyayı silip değiştirebildiği dizinler.
func daemonYazilabilirYollar(t *testing.T) []string {
	t.Helper()
	var yollar []string
	for _, satir := range strings.Split(unitOku(t, "kadrand.service"), "\n") {
		if deger, ok := strings.CutPrefix(satir, "ReadWritePaths="); ok {
			yollar = append(yollar, strings.Fields(deger)...)
		}
	}
	if len(yollar) == 0 {
		t.Fatal("kadrand.service'te ReadWritePaths yok — ölçüm geçersiz: " +
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
// `$HOME/.config/rclone` altına bakar ve `kadran`nin ev dizini
// /var/lib/kadran — daemon'un kendi dizini. Dosya root'a ait olsa bile
// daemon onu silip yerine kendisininkini koyabilir; bu canlıda bir
// sınama dosyasıyla ölçüldü. rclone yapılandırması komut çalıştırabildiği
// için (webdav `bearer_token_command`) bu, ağı olmayan daemon'a ağ gören
// bir süreçte komut çalıştırma yolu açardı.
//
// İki dosya arasındaki bir ilişki: hiçbir birim testi tek başına göremez.
func TestOffsiteRcloneConfigOutsideDaemonDirs(t *testing.T) {
	var yol string
	for _, satir := range strings.Split(unitOku(t, "kadran-offsite.service"), "\n") {
		if deger, ok := strings.CutPrefix(satir, "Environment=RCLONE_CONFIG="); ok {
			yol = strings.TrimSpace(deger)
		}
	}
	if yol == "" {
		t.Fatal("kadran-offsite.service RCLONE_CONFIG tanımlamıyor — rclone " +
			"$HOME/.config/rclone'a, yani daemon'un dizinine bakar")
	}
	if !strings.HasPrefix(yol, "/") {
		t.Fatalf("RCLONE_CONFIG göreli: %q — hangi dizine düştüğü çalışma dizinine bağlı", yol)
	}

	for _, dizin := range daemonYazilabilirYollar(t) {
		if altinda(yol, dizin) {
			t.Errorf("rclone yapılandırması %q, daemon'un yazabildiği %q altında — "+
				"ele geçirilen kadrand yükleyicinin yapılandırmasını değiştirebilir", yol, dizin)
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
	icerik := unitOku(t, "kadran-offsite.timer")

	if !strings.Contains(icerik, "Persistent=true") {
		t.Error("Persistent=true yok — kapalı kalan sunucu yedeği sessizce atlar")
	}
	if !strings.Contains(icerik, "OnCalendar=") {
		t.Error("OnCalendar yok — zamanlayıcı hiç tetiklenmez")
	}
}
