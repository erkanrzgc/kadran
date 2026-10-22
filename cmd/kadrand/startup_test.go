package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/deploy"
)

// fakeRec, uzlaştırmanın sonucunu senaryoya göre üretir.
//
// Deneme SAYISINI de kaydediyor: "yeniden deniyor" iddiası ancak çağrı
// sayısıyla ölçülebilir, dönen değerle değil.
type fakeRec struct {
	calls            int
	failFor          int // ilk kaç denemede hata dönsün
	skipped          map[string]string
	skippedRedirects map[string]string
	failWith         error
}

func (f *fakeRec) Reconcile(context.Context) (deploy.Result, error) {
	f.calls++
	if f.calls <= f.failFor {
		return deploy.Result{}, f.failWith
	}
	return deploy.Result{Routed: []string{"portfolio"}, Skipped: f.skipped, SkippedRedirects: f.skippedRedirects}, nil
}

// gunlukYakala, testin süresince varsayılan slog'u bir tampona yönlendirir.
func gunlukYakala(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	onceki := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(onceki) })
	return &buf
}

// TestStartupReconcileLogsSkippedRedirects: açılışta atlanan yönlendirme adı
// günlüğe yazılır ama açılışı kirletmez (domain rotaları yerinde).
func TestStartupReconcileLogsSkippedRedirects(t *testing.T) {
	buf := gunlukYakala(t)
	rec := &fakeRec{skippedRedirects: map[string]string{"www.example.com": `already redirects to "example.org"`}}

	if got := reconcileAtStartup(rec); got != "" {
		t.Errorf("atlanan yönlendirme açılışı sorunlu saydı: %q", got)
	}
	if want := "name=www.example.com"; !strings.Contains(buf.String(), want) {
		t.Errorf("günlükte %q yok:\n%s", want, buf)
	}
}

// TestStartupReconcileReportsFailure, kalıcı başarısızlığın ÇAĞIRANA
// bildirildiğini doğrular.
//
// ── Neden önemli ────────────────────────────────────────────────────
//
// Sonuç yalnızca günlüğe yazılıp koşulsuz READY gönderildiğinde systemd
// "active (running)" gösterir, `kadran status` sağlıklı der ve BÜTÜN
// SİTELER KAPALIDIR. Tek iz bir slog.Error satırıdır ve kimse ona bakmaz.
func TestStartupReconcileReportsFailure(t *testing.T) {
	rec := &fakeRec{
		failFor:  startupReconcileTries,
		failWith: errors.New("caddy admin soketi cevap vermiyor"),
	}

	got := reconcileAtStartup(rec)

	if got == "" {
		t.Fatal("kalıcı başarısızlık boş dönüyor — çağıran READY'yi " +
			"koşulsuz gönderir ve systemd yalan söyler")
	}
	if !strings.Contains(got, "caddy") {
		t.Errorf("sebep taşınmıyor: %q", got)
	}
	if rec.calls != startupReconcileTries {
		t.Errorf("%d deneme yapıldı, %d bekleniyordu", rec.calls, startupReconcileTries)
	}
}

// TestStartupReconcileRecoversFromTransientFailure, geçici hatanın
// yutulduğunu doğrular.
//
// En olası başarısızlık sebebi Caddy'nin admin soketini henüz açmamış
// olması ve bu saniyeler içinde kendiliğinden geçiyor. Tek denemede
// vazgeçmek, geçici bir yarışı kalıcı bir kesintiye çevirirdi.
func TestStartupReconcileRecoversFromTransientFailure(t *testing.T) {
	rec := &fakeRec{failFor: 1, failWith: errors.New("bağlantı reddedildi")}

	if got := reconcileAtStartup(rec); got != "" {
		t.Errorf("geçici hata kalıcı sayıldı: %q", got)
	}
	if rec.calls != 2 {
		t.Errorf("%d deneme yapıldı, 2 bekleniyordu — yeniden deneme yok", rec.calls)
	}
}

// TestStartupReconcileReportsSkippedApps, "uzlaştırma başarılı ama
// uygulama rotalanamadı" hâlini de sorun sayar.
//
// Bu hâl daha sinsi: hata YOK, yani her şey yolunda görünür — ama o
// uygulamanın sitesi kapalıdır.
func TestStartupReconcileReportsSkippedApps(t *testing.T) {
	rec := &fakeRec{skipped: map[string]string{"blog": "ayakta replikası yok"}}

	got := reconcileAtStartup(rec)
	if got == "" {
		t.Fatal("rotalanamayan uygulama sessiz geçti — sitesi kapalı ama " +
			"systemd sağlıklı gösterir")
	}
	if !strings.Contains(got, "blog") {
		t.Errorf("hangi uygulamanın rotalanamadığı söylenmiyor: %q", got)
	}
}

// TestStartupReconcileStaysQuietWhenHealthy, kontrol grubu: sağlıklı
// açılışta sorun UYDURULMADIĞINI doğrular.
//
// Bu test olmasaydı "her zaman sorun bildir" diyen bir uygulama da
// yukarıdaki üçünü geçerdi.
func TestStartupReconcileStaysQuietWhenHealthy(t *testing.T) {
	rec := &fakeRec{}

	if got := reconcileAtStartup(rec); got != "" {
		t.Errorf("sağlıklı açılışta sorun bildirildi: %q", got)
	}
	if rec.calls != 1 {
		t.Errorf("%d deneme yapıldı, 1 yeterliydi", rec.calls)
	}
}
