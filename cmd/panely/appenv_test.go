package main

import (
	"io"
	"strings"
	"testing"
)

// TestBuildUpdateRequestCarriesEnv, -env'in isteğe girdiğini doğrular.
func TestBuildUpdateRequestCarriesEnv(t *testing.T) {
	v := appUpdateFlags{env: map[string]string{"DATABASE_URL": "postgres://db/blog"}}

	req := buildUpdateRequest("blog", v, map[string]bool{"env": true})

	if req.GetEnv()["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("verilen -env isteğe girmedi: %+v", req.GetEnv())
	}
}

// TestBuildUpdateRequestCarriesEnvRemove, -env-rm'in isteğe girdiğini
// doğrular.
func TestBuildUpdateRequestCarriesEnvRemove(t *testing.T) {
	v := appUpdateFlags{envRemove: []string{"ESKI"}}

	req := buildUpdateRequest("blog", v, map[string]bool{"env-rm": true})

	if len(req.GetEnvRemove()) != 1 || req.GetEnvRemove()[0] != "ESKI" {
		t.Errorf("verilen -env-rm isteğe girmedi: %+v", req.GetEnvRemove())
	}
}

// TestBuildUpdateRequestLeavesEnvUnsetWhenNotGiven, KONTROL GRUBUdur.
//
// ⚠ Burada kaybedilecek şey diğer alanlardakinden farklı. Boş bir harita
// göndermek zararsız görünür (sunucu birleştirme yapıyor, boş harita
// hiçbir şeyi değiştirmez) — ama `-env` verilmediği hâlde isteğe giren
// boş harita, sunucu tarafında "env değişti" uyarısını tetikler ve
// kullanıcı hiç dokunmadığı bir şey için "yeniden dağıtın" uyarısı alır.
func TestBuildUpdateRequestLeavesEnvUnsetWhenNotGiven(t *testing.T) {
	v := appUpdateFlags{branch: "develop"}

	req := buildUpdateRequest("blog", v, map[string]bool{"branch": true})

	if len(req.GetEnv()) != 0 {
		t.Errorf("verilmeyen -env isteğe girdi: %+v", req.GetEnv())
	}
	if len(req.GetEnvRemove()) != 0 {
		t.Errorf("verilmeyen -env-rm isteğe girdi: %+v", req.GetEnvRemove())
	}
}

// TestEnvOnlyUpdateIsNotEmpty, yalnızca env taşıyan komutun
// reddedilmediğini doğrular.
//
// isEmptyUpdate'e env eklenmezse `panely app update -env X=1 blog`
// istemci tarafında "değiştirilecek bir alan verilmedi" ile durur ve
// sunucuya HİÇ gitmez. Mekanizma bağlanır, komut çalışmaz.
func TestEnvOnlyUpdateIsNotEmpty(t *testing.T) {
	req := buildUpdateRequest("blog",
		appUpdateFlags{env: map[string]string{"A": "1"}},
		map[string]bool{"env": true})
	if isEmptyUpdate(req) {
		t.Error("yalnızca -env taşıyan güncelleme boş sayıldı")
	}

	rmReq := buildUpdateRequest("blog",
		appUpdateFlags{envRemove: []string{"A"}},
		map[string]bool{"env-rm": true})
	if isEmptyUpdate(rmReq) {
		t.Error("yalnızca -env-rm taşıyan güncelleme boş sayıldı")
	}
}

// TestStringSliceFlagCollectsRepeats, tekrarlanabilir bayrağı doğrular.
func TestStringSliceFlagCollectsRepeats(t *testing.T) {
	c := &cli{}
	fs := c.newFlagSet("test")
	keys := c.stringSliceFlag(fs, "env-rm", "silinecek anahtar")

	if err := fs.Parse([]string{"-env-rm", "BIR", "-env-rm", "IKI"}); err != nil {
		t.Fatalf("ayrıştırma başarısız: %v", err)
	}
	if len(*keys) != 2 || (*keys)[0] != "BIR" || (*keys)[1] != "IKI" {
		t.Errorf("tekrarlanan bayrak toplanmadı: %+v", *keys)
	}
}

// TestStringSliceFlagRejectsDuplicates, aynı anahtarın iki kez
// verilmesini doğrular.
//
// stringMapFlag'in aynı kuralı var ve gerekçesi aynı: sessizce yutmak,
// kullanıcının yazdığı bir şeyin yok sayıldığını gizler.
func TestStringSliceFlagRejectsDuplicates(t *testing.T) {
	c := &cli{}
	fs := c.newFlagSet("test")
	// Kullanım hatasını stderr'e basmasın; test çıktısını kirletir.
	fs.SetOutput(io.Discard)
	c.stringSliceFlag(fs, "env-rm", "silinecek anahtar")

	err := fs.Parse([]string{"-env-rm", "BIR", "-env-rm", "BIR"})
	if err == nil {
		t.Fatal("aynı anahtar iki kez verildiği hâlde kabul edildi")
	}
	if !strings.Contains(err.Error(), "BIR") {
		t.Errorf("hata hangi anahtarı anlatmıyor: %v", err)
	}
}

// TestStringSliceFlagRejectsEmpty, boş anahtarı doğrular.
func TestStringSliceFlagRejectsEmpty(t *testing.T) {
	c := &cli{}
	fs := c.newFlagSet("test")
	fs.SetOutput(io.Discard)
	c.stringSliceFlag(fs, "env-rm", "silinecek anahtar")

	if err := fs.Parse([]string{"-env-rm", ""}); err == nil {
		t.Error("boş anahtar kabul edildi")
	}
}
