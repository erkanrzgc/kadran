//go:build linux

package hostmetrics

import (
	"golang.org/x/sys/unix"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// readStatfs, durum dizinini taşıyan dosya sisteminin alanı ve inode'ları.
// Bavail, Bfree DEĞİL: kadrand root değil, root'a ayrılan blokları
// kullanamaz (gerekçe internal/exec/hostinfo_linux.go). Inode biterse
// disk boş görünürken yazma durur; bu yüzden ayrıca raporlanıyor.
func readStatfs(dir string, h *kadranv1.HostUsage) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return
	}
	bs := uint64(st.Bsize) //nolint:gosec // blok boyutu negatif olamaz
	total, avail := st.Blocks*bs, st.Bavail*bs
	files, free := st.Files, st.Ffree
	h.DiskTotalBytes, h.DiskAvailableBytes = &total, &avail
	h.InodesTotal, h.InodesFree = &files, &free
}
