//go:build windows

package servers

import (
	"errors"
	"io/fs"
	"syscall"
)

// errSharingViolation, ERROR_SHARING_VIOLATION (32): dosya o an başka bir
// süreçte açık. Okuma, başka bir sürecin rename'iyle çakışınca bunu alıyor;
// rename de okunan dosyanın üzerine ERROR_ACCESS_DENIED alıyor (ölçüldü,
// K-162).
const errSharingViolation = syscall.Errno(32)

func retryable(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errSharingViolation)
}
