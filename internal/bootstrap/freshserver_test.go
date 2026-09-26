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

func kurulumBetigi(t *testing.T) string {
	t.Helper()
	b, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
