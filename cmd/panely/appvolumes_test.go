package main

import (
	"io"
	"strings"
	"testing"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// mustVolumes, test icin hacim listesi kurar.
func mustVolumes(t *testing.T, raws ...string) []*panelyv1.AppVolume {
	t.Helper()
	out := make([]*panelyv1.AppVolume, 0, len(raws))
	for _, raw := range raws {
		v, err := parseVolumeFlag(raw)
		if err != nil {
			t.Fatalf("%q ayristirilamadi: %v", raw, err)
		}
		out = append(out, &panelyv1.AppVolume{
			Name: v.name, MountPath: v.mountPath, ReadOnly: v.readOnly,
		})
	}
	return out
}

// parseOne, tek bir -volume değerini ayrıştırır.
func parseOne(t *testing.T, raw string) volumeSpec {
	t.Helper()
	v, err := parseVolumeFlag(raw)
	if err != nil {
		t.Fatalf("%q ayrıştırılamadı: %v", raw, err)
	}
	return v
}

// TestParseVolumeFlagBasic, AD:/yol biçimini doğrular.
func TestParseVolumeFlagBasic(t *testing.T) {
	v := parseOne(t, "data:/var/lib/app")
	if v.name != "data" {
		t.Errorf("ad %q", v.name)
	}
	if v.mountPath != "/var/lib/app" {
		t.Errorf("bağlama noktası %q", v.mountPath)
	}
	if v.readOnly {
		t.Error("varsayılan salt-okunur çıktı — uygulama kendi diskine yazamaz")
	}
}

// TestParseVolumeFlagReadOnly, :ro sonekini doğrular.
func TestParseVolumeFlagReadOnly(t *testing.T) {
	v := parseOne(t, "config:/etc/app:ro")
	if !v.readOnly {
		t.Error(":ro taşınmadı — salt-okunur istenen hacim yazılabilir bağlanır")
	}
	if v.mountPath != "/etc/app" {
		t.Errorf("bağlama noktası %q — sonek yola sızmış olabilir", v.mountPath)
	}
}

// TestParseVolumeFlagExplicitRW, :rw sonekini doğrular.
//
// Varsayılan zaten yazılabilir, ama açıkça yazabilmek niyeti belgeler.
// Reddetmek, kullanıcıyı "neden ro çalışıyor da rw çalışmıyor" sorusuna
// iterdi.
func TestParseVolumeFlagExplicitRW(t *testing.T) {
	v := parseOne(t, "data:/var/lib/app:rw")
	if v.readOnly {
		t.Error(":rw salt-okunur yaptı")
	}
}

// TestParseVolumeFlagRejectsBadForms, kabul edilemeyecek biçimleri
// doğrular.
//
// ⚠ "yalnızca yol" biçimi (`/var/lib/app`) ÖZELLİKLE reddedilmeli:
// Docker'ın `-v /host:/konteyner` sözdizimine alışkın biri onu yazar ve
// panely'de o HOST YOLU anlamına gelirdi — oysa panely host yolu KABUL
// ETMİYOR. Sessizce ad sanmak, kullanıcının host dizinini bağladığını
// sanmasına yol açardı.
func TestParseVolumeFlagRejectsBadForms(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"yol yok", "data"},
		{"boş ad", ":/var/lib/app"},
		{"boş yol", "data:"},
		{"bilinmeyen sonek", "data:/var/lib/app:rx"},
		{"fazla parça", "data:/var:/lib:ro:extra"},
		{"tamamen boş", ""},
		{"host yolu gibi", "/var/lib/host:/konteyner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseVolumeFlag(tc.raw); err == nil {
				t.Errorf("geçersiz biçim %q kabul edildi", tc.raw)
			}
		})
	}
}

// TestVolumeFlagCollectsRepeats, tekrarlanabilirliği doğrular.
func TestVolumeFlagCollectsRepeats(t *testing.T) {
	c := &cli{}
	fs := c.newFlagSet("test")
	vols := c.volumeFlag(fs, "volume", "hacim")

	if err := fs.Parse([]string{
		"-volume", "data:/var/lib/app",
		"-volume", "config:/etc/app:ro",
	}); err != nil {
		t.Fatalf("ayrıştırma başarısız: %v", err)
	}
	if len(*vols) != 2 {
		t.Fatalf("%d hacim toplandı, 2 bekleniyordu", len(*vols))
	}
	if (*vols)[1].GetName() != "config" || !(*vols)[1].GetReadOnly() {
		t.Errorf("ikinci hacim yanlış: %+v", (*vols)[1])
	}
}

// TestVolumeFlagRejectsDuplicateNames, aynı adın iki kez verilmesini
// doğrular.
//
// Sunucu da reddediyor ama istemcide yakalamak, kullanıcıyı ağ gidiş
// dönüşü beklemeden uyarır — ve `stringMapFlag` ile aynı kural.
func TestVolumeFlagRejectsDuplicateNames(t *testing.T) {
	c := &cli{}
	fs := c.newFlagSet("test")
	fs.SetOutput(io.Discard)
	c.volumeFlag(fs, "volume", "hacim")

	err := fs.Parse([]string{
		"-volume", "data:/bir",
		"-volume", "data:/iki",
	})
	if err == nil {
		t.Fatal("aynı hacim adı iki kez kabul edildi")
	}
	if !strings.Contains(err.Error(), "data") {
		t.Errorf("hata hangi adı anlatmıyor: %v", err)
	}
}

// TestBuildUpdateRequestCarriesVolumes, bayrağın isteğe girdiğini
// doğrular.
func TestBuildUpdateRequestCarriesVolumes(t *testing.T) {
	v := appUpdateFlags{}
	v.volumes = mustVolumes(t, "data:/var/lib/app")

	req := buildUpdateRequest("blog", v, map[string]bool{"volume": true})

	if len(req.GetVolumes()) != 1 || req.GetVolumes()[0].GetName() != "data" {
		t.Errorf("verilen -volume isteğe girmedi: %+v", req.GetVolumes())
	}
}

// TestBuildUpdateRequestLeavesVolumesUnsetWhenNotGiven, KONTROL
// GRUBUdur.
//
// Bayrak verilmediği hâlde isteğe giren boş liste, sunucu tarafında
// "hacim değişti" uyarısını tetikler ve kullanıcı hiç dokunmadığı bir
// şey için "yeniden dağıtın" uyarısı alır.
func TestBuildUpdateRequestLeavesVolumesUnsetWhenNotGiven(t *testing.T) {
	req := buildUpdateRequest("blog", appUpdateFlags{branch: "develop"},
		map[string]bool{"branch": true})

	if len(req.GetVolumes()) != 0 {
		t.Errorf("verilmeyen -volume isteğe girdi: %+v", req.GetVolumes())
	}
	if len(req.GetVolumeRemove()) != 0 {
		t.Errorf("verilmeyen -volume-rm isteğe girdi: %+v", req.GetVolumeRemove())
	}
}

// TestVolumeOnlyUpdateIsNotEmptyCLI, yalnızca hacim taşıyan komutun
// istemcide reddedilmediğini doğrular.
func TestVolumeOnlyUpdateIsNotEmptyCLI(t *testing.T) {
	v := appUpdateFlags{}
	v.volumes = mustVolumes(t, "data:/var/lib/app")
	req := buildUpdateRequest("blog", v, map[string]bool{"volume": true})
	if isEmptyUpdate(req) {
		t.Error("yalnızca -volume taşıyan güncelleme boş sayıldı")
	}

	rmReq := buildUpdateRequest("blog",
		appUpdateFlags{volumeRemove: []string{"data"}},
		map[string]bool{"volume-rm": true})
	if isEmptyUpdate(rmReq) {
		t.Error("yalnızca -volume-rm taşıyan güncelleme boş sayıldı")
	}
}
