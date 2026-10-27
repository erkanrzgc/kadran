package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ── Kökün kaçış yolu: TOTP sıfırlama (K-155) ──────────────────────────
//
// Telefon da kurtarma kodları da kaybolunca TOTP'yi yalnız sunucudaki root
// kapatabilir. Yetki anahtar işlemlerindeki gibi: root'a SSH ya da hedef
// kullanıcının parolasız sudo'su. kadrand root'u reddediyor ve veritabanı
// kadran'ın; betik onu kadran kullanıcısı olarak, temiz bir ortamla
// çalıştırıyor.

// totpResetNotInstalled, sunucuda kadran kullanıcısı yoksa betiğin çıkışı.
const totpResetNotInstalled = 3

const remoteTOTPReset = `set -euo pipefail
id -u kadran >/dev/null 2>&1 || { echo "user kadran does not exist — is the server installed? run kadran bootstrap first" >&2; exit 3; }
cd /
exec setpriv --reuid kadran --regid kadran --clear-groups -- \
    env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /usr/local/lib/kadran/kadrand -totp-reset`

// ResetTOTP, sunucuda TOTP'yi kapatır ve her oturumu geçersiz kılar;
// sunucunun çıktısı stdout'a aktarılır.
func ResetTOTP(ctx context.Context, opts KeyOptions, stdout io.Writer) error {
	if err := opts.validate(); err != nil {
		return err
	}
	var stderr bytes.Buffer
	o := opts.options()
	o.Stderr = &stderr
	code, err := sshRun(ctx, o, privileged(o, remoteTOTPReset), nil, stdout)
	if err != nil {
		return fmt.Errorf("totp reset: %w", err)
	}
	msg := strings.TrimSpace(stderr.String())
	switch code {
	case 0:
		return nil
	case totpResetNotInstalled:
		return errors.New(msg)
	default:
		return remoteFailure("totp reset", opts.Sudo, code, msg)
	}
}
