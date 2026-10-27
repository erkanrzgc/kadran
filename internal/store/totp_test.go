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

	jetonB := bytes.Repeat([]byte{8}, 32)
	gen, err := s.ConfirmTOTP(ctx, 11, jetonB, kurtarmaOzetleri(2), simdi)
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
	// İmza anahtarı yeniden kurulumda DEĞİŞMEZ: eski jetonları kuşak
	// düşürüyor ve "süresi doldu" diyebilmek için imzaları geçerli kalmalı.
	if !bytes.Equal(st.TokenKey, jetonA) {
		t.Fatalf("yeniden kurulum imza anahtarını değiştirdi: %x", st.TokenKey)
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

// TestLoginFailuresLockAfterFive: her deneme kod denenmeden ÖNCE sayılır;
// 5. denemede kilit kurulur (doğru kod çıkarsa giriş sayacı temizler).
func TestLoginFailuresLockAfterFive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const fp = "SHA256:aaa"
	var f LoginFailure
	var err error
	for i := 1; i <= 4; i++ {
		if f, err = s.ReserveLoginAttempt(ctx, fp, simdi); err != nil {
			t.Fatal(err)
		}
		if f.Count != i || !f.LockedUntil.IsZero() {
			t.Fatalf("%d. deneme: %+v", i, f)
		}
	}
	if f, err = s.ReserveLoginAttempt(ctx, fp, simdi); err != nil || !f.LockedUntil.Equal(simdi.Add(time.Minute)) {
		t.Fatalf("5. deneme: %+v (%v)", f, err)
	}
	sonra := simdi.Add(61 * time.Second)
	if f, err = s.ReserveLoginAttempt(ctx, fp, sonra); err != nil || f.Count != 6 || !f.LockedUntil.Equal(sonra.Add(2*time.Minute)) {
		t.Fatalf("kilit bitince 6. deneme: %+v (%v)", f, err)
	}
	locked, err := s.LockedLogins(ctx, sonra)
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

// TestReserveRefusesWhileLocked: kilitliyken deneme hiç başlamaz ve sayacı
// büyütmez (yoksa kilit süresi her denemeyle uzar, sahibi de kilitli kalırdı).
func TestReserveRefusesWhileLocked(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const fp = "SHA256:aaa"
	for range 5 {
		if _, err := s.ReserveLoginAttempt(ctx, fp, simdi); err != nil {
			t.Fatal(err)
		}
	}
	f, err := s.ReserveLoginAttempt(ctx, fp, simdi.Add(30*time.Second))
	if !errors.Is(err, ErrLoginLocked) || !f.LockedUntil.Equal(simdi.Add(time.Minute)) {
		t.Fatalf("kilitliyken: %+v (%v)", f, err)
	}
	if f, err := s.LoginFailureFor(ctx, fp); err != nil || f.Count != 5 {
		t.Fatalf("kilitli deneme sayacı büyüttü: %+v (%v)", f, err)
	}
}

// TestReserveIsAtomicUnderConcurrency: aynı anahtarla paralel açılan
// bağlantılar kilidi aşamaz. Okuma, kod denemesi ve yazma ayrı adımlar
// olsaydı her bağlantı "kilitli değil" görür ve 5'ten fazla kod denerdi.
func TestReserveIsAtomicUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	const fp = "SHA256:aaa"
	const n = 20
	results := make(chan error, n)
	for range n {
		go func() {
			_, err := s.ReserveLoginAttempt(ctx, fp, simdi)
			results <- err
		}()
	}
	allowed, locked := 0, 0
	for range n {
		switch err := <-results; {
		case err == nil:
			allowed++
		case errors.Is(err, ErrLoginLocked):
			locked++
		default:
			t.Fatalf("beklenmeyen hata: %v", err)
		}
	}
	if allowed != 5 || locked != n-5 {
		t.Fatalf("%d deneme başladı, %d kilitlendi; beklenen 5 ve %d", allowed, locked, n-5)
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
		if _, err := s.ReserveLoginAttempt(ctx, "SHA256:aaa", simdi); err != nil {
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
	if _, err := s.ReserveLoginAttempt(ctx, "SHA256:aaa", simdi); err != nil {
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

	// Sıfırdan kurulum yeni imza anahtarı alır ve kuşak baştan başlar.
	jetonB := bytes.Repeat([]byte{8}, 32)
	if err := s.BeginTOTPEnrollment(ctx, sir2, simdi); err != nil {
		t.Fatal(err)
	}
	if gen, err := s.ConfirmTOTP(ctx, 5, jetonB, kurtarmaOzetleri(2), simdi); err != nil || gen != 1 {
		t.Fatalf("yeniden açılış gen=%d (%v)", gen, err)
	}
	if st, err := s.TOTP(ctx); err != nil || !bytes.Equal(st.TokenKey, jetonB) {
		t.Fatalf("sıfırdan kurulum yeni anahtar almadı: %x (%v)", st.TokenKey, err)
	}
}
