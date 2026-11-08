//go:build linux

package exec

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/erkanrzgc/kadran/internal/dockerdrv"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// collectHostInfo, sunucunun çekirdek ve donanım bilgisini toplar.
//
// Tamamı salt okunur ve /proc üzerinden yapılır; hiçbir dış komut
// çalıştırılmaz. Ayrıcalıklı süreçte alt süreç doğurmak, tam olarak
// kaçındığımız şeydir.
func collectHostInfo(ctx context.Context, probe *dockerdrv.Client) *kadranv1.HostInfo {
	info := &kadranv1.HostInfo{
		CpuCount: uint32(runtime.NumCPU()),
		Os:       readOSPrettyName(),
	}

	var uts unix.Utsname
	if err := unix.Uname(&uts); err == nil {
		info.KernelVersion = nullTerminated(uts.Release[:])
		info.Architecture = nullTerminated(uts.Machine[:])
	}

	// Bellek burada OKUNMUYOR: bu birimde ProcSubset=pid var ve
	// /proc/meminfo görünmüyor. Okuma kodu yıllarca sessizce 0 döndü
	// (K-166); alanları artık yetkisiz kadrand dolduruyor
	// (internal/hostmetrics). Ayrıcalıklı yüzey bir okuyucu kadar küçüldü.

	// Disk: durum dizinini taşıyan dosya sistemi. Gerekçe common.proto'da.
	//
	// Yol olarak DefaultVolumeRoot kullanılıyor, yeni bir sabit
	// tanımlanmıyor: hacim kökü /var/lib/kadran altında, yani AYNI
	// dosya sisteminde. statfs zaten birimi ölçüyor, dizini değil.
	//
	// Bavail kullanılıyor, Bfree DEĞİL: ikisinin farkı root'a ayrılmış
	// yedek bloklar. kadrand root DEĞİL, dolayısıyla o yedeği
	// kullanamaz. Bfree raporlamak, ulaşılamayan alanı boş göstermek
	// olurdu.
	var st unix.Statfs_t
	if err := unix.Statfs(DefaultVolumeRoot, &st); err == nil {
		bs := uint64(st.Bsize) //nolint:gosec // blok boyutu negatif olamaz
		info.DiskTotalBytes = st.Blocks * bs
		info.DiskAvailableBytes = st.Bavail * bs
	}

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
