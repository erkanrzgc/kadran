package api

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/erkanrzgc/kadran/internal/alarm"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── Tüm-dal incelemesinin bulguları (K-155) ───────────────────────────

const mesajKurtarmaRed = "recovery code rejected"

// kayitSink, gerçek alarm.Manager'ın bildirdiği olayları toplar. Sahte
// alarm her Raise'i kaydediyordu ve "yalnız yeni alarmda bildir"
// davranışını gizliyordu.
type kayitSink struct {
	mu sync.Mutex
	ev []alarm.Event
}

func (k *kayitSink) Notify(_ context.Context, ev alarm.Event) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.ev = append(k.ev, ev)
}

func (k *kayitSink) say(state alarm.Transition, kind string) int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, e := range k.ev {
		if e.State == state && (kind == "" || e.Alarm.Kind == kind) {
			n++
		}
	}
	return n
}

func gercekAlarm(d *totpDuzenek) *kayitSink {
	sink := &kayitSink{}
	d.srv.alarms = alarm.New(d.db, sink)
	return sink
}

func totpAlarmlari(t *testing.T, db *store.Store) []store.Alarm {
	t.Helper()
	all, err := db.ListAlarms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Alarm
	for _, a := range all {
		if a.Kind == alarm.KindLoginLockout || a.Kind == alarm.KindLoginRecovery {
			out = append(out, a)
		}
	}
	return out
}

// TestStepUpAcceptsRecoveryCode: telefonu kaybolan kullanıcı kurtarma
// koduyla girer ve yeni telefonu da kurtarma koduyla kurar (ya da TOTP'yi
// kapatır); kök şart değil. Kurtarma kodu da kilide sayılır ve bir kez
// geçer.
func TestStepUpAcceptsRecoveryCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	ctx := oturumla(d.jeton)

	_, err := c.BeginTotpEnrollment(ctx, &kadranv1.BeginTotpEnrollmentRequest{RecoveryCode: "AAAAA-AAAAA"})
	durumMesaj(t, err, codes.PermissionDenied, mesajKurtarmaRed)
	_, err = c.BeginTotpEnrollment(ctx, &kadranv1.BeginTotpEnrollmentRequest{
		CurrentCode: "123456", RecoveryCode: d.kodlar[0],
	})
	durumMesaj(t, err, codes.InvalidArgument, "")
	if f, err := d.db.LoginFailureFor(context.Background(), yonetici.Fingerprint); err != nil || f.Count != 1 {
		t.Fatalf("yanlış kurtarma kodu sayılmadı ya da ikili istek sayıldı: %+v (%v)", f, err)
	}

	if _, err := c.BeginTotpEnrollment(ctx,
		&kadranv1.BeginTotpEnrollmentRequest{RecoveryCode: strings.ToLower(d.kodlar[0])}); err != nil {
		t.Fatalf("kurtarma koduyla yeniden kurulum: %v", err)
	}
	_, err = c.DisableTotp(ctx, &kadranv1.DisableTotpRequest{RecoveryCode: d.kodlar[0]})
	durumMesaj(t, err, codes.PermissionDenied, mesajKurtarmaRed)
	if _, err := c.DisableTotp(ctx, &kadranv1.DisableTotpRequest{RecoveryCode: d.kodlar[1]}); err != nil {
		t.Fatalf("kurtarma koduyla kapatma: %v", err)
	}
	if st, _ := d.db.TOTP(context.Background()); st.Enabled() {
		t.Fatal("kurtarma koduyla kapatılmadı")
	}
}

// TestRecoveryAlarmNotifiesEveryUse: her kurtarma kodu kullanımı GERÇEK
// alarm yöneticisinden bildirim üretir; sabit kimlikli bir alarm yalnız
// ilkinde bildirirdi.
func TestRecoveryAlarmNotifiesEveryUse(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	sink := gercekAlarm(d)
	c := d.istemci(yonetici)
	for _, k := range d.kodlar[:2] {
		if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{RecoveryCode: k}); err != nil {
			t.Fatal(err)
		}
	}
	if n := sink.say(alarm.Opened, alarm.KindLoginRecovery); n != 2 {
		t.Fatalf("iki kurtarma kodu kullanımı, %d bildirim", n)
	}
}

// TestDisableClosesTotpAlarms: TOTP kapanınca kilit ve kurtarma alarmları
// da kapanır; kalsalar, yeniden açıldıktan sonra aynı anahtarın yeni
// kilidi bildirim üretmezdi.
func TestDisableClosesTotpAlarms(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	gercekAlarm(d)
	for range totp.LockAfter {
		_, _ = d.istemci(baskasi).Login(context.Background(), &kadranv1.LoginRequest{Code: "99999x"})
	}
	c := d.istemci(yonetici)
	if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{RecoveryCode: d.kodlar[0]}); err != nil {
		t.Fatal(err)
	}
	if n := len(totpAlarmlari(t, d.db)); n != 2 {
		t.Fatalf("düzenek: 2 TOTP alarmı beklenirdi, %d", n)
	}
	d.now = d.now.Add(totp.Period)
	if _, err := c.DisableTotp(oturumla(d.jeton),
		&kadranv1.DisableTotpRequest{Code: totp.CodeAt(d.sir, d.now)}); err != nil {
		t.Fatal(err)
	}
	if kalan := totpAlarmlari(t, d.db); len(kalan) != 0 {
		t.Fatalf("kapatmadan sonra kalan TOTP alarmları: %+v", kalan)
	}
}

// TestReenrollClosesRecoveryAlarms: yeni sır ve yeni kurtarma kodları
// "kurtarma kodu kullanıldı" koşulunu kapatır.
func TestReenrollClosesRecoveryAlarms(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	gercekAlarm(d)
	if _, err := d.istemci(yonetici).Login(context.Background(),
		&kadranv1.LoginRequest{RecoveryCode: d.kodlar[0]}); err != nil {
		t.Fatal(err)
	}
	d.now = d.now.Add(totp.Period)
	d.etkinlestir()
	for _, a := range totpAlarmlari(t, d.db) {
		if a.Kind == alarm.KindLoginRecovery {
			t.Fatalf("yeniden kurulumdan sonra kurtarma alarmı kaldı: %+v", a)
		}
	}
}

// TestReenrollRotatesTokenKey: yeniden kurulum imza anahtarını döndürür.
// Dönmeseydi, eski bir veritabanı kopyasını (yerel yedek) okuyan biri bir
// yönetici anahtarıyla YENİ kuşak için jeton basabilirdi; kuşak tahmin
// edilebilir bir sayaç.
func TestReenrollRotatesTokenKey(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	eski, err := d.db.TOTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.now = d.now.Add(totp.Period)
	d.etkinlestir()
	yeni, err := d.db.TOTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(eski.TokenKey, yeni.TokenKey) {
		t.Fatal("yeniden kurulum imza anahtarını döndürmedi")
	}
	sahte := totp.SignToken(eski.TokenKey, totp.Claims{
		Fingerprint: yonetici.Fingerprint, Expires: d.now.Add(time.Hour), Generation: yeni.Generation,
	})
	_, err = d.istemci(yonetici).ListApps(oturumla(sahte), &kadranv1.ListAppsRequest{})
	durumMesaj(t, err, codes.Unauthenticated, "")
}

// TestAdminStreamNeedsSession: akış çağrıları da oturum ister (günlük
// akışı, dağıtım).
func TestAdminStreamNeedsSession(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	s, err := d.istemci(yonetici).StreamLogs(context.Background(), &kadranv1.StreamLogsRequest{AppId: "blog"})
	if err == nil {
		_, err = s.Recv()
	}
	durumMesaj(t, err, codes.Unauthenticated, mesajJetonYok)
}

// TestStaleReadCannotReuseAcceptedCode: iki bağlantı aynı kodu aynı anda
// getirir ve ikisi de durumu kabulden ÖNCE okur. Verify ikisini de
// geçirir; yalnız koşullu kabul ikinciyi durdurur. Sıra burada elle
// kuruluyor, zamanlamaya bırakılmıyor.
func TestStaleReadCannotReuseAcceptedCode(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.Period)
	bayat, err := d.db.TOTP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kod := totp.CodeAt(d.sir, d.now)
	if _, err := d.istemci(yonetici).Login(context.Background(), &kadranv1.LoginRequest{Code: kod}); err != nil {
		t.Fatal(err)
	}
	err = d.srv.checkCode(context.Background(), "auth.login", yonetici.Fingerprint, bayat, kod, store.LoginFailure{}, d.now)
	durumMesaj(t, err, codes.PermissionDenied, "code already used: wait for the next one")
}

// TestConcurrentSameCodeOneWins: aynı kod beş bağlantıdan aynı anda; tam
// bir giriş kazanır.
func TestConcurrentSameCodeOneWins(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.Period)
	kod := totp.CodeAt(d.sir, d.now)
	var istemciler []kadranv1.KadranServiceClient
	for range totp.LockAfter {
		istemciler = append(istemciler, d.istemci(yonetici))
	}
	var kazanan atomic.Int32
	var wg sync.WaitGroup
	for _, c := range istemciler {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Login(context.Background(), &kadranv1.LoginRequest{Code: kod}); err == nil {
				kazanan.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := kazanan.Load(); n != 1 {
		t.Fatalf("aynı kodla %d giriş kazandı", n)
	}
}

// TestSpacedCodeIsAccepted: uygulamalar kodu "123 456" diye gösteriyor;
// boşluklu yazılan kod reddedilip kilide sayılmasın, "saatini kontrol et"
// diye yanlış yere de yollamasın.
func TestSpacedCodeIsAccepted(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	d.now = d.now.Add(totp.Period)
	kod := totp.CodeAt(d.sir, d.now)
	if _, err := d.istemci(yonetici).Login(context.Background(),
		&kadranv1.LoginRequest{Code: kod[:3] + " " + kod[3:]}); err != nil {
		t.Fatalf("boşluklu kod: %v", err)
	}
}
