package dockerdrv

import (
	"context"
	"strings"
	"testing"
)

// Docker 29'un cgroup v2'de döndürdüğü yanıtın kullanılan alanları.
const statsV2 = `{
  "cpu_stats": {"cpu_usage": {"total_usage": 123456789}},
  "memory_stats": {"usage": 50000000, "limit": 536870912, "stats": {"inactive_file": 8000000, "anon": 30000000}},
  "pids_stats": {"current": 7},
  "networks": {"eth0": {"rx_bytes": 1000, "tx_bytes": 2000}, "eth1": {"rx_bytes": 30, "tx_bytes": 40}},
  "blkio_stats": {"io_service_bytes_recursive": [
    {"major": 252, "minor": 0, "op": "read", "value": 4096},
    {"major": 252, "minor": 0, "op": "write", "value": 8192},
    {"major": 253, "minor": 0, "op": "Read", "value": 100}
  ]}
}`

func TestStatsReadsCountersLikeDockerStats(t *testing.T) {
	f := newFakeDocker(t)
	f.stats = map[string]string{"abc": statsV2}

	u, err := f.client("/var/lib/kadran/volumes").ContainerStats(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := Usage{
		CPUNanos: 123456789,
		// docker stats ile aynı: inactive_file kullanılan bellek değil.
		MemoryBytes: 50000000 - 8000000,
		MemoryLimit: 536870912,
		RxBytes:     1030,
		TxBytes:     2040,
		// İşlem adı cgroup v1'de büyük harfli ("Read"), v2'de küçük.
		ReadBytes:  4196,
		WriteBytes: 8192,
		Tasks:      7,
	}
	if u != want {
		t.Fatalf("got %+v\nwant %+v", u, want)
	}

	r := f.op(t)
	if !strings.HasSuffix(r.Path, "/containers/abc/stats") || !strings.Contains(r.Query, "stream=false") || !strings.Contains(r.Query, "one-shot=true") {
		t.Fatalf("istek %s?%s — tek seferlik istatistik olmalı", r.Path, r.Query)
	}
}

func TestStatsCacheLargerThanUsageKeepsUsage(t *testing.T) {
	f := newFakeDocker(t)
	f.stats = map[string]string{"abc": `{"memory_stats": {"usage": 100, "stats": {"inactive_file": 500}}}`}
	u, err := f.client("/v").ContainerStats(context.Background(), "abc")
	if err != nil || u.MemoryBytes != 100 {
		t.Fatalf("bellek %d, %v — eksi çıkmamalı", u.MemoryBytes, err)
	}
}

func TestStatsPropagatesMissingContainer(t *testing.T) {
	f := newFakeDocker(t)
	if _, err := f.client("/v").ContainerStats(context.Background(), "yok"); err == nil {
		t.Fatal("olmayan konteyner hata vermeli")
	}
}
