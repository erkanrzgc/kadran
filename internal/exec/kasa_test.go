package exec

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/vault"
	"github.com/erkanrzgc/kadran/internal/vault/vaulttest"
)

// Bu dosya kasanın executor payını sınar (K-123): daemon değerleri
// executor'ın açık anahtarıyla mühürlüyor, executor konteyneri kurarken
// açıyor. Asıl soru: ele geçirilmiş bir daemon'un gönderebileceği en kötü
// mühürlü harita ne yapabilir?

// Executor vault paketini içe aktarmıyor (yüzey sayımı); önek iki yerde
// tanımlı. Ayrışırlarsa her dağıtım düşerdi.
func TestSealPrefixMatchesDaemon(t *testing.T) {
	if sealPrefix != vault.Prefix {
		t.Fatalf("executor %q, daemon %q", sealPrefix, vault.Prefix)
	}
}

// testVaultIdentity, newTestServer'ın executor'ına verilen kasa anahtarı.
var testVaultIdentity = func() *age.X25519Identity {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		panic(err)
	}
	return id
}()

// mustSealEnv, testVaultIdentity'ye mühürler (t'siz kurgular için).
func mustSealEnv(appID string, env map[string]string) map[string]string {
	s, err := vault.NewSealer(testVaultIdentity.Recipient().String())
	if err != nil {
		panic(err)
	}
	out, err := s.SealEnv(appID, env)
	if err != nil {
		panic(err)
	}
	return out
}

func sealedEnv(t *testing.T, id *age.X25519Identity, appID string, env map[string]string) map[string]string {
	t.Helper()
	out, err := vaulttest.Sealer(t, id).SealEnv(appID, env)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenEnvOpensSealedValues(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	want := map[string]string{"DATABASE_URL": "postgres://u:p@db/x", "BOS": ""}

	got, err := openEnv(id, "blog", sealedEnv(t, id, "blog", want))
	if err != nil {
		t.Fatalf("mühürlü değerler açılmadı: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("%d değer, %d bekleniyordu", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, %q bekleniyordu", k, got[k], v)
		}
	}
}

// Kasa zorunlu: öneksiz değer, daemon'un kasayı atlaması demek.
func TestOpenEnvRejectsPlaintext(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	env := sealedEnv(t, id, "blog", map[string]string{"A": "x"})
	env["PORT"] = "3000"
	if _, err := openEnv(id, "blog", env); err == nil {
		t.Fatal("şifresiz değer kabul edildi")
	}
}

// Bağ: başka uygulamanın ya da başka adın değeri açılmamalı. Yoksa
// daemon A'nın parolasını B'ye ya da A'nın günlüğe yazdığı bir değişkene
// kopyalayıp açtırırdı.
func TestOpenEnvRejectsValueMovedToAnotherAppOrKey(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	blog := sealedEnv(t, id, "blog", map[string]string{"DB_PASSWORD": "gizli"})

	if _, err := openEnv(id, "shop", blog); err == nil {
		t.Error("başka uygulamanın değeri açıldı")
	}
	tasinmis := map[string]string{"LOG_PREFIX": blog["DB_PASSWORD"]}
	if _, err := openEnv(id, "blog", tasinmis); err == nil {
		t.Error("başka adın değeri açıldı")
	}
	// Önek çakışması: "blog" uygulamasının değeri "blog2" için açılmamalı,
	// "A" adının değeri "AB" için de.
	if _, err := openEnv(id, "blog2", blog); err == nil {
		t.Error("önek eşleşmesiyle başka uygulamanın değeri açıldı")
	}
	ab := sealedEnv(t, id, "blog", map[string]string{"A": "x"})
	if _, err := openEnv(id, "blog", map[string]string{"AB": ab["A"]}); err == nil {
		t.Error("önek eşleşmesiyle başka adın değeri açıldı")
	}
}

func TestOpenEnvRejectsWrongKey(t *testing.T) {
	baska := vaulttest.NewIdentity(t)
	env := sealedEnv(t, baska, "blog", map[string]string{"A": "x"})
	if _, err := openEnv(vaulttest.NewIdentity(t), "blog", env); err == nil {
		t.Fatal("başka anahtarla mühürlenmiş değer açıldı")
	}
}

// Yalnız X25519: parolalı (scrypt) bir alıcıyla mühürlenmiş değer, ne
// kadar iyi biçimli olursa olsun açılmamalı.
func TestOpenEnvRejectsScryptRecipient(t *testing.T) {
	r, err := age.NewScryptRecipient("parola")
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(10)
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("blog\x00A\x00x")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"A": sealPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())}
	if _, err := openEnv(vaulttest.NewIdentity(t), "blog", env); err == nil {
		t.Fatal("scrypt ile mühürlenmiş değer açıldı")
	}
}

// Boyut, base64 çözülmeden ÖNCE denetlenmeli. Değer kasten geçersiz
// base64: önce çözülseydi hata "bozuk" olurdu, "büyük" değil.
func TestOpenEnvRejectsOversizedBeforeDecoding(t *testing.T) {
	env := map[string]string{"A": sealPrefix + strings.Repeat("!", maxSealedValue+1)}
	_, err := openEnv(vaulttest.NewIdentity(t), "blog", env)
	if err == nil || !strings.Contains(err.Error(), "büyük") {
		t.Fatalf("aşırı büyük değer boyutundan reddedilmedi: %v", err)
	}
}

func TestOpenEnvRejectsMalformedBase64(t *testing.T) {
	env := map[string]string{"A": sealPrefix + "bu-base64-degil!"}
	if _, err := openEnv(vaulttest.NewIdentity(t), "blog", env); err == nil {
		t.Fatal("bozuk base64 kabul edildi")
	}
}

// Açılan değer de doğrulanıyor: NUL execve dizisini keser, toplam sınırı
// mühürlü haritada değil açılmış haritada anlamlı.
func TestOpenEnvValidatesDecryptedValues(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	if _, err := openEnv(id, "blog", sealedEnv(t, id, "blog", map[string]string{"A": "a\x00b"})); err == nil {
		t.Error("NUL içeren açılmış değer kabul edildi")
	}
	buyuk := map[string]string{
		"A": strings.Repeat("x", maxEnvBytes/2),
		"B": strings.Repeat("y", maxEnvBytes/2),
	}
	if _, err := openEnv(id, "blog", sealedEnv(t, id, "blog", buyuk)); err == nil {
		t.Error("toplamı sınırı aşan açılmış harita kabul edildi")
	}
}

// Kontrol: sınırlar meşru en büyük isteği reddetmemeli. API düz metinde
// 200 girdi ve 32 KiB toplam kabul ediyor; mühürlenmiş hâli executor'ın
// ham doğrulamasından ve tek değer sınırından geçmeli.
func TestLargestLegitimateEnvPassesSealedLimits(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	app := strings.Repeat("a", maxAppIDLen)

	tek := map[string]string{"K": strings.Repeat("x", maxEnvBytes-1)}
	if _, err := openEnv(id, app, sealedEnv(t, id, app, tek)); err != nil {
		t.Errorf("32 KiB'lık tek değer reddedildi: %v", err)
	}

	cok := make(map[string]string, maxEnvEntries)
	pay := maxEnvBytes / maxEnvEntries
	for i := range maxEnvEntries {
		k := "K" + strings.Repeat("Z", 9) + string(rune('A'+i/26)) + string(rune('A'+i%26))
		cok[k] = strings.Repeat("v", pay-len(k))
	}
	sealed := sealedEnv(t, id, app, cok)
	if err := validateEnv(sealed, maxSealedEnvBytes); err != nil {
		t.Fatalf("meşru en büyük mühürlü harita ham doğrulamadan geçmedi: %v", err)
	}
	if _, err := openEnv(id, app, sealed); err != nil {
		t.Fatalf("meşru en büyük harita açılmadı: %v", err)
	}
}

func writeKey(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vault.key")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadVaultIdentity(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	got, err := LoadVaultIdentity(writeKey(t, id.String()+"\n", 0o600))
	if err != nil {
		t.Fatalf("geçerli anahtar reddedildi: %v", err)
	}
	if got.Recipient().String() != id.Recipient().String() {
		t.Error("yanlış anahtar okundu")
	}
	if _, err := LoadVaultIdentity(writeKey(t, "AGE-SECRET-KEY-BOZUK\n", 0o600)); err == nil {
		t.Error("bozuk anahtar kabul edildi")
	}
	if _, err := LoadVaultIdentity(writeKey(t, id.Recipient().String()+"\n", 0o600)); err == nil {
		t.Error("açık anahtar özel anahtar yerine kabul edildi")
	}
	_, err = LoadVaultIdentity(filepath.Join(t.TempDir(), "yok.key"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("olmayan dosyanın hatası ErrNotExist değil: %v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := LoadVaultIdentity(writeKey(t, id.String()+"\n", 0o640)); err == nil {
			t.Error("gruba açık anahtar kabul edildi")
		}
	}
}

// Anahtar hata mesajında görünmemeli: executor'ın hatası journal'a gidiyor.
func TestLoadVaultIdentityErrorDoesNotLeakKey(t *testing.T) {
	bozuk := "AGE-SECRET-KEY-1" + strings.Repeat("Q", 40) + "!"
	_, err := LoadVaultIdentity(writeKey(t, bozuk, 0o600))
	if err == nil {
		t.Fatal("bozuk anahtar kabul edildi")
	}
	if strings.Contains(err.Error(), strings.Repeat("Q", 10)) {
		t.Fatalf("hata anahtarı içeriyor: %v", err)
	}
}

func TestNewServerRequiresVault(t *testing.T) {
	_, err := NewServer(ServerOptions{Journal: openTestJournal(t)})
	if err == nil {
		t.Fatal("kasa anahtarı olmadan sunucu kuruldu")
	}
}

// Uçtan uca (executor içinde): şifresiz değerli istek reddediliyor ve
// zincire DENIED yazılıyor. Docker'a hiç gidilmiyor; gidilseydi sonuç
// FAILED olurdu (soket yok).
func TestContainerCreateRejectsPlaintextEnv(t *testing.T) {
	srv := newTestServer(t)
	req := validCreateRequest()
	req.Env = map[string]string{"PORT": "3000"}

	if _, err := srv.ContainerCreate(t.Context(), req); err == nil {
		t.Fatal("şifresiz ortam değişkeni kabul edildi")
	}
	recs := records(t, srv)
	if len(recs) != 1 || recs[0].Outcome != audit.OutcomeDenied {
		t.Fatalf("kayıt %+v, tek bir DENIED bekleniyordu", recs)
	}
}

// Kontrol: aynı istek mühürlenince kasa adımını GEÇİYOR (Docker'a ulaşıp
// orada düşüyor: FAILED). Bu olmadan yukarıdaki test, isteği başka bir
// sebeple reddeden bir sunucuda da geçerdi.
func TestContainerCreateSealedEnvReachesDocker(t *testing.T) {
	srv := newTestServer(t)
	req := validCreateRequest()
	req.Env = sealedEnv(t, testVaultIdentity, "blog", map[string]string{"PORT": "3000"})

	if _, err := srv.ContainerCreate(t.Context(), req); err == nil {
		t.Fatal("Docker soketi yokken oluşturma başarılı göründü")
	}
	recs := records(t, srv)
	if len(recs) != 1 || recs[0].Outcome != audit.OutcomeFailure {
		t.Fatalf("kayıt %+v, tek bir FAILED bekleniyordu", recs)
	}
	if strings.Contains(recs[0].ParamsJSON, "3000") || strings.Contains(recs[0].ParamsJSON, sealPrefix) {
		t.Fatalf("değer (açık ya da mühürlü) zincire girdi: %s", recs[0].ParamsJSON)
	}
}
