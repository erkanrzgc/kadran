package bootstrap

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/connproto"
)

// ── Anahtar işlemleri GERÇEK bash ile ────────────────────────────────
//
// Sahte ssh uzak komutu GERÇEK bash'e veriyor: tırnaklama, betiğin kendisi,
// grep/mktemp/chown/mv gerçek. Sudo kipinde `sudo -n -- ` önekini düşürüyor
// (root olmadan koşuyoruz). KANITLAMADIĞI: sunucudaki sshd ve sudo; onlar
// gerçek sunucuda ölçülecek (K-131).

// keyServer, sahte bir sunucu kurar: authorized_keys içinde yalnızca
// yönetici satırı var.
func keyServer(t *testing.T, sudo bool) (KeyOptions, string, string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Fatalf("bash yok: %v", err)
	}
	dir := t.TempDir()

	fake := filepath.Join(dir, "ssh")
	script := "#!/usr/bin/env bash\ncmd=\"${@: -1}\"\n"
	if sudo {
		// Sudo kipinde önek ŞART: yoksa betik root yerine bağlanan
		// kullanıcıyla koşar ve authorized_keys'e erişemezdi.
		script += "case \"$cmd\" in 'sudo -n -- '*) cmd=\"${cmd#sudo -n -- }\" ;; *) echo 'sudo öneki yok' >&2; exit 97 ;; esac\n"
	}
	script += "exec bash -c \"$cmd\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSSH, oldPath := sshCommand, clientAuthorizedKeys
	sshCommand, clientAuthorizedKeys = fake, filepath.Join(dir, "authorized_keys")
	t.Cleanup(func() { sshCommand, clientAuthorizedKeys = oldSSH, oldPath })

	adminLine, _, adminFP := testKey(t, 10, "erkan@dizustu")
	admin := `command="/usr/local/lib/panely/panely-connect",restrict ` + adminLine + "\n"
	if err := os.WriteFile(clientAuthorizedKeys, []byte(admin), 0o600); err != nil {
		t.Fatal(err)
	}
	return KeyOptions{Host: "root@sunucu", Sudo: sudo}, adminLine, adminFP
}

func readKeys(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(clientAuthorizedKeys)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKeyLifecycleWithRealBash(t *testing.T) {
	for _, sudo := range []bool{false, true} {
		opts, _, adminFP := keyServer(t, sudo)
		ctx := context.Background()
		ciLine, ciBody, ciFP := testKey(t, 11, "ci")

		added, err := AddDeployKey(ctx, opts, []byte(ciLine+"\n"), []string{"site", "api"}, "")
		if err != nil {
			t.Fatalf("sudo=%v: ekleme: %v", sudo, err)
		}
		if added.Fingerprint != ciFP || added.Role != connproto.RoleDeploy {
			t.Fatalf("eklenen: %+v", added)
		}

		keys, err := ListKeys(ctx, opts)
		if err != nil {
			t.Fatalf("listeleme: %v", err)
		}
		if len(keys) != 2 || keys[0].Fingerprint != adminFP || keys[1].Fingerprint != ciFP {
			t.Fatalf("liste: %+v", keys)
		}
		if !strings.Contains(readKeys(t), `-deploy=site,api",restrict `+ciBody+" ci\n") {
			t.Fatalf("satır yazılmadı:\n%s", readKeys(t))
		}
		if fi, _ := os.Stat(clientAuthorizedKeys); fi.Mode().Perm() != 0o600 {
			t.Errorf("izin %v, 0600 bekleniyordu", fi.Mode().Perm())
		}

		removed, err := RemoveKey(ctx, opts, ciFP)
		if err != nil || removed.Fingerprint != ciFP {
			t.Fatalf("silme: %+v, %v", removed, err)
		}
		if strings.Contains(readKeys(t), ciBody) {
			t.Fatalf("satır silinmedi:\n%s", readKeys(t))
		}
	}
}

// TestKeyAddRefusesAKeyAlreadyPresent: sshd İLK eşleşen satırı kullanır.
// Aynı anahtar iki satırda olursa hangi rolle bağlanacağı satır sırasına
// kalır; yönetici anahtarını dağıtım satırıyla gölgelemek sessiz bir yetki
// değişikliği olurdu.
func TestKeyAddRefusesAKeyAlreadyPresent(t *testing.T) {
	opts, adminLine, _ := keyServer(t, false)
	ctx := context.Background()
	before := readKeys(t)

	if _, err := AddDeployKey(ctx, opts, []byte(adminLine), []string{"site"}, ""); err == nil ||
		!strings.Contains(err.Error(), "zaten") {
		t.Fatalf("yönetici anahtarı dağıtım anahtarı olarak eklendi: %v", err)
	}
	if readKeys(t) != before {
		t.Fatalf("reddedilen ekleme dosyayı değiştirdi:\n%s", readKeys(t))
	}

	ciLine, _, _ := testKey(t, 12, "ci")
	if _, err := AddDeployKey(ctx, opts, []byte(ciLine), []string{"site"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := AddDeployKey(ctx, opts, []byte(ciLine), []string{"api"}, ""); err == nil {
		t.Fatal("aynı anahtar ikinci kez eklendi")
	}
}

// TestKeyRemoveRefusesTheLastAdmin: son yönetici satırı giderse sunucuya
// yalnızca root yoluyla (bootstrap) yeniden erişilir.
func TestKeyRemoveRefusesTheLastAdmin(t *testing.T) {
	opts, _, adminFP := keyServer(t, false)
	before := readKeys(t)

	_, err := RemoveKey(context.Background(), opts, adminFP)
	if err == nil || !strings.Contains(err.Error(), "son yönetici") {
		t.Fatalf("son yönetici anahtarı silindi ya da hata yanlış: %v", err)
	}
	if readKeys(t) != before {
		t.Fatalf("reddedilen silme dosyayı değiştirdi:\n%s", readKeys(t))
	}

	if _, err := RemoveKey(context.Background(), opts, "SHA256:yok"); err == nil ||
		!strings.Contains(err.Error(), "bulunamadı") {
		t.Fatalf("olmayan anahtar: %v", err)
	}
}

// TestRemoteRemoveOfAMissingKey: istemci önce listede arıyor, ama sunucu
// da kendi başına "bulunamadı" demeli; yarışta (arada silinmiş) sessizce
// başarılı dönmek yanlış bir "kaldırıldı" yazdırırdı.
func TestRemoteRemoveOfAMissingKey(t *testing.T) {
	opts, _, _ := keyServer(t, false)
	before := readKeys(t)
	_, missing, _ := testKey(t, 15, "")

	code, err := sshRun(context.Background(), opts.options(),
		privileged(opts.options(), remoteKeys, "remove", clientAuthorizedKeys, missing), nil, nil)
	if err != nil || code != keysNotFound {
		t.Fatalf("olmayan anahtarın silinmesi: kod %d (%v), beklenen %d", code, err, keysNotFound)
	}
	if readKeys(t) != before {
		t.Fatalf("dosya değişti:\n%s", readKeys(t))
	}
}

// TestRemoteScriptRevalidatesTheLine: istemci satırı doğruluyor ama uzak
// betik ona GÜVENMİYOR. Sunucuya doğrudan, satır sonu taşıyan bir satır
// gönderiliyor; ikinci satır kısıtsız bir anahtar olurdu.
func TestRemoteScriptRevalidatesTheLine(t *testing.T) {
	opts, _, _ := keyServer(t, false)
	before := readKeys(t)
	_, ciBody, _ := testKey(t, 13, "")
	_, evilBody, _ := testKey(t, 14, "")

	for _, line := range []string{
		`command="/usr/local/lib/panely/panely-connect -deploy=site",restrict ` + ciBody + "\n" + evilBody,
		`command="/usr/local/lib/panely/panely-connect",restrict ` + ciBody, // yönetici: key add ekleyemez
		`command="/bin/sh",restrict ` + ciBody,
		ciBody,
		// Geçerli bir satır, ama yinelenme denetimine giden gövde başka
		// bir anahtarın: denetim yanlış anahtara bakardı.
		`command="/usr/local/lib/panely/panely-connect -deploy=site",restrict ` + evilBody,
	} {
		code, err := sshRun(context.Background(), opts.options(),
			privileged(opts.options(), remoteKeys, "add", clientAuthorizedKeys, line, ciBody), nil, nil)
		if err != nil || code == 0 {
			t.Errorf("uzak betik %q satırını kabul etti (kod %d, %v)", line, code, err)
		}
	}
	if readKeys(t) != before {
		t.Fatalf("reddedilen satırlar dosyayı değiştirdi:\n%s", readKeys(t))
	}
}
