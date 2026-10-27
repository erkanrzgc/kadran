package main

import (
	"context"
	"fmt"
	"io"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/store"
)

// runTOTPReset, kökün kaçış yoludur (K-155): telefon da kurtarma kodları da
// kaybolunca TOTP'yi kapatır ve her oturumu geçersiz kılar. İş istasyonundan
// `kadran totp reset` onu root'a SSH ya da parolasız sudo ile, kadran
// kullanıcısı olarak çalıştırıyor.
//
// Daemon'u durdurmak gerekmiyor: oturum denetimi durumu her çağrıda
// veritabanından okuyor ve denetim eklemesi süreçler arası güvenli (BEGIN
// IMMEDIATE). İmza anahtarı da silindiği için eski jetonlar, TOTP yeniden
// açılsa bile geçmez.
func runTOTPReset(ctx context.Context, dbPath string, out io.Writer) error {
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer func() { _ = db.Close() }()

	st, err := db.TOTP(ctx)
	if err != nil {
		return fmt.Errorf("could not read the TOTP state: %w", err)
	}
	if !st.Enabled() && len(st.PendingSecret) == 0 {
		fmt.Fprintln(out, "TOTP is not on; nothing to reset")
		return nil
	}
	if err := db.DisableTOTP(ctx); err != nil {
		return fmt.Errorf("could not disable TOTP: %w", err)
	}

	params, err := audit.MarshalParams(nil)
	if err != nil {
		return fmt.Errorf("TOTP is disabled, but the audit record could not be encoded: %w", err)
	}
	if _, err := db.AppendAudit(ctx, audit.Record{
		Actor:      audit.Actor{Label: "root", Origin: "local"},
		Action:     "totp.reset",
		Target:     "totp",
		ParamsJSON: params,
		Outcome:    audit.OutcomeSuccess,
		Source:     audit.SourceDaemon,
	}); err != nil {
		return fmt.Errorf("TOTP is disabled, but the audit record could not be written: %w", err)
	}
	fmt.Fprintln(out, "TOTP disabled; every session is invalid")
	return nil
}
