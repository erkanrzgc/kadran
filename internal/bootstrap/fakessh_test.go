package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ── Sahte ssh: sunucuyu taklit eder ──────────────────────────────────
//
// Test binary'si kendini ssh yerine çalıştırır. Uzak komutu POSIX
// kelimelerine ayırıp hangi sabit betiğin çağrıldığını tanır ve sunucunun
// yükleme dizinini gerçek bir geçici dizinle taklit eder: yarım dosya,
// ofsetle yazma, kopma, özet denetimi, kurulum günlüğü ve bitiş işareti.
//
// KANITLADIĞI: çağrı sırası, argümanlar, kaldığı yerden devam ve hata
// sınıfları. KANITLAMADIĞI: gerçek bash/dd/flock/setsid ve uzaktaki kabuğun
// tırnak çözümü. Onlar Linux'ta gerçek araçlarla (upload_linux_test.go) ve
// gerçek sunucuda (K-127) ölçülüyor.

const (
	fakeSSHEnv        = "KADRAN_TEST_FAKE_SSH"
	fakeSSHLogEnv     = "KADRAN_TEST_FAKE_SSH_LOG"
	fakeSSHUIDEnv     = "KADRAN_TEST_FAKE_SSH_UID"
	fakeSudoFailEnv   = "KADRAN_TEST_FAKE_SUDO_FAIL"
	fakeRemoteEnv     = "KADRAN_TEST_FAKE_REMOTE"
	fakeCutOnceEnv    = "KADRAN_TEST_FAKE_CUT_ONCE"
	fakeCutAlwaysEnv  = "KADRAN_TEST_FAKE_CUT_ALWAYS"
	fakeCutFirstEnv   = "KADRAN_TEST_FAKE_CUT_FIRST"
	fakeCorruptEnv    = "KADRAN_TEST_FAKE_CORRUPT"
	fakeShortEnv      = "KADRAN_TEST_FAKE_SHORT"
	fakeStartRCEnv    = "KADRAN_TEST_FAKE_START_RC"
	fakeDiedEnv       = "KADRAN_TEST_FAKE_DIED"
	fakeFollowCutEnv  = "KADRAN_TEST_FAKE_FOLLOW_CUT"
	fakeInstallRCEnv  = "KADRAN_TEST_FAKE_INSTALL_RC"
	fakeInstallLogEnv = "KADRAN_TEST_FAKE_INSTALL_LOG"
	fakeDirEnv        = "KADRAN_TEST_FAKE_DIR"
	fakeReportedDir   = "/home/sahte/.kadran-upload"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeSSHEnv) != "" {
		fakeSSHMain()
		return
	}
	os.Exit(m.Run())
}

// shellWords, POSIX kabuğunun kelime ayırmasını uygular: tek tırnak,
// ters bölü ve boşluk.
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				j = len(s) - i - 1
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

func fakeExists(p string) bool { _, err := os.Stat(p); return err == nil }

func fakeLog(line string) {
	if f, err := os.OpenFile(os.Getenv(fakeSSHLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(line + "\n")
		_ = f.Close()
	}
}

// fakeDir, sahte sunucunun bildirdiği yükleme dizini.
func fakeDir() string {
	if d := os.Getenv(fakeDirEnv); d != "" {
		return d
	}
	return fakeReportedDir
}

func fakeEnvInt(name string) int {
	n, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return -1
	}
	return n
}

// fakeSSHMain, uzak komuta (argv'nin sonu) göre sunucuyu taklit eder.
func fakeSSHMain() {
	remote := os.Args[len(os.Args)-1]
	remoteDir := os.Getenv(fakeRemoteEnv)
	if remote == "uname -m" {
		fakeLog("uname")
		fmt.Println("x86_64")
		return
	}
	if remote == "id -u" {
		fakeLog("yetki")
		uid := os.Getenv(fakeSSHUIDEnv)
		if uid == "" {
			uid = "0"
		}
		fmt.Println(uid)
		return
	}
	words := shellWords(remote)
	sudo := len(words) >= 3 && words[0] == "sudo" && words[1] == "-n" && words[2] == "--"
	if sudo {
		words = words[3:]
	}
	if len(words) < 3 || words[0] != "bash" || words[1] != "-c" {
		fakeLog("TANINMAYAN " + remote)
		os.Exit(99)
	}
	script := words[2]
	var args []string
	if len(words) > 4 {
		args = words[4:]
	}
	tag := ""
	if sudo {
		tag = " sudo"
	}

	switch script {
	case "id -u":
		fakeLog("yetki" + tag)
		if msg := os.Getenv(fakeSudoFailEnv); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(1)
		}
		fmt.Println("0")

	case remoteUploadPrepare:
		fakeLog("hazırla" + tag)
		size := 0
		if st, err := os.Stat(filepath.Join(remoteDir, args[0]+".part")); err == nil {
			size = int(st.Size())
		}
		fmt.Printf("%s\n%d\n", fakeDir(), size)

	case remoteUploadWrite:
		off, _ := strconv.Atoi(args[1])
		fakeLog(fmt.Sprintf("yaz%s %d", tag, off))
		f, err := os.OpenFile(filepath.Join(remoteDir, args[0]+".part"), os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			os.Exit(1)
		}
		defer func() { _ = f.Close() }()
		if _, err := f.Seek(int64(off), io.SeekStart); err != nil {
			os.Exit(1)
		}
		limit := fakeEnvInt(fakeCutAlwaysEnv)
		if marker := filepath.Join(remoteDir, ".kesildi"); limit < 0 && fakeEnvInt(fakeCutOnceEnv) >= 0 && !fakeExists(marker) {
			limit = fakeEnvInt(fakeCutOnceEnv)
			_ = os.WriteFile(marker, nil, 0o600)
		}
		if first := fakeEnvInt(fakeCutFirstEnv); limit < 0 && first > 0 {
			counter := filepath.Join(remoteDir, ".kesilen")
			b, _ := os.ReadFile(counter)
			if n, _ := strconv.Atoi(string(b)); n < first {
				_ = os.WriteFile(counter, []byte(strconv.Itoa(n+1)), 0o600)
				limit = 1000
			}
		}
		if limit >= 0 {
			_, _ = io.CopyN(f, os.Stdin, int64(limit))
			_ = f.Close()
			os.Exit(sshTransportFailure)
		}
		if os.Getenv(fakeShortEnv) != "" {
			// Başarılı dönen ama eksik yazan sunucu.
			b, _ := io.ReadAll(os.Stdin)
			_, _ = f.Write(b[:len(b)/2])
			return
		}
		_, _ = io.Copy(f, os.Stdin)
		if os.Getenv(fakeCorruptEnv) != "" {
			_, _ = f.WriteAt([]byte{0xff}, 0)
		}

	case remoteInstallStart:
		if len(args) != 4 || args[0] != fakeDir() || args[3] != remoteInstallRun {
			fakeLog("başlat BOZUK ARGÜMAN")
			os.Exit(98)
		}
		fakeLog("başlat" + tag)
		sum, size := args[1], args[2]
		part := filepath.Join(remoteDir, sum+".part")
		n, _ := strconv.Atoi(size)
		_ = os.Truncate(part, int64(n))
		b, _ := os.ReadFile(part)
		got := sha256.Sum256(b)
		if hex.EncodeToString(got[:]) != sum {
			_ = os.Remove(part)
			fmt.Fprintln(os.Stderr, "bootstrap: paketin özeti tutmuyor")
			os.Exit(digestMismatch)
		}
		logText := os.Getenv(fakeInstallLogEnv)
		if logText == "" {
			logText = "kuruldu\n"
		}
		rc := os.Getenv(fakeInstallRCEnv)
		if rc == "" {
			rc = "0"
		}
		if code := os.Getenv(fakeStartRCEnv); code == strconv.Itoa(installBusy) {
			fmt.Fprintln(os.Stderr, "bootstrap: bu sunucuda başka bir kurulum sürüyor")
			os.Exit(installBusy)
		}
		_ = os.WriteFile(filepath.Join(remoteDir, sum+".log"), []byte(logText), 0o600)
		if os.Getenv(fakeDiedEnv) == "" {
			_ = os.WriteFile(filepath.Join(remoteDir, sum+".done"), []byte(rc), 0o600)
		}
		if code := os.Getenv(fakeStartRCEnv); code != "" {
			// installRunning: kurulumu önceki (kopmuş) başlatma başlatmıştı.
			n, _ := strconv.Atoi(code)
			os.Exit(n)
		}

	case remoteInstallFollow:
		if len(args) != 3 || args[0] != fakeDir() {
			fakeLog("izle BOZUK ARGÜMAN")
			os.Exit(98)
		}
		off, _ := strconv.Atoi(args[2])
		fakeLog(fmt.Sprintf("izle%s %d", tag, off))
		data, _ := os.ReadFile(filepath.Join(remoteDir, args[1]+".log"))
		rest := data[min(off, len(data)):]
		if cut := fakeEnvInt(fakeFollowCutEnv); cut >= 0 {
			if marker := filepath.Join(remoteDir, ".izle-kesildi"); !fakeExists(marker) {
				_ = os.WriteFile(marker, nil, 0o600)
				_, _ = os.Stdout.Write(rest[:min(cut, len(rest))])
				os.Exit(sshTransportFailure)
			}
		}
		_, _ = os.Stdout.Write(rest)
		if os.Getenv(fakeDiedEnv) != "" {
			os.Exit(installDied)
		}
		done, _ := os.ReadFile(filepath.Join(remoteDir, args[1]+".done"))
		rc, _ := strconv.Atoi(strings.TrimSpace(string(done)))
		os.Exit(rc)

	default:
		fakeLog("TANINMAYAN betik")
		os.Exit(99)
	}
}

// sahteSSH, sshCommand'ı test binary'sine çevirir, sunucunun yükleme
// dizinini taklit eden bir dizin kurar; çağrı günlüğünün yolunu döndürür.
func sahteSSH(t *testing.T, env map[string]string) string {
	t.Helper()
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}
	log := filepath.Join(t.TempDir(), "ssh.log")
	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHLogEnv, log)
	t.Setenv(fakeRemoteEnv, t.TempDir())
	for k, v := range env {
		t.Setenv(k, v)
	}
	original, originalBackoff := sshCommand, transferBackoff
	sshCommand = os.Args[0]
	transferBackoff = time.Millisecond
	t.Cleanup(func() { sshCommand, transferBackoff = original, originalBackoff })
	return log
}

// cagrilar, sahte sunucuya yapılan çağrıları sırasıyla döndürür.
func cagrilar(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("çağrı günlüğü okunamadı: %v", err)
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}

func kurulumSecenekleri(t *testing.T, repo, host string, sudo bool) Options {
	t.Helper()
	return Options{
		Host:          host,
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
		Sudo:          sudo,
	}
}

func birKurulumCagrisiVarMi(c []string) bool {
	for _, s := range c {
		if strings.HasPrefix(s, "hazırla") || strings.HasPrefix(s, "yaz") || strings.HasPrefix(s, "başlat") {
			return true
		}
	}
	return false
}
