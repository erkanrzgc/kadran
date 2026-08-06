package exec

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// Konteyner yaşam döngüsü uçları — Faz 1, dilim 1.
//
// ════════════════════════════════════════════════════════════════════
//  BU DOSYADA SÜRÜCÜ YOKTUR. Uçlar isteği DOĞRULAR ve Unimplemented
//  döner. Bu kasıtlıdır, eksik değildir.
// ════════════════════════════════════════════════════════════════════
//
// Gerekçe: şema güvenlik sınırının kendisidir. Sınır önce sabitlenirse,
// sürücü onu kazara genişletemez — ancak şemanın izin verdiği kadarını
// yapabilir. Ters sırada yazılsaydı, sürücünün ihtiyaç duyduğu her alan
// şemaya "geçici olarak" eklenir ve beyaz liste erirdi.
//
// Boş bırakmak da mümkün değildi: buf.gen.yaml'da
// require_unimplemented_servers=false ayarlı olduğu için ExecutorService'e
// eklenen her RPC bu paketi DERLENMEZ hâle getirir (K-002).
//
// ── Denetim günlüğü neden burada yazılmıyor ─────────────────────────
//
// Executor kendi hash-zincirli günlüğüne yalnızca GERÇEKLEŞEN ayrıcalıklı
// işlemleri yazar. Hiçbir konteynere dokunmayan bir çağrıyı kaydetmek,
// zincire olmamış olayların kaydını koyardı; zincirin değeri tam olarak
// "burada yazan oldu" güvencesidir. Kayıt, sürücüyle birlikte gelecek —
// reddedilenler AUDIT_OUTCOME_DENIED ile.
//
// ── Sürücü diliminin devraldığı yükümlülükler ───────────────────────
//
//  1. İMAJ ASLA ÇEKİLMEZ. Etiket `panely/<app_id>:<commit_sha>` olarak
//     kurulur ve kayıtsız (registry'siz) bir addır; Docker böyle bir adı
//     çözemezse docker.io/panely/<app_id> olarak YORUMLAR. Engine API'nin
//     `POST /containers/create` ucu kendiliğinden çekmez (404 döner) —
//     sürücü bu davranışa yaslanmalı ve hiçbir kod yoluna pull koymamalı.
//     Aksi hâlde yerel imaj eksikken Docker Hub'daki yabancı bir imaj
//     çalıştırılabilirdi.
//  2. Ağ adı `panely-<app_id>` olarak KURULUR, istekten alınmaz.
//  3. Hacimler `nodev,nosuid` ile bağlanmalı. Doğrulayıcı bunu sağlayamaz;
//     mount seçeneği çalışma anında verilir.
//  4. Etiket eşleşmesi TAM olmalı: `panely.app_id` etiketi taşımayan
//     hiçbir konteyner listelenmez, durdurulmaz, silinmez.

// invalidArgument, doğrulama hatasını çağırana taşır.
func invalidArgument(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// notYetImplemented, doğrulaması geçen ama sürücüsü henüz yazılmamış bir
// ucu bildirir. Unimplemented ile InvalidArgument'ı ayırmak önemlidir:
// ilki "bu sürüm yapmıyor", ikincisi "istek reddedildi" demektir.
func notYetImplemented(rpc string) error {
	return status.Errorf(codes.Unimplemented,
		"exec: %s doğrulandı ama sürücü henüz yok (Faz 1, sürücü dilimi)", rpc)
}

// NetworkEnsure, uygulamanın iç ağını oluşturur.
func (s *Server) NetworkEnsure(_ context.Context, req *panelyv1.NetworkEnsureRequest) (*panelyv1.NetworkEnsureResponse, error) {
	if err := validateAppID(req.GetAppId()); err != nil {
		return nil, invalidArgument(err)
	}
	return nil, notYetImplemented("NetworkEnsure")
}

// ContainerCreate, tek bir replika konteyner oluşturur.
func (s *Server) ContainerCreate(_ context.Context, req *panelyv1.ContainerCreateRequest) (*panelyv1.ContainerCreateResponse, error) {
	if err := validateCreate(req); err != nil {
		return nil, invalidArgument(err)
	}
	return nil, notYetImplemented("ContainerCreate")
}

// ContainerStart, seçilen konteynerleri başlatır.
func (s *Server) ContainerStart(_ context.Context, req *panelyv1.ContainerStartRequest) (*panelyv1.ContainerStartResponse, error) {
	if err := validateSelector(req.GetSelector()); err != nil {
		return nil, invalidArgument(err)
	}
	return nil, notYetImplemented("ContainerStart")
}

// ContainerStop, seçilen konteynerleri durdurur.
func (s *Server) ContainerStop(_ context.Context, req *panelyv1.ContainerStopRequest) (*panelyv1.ContainerStopResponse, error) {
	if err := validateSelector(req.GetSelector()); err != nil {
		return nil, invalidArgument(err)
	}
	if err := validateStopTimeout(req.GetTimeoutSeconds()); err != nil {
		return nil, invalidArgument(err)
	}
	return nil, notYetImplemented("ContainerStop")
}

// ContainerRemove, seçilen konteynerleri durdurup siler.
//
// Hacimlere DOKUNMAZ. Konteyner silmek geri alınabilir bir işlemdir
// (yeniden oluşturulur); hacim silmek değildir ve şartname §1.3 gereği
// TOTP kapısına tabidir. İkisini tek uçta birleştirmek, geri alınamaz
// işlemi geri alınabilir bir isteğin yan etkisi hâline getirirdi.
func (s *Server) ContainerRemove(_ context.Context, req *panelyv1.ContainerRemoveRequest) (*panelyv1.ContainerRemoveResponse, error) {
	if err := validateSelector(req.GetSelector()); err != nil {
		return nil, invalidArgument(err)
	}
	return nil, notYetImplemented("ContainerRemove")
}

// ContainerList, Panely'nin yönettiği konteynerleri sayar. Salt okunur.
//
// app_id BOŞ olabilir: o zaman `panely.app_id` etiketi taşıyan tüm
// konteynerler dönülür. panelyd bir uygulama kaydını tamamen kaybederse
// öksüz konteynerleri ancak böyle bulabilir.
func (s *Server) ContainerList(_ context.Context, req *panelyv1.ContainerListRequest) (*panelyv1.ContainerListResponse, error) {
	if appID := req.GetAppId(); appID != "" {
		if err := validateAppID(appID); err != nil {
			return nil, invalidArgument(err)
		}
	}
	return nil, notYetImplemented("ContainerList")
}

// ContainerLogs, bir replikanın çıktısını akıtır.
func (s *Server) ContainerLogs(req *panelyv1.ContainerLogsRequest, _ grpc.ServerStreamingServer[panelyv1.ContainerLogChunk]) error {
	if err := validateContainerRef(req.GetRef()); err != nil {
		return invalidArgument(err)
	}
	return notYetImplemented("ContainerLogs")
}
