package api

import (
	"strings"
	"testing"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

func protoVolumes() []*panelyv1.AppVolume {
	return []*panelyv1.AppVolume{
		{Name: "data", MountPath: "/var/lib/app"},
		{Name: "sablonlar", MountPath: "/etc/app/templates", ReadOnly: true},
	}
}

// TestAppVolumesRoundTripThroughProto, hacimlerin proto ↔ depo
// çevrimlerinde kaybolmadığını doğrular.
//
// Çevrimler elle yazılmış alan listeleri; eksik alan derleme hatası
// vermez, Go'nun sıfır değerine düşer.
func TestAppVolumesRoundTripThroughProto(t *testing.T) {
	spec := &panelyv1.AppSpec{AppId: "blog", Volumes: protoVolumes()}

	app := appFromProto(spec)
	if len(app.Volumes) != 2 {
		t.Fatalf("proto → depo çevriminde hacimler kayboldu: %+v", app.Volumes)
	}
	// ⚠ read_only TAŞINMALI: kaybolursa salt-okunur bir hacim
	// yazılabilir bağlanır ve kimse fark etmez.
	var ro bool
	for _, v := range app.Volumes {
		if v.Name == "sablonlar" {
			ro = v.ReadOnly
		}
	}
	if !ro {
		t.Error("read_only proto → depo çevriminde kayboldu")
	}

	back := appToProto(app)
	if len(back.GetSpec().GetVolumes()) != 2 {
		t.Fatalf("depo → proto çevriminde hacimler kayboldu: %+v",
			back.GetSpec().GetVolumes())
	}
	for _, v := range back.GetSpec().GetVolumes() {
		if v.GetName() == "sablonlar" && !v.GetReadOnly() {
			t.Error("read_only depo → proto çevriminde kayboldu")
		}
	}
}

// TestValidateVolumesRejectsBadNames, hacim adı kısıtını doğrular.
//
// Ad, hacmin DİSKTEKİ dizin adıdır. Executor'ınkinden gevşek olmak
// `app create`'te kabul edilip `deploy`'da patlayan bir tanım üretirdi.
func TestValidateVolumesRejectsBadNames(t *testing.T) {
	cases := []struct{ name, vol string }{
		{"büyük harf", "Data"},
		{"alt çizgi", "my_data"},
		{"tire ile başlıyor", "-data"},
		{"boş", ""},
		{"nokta nokta", ".."},
		{"eğik çizgi", "a/b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVolumes([]*panelyv1.AppVolume{
				{Name: tc.vol, MountPath: "/data"},
			})
			if err == nil {
				t.Errorf("geçersiz hacim adı %q kabul edildi", tc.vol)
			}
		})
	}
}

// TestValidateVolumesRejectsBadMountPaths, bağlama noktası kısıtlarını
// doğrular.
//
// ⚠ `/` üzerine bağlamak konteynerin KÖK DOSYA SİSTEMİNİ gizlerdi;
// `/proc`, `/sys`, `/dev` altına bağlamak çekirdek arayüzlerini
// gölgelerdi. İkisi de "uygulama açılmıyor" olarak değil, çok daha
// tuhaf belirtilerle ortaya çıkar.
func TestValidateVolumesRejectsBadMountPaths(t *testing.T) {
	cases := []struct{ name, path string }{
		{"göreli", "veri"},
		{"kök", "/"},
		{"temiz değil", "/var/../etc"},
		{"sondaki eğik çizgi", "/var/lib/app/"},
		{"proc altı", "/proc/self"},
		{"sys altı", "/sys/kernel"},
		{"dev altı", "/dev/shm"},
		{"boş", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateVolumes([]*panelyv1.AppVolume{
				{Name: "data", MountPath: tc.path},
			})
			if err == nil {
				t.Errorf("geçersiz bağlama noktası %q kabul edildi", tc.path)
			}
		})
	}
}

// TestValidateVolumesRejectsOverlap, iç içe geçen bağlama noktalarını
// doğrular.
//
// Hangi hacmin görüneceği bağlama SIRASINA kalırdı — güvenlik sınırında
// belirsiz davranış kabul edilemez.
func TestValidateVolumesRejectsOverlap(t *testing.T) {
	err := validateVolumes([]*panelyv1.AppVolume{
		{Name: "dis", MountPath: "/var/lib/app"},
		{Name: "ic", MountPath: "/var/lib/app/data"},
	})
	if err == nil {
		t.Fatal("iç içe bağlama noktaları kabul edildi")
	}
	if !strings.Contains(err.Error(), "/var/lib/app") {
		t.Errorf("hata hangi yolları anlatmıyor: %v", err)
	}
}

// TestValidateVolumesRejectsDuplicateNames, aynı adın iki kez
// kullanılmasını doğrular.
//
// İkisi de diskte AYNI dizini gösterirdi; hangisinin bağlama noktasının
// geçerli olduğu belirsiz kalırdı.
func TestValidateVolumesRejectsDuplicateNames(t *testing.T) {
	err := validateVolumes([]*panelyv1.AppVolume{
		{Name: "data", MountPath: "/bir"},
		{Name: "data", MountPath: "/iki"},
	})
	if err == nil {
		t.Fatal("aynı hacim adı iki kez kabul edildi")
	}
}

// TestValidateVolumesAcceptsRealistic, KONTROL GRUBUdur.
func TestValidateVolumesAcceptsRealistic(t *testing.T) {
	err := validateVolumes([]*panelyv1.AppVolume{
		{Name: "data", MountPath: "/var/lib/app"},
		{Name: "uploads", MountPath: "/srv/uploads"},
		{Name: "config-ro", MountPath: "/etc/app", ReadOnly: true},
	})
	if err != nil {
		t.Errorf("geçerli hacimler reddedildi: %v", err)
	}
}

// TestValidateVolumesEnforcesCount, üst sınırı doğrular.
func TestValidateVolumesEnforcesCount(t *testing.T) {
	many := make([]*panelyv1.AppVolume, 0, maxVolumes+1)
	for i := range maxVolumes + 1 {
		many = append(many, &panelyv1.AppVolume{
			Name:      "v" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			MountPath: "/m" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
		})
	}
	if err := validateVolumes(many); err == nil {
		t.Error("hacim sayısı sınırı aşıldığı hâlde kabul edildi")
	}
}

// TestValidateVolumeRemoveRejectsContradiction, aynı hacmin hem
// eklenip hem ayrılmasını doğrular.
func TestValidateVolumeRemoveRejectsContradiction(t *testing.T) {
	err := validateVolumeRemove(
		[]*panelyv1.AppVolume{{Name: "data", MountPath: "/d"}},
		[]string{"data"},
	)
	if err == nil {
		t.Fatal("aynı hacim hem eklenip hem ayrıldığı hâlde kabul edildi")
	}
	if !strings.Contains(err.Error(), "data") {
		t.Errorf("hata hangi hacmi anlatmıyor: %v", err)
	}
}

// TestVolumeAuditParamsCarryNamesNotPaths, denetim kaydının ne
// taşıdığını doğrular.
//
// Hacim adı ve bağlama noktası sır DEĞİL — "hangi disk nereye bağlandı"
// sorusu denetlenebilir kalmalı. Ama host yolu hiçbir yerde geçmemeli:
// kayıtta görünmesi, onun istekten geldiği izlenimini yaratırdı.
func TestVolumeAuditParamsCarryNamesNotPaths(t *testing.T) {
	spec := &panelyv1.AppSpec{AppId: "blog", Volumes: protoVolumes()}
	params := appAuditParams(spec)

	if params["volume.data"] == "" {
		t.Errorf("hacim denetime yazılmadı: %+v", params)
	}
	for k, v := range params {
		if strings.Contains(v, "/var/lib/panely/volumes") {
			t.Errorf("denetime HOST YOLU yazıldı (%s=%q) — yol istekten "+
				"gelmiyor, executor kuruyor", k, v)
		}
	}
}

// TestUpdateAppRejectsContradictoryVolumes, çelişkinin RPC seviyesinde
// reddedildiğini doğrular.
//
// validateVolumeRemove'un var olması yetmez — ÇAĞRILMASI gerekir.
func TestUpdateAppRejectsContradictoryVolumes(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	_, err := srv.UpdateApp(t.Context(), &panelyv1.UpdateAppRequest{
		AppId:        "blog",
		Volumes:      []*panelyv1.AppVolume{{Name: "data", MountPath: "/d"}},
		VolumeRemove: []string{"data"},
	})
	if err == nil {
		t.Fatal("aynı hacim hem eklenip hem ayrıldığı hâlde kabul edildi")
	}
}

// TestUpdateAppWarnsVolumesNeedRedeploy, komutun SESSİZCE başarılı
// demediğini doğrular.
//
// Bağlama konteyner oluşturulurken kuruluyor; çalışan bir konteynere
// sonradan disk eklenemez. Bunu söylemeyen bir cevap, kullanıcının
// diskin hazır olduğunu sanmasına yol açar.
func TestUpdateAppWarnsVolumesNeedRedeploy(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	resp := update(t, srv, &panelyv1.UpdateAppRequest{
		AppId:   "blog",
		Volumes: []*panelyv1.AppVolume{{Name: "data", MountPath: "/var/lib/app"}},
	})

	detail := resp.GetVolumeDetail()
	if detail == "" {
		t.Fatal("hacim değişti ama hiçbir uyarı döndürülmedi — kullanıcı " +
			"diskin hemen bağlandığını sanır")
	}
	if !strings.Contains(detail, "deploy") {
		t.Errorf("uyarı NE YAPILACAĞINI söylemiyor: %q", detail)
	}
}

// TestUpdateAppVolumesActuallyPersist, yazmanın GERÇEKTEN olduğunu
// doğrular.
//
// Uyarı tek başına bir şey kanıtlamaz: hiçbir şey yazmayıp yalnızca
// uyarı döndüren bir uygulama da üstteki testi geçerdi.
func TestUpdateAppVolumesActuallyPersist(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	update(t, srv, &panelyv1.UpdateAppRequest{
		AppId:   "blog",
		Volumes: []*panelyv1.AppVolume{{Name: "data", MountPath: "/var/lib/app"}},
	})

	got := mustGetSpec(t, srv, "blog")
	if len(got.GetVolumes()) != 1 || got.GetVolumes()[0].GetName() != "data" {
		t.Errorf("hacim yazılmadı: %+v", got.GetVolumes())
	}
}

// TestCreateAppRejectsInvalidVolume, doğrulamanın RPC yolunda
// koşulduğunu doğrular.
func TestCreateAppRejectsInvalidVolume(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})

	spec := testSpec()
	spec.Volumes = []*panelyv1.AppVolume{{Name: "data", MountPath: "goreli/yol"}}

	if _, err := srv.CreateApp(t.Context(),
		&panelyv1.CreateAppRequest{Spec: spec}); err == nil {
		t.Fatal("göreli bağlama noktası kabul edildi")
	}
}
