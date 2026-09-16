package api

import (
	"strconv"
	"strings"
	"testing"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// TestAppEnvRoundTripsThroughProto, env'in proto ↔ depo çevrimlerinde
// kaybolmadığını doğrular.
//
// Bu çevrimler elle yazılmış alan listeleri: yeni bir alan eklendiğinde
// derleyici UYARMAZ, çünkü eksik alan Go'nun sıfır değerine düşer. K-002
// tel tuzağı burada ateşlenmiyor — o tuzak yeni RPC'ler için kurulu, alan
// eklemek derlemeyi kırmaz. Yani bu katmanda tek koruma bu test.
func TestAppEnvRoundTripsThroughProto(t *testing.T) {
	spec := &panelyv1.AppSpec{
		AppId: "blog",
		Env:   map[string]string{"DATABASE_URL": "postgres://db/blog"},
	}

	app := appFromProto(spec)
	if app.Env["DATABASE_URL"] != "postgres://db/blog" {
		t.Fatalf("proto → depo çevriminde env kayboldu: %+v", app.Env)
	}

	back := appToProto(app)
	if back.GetSpec().GetEnv()["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("depo → proto çevriminde env kayboldu: %+v",
			back.GetSpec().GetEnv())
	}
}

// TestAppAuditParamsNeverCarryEnvValues, denetim kaydına env DEĞERİNİN
// girmediğini doğrular.
//
// ⚠ Bu testin koruduğu hata GERİ ALINAMAZ. `audit_log` üzerinde UPDATE ve
// DELETE tetikleyici düzeyinde yasak (RAISE(ABORT)); bir kez yazılan sır
// oradan SİLİNEMEZ. Veritabanı yedeği de o satırı taşır.
//
// Anahtar ADI yazılıyor ve bu kasıtlı: "kim hangi değişkeni ayarladı"
// denetlenebilir kalmalı. Sızan şey değer olurdu.
func TestAppAuditParamsNeverCarryEnvValues(t *testing.T) {
	const secret = "postgres://user:SUPERGIZLI@db:5432/blog"
	spec := &panelyv1.AppSpec{
		AppId: "blog",
		Env:   map[string]string{"DATABASE_URL": secret, "LOG_LEVEL": "debug"},
	}

	params := appAuditParams(spec)

	for k, v := range params {
		if strings.Contains(v, "SUPERGIZLI") || strings.Contains(v, secret) {
			t.Fatalf("denetim kaydına SIR sızdı: %s=%q", k, v)
		}
	}
	if params["env.DATABASE_URL"] != "[REDACTED]" {
		t.Errorf("env.DATABASE_URL %q, [REDACTED] bekleniyordu — anahtar "+
			"adı denetlenebilir kalmalı", params["env.DATABASE_URL"])
	}
	if params["env.LOG_LEVEL"] != "[REDACTED]" {
		t.Errorf("env.LOG_LEVEL %q", params["env.LOG_LEVEL"])
	}
}

// TestUpdateAuditParamsNeverCarryEnvValues, güncelleme yolunun da
// sızdırmadığını doğrular.
//
// Ayrı bir test çünkü ayrı bir fonksiyon: oluşturma yolu redakte ederken
// güncelleme yolunun ham değer yazması, tam da tek bir yolu düzeltip
// diğerini unutma sınıfı.
func TestUpdateAuditParamsNeverCarryEnvValues(t *testing.T) {
	const secret = "postgres://user:SUPERGIZLI@db:5432/blog"
	req := &panelyv1.UpdateAppRequest{
		AppId:     "blog",
		Env:       map[string]string{"DATABASE_URL": secret},
		EnvRemove: []string{"ESKI"},
	}

	params := updateAuditParams(req)

	for k, v := range params {
		if strings.Contains(v, "SUPERGIZLI") {
			t.Fatalf("denetim kaydına SIR sızdı: %s=%q", k, v)
		}
	}
	if params["env.DATABASE_URL"] != "[REDACTED]" {
		t.Errorf("env.DATABASE_URL %q", params["env.DATABASE_URL"])
	}
	// Silinen anahtarın ADI yazılmalı: "hangi değişken kaldırıldı"
	// sorusunun cevabı denetimde durmalı. Burada gizlenecek bir değer
	// zaten yok.
	if params["env_remove.ESKI"] == "" {
		t.Errorf("silinen anahtar denetime yazılmadı: %+v", params)
	}
}

// TestValidateEnvRejectsBadKeys, kabul edilemeyecek anahtarları
// doğrular.
//
// Anahtar deseni executor'ınkiyle AYNI olmalı. Gevşek olsaydı `app
// create` kabul eder, dağıtım executor'da patlardı — kullanıcı geçerli
// sandığı bir tanımla saatlerce uğraşırdı.
func TestValidateEnvRejectsBadKeys(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"rakamla başlıyor", "1PORT"},
		{"tire içeriyor", "DATABASE-URL"},
		{"boşluk içeriyor", "DATABASE URL"},
		{"eşittir içeriyor", "A=B"},
		{"boş", ""},
		{"NUL içeriyor", "A\x00B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEnv(map[string]string{tc.key: "x"})
			if err == nil {
				t.Errorf("geçersiz anahtar %q kabul edildi", tc.key)
			}
		})
	}
}

// TestValidateEnvRejectsNULInValue, değerdeki NUL'u doğrular.
//
// NUL, değişkeni execve dizisinde KESER: kabul edilen değer ile
// konteynerde görünen değer ayrışırdı.
func TestValidateEnvRejectsNULInValue(t *testing.T) {
	if err := validateEnv(map[string]string{"A": "bir\x00iki"}); err == nil {
		t.Error("değerdeki NUL kabul edildi")
	}
}

// TestValidateEnvAcceptsRealisticValues, KONTROL GRUBUdur.
//
// Olmasaydı "her şeyi reddet" diyen bir uygulama da yukarıdaki bütün
// testleri geçerdi.
func TestValidateEnvAcceptsRealisticValues(t *testing.T) {
	err := validateEnv(map[string]string{
		"DATABASE_URL": "postgres://user:pw@db:5432/blog?sslmode=require",
		"_GIZLI":       "alt çizgiyle başlamak geçerli",
		"PORT":         "3000",
		"BOS_DEGER":    "",
	})
	if err != nil {
		t.Errorf("geçerli env reddedildi: %v", err)
	}
}

// TestValidateEnvEnforcesTotalByteBudget, sınırın TOPLAM üzerinden
// işlediğini doğrular.
//
// ⚠ Ölçüldü: API katmanı build_args'ı DEĞER BAŞINA sınırlıyor
// (maxBuildArgLen), executor ise env'i TOPLAM üzerinden (maxEnvBytes).
// Aynı kalıbı kopyalasaydım 200 × 32 KiB `app create`'te kabul edilir,
// `deploy`'da executor tarafından reddedilirdi — "kabul et, sonra çök".
//
// API sınırı executor'ınkinden GEVŞEK OLAMAZ.
func TestValidateEnvEnforcesTotalByteBudget(t *testing.T) {
	big := map[string]string{}
	// 40 × 1 KiB = 40 KiB > 32 KiB. Hiçbir DEĞER tek başına büyük değil;
	// yalnızca toplam sınırı aşıyor.
	//
	// ⚠ Anahtarlar GEÇERLİ olmak zorunda. İlk yazımda `string(rune('A'+i))`
	// kullanılmıştı ve i>25 için '[', '\\', ']' gibi karakterler üretiyordu;
	// validateEnv bunları ANAHTAR DESENİNDEN reddediyor, bayt bütçesinden
	// değil. Test geçiyordu ama yanlış sebeple: bütçe kontrolünü kaldıran
	// mutasyon YEŞİL kaldı çünkü hata zaten başka bir yerden geliyordu.
	for i := range 40 {
		big["K_"+strconv.Itoa(i)] = strings.Repeat("x", 1024)
	}

	err := validateEnv(big)
	if err == nil {
		t.Fatal("toplam bayt sınırı aşıldığı hâlde kabul edildi — " +
			"executor bunu dağıtımda reddeder ve kullanıcı sebebi anlamaz")
	}
	// Hatanın SEBEBİ de doğrulanıyor: "bir hata döndü" iddiası, hatanın
	// bambaşka bir kontrolden gelmesi hâlinde de geçerdi.
	if !strings.Contains(err.Error(), "toplam") {
		t.Errorf("hata bayt bütçesinden gelmiyor: %v", err)
	}
}

// TestValidateEnvEnforcesEntryCount, girdi sayısı sınırını doğrular.
func TestValidateEnvEnforcesEntryCount(t *testing.T) {
	many := map[string]string{}
	for i := range maxEnvEntries + 1 {
		many["K_"+strconv.Itoa(i)] = "1"
	}
	if len(many) <= maxEnvEntries {
		t.Fatalf("test kurulumu bozuk: %d girdi üretildi", len(many))
	}
	if err := validateEnv(many); err == nil {
		t.Error("girdi sayısı sınırı aşıldığı hâlde kabul edildi")
	}
}

// TestValidateEnvRemoveRejectsContradiction, aynı anahtarın hem
// ayarlanıp hem silinmesini doğrular.
//
// Sessizce bir tarafı seçmek, kullanıcının iki niyetinden hangisinin
// uygulandığını belirsiz bırakırdı — ve belirsizlik sıralamaya bağlı bir
// hataya dönüşürdü.
func TestValidateEnvRemoveRejectsContradiction(t *testing.T) {
	err := validateEnvRemove(
		map[string]string{"DATABASE_URL": "x"},
		[]string{"DATABASE_URL"},
	)
	if err == nil {
		t.Fatal("aynı anahtar hem ayarlanıp hem silindiği hâlde kabul edildi")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("hata hangi anahtarı anlatmıyor: %v", err)
	}
}

// TestValidateEnvRemoveAcceptsDisjoint, KONTROL GRUBUdur.
func TestValidateEnvRemoveAcceptsDisjoint(t *testing.T) {
	err := validateEnvRemove(
		map[string]string{"YENI": "x"},
		[]string{"ESKI"},
	)
	if err != nil {
		t.Errorf("çakışmayan ayarla/sil reddedildi: %v", err)
	}
}

// TestUpdateAppRejectsContradictoryEnv, celiskinin RPC seviyesinde
// reddedildigini dogrular.
//
// validateEnvRemove'un var olmasi yetmez -- CAGRILMASI gerekir.
// Mekanizmayi yazip tetikleyiciyi unutmak bu projede olculmus bir hata
// sinifi (K-080): fonksiyon testlerde yesil, uretimde hic calismaz.
func TestUpdateAppRejectsContradictoryEnv(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	_, err := srv.UpdateApp(t.Context(), &panelyv1.UpdateAppRequest{
		AppId:     "blog",
		Env:       map[string]string{"DATABASE_URL": "x"},
		EnvRemove: []string{"DATABASE_URL"},
	})
	if err == nil {
		t.Fatal("ayni anahtar hem ayarlanip hem silindigi halde kabul edildi")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("hata hangi anahtari anlatmiyor: %v", err)
	}
}

// TestUpdateAppWarnsEnvNeedsRedeploy, komutun SESSIZCE basarili
// demedigini dogrular.
//
// Docker calisan bir konteynerin ortamini degistiremez. Kayit yazilir,
// konteynerler eski env ile kosmaya devam eder. Bunu soylemeyen bir
// cevap, kullanicinin DATABASE_URL'in devreye girdigini sanmasina yol
// acar -- ve uygulama calismayinca sebebi bambaska yerde arar.
//
// Ayni sekil `moveTraffic`ten geliyor: alan adi kaydedildi ama trafik
// tasinmadi uyarisi. Orada ogrenilen dersin burada tekrar ogrenilmesi
// gerekmiyor.
func TestUpdateAppWarnsEnvNeedsRedeploy(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	resp := update(t, srv, &panelyv1.UpdateAppRequest{
		AppId: "blog",
		Env:   map[string]string{"DATABASE_URL": "postgres://db/blog"},
	})

	detail := resp.GetEnvDetail()
	if detail == "" {
		t.Fatal("env degisti ama hicbir uyari dondurulmedi -- kullanici " +
			"degiskenin hemen devreye girdigini sanir")
	}
	if !strings.Contains(detail, "deploy") {
		t.Errorf("uyari NE YAPILACAGINI soylemiyor: %q", detail)
	}
	if !strings.Contains(strings.ToLower(detail), "env") {
		t.Errorf("uyari neyden bahsettigini soylemiyor: %q", detail)
	}
}

// TestUpdateAppEnvActuallyPersists, env'in RPC uzerinden GERCEKTEN
// yazildigini dogrular.
//
// Uyarinin dondurulmesi tek basina bir sey kanitlamaz: hicbir sey
// yazmayip yalnizca uyari donduren bir uygulama da yukaridaki testi
// gecerdi.
func TestUpdateAppEnvActuallyPersists(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	update(t, srv, &panelyv1.UpdateAppRequest{
		AppId: "blog",
		Env:   map[string]string{"DATABASE_URL": "postgres://db/blog"},
	})

	got := mustGetSpec(t, srv, "blog")
	if got.GetEnv()["DATABASE_URL"] != "postgres://db/blog" {
		t.Errorf("env yazilmadi: %+v", got.GetEnv())
	}
}

// TestCreateAppRejectsInvalidEnv, dogrulamanin RPC yolunda GERCEKTEN
// kosuldugunu dogrular.
//
// validateEnv'in dogru calismasi yetmez -- validateAppSpec'ten
// CAGRILMASI gerekir. Birim testleri fonksiyonu dogrudan cagirdigi icin
// cagriyi silmek onlarin hicbirini kirmaz: fonksiyon kusursuz calisir,
// hic kullanilmaz.
//
// Olculdu: bu test yokken "validateEnv AppSpec dogrulamasindan
// cikarildi" mutasyonu YESIL kaldi.
func TestCreateAppRejectsInvalidEnv(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})

	spec := testSpec()
	spec.Env = map[string]string{"GECERSIZ-AD": "x"}

	_, err := srv.CreateApp(t.Context(), &panelyv1.CreateAppRequest{Spec: spec})
	if err == nil {
		t.Fatal("gecersiz env anahtari kabul edildi -- executor bunu " +
			"dagitimda reddeder ve kullanici sebebini goremez")
	}
	if !strings.Contains(err.Error(), "GECERSIZ-AD") {
		t.Errorf("hata hangi anahtari anlatmiyor: %v", err)
	}
}

// TestUpdateAppRejectsInvalidEnv, guncelleme yolunun da dogruladigini
// dogrular.
//
// Ayri bir yol: UpdateApp BIRLESTIRILMIS tanimi dogruluyor. Dogrulama
// birlestirmeden once yapilsaydi tek basina gecerli gorunen bir delta
// mevcut durumla birlesip gecersiz bir tanim uretebilirdi.
func TestUpdateAppRejectsInvalidEnv(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	_, err := srv.UpdateApp(t.Context(), &panelyv1.UpdateAppRequest{
		AppId: "blog",
		Env:   map[string]string{"1RAKAMLA": "x"},
	})
	if err == nil {
		t.Fatal("gecersiz env anahtari guncellemede kabul edildi")
	}
}
