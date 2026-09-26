package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Bu dosyadaki testler v0.1 öncesi TAZE sunucu testinde (26 Eyl, K-112)
// bulunan iki kusuru kilitliyor. İkisi de yalnızca gerçek bir kurulumda,
// yavaş bir bağlantıyla ve Docker'sız bir sunucuda görünebilirdi.

// TestInstallerTimeoutSaysSo, süre sınırı dolduğunda hatanın bunu
// SÖYLEDİĞİNİ doğrular.
//
// Ölçüldü: bir koşuda 75 MiB'lık paketin yalnızca 28 MB'ı 5 dakikada
// gitti ve 10 dakikalık varsayılan sınır doldu. Süre dolunca ssh
// öldürüldü ve Windows'ta öldürülen süreç "exit status 1" döndürdüğü için
// kullanıcıya kalan tek satır "kurulum başarısız: exit status 1" oldu —
// sebep hiçbir yerde yoktu.
func TestInstallerTimeoutSaysSo(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	err := kurulumHatasi(ctx, errors.New("exit status 1"), 75<<20)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("hata süre aşımını taşımıyor: %v", err)
	}
	for _, parca := range []string{"süre sınırı", "-timeout", "75.0 MiB"} {
		if !strings.Contains(err.Error(), parca) {
			t.Errorf("hata %q içermiyor: %v", parca, err)
		}
	}
}

// TestInstallerFailureIsNotMistakenForTimeout, kontrol grubu: süre
// DOLMADAN düşen bir kurulum zaman aşımı diye raporlanmamalı. Aksi
// hâlde gerçek hata (ör. eksik Docker) "daha uzun bekle" tavsiyesinin
// arkasına saklanırdı.
func TestInstallerFailureIsNotMistakenForTimeout(t *testing.T) {
	err := kurulumHatasi(context.Background(), errors.New("exit status 1"), 75<<20)
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "süre sınırı") {
		t.Errorf("süresi dolmamış bir hata zaman aşımı sanıldı: %v", err)
	}
	if !strings.Contains(err.Error(), "kurulum başarısız") {
		t.Errorf("özgün hata kayboldu: %v", err)
	}
}

// TestInstallScriptRequiresDockerUpFront, Docker'ın kurulumun BAŞINDA
// arandığını doğrular — kullanıcı ve grup oluşturulmadan, birimler
// kurulmadan önce.
//
// Taze sunucu testinde betik Docker'a hiç bakmıyordu. README Docker'ı
// ön koşul sayıyor; betik bunu doğrulamadan ilerleyip ancak sonunda,
// sebebi belirsiz bir kontrolle düşebilirdi.
func TestInstallScriptRequiresDockerUpFront(t *testing.T) {
	text := kurulumBetigi(t)

	onKosul := strings.Index(text, `step "Ön koşullar"`)
	kullanicilar := strings.Index(text, `step "Gruplar ve kullanıcılar"`)
	if onKosul < 0 || kullanicilar < 0 {
		t.Fatal("bölüm başlıkları bulunamadı — ölçüm geçersiz")
	}
	bolum := text[onKosul:kullanicilar]
	if !strings.Contains(bolum, "command -v docker") {
		t.Error("Docker'ın varlığı ön koşullarda aranmıyor")
	}
	if !strings.Contains(bolum, "docker version") {
		t.Error("Docker daemon'ının CEVAP verdiği ön koşullarda sınanmıyor — " +
			"kurulu ama durmuş bir Docker geçer")
	}
}

// TestInstallScriptProvesDockerIsolationWasMeasured, "panely Docker'a
// erişemiyor" kontrolünün ÖLÇEBİLDİĞİNİ kanıtladıktan sonra okunduğunu
// doğrular.
//
// Taze sunucu testinde bulundu: `setpriv … docker ps` Docker hiç yokken
// de başarısız oluyor (komut bulunamadı) ve kontrol "erişemiyor ✓" diye
// GEÇİYORDU. Cevapsızlığı güvenli okumak — bu projede üçüncü kez.
// Önce root'un ulaşabildiği gösterilmeli.
func TestInstallScriptProvesDockerIsolationWasMeasured(t *testing.T) {
	text := kurulumBetigi(t)

	negatif := strings.Index(text, "setpriv --reuid panely --regid panely --clear-groups docker ps")
	if negatif < 0 {
		t.Fatal("ayrıcalık ayrımı kontrolü bulunamadı — ölçüm geçersiz")
	}
	pozitif := strings.Index(text, "if ! docker ps >/dev/null 2>&1; then")
	if pozitif < 0 {
		t.Fatal("pozitif kontrol yok: root'un Docker'a ulaştığı sınanmıyor, " +
			"dolayısıyla 'panely erişemiyor' Docker yokken de geçer")
	}
	if pozitif > negatif {
		t.Error("pozitif kontrol negatiften SONRA geliyor")
	}
}

// servislerBolumu, kurulum betiğinin "Servisler" adımını döndürür.
func servislerBolumu(t *testing.T) string {
	t.Helper()
	text := kurulumBetigi(t)
	bas := strings.Index(text, `step "Servisler"`)
	son := strings.Index(text, `step "Kurulum sonrası doğrulama"`)
	if bas < 0 || son < 0 || son < bas {
		t.Fatal("Servisler bölümü bulunamadı — ölçüm geçersiz")
	}
	return text[bas:son]
}

// TestInstallerRestartsTheControlPlane, yeniden kurulumun (yükseltme
// yolu) panelyd ve panely-exec'i YENİ ikiliyle çalıştırdığını doğrular.
//
// Taze sunucu testinde ölçüldü: betik `systemctl enable --now`
// kullanıyordu; bu, ÇALIŞAN birimi yeniden başlatmıyor. İkinci kurulumdan
// sonra /proc/<pid>/exe → "/usr/local/lib/panely/panelyd (deleted)":
// süreç diskten silinmiş ESKİ ikiliyi çalıştırıyordu ve kurulum
// "tamamlandı" diyordu. Yükseltmede yeni kod hiç çalışmazdı.
func TestInstallerRestartsTheControlPlane(t *testing.T) {
	bolum := servislerBolumu(t)
	for _, birim := range []string{"panely-exec.service", "panelyd.service"} {
		if !strings.Contains(bolum, "systemctl restart "+birim) {
			t.Errorf("%s yeniden başlatılmıyor — yükseltmede eski ikili çalışmaya devam eder", birim)
		}
		if strings.Contains(bolum, "systemctl enable --now "+birim) {
			t.Errorf("%s hâlâ yalnızca `enable --now` ile başlatılıyor — çalışan birimi yeniden başlatmaz", birim)
		}
	}
	if strings.Index(bolum, "systemctl restart panely-exec.service") > strings.Index(bolum, "systemctl restart panelyd.service") {
		t.Error("panelyd executor'dan ÖNCE yeniden başlatılıyor — daemon açılışta executor'a bağlanıyor")
	}
}

// TestInstallScriptVerifiesEveryRunningBinary, kurulum sonrası doğrulamanın
// kontrol düzlemi için de çalışan imajı kurulan ikiliyle karşılaştırdığını
// doğrular. Önceden yalnızca ters vekil için vardı (K-049, o kontrol
// TestInstallScriptVerifiesTheRunningProxyImage'da); panelyd ve
// executor'daki kusuru bu yüzden hiçbir kontrol görmedi.
func TestInstallScriptVerifiesEveryRunningBinary(t *testing.T) {
	text := kurulumBetigi(t)
	for _, ikili := range []string{"panelyd", "panely-exec"} {
		if !strings.Contains(text, "calisan_ikili_dogrula "+ikili) {
			t.Errorf("çalışan %s'nin kurulan ikili olduğu doğrulanmıyor", ikili)
		}
	}
}

// TestInstallerLeavesAnUnchangedProxyRunning, ters vekilin yalnızca bir
// şey DEĞİŞTİYSE yeniden başlatıldığını doğrular.
//
// Taze sunucu testinde ölçüldü: ikinci kurulumda (hiçbir şey
// değişmemişken) Caddy koşulsuz yeniden başlatıldı, rotasız açıldı ve
// site panelyd yeniden başlayana kadar KAPALI kaldı. Rotaları geri
// getirmek panelyd'nin işi (K-055); ama ters vekil trafiğin yolu ve
// gereksiz yeniden başlatma yine de kesinti demek.
func TestInstallerLeavesAnUnchangedProxyRunning(t *testing.T) {
	text := kurulumBetigi(t)
	if !strings.Contains(text, "vekil_parmak_izi") {
		t.Fatal("ters vekilin yapılandırma/birim parmak izi alınmıyor — değişiklik ayırt edilemez")
	}
	onceki := strings.Index(text, `vekil_once="$(vekil_parmak_izi)"`)
	kurulum := strings.Index(text, `install -m 0644 -o root -g root "$STAGE/caddy.json" /etc/panely/caddy.json`)
	if onceki < 0 || kurulum < 0 || onceki > kurulum {
		t.Error("parmak izi dosyalar kurulMADAN önce alınmıyor — önce/sonra karşılaştırması anlamsız")
	}
	if !strings.Contains(text, "ters vekil değişmedi") {
		t.Error("değişmeyen ters vekili yeniden başlatmadan bırakan dal yok")
	}
}

func kurulumBetigi(t *testing.T) string {
	t.Helper()
	b, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
