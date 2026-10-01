package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── Kaldığı yerden devam eden yükleme (K-127), sahte sunucuyla ─────────
//
// Paket rastgele baytlar: sahte sunucu tar açmıyor, yalnızca özeti
// denetliyor. 300 KB, kopmaların paketin ORTASINA düşmesine yetiyor.

const testPaketBoyutu = 300_000

func rastgelePaket(t *testing.T) ([]byte, string) {
	t.Helper()
	b := make([]byte, testPaketBoyutu)
	r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // test verisi
	for i := range b {
		b[i] = byte(r.UintN(256))
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:])
}

func yuklemeSecenekleri(sudo bool) (Options, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return Options{Host: "root@sunucu", Sudo: sudo, Stdout: &out, Stderr: &errOut}, &out, &errOut
}

func say(c []string, prefix string) int {
	n := 0
	for _, s := range c {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// Kopma sayısı deneme hakkının bir eksiği: son deneme başarılıysa yükleme
// BİTMİŞ sayılmalı. (İlk sürüm, başarılı yazmayı doğrulayan turu da bir
// deneme sayıyordu; 5 kopmadan sonra tamamlanan yüklemeyi "bağlantı sürekli
// kopuyor" diye reddediyordu.)
func TestUploadToleratesACutOnAllButTheLastAttempt(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeCutFirstEnv: strconv.Itoa(maxTransferAttempts - 1)})
	archive, _ := rastgelePaket(t)
	opts, out, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("%d kopmadan sonra tamamlanan yükleme reddedildi: %v\n%s", maxTransferAttempts-1, err, out)
	}
	if n := say(cagrilar(t, log), "yaz"); n != maxTransferAttempts {
		t.Fatalf("%d yazma bekleniyordu, %d oldu", maxTransferAttempts, n)
	}
}

func TestUploadResumesFromTheServerSize(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeCutOnceEnv: "100000"})
	archive, _ := rastgelePaket(t)
	opts, out, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("kopan yükleme sürdürülemedi: %v", err)
	}
	want := []string{"hazırla", "yaz 0", "hazırla", "yaz 100000", "hazırla", "başlat", "izle 0"}
	if c := cagrilar(t, log); strings.Join(c, "|") != strings.Join(want, "|") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
	if !strings.Contains(out.String(), "kaldığı yerden") {
		t.Fatalf("devam kullanıcıya söylenmedi:\n%s", out)
	}
}

func TestUploadGivesUpWhenTheLineKeepsDropping(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeCutAlwaysEnv: "1000"})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "denemede") {
		t.Fatalf("sürekli kopan hat hata vermedi: %v", err)
	}
	c := cagrilar(t, log)
	if n := say(c, "yaz"); n != maxTransferAttempts {
		t.Fatalf("%d yazma bekleniyordu, %d oldu: %q", maxTransferAttempts, n, c)
	}
	if say(c, "başlat") != 0 {
		t.Fatalf("yarım paketle kurulum başlatıldı: %q", c)
	}
}

// Sunucudaki dosya paketten UZUNSA (artık): yeniden yazılmıyor, kurulum
// onu pakete kesiyor (truncate) ve özeti denetliyor.
func TestALongerPartIsTruncatedNotRewritten(t *testing.T) {
	log := sahteSSH(t, nil)
	archive, sum := rastgelePaket(t)
	part := filepath.Join(os.Getenv(fakeRemoteEnv), sum+".part")
	if err := os.WriteFile(part, append(bytes.Clone(archive), "fazlalık"...), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, _, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("uzun artık kurulumu düşürdü: %v", err)
	}
	want := []string{"hazırla", "başlat", "izle 0"}
	if c := cagrilar(t, log); strings.Join(c, "|") != strings.Join(want, "|") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
}

// Sunucudaki paket bozuksa (ör. aynı adlı eski bir artık) kurulum
// başlamıyor, paket BİR KEZ baştan yükleniyor.
func TestACorruptPartIsUploadedAgainOnce(t *testing.T) {
	log := sahteSSH(t, nil)
	archive, sum := rastgelePaket(t)
	bozuk := bytes.Clone(archive)
	bozuk[len(bozuk)/2] ^= 0xff
	if err := os.WriteFile(filepath.Join(os.Getenv(fakeRemoteEnv), sum+".part"), bozuk, 0o600); err != nil {
		t.Fatal(err)
	}
	opts, out, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("bozuk artıktan kurtarılamadı: %v", err)
	}
	want := []string{"hazırla", "başlat", "hazırla", "yaz 0", "hazırla", "başlat", "izle 0"}
	if c := cagrilar(t, log); strings.Join(c, "|") != strings.Join(want, "|") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
	if !strings.Contains(out.String(), "bozuk") {
		t.Fatalf("yeniden yükleme kullanıcıya söylenmedi:\n%s", out)
	}
}

func TestPersistentCorruptionFailsAfterOneRetry(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeCorruptEnv: "1"})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "özeti tutmadı") {
		t.Fatalf("hep bozulan paket hata vermedi: %v", err)
	}
	if c := cagrilar(t, log); say(c, "başlat") != 2 || say(c, "izle") != 0 {
		t.Fatalf("iki başlatma ve sıfır izleme bekleniyordu: %q", c)
	}
}

// İzleme koparsa kaldığı bayttan sürüyor: günlük ne tekrar ne eksik.
func TestFollowReconnectsAtTheRightByte(t *testing.T) {
	const gunluk = "satir1\nsatir2\nsatir3\n"
	log := sahteSSH(t, map[string]string{fakeInstallLogEnv: gunluk, fakeFollowCutEnv: "7"})
	archive, _ := rastgelePaket(t)
	opts, out, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("izleme kopunca kurulum düştü: %v", err)
	}
	c := cagrilar(t, log)
	if got := c[len(c)-2:]; strings.Join(got, "|") != "izle 0|izle 7" {
		t.Fatalf("izleme yanlış bayttan sürdü: %q", c)
	}
	for _, satir := range []string{"satir1\n", "satir2\nsatir3\n"} {
		if n := strings.Count(out.String(), satir); n != 1 {
			t.Fatalf("%q çıktıda %d kez:\n%s", satir, n, out)
		}
	}
}

func TestInstallFailureCarriesTheExitCode(t *testing.T) {
	sahteSSH(t, map[string]string{fakeInstallRCEnv: "7"})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "çıkış 7") {
		t.Fatalf("kurulumun çıkış kodu taşınmadı: %v", err)
	}
}

// Önceki başlatma isteği kopmuş ama kurulumu başlatmışsa: ikinci başlatma
// "zaten sürüyor" der ve istemci onu izler.
func TestAnInstallAlreadyRunningIsFollowed(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeStartRCEnv: strconv.Itoa(installRunning)})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	if err := runInstaller(context.Background(), opts, archive); err != nil {
		t.Fatalf("süren kurulum izlenmedi: %v", err)
	}
	if c := cagrilar(t, log); c[len(c)-1] != "izle 0" {
		t.Fatalf("izlemeye geçilmedi: %q", c)
	}
}

func TestAnotherInstallRunningIsReported(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeStartRCEnv: strconv.Itoa(installBusy)})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "başka bir kurulum") {
		t.Fatalf("başka kurulum bildirilmedi: %v", err)
	}
	if c := cagrilar(t, log); say(c, "izle") != 0 {
		t.Fatalf("başkasının kurulumu izlendi: %q", c)
	}
}

// Kurulum süreci işaret bırakmadan öldüyse (ör. bellek yetmedi) izleme
// sonsuza dek beklemiyor, sebebi ve günlüğün yerini söylüyor.
func TestADeadInstallerIsReportedNotAwaited(t *testing.T) {
	sahteSSH(t, map[string]string{fakeDiedEnv: "1"})
	archive, sum := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "bitiş işareti") ||
		!strings.Contains(err.Error(), fakeReportedDir+"/"+sum+".log") {
		t.Fatalf("ölen kurulum doğru bildirilmedi: %v", err)
	}
}

// Yazma başarılı dönüp dosya eksik kalırsa durur; aksi hâlde kopma sayılmayan
// turlar sonsuza dek dönerdi.
func TestAShortSuccessfulWriteStopsInsteadOfLooping(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeShortEnv: "1"})
	archive, _ := rastgelePaket(t)
	opts, _, _ := yuklemeSecenekleri(false)

	err := runInstaller(context.Background(), opts, archive)

	if err == nil || !strings.Contains(err.Error(), "başarılı göründü") {
		t.Fatalf("eksik yazma fark edilmedi: %v", err)
	}
	if n := say(cagrilar(t, log), "yaz"); n != 1 {
		t.Fatalf("tek yazma bekleniyordu, %d oldu", n)
	}
}

// Sunucunun bildirdiği dizin betiklere argüman olarak gidiyor; beklenmedik
// bir değerle hiçbir şey yazılmıyor.
func TestAnUnexpectedUploadDirIsRejected(t *testing.T) {
	for _, dir := range []string{"goreli/yol", "/a/../etc", "/a b", "/a'b", "/a\"b", "/a$b"} {
		t.Run(dir, func(t *testing.T) {
			log := sahteSSH(t, map[string]string{fakeDirEnv: dir})
			archive, _ := rastgelePaket(t)
			opts, _, _ := yuklemeSecenekleri(false)

			err := runInstaller(context.Background(), opts, archive)

			if err == nil || !strings.Contains(err.Error(), "beklenmedik") {
				t.Fatalf("%q kabul edildi: %v", dir, err)
			}
			if c := cagrilar(t, log); say(c, "yaz") != 0 {
				t.Fatalf("beklenmedik dizine yazıldı: %q", c)
			}
		})
	}
}
