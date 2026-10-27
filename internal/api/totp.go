package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/kadran/internal/alarm"
	"github.com/erkanrzgc/kadran/internal/audit"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── İki aşamalı giriş: işleyiciler (K-155) ─────────────────────────────
//
// Sır, kod, kurtarma kodu ve jeton hiçbir denetim kaydına, günlüğe ya da
// alarm metnine YAZILMAZ; yalnız parmak izi, sonuç ve sebep sözcüğü.

const (
	// pendingEnrollmentTTL, onaylanmayan kurulumun geçerli kaldığı süre.
	pendingEnrollmentTTL = 10 * time.Minute
	totpIssuer           = "Kadran"
	// clockSpan, yanlış kodun saat kaymasından mı geldiğini anlamak için
	// bakılan adım sayısı (±2 dakika). Yalnız denetim kaydının ayrıntısını
	// seçer; çağırana aynı cevap döner (aksi, tahmin edilen kodun birazdan
	// geçerli olacağını sızdırırdı).
	clockSpan = 4

	auditTOTPTarget = "totp"
)

const (
	msgWrongCode    = "code rejected: check that the code is current and your phone's clock is correct"
	msgReplayedCode = "code already used: wait for the next one"
	msgNoTOTP       = "TOTP is not enabled on this server"
	msgNeedsKey     = "signing in needs an SSH key identity"
	msgNeedsCurrent = "TOTP is already on: re-enrolling needs a current code from your authenticator app"
	msgDisableCode  = "disabling TOTP needs a current code from your authenticator app"
)

// callerFingerprint, bağlantının önsözündeki parmak izidir.
func callerFingerprint(ctx context.Context) string {
	info, ok := callerFromContext(ctx)
	if !ok {
		return ""
	}
	return info.Identity.Fingerprint
}

func (s *Server) readTOTP(ctx context.Context) (store.TOTPState, error) {
	st, err := s.store.TOTP(ctx)
	if err != nil {
		return store.TOTPState{}, status.Error(codes.Internal, "could not read the TOTP state")
	}
	return st, nil
}

// BeginTotpEnrollment, onay bekleyen bir kurulum başlatır. TOTP açıksa
// (yeniden kurulum) oturum kapıda denetlendi; burada ayrıca GÜNCEL bir kod
// isteniyor: çalınmış bir jeton sırrı değiştirip sahibini dışarıda
// bırakamasın.
func (s *Server) BeginTotpEnrollment(
	ctx context.Context, req *kadranv1.BeginTotpEnrollmentRequest,
) (*kadranv1.BeginTotpEnrollmentResponse, error) {
	fp := callerFingerprint(ctx)
	if fp == "" {
		return nil, status.Error(codes.FailedPrecondition, msgNeedsKey)
	}
	st, err := s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if st.Enabled() {
		if err := s.stepUp(ctx, "totp.enroll", msgNeedsCurrent, fp, st, req.GetCurrentCode(), now); err != nil {
			return nil, err
		}
	}
	secret, err := totp.NewSecret()
	if err != nil {
		return nil, status.Error(codes.Internal, "could not generate a TOTP secret")
	}
	opErr := s.store.BeginTOTPEnrollment(ctx, secret, now)
	if err := s.completed(ctx, "totp.enroll", auditTOTPTarget, nil, opErr); err != nil {
		return nil, status.Error(codes.Internal, "could not store the TOTP enrollment")
	}

	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "server"
	}
	return &kadranv1.BeginTotpEnrollmentResponse{
		OtpauthUri:   totp.OTPAuthURI(secret, totpIssuer, host),
		SecretBase32: totp.EncodeSecret(secret),
		ExpiresAt:    timestamppb.New(now.Add(pendingEnrollmentTTL)),
	}, nil
}

// ConfirmTotpEnrollment, bekleyen kurulumu bir kodla etkin yapar.
func (s *Server) ConfirmTotpEnrollment(
	ctx context.Context, req *kadranv1.ConfirmTotpEnrollmentRequest,
) (*kadranv1.ConfirmTotpEnrollmentResponse, error) {
	fp := callerFingerprint(ctx)
	if fp == "" {
		return nil, status.Error(codes.FailedPrecondition, msgNeedsKey)
	}
	now := s.now()
	st, err := s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	if len(st.PendingSecret) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "no enrollment in progress: run kadran totp enable")
	}
	if now.Sub(st.PendingCreatedAt) > pendingEnrollmentTTL {
		return nil, status.Error(codes.FailedPrecondition, "enrollment expired: run kadran totp enable again")
	}
	// Bekleyen sırrın kodu da girişle aynı kilide sayılır.
	attempt, err := s.reserveAttempt(ctx, "totp.confirm", fp, now)
	if err != nil {
		return nil, err
	}
	// Bekleyen sır hiç kullanılmadı: son adım 0.
	step, ok := totp.Verify(st.PendingSecret, strings.TrimSpace(req.GetCode()), now, 0)
	if !ok {
		return nil, s.codeFailed(ctx, "totp.confirm", fp, attempt, now, "wrong_code",
			status.Error(codes.PermissionDenied, msgWrongCode))
	}
	if err := s.clearAttempts(ctx, fp); err != nil {
		return nil, err
	}

	codesPlain, err := totp.NewRecoveryCodes()
	if err != nil {
		return nil, status.Error(codes.Internal, "could not generate recovery codes")
	}
	hashes := make([][32]byte, len(codesPlain))
	for i, c := range codesPlain {
		hashes[i] = totp.HashRecovery(c)
	}
	key := make([]byte, totp.TokenKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, status.Error(codes.Internal, "could not generate a session key")
	}
	gen, err := s.store.ConfirmTOTP(ctx, step, key, hashes, now)
	if errors.Is(err, store.ErrNoPendingEnrollment) {
		return nil, status.Error(codes.FailedPrecondition, "no enrollment in progress: run kadran totp enable")
	}
	if err != nil {
		_ = s.completed(ctx, "totp.confirm", auditTOTPTarget, nil, err)
		return nil, status.Error(codes.Internal, "could not confirm the TOTP enrollment")
	}
	// İmza anahtarı yeniden kurulumda korunuyor (store.ConfirmTOTP);
	// jetonu saklanan anahtarla imzalamak için durum yeniden okunuyor.
	st, err = s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.completed(ctx, "totp.confirm", auditTOTPTarget, nil, nil); err != nil {
		return nil, err
	}
	token, exp := s.issueToken(st.TokenKey, fp, gen, now)
	return &kadranv1.ConfirmTotpEnrollmentResponse{
		RecoveryCodes: codesPlain,
		SessionToken:  token,
		ExpiresAt:     timestamppb.New(exp),
	}, nil
}

func (s *Server) issueToken(key []byte, fp string, gen int64, now time.Time) (string, time.Time) {
	exp := now.Add(totp.TokenLifetime)
	return totp.SignToken(key, totp.Claims{Fingerprint: fp, Expires: exp, Generation: gen}), exp
}

// Login, bir TOTP ya da kurtarma koduyla oturum jetonu verir.
func (s *Server) Login(ctx context.Context, req *kadranv1.LoginRequest) (*kadranv1.LoginResponse, error) {
	fp := callerFingerprint(ctx)
	if fp == "" {
		return nil, status.Error(codes.FailedPrecondition, msgNeedsKey)
	}
	st, err := s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Enabled() {
		return nil, status.Error(codes.FailedPrecondition, msgNoTOTP)
	}
	code, recovery := strings.TrimSpace(req.GetCode()), strings.TrimSpace(req.GetRecoveryCode())
	if (code == "") == (recovery == "") {
		return nil, status.Error(codes.InvalidArgument, "give exactly one of a TOTP code or a recovery code")
	}

	now := s.now()
	attempt, err := s.reserveAttempt(ctx, "auth.login", fp, now)
	if err != nil {
		return nil, err
	}

	detail := "totp"
	if recovery != "" {
		ok, err := s.store.UseRecoveryCode(ctx, totp.HashRecovery(recovery), now)
		if err != nil {
			return nil, status.Error(codes.Internal, "could not check the recovery code")
		}
		if !ok {
			return nil, s.codeFailed(ctx, "auth.login", fp, attempt, now, "recovery_wrong",
				status.Error(codes.PermissionDenied, "recovery code rejected"))
		}
		detail = "recovery"
	} else if err := s.checkCode(ctx, "auth.login", fp, st, code, attempt, now); err != nil {
		return nil, err
	}

	if err := s.clearAttempts(ctx, fp); err != nil {
		return nil, err
	}
	if err := s.recordAction(ctx, "auth.login", auditTOTPTarget, nil, audit.OutcomeSuccess, detail); err != nil {
		return nil, err
	}
	if detail == "recovery" {
		s.warnRecoveryUsed(ctx, fp, now)
	}
	token, exp := s.issueToken(st.TokenKey, fp, st.Generation, now)
	return &kadranv1.LoginResponse{SessionToken: token, ExpiresAt: timestamppb.New(exp)}, nil
}

// DisableTotp, TOTP'yi kapatır. Oturum kapıda denetlendi; burada ayrıca
// TAZE bir kod isteniyor: çalınmış bir jeton TOTP'yi kapatamasın.
func (s *Server) DisableTotp(ctx context.Context, req *kadranv1.DisableTotpRequest) (*kadranv1.DisableTotpResponse, error) {
	fp := callerFingerprint(ctx)
	if fp == "" {
		return nil, status.Error(codes.FailedPrecondition, msgNeedsKey)
	}
	st, err := s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Enabled() {
		return nil, status.Error(codes.FailedPrecondition, msgNoTOTP)
	}
	if err := s.stepUp(ctx, "totp.disable", msgDisableCode, fp, st, req.GetCode(), s.now()); err != nil {
		return nil, err
	}
	opErr := s.store.DisableTOTP(ctx)
	if err := s.completed(ctx, "totp.disable", auditTOTPTarget, nil, opErr); err != nil {
		return nil, status.Error(codes.Internal, "could not disable TOTP")
	}
	return &kadranv1.DisableTotpResponse{}, nil
}

// GetTotpStatus, TOTP durumunu, kalan kurtarma kodunu ve kilitli anahtarları döner.
func (s *Server) GetTotpStatus(ctx context.Context, _ *kadranv1.GetTotpStatusRequest) (*kadranv1.GetTotpStatusResponse, error) {
	st, err := s.readTOTP(ctx)
	if err != nil {
		return nil, err
	}
	resp := &kadranv1.GetTotpStatusResponse{Enabled: st.Enabled()}
	if !st.Enabled() {
		return resp, nil
	}
	resp.ConfirmedAt = timestamppb.New(st.ConfirmedAt)
	n, err := s.store.RecoveryRemaining(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not count the recovery codes")
	}
	resp.RecoveryRemaining = uint32(n) //nolint:gosec // en çok totp.RecoveryCount
	locked, err := s.store.LockedLogins(ctx, s.now())
	if err != nil {
		return nil, status.Error(codes.Internal, "could not list locked keys")
	}
	for _, f := range locked {
		resp.Locked = append(resp.Locked, &kadranv1.LockedLogin{
			KeyFingerprint: f.Fingerprint,
			Until:          timestamppb.New(f.LockedUntil),
			Failures:       uint32(f.Count), //nolint:gosec // sayaç negatif olmaz
		})
	}
	return resp, nil
}

// stepUp, oturumu olan çağırandan şu anki sırrın TAZE kodunu ister (TOTP'yi
// kapatmak ve yeniden kurmak). Boş kod bir tahmin değil, sayılmaz; dolu kod
// girişle aynı kilide sayılır ve doğruysa sayacı siler.
func (s *Server) stepUp(
	ctx context.Context, action, missing, fp string, st store.TOTPState, code string, now time.Time,
) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return status.Error(codes.InvalidArgument, missing)
	}
	attempt, err := s.reserveAttempt(ctx, action, fp, now)
	if err != nil {
		return err
	}
	if err := s.checkCode(ctx, action, fp, st, code, attempt, now); err != nil {
		return err
	}
	return s.clearAttempts(ctx, fp)
}

// clearAttempts, doğru koddan sonra anahtarın deneme sayacını ve kilit
// alarmını siler.
func (s *Server) clearAttempts(ctx context.Context, fp string) error {
	if err := s.store.ClearLoginFailures(ctx, fp); err != nil {
		return status.Error(codes.Internal, "could not clear the failed sign-ins")
	}
	if s.alarms != nil {
		s.alarms.Clear(ctx, alarm.KindLoginLockout+":"+fp)
	}
	return nil
}

// reserveAttempt, denemeyi kod denenmeden ÖNCE sayar (store.ReserveLoginAttempt).
// Kilitliyse kod hiç denenmez.
func (s *Server) reserveAttempt(ctx context.Context, action, fp string, now time.Time) (store.LoginFailure, error) {
	f, err := s.store.ReserveLoginAttempt(ctx, fp, now)
	if errors.Is(err, store.ErrLoginLocked) {
		_ = s.recordAction(ctx, action, auditTOTPTarget, nil, audit.OutcomeFailure, "locked")
		wait := f.LockedUntil.Sub(now).Round(time.Minute)
		if wait < time.Minute {
			wait = time.Minute
		}
		return f, status.Errorf(codes.ResourceExhausted,
			"too many failed codes for this key: try again in %s", wait)
	}
	if err != nil {
		return f, status.Error(codes.Internal, "could not record the sign-in attempt")
	}
	return f, nil
}

// checkCode, TOTP kodunu doğrular ve adımı kabul eder; başarısızsa kaydı
// yazar ve çağırana dönecek hatayı verir.
func (s *Server) checkCode(
	ctx context.Context, action, fp string, st store.TOTPState, code string,
	attempt store.LoginFailure, now time.Time,
) error {
	step, ok := totp.Verify(st.Secret, code, now, st.LastStep)
	if !ok {
		reason, msg := "wrong_code", msgWrongCode
		switch {
		case usedStep(st, code, now):
			reason, msg = "replay", msgReplayedCode
		case totp.InWindow(st.Secret, code, now, clockSpan):
			reason = "clock"
		}
		return s.codeFailed(ctx, action, fp, attempt, now, reason, status.Error(codes.PermissionDenied, msg))
	}
	accepted, err := s.store.AcceptTOTPStep(ctx, step)
	if err != nil {
		return status.Error(codes.Internal, "could not record the TOTP step")
	}
	if !accepted {
		// Aynı kod başka bir bağlantıda az önce kabul edildi.
		return s.codeFailed(ctx, action, fp, attempt, now, "replay",
			status.Error(codes.PermissionDenied, msgReplayedCode))
	}
	return nil
}

// usedStep, kodun penceredeki ama ZATEN KABUL EDİLMİŞ bir adıma uyduğunu
// söyler. Kod o an geçerli olduğundan bunu söylemek bir şey sızdırmaz.
func usedStep(st store.TOTPState, code string, now time.Time) bool {
	_, fresh := totp.Verify(st.Secret, code, now, 0)
	return fresh
}

// codeFailed, başarısız denemeyi denetime yazar; bu deneme anahtarı
// kilitlediyse alarm üretir.
func (s *Server) codeFailed(
	ctx context.Context, action, fp string, attempt store.LoginFailure, now time.Time,
	reason string, failure error,
) error {
	_ = s.recordAction(ctx, action, auditTOTPTarget, nil, audit.OutcomeFailure, reason)
	if s.alarms != nil && attempt.LockedUntil.After(now) {
		s.alarms.Raise(ctx, store.Alarm{
			ID:       alarm.KindLoginLockout + ":" + fp,
			Kind:     alarm.KindLoginLockout,
			Target:   fp,
			Severity: store.SeverityWarning,
			Since:    now,
			Detail: fmt.Sprintf("%d failed codes for key %s, locked until %s",
				attempt.Count, fp, attempt.LockedUntil.UTC().Format(time.RFC3339)),
		})
	}
	return failure
}

// warnRecoveryUsed, kurtarma kodu kullanımını bildirir: telefon kaybolmuş ya
// da kodlar çalınmış olabilir.
func (s *Server) warnRecoveryUsed(ctx context.Context, fp string, now time.Time) {
	if s.alarms == nil {
		return
	}
	left, err := s.store.RecoveryRemaining(ctx)
	if err != nil {
		left = -1
	}
	s.alarms.Raise(ctx, store.Alarm{
		ID:       alarm.KindLoginRecovery + ":" + fp,
		Kind:     alarm.KindLoginRecovery,
		Target:   fp,
		Severity: store.SeverityWarning,
		Since:    now,
		Detail:   fmt.Sprintf("recovery code used by key %s, %d left", fp, left),
	})
}
