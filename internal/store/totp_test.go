package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var (
	sir1   = bytes.Repeat([]byte{1}, 20)
	sir2   = bytes.Repeat([]byte{2}, 20)
	jetonA = bytes.Repeat([]byte{7}, 32)
	simdi  = time.Unix(1_800_000_000, 0)
)

func kurtarmaOzetleri(b byte) [][32]byte {
	out := make([][32]byte, 10)
	for i := range out {
		out[i][0], out[i][1] = b, byte(i)
	}
	return out
}

func etkinlestir(t *testing.T, s *Store, sir []byte, step int64) int64 {
	t.Helper()
	ctx := context.Background()
	if err := s.BeginTOTPEnrollment(ctx, sir, simdi); err != nil {
		t.Fatal(err)
	}
	gen, err := s.ConfirmTOTP(ctx, step, jetonA, kurtarmaOzetleri(sir[0]), simdi)
	if err != nil {
		t.Fatal(err)
	}
	return gen
}

func TestTOTPStartsDisabled(t *testing.T) {
	st, err := newTestStore(t).TOTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled() {
		t.Fatal("yeni veritabanında TOTP açık görünüyor")
	}
}

func TestEnrollmentNeedsConfirm(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.BeginTOTPEnrollment(ctx, sir1, simdi); err != nil {
		t.Fatal(err)
	}
	st, err := s.TOTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled() || !bytes.Equal(st.PendingSecret, sir1) || !st.PendingCreatedAt.Equal(simdi) {
		t.Fatalf("onaysız kurulum: %+v", st)
	}

	gen, err := s.ConfirmTOTP(ctx, 42, jetonA, kurtarmaOzetleri(1), simdi)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.TOTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gen != 1 || st.Generation != 1 || !st.Enabled() || !bytes.Equal(st.Secret, sir1) ||
		st.PendingSecret != nil || st.LastStep != 42 || !bytes.Equal(st.TokenKey, jetonA) {
		t.Fatalf("onaydan sonra: gen=%d %+v", gen, st)
	}
	if n, err := s.RecoveryRemaining(ctx); err != nil || n != 10 {
		t.Fatalf("kalan kurtarma kodu %d (%v)", n, err)
	}
}

// TestReenrollKeepsActiveSecretUntilConfirm: yeniden kurulum onaylanana
// kadar eski sır geçerli; onayla birlikte kuşak artar ve eski kurtarma
// kodları geçersiz olur.
func TestReenrollKeepsActiveSecretUntilConfirm(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	etkinlestir(t, s, sir1, 10)

	if err := s.BeginTOTPEnrollment(ctx, sir2, simdi); err != nil {
		t.Fatal(err)
	}
	st, err := s.TOTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(st.Secret, sir1) || !bytes.Equal(st.PendingSecret, sir2) {
		t.Fatalf("yeniden kurulum etkin sırrı değiştirdi: %+v", st)
	}

	gen, err := s.ConfirmTOTP(ctx, 11, jetonA, kurtarmaOzetleri(2), simdi)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.TOTP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if gen != 2 || !bytes.Equal(st.Secret, sir2) {
		t.Fatalf("onaydan sonra gen=%d sır=%x", gen, st.Secret)
	}
	if ok, err := s.UseRecoveryCode(ctx, kurtarmaOzetleri(1)[0], simdi); err != nil || ok {
		t.Fatalf("eski kurtarma kodu hâlâ geçerli (%v, %v)", ok, err)
	}
	if ok, err := s.UseRecoveryCode(ctx, kurtarmaOzetleri(2)[0], simdi); err != nil || !ok {
		t.Fatalf("yeni kurtarma kodu geçmedi (%v, %v)", ok, err)
	}
}

func TestConfirmWithoutPending(t *testing.T) {
	_, err := newTestStore(t).ConfirmTOTP(context.Background(), 1, jetonA, kurtarmaOzetleri(1), simdi)
	if !errors.Is(err, ErrNoPendingEnrollment) {
		t.Fatalf("bekleyen kurulum yokken: %v", err)
	}
}

// TestAcceptTOTPStepIsOneShot: aynı kod iki terminalden aynı anda girilse
// bile yalnız biri kazanır; daha eski adımlar da geçmez.
func TestAcceptTOTPStepIsOneShot(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	etkinlestir(t, s, sir1, 100)
	for _, tc := range []struct {
		step int64
		want bool
	}{{101, true}, {101, false}, {100, false}, {102, true}} {
		got, err := s.AcceptTOTPStep(ctx, tc.step)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("adım %d: %v, beklenen %v", tc.step, got, tc.want)
		}
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	etkinlestir(t, s, sir1, 1)
	h := kurtarmaOzetleri(1)[3]
	if ok, err := s.UseRecoveryCode(ctx, h, simdi); err != nil || !ok {
		t.Fatalf("ilk kullanım (%v, %v)", ok, err)
	}
	if ok, err := s.UseRecoveryCode(ctx, h, simdi); err != nil || ok {
		t.Fatalf("ikinci kullanım kabul edildi (%v, %v)", ok, err)
	}
	if n, err := s.RecoveryRemaining(ctx); err != nil || n != 9 {
		t.Fatalf("kalan %d (%v)", n, err)
	}
}

func TestLoginFailuresLockAfterFive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const fp = "SHA256:aaa"
	var f LoginFailure
	var err error
	for i := 1; i <= 4; i++ {
		if f, err = s.RecordLoginFailure(ctx, fp, simdi); err != nil {
			t.Fatal(err)
		}
		if f.Count != i || !f.LockedUntil.IsZero() {
			t.Fatalf("%d. başarısızlık: %+v", i, f)
		}
	}
	if f, err = s.RecordLoginFailure(ctx, fp, simdi); err != nil || !f.LockedUntil.Equal(simdi.Add(time.Minute)) {
		t.Fatalf("5. başarısızlık: %+v (%v)", f, err)
	}
	if f, err = s.RecordLoginFailure(ctx, fp, simdi); err != nil || !f.LockedUntil.Equal(simdi.Add(2*time.Minute)) {
		t.Fatalf("6. başarısızlık: %+v (%v)", f, err)
	}
	locked, err := s.LockedLogins(ctx, simdi)
	if err != nil || len(locked) != 1 || locked[0].Fingerprint != fp {
		t.Fatalf("kilitli anahtarlar %+v (%v)", locked, err)
	}
	if other, err := s.LoginFailureFor(ctx, "SHA256:bbb"); err != nil || other.Count != 0 {
		t.Fatalf("başka anahtar etkilendi: %+v (%v)", other, err)
	}
	if err := s.ClearLoginFailures(ctx, fp); err != nil {
		t.Fatal(err)
	}
	if f, err = s.LoginFailureFor(ctx, fp); err != nil || f.Count != 0 || !f.LockedUntil.IsZero() {
		t.Fatalf("temizlendikten sonra %+v (%v)", f, err)
	}
}

// TestFailuresAreKeptAcrossReopen: kadrand'ı yeniden başlatmak kilidi
// sıfırlamamalı; sıfırlasaydı saldırgan her denemeden önce bunu beklerdi.
func TestFailuresAreKeptAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "kadran.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := s.RecordLoginFailure(ctx, "SHA256:aaa", simdi); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	f, err := s.LoginFailureFor(ctx, "SHA256:aaa")
	if err != nil || f.Count != 5 || !f.LockedUntil.Equal(simdi.Add(time.Minute)) {
		t.Fatalf("yeniden açılınca %+v (%v)", f, err)
	}
}

func TestDisableClearsEverything(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	etkinlestir(t, s, sir1, 1)
	if _, err := s.RecordLoginFailure(ctx, "SHA256:aaa", simdi); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableTOTP(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := s.TOTP(ctx)
	if err != nil || st.Enabled() || st.PendingSecret != nil {
		t.Fatalf("kapatıldıktan sonra %+v (%v)", st, err)
	}
	if n, err := s.RecoveryRemaining(ctx); err != nil || n != 0 {
		t.Fatalf("kurtarma kodu kaldı: %d (%v)", n, err)
	}
	if f, err := s.LoginFailureFor(ctx, "SHA256:aaa"); err != nil || f.Count != 0 {
		t.Fatalf("sayaç kaldı: %+v (%v)", f, err)
	}
}
