package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"

	"github.com/erkanrzgc/panely/internal/deploy"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// Rollback, trafiği bir önceki aktif sürüme geri çevirir.
//
// ── Akış ────────────────────────────────────────────────────────────
//
//	canlı sürümü oku → geçmişten hedefi bul → hedef sürüm satırını oku →
//	replikaları çalışır hâle getir → KAPI → aktif sürümü yaz → uzlaştır →
//	boşaltma → eskiyi durdur
//
// Son beş adım dağıtımla AYNI koddan geçiyor (deploy.switchTraffic); sıra
// orada gerekçelendirilmiş ve burada tekrarlanmıyor.
//
// ── Hiçbir şey DERLENMİYOR ──────────────────────────────────────────
//
// Hedef sürümün imajı zaten var ve konteynerleri çoğu zaman durmuş hâlde
// bekliyor (K-061: boşaltma durdurur, silmez). Bu yüzden geri alma
// saniyeler sürer — Faz 1'in 5. kabul ölçütünün istediği de budur.
// Konteyner kaybolmuşsa imajdan kurulur ve yanıt bunu `recreated` ile
// söyler.
//
// ── Kapı geri almada da işliyor ─────────────────────────────────────
//
// "Bu sürüm daha önce çalışıyordu" bir sağlık kanıtı değildir. Kapıyı
// atlamak, geri almayı siteyi kurtaran değil ikinci kez düşüren işleme
// çevirirdi.
func (s *Server) Rollback(
	ctx context.Context, req *panelyv1.RollbackRequest,
) (*panelyv1.RollbackResponse, error) {
	const action = "app.rollback"

	appID := req.GetAppId()
	tgt := appTarget(appID)
	params := map[string]string{}

	app, err := s.store.GetApp(ctx, appID)
	if err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "uygulama bulunamadı")
		return nil, appError(err)
	}

	// Çağrıdan ÖNCE canlı olan sürüm. Yanıtta dönüyor çünkü operatörün
	// doğrulaması gereken şey "komut hata vermedi" değil, trafiğin nereden
	// nereye taşındığı.
	live, err := s.store.ActiveDeployment(ctx, appID)
	if err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "aktif sürüm yok")
		return nil, appError(err)
	}
	params["from_release_id"] = live.ReleaseID

	targetID, err := s.store.PreviousActiveRelease(ctx, appID)
	if err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure,
			"geri alınacak önceki sürüm yok")
		return nil, appError(err)
	}
	params["to_release_id"] = targetID
	tgt = releaseTarget(appID, targetID)

	// Sürüm satırı gerekiyor: konteyner kaybolmuşsa imajdan kurmak için
	// commit sha lazım. Ayrıca bu okuma, geçmişin gösterdiği sürümün
	// GERÇEKTEN var olduğunu doğruluyor.
	rel, err := s.store.GetRelease(ctx, appID, targetID)
	if err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "hedef sürüm okunamadı")
		return nil, appError(err)
	}

	recreated, err := s.rollout.Rollback(ctx, app, rel)
	params["recreated"] = strconv.FormatBool(recreated)
	if err != nil {
		var skipped deploy.SkippedError
		if !errors.As(err, &skipped) {
			return nil, s.completed(ctx, action, tgt, params, err)
		}
		// Bu sürüm canlıya ÇIKTI; rotalanamayan BAŞKA uygulamalar var.
		// Geri almayı başarısız saymak yanlış olurdu ama sessiz kalmak da:
		// operatörün bunu görmesi gerekiyor.
		slog.Warn("geri alma tamamlandı ama bazı uygulamalar rotalanamadı",
			"uygulama", app.ID, "surum", rel.ID, "ayrinti", skipped.Result.Error())
		params["skipped_routes"] = skipped.Result.Error()
	}

	if err := s.completed(ctx, action, tgt, params, nil); err != nil {
		return nil, err
	}
	return &panelyv1.RollbackResponse{
		AppId:         appID,
		FromReleaseId: live.ReleaseID,
		ToReleaseId:   rel.ID,
		Recreated:     recreated,
	}, nil
}
