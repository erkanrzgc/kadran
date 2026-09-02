package api

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// deleteGrace, silmeden önce konteynere SIGTERM ile SIGKILL arasında
// verilen süredir.
//
// Boşaltma penceresinden (deploy.DefaultDrain.Grace) kısa tutulabilirdi
// ama tutulmadı: silinen uygulamanın da düzgün kapanma hakkı var ve
// aradaki fark yalnızca operatörün beklediği birkaç saniye.
const deleteGrace = 10 * time.Second

// DeleteApp, uygulamayı ve izlerini kaldırır — canlı sürümü yoksa.
//
// ── Sıra ────────────────────────────────────────────────────────────
//
//	canlılık kontrolü → konteynerleri durdur+sil → KAYITLARI sil
//
// Kayıtlar EN SONA bırakılıyor ve bu taşıyıcı bir karar: konteyner
// adları `app_id`/`release_id`'den türüyor, yani veritabanı satırı o
// konteynerlere ulaşmanın TEK yolu. Önce satırı silseydik ve konteyner
// kaldırma yarıda kalsaydı, geriye kimsenin adını bilemediği çalışan
// konteynerler kalırdı.
//
// Bu yüzden konteyner kaldırma başarısız olursa kayıtlar SİLİNMİYOR ve
// hata dönüyor: komut yeniden çalıştırılabilir, ikinci deneme daha az
// konteyner bulur ve tamamlar. Her adım "zaten yok"a dayanıklı —
// `StopRelease` ve `RemoveRelease` sıfır etkilenende hata döndürmüyor,
// sayı döndürüyor.
//
// ── Uzlaştırma neden YOK ────────────────────────────────────────────
//
// Ters vekil yapılandırması `ActiveDeployments`'tan üretiliyor. Canlı
// sürümü olmayan bir uygulama orada zaten görünmüyor, yani silmeden
// önce de sonra da yapılandırmada adı geçmiyor. Uzlaştırmak gereksiz
// bir yazma olurdu — ve gereksiz değil, RİSKLİ: `Reconcile` bütün
// yapılandırmayı sıfırdan kurup yüklüyor, yani sağlıklı uygulamaları da
// yeniden yazıyor.
//
// ── Gözetmen neden karışmıyor ───────────────────────────────────────
//
// health.Supervisor yalnızca `ActiveDeployments`'ı izliyor. Canlı sürümü
// olan bir uygulamayı silmeye çalışsaydık, konteynerleri durdurduğumuz
// anda gözetmen onları ~6 saniyede geri getirir ve bizimle yarışırdı.
// Canlılık kontrolü o yarışı BAŞLAMADAN bitiriyor.
func (s *Server) DeleteApp(
	ctx context.Context, req *panelyv1.DeleteAppRequest,
) (*panelyv1.DeleteAppResponse, error) {
	const action = "app.delete"

	appID := req.GetAppId()
	tgt := appTarget(appID)
	params := map[string]string{}

	if _, err := s.store.GetApp(ctx, appID); err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "uygulama bulunamadı")
		return nil, appError(err)
	}

	// Canlılık BURADA da bakılıyor, deponun içinde de. Buradaki erken
	// kontrol operatöre konteynerlere dokunulmadan net bir hata veriyor;
	// depodaki kontrol ise değişmezi işlemin içinde koruyor (araya bir
	// dağıtım girerse). İkisi aynı şeyi iki farklı sebeple yapıyor.
	if live, err := s.store.ActiveDeployment(ctx, appID); err == nil {
		params["live_release_id"] = live.ReleaseID
		cause := fmt.Errorf(
			"uygulamanın canlı sürümü var (%s) — silmek trafiği keserdi. "+
				"Canlı uygulamaların silinmesi TOTP kapısıyla birlikte gelecek",
			live.ReleaseID)
		// ⚠ `s.denied()` KULLANILMIYOR: o her zaman InvalidArgument
		// döndürüyor ve burası bir argüman hatası DEĞİL. İstek kusursuz;
		// izin vermeyen şey sistemin DURUMU. Aynı ayrım `appError`'da
		// ErrNoDeployment için de yapılıyor.
		//
		// Fark pratik: InvalidArgument gören bir istemci isteği düzeltmeye
		// çalışır, FailedPrecondition gören ise önce durumu değiştirmesi
		// gerektiğini bilir — ki burada yapılacak şey tam olarak budur.
		_ = s.recordAction(ctx, action, tgt, params, audit.OutcomeDenied, cause.Error())
		return nil, status.Error(codes.FailedPrecondition, cause.Error())
	}

	removed, err := s.removeContainers(ctx, appID)
	params["containers_removed"] = strconv.FormatUint(uint64(removed), 10)
	if err != nil {
		// Kayıtlara DOKUNULMADI: komut yeniden çalıştırılabilir.
		return nil, s.completed(ctx, action, tgt, params, err)
	}

	counts, err := s.store.DeleteApp(ctx, appID)
	if err != nil {
		return nil, s.completed(ctx, action, tgt, params, err)
	}
	params["releases_deleted"] = strconv.FormatUint(uint64(counts.Releases), 10)
	params["deployments_deleted"] = strconv.FormatUint(uint64(counts.Deployments), 10)

	if err := s.completed(ctx, action, tgt, params, nil); err != nil {
		return nil, err
	}
	return &panelyv1.DeleteAppResponse{
		AppId:              appID,
		ContainersRemoved:  removed,
		ReleasesDeleted:    counts.Releases,
		DeploymentsDeleted: counts.Deployments,
	}, nil
}

// removeContainers, uygulamanın BÜTÜN konteynerlerini durdurup siler.
//
// Sürümler kontrol düzleminden değil, HOST'tan okunuyor: gerçek olan
// orada duran konteynerler. Kayıtlarda olmayan bir konteyner (elle
// yaratılmış, göç sırasında kalmış) de böylece temizleniyor.
func (s *Server) removeContainers(ctx context.Context, appID string) (uint32, error) {
	reps, err := s.exec.ListReplicas(ctx, appID)
	if err != nil {
		return 0, fmt.Errorf("konteynerler listelenemedi: %w", err)
	}

	seen := map[string]struct{}{}
	releases := make([]string, 0, len(reps))
	for _, r := range reps {
		if _, ok := seen[r.ReleaseID]; ok {
			continue
		}
		seen[r.ReleaseID] = struct{}{}
		releases = append(releases, r.ReleaseID)
	}
	// Sıra belirleyici olsun: aynı girdi aynı denetim kaydını üretmeli.
	sort.Strings(releases)

	var removed uint32
	for _, relID := range releases {
		if _, err := s.exec.StopRelease(ctx, appID, relID, deleteGrace); err != nil {
			return removed, fmt.Errorf("sürüm %s durdurulamadı: %w", relID, err)
		}
		n, err := s.exec.RemoveRelease(ctx, appID, relID)
		if err != nil {
			return removed, fmt.Errorf("sürüm %s kaldırılamadı: %w", relID, err)
		}
		removed += n
	}
	return removed, nil
}
