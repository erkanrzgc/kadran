package bootstrap

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gondericiBirimleri, Telegram'a çıkan iki birimdir. İkisi de aynı
// kısıtları taşımak ZORUNDA: biri gevşerse kısıtın anlamı kalmaz.
var gondericiBirimleri = []string{
	"panely-notify.service",
	"panely-notify-failure@.service",
}

// yonergeDegerleri, bir birimdeki bir yönergenin bütün değerlerini
// (boşlukla ayrılmış, birden çok satırdan) toplar.
func yonergeDegerleri(t *testing.T, birim, yonerge string) []string {
	t.Helper()
	var degerler []string
	for _, satir := range strings.Split(unitOku(t, birim), "\n") {
		if deger, ok := strings.CutPrefix(satir, yonerge+"="); ok {
			degerler = append(degerler, strings.Fields(deger)...)
		}
	}
	return degerler
}

// TestNotifyTokenIsHiddenFromDaemon, Telegram anahtarının panelyd'ye
// KAPALI olduğunu doğrular (K-108).
//
// ── Kilitlenen karar ─────────────────────────────────────────────────
//
// Gönderici `panely` kullanıcısıyla koşsaydı, anahtar dosyası `panely`
// tarafından okunabilir olmak zorunda kalırdı ve ele geçirilen daemon
// sizin adınıza mesaj atabilirdi. Bunun yerine:
//
//   - DynamicUser: her koşuda geçici, panely olmayan bir kullanıcı
//   - LoadCredential: systemd dosyayı ROOT olarak okuyup yalnızca bu
//     birime veriyor; dosya 0600 root:root kalabiliyor
//
// Canlıda ölçüldü: `panely` anahtar dosyasını okuyamıyor, birim
// kimlik bilgisini alabiliyor.
//
// Anahtar dosyası daemon'un yazabildiği bir dizinde de durmamalı:
// orada daemon onu silip kendi sohbet kimliğiyle değiştirerek alarmları
// başka yere yönlendirebilirdi (K-100'ün dersi).
func TestNotifyTokenIsHiddenFromDaemon(t *testing.T) {
	daemonDizinleri := daemonYazilabilirYollar(t)

	for _, birim := range gondericiBirimleri {
		t.Run(birim, func(t *testing.T) {
			if !slices.Contains(yonergeDegerleri(t, birim, "DynamicUser"), "yes") {
				t.Error("DynamicUser=yes yok — gönderici sabit bir kullanıcıyla koşuyor")
			}
			if kullanici := yonergeDegerleri(t, birim, "User"); len(kullanici) > 0 {
				t.Errorf("User=%v tanımlı — gönderici panelyd'nin kullanıcısıyla "+
					"koşarsa anahtar daemon'a açık olur", kullanici)
			}

			kimlik := yonergeDegerleri(t, birim, "LoadCredential")
			if len(kimlik) != 1 {
				t.Fatalf("LoadCredential tam bir kez tanımlı olmalı, bulunan: %q", kimlik)
			}
			ad, yol, ok := strings.Cut(kimlik[0], ":")
			if !ok || ad != "notify" {
				t.Fatalf("LoadCredential %q — betik $CREDENTIALS_DIRECTORY/notify okuyor", kimlik[0])
			}
			if !strings.HasPrefix(yol, "/etc/panely/") {
				t.Errorf("anahtar dosyası %q — root'un dizininde (/etc/panely) olmalı", yol)
			}
			for _, dizin := range daemonDizinleri {
				if altinda(yol, dizin) {
					t.Errorf("anahtar dosyası %q, daemon'un yazabildiği %q altında — "+
						"ele geçirilen panelyd alarmları başka sohbete yönlendirebilir", yol, dizin)
				}
			}

			if rw := yonergeDegerleri(t, birim, "ReadWritePaths"); len(rw) > 0 {
				t.Errorf("ReadWritePaths=%v — göndericinin durum dizini dışında "+
					"yazması gereken bir yer yok", rw)
			}
		})
	}
}

// TestNotifyUnitsCanReadJournal, göndericinin journal okuma yetkisini
// taşıdığını doğrular.
//
// Bu yetki olmadan hata SESSİZ olurdu: journalctl izinsiz kullanıcıya
// hata vermiyor, yalnızca kendi satırlarını — yani HİÇBİR ALARM'ı —
// gösteriyor. Canlıda ölçüldü: grup varken 8 ALARM satırı, grup yokken
// 0. Gönderici "gönderilecek bir şey yok" diye mutlu mutlu çıkardı.
func TestNotifyUnitsCanReadJournal(t *testing.T) {
	for _, birim := range gondericiBirimleri {
		if !slices.Contains(yonergeDegerleri(t, birim, "SupplementaryGroups"), "systemd-journal") {
			t.Errorf("%s: SupplementaryGroups=systemd-journal yok — journal SESSİZCE "+
				"boş görünür, hiçbir alarm gönderilmez", birim)
		}
	}
}

// TestNotifyUnitsCanResolveNamesButNotReachLocalhost, K-107'nin
// dersini göndericiye uygular: yerel ağ engeli DNS çözücüsünü
// kapatmamalı, istisna da çözücüden geniş olmamalı.
func TestNotifyUnitsCanResolveNamesButNotReachLocalhost(t *testing.T) {
	for _, birim := range gondericiBirimleri {
		t.Run(birim, func(t *testing.T) {
			deny := yonergeDegerleri(t, birim, "IPAddressDeny")
			allow := yonergeDegerleri(t, birim, "IPAddressAllow")
			if !slices.Contains(deny, "localhost") {
				t.Fatalf("IPAddressDeny localhost'u engellemiyor (%q)", deny)
			}
			if !slices.Equal(allow, []string{"127.0.0.53"}) {
				t.Errorf("IPAddressAllow %q — yalnızca 127.0.0.53 olmalı: yoksa "+
					"api.telegram.org çözülemez, genişse yerel servisler açılır", allow)
			}
			aileler := yonergeDegerleri(t, birim, "RestrictAddressFamilies")
			for _, gerekli := range []string{"AF_INET", "AF_INET6"} {
				if !slices.Contains(aileler, gerekli) {
					t.Errorf("RestrictAddressFamilies %q içermiyor — api.telegram.org "+
						"hem IPv4 hem IPv6 çözülüyor (ölçüldü)", gerekli)
				}
			}
		})
	}
}

// TestOffsiteFailureIsNotified, uzak yedek başarısız olduğunda
// bildirimin TETİKLENDİĞİNİ doğrular.
//
// Uzak yedeğin başarısızlığı panelyd'de ALARM üretmiyor — ayrı bir
// birim ve panelyd onu göremiyor. K-098'den bu yana her kayıt "arıza
// kimseye bildirilmiyor" diyordu; K-107'nin ilk gerçek koşusu tam
// böyle düştü. Bağlantı iki dosya arasında: OnFailure satırı ve onun
// adlandırdığı şablon birim. Biri silinirse ya da ad kayarsa arıza
// yine sessiz kalır.
func TestOffsiteFailureIsNotified(t *testing.T) {
	onFailure := yonergeDegerleri(t, "panely-offsite.service", "OnFailure")
	if !slices.Contains(onFailure, "panely-notify-failure@%n.service") {
		t.Fatalf("panely-offsite.service OnFailure=%q — uzak yedek başarısız "+
			"olursa kimse haber almaz", onFailure)
	}
	if _, err := os.Stat(filepath.Join("..", "..", "deploy", "systemd",
		"panely-notify-failure@.service")); err != nil {
		t.Errorf("OnFailure'ın adlandırdığı şablon birim yok: %v", err)
	}
	exec := yonergeDegerleri(t, "panely-notify-failure@.service", "ExecStart")
	if !slices.Contains(exec, "hata") || !slices.Contains(exec, "%i") {
		t.Errorf("şablon birim `hata %%i` kipini çağırmıyor: %q", exec)
	}
}

// TestNotifyTimerDoesNotFloodJournal, dakikada bir koşan göndericinin
// journal'ı doldurmadığını doğrular.
//
// Ölçüldü: sınırsız bir birim her koşuda 2 satır ("Starting/Finished")
// bırakıyor — dakikada bir koşuda günde ~2.900 satır. LogLevelMax=notice
// başarılı koşuyu susturuyor ama BAŞARISIZ koşuyu susturmuyor (ölçüldü:
// 2 satır kaldı). Daha sıkı bir sınır (warning) başarısızlığın bir
// satırını da yutuyordu.
func TestNotifyTimerDoesNotFloodJournal(t *testing.T) {
	seviye := yonergeDegerleri(t, "panely-notify.service", "LogLevelMax")
	if !slices.Equal(seviye, []string{"notice"}) {
		t.Errorf("LogLevelMax=%q — notice olmalı: yoksa journal dakikada iki "+
			"satırla dolar, daha sıkısı başarısızlığı gizler", seviye)
	}
}
