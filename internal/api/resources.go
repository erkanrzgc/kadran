package api

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/erkanrzgc/kadran/internal/execclient"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// GetResourceUsage, hostun ve istenirse uygulamaların kümülatif kaynak
// sayaçlarını döner (K-166). Salt okunur; denetim zincirine yazılmaz
// (gerekçe LoggingInterceptor'da). Yalnız yöneticiye açık: izin listesi
// varsayılan ret (K-131).
//
// Hız (CPU %, bayt/sn) burada hesaplanmıyor: sunucu durumsuz, istemci iki
// yanıtın farkını alıyor.
func (s *Server) GetResourceUsage(ctx context.Context, req *kadranv1.GetResourceUsageRequest) (*kadranv1.GetResourceUsageResponse, error) {
	resp := &kadranv1.GetResourceUsageResponse{SampledAtUnixMs: s.now().UnixMilli(), Host: s.host.Read()}
	if !req.GetIncludeApps() {
		return resp, nil
	}

	ctx, cancel := context.WithTimeout(ctx, execclient.DefaultTimeout)
	defer cancel()
	usage, err := s.exec.ContainerStats(ctx)
	if err != nil {
		// Executor kapalıyken host yine gösterilir. Boş liste "uygulama
		// yok" sanılmasın diye apps_measured false kalıyor.
		return resp, nil
	}
	resp.Apps, resp.AppsMeasured = aggregateApps(usage), true
	return resp, nil
}

// aggregateApps, replikaları uygulamaya göre toplar, app_id'ye göre sıralar.
func aggregateApps(list []*kadranv1.ContainerUsage) []*kadranv1.AppUsage {
	byApp := map[string]*kadranv1.AppUsage{}
	for _, c := range list {
		id := c.GetRef().GetRelease().GetAppId()
		a, ok := byApp[id]
		if !ok {
			a = &kadranv1.AppUsage{AppId: id}
			byApp[id] = a
		}
		a.Containers++
		a.CpuUsageNs += c.GetCpuUsageNs()
		a.MemoryBytes += c.GetMemoryBytes()
		a.MemoryLimitBytes += c.GetMemoryLimitBytes()
		a.NetRxBytes += c.GetNetRxBytes()
		a.NetTxBytes += c.GetNetTxBytes()
		a.DiskReadBytes += c.GetDiskReadBytes()
		a.DiskWriteBytes += c.GetDiskWriteBytes()
		a.Tasks += c.GetTasks()
	}
	return slices.SortedFunc(maps.Values(byApp), func(a, b *kadranv1.AppUsage) int {
		return strings.Compare(a.GetAppId(), b.GetAppId())
	})
}
