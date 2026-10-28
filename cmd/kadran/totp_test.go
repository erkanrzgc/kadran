package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/kadran/internal/bootstrap"
	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── İki aşamalı giriş komutları (K-155) ───────────────────────────────

const (
	totpHedef    = "srv.example"
	totpAnahtar  = "kadran-client@srv.example" // totpHedef'in oturum anahtarı
	totpJeton    = "v1.yuk.imza"
	totpYeniJton = "v1.yeni.imza"
)

// fakeTotpRPC, TOTP RPC'lerini ve ListAlarms'ı cevaplayan sahte sunucu.
type fakeTotpRPC struct {
	kadranv1.KadranServiceClient
	login     *kadranv1.LoginRequest
	loginErr  error
	begin     *kadranv1.BeginTotpEnrollmentRequest
	beginErr  error
	confirm   *kadranv1.ConfirmTotpEnrollmentRequest
	disable   *kadranv1.DisableTotpRequest
	status    *kadranv1.GetTotpStatusResponse
	alarmsErr error
}

func bitis() *timestamppb.Timestamp { return timestamppb.New(time.Now().Add(8 * time.Hour)) }

func (f *fakeTotpRPC) Login(_ context.Context, in *kadranv1.LoginRequest, _ ...grpc.CallOption) (*kadranv1.LoginResponse, error) {
	f.login = in
	if f.loginErr != nil {
		return nil, f.loginErr
	}
	return &kadranv1.LoginResponse{SessionToken: totpYeniJton, ExpiresAt: bitis()}, nil
}

// fakeOtpauthURI, sunucunun yazdığı biçimde (internal/totp.OTPAuthURI).
const fakeOtpauthURI = "otpauth://totp/Kadran:srv?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=JBSWY3DPEHPK3PXP"

func (f *fakeTotpRPC) BeginTotpEnrollment(_ context.Context, in *kadranv1.BeginTotpEnrollmentRequest, _ ...grpc.CallOption) (*kadranv1.BeginTotpEnrollmentResponse, error) {
	f.begin = in
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return &kadranv1.BeginTotpEnrollmentResponse{
		OtpauthUri:   fakeOtpauthURI,
		SecretBase32: "JBSWY3DPEHPK3PXP",
		ExpiresAt:    timestamppb.New(time.Now().Add(10 * time.Minute)),
	}, nil
}

func (f *fakeTotpRPC) ConfirmTotpEnrollment(_ context.Context, in *kadranv1.ConfirmTotpEnrollmentRequest, _ ...grpc.CallOption) (*kadranv1.ConfirmTotpEnrollmentResponse, error) {
	f.confirm = in
	return &kadranv1.ConfirmTotpEnrollmentResponse{
		RecoveryCodes: []string{"AAAAA-BBBBB", "CCCCC-DDDDD"},
		SessionToken:  totpYeniJton,
		ExpiresAt:     bitis(),
	}, nil
}

func (f *fakeTotpRPC) DisableTotp(_ context.Context, in *kadranv1.DisableTotpRequest, _ ...grpc.CallOption) (*kadranv1.DisableTotpResponse, error) {
	f.disable = in
	return &kadranv1.DisableTotpResponse{}, nil
}

func (f *fakeTotpRPC) GetTotpStatus(context.Context, *kadranv1.GetTotpStatusRequest, ...grpc.CallOption) (*kadranv1.GetTotpStatusResponse, error) {
	return f.status, nil
}

func (f *fakeTotpRPC) ListAlarms(context.Context, *kadranv1.ListAlarmsRequest, ...grpc.CallOption) (*kadranv1.ListAlarmsResponse, error) {
	if f.alarmsErr != nil {
		return nil, f.alarmsErr
	}
	return &kadranv1.ListAlarmsResponse{}, nil
}

type totpDuzen struct {
	c       *cli
	out     *bytes.Buffer
	errOut  *bytes.Buffer
	rpc     *fakeTotpRPC
	store   client.SessionStore
	conns   []*client.Client
	targets []string
}

// totpCLI, sahte sunucuya bağlanan ve oturumlarını t.TempDir()'de tutan
// bir cli kurar. totpOn, Ping'in TOTP'yi açık bildirip bildirmediği.
func totpCLI(t *testing.T, stdin string, totpOn bool) *totpDuzen {
	t.Helper()
	c, out, errOut := newTestCLI(stdin)
	d := &totpDuzen{c: c, out: out, errOut: errOut, rpc: &fakeTotpRPC{}, store: client.SessionStore{Dir: t.TempDir()}}
	c.sessions = d.store
	c.dial = func(_ context.Context, raw string) (*client.Client, *kadranv1.PingResponse, error) {
		conn := client.NewWithRPC(d.rpc)
		d.conns, d.targets = append(d.conns, conn), append(d.targets, raw)
		return conn, &kadranv1.PingResponse{TotpRequired: proto.Bool(totpOn)}, nil
	}
	return d
}

func (d *totpDuzen) calistir(t *testing.T, args ...string) int {
	t.Helper()
	return d.c.run(context.Background(), args)
}

func (d *totpDuzen) kayitli(t *testing.T) string {
	t.Helper()
	tok, _, err := d.store.Load(totpAnahtar, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestLoginSavesSession(t *testing.T) {
	d := totpCLI(t, "123456\n", true)
	if code := d.calistir(t, "login", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.login.GetCode() != "123456" || d.rpc.login.GetRecoveryCode() != "" {
		t.Fatalf("Login isteği: %+v", d.rpc.login)
	}
	// "srv.example" ile "kadran-client@srv.example" aynı oturumu paylaşır.
	if got := d.kayitli(t); got != totpYeniJton {
		t.Fatalf("kayıtlı jeton %q", got)
	}
	if !strings.Contains(d.out.String(), "signed in until ") || !strings.Contains(d.errOut.String(), "6-digit code from the app: ") {
		t.Fatalf("çıktı: %q / %q", d.out, d.errOut)
	}
}

func TestLoginWithRecoveryCode(t *testing.T) {
	d := totpCLI(t, "abcde-fghij\n", true)
	if code := d.calistir(t, "login", "-recovery", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.login.GetRecoveryCode() != "abcde-fghij" || d.rpc.login.GetCode() != "" {
		t.Fatalf("Login isteği: %+v", d.rpc.login)
	}
	if !strings.Contains(d.errOut.String(), "recovery code: ") {
		t.Fatalf("istem: %q", d.errOut)
	}
}

func TestLoginWithoutCodeFails(t *testing.T) {
	d := totpCLI(t, "\n", true)
	if code := d.calistir(t, "login", totpHedef); code != exitError {
		t.Fatalf("çıkış %d", code)
	}
	if d.rpc.login != nil {
		t.Fatal("boş kodla Login çağrıldı")
	}
}

// TestCodeIsNeverAnArgument: kod kabuk geçmişine ve süreç listesine
// düşmesin diye hiçbir zaman argüman değil; altı haneli bir argüman da
// hedeftir.
func TestCodeIsNeverAnArgument(t *testing.T) {
	d := totpCLI(t, "654321\n", true)
	if code := d.calistir(t, "login", "123456"); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if len(d.targets) != 1 || d.targets[0] != "123456" || d.rpc.login.GetCode() != "654321" {
		t.Fatalf("hedef %v, kod %q", d.targets, d.rpc.login.GetCode())
	}
	d = totpCLI(t, "654321\n", true)
	if code := d.calistir(t, "login", "123456", totpHedef); code != exitUsage {
		t.Fatalf("iki argüman: çıkış %d", code)
	}
}

func TestCommandsSendSavedSession(t *testing.T) {
	d := totpCLI(t, "", true)
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if code := d.calistir(t, "alarms", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if got := d.conns[0].Session(); got != totpJeton {
		t.Fatalf("bağlantıdaki jeton %q", got)
	}
}

// TestSessionNotSentWhenTotpIsOff: TOTP kapalı sunucuya jeton gönderilmez.
func TestSessionNotSentWhenTotpIsOff(t *testing.T) {
	d := totpCLI(t, "", false)
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if code := d.calistir(t, "alarms", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if got := d.conns[0].Session(); got != "" {
		t.Fatalf("TOTP kapalıyken jeton gönderildi: %q", got)
	}
}

// bozOturum, kayıtlı tek oturum dosyasının içini bozar.
func (d *totpDuzen) bozOturum(t *testing.T) {
	t.Helper()
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(d.store.Dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("oturum dosyası: %v (%v)", entries, err)
	}
	if err := os.WriteFile(filepath.Join(d.store.Dir, entries[0].Name()), []byte("{bozuk"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestDamagedSessionDoesNotBlockLogin: bozuk bir oturum dosyası komutu
// durdurmaz; durdursaydı onu yeniden yazacak tek komut olan login de
// çalışmazdı.
func TestDamagedSessionDoesNotBlockLogin(t *testing.T) {
	d := totpCLI(t, "123456\n", true)
	d.bozOturum(t)
	if code := d.calistir(t, "login", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if !strings.Contains(d.errOut.String(), "warning: saved session is damaged") {
		t.Fatalf("uyarı yok: %q", d.errOut)
	}
	if got := d.kayitli(t); got != totpYeniJton {
		t.Fatalf("login bozuk dosyayı yenilemedi: %q", got)
	}
}

func TestSessionErrorPrintsLoginHint(t *testing.T) {
	d := totpCLI(t, "", true)
	d.rpc.alarmsErr = status.Error(codes.Unauthenticated, `session expired: sign in again with "kadran login"`)
	if code := d.calistir(t, "alarms", totpHedef); code != exitError {
		t.Fatalf("çıkış %d", code)
	}
	if !strings.Contains(d.errOut.String(), "session expired") ||
		!strings.Contains(d.errOut.String(), "\n  run: kadran login "+totpHedef+"\n") {
		t.Fatalf("ipucu: %q", d.errOut)
	}

	// Sunucunun kendisinde jeton alınamaz; "kadran login" ipucu bir döngü olurdu.
	d = totpCLI(t, "", true)
	d.rpc.alarmsErr = status.Error(codes.Unauthenticated,
		"TOTP is on for this server: admin commands need an SSH key identity, so run kadran from your workstation over SSH")
	if code := d.calistir(t, "alarms"); code != exitError {
		t.Fatalf("yerel: çıkış %d", code)
	}
	if strings.Contains(d.errOut.String(), "run: kadran login") {
		t.Fatalf("yerel hedefe login ipucu verildi: %q", d.errOut)
	}
}

func TestEnablePrintsRecoveryCodesOnce(t *testing.T) {
	d := totpCLI(t, "123456\n", false)
	if code := d.calistir(t, "totp", "enable", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.begin.GetCurrentCode() != "" || d.rpc.confirm.GetCode() != "123456" {
		t.Fatalf("istekler: %+v %+v", d.rpc.begin, d.rpc.confirm)
	}
	out := d.out.String()
	for _, want := range []string{"JBSWY3DPEHPK3PXP", "otpauth://totp/Kadran:srv", "recovery codes (shown once, store them offline):"} {
		if !strings.Contains(out, want) {
			t.Fatalf("çıktıda %q yok:\n%s", want, out)
		}
	}
	hepsi := out + d.errOut.String()
	for _, k := range []string{"AAAAA-BBBBB", "CCCCC-DDDDD"} {
		if n := strings.Count(hepsi, k); n != 1 {
			t.Fatalf("%s %d kez yazıldı", k, n)
		}
	}
	if got := d.kayitli(t); got != totpYeniJton {
		t.Fatalf("kurulumdan sonra oturum kaydedilmedi: %q", got)
	}
}

// TestReenrollSendsCurrentCode: TOTP açıkken önce şu anki kod, sonra yeni
// sırrın kodu soruluyor; ikisi de aynı stdin okuyucusundan.
func TestReenrollSendsCurrentCode(t *testing.T) {
	d := totpCLI(t, "111111\n222222\n", true)
	if code := d.calistir(t, "totp", "enable", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.begin.GetCurrentCode() != "111111" || d.rpc.confirm.GetCode() != "222222" {
		t.Fatalf("istekler: %+v %+v", d.rpc.begin, d.rpc.confirm)
	}
	if !strings.Contains(d.errOut.String(), "current 6-digit code from the app: ") {
		t.Fatalf("istem: %q", d.errOut)
	}
}

// ── Kurulum ve kod istemi (K-156) ────────────────────────────────────
//
// Canlıda (K-155, GCP) kurulum zordu: doğrulayıcı uygulama boşlukla
// gruplanmış anahtarı kabul etmedi, kullanıcı anahtarı "code:" istemine
// yazdı ve üç onay kilit sayacına eklendi.

// TestEnableShowsQRAndWholeKey: anahtar TEK PARÇA, yanında kısa linkin QR'ı;
// talimat anahtarın değil uygulamanın kodunun yazılacağını söylüyor.
func TestEnableShowsQRAndWholeKey(t *testing.T) {
	d := totpCLI(t, "123456\n", false)
	if code := d.calistir(t, "totp", "enable", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	out := d.out.String()
	if !strings.Contains(out, "JBSWY3DPEHPK3PXP") || strings.Contains(out, "JBSW Y3DP") {
		t.Fatalf("anahtar tek parça değil:\n%s", out)
	}
	// QR'da varsayılanları atılmış kısa link; yazılan link sunucununki, tam.
	q, err := qrBlock("otpauth://totp/Kadran:srv?issuer=Kadran&secret=JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, q) {
		t.Fatalf("QR kısa linkin QR'ı değil:\n%s", out)
	}
	if !strings.Contains(out, "link: "+fakeOtpauthURI+"\n") {
		t.Fatalf("link tam değil:\n%s", out)
	}
	if !strings.Contains(out, "(not the key)") || !strings.Contains(d.errOut.String(), "6-digit code from the app: ") {
		t.Fatalf("talimat ya da istem: %q / %q", out, d.errOut)
	}
}

// TestSetupKeyTypedAsCodeIsAskedAgain: anahtar sunucuya GİTMİYOR (onay
// kilide sayılırdı); ne olduğu söylenip yeniden soruluyor ve aynı kurulum
// sürüyor. Yeniden çalıştırmak yeni bir sır, telefonda yeni bir kayıt demekti.
func TestSetupKeyTypedAsCodeIsAskedAgain(t *testing.T) {
	d := totpCLI(t, "JBSW Y3DP EHPK 3PXP\n123456\n", false)
	if code := d.calistir(t, "totp", "enable", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.confirm.GetCode() != "123456" {
		t.Fatalf("onaya giden kod %q", d.rpc.confirm.GetCode())
	}
	if !strings.Contains(d.errOut.String(), "that is the setup key") {
		t.Fatalf("ipucu yok: %q", d.errOut)
	}
	if n := strings.Count(d.errOut.String(), "6-digit code from the app: "); n != 2 {
		t.Fatalf("istem %d kez soruldu", n)
	}
}

// TestMalformedCodeNeverReachesServer: 6 rakam olmayan giriş sunucuya hiç
// gitmiyor (kilit sayacına eklenmiyor); üç denemeden sonra komut duruyor.
func TestMalformedCodeNeverReachesServer(t *testing.T) {
	d := totpCLI(t, "12345\n1234567\n12345a\n", true)
	if code := d.calistir(t, "login", totpHedef); code != exitError {
		t.Fatalf("çıkış %d", code)
	}
	if d.rpc.login != nil || len(d.conns) != 0 {
		t.Fatalf("bozuk kod sunucuya gitti: %+v (%d bağlantı)", d.rpc.login, len(d.conns))
	}
	if n := strings.Count(d.errOut.String(), "6-digit code from the app: "); n != 3 {
		t.Fatalf("istem %d kez soruldu", n)
	}
	if !strings.Contains(d.errOut.String(), "a code is 6 digits") {
		t.Fatalf("ipucu yok: %q", d.errOut)
	}
}

// TestRecoveryShapedCodeSuggestsFlag: kurtarma kodu biçimindeki giriş
// sunucuya gitmiyor, -recovery öneriliyor.
func TestRecoveryShapedCodeSuggestsFlag(t *testing.T) {
	d := totpCLI(t, "ABCDE-FGHIJ\n123456\n", true)
	if code := d.calistir(t, "login", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.login.GetCode() != "123456" {
		t.Fatalf("Login kodu %q", d.rpc.login.GetCode())
	}
	if !strings.Contains(d.errOut.String(), "run the command again with -recovery") {
		t.Fatalf("ipucu yok: %q", d.errOut)
	}
}

// TestSpacedCodeIsSentWithoutSpaces: uygulamaların gösterdiği biçim (123
// 456) geçiyor ve boşluksuz gidiyor; sunucu da aynı boşlukları atıyor (K-155).
func TestSpacedCodeIsSentWithoutSpaces(t *testing.T) {
	d := totpCLI(t, "123 456\n", true)
	if code := d.calistir(t, "login", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.login.GetCode() != "123456" {
		t.Fatalf("Login kodu %q", d.rpc.login.GetCode())
	}
}

// TestCodePromptStopsAtEOF: borudan gelen girdi bitince istem döngüsü
// biter, takılmaz.
func TestCodePromptStopsAtEOF(t *testing.T) {
	d := totpCLI(t, "abc", true)
	if code := d.calistir(t, "login", totpHedef); code != exitError {
		t.Fatalf("çıkış %d", code)
	}
	if d.rpc.login != nil {
		t.Fatalf("Login çağrıldı: %+v", d.rpc.login)
	}
}

// TestDisableChecksCodeShape: kapatmanın kodu da aynı denetimden geçiyor;
// -recovery ise geçmiyor (TestDisableWithRecoveryFlag).
func TestDisableChecksCodeShape(t *testing.T) {
	d := totpCLI(t, "JBSWY3DPEHPK3PXP\n", true)
	if code := d.calistir(t, "totp", "disable", totpHedef); code != exitError {
		t.Fatalf("çıkış %d", code)
	}
	if d.rpc.disable != nil {
		t.Fatalf("DisableTotp çağrıldı: %+v", d.rpc.disable)
	}
}

// TestReenrollWithRecoveryFlag: telefonu kaybolan kullanıcı yeni telefonu
// kurtarma koduyla kurar.
func TestReenrollWithRecoveryFlag(t *testing.T) {
	d := totpCLI(t, "abcde-fghij\n222222\n", true)
	if code := d.calistir(t, "totp", "enable", "-recovery", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.begin.GetRecoveryCode() != "abcde-fghij" || d.rpc.begin.GetCurrentCode() != "" ||
		d.rpc.confirm.GetCode() != "222222" {
		t.Fatalf("istekler: %+v %+v", d.rpc.begin, d.rpc.confirm)
	}
	if !strings.Contains(d.errOut.String(), "recovery code: ") {
		t.Fatalf("istem: %q", d.errOut)
	}

	// TOTP kapalıyken -recovery anlamsız: değiştirilecek bir şey yok.
	d = totpCLI(t, "abcde-fghij\n", false)
	if code := d.calistir(t, "totp", "enable", "-recovery", totpHedef); code != exitError || d.rpc.begin != nil {
		t.Fatalf("TOTP kapalıyken -recovery: çıkış %d, begin %+v", code, d.rpc.begin)
	}
}

func TestDisableWithRecoveryFlag(t *testing.T) {
	d := totpCLI(t, "abcde-fghij\n", true)
	if code := d.calistir(t, "totp", "disable", "-recovery", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.disable.GetRecoveryCode() != "abcde-fghij" || d.rpc.disable.GetCode() != "" {
		t.Fatalf("DisableTotp isteği: %+v", d.rpc.disable)
	}
}

func TestTotpUnimplementedSaysUpgrade(t *testing.T) {
	eski := status.Error(codes.Unimplemented, "unknown method Login for service kadran.v1.KadranService")
	for _, args := range [][]string{{"login", totpHedef}, {"totp", "enable", totpHedef}} {
		d := totpCLI(t, "123456\n", false)
		d.rpc.loginErr, d.rpc.beginErr = eski, eski
		if code := d.calistir(t, args...); code != exitError {
			t.Fatalf("%v: çıkış %d", args, code)
		}
		if !strings.Contains(d.errOut.String(), "this server does not support TOTP; upgrade it with kadran bootstrap") {
			t.Fatalf("%v: %q", args, d.errOut)
		}
	}
}

func TestDisableDeletesSession(t *testing.T) {
	d := totpCLI(t, "123456\n", true)
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if code := d.calistir(t, "totp", "disable", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if d.rpc.disable.GetCode() != "123456" {
		t.Fatalf("DisableTotp isteği: %+v", d.rpc.disable)
	}
	if got := d.kayitli(t); got != "" {
		t.Fatalf("oturum silinmedi: %q", got)
	}
}

func TestLogoutDeletesSessionWithoutServer(t *testing.T) {
	d := totpCLI(t, "", true)
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if code := d.calistir(t, "logout", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if len(d.conns) != 0 {
		t.Fatal("logout sunucuya bağlandı")
	}
	if got := d.kayitli(t); got != "" {
		t.Fatalf("oturum silinmedi: %q", got)
	}
}

func TestTotpStatusPrints(t *testing.T) {
	d := totpCLI(t, "", true)
	d.rpc.status = &kadranv1.GetTotpStatusResponse{
		Enabled:           true,
		ConfirmedAt:       timestamppb.New(time.Now().Add(-time.Hour)),
		RecoveryRemaining: 2,
		Locked: []*kadranv1.LockedLogin{{
			KeyFingerprint: "SHA256:kilitli", Until: timestamppb.New(time.Now().Add(time.Minute)), Failures: 5,
		}},
	}
	if code := d.calistir(t, "totp", "status", totpHedef); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	for _, want := range []string{"TOTP: on", "recovery codes left: 2", "SHA256:kilitli", "5 failures"} {
		if !strings.Contains(d.out.String(), want) {
			t.Fatalf("çıktıda %q yok:\n%s", want, d.out)
		}
	}
	if !strings.Contains(d.errOut.String(), "kadran totp enable") {
		t.Fatalf("az kurtarma kodu uyarısı yok: %q", d.errOut)
	}
}

// TestTotpResetUsesRootPath: sıfırlama kadran-client'tan değil, anahtar
// işlemleriyle aynı root yolundan (root@ ya da -sudo) gidiyor.
func TestTotpResetUsesRootPath(t *testing.T) {
	d := totpCLI(t, "", true)
	var got bootstrap.KeyOptions
	d.c.totpReset = func(_ context.Context, o bootstrap.KeyOptions, w io.Writer) error {
		got = o
		_, err := io.WriteString(w, "TOTP disabled; every session is invalid\n")
		return err
	}
	if code := d.calistir(t, "totp", "reset", "-sudo", "erkan@srv.example:2222"); code != exitOK {
		t.Fatalf("çıkış %d: %s", code, d.errOut)
	}
	if got != (bootstrap.KeyOptions{Host: "erkan@srv.example", Port: 2222, Sudo: true}) {
		t.Fatalf("seçenekler %+v", got)
	}
	if !strings.Contains(d.out.String(), "every session is invalid") || len(d.conns) != 0 {
		t.Fatalf("çıktı %q, kadran-client bağlantısı %d", d.out, len(d.conns))
	}

	d = totpCLI(t, "", true)
	d.c.totpReset = func(context.Context, bootstrap.KeyOptions, io.Writer) error {
		t.Fatal("yerel hedefle sıfırlama çağrıldı")
		return nil
	}
	if code := d.calistir(t, "totp", "reset"); code != exitUsage {
		t.Fatalf("hedefsiz: çıkış %d", code)
	}
	if code := d.calistir(t, "totp", "reset", "/run/kadran/api.sock"); code != exitError {
		t.Fatalf("yerel soket: çıkış %d", code)
	}
}

// TestSidecarSendsSavedSession: masaüstü açıkken CLI'da yapılan login ve
// logout, ÖNBELLEKTEKİ bağlantının bir sonraki çağrısında geçerli.
func TestSidecarSendsSavedSession(t *testing.T) {
	d := totpCLI(t, "", true)
	conn := client.NewWithRPC(d.rpc)
	s := &sidecar{cli: d.c, callTimeout: 5 * time.Second, conns: map[string]*client.Client{totpAnahtar: conn}}
	params, err := json.Marshal(targetParams{Target: totpHedef})
	if err != nil {
		t.Fatal(err)
	}
	gorulen := func() string {
		var tok string
		if _, fail := s.withConn(context.Background(), params, func(_ context.Context, c *client.Client) (any, error) {
			tok = c.Session()
			return nil, nil
		}); fail != nil {
			t.Fatalf("çağrı: %+v", fail)
		}
		return tok
	}

	if got := gorulen(); got != "" {
		t.Fatalf("oturum yokken jeton: %q", got)
	}
	if err := d.store.Save(totpAnahtar, totpJeton, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := gorulen(); got != totpJeton {
		t.Fatalf("login'den sonra jeton %q", got)
	}
	if err := d.store.Delete(totpAnahtar); err != nil {
		t.Fatal(err)
	}
	if got := gorulen(); got != "" {
		t.Fatalf("logout'tan sonra jeton %q", got)
	}
}
