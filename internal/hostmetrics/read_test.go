package hostmetrics

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fixture() Reader {
	return Reader{Proc: filepath.Join("testdata", "proc"), Sys: filepath.Join("testdata", "sys"), StateDir: "testdata"}
}

func TestReadFromFixture(t *testing.T) {
	h := fixture().Read()

	if h.GetCpuCount() != 2 {
		t.Errorf("çekirdek %d, 2 olmalı (cpuN satırları)", h.GetCpuCount())
	}
	// İlk sekiz sütun: user nice system idle iowait irq softirq steal.
	// guest ve guest_nice zaten user'ın içinde, eklenmez.
	if h.CpuTotalTicks == nil || h.GetCpuTotalTicks() != 6610 {
		t.Errorf("toplam tık %v, 6610 olmalı", h.CpuTotalTicks)
	}
	if h.GetCpuIdleTicks() != 5200 || h.GetCpuIowaitTicks() != 200 || h.GetCpuStealTicks() != 30 {
		t.Errorf("idle/iowait/steal %d/%d/%d", h.GetCpuIdleTicks(), h.GetCpuIowaitTicks(), h.GetCpuStealTicks())
	}

	if h.GetMemoryTotalBytes() != 4000000*1024 || h.GetMemoryAvailableBytes() != 2500000*1024 {
		t.Errorf("bellek %d/%d", h.GetMemoryTotalBytes(), h.GetMemoryAvailableBytes())
	}
	if h.GetSwapTotalBytes() != 1048576*1024 || h.GetSwapFreeBytes() != 524288*1024 {
		t.Errorf("swap %d/%d", h.GetSwapTotalBytes(), h.GetSwapFreeBytes())
	}

	if h.GetLoad1() != 0.52 || h.GetLoad5() != 0.38 || h.GetLoad15() != 0.30 || h.GetTasks() != 412 {
		t.Errorf("yük %v %v %v görev %d", h.GetLoad1(), h.GetLoad5(), h.GetLoad15(), h.GetTasks())
	}

	// lo, docker0, br-*, veth* hariç: eth0 + ens4.
	if h.GetNetRxBytes() != 5200000 || h.GetNetTxBytes() != 3100000 {
		t.Errorf("ağ %d/%d, 5200000/3100000 olmalı", h.GetNetRxBytes(), h.GetNetTxBytes())
	}

	// Yalnız vda: vda1 bölüm (/sys/block'ta yok), loop0 ve dm-0 sanal.
	if h.GetDiskReadBytes() != 400000*512 || h.GetDiskWriteBytes() != 600000*512 {
		t.Errorf("disk G/Ç %d/%d", h.GetDiskReadBytes(), h.GetDiskWriteBytes())
	}

	if h.GetCpuPressure() != 1.5 || h.GetMemoryPressure() != 0.25 || h.GetIoPressure() != 3.75 {
		t.Errorf("baskı %v %v %v", h.GetCpuPressure(), h.GetMemoryPressure(), h.GetIoPressure())
	}
	if h.GetUptimeSeconds() != 123456 {
		t.Errorf("açık kalma %d", h.GetUptimeSeconds())
	}

	if runtime.GOOS == "linux" {
		if h.GetDiskTotalBytes() == 0 || h.DiskAvailableBytes == nil || h.GetInodesTotal() == 0 || h.InodesFree == nil {
			t.Errorf("statfs alanları boş: %v", h)
		}
	} else if h.DiskTotalBytes != nil || h.InodesTotal != nil {
		t.Errorf("linux dışında disk alanı ölçülemez, alan boş kalmalı")
	}
}

// Her dosya ayrı: biri okunamazsa yalnız kendi alanları boş kalır, sıfır
// YAZILMAZ (bellek bir kez sessizce 0 raporlandı, K-166).
func TestMissingFileLeavesFieldsUnset(t *testing.T) {
	proc := t.TempDir()
	for _, name := range []string{"stat", "loadavg"} {
		b, err := os.ReadFile(filepath.Join("testdata", "proc", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proc, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := Reader{Proc: proc, Sys: filepath.Join("testdata", "sys"), StateDir: filepath.Join(proc, "yok")}.Read()

	if h.CpuTotalTicks == nil || h.Load1 == nil || h.Tasks == nil {
		t.Fatal("var olan dosyaların alanları dolmalı")
	}
	unset := map[string]bool{
		"memory": h.MemoryTotalBytes == nil && h.MemoryAvailableBytes == nil,
		"swap":   h.SwapTotalBytes == nil && h.SwapFreeBytes == nil,
		"net":    h.NetRxBytes == nil && h.NetTxBytes == nil,
		"diskio": h.DiskReadBytes == nil && h.DiskWriteBytes == nil,
		"psi":    h.CpuPressure == nil && h.MemoryPressure == nil && h.IoPressure == nil,
		"uptime": h.UptimeSeconds == nil,
		"statfs": h.DiskTotalBytes == nil && h.InodesTotal == nil,
	}
	for name, ok := range unset {
		if !ok {
			t.Errorf("%s: okunamayan dosya alanı doldurdu", name)
		}
	}
}

func TestMemoryHelper(t *testing.T) {
	total, available, ok := fixture().Memory()
	if !ok || total != 4000000*1024 || available != 2500000*1024 {
		t.Fatalf("Memory() = %d, %d, %v", total, available, ok)
	}
	if _, _, ok := (Reader{Proc: t.TempDir()}).Memory(); ok {
		t.Fatal("meminfo yokken ok dönmemeli")
	}
	// MemAvailable'ı olmayan çekirdek (3.14 öncesi): kullanılabilir 0 sanılıp
	// durum ekranı belleği tamamen dolu göstermesin.
	proc := t.TempDir()
	if err := os.WriteFile(filepath.Join(proc, "meminfo"), []byte("MemTotal: 4000000 kB\nMemFree: 500000 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := (Reader{Proc: proc}).Memory(); ok {
		t.Fatal("MemAvailable yokken ok dönmemeli")
	}
}

func TestDefaultPaths(t *testing.T) {
	d := Default()
	if d.Proc != "/proc" || d.Sys != "/sys" || d.StateDir != "/var/lib/kadran" {
		t.Fatalf("varsayılan yollar: %+v", d)
	}
}
