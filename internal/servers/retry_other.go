//go:build !windows

package servers

// retryable: POSIX'te rename açık dosyanın üzerine de yazar, okuma da
// engellenmez; yeniden denenecek hata yok.
func retryable(error) bool { return false }
