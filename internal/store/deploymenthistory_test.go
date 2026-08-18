package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// hexSHA, geçerli bir commit sha üretir (40 hane, küçük harf onaltılık).
func hexSHA(n int) string { return fmt.Sprintf("%040x", n) }

// activateSeries, sürümleri sırayla derleyip canlıya alır ve kimliklerini
// döndürür.
func activateSeries(t *testing.T, s *Store, appID string, count int) []string {
	t.Helper()
	ctx := context.Background()

	ids := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		rel := buildRelease(t, s, appID, hexSHA(i))
		if err := s.SetActiveRelease(ctx, appID, rel.ID); err != nil {
			t.Fatalf("%s canlıya alınamadı: %v", rel.ID, err)
		}
		ids = append(ids, rel.ID)
	}
	return ids
}

// rawDeploymentRows, tablodaki TÜM satırları sayar (geçmiş dahil).
func rawDeploymentRows(t *testing.T, s *Store, appID string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM deployments WHERE app_id = ?`, appID).Scan(&n); err != nil {
		t.Fatalf("satırlar sayılamadı: %v", err)
	}
	return n
}

// TestRollbackTargetFollowsActivationHistoryNotReleaseOrder, geri alma
// hedefinin SÜRÜM SIRASINDAN değil AKTİVASYON GEÇMİŞİNDEN geldiğini
// doğrular.
//
// ── Bu test neden bu şekilde yazıldı? ───────────────────────────────
//
// Yalnızca r2→r1 sınayan bir test, "önceki sürüm = seq - 1" diye yazılmış
// YANLIŞ bir uygulamayla da GEÇER ve hiçbir şey kanıtlamaz. Ayırt edici
// senaryo şudur:
//
//	r1..r5 sırayla canlıya alınır      → önceki = r4
//	r3'e geri alınır (r5 canlıdayken)  → önceki = r5   ← BURASI
//
// Son satırda `seq - 1` mantığı r2 derdi. Gerçek cevap r5'tir, çünkü
// gerçekten canlı olan en son önceki sürüm odur. Sıra, geçmiş değildir.
func TestRollbackTargetFollowsActivationHistoryNotReleaseOrder(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	ids := activateSeries(t, s, "blog", 5)
	r3, r4, r5 := ids[2], ids[3], ids[4]

	// 1. Düz ilerleyişte önceki, bir öncekidir.
	prev, err := s.PreviousActiveRelease(ctx, "blog")
	if err != nil {
		t.Fatalf("önceki sürüm okunamadı: %v", err)
	}
	if prev != r4 {
		t.Fatalf("önceki sürüm %q, %q bekleniyordu", prev, r4)
	}

	// 2. r5 canlıyken r3'e geri al.
	if err := s.SetActiveRelease(ctx, "blog", r3); err != nil {
		t.Fatalf("r3'e geri alınamadı: %v", err)
	}

	// 3. AYIRT EDİCİ İDDİA: artık önceki r5'tir, r2 değil.
	prev, err = s.PreviousActiveRelease(ctx, "blog")
	if err != nil {
		t.Fatalf("önceki sürüm okunamadı: %v", err)
	}
	if prev != r5 {
		t.Fatalf("geri almadan sonra önceki sürüm %q — %q olmalıydı; "+
			"sürüm sırası aktivasyon geçmişinin yerine geçmiş", prev, r5)
	}

	// 4. Canlı gerçekten r3 olmalı; yoksa yukarıdaki iddia boşlukta kalır.
	live, err := s.ActiveDeployment(ctx, "blog")
	if err != nil {
		t.Fatalf("aktif dağıtım okunamadı: %v", err)
	}
	if live.ReleaseID != r3 {
		t.Errorf("canlı sürüm %q, %q bekleniyordu", live.ReleaseID, r3)
	}
}

// TestActiveQueriesIgnoreHistoryRows, geçmiş satırlarının canlı
// sorgulardan DIŞLANDIĞINI doğrular.
//
// ── Neden bu, sessiz ve geniş çaplı bir arıza ───────────────────────
//
// Ters vekil yapılandırması Caddy'ye `POST /load` ile KÖK nesne olarak
// gidiyor. `ActiveDeployments` geçmiş satırlarını da döndürseydi, her
// uygulama için birden çok upstream üretilirdi ve tek bir dağıtım TÜM
// siteleri aynı anda yanlış konteynerlere bağlardı — hata mesajı da
// olmadan.
//
// Test hem satırların GERÇEKTEN biriktiğini hem de filtrelendiğini
// ölçüyor: yalnızca "1 sonuç döndü" demek, geçmişin hiç yazılmadığı bir
// uygulamayla da geçerdi.
func TestActiveQueriesIgnoreHistoryRows(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	ids := activateSeries(t, s, "blog", 4)
	newest := ids[3]

	// Geçmiş gerçekten birikmiş olmalı.
	if got := rawDeploymentRows(t, s, "blog"); got != 4 {
		t.Fatalf("tabloda %d satır var, 4 bekleniyordu — geçmiş tutulmuyor", got)
	}

	all, err := s.ActiveDeployments(ctx)
	if err != nil {
		t.Fatalf("dağıtımlar okunamadı: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("%d aktif dağıtım döndü, 1 olmalı — geçmiş satırları "+
			"ters vekil yapılandırmasına sızıyor: %+v", len(all), all)
	}
	if all[0].ReleaseID != newest {
		t.Errorf("aktif sürüm %q, %q bekleniyordu", all[0].ReleaseID, newest)
	}

	one, err := s.ActiveDeployment(ctx, "blog")
	if err != nil {
		t.Fatalf("aktif dağıtım okunamadı: %v", err)
	}
	if one.ReleaseID != newest {
		t.Errorf("tekil sorgu %q döndü, %q bekleniyordu", one.ReleaseID, newest)
	}
}

// TestTwoActiveReleasesAreUnrepresentable, kısmi tekil indeksin "aynı anda
// iki aktif sürüm" durumunu ŞEMADA imkânsız kıldığını doğrular.
//
// Depo katmanı bu durumu zaten üretmiyor; test doğrudan SQL yazarak
// katmanı ATLIYOR. Sebep: korumanın Go kodunda değil veritabanında olması
// gerekiyor — ileride yazılacak ikinci bir yol (geri alma, uzlaştırıcı,
// elle müdahale) aynı kısıta çarpmalı.
func TestTwoActiveReleasesAreUnrepresentable(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	first := buildRelease(t, s, "blog", hexSHA(1))
	second := buildRelease(t, s, "blog", hexSHA(2))

	if err := s.SetActiveRelease(ctx, "blog", first.ID); err != nil {
		t.Fatalf("canlıya alınamadı: %v", err)
	}

	// Depo katmanını atlayarak İKİNCİ bir açık satır eklemeye çalış.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, release_id, activated_at)
		 VALUES (?, ?, ?)`, "blog", second.ID, 1)
	if err == nil {
		t.Fatal("ikinci bir AÇIK dağıtım satırı eklendi — " +
			"bir uygulama aynı anda iki sürüme trafik gönderebilir")
	}

	// Kapanmış satırlar indeksin dışında: geçmiş istendiği kadar birikmeli.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, release_id, activated_at, deactivated_at)
		 VALUES (?, ?, ?, ?)`, "blog", second.ID, 1, 2); err != nil {
		t.Fatalf("KAPANMIŞ satır reddedildi — indeks geçmişi de kilitliyor: %v", err)
	}
}

// TestDeploymentHistoryIsAppendOnly, geçmişin ezilemediğini doğrular.
//
// Gerekçe denetim zincirininkiyle aynı: geçmişi değiştirebilen bir tablo
// geçmişi yanıtlayamaz. Tek meşru değişiklik AÇIK bir satırın
// KAPATILMASIDIR.
func TestDeploymentHistoryIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	ids := activateSeries(t, s, "blog", 2)
	old := ids[0]

	cases := []struct {
		ad  string
		sql string
		arg any
	}{
		{"sürüm kimliği değiştirilemez",
			`UPDATE deployments SET release_id = ? WHERE app_id = 'blog'`, old},
		{"aktivasyon zamanı değiştirilemez",
			`UPDATE deployments SET activated_at = ? WHERE app_id = 'blog'`, 999},
		{"uygulama kimliği değiştirilemez",
			`UPDATE deployments SET app_id = ? WHERE app_id = 'blog'`, "shop"},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			if _, err := s.db.ExecContext(ctx, c.sql, c.arg); err == nil {
				t.Fatal("geçmiş değiştirildi")
			} else if !strings.Contains(err.Error(), "ekle-sadece") {
				t.Errorf("hata sebebi açık değil: %v", err)
			}
		})
	}

	// KAPANMIŞ bir satır bir daha açılamaz.
	t.Run("kapanmış satır yeniden açılamaz", func(t *testing.T) {
		_, err := s.db.ExecContext(ctx,
			`UPDATE deployments SET deactivated_at = NULL
			 WHERE app_id = 'blog' AND deactivated_at IS NOT NULL`)
		if err == nil {
			t.Fatal("kapanmış dağıtım yeniden açıldı")
		}
	})

	// Kontrol grubu: MEŞRU kapatma HÂLÂ çalışmalı. Yoksa test yalnızca
	// "her UPDATE reddediliyor" der ve tetikleyicinin ayırt ettiğini
	// kanıtlamaz — bu durumda geri alma da imkânsız olurdu.
	t.Run("açık satır kapatılabilir", func(t *testing.T) {
		if err := s.ClearActiveRelease(ctx, "blog"); err != nil {
			t.Fatalf("açık dağıtım kapatılamadı: %v", err)
		}
		if _, err := s.ActiveDeployment(ctx, "blog"); !errors.Is(err, ErrNoDeployment) {
			t.Errorf("kapatmadan sonra %v döndü, ErrNoDeployment bekleniyordu", err)
		}
	})
}

// TestClearActiveReleaseKeepsHistory, trafikten çekmenin geçmişi
// SİLMEDİĞİNİ doğrular.
//
// Eski uygulama `DELETE` ediyordu. Ekle-sadece bir tabloda bu, geri
// almanın dayandığı cevabı yok ederdi: trafikten çekilmiş bir uygulamayı
// tekrar canlıya almak, en son hangi sürümün çalıştığını bilmeyi
// gerektirir.
func TestClearActiveReleaseKeepsHistory(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	ids := activateSeries(t, s, "blog", 2)
	newest := ids[1]

	if err := s.ClearActiveRelease(ctx, "blog"); err != nil {
		t.Fatalf("trafikten çekilemedi: %v", err)
	}

	if got := rawDeploymentRows(t, s, "blog"); got != 2 {
		t.Fatalf("tabloda %d satır kaldı, 2 bekleniyordu — geçmiş silindi", got)
	}

	// Çekilen sürüm, geri almanın hedefi olmalı.
	prev, err := s.PreviousActiveRelease(ctx, "blog")
	if err != nil {
		t.Fatalf("önceki sürüm okunamadı: %v", err)
	}
	if prev != newest {
		t.Errorf("önceki sürüm %q, %q bekleniyordu", prev, newest)
	}
}

// TestReactivatingSameReleaseIsNoOp, canlı olan sürümün yeniden
// aktifleştirilmesinin geçmişe SAHTE bir giriş eklemediğini doğrular.
//
// Ekleseydi, geri alma "önceki sürüm" diye AYNI sürümü bulurdu ve komut
// hiçbir şey yapmadan başarılı görünürdü — yani bu bir verimlilik
// ayrıntısı değil, doğruluk şartı.
func TestReactivatingSameReleaseIsNoOp(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}
	ids := activateSeries(t, s, "blog", 2)
	first, second := ids[0], ids[1]

	for i := 0; i < 3; i++ {
		if err := s.SetActiveRelease(ctx, "blog", second); err != nil {
			t.Fatalf("yeniden aktifleştirme hata verdi: %v", err)
		}
	}

	if got := rawDeploymentRows(t, s, "blog"); got != 2 {
		t.Fatalf("tabloda %d satır var, 2 bekleniyordu — "+
			"aynı sürümün yeniden aktifleştirilmesi geçmişi şişiriyor", got)
	}

	prev, err := s.PreviousActiveRelease(ctx, "blog")
	if err != nil {
		t.Fatalf("önceki sürüm okunamadı: %v", err)
	}
	if prev != first {
		t.Errorf("önceki sürüm %q, %q bekleniyordu — geri alma kendini "+
			"hedef gösteriyor", prev, first)
	}
}

// TestPreviousActiveReleaseDistinguishesFirstDeployment, "hiç
// dağıtılmamış" ile "geri alınacak öncesi yok" durumlarının AYRI
// raporlandığını doğrular.
//
// Kullanıcıya söylenecek şey farklı: birincisinde "önce dağıt", ikincisinde
// "dönecek bir sürüm yok".
func TestPreviousActiveReleaseDistinguishesFirstDeployment(t *testing.T) {
	ctx := context.Background()
	s := newAppStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama oluşturulamadı: %v", err)
	}

	// Hiç dağıtım yok.
	if _, err := s.PreviousActiveRelease(ctx, "blog"); !errors.Is(err, ErrNoPreviousDeployment) {
		t.Fatalf("dağıtılmamış uygulama için %v döndü", err)
	}

	// İlk dağıtımdan sonra da geri alınacak bir şey yok.
	activateSeries(t, s, "blog", 1)
	if _, err := s.PreviousActiveRelease(ctx, "blog"); !errors.Is(err, ErrNoPreviousDeployment) {
		t.Fatalf("ilk dağıtımdan sonra %v döndü, ErrNoPreviousDeployment bekleniyordu", err)
	}

	// İkinci dağıtımdan sonra VAR — kontrol grubu.
	activateSeries(t, s, "blog", 1)
	if _, err := s.PreviousActiveRelease(ctx, "blog"); err != nil {
		t.Fatalf("ikinci dağıtımdan sonra önceki sürüm bulunamadı: %v", err)
	}
}
