package hostmetrics

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Reader, sayaçların okunduğu kökler. Testler örnek dosyalara yönlendirir.
type Reader struct {
	Proc string
	Sys  string
	// StateDir, disk alanı ve inode için ölçülen dizin: Kadran'ın durum
	// dizini (HostInfo'daki disk alanıyla aynı birim, common.proto).
	StateDir string
}

// Default, hostun gerçek yolları.
func Default() Reader {
	return Reader{Proc: "/proc", Sys: "/sys", StateDir: "/var/lib/kadran"}
}

// Read, bütün sayaçları okur. Her dosya ayrı: okunamayan dosya yalnız
// kendi alanlarını boş bırakır, sıfır yazmaz.
func (r Reader) Read() *kadranv1.HostUsage {
	h := &kadranv1.HostUsage{CpuCount: uint32(runtime.NumCPU())} //nolint:gosec // çekirdek sayısı uint32'ye sığar
	r.readCPU(h)
	r.readMemory(h)
	r.readLoad(h)
	r.readNet(h)
	r.readDiskIO(h)
	r.readPressure(h)
	if b, err := os.ReadFile(filepath.Join(r.Proc, "uptime")); err == nil {
		if n, ok := parseUptime(string(b)); ok {
			h.UptimeSeconds = &n
		}
	}
	readStatfs(r.StateDir, h)
	return h
}

// Memory, toplam ve kullanılabilir bellek (GetSystemInfo için).
func (r Reader) Memory() (total, available uint64, ok bool) {
	m, found := r.meminfo()
	if !found {
		return 0, 0, false
	}
	total, ok1 := m["MemTotal"]
	available, ok2 := m["MemAvailable"]
	return total, available, ok1 && ok2
}

func (r Reader) meminfo() (map[string]uint64, bool) {
	f, err := os.Open(filepath.Join(r.Proc, "meminfo"))
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	return parseMeminfo(f), true
}

func (r Reader) readCPU(h *kadranv1.HostUsage) {
	f, err := os.Open(filepath.Join(r.Proc, "stat"))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	c, ok := parseStat(f)
	if !ok {
		return
	}
	h.CpuTotalTicks, h.CpuIdleTicks, h.CpuIowaitTicks, h.CpuStealTicks = &c.total, &c.idle, &c.iowait, &c.steal
	// cpuN satırları hostun çekirdekleri; runtime.NumCPU sürecin
	// eşlendiği çekirdekler (CPUAffinity varsa daha az).
	if c.cores > 0 {
		h.CpuCount = c.cores
	}
}

func (r Reader) readMemory(h *kadranv1.HostUsage) {
	m, ok := r.meminfo()
	if !ok {
		return
	}
	set := func(key string, dst **uint64) {
		if v, found := m[key]; found {
			*dst = &v
		}
	}
	set("MemTotal", &h.MemoryTotalBytes)
	set("MemAvailable", &h.MemoryAvailableBytes)
	set("SwapTotal", &h.SwapTotalBytes)
	set("SwapFree", &h.SwapFreeBytes)
}

func (r Reader) readLoad(h *kadranv1.HostUsage) {
	b, err := os.ReadFile(filepath.Join(r.Proc, "loadavg"))
	if err != nil {
		return
	}
	if l1, l5, l15, tasks, ok := parseLoadavg(string(b)); ok {
		h.Load1, h.Load5, h.Load15, h.Tasks = &l1, &l5, &l15, &tasks
	}
}

func (r Reader) readNet(h *kadranv1.HostUsage) {
	f, err := os.Open(filepath.Join(r.Proc, "net", "dev"))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if rx, tx, ok := parseNetDev(f); ok {
		h.NetRxBytes, h.NetTxBytes = &rx, &tx
	}
}

func (r Reader) readDiskIO(h *kadranv1.HostUsage) {
	f, err := os.Open(filepath.Join(r.Proc, "diskstats"))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if rd, wr, ok := parseDiskstats(f, r.isDisk); ok {
		h.DiskReadBytes, h.DiskWriteBytes = &rd, &wr
	}
}

// virtualDisk: /sys/block'ta görünen ama G/Ç'yi altındaki diskle iki kez
// sayan ya da diske hiç gitmeyen aygıtlar.
var virtualDisk = []string{"loop", "ram", "dm-", "zram", "sr", "md", "nbd"}

// isDisk: /sys/block yalnız tam aygıtları listeler (bölümler orada yok).
func (r Reader) isDisk(name string) bool {
	for _, p := range virtualDisk {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	_, err := os.Stat(filepath.Join(r.Sys, "block", name))
	return err == nil
}

func (r Reader) readPressure(h *kadranv1.HostUsage) {
	for name, dst := range map[string]**float64{"cpu": &h.CpuPressure, "memory": &h.MemoryPressure, "io": &h.IoPressure} {
		f, err := os.Open(filepath.Join(r.Proc, "pressure", name))
		if err != nil {
			continue
		}
		if v, ok := parsePressure(f); ok {
			*dst = &v
		}
		_ = f.Close()
	}
}
