package store

import (
	"context"
	"testing"
)

func sampleVolumes() []VolumeMount {
	return []VolumeMount{
		{Name: "data", MountPath: "/var/lib/app", ReadOnly: false},
		{Name: "sablonlar", MountPath: "/etc/app/templates", ReadOnly: true},
	}
}

// TestCreateAppRoundTripsVolumes, hacimlerin yazılıp GERİ OKUNDUĞUNU
// doğrular.
//
// env'de olduğu gibi asıl tuzak `appSelect`: sütunu ekleyip select
// listesine yazmayı unutmak, yazma yolunu yeşil bırakıp okuma yolunu
// sessizce boş döndürür.
func TestCreateAppRoundTripsVolumes(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	want := sampleApp("blog")
	want.Volumes = sampleVolumes()

	if _, err := s.CreateApp(ctx, want); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}

	if len(got.Volumes) != 2 {
		t.Fatalf("%d hacim döndü, 2 bekleniyordu: %+v", len(got.Volumes), got.Volumes)
	}
	// Sıra BELİRLENİMCİ olmalı: ada göre sıralanıyor.
	if got.Volumes[0].Name != "data" || got.Volumes[1].Name != "sablonlar" {
		t.Errorf("hacim sırası belirlenimci değil: %+v", got.Volumes)
	}
	if got.Volumes[0].MountPath != "/var/lib/app" {
		t.Errorf("bağlama noktası %q", got.Volumes[0].MountPath)
	}
	// ⚠ ReadOnly bayrağı TAŞINMALI. Kaybolursa salt-okunur olması
	// gereken bir hacim YAZILABİLİR bağlanır — sessiz bir yetki
	// genişlemesi.
	if !got.Volumes[1].ReadOnly {
		t.Error("read_only bayrağı kayboldu — salt-okunur hacim yazılabilir bağlanır")
	}
	if got.Volumes[0].ReadOnly {
		t.Error("read_only bayrağı uydurulmuş")
	}
}

// TestNilVolumesBecomeEmptySlice, hacimsiz uygulamanın nil DEĞİL boş
// dilim döndürdüğünü doğrular.
func TestNilVolumesBecomeEmptySlice(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Volumes = nil
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if got.Volumes == nil {
		t.Error("hacimler nil döndü — boş dilim bekleniyordu")
	}
	if len(got.Volumes) != 0 {
		t.Errorf("hacimler boş değil: %+v", got.Volumes)
	}
}

// TestUnrelatedUpdatePreservesVolumes, EN ÖNEMLİ testtir.
//
// `UpdateApp` satırı oku-değiştir-yaz ile geri yazıyor. `volumes_json`
// cümleye eklenmezse ya da `scanApp` okumazsa, alan adını değiştiren bir
// komut uygulamanın BÜTÜN hacim tanımlarını uçurur — ve hiçbir hata
// çıkmaz. Veri diskte kalır ama uygulama ona bir daha bağlanmaz.
func TestUnrelatedUpdatePreservesVolumes(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Volumes = sampleVolumes()
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	newDomain := "yeni.example.com"
	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{Domain: &newDomain}); err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	// DİSKTEN okunuyor: dönen struct doğru olup satır yanlış olabilir.
	// Bu tuzak env'de ÖLÇÜLDÜ — mutasyon ilk turda yeşil kalmıştı.
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if len(got.Volumes) != 2 {
		t.Errorf("alan adı güncellemesi hacimleri sildi: %+v — hacimlere "+
			"HİÇ dokunulmamalıydı", got.Volumes)
	}
}

// TestUpdateMergesVolumesByName, birleştirmenin ADA göre yapıldığını
// doğrular.
//
// `repeated` alanların da presence'ı yok (env'deki `map` ile aynı
// sebep), dolayısıyla semantik burada da birleştirme olmak zorunda.
// Anahtar ADI: aynı ada sahip bir hacim, bağlama noktası değişse bile
// AYNI hacimdir — diskte aynı dizini gösterir.
func TestUpdateMergesVolumesByName(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Volumes = sampleVolumes()
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{
		Volumes: []VolumeMount{
			{Name: "data", MountPath: "/yeni/yol"},     // var olanı güncelle
			{Name: "yedek", MountPath: "/var/backups"}, // yeni ekle
		},
	}); err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if len(got.Volumes) != 3 {
		t.Fatalf("%d hacim, 3 bekleniyordu: %+v", len(got.Volumes), got.Volumes)
	}

	byName := map[string]VolumeMount{}
	for _, v := range got.Volumes {
		byName[v.Name] = v
	}
	if byName["data"].MountPath != "/yeni/yol" {
		t.Errorf("var olan hacim güncellenmedi: %+v", byName["data"])
	}
	if byName["yedek"].MountPath != "/var/backups" {
		t.Errorf("yeni hacim eklenmedi: %+v", byName)
	}
	if byName["sablonlar"].MountPath != "/etc/app/templates" {
		t.Errorf("dokunulmayan hacim kayboldu: %+v", byName)
	}
	if !byName["sablonlar"].ReadOnly {
		t.Error("dokunulmayan hacmin read_only bayrağı kayboldu")
	}
}

// TestUpdateRemovesVolumeByName, ayırma işlemini doğrular.
//
// ⚠ Bu VERİYİ SİLMEZ, yalnızca bağlamayı kaldırır. Diskteki dizin
// olduğu gibi kalır — `app delete`'in kararıyla aynı çizgi: yıkıcı olan
// asla örtük olmaz.
func TestUpdateRemovesVolumeByName(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Volumes = sampleVolumes()
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{
		VolumeRemove: []string{"sablonlar"},
	}); err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if len(got.Volumes) != 1 || got.Volumes[0].Name != "data" {
		t.Errorf("ayırma yanlış sonuç verdi: %+v", got.Volumes)
	}
}

// TestVolumeOnlyUpdateIsNotEmpty, yalnızca hacim taşıyan güncellemenin
// işlemsizlik sayılmadığını doğrular.
func TestVolumeOnlyUpdateIsNotEmpty(t *testing.T) {
	if (AppUpdate{Volumes: []VolumeMount{{Name: "data", MountPath: "/d"}}}).IsEmpty() {
		t.Error("hacim taşıyan güncelleme boş sayıldı")
	}
	if (AppUpdate{VolumeRemove: []string{"data"}}).IsEmpty() {
		t.Error("hacim ayırma taşıyan güncelleme boş sayıldı")
	}
}

// TestApplyDoesNotMutateCallersVolumes, Apply'ın ÇAĞIRANIN dilimini
// değiştirmediğini doğrular.
//
// Go'da dilim kopyası ALTTAKİ DİZİYİ paylaşır. Birleştirmeyi yerinde
// yapmak, API katmanının yalnızca doğrulamak için aldığı geçici
// birleşimin mevcut kaydı da bozması demekti.
func TestApplyDoesNotMutateCallersVolumes(t *testing.T) {
	current := sampleApp("blog")
	current.Volumes = sampleVolumes()

	upd := AppUpdate{Volumes: []VolumeMount{{Name: "data", MountPath: "/degisti"}}}
	merged := upd.Apply(current)

	if current.Volumes[0].MountPath != "/var/lib/app" {
		t.Errorf("Apply çağıranın dilimini değiştirdi: %+v", current.Volumes)
	}
	var found bool
	for _, v := range merged.Volumes {
		if v.Name == "data" && v.MountPath == "/degisti" {
			found = true
		}
	}
	if !found {
		t.Errorf("birleştirilmiş kopya yanlış: %+v", merged.Volumes)
	}
}
