package api

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc"

	"github.com/erkanrzgc/panely/internal/deploy"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// maxReleaseDetail, sürüm satırına yazılacak hata metninin üst sınırı.
//
// Derleyici hatası kullanıcının deposundan gelir ve uzunluğu sınırsızdır;
// kontrol veritabanı bir günlük deposu değildir.
const maxReleaseDetail = 4 << 10

// sealTimeout, sürüm mühürlemesi için üst sınır.
//
// Mühürleme çağıranın bağlamından KOPARILIYOR (aşağıya bakın), yani
// oradan bir iptal gelmez. Sınırsız bırakmak, asılı kalmış bir
// veritabanında dağıtım gorutinini sonsuza kadar tutardı.
const sealTimeout = 10 * time.Second

// Deploy, bir commit'i derler, ayağa kaldırır ve trafiği ona çevirir.
//
// ── Akışın tamamı ───────────────────────────────────────────────────
//
//	derle → sürümü mühürle → konteynerleri başlat → KAPI →
//	aktif sürümü yaz → ters vekili uzlaştır
//
// Kapı İKİ şeyi birden ölçüyor: konteynerin çalışıp adreslenebilir
// olmasını (Lifecycle) ve uygulamanın sağlık yoluna cevap vermesini
// (HTTP yoklaması). İkincisi olmadan açılan ama 500 dönen bir uygulama
// kapıdan geçerdi. Ayrıntı deploy.awaitReady'de.
//
// Kapıdan sonra boşaltma penceresi beklenir ve ESKİ sürümlerin
// konteynerleri durdurulur — ama silinmez, geri alma hızlı olsun diye.
//
// Geri almanın dayandığı geçmiş burada birikiyor: `SetActiveRelease` açık
// dağıtım satırını kapatıp yenisini ekliyor (göç 0005), yani "önceki AKTİF
// sürüm" sorusunun cevabı her dağıtımda şemaya yazılıyor.
//
// ── Başarı ölçütü akışın ŞEKLİNDE ──────────────────────────────────
//
// Akış şöyle biter:
//
//	başarı → son mesaj DeploySucceeded (imaj kimliğiyle)
//	hata   → gRPC hata durumu, DeploySucceeded YOK
//
// Bu, K-042'nin bu katmandaki karşılığıdır. Docker'ın klasik derleyicisi
// derleme ORTASINDA ölen bir yapı için de HTTP 200 döner; "hata karesi
// görmedim" bir başarı kanıtı değildir. İstemci de aynı ölçütü
// kullanabilir: DeploySucceeded görmeden biten akış BAŞARISIZDIR.
func (s *Server) Deploy(
	req *panelyv1.DeployRequest,
	stream grpc.ServerStreamingServer[panelyv1.DeployResponse],
) error {
	const action = "app.deploy"

	ctx := stream.Context()
	appID := req.GetAppId()
	tgt := appTarget(appID)
	params := map[string]string{"commit_sha": req.GetCommitSha()}

	if err := validateCommitSHA(req.GetCommitSha()); err != nil {
		return s.denied(ctx, action, tgt, params, err)
	}

	app, err := s.store.GetApp(ctx, appID)
	if err != nil {
		// Bilinmeyen uygulama bir doğrulama reddi DEĞİL, bir arama
		// başarısızlığıdır: denied() InvalidArgument döndürürdü ve istemci
		// "yazım hatası yaptım" ile "bu uygulama yok"u ayırt edemezdi.
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "uygulama bulunamadı")
		return appError(err)
	}
	params["source"] = app.GitHost + "/" + app.GitOwner + "/" + app.GitRepo

	// Sürüm kaydı derleme BAŞLAMADAN açılır: istemci düşse bile sürüm
	// veritabanında adreslenebilir kalmalı ve hostta oluşabilecek bir
	// imajın karşılığı bulunmalı.
	rel, err := s.store.StartRelease(ctx, appID, req.GetCommitSha())
	if err != nil {
		_ = s.recordAction(ctx, action, tgt, params, auditFailure, "sürüm açılamadı")
		return appError(err)
	}
	params["release_id"] = rel.ID
	tgt = releaseTarget(appID, rel.ID)

	if err := stream.Send(&panelyv1.DeployResponse{
		Event: &panelyv1.DeployResponse_Accepted{Accepted: &panelyv1.DeployAccepted{
			ReleaseId: rel.ID,
			AppId:     appID,
			CommitSha: req.GetCommitSha(),
		}},
	}); err != nil {
		s.sealFailed(ctx, rel, "istemciye sürüm bildirilemedi")
		return s.completed(ctx, action, tgt, params, err)
	}

	imageID, buildErr := s.exec.ImageBuild(ctx, buildRequest(app, rel, req.GetCommitSha()),
		func(data []byte, isStderr bool) error {
			return stream.Send(&panelyv1.DeployResponse{
				Event: &panelyv1.DeployResponse_Output{Output: &panelyv1.BuildOutput{
					Data:     data,
					IsStderr: isStderr,
				}},
			})
		})

	if buildErr != nil {
		// ⚠ Hata METNİ sürüm satırına yazılır ama denetim zincirine
		// YAZILMAZ. Ayrım kasıtlı ve önemli: zincir ekle-sadece'dir, bir
		// kez yazılan bir sır geri alınamaz. `releases.detail` ise
		// değiştirilebilir ve kullanıcının kendi derleme çıktısıdır —
		// hata sebebini orada görmesi gerekir.
		s.sealFailed(ctx, rel, buildErr.Error())
		return s.completed(ctx, action, tgt, params, buildErr)
	}

	params["image_id"] = imageID
	if err := s.sealBuilt(ctx, rel, imageID); err != nil {
		return s.completed(ctx, action, tgt, params, err)
	}

	// ── Trafik buradan sonra taşınıyor ──────────────────────────────
	//
	// Sürüm BUILT olarak mühürlendi ve öyle KALIYOR: imaj gerçekten
	// üretildi ve bu, dağıtımın devamı başarısız olsa bile doğru.
	// Başarısızlık hâlinde değişmeyen şey TRAFİK — eski sürüm canlıda
	// kalır ve istemci hata alır.
	rel.CommitSHA = req.GetCommitSha()
	if err := s.rollout.Run(ctx, app, rel); err != nil {
		var skipped deploy.SkippedError
		if !errors.As(err, &skipped) {
			return s.completed(ctx, action, tgt, params, err)
		}
		// Bu sürüm canlıya ÇIKTI; rotalanamayan BAŞKA uygulamalar var.
		// Dağıtımı başarısız saymak yanlış olurdu ama sessiz kalmak da:
		// operatörün bunu görmesi gerekiyor.
		slog.Warn("dağıtım tamamlandı ama bazı uygulamalar rotalanamadı",
			"uygulama", app.ID, "surum", rel.ID, "ayrinti", skipped.Result.Error())
		params["skipped_routes"] = skipped.Result.Error()
	}

	if err := stream.Send(&panelyv1.DeployResponse{
		Event: &panelyv1.DeployResponse_Succeeded{Succeeded: &panelyv1.DeploySucceeded{
			ReleaseId: rel.ID,
			ImageId:   imageID,
		}},
	}); err != nil {
		// İmaj gerçekten üretildi ve sürüm BUILT: bunu geri almıyoruz.
		// Yalnızca istemciye ulaştıramadık ve denetim bunu böyle yazar.
		return s.completed(ctx, action, tgt, params, err)
	}
	return s.completed(ctx, action, tgt, params, nil)
}

// buildRequest, uygulama tanımı ve çözülmüş sha'dan executor isteğini
// KURAR.
//
// GitSource'un parçaları tek tek veriliyor; bir URL alınmıyor. Bağlam
// URL'ini executor kendisi kuruyor (exec.BuildContextURL) ve şema sabit
// https, fragment de doğrulanmış 40 haneli bir sha olduğu için subdir
// bileşeni oluşturulamıyor.
func buildRequest(app store.App, rel store.Release, sha string) *panelyv1.ImageBuildRequest {
	return &panelyv1.ImageBuildRequest{
		Release: &panelyv1.ReleaseRef{
			AppId:     app.ID,
			ReleaseId: rel.ID,
		},
		Source: &panelyv1.GitSource{
			Host:      app.GitHost,
			Owner:     app.GitOwner,
			Repo:      app.GitRepo,
			CommitSha: sha,
		},
		DockerfilePath: app.DockerfilePath,
		BuildArgs:      app.BuildArgs,
	}
}

// sealFailed ve sealBuilt, sürümü kapatır.
//
// ── Bağlam KASTEN koparılıyor ──────────────────────────────────────
//
// En sık başarısızlık sebebi istemcinin kopmasıdır ve o anda stream'in
// bağlamı ZATEN İPTAL OLMUŞTUR. Mühürlemeyi o bağlamla yapmak, tam da
// mühürlemenin en gerekli olduğu durumda çalışmamak demekti: sürüm
// sonsuza kadar BUILDING'de asılı kalırdı.
//
// context.WithoutCancel iptali keser; WithTimeout sınırı geri koyar.
func (s *Server) sealFailed(ctx context.Context, rel store.Release, detail string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sealTimeout)
	defer cancel()

	if len(detail) > maxReleaseDetail {
		detail = detail[:maxReleaseDetail] + "… (kısaltıldı)"
	}
	if err := s.store.FailRelease(ctx, rel.AppID, rel.ID, detail); err != nil {
		// Yutulmuyor, günlüğe yazılıyor. Çağıran zaten bir hata döndürecek
		// (derleme başarısız oldu) ve onun yerine mühürleme hatasını
		// döndürmek asıl sebebi gizlerdi. Ama sessiz kalmak da olmaz:
		// sürüm BUILDING'de asılı kalmış demektir ve operatörün bunu
		// görmesi gerekir.
		slog.Error("sürüm başarısız olarak mühürlenemedi — BUILDING'de asılı kaldı",
			"uygulama", rel.AppID, "surum", rel.ID, "hata", err)
	}
}

func (s *Server) sealBuilt(ctx context.Context, rel store.Release, imageID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sealTimeout)
	defer cancel()

	if err := s.store.FinishRelease(ctx, rel.AppID, rel.ID, imageID); err != nil {
		// Bu hata YUTULMAZ. İmaj hostta duruyor ama sürüm BUILDING'de
		// kalmış demektir; istemciye başarı bildirmek, kontrol düzleminin
		// gerçeği ile hostun gerçeğini ayrıştırırdı.
		return appError(err)
	}
	return nil
}

func releaseTarget(appID, releaseID string) string {
	return "app/" + appID + "/release/" + releaseID
}
