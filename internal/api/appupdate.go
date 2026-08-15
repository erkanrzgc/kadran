package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// UpdateApp, var olan bir uygulamanın değiştirilebilir alanlarını yazar.
//
// ── Sıra yük taşıyor ────────────────────────────────────────────────
//
//	oku → birleştir → DOĞRULA → yaz → (alan adı değiştiyse) uzlaştır
//
// Doğrulama BİRLEŞTİRİLMİŞ tanıma yapılıyor, isteğin kendisine değil.
// Deltayı tek başına doğrulamak yetmez: tek başına makul görünen bir
// değişiklik mevcut durumla birleşince geçersiz bir tanım üretebilir.
//
// Uzlaştırma EN SONA bırakıldı ve yazmadan önce yapılamaz: ters vekil
// yapılandırması `apps` tablosundan üretiliyor, yani yazılmamış bir alan
// adı ortaya çıkamaz. Bu, SetActiveRelease'in belgelediği sıranın
// aynısı — önce kontrol düzlemindeki gerçek, sonra ona uyum.
func (s *Server) UpdateApp(
	ctx context.Context, req *panelyv1.UpdateAppRequest,
) (*panelyv1.UpdateAppResponse, error) {
	const action = "app.update"

	appID := req.GetAppId()
	tgt := appTarget(appID)
	params := updateAuditParams(req)
	upd := updateFromProto(req)

	if upd.IsEmpty() {
		return nil, s.denied(ctx, action, tgt, params, errors.New(
			"hiçbir alan belirtilmedi — bu çağrı yalnızca zaman damgasını "+
				"ilerletirdi, yani sessiz bir işlemsizlik olurdu"))
	}

	current, err := s.store.GetApp(ctx, appID)
	if err != nil {
		// Var olmayan bir uygulamaya yazma DENEMESİ de zincire girer:
		// yalnızca başarılı yazmaları kaydeden bir denetim günlüğü,
		// "kim neyi denedi" sorusunu yanıtlayamaz.
		_ = s.recordAction(ctx, action, tgt, params, audit.OutcomeDenied, err.Error())
		return nil, appError(err)
	}

	if err := validateAppSpec(appToProto(upd.Apply(current)).GetSpec()); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}

	// Alan adının GERÇEKTEN değişip değişmediği YAZMADAN ÖNCE saptanmalı:
	// yazdıktan sonra eski değer artık okunamaz.
	domainMoved := upd.ChangesDomain(current.Domain)

	app, opErr := s.store.UpdateApp(ctx, appID, upd)
	if err := s.completed(ctx, action, tgt, params, opErr); err != nil {
		return nil, appError(err)
	}

	resp := &panelyv1.UpdateAppResponse{App: appToProto(app)}
	if domainMoved {
		detail, err := s.moveTraffic(ctx, appID, current.Domain, app.Domain)
		if err != nil {
			return nil, err
		}
		resp.ProxyDetail = detail
	}
	return resp, nil
}

// moveTraffic, alan adı değişikliğini ters vekile yansıtır.
//
// ── Neden dağıtım beklemiyoruz? ─────────────────────────────────────
//
// Uzlaştırma yalnızca İKİ yerden çağrılıyordu: panelyd açılışı ve
// dağıtım. Yani `app update -domain` tek başına trafiği taşımazdı; alan
// adı veritabanında değişir, canlıda hiçbir şey olmazdı ve kullanıcı
// komut "başarılı" dediği için taşındığını sanırdı. Bu işin var olma
// sebebi apex'i dağıtımsız taşımak, dolayısıyla uzlaştırma buraya ait.
//
// ── Üç sonuç, üç ayrı cevap ─────────────────────────────────────────
//
// Hepsini "tamam" diye raporlamak, en tehlikeli ikisini gizlerdi.
func (s *Server) moveTraffic(ctx context.Context, appID, from, to string) (string, error) {
	res, err := s.reconciler.Reconcile(ctx)
	if err != nil {
		// Değişiklik YAZILDI. Bunu söylemeyen bir hata, kullanıcıyı
		// komutun hiç etki etmediğini sanıp başka bir alan adıyla
		// yeniden denemeye iterdi — oysa kayıt zaten yeni değerde.
		return "", status.Errorf(codes.Unavailable,
			"alan adı %q → %q olarak KAYDEDİLDİ, ama ters vekil "+
				"güncellenemedi: trafik hâlâ eski rotada. Uzlaştırma bir "+
				"sonraki dağıtımda veya panelyd yeniden başlatıldığında "+
				"tekrar denenir. Sebep: %v", from, to, err)
	}

	if to == "" {
		return "uygulama ters vekilden ÇIKARILDI — artık hiçbir alan adında yayınlanmıyor", nil
	}
	if why, skipped := res.Skipped[appID]; skipped {
		// Hata DEĞİL: hiç dağıtılmamış bir uygulamanın ayakta replikası
		// olmaz ve rota üretilemez. Ama sessiz kalmak, kullanıcının yeni
		// alan adının canlıda cevap verdiğini sanması demekti.
		return fmt.Sprintf(
			"⚠ alan adı kaydedildi ama TRAFİK TAŞINMADI (%s) — "+
				"uygulamayı dağıtın: panely deploy %s", why, appID), nil
	}
	return fmt.Sprintf("ters vekil güncellendi — %q artık bu uygulamaya gidiyor", to), nil
}

// updateFromProto, isteği depo katmanının tipine çevirir.
//
// İşaretçiler KOPYALANIYOR, proto'nunkiler paylaşılmıyor: istek nesnesi
// çağrı bittikten sonra gRPC tarafından yeniden kullanılabilir.
func updateFromProto(req *panelyv1.UpdateAppRequest) store.AppUpdate {
	var upd store.AppUpdate
	if req.Domain != nil {
		v := req.GetDomain()
		upd.Domain = &v
	}
	if req.GitBranch != nil {
		v := req.GetGitBranch()
		upd.GitBranch = &v
	}
	if req.HealthPath != nil {
		v := req.GetHealthPath()
		upd.HealthPath = &v
	}
	if req.Replicas != nil {
		v := req.GetReplicas()
		upd.Replicas = &v
	}
	return upd
}

// updateAuditParams, denetime yalnızca BELİRTİLEN alanları yazar.
//
// Belirtilmeyen alanı boş değeriyle yazmak, kaydı okuyan birine
// "health_path boşaltıldı" dedirtirdi — oysa ona hiç dokunulmadı.
// Zincir ekle-sadece: bir kez yazılan yanlış bilgi düzeltilemez.
func updateAuditParams(req *panelyv1.UpdateAppRequest) map[string]string {
	params := map[string]string{}
	if req.Domain != nil {
		params["domain"] = req.GetDomain()
	}
	if req.GitBranch != nil {
		params["branch"] = req.GetGitBranch()
	}
	if req.HealthPath != nil {
		params["health_path"] = req.GetHealthPath()
	}
	if req.Replicas != nil {
		params["replicas"] = strconv.FormatUint(uint64(req.GetReplicas()), 10)
	}
	return params
}
