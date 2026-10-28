package api

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/connproto"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── Oturum jetonu (K-155) ─────────────────────────────────────────────
//
// TOTP açıkken yönetici rolünün her çağrısı `kadran-session` metadata'sında
// geçerli bir jeton ister. Metadata'yı uzak istemci yazıyor (bkz.
// identity.go); jeton bu yüzden bir taşıyıcı kimlik bilgisi ve ancak
// bağlantının önsözdeki parmak iziyle birlikte geçerli: A anahtarı için
// basılan jeton B anahtarının bağlantısında reddedilir.
//
// Dağıtım rolü muaf (CI kod giremez); zaten GetApp'i çağıramıyor.

// SessionGate, yetki önleyicisinin rol denetiminden sonra çağırdığı oturum
// denetimidir.
type SessionGate interface {
	CheckSession(ctx context.Context, id connproto.Identity, method string) error
}

// sessionExempt, TOTP açıkken de jetonsuz çağrılabilen yöntemler. Ping:
// istemci bağlanırken protokolü onunla soruyor. Login: jeton onunla alınıyor.
var sessionExempt = map[string]bool{
	kadranv1.KadranService_Ping_FullMethodName:  true,
	kadranv1.KadranService_Login_FullMethodName: true,
}

// Oturum hataları. İstemci (client.IsSessionError) bu öneklere bakıyor.
const (
	msgNoSession      = `TOTP is on for this server: sign in with "kadran login" (upgrade kadran if it has no login command)`
	msgSessionExpired = `session expired: sign in again with "kadran login"`
	msgSessionInvalid = `invalid session token: sign in again with "kadran login"`
	// Parmak izsiz yönetici (sunucudaki yerel soket) jeton alamaz; ona
	// "kadran login" demek bir döngü olurdu.
	msgSessionNoKey = "TOTP is on for this server: admin commands need an SSH key identity, so run kadran from your workstation over SSH"
)

// CheckSession, SessionGate'i karşılar. Durum her çağrıda veritabanından
// okunuyor: kapatma ve kökün sıfırlaması anında etki eder.
func (s *Server) CheckSession(ctx context.Context, id connproto.Identity, method string) error {
	if id.Role != connproto.RoleAdmin || sessionExempt[method] {
		return nil
	}
	st, err := s.store.TOTP(ctx)
	if err != nil {
		// Okunamıyorsa KAPALI sayılmaz: kapıyı açık bırakmaktansa çağrı düşer.
		slog.Error("TOTP state could not be read", "error", err)
		return status.Error(codes.Internal, "could not read the TOTP state")
	}
	if !st.Enabled() {
		return nil
	}
	if id.Fingerprint == "" {
		return sessionDenied(id, method, "no key identity", msgSessionNoKey)
	}

	tok := sessionToken(ctx)
	if tok == "" {
		return sessionDenied(id, method, "no session token", msgNoSession)
	}
	c, err := totp.ParseToken(st.TokenKey, tok)
	if errors.Is(err, totp.ErrBadSignature) && prevKeyToken(st.PrevTokenKey, tok, id.Fingerprint) {
		// Yeniden kurulumdan önce alınmış jeton: hiçbir zaman kabul
		// edilmez, yalnız doğru mesajı alır.
		return sessionDenied(id, method, "token from before re-enrollment", msgSessionExpired)
	}
	if err != nil || c.Fingerprint == "" || c.Fingerprint != id.Fingerprint {
		return sessionDenied(id, method, "invalid session token", msgSessionInvalid)
	}
	if c.Generation != st.Generation || !s.now().Before(c.Expires) {
		return sessionDenied(id, method, "expired session token", msgSessionExpired)
	}
	return nil
}

// prevKeyToken, jetonun bir önceki imza anahtarıyla aynı anahtar için
// imzalandığını söyler.
func prevKeyToken(prevKey []byte, tok, fp string) bool {
	if len(prevKey) == 0 {
		return false
	}
	c, err := totp.ParseToken(prevKey, tok)
	return err == nil && c.Fingerprint == fp
}

// sessionToken, metadata'daki jetonu okur; yoksa ya da birden çoksa boş.
func sessionToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	v := md.Get(totp.SessionMetadataKey)
	if len(v) != 1 {
		return ""
	}
	return v[0]
}

// sessionDenied, reddi günlüğe yazar (jetonun kendisi YAZILMAZ).
func sessionDenied(id connproto.Identity, method, why, msg string) error {
	slog.Warn("session rejected",
		"method", method, "reason", why,
		"key", orUnknown(id.Fingerprint), "source_ip", orUnknown(id.SourceIP))
	return status.Error(codes.Unauthenticated, msg)
}
