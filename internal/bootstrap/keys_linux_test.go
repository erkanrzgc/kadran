package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// Betik dosyanın sahibine geçiyor (K-140). Burada sahibi testi koşturan
	// kullanıcı: root'suz koşuda geçiş hiç olmaz, root'ta kendine geçer.
	// Sayısal: konteynerde uid'in adı olmayabilir.
	oldSSH, oldPath, oldUser := sshCommand, clientAuthorizedKeys, keysOwner
	sshCommand, clientAuthorizedKeys = fake, filepath.Join(dir, "authorized_keys")
	keysOwner = strconv.Itoa(os.Getuid())
	t.Cleanup(func() { sshCommand, clientAuthorizedKeys, keysOwner = oldSSH, oldPath, oldUser })

	adminLine, _, adminFP := testKey(t, 10, "erkan@dizustu")
	admin := `command="/usr/local/lib/kadran/kadran-connect",restrict ` + adminLine + "\n"
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
		privileged(opts.options(), remoteKeys, keysOwner, "remove", clientAuthorizedKeys, missing), nil, nil)
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
		`command="/usr/local/lib/kadran/kadran-connect -deploy=site",restrict ` + ciBody + "\n" + evilBody,
		`command="/usr/local/lib/kadran/kadran-connect",restrict ` + ciBody, // yönetici: key add ekleyemez
		`command="/bin/sh",restrict ` + ciBody,
		ciBody,
		// Geçerli bir satır, ama yinelenme denetimine giden gövde başka
		// bir anahtarın: denetim yanlış anahtara bakardı.
		`command="/usr/local/lib/kadran/kadran-connect -deploy=site",restrict ` + evilBody,
	} {
		code, err := sshRun(context.Background(), opts.options(),
			privileged(opts.options(), remoteKeys, keysOwner, "add", clientAuthorizedKeys, line, ciBody), nil, nil)
		if err != nil || code == 0 {
			t.Errorf("uzak betik %q satırını kabul etti (kod %d, %v)", line, code, err)
		}
	}
	if readKeys(t) != before {
		t.Fatalf("reddedilen satırlar dosyayı değiştirdi:\n%s", readKeys(t))
	}
}

// TestKeyOpsRunAsClientUnderRealSudo (K-140): uzak betik root olarak
// başlıyor ama authorized_keys'e kadran-client olarak dokunuyor. Dosya o
// kullanıcının dizininde; o kullanıcı dosyanın yerine bir bağ koyabilir.
// Root olarak `[ -f ]`, `cat`, `chown --reference` bağı izliyordu: bağın
// hedefi root'un bir dosyasıysa root onu okuyup listeye ve yeni dosyaya
// taşıyordu.
//
// Gerçek sudo, gerçek bir kullanıcı ve setpriv ister: yalnız CI'da
// (KADRAN_TEST_REAL_SUDO). Sahte ssh `sudo -n --` önekini DÜŞÜRMÜYOR.
func TestKeyOpsRunAsClientUnderRealSudo(t *testing.T) {
	if os.Getenv("KADRAN_TEST_REAL_SUDO") == "" {
		t.Skip("yalnızca CI'da (KADRAN_TEST_REAL_SUDO): parolasız sudo ister")
	}
	sudo := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("sudo", append([]string{"-n", "--"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("sudo %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	const user = "kadran-k140-test"
	_ = exec.Command("sudo", "-n", "--", "userdel", user).Run()
	_ = exec.Command("sudo", "-n", "--", "groupdel", user).Run()
	sudo("useradd", "--system", "--user-group", "--no-create-home", "--shell", "/usr/sbin/nologin", user)
	t.Cleanup(func() {
		_ = exec.Command("sudo", "-n", "--", "userdel", user).Run()
		_ = exec.Command("sudo", "-n", "--", "groupdel", user).Run()
	})
	uid, err := strconv.Atoi(strings.TrimSpace(sudo("id", "-u", user)))
	if err != nil {
		t.Fatal(err)
	}

	// Kök dizin 0755: o kullanıcı yalnız kendi ev dizinine yazabilir.
	dir, err := os.MkdirTemp("", "kadran-k140-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("sudo", "-n", "--", "rm", "-rf", dir).Run() })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ev := filepath.Join(dir, "ev")
	sudo("install", "-d", "-o", user, "-g", user, "-m", "0755", ev)
	sudo("install", "-d", "-o", user, "-g", user, "-m", "0700", filepath.Join(ev, ".ssh"))
	ak := filepath.Join(ev, ".ssh", "authorized_keys")

	adminLine, _, adminFP := testKey(t, 20, "erkan@dizustu")
	kaynak := filepath.Join(dir, "kaynak")
	admin := `command="/usr/local/lib/kadran/kadran-connect",restrict ` + adminLine + "\n"
	if err := os.WriteFile(kaynak, []byte(admin), 0o644); err != nil {
		t.Fatal(err)
	}
	sudo("install", "-o", user, "-g", user, "-m", "0600", kaynak, ak)

	gizli := filepath.Join(dir, "gizli")
	sudo("sh", "-c", "printf 'GIZLI-K140\n' > '"+gizli+"' && chmod 0600 '"+gizli+"'")

	fake := filepath.Join(dir, "ssh")
	if err := os.WriteFile(fake, []byte("#!/usr/bin/env bash\nexec bash -c \"${@: -1}\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldSSH, oldPath, oldUser := sshCommand, clientAuthorizedKeys, keysOwner
	sshCommand, clientAuthorizedKeys, keysOwner = fake, ak, user
	t.Cleanup(func() { sshCommand, clientAuthorizedKeys, keysOwner = oldSSH, oldPath, oldUser })
	opts := KeyOptions{Host: "root@sunucu", Sudo: true}
	ctx := context.Background()

	sahibi := func() (int, os.FileMode) {
		t.Helper()
		out := strings.Fields(sudo("stat", "-c", "%u %a", ak))
		u, _ := strconv.Atoi(out[0])
		m, _ := strconv.ParseUint(out[1], 8, 32)
		return u, os.FileMode(m)
	}

	// Yaşam döngüsü: dosya o kullanıcının ve 0600 kalıyor.
	ciLine, ciBody, ciFP := testKey(t, 21, "ci")
	if _, err := AddDeployKey(ctx, opts, []byte(ciLine), []string{"site"}, ""); err != nil {
		t.Fatalf("ekleme: %v", err)
	}
	if u, m := sahibi(); u != uid || m != 0o600 {
		t.Fatalf("ekleme sonrası sahip %d izin %o; beklenen %d 600", u, m, uid)
	}
	keys, err := ListKeys(ctx, opts)
	if err != nil || len(keys) != 2 || keys[0].Fingerprint != adminFP || keys[1].Fingerprint != ciFP {
		t.Fatalf("liste: %+v, %v", keys, err)
	}
	if _, err := RemoveKey(ctx, opts, ciFP); err != nil {
		t.Fatalf("silme: %v", err)
	}
	if strings.Contains(sudo("cat", ak), ciBody) {
		t.Fatal("satır silinmedi")
	}

	// Bağ: hedef root'un 0600 dosyası. Kullanıcı olarak okunamaz; ne liste
	// ne ekleme onu açmamalı, bağ yerinde kalmalı.
	sudo("ln", "-sfn", gizli, ak)
	if keys, err := ListKeys(ctx, opts); err == nil {
		t.Fatalf("bağlı authorized_keys listelendi (root olarak okundu): %+v", keys)
	} else if strings.Contains(err.Error(), "GIZLI") || !strings.Contains(err.Error(), "okunamıyor") {
		t.Fatalf("hata iletisi sırrı taşıyor ya da sebebi söylemiyor: %v", err)
	}
	evLine, _, _ := testKey(t, 22, "ev")
	if _, err := AddDeployKey(ctx, opts, []byte(evLine), []string{"site"}, ""); err == nil {
		t.Fatal("bağlı authorized_keys'e ekleme yapıldı")
	}
	if got := sudo("stat", "-c", "%F", ak); !strings.Contains(got, "symbolic link") {
		t.Fatalf("bağ yerinde değil: %q", got)
	}
	if got := sudo("cat", gizli); got != "GIZLI-K140\n" {
		t.Fatalf("root'un dosyası değişti: %q", got)
	}
	// grep -r yinelemede bağları izlemiyor: yalnız gerçek dosyalara bakar.
	// 1 "eşleşme yok" demek; başka her şey ölçümün kendisini bozar.
	out, err := exec.Command("sudo", "-n", "--", "grep", "-rl", "GIZLI-K140", ev).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("sır kullanıcının dizinine düştü ya da arama bozuk (%v): %s", err, out)
	}

	// Hiç kurulmamış sunucu: kullanıcı yok. Geçiş setpriv'in kendi hatasıyla
	// değil, kurulumu öneren iletiyle durmalı.
	keysOwner = "kadran-yok-boyle-k140"
	if _, err := ListKeys(ctx, opts); err == nil || !strings.Contains(err.Error(), "önce kadran bootstrap") {
		t.Fatalf("olmayan kullanıcı: %v", err)
	}
}
