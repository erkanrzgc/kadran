//go:build linux

package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// ── Uzak betikler GERÇEK araçlarla (K-127) ────────────────────────────
//
// ssh yerine küçük bir ara betik: son argümanı (uzak komutu) `sh -c` ile
// koşturuyor, tıpkı sunucudaki giriş kabuğu gibi (Debian/Ubuntu'da dash).
// Böylece bashArgs/privileged'in tırnakları gerçek dash'ten, betikler
// gerçek dd, flock, setsid, sha256sum, truncate ve tar'dan geçiyor. HOME,
// geçici bir dizin: yükleme dizini orada.
//
// install.sh burada ZARARSIZ bir test betiği; gerçek kurulum betiği
// çalıştırılmıyor. Ölçülemeyen: sshd'nin ve logind'in (KillUserProcesses)
// oturum bitince ne yaptığı — o gerçek sunucuda ölçülür (K-127).

// gercekSunucu, ssh'ı yerel ara betiğe çevirir ve HOME'u döndürür. Bu
// dosya yalnızca Linux'ta derleniyor; araçlar orada GARANTİ, eksikse test
// düşüyor (atlamıyor).
func gercekSunucu(t *testing.T) string {
	t.Helper()
	for _, tool := range []string{"sh", "bash", "dd", "flock", "setsid", "sha256sum", "truncate", "stat", "tar", "mktemp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s yok: %v", tool, err)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	shim := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nfor last; do :; done\nexec sh -c \"$last\"\n"), 0o755); err != nil { //nolint:gosec // çalıştırılabilir olmalı
		t.Fatal(err)
	}
	original, originalBackoff := sshCommand, transferBackoff
	sshCommand, transferBackoff = shim, time.Millisecond
	t.Cleanup(func() { sshCommand, transferBackoff = original, originalBackoff })
	return home
}

// kilitliTampon, iki goroutine'in (exec.Cmd'nin stdout ve stderr
// kopyalayıcıları) aynı anda yazabildiği tampon. Çıplak bytes.Buffer ikisine
// birden verilince ReadFrom'u öbürünün yazdığını SİLİYORDU: bu testin ilk
// koşusu izlemenin hiçbir şey basmadığını sandı.
type kilitliTampon struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (k *kilitliTampon) Write(p []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.buf.Write(p)
}

func (k *kilitliTampon) String() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.buf.String()
}

// testPaketi, verilen install.sh'i ve sıkıştırılamayan bir dolguyu içeren
// tar.gz üretir. Dolgu, ofsetleri dd blok boyutunun (64 KiB) üstüne taşıyor.
func testPaketi(t *testing.T, installSh string) ([]byte, string) {
	t.Helper()
	dolgu := make([]byte, 300_000)
	if _, err := rand.Read(dolgu); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		mode int64
		data []byte
	}{{"install.sh", 0o755, []byte(installSh)}, {"dolgu", 0o644, dolgu}} {
		hdr := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.data)), ModTime: archiveModTime, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func okunmali(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s okunamadı: %v", path, err)
	}
	return string(b)
}

// dokum, hata mesajı için sunucu dizinini ve küçük dosyaları döker.
func dokum(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err.Error()
	}
	for _, e := range entries {
		info, _ := e.Info()
		fmt.Fprintf(&b, "  %s (%d bayt)\n", e.Name(), info.Size())
		if info.Size() < 4096 && !strings.HasSuffix(e.Name(), ".part") {
			c, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			fmt.Fprintf(&b, "    %q\n", c)
		}
	}
	return b.String()
}

func yokOlmali(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s kalmamalıydı (err=%v)", path, err)
	}
}

// Uçtan uca: yükleme, başlatma, izleme. Başlatma, kurulum SÜRERKEN
// dönmeli: arkadaki süreç ssh'ın çıktı borusunu tutsaydı exec.Cmd.Wait
// kurulum bitene kadar beklerdi (oturumdan ayrılmanın yerelde ölçülebilen
// yarısı).
//
// umask KASTEN 077: başlatma betiğindeki `umask 022` olmasa günlük ve
// kilit 0600 olurdu. Bu testin sudo'lu kardeşi bunu her yerde göremiyor:
// Ubuntu 24.04'te pam_umask sudo'nun umask'ını 0022'ye çekiyor, Debian
// 13'te kullanıcınınki (0077) aynen geçiyor (K-127, ikisi de ölçüldü).
func TestRealScriptsInstallDetachedAndFollow(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	gercekKurulum(t, false)
}

// Sudo kipi: başlatma GERÇEK `sudo -n` ile root olarak koşuyor; izleme
// yetkisiz kullanıcıyla root'un yazdığı günlüğü ve kilidi okuyor. Kullanıcının
// umask'ı KASTEN 077. Debian 13'te sudo onu root'a geçiriyor ve başlatma
// betiğindeki `umask 022` olmasa izleyen kullanıcı günlüğü okuyamazdı;
// Ubuntu 24.04'te pam_umask onu zaten 0022'ye çekiyor (ikisi ölçüldü). CI'da
// parolasız sudo var ve değişken orada set; yerelde atlanıyor.
func TestRealScriptsInstallUnderSudo(t *testing.T) {
	if os.Getenv("KADRAN_TEST_REAL_SUDO") == "" {
		t.Skip("yalnızca CI'da (KADRAN_TEST_REAL_SUDO): parolasız sudo ister")
	}
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	gercekKurulum(t, true)
}

func gercekKurulum(t *testing.T, sudo bool) {
	t.Helper()
	home := gercekSunucu(t)
	work := t.TempDir()
	goFile, marker := filepath.Join(work, "GO"), filepath.Join(work, "kuruldu")
	archive, sum := testPaketi(t, `#!/bin/bash
echo "basladi"
for i in $(seq 1 200); do [ -f '`+goFile+`' ] && break; sleep 0.1; done
echo "bitti"
echo "$1" > '`+marker+`'
# Kurulumdan uzun yaşayan bir alt süreç: kilidi miras ALMAMALI.
sleep 5 </dev/null >/dev/null 2>&1 &
`)
	out := &kilitliTampon{}
	opts := Options{Host: "yerel", Sudo: sudo, Stdout: out, Stderr: out}
	ctx := context.Background()

	dir, err := uploadArchive(ctx, opts, archive, sum)
	if err != nil {
		t.Fatalf("yükleme: %v\n%s", err, out.String())
	}
	if want := filepath.Join(home, ".kadran-upload"); dir != want {
		t.Fatalf("yükleme dizini %q, beklenen %q", dir, want)
	}

	began := time.Now()
	code, err := startInstall(ctx, opts, dir, sum, len(archive))
	if err != nil || code != 0 {
		t.Fatalf("başlatma: kod=%d err=%v\n%s", code, err, out.String())
	}
	if d := time.Since(began); d > 10*time.Second {
		t.Fatalf("başlatma kurulumu bekledi (%v): süreç oturumdan ayrılmıyor", d)
	}
	if err := os.WriteFile(goFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := followInstall(ctx, opts, dir, sum); err != nil {
		t.Fatalf("izleme: %v\n%s", err, out.String())
	}

	for _, satir := range []string{"basladi\n", "bitti\n"} {
		if n := strings.Count(out.String(), satir); n != 1 {
			t.Fatalf("%q çıktıda %d kez:\n%s\nsunucu dizini:\n%s", satir, n, out.String(), dokum(t, dir))
		}
	}
	if got := okunmali(t, filepath.Join(dir, sum+".done")); got != "0\n" {
		t.Fatalf("bitiş işareti %q", got)
	}
	yokOlmali(t, filepath.Join(dir, sum+".part"))
	yokOlmali(t, strings.TrimSpace(okunmali(t, marker))) // geçici kurulum dizini
	kilitBosalmali(t, dir)
	// Sudo kipinde günlüğü ve kilidi root yazıyor, yetkisiz kullanıcı
	// izliyor: ikisi de grup ve diğerlerine okunur olmalı (dizin 0700).
	for _, name := range []string{sum + ".log", "install.lock"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o044 != 0o044 {
			t.Fatalf("%s kipi %v: izleyen kullanıcı okuyamaz", name, st.Mode().Perm())
		}
	}
	if sudo {
		var st syscall.Stat_t
		if err := syscall.Stat(filepath.Join(dir, sum+".log"), &st); err != nil {
			t.Fatal(err)
		}
		if st.Uid != 0 {
			t.Fatalf("günlüğün sahibi uid %d: başlatma sudo altında koşmadı", st.Uid)
		}
	}
}

// kilitBosalmali: kurulum bittikten sonra kilit 2 sn içinde boşalmalı.
// Kurulumun arkada bıraktığı süreç kilidi miras alsaydı sonraki her kurulum
// "başka bir kurulum sürüyor" derdi.
func kilitBosalmali(t *testing.T, dir string) {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "install.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("kurulum bitti ama kilit hâlâ tutuluyor: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Kaldığı yerden devam (dd blok boyutunun katı OLMAYAN ofset) ve ölü
// oturumun gecikmiş yazması: ikisinden sonra da özet tutmalı.
func TestRealScriptsResumeAndSurviveAStaleWriter(t *testing.T) {
	gercekSunucu(t)
	archive, sum := testPaketi(t, "#!/bin/bash\necho tamam\n")
	out := &kilitliTampon{}
	opts := Options{Host: "yerel", Stdout: out, Stderr: out}
	ctx := context.Background()

	dir, have, code, err := prepareUpload(ctx, opts, sum)
	if err != nil || code != 0 || have != 0 {
		t.Fatalf("hazırlık: have=%d kod=%d err=%v", have, code, err)
	}
	const yarim = 100_001
	write := func(off, n int) {
		t.Helper()
		code, err := sshRun(ctx, opts, bashArgs(remoteUploadWrite, sum, strconv.Itoa(off)),
			bytes.NewReader(archive[off:off+n]), out)
		if err != nil || code != 0 {
			t.Fatalf("yazma (ofset %d): kod=%d err=%v\n%s", off, code, err, out.String())
		}
	}
	write(0, yarim)

	if _, err := uploadArchive(ctx, opts, archive, sum); err != nil {
		t.Fatalf("devam: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "kaldığı yerden") {
		t.Fatalf("devam edilmedi:\n%s", out.String())
	}
	// Ölü oturum, başı sonradan yeniden yazıyor: dosya kısalmamalı.
	write(0, 70_000)
	part := filepath.Join(dir, sum+".part")
	// Önce boyut: yanlış ofsetle (ör. blok cinsinden) yazılmış seyrek bir
	// dosya gigabaytlarca olabilir, belleğe okunmamalı.
	st, err := os.Stat(part)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != int64(len(archive)) {
		t.Fatalf("paket boyutu %d, beklenen %d", st.Size(), len(archive))
	}
	if got := okunmali(t, part); got != string(archive) {
		t.Fatalf("paket bozuldu: %d bayt, beklenen %d", len(got), len(archive))
	}

	if err := runInstaller(ctx, opts, archive); err != nil {
		t.Fatalf("kurulum: %v\n%s", err, out.String())
	}
	if strings.Count(out.String(), "tamam\n") != 1 {
		t.Fatalf("kurulum çıktısı:\n%s", out.String())
	}
}

// Sunucudaki paket bozuksa başlatma kurulumu BAŞLATMIYOR (gerçek
// sha256sum); istemci bir kez baştan yüklüyor.
func TestRealScriptsRejectACorruptPart(t *testing.T) {
	home := gercekSunucu(t)
	archive, sum := testPaketi(t, "#!/bin/bash\necho tamam\n")
	dir := filepath.Join(home, ".kadran-upload")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bozuk := bytes.Clone(archive)
	bozuk[len(bozuk)/2] ^= 0xff
	if err := os.WriteFile(filepath.Join(dir, sum+".part"), bozuk, 0o600); err != nil {
		t.Fatal(err)
	}
	out := &kilitliTampon{}
	opts := Options{Host: "yerel", Stdout: out, Stderr: out}

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("bozuk paketten kurtarılamadı: %v\n%s", err, out.String())
	}
	for _, parca := range []string{"özeti tutmuyor", "baştan yükleniyor", "tamam\n"} {
		if !strings.Contains(out.String(), parca) {
			t.Fatalf("%q yok:\n%s", parca, out.String())
		}
	}
}

// Kilit tutuluyorken: aynı paketin günlüğü varsa "zaten sürüyor", yoksa
// "başka kurulum". İkisinde de pakete dokunulmuyor.
func TestRealScriptsLockDistinguishesOursFromAnother(t *testing.T) {
	home := gercekSunucu(t)
	dir := filepath.Join(home, ".kadran-upload")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, "install.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	const sum = "abc123"
	part := filepath.Join(dir, sum+".part")
	if err := os.WriteFile(part, []byte("dokunulmamalı"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := Options{Host: "yerel", Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}

	if code, err := startInstall(context.Background(), opts, dir, sum, 5); err != nil || code != installBusy {
		t.Fatalf("başka kurulum: kod=%d err=%v", code, err)
	}
	if err := os.WriteFile(filepath.Join(dir, sum+".log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, err := startInstall(context.Background(), opts, dir, sum, 5); err != nil || code != installRunning {
		t.Fatalf("süren kurulum: kod=%d err=%v", code, err)
	}
	if got := okunmali(t, part); got != "dokunulmamalı" {
		t.Fatalf("kilit tutulurken paket değişti: %q", got)
	}
}

// Kurulum süreci öldürülürse (bellek yetmemesinin benzeri) izleme kilidin
// boşaldığını görüp işaret beklemeden dönüyor.
func TestRealScriptsDetectADeadInstaller(t *testing.T) {
	gercekSunucu(t)
	leftover := filepath.Join(t.TempDir(), "gecici")
	archive, _ := testPaketi(t, `#!/bin/bash
echo "$1" > '`+leftover+`'
echo yarim
kill -9 "$PPID"
`)
	out := &kilitliTampon{}
	opts := Options{Host: "yerel", Stdout: out, Stderr: out}
	t.Cleanup(func() {
		if b, err := os.ReadFile(leftover); err == nil {
			_ = os.RemoveAll(strings.TrimSpace(string(b))) // trap SIGKILL'de koşmuyor
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := runInstaller(ctx, opts, archive)

	if err == nil || !strings.Contains(err.Error(), "bitiş işareti") {
		t.Fatalf("ölen kurulum bildirilmedi: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "yarim\n") {
		t.Fatalf("ölmeden önceki günlük basılmadı:\n%s", out.String())
	}
}
