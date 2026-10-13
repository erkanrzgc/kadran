//go:build linux

package exec

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func readPrivateGitFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("private Git credential path must be absolute and clean")
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0o022 != 0 {
			return nil, errors.New("private Git credential directories must be root-owned and not writable by others")
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 {
			return nil, errors.New("private Git credential directories must be root-owned")
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("could not securely open private Git credentials")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o777 != 0o600 || st.Size > 64<<10 {
		return nil, errors.New("private Git credentials must be a root-owned regular 0600 file of at most 64 KiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if err != nil || len(data) > 64<<10 {
		return nil, errors.New("could not read bounded private Git credentials")
	}
	return data, nil
}
