package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// buildRelease, derlenmiş (BUILT) bir sürüm üretir.
func buildRelease(t *testing.T, s *Store, appID, sha string) Release {
	t.Helper()
	ctx := context.Background()

	rel, err := s.StartRelease(ctx, appID, sha)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	if err := s.FinishRelease(ctx, appID, rel.ID, "sha256:"+sha); err != nil {
		t.Fatalf("sürüm mühürlenemedi: %v", err)
	}
	return rel
}

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
	shaC = "3333333333333333333333333333333333333333"
)

// TestActiveReleaseMustBeBuilt, imajı KANITLANMAMIŞ bir sürüme trafik
// taşınamadığını doğrular.
//
// Bu, K-042 zincirinin son halkası. `releases` şeması "BUILT ise image_id
// dolu olmalı" diyordu; buradaki tetikleyici "aktif sürüm BUILT olmalı"
// diyor. İkisi birleşince, derlemesi yarıda kalmış ya da BAŞARISIZ bir
// sürümün canlıya alınması veritabanında TEMSİL EDİLEMEZ oluyor.
//
// Uygulama katmanına bırakılsaydı, dağıtım akışındaki tek bir hata
// kullanıcının sitesini 502 yapardı.
func TestActiveReleaseMustBeBuilt(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}

	// 1. Hâlâ derlenmekte olan sürüm.
	building, err := s.StartRelease(ctx, "blog", shaA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	err = s.SetActiveRelease(ctx, "blog", building.ID)
	if err == nil {
		t.Fatal("DERLENMEKTE olan sürüm canlıya alındı")
	}
	if !strings.Contains(err.Error(), "BUILT olmalı") {
		t.Errorf("hata sebebi açık değil: %v", err)
	}

	// 2. Derlemesi BAŞARISIZ sürüm.
	failed, err := s.StartRelease(ctx, "blog", shaB)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	if err := s.FailRelease(ctx, "blog", failed.ID, "derleme hatası"); err != nil {
		t.Fatalf("sürüm mühürlenemedi: %v", err)
	}
	if err := s.SetActiveRelease(ctx, "blog", failed.ID); err == nil {
		t.Fatal("BAŞARISIZ sürüm canlıya alındı")
	}

	// 3. Derlenmiş sürüm geçmeli — aksi hâlde test hiçbir şey kanıtlamaz,
	//    yalnızca "her şey reddediliyor" der.
	built := buildRelease(t, s, "blog", shaC)
	if err := s.SetActiveRelease(ctx, "blog", built.ID); err != nil {
		t.Fatalf("DERLENMİŞ sürüm reddedildi: %v", err)
	}
}

// TestActivatingReplacesRatherThanAdds, bir uygulamanın aynı anda iki
// aktif sürümü OLAMAYACAĞINI doğrular.
//
// Kısıt `app_id`'nin birincil anahtar olmasından geliyor, bir kontrolden
// değil: blue-green geçişi yarıda kalsa bile ikili durum oluşamaz.
func TestActivatingReplacesRatherThanAdds(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	first := buildRelease(t, s, "blog", shaA)
	second := buildRelease(t, s, "blog", shaB)

	for _, rel := range []Release{first, second} {
		if err := s.SetActiveRelease(ctx, "blog", rel.ID); err != nil {
			t.Fatalf("%s canlıya alınamadı: %v", rel.ID, err)
		}
	}

	all, err := s.ActiveDeployments(ctx)
	if err != nil {
		t.Fatalf("dağıtımlar okunamadı: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("%d aktif dağıtım var, 1 olmalı: %+v", len(all), all)
	}
	if all[0].ReleaseID != second.ID {
		t.Errorf("aktif sürüm %q, %q bekleniyordu", all[0].ReleaseID, second.ID)
	}
}

// TestActiveDeploymentsCarriesEveryApp, ters vekil yapılandırmasının
// üretileceği listenin EKSİKSİZ olduğunu doğrular.
//
// Caddy'nin POST /load ucu kök nesnenin tamamını değiştiriyor: tek bir
// uygulamadan üretilmiş bir yapılandırma diğerlerinin rotalarını siler ve
// alakasız bir siteyi internetten düşürür. Bu sorgunun eksik dönmesi,
// aynı sonucu SESSİZCE üretirdi.
func TestActiveDeploymentsCarriesEveryApp(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	// Sıra kasten ters: sonucun app_id'ye göre BELİRLENİMLİ sıralandığı
	// da sınanıyor. Sıralama olmasaydı aynı durumdan iki farklı JSON
	// çıkar ve geri okuma karşılaştırması gürültüye boğulurdu.
	for _, id := range []string{"shop", "blog"} {
		app := sampleApp(id)
		app.Domain = id + ".example.com"
		if _, err := s.CreateApp(ctx, app); err != nil {
			t.Fatalf("%s oluşturulamadı: %v", id, err)
		}
		rel := buildRelease(t, s, id, shaA)
		if err := s.SetActiveRelease(ctx, id, rel.ID); err != nil {
			t.Fatalf("%s canlıya alınamadı: %v", id, err)
		}
	}

	all, err := s.ActiveDeployments(ctx)
	if err != nil {
		t.Fatalf("dağıtımlar okunamadı: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("%d dağıtım döndü, 2 bekleniyordu", len(all))
	}
	if all[0].AppID != "blog" || all[1].AppID != "shop" {
		t.Errorf("sıralama belirlenimli değil: %s, %s", all[0].AppID, all[1].AppID)
	}
	// Alan adı ve port JOIN'den gelmeli: ters vekil rotası bunlarsız
	// üretilemez.
	if all[0].Domain != "blog.example.com" {
		t.Errorf("alan adı taşınmıyor: %q", all[0].Domain)
	}
	if all[0].ContainerPort == 0 {
		t.Error("konteyner portu taşınmıyor — upstream adresi kurulamaz")
	}
}

// TestActiveReleaseMustBelongToTheApp, başka bir uygulamanın sürümünün
// canlıya alınamadığını doğrular.
//
// Kimlikler uygulamalar arasında tekrar ediyor ("r1" her uygulamada var),
// yani yalnızca release_id'ye bakan bir kısıt yanlış imajı canlıya
// çıkarırdı.
//
// ⚠ BU TESTİ GEÇİREN ŞEY YABANCI ANAHTAR DEĞİL, TETİKLEYİCİ.
//
// Ölçüldü: DSN `foreign_keys(0)` yapıldığında bu test HÂLÂ GEÇİYOR.
// Sebebi, tetikleyicinin alt sorgusunun aynı demete bakması — sürüm satırı
// bulunamayınca NULL dönüyor ve `NULL IS NOT 2` doğru olduğu için ABORT
// ediyor. Bileşik yabancı anahtar burada ikinci katman, tek katman değil.
//
// Bu not, yorumun andığı mekanizmanın gerçekten çalışan mekanizma olması
// içindir: ilk hâlinde "yabancı anahtar yakalıyor" yazıyordu ve YANLIŞTI.
// Yabancı anahtarın tek başına koruduğu yön ayrıca sınanıyor
// (bkz. TestActiveReleaseCannotBeDeleted).
func TestActiveReleaseMustBelongToTheApp(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	for _, id := range []string{"blog", "shop"} {
		if _, err := s.CreateApp(ctx, sampleApp(id)); err != nil {
			t.Fatalf("%s oluşturulamadı: %v", id, err)
		}
	}
	shopRelease := buildRelease(t, s, "shop", shaA)

	// `blog`ın hiç sürümü yok; `shop`unkini kullanmaya çalış.
	if err := s.SetActiveRelease(ctx, "blog", shopRelease.ID); err == nil {
		t.Fatal("başka uygulamanın sürümü canlıya alındı")
	}
}

// TestActiveDeploymentReportsAbsenceDistinctly, "aktif sürüm yok"
// durumunun bir HATA GÜRÜLTÜSÜ değil, ayırt edilebilir bir cevap
// olduğunu doğrular.
//
// Uzlaştırma döngüsü bu iki durumu ayırmak zorunda: henüz dağıtılmamış
// bir uygulama olağan, veritabanı hatası ise değil.
func TestActiveDeploymentReportsAbsenceDistinctly(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}

	_, err := s.ActiveDeployment(ctx, "blog")
	if !errors.Is(err, ErrNoDeployment) {
		t.Fatalf("dağıtılmamış uygulama için %v döndü, ErrNoDeployment bekleniyordu", err)
	}
}

// TestForeignKeysAreEnforcedOnTheRealConnection, yabancı anahtarların
// panelyd'nin KULLANDIĞI bağlantıda etkin olduğunu doğrular.
//
// ── Bu test neden var? ──────────────────────────────────────────────
//
// SQLite'ta `foreign_keys` BAĞLANTI BAŞINA bir ayardır ve varsayılanı
// KAPALI. DSN'e yazılmış olması yeterli görünüyordu; gerçek sunucuda
// `sqlite3` CLI ile bakıldığında 0 döndü — ama o, CLI'ın kendi
// bağlantısıydı, panelyd'ninki değil. Yani o ölçüm bu soruyu
// yanıtlamıyordu.
//
// Ayrıca: deployments üzerindeki tetikleyiciler, INSERT/UPDATE yolunda
// yabancı anahtarla AYNI demeti kontrol ediyor. Dolayısıyla mevcut
// testlerin hiçbiri yabancı anahtarın çalıştığını KANITLAMIYOR — hepsi
// tetikleyici kapatılsa bile geçerdi. Yabancı anahtarın tek başına
// koruduğu şey SİLME yönü: aktif sürümü olan bir uygulamanın ya da
// canlıdaki bir sürümün silinmesi.
func TestForeignKeysAreEnforcedOnTheRealConnection(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	var on int
	if err := s.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&on); err != nil {
		t.Fatalf("pragma okunamadı: %v", err)
	}
	if on != 1 {
		t.Fatalf("foreign_keys KAPALI (%d) — bileşik yabancı anahtar hiçbir şey korumuyor", on)
	}
}

// TestActiveReleaseCannotBeDeleted, canlıdaki bir sürümün satırının
// silinemediğini doğrular.
//
// Bu, YALNIZCA yabancı anahtarın koruduğu yön: tetikleyiciler
// deployments üzerindeki INSERT/UPDATE'e bakıyor, releases'ten silmeye
// değil. Yabancı anahtar sessizce kapalı olsaydı bu test kırmızıya
// dönerdi — yukarıdaki pragma testinin gerçek karşılığı budur.
func TestActiveReleaseCannotBeDeleted(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	rel := buildRelease(t, s, "blog", shaA)
	if err := s.SetActiveRelease(ctx, "blog", rel.ID); err != nil {
		t.Fatalf("canlıya alınamadı: %v", err)
	}

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM releases WHERE app_id = ? AND id = ?", "blog", rel.ID)
	if err == nil {
		t.Fatal("canlıdaki sürüm silindi — dağıtım kaydı olmayan bir sürümü gösteriyor")
	}
}
