package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

type fakeHost struct {
	usage            *kadranv1.HostUsage
	total, available uint64
	memOK            bool
}

func (f fakeHost) Read() *kadranv1.HostUsage                  { return f.usage }
func (f fakeHost) Memory() (total, available uint64, ok bool) { return f.total, f.available, f.memOK }

func usage(app, release string, replica uint32, n uint64) *kadranv1.ContainerUsage {
	return &kadranv1.ContainerUsage{
		Ref:        &kadranv1.ContainerRef{Release: &kadranv1.ReleaseRef{AppId: app, ReleaseId: release}, Replica: replica},
		CpuUsageNs: n, MemoryBytes: n * 10, MemoryLimitBytes: n * 100, NetRxBytes: n + 1, NetTxBytes: n + 2,
		DiskReadBytes: n + 3, DiskWriteBytes: n + 4, Tasks: uint32(n), //nolint:gosec // test değeri küçük
	}
}

func resourceServer(t *testing.T, fe *fakeExec) *Server {
	t.Helper()
	srv, _ := newDeployServer(t, fe)
	srv.host = fakeHost{usage: &kadranv1.HostUsage{CpuCount: 2, CpuTotalTicks: proto.Uint64(6610)}}
	srv.now = func() time.Time { return time.UnixMilli(1_700_000_000_123) }
	return srv
}

// Filo kartı uygulamaları istemez: executor'a hiç gidilmemeli.
func TestResourceUsageHostOnly(t *testing.T) {
	fe := &fakeExec{stats: []*kadranv1.ContainerUsage{usage("blog", "r1", 0, 5)}}
	resp, err := resourceServer(t, fe).GetResourceUsage(context.Background(), &kadranv1.GetResourceUsageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if fe.statsCalls != 0 {
		t.Fatalf("executor %d kez soruldu, hiç sorulmamalı", fe.statsCalls)
	}
	if resp.GetSampledAtUnixMs() != 1_700_000_000_123 || resp.GetHost().GetCpuTotalTicks() != 6610 || resp.GetHost().GetCpuCount() != 2 {
		t.Fatalf("yanıt %v", resp)
	}
	if len(resp.GetApps()) != 0 || resp.GetAppsMeasured() {
		t.Fatalf("uygulama istenmedi ama %v / %v döndü", resp.GetApps(), resp.GetAppsMeasured())
	}
}

func TestResourceUsageAggregatesReplicas(t *testing.T) {
	fe := &fakeExec{stats: []*kadranv1.ContainerUsage{
		usage("blog", "r1", 0, 5),
		usage("api", "r2", 0, 7),
		usage("blog", "r1", 1, 3),
	}}
	resp, err := resourceServer(t, fe).GetResourceUsage(context.Background(), &kadranv1.GetResourceUsageRequest{IncludeApps: true})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.GetAppsMeasured() || len(resp.GetApps()) != 2 {
		t.Fatalf("ölçüldü=%v, %d uygulama", resp.GetAppsMeasured(), len(resp.GetApps()))
	}
	api, blog := resp.GetApps()[0], resp.GetApps()[1]
	if api.GetAppId() != "api" || blog.GetAppId() != "blog" {
		t.Fatalf("sıra %s, %s — app_id'ye göre sıralı olmalı", api.GetAppId(), blog.GetAppId())
	}
	want := &kadranv1.AppUsage{
		AppId: "blog", Containers: 2, CpuUsageNs: 8, MemoryBytes: 80, MemoryLimitBytes: 800,
		NetRxBytes: 10, NetTxBytes: 12, DiskReadBytes: 14, DiskWriteBytes: 16, Tasks: 8,
	}
	if !proto.Equal(blog, want) {
		t.Fatalf("blog %v\nbeklenen %v", blog, want)
	}
	if api.GetContainers() != 1 || api.GetCpuUsageNs() != 7 {
		t.Fatalf("api %v", api)
	}
}

// Executor kapalıyken host yine gösterilir; uygulamalar "ölçülemedi".
// RPC'nin kendisi hata vermez: ekranın yarısı için bütün ekran boşalmasın.
func TestResourceUsageWithoutExecutor(t *testing.T) {
	fe := &fakeExec{statsErr: errors.New("executor kapalı")}
	resp, err := resourceServer(t, fe).GetResourceUsage(context.Background(), &kadranv1.GetResourceUsageRequest{IncludeApps: true})
	if err != nil {
		t.Fatalf("hata döndü: %v", err)
	}
	if resp.GetHost().GetCpuTotalTicks() != 6610 || resp.GetAppsMeasured() || len(resp.GetApps()) != 0 {
		t.Fatalf("yanıt %v", resp)
	}
}

// Bellek executor'dan gelmiyor (ProcSubset=pid, K-166); kadrand dolduruyor.
func TestSystemInfoMemoryFromDaemon(t *testing.T) {
	fe := &fakeExec{hostInfo: &kadranv1.HostInfo{CpuCount: 2, DiskTotalBytes: 9}}
	srv := resourceServer(t, fe)
	srv.host = fakeHost{total: 4 << 30, available: 1 << 30, memOK: true}
	resp, err := srv.GetSystemInfo(context.Background(), &kadranv1.GetSystemInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	h := resp.GetHost()
	if h.GetMemoryTotalBytes() != 4<<30 || h.GetMemoryAvailableBytes() != 1<<30 || h.GetDiskTotalBytes() != 9 {
		t.Fatalf("host %v", h)
	}

	srv = resourceServer(t, &fakeExec{hostInfo: &kadranv1.HostInfo{CpuCount: 2}})
	srv.host = fakeHost{}
	resp, _ = srv.GetSystemInfo(context.Background(), &kadranv1.GetSystemInfoRequest{})
	if resp.GetHost() == nil || resp.GetHost().GetMemoryTotalBytes() != 0 {
		t.Fatal("okunamayan bellek uydurulmamalı")
	}
}
