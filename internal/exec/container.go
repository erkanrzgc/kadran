package exec

import (
	"context"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/dockerdrv"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// Konteyner yaşam döngüsü uçları.
//
// Her uç aynı üç adımı izler:
//
//	1. DOĞRULA — ayrıcalıklı hiçbir şey yapılmadan önce, tek noktada.
//	   Reddedilirse AUDIT_OUTCOME_DENIED yazılır (record.go).
//	2. YAP     — sürücüye dar, ayrıştırılmış argümanlarla git. Sürücü
//	   proto mesajı KABUL ETMEZ, bu yüzden doğrulanmamış bir istek o
//	   sınırda temsil edilemez.
//	3. YAZ     — gerçekleşen işlem denetim zincirine girer.
//
// Sürücünün devraldığı yükümlülüklerin hepsi internal/dockerdrv'de
// karşılanıyor: imaj çekilmez, ağ/etiket/hacim yolu istekten alınmaz,
// etiket eşleşmesi listeleme sonrası KODDA yeniden doğrulanır.

// invalidArgument, doğrulama hatasını çağırana taşır.
func invalidArgument(err error) error {
	return status.Error(codes.InvalidArgument, err.Error())
}

// internalError, sürücü hatasını çağırana taşır.
//
// Docker'ın mesajı çağırana GİDER (istemci ne olduğunu bilmeli) ama
// denetim kaydına GİRMEZ — gerekçe record.go'da.
func internalError(err error) error {
	return status.Error(codes.Internal, err.Error())
}

// target, denetim kaydındaki hedef dizgisidir.
func target(appID, releaseID string) string {
	if releaseID == "" {
		return "app/" + appID
	}
	return "release/" + appID + "/" + releaseID
}

// selectorOf, doğrulanmış bir proto seçicisini sürücünün dar tipine çevirir.
func selectorOf(sel *panelyv1.ContainerSelector) dockerdrv.Selector {
	out := dockerdrv.Selector{
		AppID:     sel.GetRelease().GetAppId(),
		ReleaseID: sel.GetRelease().GetReleaseId(),
	}
	// Replica VERİLMEMİŞ olabilir; o zaman sürümün tamamı seçilir.
	if sel.Replica != nil {
		r := sel.GetReplica()
		out.Replica = &r
	}
	return out
}

// NetworkEnsure, uygulamanın iç ağını oluşturur.
func (s *Server) NetworkEnsure(ctx context.Context, req *panelyv1.NetworkEnsureRequest) (*panelyv1.NetworkEnsureResponse, error) {
	appID := req.GetAppId()
	const action = "network.ensure"

	if err := validateAppID(appID); err != nil {
		return nil, s.denied(action, target(appID, ""), nil, err)
	}

	name, opErr := s.docker.NetworkEnsure(ctx, appID)
	if err := s.completed(action, target(appID, ""), nil, opErr); err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.NetworkEnsureResponse{NetworkName: name}, nil
}

// ContainerCreate, tek bir replika konteyner oluşturur. Başlatmaz.
func (s *Server) ContainerCreate(ctx context.Context, req *panelyv1.ContainerCreateRequest) (*panelyv1.ContainerCreateResponse, error) {
	const action = "container.create"
	ref := req.GetRef()
	tgt := target(ref.GetRelease().GetAppId(), ref.GetRelease().GetReleaseId())

	// ⚠ Parametreler VARSAYILAN-REDDET ile redakte edilir. Ortam
	// değişkenleri kasadan çözülmüş sırlar taşır; zincir ekle-sadece'dir
	// ve bir kez yazılan sır GERİ ALINAMAZ (silmek zinciri koparır).
	params := auditParams(req)

	if err := validateCreate(req); err != nil {
		return nil, s.denied(action, tgt, params, err)
	}

	opErr := s.docker.ContainerCreate(ctx, dockerdrv.CreateSpec{
		AppID:         ref.GetRelease().GetAppId(),
		ReleaseID:     ref.GetRelease().GetReleaseId(),
		Replica:       ref.GetReplica(),
		CommitSHA:     req.GetCommitSha(),
		Env:           req.GetEnv(),
		MemoryBytes:   req.GetLimits().GetMemoryBytes(),
		CPUMillis:     req.GetLimits().GetCpuMillis(),
		BlkioWeight:   req.GetLimits().GetBlkioWeight(),
		Mounts:        mountsOf(req.GetVolumes()),
		ContainerPort: req.GetContainerPort(),
	})
	if err := s.completed(action, tgt, params, opErr); err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.ContainerCreateResponse{Ref: ref}, nil
}

// auditParams, oluşturma isteğinin denetime yazılacak parametrelerini
// hazırlar.
//
// commit_sha ve replika AÇIK yazılır — bunlar sır değil, tam tersine
// denetimin asıl işine yarayan alanlar: release_id ↔ commit_sha sapması
// ancak kayıtta ikisi de varsa yakalanabilir (exec.proto'daki nota
// bakınız). env ise DEĞERLERİ olmadan, yalnızca anahtar adlarıyla girer.
func auditParams(req *panelyv1.ContainerCreateRequest) map[string]string {
	params := map[string]string{
		"commit_sha": req.GetCommitSha(),
		"replica":    strconv.FormatUint(uint64(req.GetRef().GetReplica()), 10),
	}
	for k, v := range audit.RedactEnv(req.GetEnv()) {
		params["env."+k] = v
	}
	return params
}

// mountsOf, doğrulanmış hacim bağlamalarını sürücünün dar tipine çevirir.
func mountsOf(vols []*panelyv1.VolumeMount) []dockerdrv.Mount {
	if len(vols) == 0 {
		return nil
	}
	out := make([]dockerdrv.Mount, 0, len(vols))
	for _, v := range vols {
		out = append(out, dockerdrv.Mount{
			VolumeName: v.GetVolumeName(),
			MountPath:  v.GetMountPath(),
			ReadOnly:   v.GetReadOnly(),
		})
	}
	return out
}

// ContainerStart, seçilen konteynerleri başlatır.
func (s *Server) ContainerStart(ctx context.Context, req *panelyv1.ContainerStartRequest) (*panelyv1.ContainerStartResponse, error) {
	const action = "container.start"
	sel := req.GetSelector()
	tgt := target(sel.GetRelease().GetAppId(), sel.GetRelease().GetReleaseId())

	if err := validateSelector(sel); err != nil {
		return nil, s.denied(action, tgt, nil, err)
	}

	n, opErr := s.docker.ContainerStart(ctx, selectorOf(sel))
	if err := s.completed(action, tgt, affectedParams(n), opErr); err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.ContainerStartResponse{Affected: uint32(n)}, nil //nolint:gosec // sürücü sayımı replika sınırıyla kapalı
}

// ContainerStop, seçilen konteynerleri durdurur.
func (s *Server) ContainerStop(ctx context.Context, req *panelyv1.ContainerStopRequest) (*panelyv1.ContainerStopResponse, error) {
	const action = "container.stop"
	sel := req.GetSelector()
	tgt := target(sel.GetRelease().GetAppId(), sel.GetRelease().GetReleaseId())

	if err := validateSelector(sel); err != nil {
		return nil, s.denied(action, tgt, nil, err)
	}
	if err := validateStopTimeout(req.GetTimeoutSeconds()); err != nil {
		return nil, s.denied(action, tgt, nil, err)
	}

	n, opErr := s.docker.ContainerStop(ctx, selectorOf(sel), req.GetTimeoutSeconds())
	if err := s.completed(action, tgt, affectedParams(n), opErr); err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.ContainerStopResponse{Affected: uint32(n)}, nil //nolint:gosec // sürücü sayımı replika sınırıyla kapalı
}

// ContainerRemove, seçilen konteynerleri durdurup siler.
//
// Hacimlere DOKUNMAZ. Konteyner silmek geri alınabilir bir işlemdir
// (yeniden oluşturulur); hacim silmek değildir ve şartname §1.3 gereği
// TOTP kapısına tabidir. İkisini tek uçta birleştirmek, geri alınamaz
// işlemi geri alınabilir bir isteğin yan etkisi hâline getirirdi.
func (s *Server) ContainerRemove(ctx context.Context, req *panelyv1.ContainerRemoveRequest) (*panelyv1.ContainerRemoveResponse, error) {
	const action = "container.remove"
	sel := req.GetSelector()
	tgt := target(sel.GetRelease().GetAppId(), sel.GetRelease().GetReleaseId())

	if err := validateSelector(sel); err != nil {
		return nil, s.denied(action, tgt, nil, err)
	}

	n, opErr := s.docker.ContainerRemove(ctx, selectorOf(sel))
	if err := s.completed(action, tgt, affectedParams(n), opErr); err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.ContainerRemoveResponse{Affected: uint32(n)}, nil //nolint:gosec // sürücü sayımı replika sınırıyla kapalı
}

// affectedParams, kaç konteynerin etkilendiğini denetime yazar.
//
// Bu sayı denetimin asıl bilgisi: "sürümü durdur" isteğinin 1 mi 40
// konteyner durdurduğu, kayıttan okunabilmeli.
func affectedParams(n int) map[string]string {
	return map[string]string{"affected": strconv.Itoa(n)}
}

// ContainerList, Panely'nin yönettiği konteynerleri sayar. Salt okunur.
//
// Denetime YAZILMAZ: durum değiştirmeyen çağrılar kaydedilmez (server.go'
// daki denetim politikasına bakınız). panelyd bu ucu durum ekranı için
// düzenli olarak çağırır; her çağrıyı kaydetmek günlüğü gürültüyle
// doldurur ve asıl ayrıcalıklı işlemleri görünmez kılardı.
//
// app_id BOŞ olabilir: o zaman `panely.app_id` etiketi taşıyan tüm
// konteynerler dönülür. panelyd bir uygulama kaydını tamamen kaybederse
// öksüz konteynerleri ancak böyle bulabilir.
func (s *Server) ContainerList(ctx context.Context, req *panelyv1.ContainerListRequest) (*panelyv1.ContainerListResponse, error) {
	if appID := req.GetAppId(); appID != "" {
		if err := validateAppID(appID); err != nil {
			return nil, invalidArgument(err)
		}
	}

	found, err := s.docker.ContainerList(ctx, req.GetAppId())
	if err != nil {
		return nil, internalError(err)
	}
	return &panelyv1.ContainerListResponse{Containers: managedToProto(found)}, nil
}

// managedToProto, sürücünün konteyner tipini şemaya çevirir.
//
// # Neden pbconv'da değil?
//
// pbconv'u panelyd de içe aktarıyor. Dönüşüm oraya konsaydı pbconv
// dockerdrv'ye bağımlı olur ve YETKİSİZ daemon, Docker sürücüsünü
// binary'sine LİNKLERDİ. Kod yolu çağrılmasa bile bu, "panelyd Docker'a
// erişemez" iddiasını okuyan biri için gereksiz bir soru işareti yaratır.
// Dönüşümün tek kullanıcısı burası.
func managedToProto(list []dockerdrv.Container) []*panelyv1.ManagedContainer {
	out := make([]*panelyv1.ManagedContainer, 0, len(list))
	for _, c := range list {
		out = append(out, &panelyv1.ManagedContainer{
			Ref: &panelyv1.ContainerRef{
				Release: &panelyv1.ReleaseRef{AppId: c.AppID, ReleaseId: c.ReleaseID},
				Replica: c.Replica,
			},
			State:     containerStateToProto(c.State),
			CreatedAt: timestamppb.New(c.CreatedAt),
			IpAddress: c.IPAddress,
		})
	}
	return out
}

// containerStateToProto, Docker'ın durum dizgisini şema enum'una çevirir.
//
// TANINMAYAN DURUM UNSPECIFIED olur, "çalışıyor" değil. Docker ileride
// yeni bir durum eklerse, onu yanlışlıkla çalışıyor saymak sağlık
// denetçisini yanıltır ve ölü bir sürüme trafik taşınmasına yol açabilir.
func containerStateToProto(state string) panelyv1.ContainerState {
	switch state {
	case "created":
		return panelyv1.ContainerState_CONTAINER_STATE_CREATED
	case "running":
		return panelyv1.ContainerState_CONTAINER_STATE_RUNNING
	case "paused":
		return panelyv1.ContainerState_CONTAINER_STATE_PAUSED
	case "restarting":
		return panelyv1.ContainerState_CONTAINER_STATE_RESTARTING
	case "exited":
		return panelyv1.ContainerState_CONTAINER_STATE_EXITED
	case "dead":
		return panelyv1.ContainerState_CONTAINER_STATE_DEAD
	case "removing":
		return panelyv1.ContainerState_CONTAINER_STATE_REMOVING
	default:
		return panelyv1.ContainerState_CONTAINER_STATE_UNSPECIFIED
	}
}

// ContainerLogs, bir replikanın çıktısını akıtır.
//
// # Neden denetime YAZILMIYOR
//
// Salt okunur bir çağrıdır ve server.go'daki politika yalnızca durum
// değiştiren işlemleri kaydeder — ContainerList ile aynı gerekçe: panelyd
// bu ucu sürekli çağırır (GUI'de akan günlük penceresi) ve her açılışı
// kaydetmek zinciri gürültüyle doldurup asıl ayrıcalıklı işlemleri
// görünmez kılardı.
//
// Bu, §8.1'in terminaliyle KASITLI bir asimetridir: PTYOpen çalıştırma
// yeteneği verir, kendi denetim kaydı ve TOTP kapısı vardır. Günlük okumak
// yalnızca gözlemdir.
func (s *Server) ContainerLogs(req *panelyv1.ContainerLogsRequest, stream grpc.ServerStreamingServer[panelyv1.ContainerLogsResponse]) error {
	ref := req.GetRef()
	if err := validateContainerRef(ref); err != nil {
		return invalidArgument(err)
	}

	replica := ref.GetReplica()
	sel := dockerdrv.Selector{
		AppID:     ref.GetRelease().GetAppId(),
		ReleaseID: ref.GetRelease().GetReleaseId(),
		// Günlük TEK replikanındır; seçici tekilleştirilmiş olmalı.
		Replica: &replica,
	}

	// since verilmemişse sıfır zaman gider ve sürücü parametreyi hiç koymaz.
	var since time.Time
	if ts := req.GetSince(); ts != nil {
		since = ts.AsTime()
	}

	err := s.docker.ContainerLogs(stream.Context(), sel,
		req.GetTailLines(), req.GetFollow(), since,
		func(data []byte, isStderr bool) error {
			return stream.Send(&panelyv1.ContainerLogsResponse{Data: data, IsStderr: isStderr})
		})
	if err != nil {
		return internalError(err)
	}
	return nil
}

// ImageBuild, bir sürümün imajını uzak git bağlamından derletir.
//
// ── Ağa KİM çıkıyor? Hiçbirimiz ─────────────────────────────────────
//
// Bu soru bu RPC'yi bir dilim boyunca geciktirdi ve notlardaki ikilem
// ("panelyd mi çeksin, executor mü?") YANLIŞTI: her iki birim de
// `RestrictAddressFamilies=AF_UNIX` taşıyor ve ikisinin de gevşetilmesi
// gerekmiyor. Uzak bağlamı BuildKit kendi ağ ad alanında çözüyor;
// executor yalnızca unix soketine bayt yazıyor (docs/decisions.md K-035).
//
// Sürücü diliminin devraldığı yükümlülükler:
//
//  1. Uzak bağlam URL'i YALNIZCA BuildContextURL ile kurulur.
//  2. Etiket YALNIZCA dockerdrv.ImageTag ile kurulur.
//  3. Derleme argümanları denetime yazılırken audit.RedactEnv uygulanır.
//  4. Derleme çıktısı istemciye akar ama denetim kaydına GİRMEZ.
//  5. ⚠ BAŞARISIZLIK İKİ BİÇİMDE GELİYOR ve yalnızca biri HTTP hatası —
//     ölçüldü: çekme aşamasındaki hata HTTP 500, derleme ORTASINDAKİ hata
//     HTTP 200 + akışın son karesinde `{"error":...}`. HTTP kodunu
//     kontrol etmek başarısız her derlemeyi BAŞARILI göstermeye yeter.
//  6. Başarı ölçütü POZİTİF olmalı: sürücü `aux` karesini şart koşuyor,
//     "hata karesi görmedim"i yeterli saymıyor (dockerdrv/build.go).
func (s *Server) ImageBuild(req *panelyv1.ImageBuildRequest, stream grpc.ServerStreamingServer[panelyv1.ImageBuildResponse]) error {
	const action = "image.build"
	rel := req.GetRelease()
	tgt := target(rel.GetAppId(), rel.GetReleaseId())
	params := buildAuditParams(req)

	if err := validateImageBuild(req, s.allowedGitHosts); err != nil {
		return s.denied(action, tgt, params, err)
	}

	// ⚠ Bu çağrı ile completed() arasına ERKEN DÖNÜŞ KONULAMAZ.
	//
	// Akış dört yoldan biter: başarı, hata karesi, taşıma hatası ve
	// istemcinin kopması / bağlam iptali. Dördüncüsü sinsi olanıdır —
	// derleme gerçekten koşmuş, imaj üretilmiş ama kayıt yazılmamış
	// olabilirdi. Kayıt her dört yolda da yazılsın diye tek çıkış var.
	imageID, opErr := s.docker.ImageBuild(stream.Context(), dockerdrv.BuildSpec{
		AppID:      rel.GetAppId(),
		CommitSHA:  req.GetSource().GetCommitSha(),
		ContextURL: BuildContextURL(req.GetSource()),
		Dockerfile: req.GetDockerfilePath(),
		BuildArgs:  req.GetBuildArgs(),
	}, func(data []byte, isStderr bool) error {
		// ⚠ Derleme ÇIKTISI istemciye akar ama denetime GİRMEZ: kullanıcının
		// deposundan gelir ve zincir ekle-sadece'dir.
		return stream.Send(&panelyv1.ImageBuildResponse{Data: data, IsStderr: isStderr})
	})

	// image_id kaydın YANLIŞLANABİLİR olmasını sağlayan alandır: hangi
	// imajın gerçekten üretildiği sonradan hostta kontrol edilebilir.
	//
	// Aynı değer panelyd'ye de gönderilir: başarının pozitif kanıtı
	// yalnızca burada kalsaydı, daemon'ın elinde "hata almadım"dan başka
	// bir ölçüt olmazdı (bkz. exec.proto, ImageBuildResponse.image_id).
	// Gönderim başarısız olursa opErr dolar ve kayıt FAILURE yazar —
	// doğru yön: panelyd kimliği öğrenmediyse sürümü mühürleyemez.
	if imageID != "" {
		params["image_id"] = imageID
		if opErr == nil {
			opErr = stream.Send(&panelyv1.ImageBuildResponse{ImageId: imageID})
		}
	}
	if err := s.completed(action, tgt, params, opErr); err != nil {
		return internalError(err)
	}
	return nil
}

// buildAuditParams, derleme isteğinin denetime yazılacak parametreleridir.
//
// Kaynak üçlüsü ve commit_sha AÇIK yazılır — denetimin asıl işi "hangi
// koddan hangi imaj çıktı" sorusudur. Derleme argümanlarının DEĞERLERİ
// yazılmaz: exec.proto onların sır taşımaması gerektiğini söylüyor, ama
// "gerekiyor" ile "öyle" aynı şey değil ve zincire bir kez giren sır geri
// alınamaz.
func buildAuditParams(req *panelyv1.ImageBuildRequest) map[string]string {
	src := req.GetSource()
	params := map[string]string{
		"source":          src.GetHost() + "/" + src.GetOwner() + "/" + src.GetRepo(),
		"commit_sha":      src.GetCommitSha(),
		"dockerfile_path": req.GetDockerfilePath(),
	}
	for k, v := range audit.RedactEnv(req.GetBuildArgs()) {
		params["build_arg."+k] = v
	}
	return params
}
