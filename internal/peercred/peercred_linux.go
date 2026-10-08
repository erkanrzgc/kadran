//go:build linux

package peercred

import (
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// FromConn, bağlantının diğer ucundaki sürecin kimliğini çekirdekten okur.
//
// SO_PEERCRED, bağlantı kurulduğu ANDAKİ kimliği döndürür ve sonradan
// değiştirilemez. Çağıran setuid yapsa bile burada okunan değer bağlantı
// anındaki değerdir — kimlik bilgisi yarışına (credential race) kapalıdır.
func FromConn(c net.Conn) (Cred, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return Cred{}, fmt.Errorf("%w: %T", ErrNotUnixConn, c)
	}

	raw, err := uc.SyscallConn()
	if err != nil {
		return Cred{}, fmt.Errorf("peercred: could not get the syscall connection: %w", err)
	}

	var (
		ucred   *unix.Ucred
		sockErr error
	)
	// Control, fd kullanımda kalırken güvenli erişim sağlar; fd'yi
	// doğrudan almak dosya tanıtıcısının altımızdan kapanmasına açıktır.
	if ctlErr := raw.Control(func(fd uintptr) {
		ucred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); ctlErr != nil {
		return Cred{}, fmt.Errorf("peercred: could not access the file descriptor: %w", ctlErr)
	}
	if sockErr != nil {
		return Cred{}, fmt.Errorf("peercred: could not read SO_PEERCRED: %w", sockErr)
	}

	return Cred{PID: ucred.Pid, UID: ucred.Uid, GID: ucred.Gid}, nil
}
