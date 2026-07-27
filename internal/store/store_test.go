package store

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/erkanrzgc/panely/internal/audit"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	// Gerçek dosya kullanılır: WAL modu, tetikleyiciler ve göç akışı
	// bellek veritabanında tam olarak aynı davranmayabilir.
	path := filepath.Join(t.TempDir(), "panely.db")

	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("store açılamadı: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store kapatılamadı: %v", err)
		}
	})
	return s
}

func testRecord(action string) audit.Record {
	return audit.Record{
		Actor: audit.Actor{
			KeyFingerprint: "SHA256:TEST",
			SourceIP:       "203.0.113.7",
			Label:          "erkan@laptop",
			Origin:         "cli",
		},
		Action:     action,
		Target:     "app/blog",
		ParamsJSON: `{}`,
		Outcome:    audit.OutcomeSuccess,
		Source:     audit.SourceDaemon,
	}
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, table := range []string{"audit_log", "settings", "schema_migrations"} {
		var name string
		err := s.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("%s tablosu bulunamadı: %v", table, err)
		}
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panely.db")

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("ilk açılış başarısız: %v", err)
	}
	if _, err := s1.AppendAudit(ctx, testRecord("first.open")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("kapatılamadı: %v", err)
	}

	// İkinci açılış göçleri yeniden uygulamamalı ve veriyi korumalı.
	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("ikinci açılış başarısız: %v", err)
	}
	defer func() { _ = s2.Close() }()

	seq, _, err := s2.AuditHead(ctx)
	if err != nil {
		t.Fatalf("zincir başı okunamadı: %v", err)
	}
	if seq != 1 {
		t.Errorf("yeniden açılışta zincir başı = %d, beklenen 1", seq)
	}
}

func TestAppendAuditChainsRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.AppendAudit(ctx, testRecord("app.create"))
	if err != nil {
		t.Fatalf("ilk kayıt eklenemedi: %v", err)
	}
	second, err := s.AppendAudit(ctx, testRecord("app.deploy"))
	if err != nil {
		t.Fatalf("ikinci kayıt eklenemedi: %v", err)
	}

	if first.Seq != 1 {
		t.Errorf("ilk kaydın sırası = %d, beklenen 1", first.Seq)
	}
	if second.Seq != 2 {
		t.Errorf("ikinci kaydın sırası = %d, beklenen 2", second.Seq)
	}
	if first.PrevHash != audit.GenesisHash {
		t.Error("ilk kaydın prev_hash değeri genesis olmalı")
	}
	if second.PrevHash != first.Hash {
		t.Error("ikinci kayıt ilk kayda zincirlenmemiş")
	}
}

// TestAppendAuditIgnoresCallerSuppliedSeq, çağıranın zinciri yanlış sıraya
// sokamayacağını doğrular: seq ve prev_hash daima veritabanından okunur.
func TestAppendAuditIgnoresCallerSuppliedSeq(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rec := testRecord("app.create")
	rec.Seq = 9999
	rec.PrevHash = [audit.HashSize]byte{0xFF}

	sealed, err := s.AppendAudit(ctx, rec)
	if err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}
	if sealed.Seq != 1 {
		t.Errorf("çağıranın verdiği seq kullanıldı: %d", sealed.Seq)
	}
	if sealed.PrevHash != audit.GenesisHash {
		t.Error("çağıranın verdiği prev_hash kullanıldı")
	}
}

func TestAppendAuditRejectsInvalidRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		mutate func(*audit.Record)
	}{
		{"boş action", func(r *audit.Record) { r.Action = "" }},
		{"geçersiz outcome", func(r *audit.Record) { r.Outcome = audit.Outcome(0) }},
		{"geçersiz source", func(r *audit.Record) { r.Source = audit.Source(7) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := testRecord("app.create")
			tc.mutate(&rec)
			if _, err := s.AppendAudit(ctx, rec); err == nil {
				t.Fatal("geçersiz kayıt kabul edildi")
			}
		})
	}
}

// TestAuditLogRejectsUpdate, veritabanı katmanındaki ilk savunma hattını
// doğrular: uygulama hatası veya SQL enjeksiyonu geçmişi değiştiremez.
func TestAuditLogRejectsUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendAudit(ctx, testRecord("app.create")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	_, err := s.DB().ExecContext(ctx,
		`UPDATE audit_log SET action = 'kurcalandi' WHERE seq = 1`)
	if err == nil {
		t.Fatal("audit_log üzerinde UPDATE başarılı oldu — tetikleyici çalışmıyor")
	}
	if !strings.Contains(err.Error(), "yalnızca eklemeye açıktır") {
		t.Errorf("beklenen tetikleyici hatası değil: %v", err)
	}
}

func TestAuditLogRejectsDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendAudit(ctx, testRecord("app.create")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	_, err := s.DB().ExecContext(ctx, `DELETE FROM audit_log WHERE seq = 1`)
	if err == nil {
		t.Fatal("audit_log üzerinde DELETE başarılı oldu — tetikleyici çalışmıyor")
	}
	if !strings.Contains(err.Error(), "yalnızca eklemeye açıktır") {
		t.Errorf("beklenen tetikleyici hatası değil: %v", err)
	}
}

func TestVerifyAuditChainAcceptsValidChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 25
	for i := range n {
		if _, err := s.AppendAudit(ctx, testRecord("app.deploy")); err != nil {
			t.Fatalf("kayıt %d eklenemedi: %v", i, err)
		}
	}

	checked, err := s.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("geçerli zincir doğrulanamadı: %v", err)
	}
	if checked != n {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen %d", checked, n)
	}
}

func TestVerifyAuditChainOnEmptyChain(t *testing.T) {
	s := newTestStore(t)

	checked, err := s.VerifyAuditChain(context.Background())
	if err != nil {
		t.Fatalf("boş zincir doğrulanamadı: %v", err)
	}
	if checked != 0 {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen 0", checked)
	}
}

// TestVerifyAuditChainDetectsForgedInsert, tetikleyicilerin ATLATILDIĞI
// senaryoyu kapsar.
//
// UPDATE ve DELETE tetikleyicilerle kapalıdır, ama INSERT açıktır — zincire
// yazmak zaten böyle çalışır. Doğrudan SQL erişimi olan bir saldırgan sahte
// bir kayıt ekleyebilir. Kriptografik katmanın var olma sebebi tam olarak
// budur: veritabanı bunu kabul eder, zincir doğrulaması etmez.
func TestVerifyAuditChainDetectsForgedInsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendAudit(ctx, testRecord("app.create")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	// Şema kısıtlarını geçen ama zincire uymayan sahte kayıt.
	bogus := make([]byte, audit.HashSize)
	_, err := s.DB().ExecContext(ctx, `
		INSERT INTO audit_log (
			seq, ts_unix_nano, actor_fp, actor_ip, actor_label, actor_origin,
			action, target, params_json, outcome, detail, source, prev_hash, hash
		) VALUES (2, 0, '', '', '', '', 'sahte.kayit', '', '{}', 1, '', 1, ?, ?)`,
		bogus, bogus)
	if err != nil {
		t.Fatalf("sahte kayıt eklenemedi (test kurulumu): %v", err)
	}

	checked, err := s.VerifyAuditChain(ctx)
	if err == nil {
		t.Fatal("sahte kayıt tespit edilmedi")
	}
	if checked != 1 {
		t.Errorf("sahte kayıttan önce 1 kayıt doğrulanmalıydı, %d oldu", checked)
	}
}

func TestListAuditPaging(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const n = 10
	for range n {
		if _, err := s.AppendAudit(ctx, testRecord("app.deploy")); err != nil {
			t.Fatalf("kayıt eklenemedi: %v", err)
		}
	}

	page1, err := s.ListAudit(ctx, 0, 4)
	if err != nil {
		t.Fatalf("ilk sayfa okunamadı: %v", err)
	}
	if len(page1) != 4 {
		t.Fatalf("ilk sayfa boyutu = %d, beklenen 4", len(page1))
	}
	if page1[0].Seq != 1 || page1[3].Seq != 4 {
		t.Errorf("ilk sayfa sıraları beklenmedik: %d..%d", page1[0].Seq, page1[3].Seq)
	}

	page2, err := s.ListAudit(ctx, page1[3].Seq, 4)
	if err != nil {
		t.Fatalf("ikinci sayfa okunamadı: %v", err)
	}
	if page2[0].Seq != 5 {
		t.Errorf("ikinci sayfa %d'den başladı, beklenen 5", page2[0].Seq)
	}
}

func TestListAuditClampsLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendAudit(ctx, testRecord("app.create")); err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	// Üst sınırın aşılması hata değil, kırpma ile karşılanmalı.
	if _, err := s.ListAudit(ctx, 0, MaxAuditLimit*10); err != nil {
		t.Errorf("aşırı limit hata verdi: %v", err)
	}
	if _, err := s.ListAudit(ctx, 0, -5); err != nil {
		t.Errorf("negatif limit hata verdi: %v", err)
	}
}

func TestListAuditRoundTripsAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	want := testRecord("secret.rotate")
	want.Detail = "kasa anahtarı döndürüldü"
	want.Outcome = audit.OutcomeDenied
	want.ParamsJSON = `{"key":"[REDACTED]"}`

	sealed, err := s.AppendAudit(ctx, want)
	if err != nil {
		t.Fatalf("kayıt eklenemedi: %v", err)
	}

	got, err := s.ListAudit(ctx, 0, 10)
	if err != nil {
		t.Fatalf("kayıtlar okunamadı: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("kayıt sayısı = %d, beklenen 1", len(got))
	}

	g := got[0]
	if g.Action != sealed.Action || g.Target != sealed.Target ||
		g.ParamsJSON != sealed.ParamsJSON || g.Detail != sealed.Detail ||
		g.Outcome != sealed.Outcome || g.Source != sealed.Source ||
		g.Actor != sealed.Actor || g.Hash != sealed.Hash || g.PrevHash != sealed.PrevHash {
		t.Errorf("kayıt gidiş-dönüşte değişti:\nyazılan: %+v\nokunan:  %+v", sealed, g)
	}
	if !g.TS.Equal(sealed.TS) {
		t.Errorf("zaman damgası değişti: %v != %v", g.TS, sealed.TS)
	}
}

// TestConcurrentAppendsProduceContiguousChain, eşzamanlı eklemelerin
// zinciri bozmadığını doğrular. `go test -race` altında çalıştırılmalıdır.
func TestConcurrentAppendsProduceContiguousChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	const goroutines = 8
	const perGoroutine = 10

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perGoroutine)

	for g := range goroutines {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for range perGoroutine {
				if _, err := s.AppendAudit(ctx, testRecord("concurrent.append")); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("eşzamanlı ekleme başarısız: %v", err)
	}

	checked, err := s.VerifyAuditChain(ctx)
	if err != nil {
		t.Fatalf("eşzamanlı eklemeler zinciri bozdu: %v", err)
	}
	if want := uint64(goroutines * perGoroutine); checked != want {
		t.Errorf("doğrulanan kayıt sayısı = %d, beklenen %d", checked, want)
	}
}
