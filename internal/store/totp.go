package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── İki aşamalı giriş (K-155) ─────────────────────────────────────────
//
// Durum her çağrıda buradan okunuyor, kadrand bellekte tutmuyor: kök
// kullanıcının `kadrand -totp-reset`'i ya da bir `DisableTotp` anında etki
// eder, kadrand'ı yeniden başlatmak gerekmez.

// ErrNoPendingEnrollment, onaylanacak bir kurulum olmadığını söyler.
var ErrNoPendingEnrollment = errors.New("no pending TOTP enrollment")

// TOTPState, sunucunun TOTP durumudur.
type TOTPState struct {
	Secret           []byte
	ConfirmedAt      time.Time
	LastStep         int64
	TokenKey         []byte
	Generation       int64
	PendingSecret    []byte
	PendingCreatedAt time.Time
}

// Enabled, TOTP'nin açık (etkin sırrın var) olduğunu söyler. Yalnız
// onay bekleyen bir kurulum TOTP'yi AÇMAZ.
func (t TOTPState) Enabled() bool { return len(t.Secret) > 0 }

func unixOrZero(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 == 0 {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0)
}

// TOTP, durumu okur; satır yoksa sıfır değer (kapalı) döner.
func (s *Store) TOTP(ctx context.Context) (TOTPState, error) {
	var st TOTPState
	var confirmed, pendingAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT secret, confirmed_at, last_step, token_key, generation,
		       pending_secret, pending_created_at
		FROM totp WHERE id = 1`).Scan(
		&st.Secret, &confirmed, &st.LastStep, &st.TokenKey, &st.Generation,
		&st.PendingSecret, &pendingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPState{}, nil
	}
	if err != nil {
		return TOTPState{}, fmt.Errorf("could not read the TOTP state: %w", err)
	}
	st.ConfirmedAt = unixOrZero(confirmed)
	st.PendingCreatedAt = unixOrZero(pendingAt)
	return st, nil
}

// BeginTOTPEnrollment, onay bekleyen bir kurulum yazar. Etkin sır (varsa)
// onaylanana kadar yerinde kalır; önceki bekleyen kurulumun üzerine yazılır.
func (s *Store) BeginTOTPEnrollment(ctx context.Context, pending []byte, now time.Time) error {
	if len(pending) == 0 {
		return errors.New("empty TOTP secret")
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO totp (id, pending_secret, pending_created_at) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			pending_secret = excluded.pending_secret,
			pending_created_at = excluded.pending_created_at`,
		pending, now.Unix()); err != nil {
		return fmt.Errorf("could not store the TOTP enrollment: %w", err)
	}
	return nil
}

// ConfirmTOTP, bekleyen kurulumu etkin yapar.
//
// Tek transaction: sır yer değiştirir, son adım yazılır (onay kodu bir
// daha kullanılamasın), kuşak artar ve imza anahtarı yenilenir (eski
// jetonların hepsi düşer), kurtarma kodları değiştirilir.
func (s *Store) ConfirmTOTP(
	ctx context.Context, step int64, tokenKey []byte, recovery [][32]byte, now time.Time,
) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("could not start a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE totp SET
			secret = pending_secret, confirmed_at = ?, last_step = ?,
			generation = generation + 1, token_key = ?,
			pending_secret = NULL, pending_created_at = NULL
		WHERE id = 1 AND pending_secret IS NOT NULL`,
		now.Unix(), step, tokenKey)
	if err != nil {
		return 0, fmt.Errorf("could not confirm the TOTP enrollment: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, fmt.Errorf("could not confirm the TOTP enrollment: %w", err)
	} else if n == 0 {
		return 0, ErrNoPendingEnrollment
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM totp_recovery`); err != nil {
		return 0, fmt.Errorf("could not replace the recovery codes: %w", err)
	}
	for _, h := range recovery {
		if _, err := tx.ExecContext(ctx, `INSERT INTO totp_recovery (hash) VALUES (?)`, h[:]); err != nil {
			return 0, fmt.Errorf("could not store a recovery code: %w", err)
		}
	}

	var gen int64
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM totp WHERE id = 1`).Scan(&gen); err != nil {
		return 0, fmt.Errorf("could not read the TOTP generation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("could not confirm the TOTP enrollment: %w", err)
	}
	return gen, nil
}

// AcceptTOTPStep, adımı son kabul edilen adım yapar; yalnız gerçekten
// ilerlediyse true döner. Koşullu güncelleme sayesinde aynı kodla iki
// eşzamanlı girişten yalnız biri kazanır.
func (s *Store) AcceptTOTPStep(ctx context.Context, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE totp SET last_step = ?
		WHERE id = 1 AND secret IS NOT NULL AND last_step < ?`, step, step)
	if err != nil {
		return false, fmt.Errorf("could not record the TOTP step: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("could not record the TOTP step: %w", err)
	}
	return n == 1, nil
}

// UseRecoveryCode, kullanılmamış bir kurtarma kodunu kullanılmış yapar.
func (s *Store) UseRecoveryCode(ctx context.Context, hash [32]byte, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE totp_recovery SET used_at = ? WHERE hash = ? AND used_at IS NULL`,
		now.Unix(), hash[:])
	if err != nil {
		return false, fmt.Errorf("could not use the recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("could not use the recovery code: %w", err)
	}
	return n == 1, nil
}

// RecoveryRemaining, kullanılmamış kurtarma kodu sayısıdır.
func (s *Store) RecoveryRemaining(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM totp_recovery WHERE used_at IS NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("could not count the recovery codes: %w", err)
	}
	return n, nil
}

// DisableTOTP, TOTP'yi kapatır: sır, kurtarma kodları ve sayaçlar silinir.
func (s *Store) DisableTOTP(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not start a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{`DELETE FROM totp`, `DELETE FROM totp_recovery`, `DELETE FROM totp_failures`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("could not disable TOTP: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("could not disable TOTP: %w", err)
	}
	return nil
}

// LoginFailure, bir anahtarın ardışık başarısız girişleridir.
type LoginFailure struct {
	Fingerprint string
	Count       int
	LockedUntil time.Time
}

// ErrLoginLocked, anahtarın şu an kilitli olduğunu söyler; deneme
// başlatılmadı, sayaç büyümedi.
var ErrLoginLocked = errors.New("sign-in locked")

// ReserveLoginAttempt, bir giriş denemesini KOD DENENMEDEN ÖNCE sayar.
//
// ── Neden önce sayılıyor ────────────────────────────────────────────
//
// "Kilitli mi bak → kodu dene → başarısızsa yaz" üç ayrı adım olsaydı,
// aynı anahtarla paralel açılan bağlantıların hepsi "kilitli değil" görür
// ve 5'ten fazla kod denerdi. Okuma ve sayma tek transaction'da (DSN:
// BEGIN IMMEDIATE, süreçler arası da sıralı): kilitliyse ErrLoginLocked
// döner ve sayaç BÜYÜMEZ; değilse deneme sayılır ve totp.LockDuration'ın
// kilidi şimdiden kurulur. Doğru kod çıkarsa çağıran ClearLoginFailures
// ile temizler.
func (s *Store) ReserveLoginAttempt(ctx context.Context, fp string, now time.Time) (LoginFailure, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginFailure{}, fmt.Errorf("could not start a transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	f := LoginFailure{Fingerprint: fp}
	var until int64
	err = tx.QueryRowContext(ctx,
		`SELECT count, locked_until FROM totp_failures WHERE fingerprint = ?`, fp).Scan(&f.Count, &until)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LoginFailure{}, fmt.Errorf("could not read the failed sign-ins: %w", err)
	}
	if until > now.Unix() {
		f.LockedUntil = time.Unix(until, 0)
		return f, ErrLoginLocked
	}

	f.Count++
	until = 0
	if d := totp.LockDuration(f.Count); d > 0 {
		f.LockedUntil = now.Add(d)
		until = f.LockedUntil.Unix()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO totp_failures (fingerprint, count, locked_until, last_failure_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET
			count = excluded.count, locked_until = excluded.locked_until,
			last_failure_at = excluded.last_failure_at`,
		fp, f.Count, until, now.Unix()); err != nil {
		return LoginFailure{}, fmt.Errorf("could not record the sign-in attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LoginFailure{}, fmt.Errorf("could not record the sign-in attempt: %w", err)
	}
	return f, nil
}

// LoginFailureFor, anahtarın sayacını okur; yoksa sıfır değer.
func (s *Store) LoginFailureFor(ctx context.Context, fp string) (LoginFailure, error) {
	f := LoginFailure{Fingerprint: fp}
	var until sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT count, locked_until FROM totp_failures WHERE fingerprint = ?`, fp).Scan(&f.Count, &until)
	if errors.Is(err, sql.ErrNoRows) {
		return f, nil
	}
	if err != nil {
		return LoginFailure{}, fmt.Errorf("could not read the failed sign-ins: %w", err)
	}
	f.LockedUntil = unixOrZero(until)
	return f, nil
}

// ClearLoginFailures, başarılı girişten sonra sayacı siler.
func (s *Store) ClearLoginFailures(ctx context.Context, fp string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM totp_failures WHERE fingerprint = ?`, fp); err != nil {
		return fmt.Errorf("could not clear the failed sign-ins: %w", err)
	}
	return nil
}

// LockedLogins, şu an kilitli olan anahtarlardır.
func (s *Store) LockedLogins(ctx context.Context, now time.Time) ([]LoginFailure, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT fingerprint, count, locked_until FROM totp_failures
		WHERE locked_until > ? ORDER BY fingerprint`, now.Unix())
	if err != nil {
		return nil, fmt.Errorf("could not list locked keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []LoginFailure
	for rows.Next() {
		var f LoginFailure
		var until sql.NullInt64
		if err := rows.Scan(&f.Fingerprint, &f.Count, &until); err != nil {
			return nil, fmt.Errorf("could not read a locked key: %w", err)
		}
		f.LockedUntil = unixOrZero(until)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("could not list locked keys: %w", err)
	}
	return out, nil
}
