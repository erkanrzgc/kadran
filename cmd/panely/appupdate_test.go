package main

import (
	"errors"
	"strings"
	"testing"
)

// TestBuildUpdateRequestKeepsUnsetFieldsUnset, bu komuttaki ASIL tuzağı
// sınar.
//
// Go'nun flag paketi "-domain hiç verilmedi" ile `-domain=""` durumlarını
// aynı boş dizeye indirger. Ayrımı yalnızca fs.Visit taşıyor; kaybolursa
// dala dokunmak isteyen bir kullanıcı, uygulamanın alan adını da silmiş
// olur ve site düşer. Sessiz, geri alınması dağıtım gerektiren bir hata.
func TestBuildUpdateRequestKeepsUnsetFieldsUnset(t *testing.T) {
	v := appUpdateFlags{domain: "", branch: "develop", health: "", replicas: 0}

	req := buildUpdateRequest("blog", v, map[string]bool{"branch": true})

	if req.Domain != nil {
		t.Errorf("verilmeyen -domain isteğe girdi (%q) — alan adını SİLERDİ", req.GetDomain())
	}
	if req.HealthPath != nil {
		t.Errorf("verilmeyen -health-path isteğe girdi (%q)", req.GetHealthPath())
	}
	if req.Replicas != nil {
		t.Errorf("verilmeyen -replicas isteğe girdi (%d) — sunucu 0'ı reddederdi", req.GetReplicas())
	}
	if req.GitBranch == nil || req.GetGitBranch() != "develop" {
		t.Errorf("verilen -branch isteğe girmedi: %v", req.GitBranch)
	}
	if req.GetAppId() != "blog" {
		t.Errorf("app_id = %q", req.GetAppId())
	}
}

// TestBuildUpdateRequestCarriesExplicitEmptyValues, üstteki testin ikiz
// yarısı. Tek başına o test, HİÇBİR ŞEYİ isteğe koymayan bir uygulamayı
// da geçirirdi.
func TestBuildUpdateRequestCarriesExplicitEmptyValues(t *testing.T) {
	v := appUpdateFlags{domain: "", health: ""}

	req := buildUpdateRequest("blog", v, map[string]bool{"domain": true, "health-path": true})

	if req.Domain == nil {
		t.Fatal("açıkça verilen -domain=\"\" isteğe girmedi — vekilden çıkarma imkânsız olurdu")
	}
	if req.GetDomain() != "" {
		t.Errorf("alan adı = %q, boş olmalıydı", req.GetDomain())
	}
	if req.HealthPath == nil {
		t.Fatal("açıkça verilen -health-path=\"\" isteğe girmedi")
	}
	if req.GetHealthPath() != "" {
		t.Errorf("sağlık yolu = %q, boş olmalıydı", req.GetHealthPath())
	}
}

func TestIsEmptyUpdateDetectsNoFields(t *testing.T) {
	empty := buildUpdateRequest("blog", appUpdateFlags{}, map[string]bool{})
	if !isEmptyUpdate(empty) {
		t.Error("hiç seçenek verilmemiş istek boş sayılmadı")
	}
	// Açıkça boşaltma İSTEĞİ boş bir güncelleme DEĞİLDİR.
	clearing := buildUpdateRequest("blog", appUpdateFlags{}, map[string]bool{"domain": true})
	if isEmptyUpdate(clearing) {
		t.Error("-domain=\"\" boş güncelleme sayıldı — vekilden çıkarma reddedilirdi")
	}
}

// TestUpdateFailureDoesNotContradictTheServerMessage, KULLANICININ
// GÖRDÜĞÜ satırı kuruyor.
//
// Sunucu tarafındaki test hatanın "KAYDEDİLDİ" içerdiğini doğruluyordu ve
// YEŞİLDİ — ama CLI o hatayı "uygulama güncellenemedi: %w" ile
// sarmalıyordu, yani terminalde şu görünüyordu:
//
//	panely: uygulama güncellenemedi: ... KAYDEDİLDİ, ama ...
//
// Test doğru şeyi kontrol ediyordu, yanlış KATMANDA. Bu, aynı dilimde
// yakalanan "dönen struct'a bakan test diski ölçmez" kusurunun kardeşi.
func TestUpdateFailureDoesNotContradictTheServerMessage(t *testing.T) {
	c, _, errOut := newTestCLI("")

	// Sunucunun ürettiği gerçek mesajın şekli.
	serverErr := errors.New(
		`alan adı "eski.example.com" → "yeni.example.com" olarak KAYDEDİLDİ, ` +
			`ama ters vekil güncellenemedi: trafik hâlâ eski rotada`)

	if code := c.failUpdate(serverErr); code != exitError {
		t.Fatalf("çıkış kodu = %d, beklenen %d", code, exitError)
	}

	line := errOut.String()
	if !strings.Contains(line, "KAYDEDİLDİ") {
		t.Fatalf("sunucunun mesajı kayboldu: %q", line)
	}
	for _, contradiction := range []string{"güncellenemedi:", "başarısız"} {
		// Not: sunucunun kendi metnindeki "ters vekil güncellenemedi"
		// ifadesi ÖN EK DEĞİL ve doğru — aranan şey iddianın mesajdan
		// ÖNCE gelmesi.
		if idx := strings.Index(line, contradiction); idx != -1 &&
			idx < strings.Index(line, "KAYDEDİLDİ") {
			t.Errorf("kullanıcı önce %q okuyor, sonra KAYDEDİLDİ — çelişki: %q",
				contradiction, line)
		}
	}
}

// TestAppUpdateRejectsMissingName, alt komutun ad olmadan çağrılmasının
// yakalandığını sınar.
func TestAppUpdateRejectsMissingName(t *testing.T) {
	c, _, out := newTestCLI("")
	if code := c.runAppUpdate(t.Context(), []string{"-domain", "a.example.com"}); code != exitUsage {
		t.Fatalf("çıkış kodu = %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(out.String(), "kullanım") {
		t.Errorf("kullanım metni basılmadı: %q", out.String())
	}
}

// TestAppUpdateRejectsEmptyChangeSet, hiçbir alan verilmemiş çağrının
// SUNUCUYA HİÇ GİTMEDEN reddedildiğini sınar.
func TestAppUpdateRejectsEmptyChangeSet(t *testing.T) {
	c, _, out := newTestCLI("")
	if code := c.runAppUpdate(t.Context(), []string{"blog"}); code != exitUsage {
		t.Fatalf("çıkış kodu = %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(out.String(), "değiştirilecek bir alan verilmedi") {
		t.Errorf("sebep açıklanmadı: %q", out.String())
	}
}
