//go:build linux

package exec

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/erkanrzgc/panely/internal/dockerdrv"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// collectHostInfo, sunucunun çekirdek ve donanım bilgisini toplar.
//
// Tamamı salt okunur ve /proc üzerinden yapılır; hiçbir dış komut
// çalıştırılmaz. Ayrıcalıklı süreçte alt süreç doğurmak, tam olarak
// kaçındığımız şeydir.
func collectHostInfo(ctx context.Context, probe *dockerdrv.Client) *panelyv1.HostInfo {
	info := &panelyv1.HostInfo{
		CpuCount: uint32(runtime.NumCPU()),
		Os:       readOSPrettyName(),
	}

	var uts unix.Utsname
	if err := unix.Uname(&uts); err == nil {
		info.KernelVersion = nullTerminated(uts.Release[:])
		info.Architecture = nullTerminated(uts.Machine[:])
	}

	total, available := readMemInfo()
	info.MemoryTotalBytes = total
	info.MemoryAvailableBytes = available

	// Docker'a ulaşılamaması hata değildir: Faz 0'da Docker henüz
	// gerekli değil ve boş sürüm "kurulu değil" anlamına gelir.
	if v, err := probe.Ping(ctx); err == nil {
		info.DockerVersion = v
	}

	return info
}

// nullTerminated, C tarzı sabit boyutlu bir tamponu Go dizesine çevirir.
func nullTerminated(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// readOSPrettyName, /etc/os-release içindeki PRETTY_NAME değerini okur.
func readOSPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		value, ok := strings.CutPrefix(line, "PRETTY_NAME=")
		if !ok {
			continue
		}
		return strings.Trim(value, `"`)
	}
	return runtime.GOOS
}

// readMemInfo, /proc/meminfo'dan toplam ve kullanılabilir belleği okur.
//
// MemAvailable, MemFree'den daha anlamlıdır: çekirdeğin geri
// kazanabileceği önbelleği de hesaba katar. Kaynak kotası kararlarında
// bakılması gereken değer budur.
func readMemInfo() (total, available uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			total = parseKilobytes(value)
		case "MemAvailable":
			available = parseKilobytes(value)
		}
		if total > 0 && available > 0 {
			break
		}
	}
	return total, available
}

// parseKilobytes, "  16316420 kB" biçimindeki değeri bayta çevirir.
func parseKilobytes(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	kb, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return kb * 1024
}
