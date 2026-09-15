package store

import (
	"context"
	"testing"
)

// TestCreateAppRoundTripsEnv, env'in yazılıp GERİ OKUNDUĞUNU doğrular.
//
// Yalnızca yazmayı sınamak yetmez: bu depoda sütun eklenip `appSelect`
// listesine yazılmayı unutmak, yazma yolunu yeşil bırakıp okuma yolunu
// sessizce boş döndüren bir hata üretir. Kayıt doğru, gerçeklik boş.
func TestCreateAppRoundTripsEnv(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	want := sampleApp("blog")
	want.Env = map[string]string{
		"DATABASE_URL": "postgres://user:pw@db:5432/blog",
		"LOG_LEVEL":    "debug",
	}

	if _, err := s.CreateApp(ctx, want); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if len(got.Env) != 2 {
		t.Fatalf("env %d girdi döndü, 2 bekleniyordu: %+v", len(got.Env), got.Env)
	}
	if got.Env["DATABASE_URL"] != want.Env["DATABASE_URL"] {
		t.Errorf("DATABASE_URL %q, %q bekleniyordu",
			got.Env["DATABASE_URL"], want.Env["DATABASE_URL"])
	}
	if got.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL %q", got.Env["LOG_LEVEL"])
	}
}

// TestNilEnvBecomesEmptyMap, env verilmeyen uygulamanın nil DEĞİL boş
// harita döndürdüğünü doğrular.
//
// encoding/json nil bir map'i "null" yazar, şema ise DEFAULT '{}' diyor.
// İkisi ayrışırsa "env yok" durumu yazma yoluna göre farklı temsil edilir
// ve çağıranların yarısı nil kontrolü yapmak zorunda kalır. build_args
// bu dersi zaten öğrenmişti (sortedArgs); env de aynı yolu izlemeli.
func TestNilEnvBecomesEmptyMap(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Env = nil

	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if got.Env == nil {
		t.Error("env nil döndü — boş harita bekleniyordu")
	}
	if len(got.Env) != 0 {
		t.Errorf("env boş değil: %+v", got.Env)
	}
}

// TestUnrelatedUpdatePreservesEnv, EN ÖNEMLİ testtir.
//
// `UpdateApp` sabit bir UPDATE cümlesi kullanıyor ve satırı oku-değiştir-yaz
// ile geri yazıyor. `env_json` o cümleye eklenmezse env sessizce ESKİ
// değerinde kalır (iyi), ama `scanApp` okumazsa boş haritaya düşer ve
// geri yazma onu SİLER. Yani alan adını değiştiren bir komut, uygulamanın
// bütün env değişkenlerini uçurabilir — ve hiçbir hata mesajı çıkmaz.
//
// Bu tam olarak K-080'in şekli: kayıt doğru görünür, gerçeklik değişir.
func TestUnrelatedUpdatePreservesEnv(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Env = map[string]string{"DATABASE_URL": "postgres://db/blog"}
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	newDomain := "yeni.example.com"
	got, err := s.UpdateApp(ctx, "blog", AppUpdate{Domain: &newDomain})
	if err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	if got.Env["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("alan adı güncellemesi env'i bozdu: %+v — env'e HİÇ "+
			"dokunulmamalıydı", got.Env)
	}

	// Diskten de oku: dönen struct doğru olup satır yanlış olabilir.
	reread, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if reread.Env["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("DİSKTEKİ env bozuldu: %+v", reread.Env)
	}
}

// TestUpdateMergesEnv, birleştirme semantiğini doğrular.
//
// Verilmeyen anahtarlara DOKUNULMAZ. Aksi (tamamen değiştirme) proto3'te
// zaten dürüstçe temsil edilemezdi: map alanlarının presence'ı yok, yani
// "hiç verilmedi" ile "boşalt" aynı boş haritaya düşer ve `-env` yazmayan
// her güncelleme bütün env'i silerdi.
func TestUpdateMergesEnv(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Env = map[string]string{
		"DATABASE_URL": "postgres://db/blog",
		"LOG_LEVEL":    "info",
	}
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{
		Env: map[string]string{"LOG_LEVEL": "debug", "PORT": "3000"},
	}); err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	// ⚠ DİSKTEN okunuyor, UpdateApp'in dönüşünden DEĞİL.
	//
	// `UpdateApp` satırı bellekte değiştirip döndürüyor. Dönen struct'a
	// bakan bir test, UPDATE cümlesi `env_json` sütununu hiç yazmasa bile
	// GEÇER — ölçüldü: sütunu cümleden düşüren mutasyon yeşil kaldı.
	// Kaydın doğru görünüp gerçekliğin değişmemesi, bu testin var olma
	// sebebi olan hatanın ta kendisi.
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}

	if got.Env["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("dokunulmayan anahtar kayboldu: %+v", got.Env)
	}
	if got.Env["LOG_LEVEL"] != "debug" {
		t.Errorf("var olan anahtar güncellenmedi: %q", got.Env["LOG_LEVEL"])
	}
	if got.Env["PORT"] != "3000" {
		t.Errorf("yeni anahtar eklenmedi: %+v", got.Env)
	}
}

// TestUpdateRemovesEnvKeys, silme bayrağının GERÇEKTEN sildiğini doğrular.
//
// Birleştirme tek başına bir anahtarı kaldırmayı imkânsız kılardı — boş
// dizeye ayarlamak silmek DEĞİLDİR: konteyner o değişkeni boş değerle
// görür ve "tanımlı mı" diye bakan uygulama yanlış cevap alır.
func TestUpdateRemovesEnvKeys(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Env = map[string]string{"DATABASE_URL": "postgres://db/blog", "ESKI": "1"}
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama yazılamadı: %v", err)
	}

	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{
		EnvRemove: []string{"ESKI"},
	}); err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}

	// Silme de DİSKTEN doğrulanıyor — aynı gerekçe (yukarıya bakın).
	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}

	if _, still := got.Env["ESKI"]; still {
		t.Errorf("anahtar silinmedi: %+v", got.Env)
	}
	if got.Env["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("silme, ilgisiz anahtarı da götürdü: %+v", got.Env)
	}
}

// TestEnvUpdateIsNotEmpty, yalnızca env taşıyan bir güncellemenin
// "hiçbir alan belirtilmedi" sayılmadığını doğrular.
//
// IsEmpty'ye env eklenmezse API katmanı `panely app update -env ...`
// çağrısını işlemsizlik diye REDDEDER — mekanizma bağlanır, komut
// çalışmaz.
func TestEnvUpdateIsNotEmpty(t *testing.T) {
	set := AppUpdate{Env: map[string]string{"A": "1"}}
	if set.IsEmpty() {
		t.Error("env taşıyan güncelleme boş sayıldı")
	}

	rm := AppUpdate{EnvRemove: []string{"A"}}
	if rm.IsEmpty() {
		t.Error("env silme taşıyan güncelleme boş sayıldı")
	}

	if !(AppUpdate{}).IsEmpty() {
		t.Error("gerçekten boş güncelleme boş sayılmadı")
	}
}

// TestApplyDoesNotMutateCallersEnv, Apply'ın ÇAĞIRANIN haritasını
// değiştirmediğini doğrular.
//
// Apply değer alıcılı ve bir kopya döndürüyor — ama Go'da struct kopyası
// haritayı DERİN kopyalamaz. Birleştirmeyi doğrudan `app.Env` üzerine
// yazmak, doğrulama için alınan geçici kopyanın mevcut kaydı da
// değiştirmesi demekti: API katmanı `upd.Apply(current)` ile doğruluyor ve
// `current`'ı sonra "eski değer" olarak okuyor.
func TestApplyDoesNotMutateCallersEnv(t *testing.T) {
	current := sampleApp("blog")
	current.Env = map[string]string{"DATABASE_URL": "eski"}

	upd := AppUpdate{Env: map[string]string{"DATABASE_URL": "yeni"}}
	merged := upd.Apply(current)

	if current.Env["DATABASE_URL"] != "eski" {
		t.Errorf("Apply çağıranın haritasını değiştirdi: %+v", current.Env)
	}
	if merged.Env["DATABASE_URL"] != "yeni" {
		t.Errorf("birleştirilmiş kopya yanlış: %+v", merged.Env)
	}
}
