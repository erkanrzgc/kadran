package api

import (
	"context"
	"encoding/base32"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/connproto"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── İki aşamalı giriş (K-155) ─────────────────────────────────────────
//
// Testler gerçek kablolamadan geçiyor: NewGRPCServer, gerçek önsöz,
// gerçek store. Saat sahte (srv.now), alarm yöneticisi sahte.

var (
	yonetici  = connproto.Identity{Fingerprint: "SHA256:yonetici", Origin: "ssh", Role: connproto.RoleAdmin}
	baskasi   = connproto.Identity{Fingerprint: "SHA256:baskasi", Origin: "ssh", Role: connproto.RoleAdmin}
	dagitici  = connproto.Identity{Fingerprint: "SHA256:ci", Origin: "ssh", Role: connproto.RoleDeploy, Apps: []string{"blog"}}
	izsiz     = connproto.Identity{Origin: "local", Role: connproto.RoleAdmin}
	baslangic = time.Unix(1_800_000_000, 0)
)

const (
	mesajJetonYok      = `TOTP is on for this server: sign in with "kadran login" (upgrade kadran if it has no login command)`
	mesajSuresiBitti   = `session expired: sign in again with "kadran login"`
	mesajGecersiz      = `invalid session token: sign in again with "kadran login"`
	mesajYanlisKod     = "code rejected: check that the code is current and your phone's clock is correct"
	mesajYerelYonetici = "TOTP is on for this server: admin commands need an SSH key identity, so run kadran from your workstation over SSH"
	mesajGuncelKod     = "TOTP is already on: re-enrolling needs a current code from your authenticator app or a recovery code"
)

// acikKapi, oturumu denetlemeyen kapı: yalnız rol denetimini doğrudan
// sınayan birim testleri için (birimCagri).
type acikKapi struct{}

func (acikKapi) CheckSession(context.Context, connproto.Identity, string) error { return nil }

type sahteAlarm struct {
	mu      sync.Mutex
	raised  []store.Alarm
	cleared []string
}

func (a *sahteAlarm) Raise(_ context.Context, al store.Alarm) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.raised = append(a.raised, al)
}

func (a *sahteAlarm) Clear(_ context.Context, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cleared = append(a.cleared, id)
}

type totpDuzenek struct {
	t      *testing.T
	srv    *Server
	db     *store.Store
	alarm  *sahteAlarm
	now    time.Time
	addr   string
	sir    []byte
	jeton  string
	kodlar []string
}

func totpKur(t *testing.T) *totpDuzenek {
	t.Helper()
	srv, db := newTestServer(t)
	d := &totpDuzenek{t: t, srv: srv, db: db, alarm: &sahteAlarm{}, now: baslangic}
	srv.now = func() time.Time { return d.now }
	srv.alarms = d.alarm

	gs := NewGRPCServer(srv, &callerCreds{peer: kabulEdenPeer{}})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("dinlenemedi: %v", err)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	d.addr = lis.Addr().String()
	return d
}

// istemci, verilen kimliğin önsözüyle bağlanan bir istemci döner.
func (d *totpDuzenek) istemci(id connproto.Identity) kadranv1.KadranServiceClient {
	d.t.Helper()
	conn, err := grpc.NewClient("passthrough:///totp",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			var nd net.Dialer
			c, err := nd.DialContext(ctx, "tcp", d.addr)
			if err != nil {
				return nil, err
			}
			if err := connproto.Write(c, id); err != nil {
				_ = c.Close()
				return nil, err
			}
			return c, nil
		}))
	if err != nil {
		d.t.Fatalf("istemci kurulamadı: %v", err)
	}
	d.t.Cleanup(func() { _ = conn.Close() })
	return kadranv1.NewKadranServiceClient(conn)
}

func oturumla(jeton string) context.Context {
	ctx := context.Background()
	if jeton == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, totp.SessionMetadataKey, jeton)
}

func cozSir(t *testing.T, b32 string) []byte {
	t.Helper()
	sir, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(b32)
	if err != nil {
		t.Fatalf("sır çözülemedi: %v", err)
	}
	return sir
}

// etkinlestir, yönetici olarak TOTP'yi açar ve sırrı, jetonu ve kurtarma
// kodlarını düzeneğe yazar.
func (d *totpDuzenek) etkinlestir() {
	d.t.Helper()
	c := d.istemci(yonetici)
	req := &kadranv1.BeginTotpEnrollmentRequest{}
	if d.sir != nil {
		req.CurrentCode = totp.CodeAt(d.sir, d.now) // yeniden kurulum güncel kod ister
	}
	b, err := c.BeginTotpEnrollment(oturumla(d.jeton), req)
	if err != nil {
		d.t.Fatalf("kurulum başlamadı: %v", err)
	}
	d.sir = cozSir(d.t, b.GetSecretBase32())
	r, err := c.ConfirmTotpEnrollment(oturumla(d.jeton),
		&kadranv1.ConfirmTotpEnrollmentRequest{Code: totp.CodeAt(d.sir, d.now)})
	if err != nil {
		d.t.Fatalf("kurulum onaylanmadı: %v", err)
	}
	d.jeton, d.kodlar = r.GetSessionToken(), r.GetRecoveryCodes()
}

func durumMesaj(t *testing.T, err error, code codes.Code, msg string) {
	t.Helper()
	st, _ := status.FromError(err)
	if st.Code() != code || (msg != "" && st.Message() != msg) {
		t.Fatalf("beklenen %v %q, gelen %v %q", code, msg, st.Code(), st.Message())
	}
}

func sonGiris(t *testing.T, db *store.Store) (outcome, detail string) {
	t.Helper()
	recs, err := db.ListAudit(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Action == "auth.login" {
			return recs[i].Outcome.String(), recs[i].Detail
		}
	}
	t.Fatal("auth.login kaydı yok")
	return "", ""
}

func TestNoEnforcementWhileDisabled(t *testing.T) {
	d := totpKur(t)
	if _, err := d.istemci(yonetici).ListApps(context.Background(), &kadranv1.ListAppsRequest{}); err != nil {
		t.Fatalf("TOTP kapalıyken jetonsuz çağrı reddedildi: %v", err)
	}
}

func TestAdminNeedsSessionWhenEnabled(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	_, err := d.istemci(yonetici).ListApps(context.Background(), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajJetonYok)
	if _, err := d.istemci(yonetici).ListApps(oturumla(d.jeton), &kadranv1.ListAppsRequest{}); err != nil {
		t.Fatalf("geçerli jetonla reddedildi: %v", err)
	}
}

// TestDeployRoleIsExemptFromSession: CI'ın dağıtım anahtarı TOTP
// açıldıktan sonra da jetonsuz dağıtır (kapsam denetimi yine çalışır).
// TestClientRecognisesSessionErrors: CLI'ın "kadran login" ipucu
// client.IsSessionError'ın öneklerine bağlı; üç oturum hatası da gerçek
// sunucudan alınıp sınanıyor, yoksa bir mesaj değişikliği ipucunu sessizce
// kapatırdı. Yanlış kod bir oturum hatası değil.
func TestClientRecognisesSessionErrors(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	_, yok := c.ListApps(context.Background(), &kadranv1.ListAppsRequest{})
	_, bozuk := c.ListApps(oturumla(d.jeton+"x"), &kadranv1.ListAppsRequest{})
	_, yanlis := c.Login(context.Background(), &kadranv1.LoginRequest{Code: "12345x"})
	d.now = d.now.Add(totp.TokenLifetime + time.Second)
	_, bitti := c.ListApps(oturumla(d.jeton), &kadranv1.ListAppsRequest{})

	for ad, err := range map[string]error{"jeton yok": yok, "bozuk": bozuk, "süresi bitti": bitti} {
		if !client.IsSessionError(err) {
			t.Errorf("%s: istemci oturum hatası saymadı: %v", ad, err)
		}
	}
	if client.IsSessionError(yanlis) {
		t.Errorf("yanlış kod oturum hatası sayıldı: %v", yanlis)
	}
}

// TestLocalAdminIsToldToUseSSH: parmak izsiz yönetici (sunucudaki yerel
// soket) TOTP açıkken çağrı yapamıyor (spec kararı) ve jetonu da
// alamıyor; mesaj onu "kadran login" döngüsüne değil SSH'a yollamalı.
func TestLocalAdminIsToldToUseSSH(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	_, err := d.istemci(izsiz).ListApps(oturumla(d.jeton), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajYerelYonetici)
}

func TestDeployRoleIsExemptFromSession(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	st, err := d.istemci(dagitici).Deploy(context.Background(), &kadranv1.DeployRequest{AppId: "blog"})
	if err == nil {
		_, err = st.Recv()
	}
	if c := status.Code(err); c == codes.Unauthenticated || c == codes.PermissionDenied {
		t.Fatalf("dağıtım anahtarı engellendi: %v", err)
	}
}

func TestPingAndLoginAreExempt(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	if _, err := c.Ping(context.Background(), &kadranv1.PingRequest{}); err != nil {
		t.Fatalf("Ping jetonsuz reddedildi: %v", err)
	}
	_, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: "000000"})
	if status.Code(err) == codes.Unauthenticated {
		t.Fatalf("Login jeton istedi: %v", err)
	}
}

func TestLoginIssuesTokenBoundToKey(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.Period)
	r, err := d.istemci(yonetici).Login(context.Background(), &kadranv1.LoginRequest{Code: totp.CodeAt(d.sir, d.now)})
	if err != nil {
		t.Fatalf("giriş: %v", err)
	}
	if want := d.now.Add(totp.TokenLifetime); !r.GetExpiresAt().AsTime().Equal(want) {
		t.Fatalf("bitiş %v, beklenen %v", r.GetExpiresAt().AsTime(), want)
	}
	if _, err := d.istemci(yonetici).ListApps(oturumla(r.GetSessionToken()), &kadranv1.ListAppsRequest{}); err != nil {
		t.Fatalf("aynı anahtarla jeton reddedildi: %v", err)
	}
	_, err = d.istemci(baskasi).ListApps(oturumla(r.GetSessionToken()), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajGecersiz)
}

func TestOldGenerationTokenIsRejected(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	eski := d.jeton
	d.now = d.now.Add(totp.Period)
	d.etkinlestir() // yeniden kurulum, eski jetonla
	_, err := d.istemci(yonetici).ListApps(oturumla(eski), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajSuresiBitti)
	if _, err := d.istemci(yonetici).ListApps(oturumla(d.jeton), &kadranv1.ListAppsRequest{}); err != nil {
		t.Fatalf("yeni kuşağın jetonu reddedildi: %v", err)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.TokenLifetime + time.Second)
	_, err := d.istemci(yonetici).ListApps(oturumla(d.jeton), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajSuresiBitti)
}

func TestTamperedTokenIsRejected(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	_, err := d.istemci(yonetici).ListApps(oturumla(d.jeton+"x"), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, mesajGecersiz)
}

func TestLoginRejectsReplayedCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.Period)
	kod := totp.CodeAt(d.sir, d.now)
	c := d.istemci(yonetici)
	if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: kod}); err != nil {
		t.Fatalf("ilk giriş: %v", err)
	}
	_, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: kod})
	durumMesaj(t, err, codes.PermissionDenied, "code already used: wait for the next one")
}

// TestLoginOutsideWindowSameMessageAuditsClock: pencere dışında ama yakın
// bir kod ile tamamen yanlış bir kod ÇAĞIRANA aynı cevabı alır (yoksa
// cevap, tahmin edilen kodun birazdan geçerli olacağını sızdırırdı);
// ayrım yalnız denetim kaydında.
func TestLoginOutsideWindowSameMessageAuditsClock(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)

	ileri := totp.CodeAt(d.sir, d.now.Add(2*totp.Period))
	_, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: ileri})
	durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	if o, det := sonGiris(t, d.db); o != "failure" || det != "clock" {
		t.Fatalf("saat kayması kaydı: %s %q", o, det)
	}

	yanlis := "000000"
	if yanlis == totp.CodeAt(d.sir, d.now) {
		yanlis = "111111"
	}
	_, err = c.Login(context.Background(), &kadranv1.LoginRequest{Code: yanlis})
	durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	if _, det := sonGiris(t, d.db); det != "wrong_code" && det != "clock" {
		t.Fatalf("yanlış kod kaydı: %q", det)
	}
}

func TestLockoutAfterFiveFailures(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	yanlislar := []string{"000001", "000002", "000003", "000004", "000005"}
	for _, k := range yanlislar {
		if k == totp.CodeAt(d.sir, d.now) {
			t.Skip("yanlış kod tesadüfen doğru")
		}
		_, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: k})
		durumMesaj(t, err, codes.PermissionDenied, "")
	}
	d.now = d.now.Add(totp.Period)
	_, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: totp.CodeAt(d.sir, d.now)})
	durumMesaj(t, err, codes.ResourceExhausted, "")
	if !strings.Contains(status.Convert(err).Message(), "too many failed codes for this key: try again in") {
		t.Fatalf("kilit mesajı: %v", err)
	}
	if len(d.alarm.raised) != 1 || d.alarm.raised[0].ID != "login_lockout:SHA256:yonetici" ||
		d.alarm.raised[0].Severity != store.SeverityWarning {
		t.Fatalf("kilit alarmı: %+v", d.alarm.raised)
	}
	recs, err := d.db.ListAudit(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		for _, k := range yanlislar {
			if strings.Contains(r.ParamsJSON, k) || strings.Contains(r.Detail, k) {
				t.Fatalf("kod denetime yazıldı: %+v", r)
			}
		}
	}
}

func TestSuccessfulLoginClearsLockAlarm(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	for i := range 5 {
		_, _ = c.Login(context.Background(), &kadranv1.LoginRequest{Code: "00000" + string(rune('1'+i))})
	}
	// Kurulumun onayı da aynı alarmı kapatıyor; yalnız girişin etkisi sayılsın.
	d.alarm.mu.Lock()
	d.alarm.cleared = nil
	d.alarm.mu.Unlock()
	d.now = d.now.Add(61 * time.Second)
	if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: totp.CodeAt(d.sir, d.now)}); err != nil {
		t.Fatalf("kilit bitince doğru kod: %v", err)
	}
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 0 {
		t.Fatalf("başarılı giriş sayacı silmedi: %+v (%v)", f, err)
	}
	found := false
	for _, id := range d.alarm.cleared {
		found = found || id == "login_lockout:SHA256:yonetici"
	}
	if !found {
		t.Fatalf("kilit alarmı kapanmadı: %v", d.alarm.cleared)
	}
}

func TestRecoveryCodeLoginOnceAndWarns(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	kod := strings.ToLower(d.kodlar[0])
	if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{RecoveryCode: kod}); err != nil {
		t.Fatalf("kurtarma koduyla giriş: %v", err)
	}
	// Kimlik kullanım başına (kalan sayıyla): her kullanım bildirilsin.
	if len(d.alarm.raised) != 1 || d.alarm.raised[0].ID != "login_recovery:SHA256:yonetici:9" {
		t.Fatalf("kurtarma alarmı: %+v", d.alarm.raised)
	}
	if !strings.Contains(d.alarm.raised[0].Detail, "9 left") {
		t.Fatalf("alarm kalan sayıyı söylemiyor: %q", d.alarm.raised[0].Detail)
	}
	_, err := c.Login(context.Background(), &kadranv1.LoginRequest{RecoveryCode: kod})
	durumMesaj(t, err, codes.PermissionDenied, "recovery code rejected")
}

func TestLoginNeedsExactlyOneCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	for _, req := range []*kadranv1.LoginRequest{{}, {Code: "123456", RecoveryCode: d.kodlar[0]}} {
		_, err := c.Login(context.Background(), req)
		durumMesaj(t, err, codes.InvalidArgument, "")
	}
}

func TestPendingEnrollmentExpires(t *testing.T) {
	d := totpKur(t)
	c := d.istemci(yonetici)
	b, err := c.BeginTotpEnrollment(context.Background(), &kadranv1.BeginTotpEnrollmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if want := d.now.Add(10 * time.Minute); !b.GetExpiresAt().AsTime().Equal(want) {
		t.Fatalf("kurulum bitişi %v, beklenen %v", b.GetExpiresAt().AsTime(), want)
	}
	sir := cozSir(t, b.GetSecretBase32())
	d.now = d.now.Add(10*time.Minute + time.Second)
	_, err = c.ConfirmTotpEnrollment(context.Background(),
		&kadranv1.ConfirmTotpEnrollmentRequest{Code: totp.CodeAt(sir, d.now)})
	durumMesaj(t, err, codes.FailedPrecondition, "enrollment expired: run kadran totp enable again")
	if st, err := d.db.TOTP(context.Background()); err != nil || st.Enabled() {
		t.Fatalf("süresi dolan kurulum TOTP'yi açtı: %+v (%v)", st, err)
	}
}

func TestConfirmRejectsWrongCode(t *testing.T) {
	d := totpKur(t)
	c := d.istemci(yonetici)
	if _, err := c.BeginTotpEnrollment(context.Background(), &kadranv1.BeginTotpEnrollmentRequest{}); err != nil {
		t.Fatal(err)
	}
	_, err := c.ConfirmTotpEnrollment(context.Background(), &kadranv1.ConfirmTotpEnrollmentRequest{Code: "12345x"})
	durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	if st, err := d.db.TOTP(context.Background()); err != nil || st.Enabled() {
		t.Fatalf("yanlış kodla açıldı: %+v (%v)", st, err)
	}
}

// TestConfirmWrongCodesCountTowardLock: bekleyen sırrın kodu da girişle
// aynı kilide sayılır; doğru kod sayacı siler.
func TestConfirmWrongCodesCountTowardLock(t *testing.T) {
	d := totpKur(t)
	c := d.istemci(yonetici)
	b, err := c.BeginTotpEnrollment(context.Background(), &kadranv1.BeginTotpEnrollmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	sir := cozSir(t, b.GetSecretBase32())
	_, err = c.ConfirmTotpEnrollment(context.Background(), &kadranv1.ConfirmTotpEnrollmentRequest{Code: "12345x"})
	durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 1 {
		t.Fatalf("yanlış onay kodu sayılmadı: %+v (%v)", f, err)
	}
	for range totp.LockAfter - 1 {
		_, err = c.ConfirmTotpEnrollment(context.Background(), &kadranv1.ConfirmTotpEnrollmentRequest{Code: "12345x"})
		durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	}
	_, err = c.ConfirmTotpEnrollment(context.Background(),
		&kadranv1.ConfirmTotpEnrollmentRequest{Code: totp.CodeAt(sir, d.now)})
	durumMesaj(t, err, codes.ResourceExhausted, "")
	if st, err := d.db.TOTP(context.Background()); err != nil || st.Enabled() {
		t.Fatalf("kilitliyken açıldı: %+v (%v)", st, err)
	}

	// Kilit bitince (kurulum henüz geçerli) doğru kod kabul edilir.
	d.now = d.now.Add(totp.LockDuration(totp.LockAfter) + time.Second)
	if _, err := c.ConfirmTotpEnrollment(context.Background(),
		&kadranv1.ConfirmTotpEnrollmentRequest{Code: totp.CodeAt(sir, d.now)}); err != nil {
		t.Fatalf("kilit bitince onay: %v", err)
	}
}

// TestConfirmClearsFailures: doğru onay kodu anahtarın sayacını siler.
func TestConfirmClearsFailures(t *testing.T) {
	d := totpKur(t)
	c := d.istemci(yonetici)
	b, err := c.BeginTotpEnrollment(context.Background(), &kadranv1.BeginTotpEnrollmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.ConfirmTotpEnrollment(context.Background(), &kadranv1.ConfirmTotpEnrollmentRequest{Code: "12345x"})
	if _, err := c.ConfirmTotpEnrollment(context.Background(), &kadranv1.ConfirmTotpEnrollmentRequest{
		Code: totp.CodeAt(cozSir(t, b.GetSecretBase32()), d.now),
	}); err != nil {
		t.Fatalf("doğru onay kodu: %v", err)
	}
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 0 {
		t.Fatalf("başarılı onay sayacı silmedi: %+v (%v)", f, err)
	}
}

// TestReenrollNeedsCurrentCode: TOTP açıkken yeniden kurulum, oturum
// jetonunun yanında GÜNCEL bir kod ister; yoksa çalınmış bir jeton sırrı
// değiştirip sahibini dışarıda bırakırdı.
func TestReenrollNeedsCurrentCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	d.now = d.now.Add(totp.Period)

	_, err := c.BeginTotpEnrollment(oturumla(d.jeton), &kadranv1.BeginTotpEnrollmentRequest{})
	durumMesaj(t, err, codes.InvalidArgument, mesajGuncelKod)
	_, err = c.BeginTotpEnrollment(oturumla(d.jeton), &kadranv1.BeginTotpEnrollmentRequest{CurrentCode: "12345x"})
	durumMesaj(t, err, codes.PermissionDenied, mesajYanlisKod)
	st, err := d.db.TOTP(context.Background())
	if err != nil || len(st.PendingSecret) != 0 {
		t.Fatalf("güncel kod olmadan bekleyen sır yazıldı: %+v (%v)", st, err)
	}
	// Boş kod bir tahmin değil, sayılmaz; yanlış kod sayılır.
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 1 {
		t.Fatalf("deneme sayısı: %+v (%v)", f, err)
	}

	if _, err := c.BeginTotpEnrollment(oturumla(d.jeton),
		&kadranv1.BeginTotpEnrollmentRequest{CurrentCode: totp.CodeAt(d.sir, d.now)}); err != nil {
		t.Fatalf("güncel kodla yeniden kurulum: %v", err)
	}
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 0 {
		t.Fatalf("doğru kod sayacı silmedi: %+v (%v)", f, err)
	}
}

func TestDisableNeedsFreshCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	_, err := c.DisableTotp(oturumla(d.jeton), &kadranv1.DisableTotpRequest{Code: "12345x"})
	durumMesaj(t, err, codes.PermissionDenied, "")
	if st, _ := d.db.TOTP(context.Background()); !st.Enabled() {
		t.Fatal("yanlış kodla kapandı")
	}
	_, err = c.DisableTotp(context.Background(), &kadranv1.DisableTotpRequest{Code: totp.CodeAt(d.sir, d.now)})
	durumMesaj(t, err, codes.Unauthenticated, mesajJetonYok)

	d.now = d.now.Add(totp.Period)
	if _, err := c.DisableTotp(oturumla(d.jeton), &kadranv1.DisableTotpRequest{Code: totp.CodeAt(d.sir, d.now)}); err != nil {
		t.Fatalf("taze kodla kapatılamadı: %v", err)
	}
	if st, _ := d.db.TOTP(context.Background()); st.Enabled() {
		t.Fatal("kapatılmadı")
	}
}

func TestEmptyFingerprintCannotLogin(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	_, err := d.istemci(izsiz).Login(context.Background(), &kadranv1.LoginRequest{Code: totp.CodeAt(d.sir, d.now)})
	durumMesaj(t, err, codes.FailedPrecondition, "signing in needs an SSH key identity")
}

func TestLoginWhileDisabled(t *testing.T) {
	d := totpKur(t)
	_, err := d.istemci(yonetici).Login(context.Background(), &kadranv1.LoginRequest{Code: "123456"})
	durumMesaj(t, err, codes.FailedPrecondition, "TOTP is not enabled on this server")
}

func TestPingReportsTotpRequired(t *testing.T) {
	d := totpKur(t)
	c := d.istemci(yonetici)
	p, err := c.Ping(context.Background(), &kadranv1.PingRequest{})
	if err != nil || p.TotpRequired == nil || p.GetTotpRequired() {
		t.Fatalf("kapalıyken Ping: %+v (%v)", p, err)
	}
	d.etkinlestir()
	if p, err = c.Ping(context.Background(), &kadranv1.PingRequest{}); err != nil || !p.GetTotpRequired() {
		t.Fatalf("açıkken Ping: %+v (%v)", p, err)
	}
}

func TestTotpStatus(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	for range 5 {
		_, _ = d.istemci(baskasi).Login(context.Background(), &kadranv1.LoginRequest{Code: "99999x"})
	}
	st, err := c.GetTotpStatus(oturumla(d.jeton), &kadranv1.GetTotpStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !st.GetEnabled() || st.GetRecoveryRemaining() != 10 || len(st.GetLocked()) != 1 ||
		st.GetLocked()[0].GetKeyFingerprint() != "SHA256:baskasi" || st.GetLocked()[0].GetFailures() != 5 {
		t.Fatalf("durum: %+v", st)
	}
}

// TestSecretsNeverInAudit: kurulum, onay, giriş, kurtarma ve kapatma
// akışından sonra hiçbir denetim kaydı sırrı, kodu, kurtarma kodunu ya da
// jetonu taşımıyor.
func TestSecretsNeverInAudit(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	gizliler := []string{base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(d.sir), d.jeton}
	gizliler = append(gizliler, d.kodlar...)

	d.now = d.now.Add(totp.Period)
	kod := totp.CodeAt(d.sir, d.now)
	gizliler = append(gizliler, kod)
	r, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: kod})
	if err != nil {
		t.Fatal(err)
	}
	gizliler = append(gizliler, r.GetSessionToken())
	if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{RecoveryCode: d.kodlar[1]}); err != nil {
		t.Fatal(err)
	}
	d.now = d.now.Add(totp.Period)
	kapat := totp.CodeAt(d.sir, d.now)
	gizliler = append(gizliler, kapat)
	if _, err := c.DisableTotp(oturumla(r.GetSessionToken()), &kadranv1.DisableTotpRequest{Code: kapat}); err != nil {
		t.Fatal(err)
	}

	recs, err := d.db.ListAudit(context.Background(), 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	eylemler := map[string]bool{}
	for _, rec := range recs {
		eylemler[rec.Action] = true
		for _, g := range gizliler {
			if strings.Contains(rec.ParamsJSON, g) || strings.Contains(rec.Detail, g) || strings.Contains(rec.Target, g) {
				t.Fatalf("gizli değer denetimde: %+v", rec)
			}
		}
	}
	for _, e := range []string{"totp.enroll", "totp.confirm", "auth.login", "totp.disable"} {
		if !eylemler[e] {
			t.Errorf("%s kaydı yok (%v)", e, eylemler)
		}
	}
}
