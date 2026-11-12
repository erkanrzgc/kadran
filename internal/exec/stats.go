package exec

import (
	"context"

	"github.com/erkanrzgc/kadran/internal/dockerdrv"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// statsSource, collectStats'ın Docker'dan istediği iki çağrı. Arayüz
// testler için: sürücü unix soketine bağlanıyor.
type statsSource interface {
	ContainerList(ctx context.Context, appID string) ([]dockerdrv.Container, error)
	ContainerStats(ctx context.Context, id string) (dockerdrv.Usage, error)
}

// ContainerStats, Kadran'ın etiketlediği çalışan konteynerlerin sayaçları
// (K-166). Salt okunur; yanıtta konteyner kimliği yok, replika etiket
// üçlüsüyle adlandırılıyor.
func (s *Server) ContainerStats(ctx context.Context, _ *kadranv1.ContainerStatsRequest) (*kadranv1.ContainerStatsResponse, error) {
	usage, err := collectStats(ctx, s.docker)
	if err != nil {
		return nil, internalError(err)
	}
	return &kadranv1.ContainerStatsResponse{Containers: usage}, nil
}

// collectStats: liste hatası döner; tek bir konteynerin istatistiği
// alınamazsa o konteyner atlanır. Liste ile istatistik arasında duran
// konteyner beklenen bir durum ve bir replikanın eksik sayılması bütün
// ekranı boşaltmaktan iyidir.
func collectStats(ctx context.Context, src statsSource) ([]*kadranv1.ContainerUsage, error) {
	found, err := src.ContainerList(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]*kadranv1.ContainerUsage, 0, len(found))
	for _, c := range found {
		if c.State != "running" {
			continue
		}
		u, err := src.ContainerStats(ctx, c.ID)
		if err != nil {
			continue
		}
		out = append(out, &kadranv1.ContainerUsage{
			Ref:        &kadranv1.ContainerRef{Release: &kadranv1.ReleaseRef{AppId: c.AppID, ReleaseId: c.ReleaseID}, Replica: c.Replica},
			CpuUsageNs: u.CPUNanos, MemoryBytes: u.MemoryBytes, MemoryLimitBytes: u.MemoryLimit,
			NetRxBytes: u.RxBytes, NetTxBytes: u.TxBytes, DiskReadBytes: u.ReadBytes, DiskWriteBytes: u.WriteBytes, Tasks: u.Tasks,
		})
	}
	return out, nil
}
