package store

import (
	"context"
	"database/sql"
	"testing"
)

// TestMigration0005CarriesExistingDeploymentsForward, göçün MEVCUT
// dağıtımları kaybetmediğini doğrular.
//
// ── Bu test neden ayrı yazıldı? ─────────────────────────────────────
//
// Diğer bütün depo testleri BOŞ bir `:memory:` veritabanıyla başlıyor ve
// bütün göçler tek seferde uygulanıyor. Yani 0005'in
//
//	INSERT INTO deployments_new ... SELECT ... FROM deployments
//
// satırı o testlerde HİÇ satır taşımıyor — sıfır satırlık bir kopya her
// zaman başarılıdır. Göç, gerçek sunucuda (canlı `portfolio` dağıtımıyla)
// ilk kez veri taşıyacaktı ve yanlışsa siteyi düşürürdü.
//
// Bu test göçleri ELLE, sırayla uyguluyor: 0004'e kadar getir, ESKİ şemaya
// bir dağıtım satırı yaz, sonra 0005'i uygula ve satırın AÇIK olarak
// hayatta kaldığını ölç.
func TestMigration0005CarriesExistingDeploymentsForward(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		) STRICT;`); err != nil {
		t.Fatalf("schema_migrations oluşturulamadı: %v", err)
	}

	// ── 0005 ÖNCESİ duruma getir ────────────────────────────────────
	for _, m := range []string{
		"0001_init.sql", "0002_apps.sql",
		"0003_deployments.sql", "0004_app_domain_unique.sql",
	} {
		if err := s.applyMigration(ctx, m); err != nil {
			t.Fatalf("%s uygulanamadı: %v", m, err)
		}
	}

	app := sampleApp("portfolio")
	app.Domain = "panely.erkanrzgc.dev"
	if _, err := s.CreateApp(ctx, app); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	rel := buildRelease(t, s, "portfolio", hexSHA(3))

	// ESKİ şemaya doğrudan yazılıyor: bu noktada `deactivated_at` sütunu
	// YOK, yani SetActiveRelease'in güncel hâli burada çalışamaz. Testin
	// göçten önceki gerçeği taklit etmesi gerekiyor.
	const activatedAt = 1_700_000_000_000_000_000
	if _, err := db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, release_id, activated_at) VALUES (?, ?, ?)`,
		"portfolio", rel.ID, activatedAt); err != nil {
		t.Fatalf("eski şemaya dağıtım yazılamadı: %v", err)
	}

	// ── GÖÇ ──────────────────────────────────────────────────────────
	if err := s.applyMigration(ctx, "0005_deployment_history.sql"); err != nil {
		t.Fatalf("0005 uygulanamadı — CANLI VERİTABANI GÖÇEMEZ: %v", err)
	}

	// ── Dağıtım hayatta ve AÇIK olmalı ──────────────────────────────
	live, err := s.ActiveDeployment(ctx, "portfolio")
	if err != nil {
		t.Fatalf("göçten sonra aktif dağıtım kayboldu: %v", err)
	}
	if live.ReleaseID != rel.ID {
		t.Errorf("aktif sürüm %q, %q bekleniyordu", live.ReleaseID, rel.ID)
	}
	if live.Domain != "panely.erkanrzgc.dev" {
		t.Errorf("alan adı taşınmadı: %q — ters vekil rotası üretilemez", live.Domain)
	}
	if got := live.ActivatedAt.UnixNano(); got != activatedAt {
		t.Errorf("aktivasyon zamanı değişti: %d, %d bekleniyordu", got, activatedAt)
	}

	// Tam olarak BİR satır olmalı ve o satır AÇIK olmalı.
	var total, open int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE deactivated_at IS NULL)
		 FROM deployments`).Scan(&total, &open); err != nil {
		t.Fatalf("satırlar sayılamadı: %v", err)
	}
	if total != 1 || open != 1 {
		t.Fatalf("göçten sonra %d satır (%d açık) — 1 açık satır bekleniyordu", total, open)
	}

	// ── Göçten SONRA yeni şema tam işlevli olmalı ────────────────────
	//
	// Kontrol grubu: göç yalnızca veriyi taşımakla kalmayıp çalışan bir
	// şema bırakmalı. Bırakmasaydı ilk dağıtımda anlaşılırdı.
	next := buildRelease(t, s, "portfolio", hexSHA(4))
	if err := s.SetActiveRelease(ctx, "portfolio", next.ID); err != nil {
		t.Fatalf("göçten sonra dağıtım yapılamıyor: %v", err)
	}
	prev, err := s.PreviousActiveRelease(ctx, "portfolio")
	if err != nil {
		t.Fatalf("göçten sonra geri alma hedefi bulunamıyor: %v", err)
	}
	if prev != rel.ID {
		t.Errorf("geri alma hedefi %q, %q bekleniyordu — göçten önceki "+
			"dağıtım geçmişe girmemiş", prev, rel.ID)
	}
}
