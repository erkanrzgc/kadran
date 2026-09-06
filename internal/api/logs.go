package api

import (
	"google.golang.org/grpc"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// maxTailLines, geçmişten istenebilecek azami satır sayısıdır.
//
// Sınırsız bırakmak, aylardır koşan bir konteynerin bütün geçmişini tek
// istekte executor'dan panelyd'ye, oradan SSH üzerinden istemciye
// pompalamak demekti. Sınır kaba ama gerçek: 10.000 satır bir teşhis
// için fazlasıyla yeter, gigabaytlık bir akışı ise engeller.
const maxTailLines = 10_000

// StreamLogs, uygulamanın CANLI sürümünün çıktısını akıtır.
//
// ── Sürümü sunucu buluyor ───────────────────────────────────────────
//
// İstek yalnızca `app_id` taşıyor. Aktif sürüm dağıtım kaydında duruyor
// ve onu burada okumak, istemcinin bayat bir sürüm kimliği göndermesini
// imkânsız kılıyor: kullanıcı `app show` çıktısından bir sürüm kopyalayıp
// komutu çalıştırana kadar dağıtım değişmiş olabilirdi.
//
// ── Neden denetim zincirine YAZILMIYOR ──────────────────────────────
//
// Günlük okumak durum DEĞİŞTİRMEYEN bir işlem ve zincir yıkıcı eylemler
// için var. Her `logs -f` çağrısını yazmak, zinciri asıl aradığın
// kayıtların görünmez olacağı kadar gürültüyle doldururdu — `audit list`
// zaten sayfalı ve varsayılanı 50 satır.
//
// ⚠ Bu bir TERCİH, bir unutma değil: okuma erişimi zaten `panely-client`
// grubuyla sınırlı ve o sınır SO_PEERCRED ile bağlantı kurulurken
// uygulanıyor. Kimin okuduğu sorusu gerekirse ayrı bir okuma günlüğü
// ister; append-only zincire karıştırmak yanlış yer olurdu.
func (s *Server) StreamLogs(
	req *panelyv1.StreamLogsRequest,
	stream grpc.ServerStreamingServer[panelyv1.StreamLogsResponse],
) error {
	ctx := stream.Context()
	appID := req.GetAppId()

	if _, err := s.store.GetApp(ctx, appID); err != nil {
		return appError(err)
	}

	// Canlı sürüm YOKSA bu bir argüman hatası değil, durum hatası:
	// istek kusursuz, henüz dağıtılmamış bir uygulamanın günlüğü yok.
	// appError bu ayrımı ErrNoDeployment için zaten yapıyor.
	live, err := s.store.ActiveDeployment(ctx, appID)
	if err != nil {
		return appError(err)
	}

	tail := req.GetTailLines()
	if tail > maxTailLines {
		tail = maxTailLines
	}

	return s.exec.ContainerLogs(ctx, execclient.LogOptions{
		AppID:     appID,
		ReleaseID: live.ReleaseID,
		// Replica 0: bugün varsayılan tek replika. Çoklu replikada doğru
		// davranış hepsini harmanlamak ve o, şemaya alan eklemeden
		// yapılabilir — bkz. api.proto'daki gerekçe.
		Replica:   0,
		TailLines: tail,
		Follow:    req.GetFollow(),
	}, func(data []byte, isStderr bool) error {
		return stream.Send(&panelyv1.StreamLogsResponse{
			Data:     data,
			IsStderr: isStderr,
		})
	})
}
