package main

import (
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
