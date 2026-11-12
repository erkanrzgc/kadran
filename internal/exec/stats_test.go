package exec

import (
	"context"
	"errors"
	"testing"

	"github.com/erkanrzgc/kadran/internal/dockerdrv"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

type fakeStats struct {
	list    []dockerdrv.Container
	listErr error
	usage   map[string]dockerdrv.Usage
	asked   []string
}

func (f *fakeStats) ContainerList(context.Context, string) ([]dockerdrv.Container, error) {
	return f.list, f.listErr
}

func (f *fakeStats) ContainerStats(_ context.Context, id string) (dockerdrv.Usage, error) {
	f.asked = append(f.asked, id)
	u, ok := f.usage[id]
	if !ok {
		return dockerdrv.Usage{}, errors.New("no such container")
	}
	return u, nil
}

func TestCollectStatsOnlyRunningContainers(t *testing.T) {
	src := &fakeStats{
		list: []dockerdrv.Container{
			{ID: "a", AppID: "blog", ReleaseID: "r1", Replica: 1, State: "running"},
			{ID: "b", AppID: "blog", ReleaseID: "r0", Replica: 0, State: "exited"},
			// Listeden sonra durdu: istatistik hata veriyor, atlanır.
			{ID: "c", AppID: "api", ReleaseID: "r9", Replica: 0, State: "running"},
		},
		usage: map[string]dockerdrv.Usage{"a": {CPUNanos: 5, MemoryBytes: 6, MemoryLimit: 7, RxBytes: 8, TxBytes: 9, ReadBytes: 10, WriteBytes: 11, Tasks: 12}},
	}
	got, err := collectStats(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.asked) != 2 || src.asked[0] != "a" || src.asked[1] != "c" {
		t.Fatalf("istatistik sorulanlar %v — durmuş konteyner sorulmamalı", src.asked)
	}
	if len(got) != 1 {
		t.Fatalf("%d sonuç, 1 olmalı", len(got))
	}
	want := &kadranv1.ContainerUsage{
		Ref:        &kadranv1.ContainerRef{Release: &kadranv1.ReleaseRef{AppId: "blog", ReleaseId: "r1"}, Replica: 1},
		CpuUsageNs: 5, MemoryBytes: 6, MemoryLimitBytes: 7, NetRxBytes: 8, NetTxBytes: 9, DiskReadBytes: 10, DiskWriteBytes: 11, Tasks: 12,
	}
	g := got[0]
	if g.GetRef().GetRelease().GetAppId() != "blog" || g.GetRef().GetRelease().GetReleaseId() != "r1" || g.GetRef().GetReplica() != 1 ||
		g.GetCpuUsageNs() != want.CpuUsageNs || g.GetMemoryBytes() != want.MemoryBytes || g.GetMemoryLimitBytes() != want.MemoryLimitBytes ||
		g.GetNetRxBytes() != want.NetRxBytes || g.GetNetTxBytes() != want.NetTxBytes ||
		g.GetDiskReadBytes() != want.DiskReadBytes || g.GetDiskWriteBytes() != want.DiskWriteBytes || g.GetTasks() != want.Tasks {
		t.Fatalf("got %v\nwant %v", g, want)
	}
}

func TestCollectStatsPropagatesListError(t *testing.T) {
	if _, err := collectStats(context.Background(), &fakeStats{listErr: errors.New("docker down")}); err == nil {
		t.Fatal("liste hatası yutuldu")
	}
}

func TestContainerStatsWithoutDockerIsAnError(t *testing.T) {
	if _, err := newTestServer(t).ContainerStats(context.Background(), &kadranv1.ContainerStatsRequest{}); err == nil {
		t.Fatal("Docker'a ulaşılamazken boş liste dönmemeli")
	}
}
