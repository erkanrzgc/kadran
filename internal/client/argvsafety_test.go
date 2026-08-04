package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// # Neden bu dosya var?
//
// `ssh` bir kabuk üzerinden çağrılmıyor, argümanlar exec'e dizi olarak
// veriliyor. Bu kabuk enjeksiyonunu kapatır ama BAŞKA bir sınıfı açık
// bırakır: argüman enjeksiyonu. `-` ile başlayan bir konumsal argüman
// ssh tarafından SEÇENEK olarak okunur ve `-oProxyCommand=...` keyfî
// yerel komut çalıştırır.
//
// Hedef dizesi yalnızca komut satırından gelmiyor: sidecar hedefleri
// GUI profillerinden alıyor. Yani "kullanıcı kendi ayağına sıkar"
// savunması burada geçerli değil.

// TestParseTargetRejectsOptionLikeUser, `-` ile başlayan kullanıcı adının
// çözümleme aşamasında reddedildiğini doğrular.
//
// `strings.Cut(s, "@")` ilk @'te böldüğü için `-oProxyCommand=x@sunucu`
// girdisi kullanıcı adını saldırganın denetimine verir; birleştirilmiş
// argüman da `-` ile başlar.
func TestParseTargetRejectsOptionLikeUser(t *testing.T) {
	kotu := []string{
		"-oProxyCommand=touch /tmp/pwned@sunucu",
		"-E/tmp/gunluk@sunucu",
		"-oProxyCommand=x@[2001:db8::1]:2222",
	}

	for _, giris := range kotu {
		t.Run(giris, func(t *testing.T) {
			hedef, err := ParseTarget(giris)
			if err == nil {
				t.Fatalf("seçenek benzeri hedef kabul edildi: %+v "+
					"(ssh'a geçecek argüman: %q)",
					hedef, hedef.SSHUser+"@"+hedef.SSHHost)
			}
		})
	}
}

// TestParseTargetRejectsOptionLikeHost, kullanıcı adı olmadan verilen
// `-` başlangıçlı sunucu adını da reddettiğimizi doğrular.
//
// Varsayılan kullanıcı öne eklendiği için bu biçim tek başına argüman
// enjeksiyonu ÜRETMEZ, ama `--user` benzeri bir seçenek ileride eklenirse
// üretir. Girdiyi kaynağında reddetmek, sınıfı gelecekte de kapalı tutar.
func TestParseTargetRejectsOptionLikeHost(t *testing.T) {
	if hedef, err := ParseTarget("kullanici@-sunucu"); err == nil {
		t.Fatalf("`-` ile başlayan sunucu adı kabul edildi: %+v", hedef)
	}
}

// TestDialSSHRefusesOptionLikeTarget, çözümlemeyi ATLAYAN bir yolun da
// korunduğunu doğrular.
//
// ParseTarget tek savunma olsaydı, doğrudan kurulan bir Target (örneğin
// diskteki bir profilden okunan alanlar) korumayı baypas ederdi.
// Doğrulama argv'nin kurulduğu yerde de duruyor.
func TestDialSSHRefusesOptionLikeTarget(t *testing.T) {
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}

	argvDosya := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHOutEnv, filepath.Join(t.TempDir(), "baytlar.bin"))
	t.Setenv(fakeSSHArgvEnv, argvDosya)

	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })

	_, err := dialSSH(context.Background(), Target{
		SSHUser: "-oProxyCommand=touch /tmp/pwned",
		SSHHost: "sunucu",
	})
	if err == nil {
		t.Fatal("seçenek benzeri hedefle ssh başlatıldı")
	}

	// Asıl kanıt: süreç HİÇ çalıştırılmamış olmalı. Hata döndürüp yine de
	// exec etmek açığı kapatmaz.
	//
	// Burada beklemek ŞART: alt süreç argv'yi eşzamansız yazıyor.
	// "Dosya yok ⇒ exec edilmedi" demek, henüz yazmamış bir sürecin
	// olduğu durumda BOŞ YERE geçerdi. Aşağıdaki pozitif kontrol aynı
	// bekleme süresiyle dosyayı görüyor; yani ölçüm çalışıyor.
	if argv, err := waitForFile(argvDosya, 1, argvBekleme); err == nil {
		t.Fatalf("ssh yine de çalıştırıldı, argv:\n%s", argv)
	}
}

// argvBekleme, alt sürecin argv'yi yazması için tanınan süre.
//
// Negatif ve pozitif testte AYNI değer kullanılıyor: pozitif kontrol bu
// sürede dosyayı görüyorsa, negatif testte görülmemesi "henüz yazmadı"
// değil "hiç çalıştırılmadı" demektir.
const argvBekleme = 3 * time.Second

// TestDialSSHPassesHostAsSinglePositionalArg, meşru bir hedefte argv'nin
// beklendiği gibi kurulduğunu doğrular.
//
// Bu satır olmadan yukarıdaki testler, dialSSH'ın her şeyi reddettiği
// bozuk bir durumda da geçerdi.
func TestDialSSHPassesHostAsSinglePositionalArg(t *testing.T) {
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}

	argvDosya := filepath.Join(t.TempDir(), "argv.txt")
	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHOutEnv, filepath.Join(t.TempDir(), "baytlar.bin"))
	t.Setenv(fakeSSHArgvEnv, argvDosya)

	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })

	conn, err := dialSSH(context.Background(), Target{
		SSHUser: "panely-client",
		SSHHost: "sunucu",
	})
	if err != nil {
		t.Fatalf("meşru hedef reddedildi: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ham, err := waitForFile(argvDosya, 1, argvBekleme)
	if err != nil {
		t.Fatalf("argv kaydedilmedi (%v) — ölçüm düzeneği çalışmıyor, "+
			"bu durumda negatif test de hiçbir şey kanıtlamaz", err)
	}
	argv := strings.Split(strings.TrimSpace(string(ham)), "\n")

	son := argv[len(argv)-1]
	if son != "panely-client@sunucu" {
		t.Errorf("son argüman %q, beklenen \"panely-client@sunucu\"", son)
	}
	if strings.HasPrefix(son, "-") {
		t.Errorf("konumsal argüman `-` ile başlıyor: %q", son)
	}
}

// TestDefaultTargetsSurviveOptionCheck, doğrulamanın BİRİNCİL kullanım
// yollarını kapatmadığını sabitler.
//
// # Neden ayrı bir test?
//
// Yeni bir girdi reddi eklerken asıl risk, yasakladığın şeyin meşru
// varsayılanı da kapsaması. Bu projede aynı şekil bir kez yaşandı:
// yerel bağlantı yolu kimlik önsözünü yazmıyordu ve argümansız
// `panely status` — birincil kullanım — ölüyordu (K-012).
//
// Yukarıdaki pozitif kontrol "panely-client" dizgisini ELLE yazıyor;
// yani DefaultSSHUser değişse haberi olmazdı. Burada sabitlerin
// kendileri sınanıyor.
func TestDefaultTargetsSurviveOptionCheck(t *testing.T) {
	// Argümansız çağrı: yerel sokete düşmeli, SSH doğrulamasına hiç
	// uğramamalı.
	yerel, err := ParseTarget("")
	if err != nil {
		t.Fatalf("boş hedef reddedildi — argümansız `panely status` kırık: %v", err)
	}
	if !yerel.IsLocal() || yerel.SocketPath != DefaultSocketPath {
		t.Errorf("boş hedef %+v, beklenen yerel soket %q", yerel, DefaultSocketPath)
	}

	// Çıplak sunucu adı: varsayılan kullanıcı öne eklenmeli ve
	// birleşen argüman `-` ile başlamamalı.
	uzak, err := ParseTarget("sunucu")
	if err != nil {
		t.Fatalf("çıplak sunucu adı reddedildi: %v", err)
	}
	if uzak.SSHUser != DefaultSSHUser {
		t.Errorf("kullanıcı %q, beklenen %q", uzak.SSHUser, DefaultSSHUser)
	}
	if strings.HasPrefix(DefaultSSHUser, "-") {
		t.Fatalf("DefaultSSHUser `-` ile başlıyor (%q) — kendi doğrulamamız "+
			"varsayılan yolu kapatır", DefaultSSHUser)
	}

	// Tam biçim de geçmeli.
	if _, err := ParseTarget(DefaultSSHUser + "@sunucu:2222"); err != nil {
		t.Errorf("varsayılan kullanıcıyla açık hedef reddedildi: %v", err)
	}
}
