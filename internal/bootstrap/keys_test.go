package bootstrap

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/connproto"
)

// testKey, gerçek biçimde bir ed25519 açık anahtar satırı ve parmak izini
// üretir. Rastgele değil: tohum sabit, testler tekrarlanabilir.
func testKey(t *testing.T, seed byte, comment string) (line, body, fp string) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(append(make([]byte, 31), seed))
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatal("ed25519 açık anahtarı üretilemedi")
	}

	const typ = "ssh-ed25519"
	blob := binary.BigEndian.AppendUint32(nil, uint32(len(typ)))
	blob = append(blob, typ...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(len(pub)))
	blob = append(blob, pub...)

	sum := sha256.Sum256(blob)
	body = typ + " " + base64.StdEncoding.EncodeToString(blob)
	line = body
	if comment != "" {
		line += " " + comment
	}
	return line, body, "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func TestParsePublicKey(t *testing.T) {
	line, body, fp := testKey(t, 1, "ci@github")
	k, err := parsePublicKey([]byte(line + "\n"))
	if err != nil {
		t.Fatalf("geçerli anahtar reddedildi: %v", err)
	}
	if k.body() != body || k.Fingerprint != fp || k.Comment != "ci@github" {
		t.Fatalf("ayrıştırma: %+v (gövde %q, parmak izi %q)", k, body, fp)
	}
}

// TestParsePublicKeyRejectsForgedBodies: gövde gerçekten o türde bir anahtar
// olmalı. authorized_keys'e yazılacak her bayt doğrulanıyor.
func TestParsePublicKeyRejectsForgedBodies(t *testing.T) {
	_, body, _ := testKey(t, 1, "")
	b64 := strings.Fields(body)[1]
	for _, in := range []string{
		"ssh-ed25519 bu-base64-degil",
		"ssh-ed25519 AAAA",                         // tür alanı yok
		"ssh-rsa " + b64,                           // tür gövdeyle uyuşmuyor
		"ssh-ed25519 " + b64[:len(b64)-8],          // kesik gövde
		body + "\n" + body,                         // iki satır
		"-----BEGIN OPENSSH PRIVATE KEY-----\nx\n", // özel anahtar
	} {
		if k, err := parsePublicKey([]byte(in)); err == nil {
			t.Errorf("%q kabul edildi: %+v", in, k)
		}
	}
}

func TestDeployLine(t *testing.T) {
	line, body, _ := testKey(t, 2, "ci@github")
	k, err := parsePublicKey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}

	got, err := deployLine(k, []string{"site", "api"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := `command="/usr/local/lib/kadran/kadran-connect -deploy=site,api",restrict ` + body + " ci@github"
	if got != want {
		t.Fatalf("satır:\n%s\nbeklenen:\n%s", got, want)
	}

	// -name anahtarın yorumunu geçersiz kılar.
	if got, _ := deployLine(k, []string{"site"}, "deploy-bot"); !strings.HasSuffix(got, " deploy-bot") {
		t.Errorf("ad uygulanmadı: %s", got)
	}

	// Satırın kendisi kadrand'nin göreceği rolü taşımalı: kadran-connect'in
	// argümanı connproto'nun ayrıştırıcısından geçmeli.
	ak := ParseAuthorizedKeys(got)
	if len(ak) != 1 || ak[0].Role != connproto.RoleDeploy || !reflect.DeepEqual(ak[0].Apps, []string{"site", "api"}) {
		t.Fatalf("üretilen satır geri ayrıştırılamadı: %+v", ak)
	}
}

func TestDeployLineRejects(t *testing.T) {
	line, _, _ := testKey(t, 2, "boşluklu yorum")
	k, err := parsePublicKey([]byte(line))
	if err != nil {
		t.Fatal(err)
	}

	for _, apps := range [][]string{nil, {""}, {"Site"}, {"a,b"}, {"$(id)"}} {
		if got, err := deployLine(k, apps, ""); err == nil {
			t.Errorf("kapsam %q kabul edildi: %s", apps, got)
		}
	}
	for _, name := range []string{"iki kelime", "a\nb", `"tırnak"`, strings.Repeat("a", 65)} {
		if got, err := deployLine(k, []string{"site"}, name); err == nil {
			t.Errorf("ad %q kabul edildi: %s", name, got)
		}
	}

	// Anahtarın kendi yorumu satıra giremeyecek karakterler taşıyorsa
	// SESSİZCE düşer; satır yine üretilir.
	got, err := deployLine(k, []string{"site"}, "")
	if err != nil || strings.Contains(got, "boşluklu") {
		t.Fatalf("geçersiz yorum satıra girdi ya da satır üretilmedi: %q, %v", got, err)
	}
}

func TestParseAuthorizedKeys(t *testing.T) {
	adminLine, _, adminFP := testKey(t, 3, "erkan@dizustu")
	ciLine, _, ciFP := testKey(t, 4, "ci")
	rawLine, _, rawFP := testKey(t, 5, "elle")

	content := strings.Join([]string{
		"# yorum",
		"",
		`command="/usr/local/lib/kadran/kadran-connect",restrict ` + adminLine,
		`command="/usr/local/lib/kadran/kadran-connect -deploy=site,api",restrict ` + ciLine,
		rawLine, // zorlanmış komut YOK
		`command="/bin/sh",restrict ` + rawLine,
		`command="/usr/local/lib/kadran/kadran-connect" ` + rawLine, // restrict yok
		`command="/usr/local/lib/kadran/kadran-connect -deploy=",restrict ` + rawLine,
	}, "\n")

	got := ParseAuthorizedKeys(content)
	if len(got) != 6 {
		t.Fatalf("%d satır ayrıştırıldı, 6 bekleniyordu: %+v", len(got), got)
	}

	want := []struct {
		role       string
		apps       []string
		fp         string
		restricted bool
	}{
		{connproto.RoleAdmin, nil, adminFP, true},
		{connproto.RoleDeploy, []string{"site", "api"}, ciFP, true},
		{"", nil, rawFP, false},
		{"", nil, rawFP, false},
		{"", nil, rawFP, false},
		{"", nil, rawFP, false},
	}
	for i, w := range want {
		g := got[i]
		if g.Role != w.role || !reflect.DeepEqual(g.Apps, w.apps) || g.Fingerprint != w.fp || g.Restricted() != w.restricted {
			t.Errorf("satır %d: %+v (kısıtlı=%v); beklenen rol %q kapsam %q parmak izi %q kısıtlı=%v",
				i, g, g.Restricted(), w.role, w.apps, w.fp, w.restricted)
		}
	}
	if got[1].Comment != "ci" || got[0].Comment != "erkan@dizustu" {
		t.Errorf("yorumlar: %q, %q", got[0].Comment, got[1].Comment)
	}
}

// TestClientPathsMatchInstallScript: anahtar işlemleri install.sh'in
// kullandığı yolları kullanmalı. Biri değişip öbürü değişmezse `key add`
// hiçbir şeye etki etmeyen bir dosyaya yazardı.
func TestClientPathsMatchInstallScript(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	if !strings.Contains(text, "\nLIB_DIR="+libDir+"\n") {
		t.Errorf("install.sh LIB_DIR'ı %q değil", libDir)
	}
	if !strings.Contains(text, "\nCLIENT_HOME="+strings.TrimSuffix(clientAuthorizedKeys, "/.ssh/authorized_keys")+"\n") {
		t.Errorf("install.sh CLIENT_HOME'u %q ile uyuşmuyor", clientAuthorizedKeys)
	}
	if !strings.Contains(text, `auth_file="$CLIENT_HOME/.ssh/authorized_keys"`) {
		t.Error("install.sh authorized_keys yolunu değiştirmiş")
	}
}
