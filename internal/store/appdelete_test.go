package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// seedApp, silme testleri için uygulama + sürüm + geçmiş kurar.
func seedApp(t *testing.T, s *Store, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.CreateApp(ctx, sampleApp(id)); err != nil {
		t.Fatalf("uygulama kurulamadı: %v", err)
	}
}

// TestDeleteAppRemovesEveryTrace, silmenin kontrol düzleminde hiçbir
// artık bırakmadığını doğrular.
func TestDeleteAppRemovesEveryTrace(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedApp(t, s, "blog")

	r1 := buildRelease(t, s, "blog", shaA)
	buildRelease(t, s, "blog", shaB)

	// Bir dağıtım geçmişi de olsun, sonra kapatılsın: silinecek uygulama
	// "hiç dağıtılmamış" değil, "artık canlı değil" olmalı.
	if err := s.SetActiveRelease(ctx, "blog", r1.ID); err != nil {
		t.Fatalf("aktif sürüm yazılamadı: %v", err)
	}
	if err := s.ClearActiveRelease(ctx, "blog"); err != nil {
		t.Fatalf("aktif sürüm kaldırılamadı: %v", err)
	}

	counts, err := s.DeleteApp(ctx, "blog")
	if err != nil {
		t.Fatalf("silme başarısız: %v", err)
	}
	if counts.Releases != 2 {
		t.Errorf("silinen sürüm %d, 2 bekleniyordu", counts.Releases)
	}
	if counts.Deployments != 1 {
		t.Errorf("silinen dağıtım kaydı %d, 1 bekleniyordu", counts.Deployments)
	}

	if _, err := s.GetApp(ctx, "blog"); !errors.Is(err, ErrAppNotFound) {
		t.Errorf("uygulama hâlâ okunabiliyor: %v", err)
	}
	// Çocuk tablolarda artık kalmamalı: kalsaydı aynı kimlikle yeni bir
	// uygulama yaratıldığında ölü sürümleri miras alırdı.
	assertNoRows(t, s, "releases", "blog")
	assertNoRows(t, s, "deployments", "blog")
}

func assertNoRows(t *testing.T, s *Store, table, appID string) {
	t.Helper()
	var n int
	q := "SELECT COUNT(*) FROM " + table + " WHERE app_id = ?"
	if err := s.db.QueryRowContext(context.Background(), q, appID).Scan(&n); err != nil {
		t.Fatalf("%s sayılamadı: %v", table, err)
	}
	if n != 0 {
		t.Errorf("%s tablosunda %d artık satır kaldı", table, n)
	}
}

// TestDeleteAppRefusesWhileLive, canlı sürümü olan uygulamanın
// silinemediğini doğrular.
//
// ── Kontrol neden İŞLEMİN İÇİNDE ────────────────────────────────────
//
// Yalnızca çağıran tarafta bakılsaydı, kontrol ile silme arasında
// tamamlanan bir dağıtım canlı bir uygulamanın kaydını sildirirdi:
// konteynerleri çalışmaya devam eder, ama adları app_id/release_id'den
// türediği için onlara ulaşacak hiçbir kayıt kalmazdı.
func TestDeleteAppRefusesWhileLive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedApp(t, s, "blog")
	rel := buildRelease(t, s, "blog", shaA)
	if err := s.SetActiveRelease(ctx, "blog", rel.ID); err != nil {
		t.Fatalf("aktif sürüm yazılamadı: %v", err)
	}

	_, err := s.DeleteApp(ctx, "blog")
	if !errors.Is(err, ErrAppIsLive) {
		t.Fatalf("hata %v, ErrAppIsLive bekleniyordu", err)
	}
	// Hangi sürümün canlı olduğu SÖYLENMELİ: "silinemez" tek başına
	// operatöre ne yapacağını göstermiyor.
	if !strings.Contains(err.Error(), rel.ID) {
		t.Errorf("hata %q — canlı sürüm kimliğini (%s) içermeliydi", err, rel.ID)
	}

	// Ve hiçbir şeye dokunulmamış olmalı.
	if _, err := s.GetApp(ctx, "blog"); err != nil {
		t.Errorf("reddedilen silme uygulamayı bozdu: %v", err)
	}
	if _, err := s.ActiveDeployment(ctx, "blog"); err != nil {
		t.Errorf("reddedilen silme dağıtımı bozdu: %v", err)
	}
}

// TestDeleteAppReportsMissingApp, olmayan uygulamanın ayırt edilebilir
// bir hata döndürdüğünü doğrular.
func TestDeleteAppReportsMissingApp(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.DeleteApp(context.Background(), "yok"); !errors.Is(err, ErrAppNotFound) {
		t.Errorf("hata %v, ErrAppNotFound bekleniyordu", err)
	}
}

// TestDeleteAppIsRepeatable, ikinci çağrının "bulunamadı" ile
// bitmesini doğrular — yarıda kalmış bir silme yeniden çalıştırılabilmeli.
func TestDeleteAppIsRepeatable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedApp(t, s, "blog")
	buildRelease(t, s, "blog", shaA)

	if _, err := s.DeleteApp(ctx, "blog"); err != nil {
		t.Fatalf("ilk silme başarısız: %v", err)
	}
	if _, err := s.DeleteApp(ctx, "blog"); !errors.Is(err, ErrAppNotFound) {
		t.Errorf("ikinci silme %v döndü, ErrAppNotFound bekleniyordu", err)
	}
}

// TestDeleteAppLeavesOtherAppsAlone, silmenin yalnızca hedefi
// etkilediğini doğrular.
//
// `WHERE app_id = ?` unutulmuş tek bir DELETE, bütün kontrol düzlemini
// siler ve bunu sessizce yapar.
func TestDeleteAppLeavesOtherAppsAlone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedApp(t, s, "blog")
	seedApp(t, s, "portfolio")
	buildRelease(t, s, "blog", shaA)
	keep := buildRelease(t, s, "portfolio", shaB)
	if err := s.SetActiveRelease(ctx, "portfolio", keep.ID); err != nil {
		t.Fatalf("aktif sürüm yazılamadı: %v", err)
	}

	if _, err := s.DeleteApp(ctx, "blog"); err != nil {
		t.Fatalf("silme başarısız: %v", err)
	}

	if _, err := s.GetApp(ctx, "portfolio"); err != nil {
		t.Errorf("başka uygulama silindi: %v", err)
	}
	if _, err := s.GetRelease(ctx, "portfolio", keep.ID); err != nil {
		t.Errorf("başka uygulamanın sürümü silindi: %v", err)
	}
	if _, err := s.ActiveDeployment(ctx, "portfolio"); err != nil {
		t.Errorf("başka uygulamanın dağıtımı silindi: %v", err)
	}
}
