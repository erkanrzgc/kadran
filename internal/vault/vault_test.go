package vault_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/erkanrzgc/kadran/internal/vault"
	"github.com/erkanrzgc/kadran/internal/vault/vaulttest"
)

// Mühürlenen düz metin yalnız değer değil: uygulama ve ad da içinde. Bağ
// olmasaydı ele geçirilmiş bir daemon A'nın şifreli değerini başka bir ada
// ya da uygulamaya kopyalayıp executor'a açtırabilirdi (K-123).
func TestSealBindsAppAndKey(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	s := vaulttest.Sealer(t, id)

	sealed, err := s.Seal("blog", "DB_PASSWORD", "gizli-deger")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, vault.Prefix) {
		t.Fatalf("önek yok: %q", sealed)
	}
	if strings.Contains(sealed, "gizli-deger") {
		t.Fatal("düz değer mühürlü metinde görünüyor")
	}
	app, key, val := vaulttest.Open(t, id, sealed)
	if app != "blog" || key != "DB_PASSWORD" || val != "gizli-deger" {
		t.Fatalf("açılan (%q, %q, %q)", app, key, val)
	}
}

// Aynı değer iki kez mühürlenince iki farklı metin çıkmalı; yoksa
// veritabanını okuyan biri hangi uygulamaların aynı parolayı paylaştığını
// görürdü.
func TestSealIsRandomized(t *testing.T) {
	s := vaulttest.Sealer(t, vaulttest.NewIdentity(t))
	a, err := s.Seal("blog", "K", "aynı")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Seal("blog", "K", "aynı")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("aynı değer aynı metne mühürlendi")
	}
}

// Boş değer de mühürlenir: executor öneksiz değeri reddediyor, boş bir
// değer düz kalsaydı dağıtım düşerdi.
func TestSealEnvSealsEveryValueIncludingEmpty(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	s := vaulttest.Sealer(t, id)

	in := map[string]string{"A": "bir", "BOS": ""}
	out, err := s.SealEnv("blog", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("%d değer döndü, %d bekleniyordu", len(out), len(in))
	}
	for k, want := range in {
		app, key, val := vaulttest.Open(t, id, out[k])
		if app != "blog" || key != k || val != want {
			t.Errorf("%s: açılan (%q, %q, %q)", k, app, key, val)
		}
	}
	if in["A"] != "bir" {
		t.Error("çağıranın haritası değişti")
	}
}

func TestNewSealerAcceptsOnlyX25519(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	if _, err := vault.NewSealer(id.Recipient().String()); err != nil {
		t.Fatalf("geçerli alıcı reddedildi: %v", err)
	}
	for ad, r := range map[string]string{
		"boş":          "",
		"özel anahtar": id.String(),
		"ssh anahtarı": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIYonetici x",
		"bozuk":        "age1xyz",
	} {
		if _, err := vault.NewSealer(r); err == nil {
			t.Errorf("%s alıcı olarak kabul edildi", ad)
		}
	}
}

func TestLoadSealerReadsRecipientFile(t *testing.T) {
	id := vaulttest.NewIdentity(t)
	p := filepath.Join(t.TempDir(), "vault.pub")
	if err := os.WriteFile(p, []byte(id.Recipient().String()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := vault.LoadSealer(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Recipient() != id.Recipient().String() {
		t.Errorf("alıcı %q", s.Recipient())
	}

	if _, err := vault.LoadSealer(filepath.Join(t.TempDir(), "yok.pub")); err == nil {
		t.Error("olmayan dosya kabul edildi")
	}
	buyuk := filepath.Join(t.TempDir(), "buyuk.pub")
	if err := os.WriteFile(buyuk, []byte(strings.Repeat("a", 64<<10)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.LoadSealer(buyuk); err == nil {
		t.Error("aşırı büyük dosya kabul edildi")
	}
}

// Kontrol: test yardımcısının açtığı şey gerçekten age; yardımcı bozuksa
// yukarıdaki testler hiçbir şey kanıtlamazdı.
func TestOpenHelperRejectsWrongIdentity(t *testing.T) {
	s := vaulttest.Sealer(t, vaulttest.NewIdentity(t))
	sealed, err := s.Seal("blog", "K", "v")
	if err != nil {
		t.Fatal(err)
	}
	baska, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := vaulttest.TryOpen(baska, sealed); err == nil {
		t.Fatal("başka anahtar mühürlü değeri açtı")
	}
}

// PlainLen, açmadan doğru boyutu vermeli; 64 KiB parça sınırının iki
// yanında da. Yanlış olsaydı güncelleme meşru tanımları reddeder ya da
// sınırı aşanları geçirirdi.
func TestPlainLenMatchesSealedValue(t *testing.T) {
	s := vaulttest.Sealer(t, vaulttest.NewIdentity(t))
	for _, n := range []int{0, 1, 100, 32 << 10, 64<<10 - 20, 64 << 10, 64<<10 + 1, 130 << 10} {
		sealed, err := s.Seal("blog", "DB_URL", strings.Repeat("x", n))
		if err != nil {
			t.Fatal(err)
		}
		got, err := vault.PlainLen("blog", "DB_URL", sealed)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		if got != n {
			t.Errorf("n=%d: PlainLen %d", n, got)
		}
	}
	for ad, v := range map[string]string{
		"öneksiz":      "düz",
		"bozuk base64": vault.Prefix + "!!!",
		"çok kısa":     vault.Prefix + "YWdl",
	} {
		if _, err := vault.PlainLen("blog", "K", v); err == nil {
			t.Errorf("%s kabul edildi", ad)
		}
	}
}
