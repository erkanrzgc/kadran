//go:build linux

package github

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

var (
	errStatePath = errors.New("github: state path must be absolute and clean")
	errStateDir  = errors.New("github: state directories must be owned by root or the service user, not symlinks and not writable by others")
	errStateOpen = errors.New("github: could not securely open the state file")
	errStatePerm = errors.New("github: state file must be a regular 0600 file of the service user, at most 64 KiB")
)

// checkStateDirs yoldaki her üst dizini doğrular: symlink değil, grup ya da
// başkaları tarafından yazılamaz, sahibi root ya da süreç kullanıcısı. Sticky
// bitli root dizinler (/tmp) başkasının girdiyi değiştirmesine izin vermediği
// için kabul edilir; ama yalnız dolaylı üst dizinler için: dosyayı doğrudan
// içeren dizin sticky olsa bile yazılamaz olmalıdır.
func checkStateDirs(path string) error {
	euid := uint32(os.Geteuid())
	parent := filepath.Dir(path)
	for dir := parent; ; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("github: state directory is missing: %w", os.ErrNotExist)
			}
			return errStateDir
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return errStateDir
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && st.Uid != euid) {
			return errStateDir
		}
		if fi.Mode().Perm()&0o022 != 0 && !(dir != parent && fi.Mode()&os.ModeSticky != 0 && st.Uid == 0) {
			return errStateDir
		}
		if dir == filepath.Dir(dir) {
			return nil
		}
	}
}

// readStateFile durum dosyasını güvenle okur: mutlak ve temiz yol, güvenilir
// üst dizinler, O_NOFOLLOW, sahibi süreç kullanıcısı, normal dosya, kip 0600, ≤ 64 KiB.
func readStateFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errStatePath
	}
	if err := checkStateDirs(path); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, fmt.Errorf("github: state file is missing: %w", os.ErrNotExist)
		}
		return nil, errStateOpen
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o777 != 0o600 || st.Size > maxStateFile {
		return nil, errStatePerm
	}
	data, err := io.ReadAll(io.LimitReader(f, maxStateFile+1))
	if err != nil || len(data) > maxStateFile {
		return nil, errStateTooLarge
	}
	return data, nil
}

// syncDir dizin girdisinin (rename) diske inmesini sağlar.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
