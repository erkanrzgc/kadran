package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/bootstrap"
	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── İki aşamalı giriş komutları (K-155) ───────────────────────────────
//
// Kod hiçbir zaman komut satırı argümanı değil: kabuk geçmişine ve süreç
// listesine düşerdi. stdin'den okunur; istem stderr'e yazılır ki stdout
// (kurtarma kodları) bir betikte ayrı yakalanabilsin.

const msgTOTPUnsupported = "this server does not support TOTP; upgrade it with kadran bootstrap"

// lowRecoveryCodes, bu sayıda ya da daha az kurtarma kodu kalınca uyarılır.
const lowRecoveryCodes = 3

// sessionStore, oturum dizinidir. nil alıcıda da çalışır: sidecar'ın
// bazı testleri cli'sız kuruluyor.
func (c *cli) sessionStore() (client.SessionStore, error) {
	if c != nil && c.sessions.Dir != "" {
		return c.sessions, nil
	}
	return client.DefaultSessionStore()
}

// sessionKey, hedefin oturum anahtarıdır: "srv" ile "kadran-client@srv"
// aynı oturumu paylaşır. Oturuma dokunan her yol bunu kullanır.
func sessionKey(rawTarget string) (string, error) {
	t, err := client.ParseTarget(rawTarget)
	if err != nil {
		return "", err
	}
	return t.String(), nil
}

// loadSession, hedefin kayıtlı ve süresi dolmamış jetonunu döner (yoksa boş).
func (c *cli) loadSession(rawTarget string) (string, error) {
	key, err := sessionKey(rawTarget)
	if err != nil {
		return "", err
	}
	store, err := c.sessionStore()
	if err != nil {
		return "", err
	}
	tok, _, err := store.Load(key, time.Now())
	return tok, err
}

// attachSession, kayıtlı jetonu bağlantıya ekler. Okunamayan bir oturum
// dosyası komutu DURDURMAZ, uyarır: login de connect'ten geçiyor ve dosyayı
// yeniden yazacak tek komut o.
func (c *cli) attachSession(conn *client.Client, rawTarget string) {
	tok, err := c.loadSession(rawTarget)
	if err != nil {
		fmt.Fprintln(c.stderr, progName+": warning:", err)
	}
	conn.SetSession(tok)
}

func (c *cli) saveSession(rawTarget, token string, expires time.Time) error {
	key, err := sessionKey(rawTarget)
	if err != nil {
		return err
	}
	store, err := c.sessionStore()
	if err != nil {
		return err
	}
	return store.Save(key, token, expires)
}

func (c *cli) deleteSession(rawTarget string) (string, error) {
	key, err := sessionKey(rawTarget)
	if err != nil {
		return "", err
	}
	store, err := c.sessionStore()
	if err != nil {
		return "", err
	}
	return key, store.Delete(key)
}

// prompt, stdin'den bir satır okur. Komut başına TEK okuyucu kullanılmalı:
// her istem için yeni bir bufio.Reader ileriyi tamponlayıp sonraki satırı
// yutardı.
func (c *cli) prompt(in *bufio.Reader, label string) (string, error) {
	fmt.Fprintf(c.stderr, "%s: ", label)
	line, err := in.ReadString('\n')
	line = strings.TrimSpace(line)
	if line != "" {
		return line, nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("could not read the %s: %w", label, err)
	}
	return "", fmt.Errorf("no %s entered", label)
}

// totpErr, TOTP'yi bilmeyen eski bir sunucunun cevabını anlaşılır yapar.
func totpErr(what string, err error) error {
	if status.Code(err) == codes.Unimplemented {
		return errors.New(msgTOTPUnsupported)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func localTime(t time.Time) string { return t.Local().Format("2006-01-02 15:04 MST") }

// singleTarget, en çok bir konumsal argümanı (hedef) kabul eder.
func singleTarget(args []string) (string, bool) {
	switch len(args) {
	case 0:
		return "", true
	case 1:
		return args[0], true
	default:
		return "", false
	}
}

// runLogin, bir TOTP ya da kurtarma koduyla oturum jetonu alır ve saklar.
func (c *cli) runLogin(ctx context.Context, args []string) int {
	fs := c.newFlagSet("login")
	recovery := fs.Bool("recovery", false, "sign in with a recovery code instead of a TOTP code")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	raw, ok := singleTarget(fs.Args())
	if !ok {
		return c.usageError("usage: kadran login [-recovery] [target] (the code is read from stdin, never an argument)")
	}

	// Kod bağlanmadan önce soruluyor: kullanıcı telefonunu ararken bir
	// çağrı süresi işlemesin.
	label := "code"
	if *recovery {
		label = "recovery code"
	}
	code, err := c.prompt(bufio.NewReader(c.stdin), label)
	if err != nil {
		return c.fail(err)
	}
	req := &kadranv1.LoginRequest{Code: code}
	if *recovery {
		req = &kadranv1.LoginRequest{RecoveryCode: code}
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	conn, _, err := c.connect(ctx, raw)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().Login(ctx, req)
	if err != nil {
		return c.fail(totpErr("sign-in failed", err))
	}
	exp := resp.GetExpiresAt().AsTime()
	if err := c.saveSession(raw, resp.GetSessionToken(), exp); err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "signed in until %s\n", localTime(exp))
	return exitOK
}

// runLogout, hedefin kayıtlı jetonunu bu makineden siler. Sunucuya
// bağlanmaz; jeton sunucuda süresi dolana kadar geçerli kalır, ama bu
// makinede artık yoktur.
func (c *cli) runLogout(_ context.Context, args []string) int {
	raw, ok := singleTarget(args)
	if !ok {
		return c.usageError("usage: kadran logout [target]")
	}
	key, err := c.deleteSession(raw)
	if err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "saved session for %s deleted from this machine\n", key)
	return exitOK
}

const totpUsage = "usage:\n" +
	"  kadran totp status|enable|disable [target]\n" +
	"  kadran totp reset [-sudo] root@server   (lost phone and recovery codes)"

// runTotp, TOTP'yi yönetir.
func (c *cli) runTotp(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("%s", totpUsage)
	}
	if args[0] == "reset" {
		return c.runTotpReset(ctx, args[1:])
	}
	raw, ok := singleTarget(args[1:])
	if !ok {
		return c.usageError("%s", totpUsage)
	}
	switch args[0] {
	case "status":
		return c.runTotpStatus(ctx, raw)
	case "enable":
		return c.runTotpEnable(ctx, raw)
	case "disable":
		return c.runTotpDisable(ctx, raw)
	default:
		return c.usageError("unknown totp subcommand %q — status, enable, disable or reset", args[0])
	}
}

// runTotpReset, kökün kaçış yolu: TOTP'yi sunucudaki root olarak kapatır.
// kadran-client'tan değil, anahtar işlemleriyle aynı root yolundan gider;
// TOTP o yolu korumuyor (SSH anahtarı + root ya da parolasız sudo).
func (c *cli) runTotpReset(ctx context.Context, args []string) int {
	fs := c.newFlagSet("totp reset")
	sudo := fs.Bool("sudo", false, "through the target user's PASSWORDLESS sudo (never asks for a password)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		return c.usageError("%s", totpUsage)
	}
	opts, err := rootTarget("totp reset", fs.Arg(0), *sudo, c.totpReset != nil)
	if err != nil {
		return c.fail(err)
	}
	reset := c.totpReset
	if reset == nil {
		reset = bootstrap.ResetTOTP
	}
	ctx, cancel := context.WithTimeout(ctx, keyTimeout)
	defer cancel()
	if err := reset(ctx, opts, c.stdout); err != nil {
		return c.fail(err)
	}
	fmt.Fprintln(c.stdout, "turn it back on with kadran totp enable (new secret, new recovery codes)")
	return exitOK
}

func (c *cli) runTotpStatus(ctx context.Context, raw string) int {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	conn, _, err := c.connect(ctx, raw)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	st, err := conn.RPC().GetTotpStatus(ctx, &kadranv1.GetTotpStatusRequest{})
	if err != nil {
		return c.fail(totpErr("totp status", err))
	}
	if !st.GetEnabled() {
		fmt.Fprintln(c.stdout, "TOTP: off")
		return exitOK
	}
	fmt.Fprintf(c.stdout, "TOTP: on since %s\n", localTime(st.GetConfirmedAt().AsTime()))
	left := st.GetRecoveryRemaining()
	fmt.Fprintf(c.stdout, "recovery codes left: %d\n", left)
	if len(st.GetLocked()) > 0 {
		fmt.Fprintln(c.stdout, "locked keys:")
		for _, l := range st.GetLocked() {
			fmt.Fprintf(c.stdout, "  %s  until %s  (%d failures)\n",
				l.GetKeyFingerprint(), localTime(l.GetUntil().AsTime()), l.GetFailures())
		}
	}
	if left <= lowRecoveryCodes {
		fmt.Fprintln(c.stderr,
			"WARNING: few recovery codes left: run kadran totp enable to replace the secret and get new codes")
	}
	return exitOK
}

// runTotpEnable, TOTP'yi kurar ya da (açıksa) yeniden kurar.
func (c *cli) runTotpEnable(ctx context.Context, raw string) int {
	in := bufio.NewReader(c.stdin)
	connCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	conn, ping, err := c.connect(connCtx, raw)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	req := &kadranv1.BeginTotpEnrollmentRequest{}
	if ping.GetTotpRequired() {
		// Yeniden kurulum: oturumun yanında şu anki sırrın kodu (K-155).
		fmt.Fprintln(c.stderr, "TOTP is already on: replacing it needs a code from your current authenticator")
		if req.CurrentCode, err = c.prompt(in, "current code"); err != nil {
			return c.fail(err)
		}
	}
	callCtx, cancelCall := context.WithTimeout(ctx, defaultTimeout)
	b, err := conn.RPC().BeginTotpEnrollment(callCtx, req)
	cancelCall()
	if err != nil {
		return c.fail(totpErr("totp enable", err))
	}

	fmt.Fprintln(c.stdout, "add this to your authenticator app, then enter the code it shows")
	fmt.Fprintf(c.stdout, "  secret: %s\n", groupSecret(b.GetSecretBase32()))
	fmt.Fprintf(c.stdout, "  uri:    %s\n", b.GetOtpauthUri())
	fmt.Fprintf(c.stdout, "  (confirm before %s)\n", localTime(b.GetExpiresAt().AsTime()))
	code, err := c.prompt(in, "code")
	if err != nil {
		return c.fail(err)
	}

	callCtx, cancelCall = context.WithTimeout(ctx, defaultTimeout)
	r, err := conn.RPC().ConfirmTotpEnrollment(callCtx, &kadranv1.ConfirmTotpEnrollmentRequest{Code: code})
	cancelCall()
	if err != nil {
		return c.fail(totpErr("totp enable", err))
	}

	fmt.Fprintln(c.stdout, "TOTP is on.")
	fmt.Fprintln(c.stdout, "recovery codes (shown once, store them offline):")
	for _, rc := range r.GetRecoveryCodes() {
		fmt.Fprintf(c.stdout, "  %s\n", rc)
	}
	exp := r.GetExpiresAt().AsTime()
	if err := c.saveSession(raw, r.GetSessionToken(), exp); err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "signed in until %s\n", localTime(exp))
	return exitOK
}

// runTotpDisable, TOTP'yi kapatır; sunucu oturumun yanında taze bir kod ister.
func (c *cli) runTotpDisable(ctx context.Context, raw string) int {
	code, err := c.prompt(bufio.NewReader(c.stdin), "code")
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	conn, _, err := c.connect(ctx, raw)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.RPC().DisableTotp(ctx, &kadranv1.DisableTotpRequest{Code: code}); err != nil {
		return c.fail(totpErr("totp disable", err))
	}
	if _, err := c.deleteSession(raw); err != nil {
		fmt.Fprintln(c.stderr, progName+": warning:", err)
	}
	fmt.Fprintln(c.stdout, "TOTP is off")
	return exitOK
}

// groupSecret, base32 sırrı elle girmek için dörtlü gruplara ayırır.
func groupSecret(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
