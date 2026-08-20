package api

import (
	"context"
	"errors"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// CreateApp, yeni bir uygulama tanımı kaydeder.
func (s *Server) CreateApp(
	ctx context.Context, req *panelyv1.CreateAppRequest,
) (*panelyv1.CreateAppResponse, error) {
	const action = "app.create"

	spec := req.GetSpec()
	tgt := appTarget(spec.GetAppId())
	params := appAuditParams(spec)

	if err := validateAppSpec(spec); err != nil {
		return nil, s.denied(ctx, action, tgt, params, err)
	}

	app, opErr := s.store.CreateApp(ctx, appFromProto(spec))
	if err := s.completed(ctx, action, tgt, params, opErr); err != nil {
		return nil, appError(err)
	}
	return &panelyv1.CreateAppResponse{App: appToProto(app)}, nil
}

// ListApps, tanımlı uygulamaları döner. Salt okunur: zincire yazılmaz.
func (s *Server) ListApps(
	ctx context.Context, _ *panelyv1.ListAppsRequest,
) (*panelyv1.ListAppsResponse, error) {
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "uygulamalar okunamadı: %v", err)
	}

	out := make([]*panelyv1.App, 0, len(apps))
	for _, a := range apps {
		out = append(out, appToProto(a))
	}
	return &panelyv1.ListAppsResponse{Apps: out}, nil
}

// GetApp, tek bir uygulamayı ve sürüm geçmişini döner. Salt okunur.
func (s *Server) GetApp(
	ctx context.Context, req *panelyv1.GetAppRequest,
) (*panelyv1.GetAppResponse, error) {
	app, err := s.store.GetApp(ctx, req.GetAppId())
	if err != nil {
		return nil, appError(err)
	}

	releases, err := s.store.ListReleases(ctx, app.ID, int(req.GetReleaseLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "sürümler okunamadı: %v", err)
	}

	out := make([]*panelyv1.Release, 0, len(releases))
	for _, r := range releases {
		out = append(out, releaseToProto(r))
	}
	return &panelyv1.GetAppResponse{App: appToProto(app), Releases: out}, nil
}

// ── Dönüşümler ───────────────────────────────────────────────────────
//
// internal/pbconv'a KONULMADI ve konulmamalı: o paket panely-exec'in içe
// aktarma grafiğinde ve ayrıcalıklı yüzey bütçesine yazılıyor
// (scripts/check-exec-surface.sh). Yalnızca panelyd'nin kullandığı
// dönüştürücüleri oraya koymak, root süreçle hiç ilgisi olmayan kodu
// root bütçesinden harcamak olurdu — ölçüldü: bütçede 6 satır kalmıştı.

func appFromProto(spec *panelyv1.AppSpec) store.App {
	l := spec.GetLimits()
	return store.App{
		ID:             spec.GetAppId(),
		GitHost:        spec.GetGitHost(),
		GitOwner:       spec.GetGitOwner(),
		GitRepo:        spec.GetGitRepo(),
		GitBranch:      spec.GetGitBranch(),
		DockerfilePath: spec.GetDockerfilePath(),
		BuildArgs:      spec.GetBuildArgs(),
		ContainerPort:  spec.GetContainerPort(),
		Replicas:       spec.GetReplicas(),
		HealthPath:     spec.GetHealthPath(),
		Domain:         spec.GetDomain(),
		MemoryBytes:    l.GetMemoryBytes(),
		CPUMillis:      l.GetCpuMillis(),
		BlkioWeight:    l.GetBlkioWeight(),
	}
}

func appToProto(a store.App) *panelyv1.App {
	return &panelyv1.App{
		Spec: &panelyv1.AppSpec{
			AppId:          a.ID,
			GitHost:        a.GitHost,
			GitOwner:       a.GitOwner,
			GitRepo:        a.GitRepo,
			GitBranch:      a.GitBranch,
			DockerfilePath: a.DockerfilePath,
			BuildArgs:      a.BuildArgs,
			ContainerPort:  a.ContainerPort,
			Replicas:       a.Replicas,
			HealthPath:     a.HealthPath,
			Domain:         a.Domain,
			Limits: &panelyv1.ResourceLimits{
				MemoryBytes: a.MemoryBytes,
				CpuMillis:   a.CPUMillis,
				BlkioWeight: a.BlkioWeight,
			},
		},
		CreatedAt:    timestamppb.New(a.CreatedAt),
		UpdatedAt:    timestamppb.New(a.UpdatedAt),
		ReleaseCount: a.ReleaseSeq,
	}
}

func releaseToProto(r store.Release) *panelyv1.Release {
	out := &panelyv1.Release{
		ReleaseId: r.ID,
		AppId:     r.AppID,
		CommitSha: r.CommitSHA,
		Status:    panelyv1.ReleaseStatus(r.Status),
		ImageId:   r.ImageID,
		StartedAt: timestamppb.New(r.StartedAt),
		Detail:    r.Detail,
	}
	// Bitmemiş sürümde alan HİÇ doldurulmaz. timestamppb.New(zero) 1970'i
	// üretirdi ve arayüzde "1 Ocak 1970'te bitti" diye görünürdü.
	if !r.FinishedAt.IsZero() {
		out.FinishedAt = timestamppb.New(r.FinishedAt)
	}
	return out
}

// ── Denetim ve hata eşleme ───────────────────────────────────────────

func appTarget(appID string) string { return "app/" + appID }

// appAuditParams, uygulama tanımının denetime yazılacak alanlarıdır.
//
// Kaynak üçlüsü ve yapılandırma AÇIK yazılır: denetimin işi "hangi
// tanım kaydedildi" sorusunu yanıtlamak. Derleme argümanlarının
// DEĞERLERİ yazılmaz — RedactSensitive recordAction içinde uygulanıyor,
// ama burada adları da ayrıca ayrıştırılmıyor: değer hiç girmiyor.
func appAuditParams(spec *panelyv1.AppSpec) map[string]string {
	params := map[string]string{
		"source": spec.GetGitHost() + "/" + spec.GetGitOwner() + "/" + spec.GetGitRepo(),
		"branch": spec.GetGitBranch(),
		"domain": spec.GetDomain(),
		"port":   strconv.FormatUint(uint64(spec.GetContainerPort()), 10),
		"replicas": strconv.FormatUint(
			uint64(spec.GetReplicas()), 10),
	}
	for k := range spec.GetBuildArgs() {
		params["build_arg."+k] = "[REDACTED]"
	}
	return params
}

// appError, depo hatalarını gRPC kodlarına çevirir.
//
// Eşleme AÇIK tutuluyor: her şeyi Internal döndürmek, "böyle bir
// uygulama yok" ile "veritabanı bozuk" durumlarını istemci için
// ayırt edilemez yapardı.
func appError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrAppNotFound), errors.Is(err, store.ErrReleaseNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, store.ErrAppExists):
		return status.Error(codes.AlreadyExists, err.Error())
	// ⚠ ErrAppExists ile AYNI kola konulmadı. İkisi de AlreadyExists'e
	// düşüyor ama farklı şeyler söylüyorlar: biri "bu kimlik dolu", öteki
	// "bu alan adı BAŞKA bir uygulamada". Tek kola indirgemek, ileride
	// biri farklı bir kod alması gerektiğinde ayrımın kaybolduğu yer
	// olurdu — ve mesajlar zaten farklı, yani kullanıcı ikisini ayırt
	// edebiliyor.
	case errors.Is(err, store.ErrDomainTaken):
		return status.Error(codes.AlreadyExists, err.Error())
	// ⚠ NotFound DEĞİL, FailedPrecondition. Uygulama VAR; eksik olan onun
	// durumu — henüz dağıtılmamış ya da geri alınacak bir öncesi yok.
	// NotFound'a düşürmek, "böyle bir uygulama yok" ile karıştırılırdı ve
	// kullanıcı yazım hatası aramaya başlardı.
	case errors.Is(err, store.ErrNoDeployment), errors.Is(err, store.ErrNoPreviousDeployment):
		return status.Error(codes.FailedPrecondition, err.Error())
	case status.Code(err) != codes.Unknown:
		// Zaten bir gRPC hatası (ör. denetim kaydı yazılamadı).
		return err
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
