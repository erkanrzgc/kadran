package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── Kaldığı yerden devam eden yükleme ve oturumdan ayrılan kurulum (K-127)
//
// Eskiden paket TEK bir ssh akışıyla gönderiliyor ve aynı akışta
// kuruluyordu. 1 Ekim'de yavaş bir hatta üç uzun yüklemenin üçü de
// "Connection reset by peer" ile koptu ve her biri baştan başlamak
// zorundaydı.
//
// Şimdi üç ayrı adım var:
//
//  1. Yükleme: paket, sunucuda bağlanan kullanıcının KENDİ dizinine
//     (~/.kadran-upload, 0700) adı paketin sha256'sı olan bir dosyaya
//     yazılıyor. Kopan bağlantıdan sonra sunucudaki boyut sorulup yalnızca
//     eksik kısım gönderiliyor. Yazma EKLEME değil AÇIK OFSETLE
//     (`dd seek_bytes conv=notrunc`): ölü bir oturumun süreci sonradan yazsa
//     da aynı baytları aynı yere yazar (1 Ekim'de iki sunucuda da ölü
//     oturumlar dakikalarca yaşadı).
//  2. Başlatma: özet tutarsa kurulum `setsid` ile oturumdan ayrılıyor ve
//     çıktısı bir günlüğe gidiyor. `flock -n` aynı anda tek kurulum; kilidi
//     kurulum süreci bitene kadar tutuyor.
//  3. İzleme: istemci günlüğü kaldığı bayttan izliyor, kopunca yeniden
//     bağlanıyor, bitişi bir işaret dosyasından (çıkış koduyla) okuyor.
//     Kilit boşaldığı hâlde işaret yoksa kurulum süreci ölmüş demektir
//     (ör. bellek yetmedi); izleme bunu bekleyip asılı kalmıyor.
//
// Uzak betikler SABİT; değişkenler (sha, dizin, ofset, boyut) doğrulanmış
// ARGÜMAN olarak geçiyor: `bash -c '<betik>' _ <arg>...`. Sudo kipinde
// yalnızca başlatma adımı sudo altında koşuyor; `umask 022` root'un
// yazdığı günlüğü ve kilidi, izleyen (yetkisiz) kullanıcıya okunur kılıyor.
// Dizin 0700 olduğu için başkası göremiyor.

const (
	// maxTransferAttempts, bir adımın bağlantı kopmalarıyla birlikte toplam
	// deneme sayısı: son deneme dahil maxTransferAttempts-1 kopma tolere edilir.
	maxTransferAttempts = 6
	// sshTransportFailure, ssh'ın KENDİ hatasının çıkış kodu: uzak komutun
	// değil bağlantının düştüğünü söyler. Yalnızca bu yeniden denenir.
	sshTransportFailure = 255
	// Başlatma betiğinin çıkış kodları.
	digestMismatch = 3
	installRunning = 4 // aynı paketin kurulumu zaten sürüyor: izlemeye geç
	installBusy    = 5 // başka bir kurulum sürüyor
	// installDied, izleme betiğinin "süreç işaret bırakmadan bitti" kodu.
	// Kurulumun kendi 253-255 kodları 254'e katlanıyor; çakışmıyor.
	installDied = 253
)

// transferBackoff, iki deneme arası bekleme; testler kısaltıyor.
var transferBackoff = 3 * time.Second

const remoteUploadPrepare = `set -e
umask 077
mkdir -p "$HOME/.kadran-upload"
chmod 700 "$HOME/.kadran-upload"
cd "$HOME/.kadran-upload"
find . -maxdepth 1 -type f -mtime +0 -delete
pwd
if [ -f "$1.part" ]; then stat -c %s "$1.part"; else echo 0; fi`

const remoteUploadWrite = `set -e
cd "$HOME/.kadran-upload"
dd of="$1.part" bs=65536 seek="$2" oflag=seek_bytes conv=notrunc status=none`

// remoteInstallStart; argümanlar: $1 dizin, $2 sha, $3 boyut, $4 çalıştırıcı.
// `truncate -c`: dosya yoksa sıfırlarla dolu bir dosya OLUŞTURMUYOR; özet
// yine tutmaz ve paket yeniden yüklenir.
const remoteInstallStart = `set -e
umask 022
cd "$1"
exec 9>install.lock
if ! flock -n 9; then
    if [ -f "$2.log" ] && [ ! -f "$2.done" ]; then exit 4; fi
    echo "bootstrap: another install is running on this server" >&2
    exit 5
fi
truncate -c -s "$3" "$2.part"
if ! echo "$2  $2.part" | sha256sum -c --status 2>/dev/null; then
    echo "bootstrap: the package digest does not match" >&2
    rm -f "$2.part"
    exit 3
fi
rm -f "$2.log" "$2.done" "$2.done.tmp"
setsid bash -c "$4" _ "$1" "$2" > "$2.log" 2>&1 < /dev/null &`

// remoteInstallRun, oturumdan ayrılmış kurulumun kendisi; argümanlar: $1
// dizin, $2 sha. Kilidi (fd 9) bitene kadar tutuyor; alt süreçlere
// geçirmiyor (`9>&-`), arkada kalan bir süreç kilidi sonsuza dek tutmasın.
// İşaret önce geçici dosyaya yazılıp taşınıyor: izleme yarım yazılmış
// (boş) bir işaret okumasın. `-z`: paket gzip'li (buildArchive) —
// TestRemoteExtractionMatchesTheArchiveFormat ikisini birbirine bağlıyor.
const remoteInstallRun = `d="$(mktemp -d /tmp/kadran-bootstrap.XXXXXX)"
trap 'rm -rf "$d"' EXIT
rc=0
tar -x -z -m -C "$d" -f "$1/$2.part" 9>&- && bash "$d/install.sh" "$d" 9>&- || rc=$?
echo "$rc" > "$1/$2.done.tmp" && mv -f "$1/$2.done.tmp" "$1/$2.done"
if [ "$rc" = 0 ]; then rm -f "$1/$2.part"; fi`

// remoteInstallFollow; argümanlar: $1 dizin, $2 sha, $3 zaten okunmuş bayt.
const remoteInstallFollow = `cd "$1"
log="$2.log"
fin="$2.done"
off="$3"
emit() {
    sz=$(stat -c %s "$log" 2>/dev/null) || sz=0
    if [ "$sz" -gt "$off" ]; then
        tail -c +"$((off + 1))" "$log" | head -c "$((sz - off))"
        off=$sz
    fi
}
while :; do
    emit
    if [ -f "$fin" ]; then
        emit
        rc=$(cat "$fin")
        if [ "$rc" -ge 253 ]; then rc=254; fi
        exit "$rc"
    fi
    if flock -n install.lock true; then
        if [ -f "$fin" ]; then continue; fi
        emit
        exit 253
    fi
    sleep 1
done`

// uploadDirPattern, sunucudan gelen yükleme dizini: mutlak, boşluksuz,
// tırnaksız. Değer betiğe argüman olarak gidiyor; yine de sunucudan geldiği
// için sınırlanıyor.
var uploadDirPattern = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)

func validUploadDir(dir string) error {
	if !uploadDirPattern.MatchString(dir) || strings.Contains(dir, "..") {
		return fmt.Errorf("bootstrap: unexpected upload directory reported by the server: %q", dir)
	}
	return nil
}

// bashArgs, sabit bir betiği konumsal argümanlarla çağıran uzak komut.
func bashArgs(script string, args ...string) string {
	var b strings.Builder
	b.WriteString("bash -c ")
	b.WriteString(shellQuote(script))
	b.WriteString(" _")
	for _, a := range args {
		b.WriteString(" ")
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

// privileged, betiği kipine göre root olarak koşturur.
func privileged(opts Options, script string, args ...string) string {
	cmd := bashArgs(script, args...)
	if opts.Sudo {
		return "sudo -n -- " + cmd
	}
	return cmd
}

// sshRun, uzak komutu koşturur ve ÇIKIŞ KODUNU döndürür. err yalnızca
// ssh'ın hiç başlayamadığı ya da bağlamın bittiği durumlar için; çağıran
// onu kurulumHatasi'ndan geçiriyor.
func sshRun(ctx context.Context, opts Options, remote string, stdin io.Reader, stdout io.Writer) (int, error) {
	// G204 gerekçesi bootstrap.go'daki notta: sabit program, kabuksuz argv,
	// `-` ile başlayan hedef validate()'te reddediliyor; uzak betikler sabit.
	cmd := exec.CommandContext(ctx, sshCommand, sshArgs(opts, remote)...) //nolint:gosec
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = opts.Stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// retrier, bir adımın bağlantı kopmalarını sayar.
type retrier struct {
	opts     Options
	what     string // küçük harfle: "yükleme"
	size     int    // süre sınırı mesajı için paket boyutu
	failures int
}

// cut, bir kopmayı kaydeder ve bekler. Deneme hakkı bittiyse ya da bağlam
// bittiyse kullanıcıya gösterilecek son hatayı döndürür.
func (r *retrier) cut(ctx context.Context) error {
	r.failures++
	if r.failures >= maxTransferAttempts {
		return fmt.Errorf("bootstrap: %s did not finish in %d attempts — the connection keeps dropping",
			r.what, maxTransferAttempts)
	}
	fmt.Fprintf(r.opts.Stdout, "==> Connection dropped (%s, attempt %d/%d); retrying\n",
		r.what, r.failures, maxTransferAttempts)
	t := time.NewTimer(time.Duration(r.failures) * transferBackoff)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return kurulumHatasi(ctx, ctx.Err(), r.size)
	}
}

// prepareUpload, sunucudaki yükleme dizinini ve oradaki yarım paketin
// boyutunu sorar. code, ssh'ın çıkış kodu.
func prepareUpload(ctx context.Context, opts Options, sum string) (dir string, have int, code int, err error) {
	var out bytes.Buffer
	code, err = sshRun(ctx, opts, bashArgs(remoteUploadPrepare, sum), nil, &out)
	if err != nil || code != 0 {
		return "", 0, code, err
	}
	fields := strings.Fields(out.String())
	if len(fields) != 2 {
		return "", 0, 0, fmt.Errorf("bootstrap: unexpected upload directory reply: %q", out.String())
	}
	if err := validUploadDir(fields[0]); err != nil {
		return "", 0, 0, err
	}
	have, err = strconv.Atoi(fields[1])
	if err != nil || have < 0 {
		return "", 0, 0, fmt.Errorf("bootstrap: could not read the partial upload size on the server: %q", fields[1])
	}
	return fields[0], have, 0, nil
}

// uploadArchive, paketi sunucuya kaldığı yerden devam ederek yükler ve
// sunucudaki dizini döndürür.
//
// Sunucudaki dosya paketten UZUNSA da yükleme bitmiş sayılıyor: başlatma
// onu paket boyutuna kesiyor ve özeti denetliyor; tutmazsa siliyor ve
// runInstaller bir kez baştan yüklüyor.
func uploadArchive(ctx context.Context, opts Options, archive []byte, sum string) (string, error) {
	total := len(archive)
	r := &retrier{opts: opts, what: "upload", size: total}
	wrote := false
	for {
		dir, have, code, err := prepareUpload(ctx, opts, sum)
		if err != nil {
			return "", kurulumHatasi(ctx, err, total)
		}
		switch {
		case code == sshTransportFailure:
			if err := r.cut(ctx); err != nil {
				return "", err
			}
			continue
		case code != 0:
			return "", fmt.Errorf("bootstrap: could not prepare the upload directory (exit %d)", code)
		case have >= total:
			return dir, nil
		case wrote:
			// Yazma başarılı döndü ama dosya eksik: yeniden denemek aynı
			// şeyi tekrarlar; sonsuz döngü yerine dur.
			return "", fmt.Errorf("bootstrap: the write looked successful but the server has %s/%s",
				humanSize(have), humanSize(total))
		case have > 0:
			fmt.Fprintf(opts.Stdout, "==> Resuming the upload: %s/%s\n", humanSize(have), humanSize(total))
		}

		code, err = sshRun(ctx, opts, bashArgs(remoteUploadWrite, sum, strconv.Itoa(have)),
			bytes.NewReader(archive[have:]), io.Discard)
		if err != nil {
			return "", kurulumHatasi(ctx, err, total)
		}
		switch code {
		case 0:
			// Bir sonraki tur sunucudaki boyutu yeniden sorup bitişi doğruluyor.
			wrote = true
		case sshTransportFailure:
			if err := r.cut(ctx); err != nil {
				return "", err
			}
		default:
			// Disk dolması gibi gerçek bir yazma hatası: yeniden denemek
			// aynı hatayı tekrarlar.
			return "", fmt.Errorf("bootstrap: could not write the package to the server (exit %d)", code)
		}
	}
}

// countingWriter, izlemenin kaldığı yeri saymak için yazılan baytları sayar.
type countingWriter struct {
	w io.Writer
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// runInstaller, paketi yükler, kurulumu oturumdan ayırarak başlatır ve
// çıktısını izler (K-127).
func runInstaller(ctx context.Context, opts Options, archive []byte) error {
	digest := sha256.Sum256(archive)
	sum := hex.EncodeToString(digest[:])

	for reuploaded := false; ; reuploaded = true {
		dir, err := uploadArchive(ctx, opts, archive, sum)
		if err != nil {
			return err
		}
		fmt.Fprintln(opts.Stdout, "==> Package uploaded; checking its digest on the server")

		code, err := startInstall(ctx, opts, dir, sum, len(archive))
		if err != nil {
			return err
		}
		switch code {
		case 0, installRunning:
			// installRunning: önceki başlatma isteği kopmuş ama kurulumu
			// başlatmış; izleme onun günlüğünü okur.
			return followInstall(ctx, opts, dir, sum)
		case digestMismatch:
			if reuploaded {
				return errors.New("bootstrap: the package was uploaded twice but its digest did not match")
			}
			fmt.Fprintln(opts.Stdout, "==> The package on the server is corrupt; uploading it once more from scratch")
		case installBusy:
			return errors.New("bootstrap: another install is running on the server; wait for it to finish and try again")
		default:
			return fmt.Errorf("bootstrap: could not start the install (exit %d)", code)
		}
	}
}

func startInstall(ctx context.Context, opts Options, dir, sum string, size int) (int, error) {
	remote := privileged(opts, remoteInstallStart, dir, sum, strconv.Itoa(size), remoteInstallRun)
	r := &retrier{opts: opts, what: "starting the install", size: size}
	for {
		code, err := sshRun(ctx, opts, remote, nil, opts.Stdout)
		if err != nil {
			return -1, kurulumHatasi(ctx, err, size)
		}
		if code != sshTransportFailure {
			return code, nil
		}
		if err := r.cut(ctx); err != nil {
			return -1, err
		}
	}
}

func followInstall(ctx context.Context, opts Options, dir, sum string) error {
	logPath := dir + "/" + sum + ".log"
	out := &countingWriter{w: opts.Stdout}
	r := &retrier{opts: opts, what: "following the install"}
	for {
		code, err := sshRun(ctx, opts,
			bashArgs(remoteInstallFollow, dir, sum, strconv.Itoa(out.n)), nil, out)
		if err == nil && code == sshTransportFailure {
			// Günlük satır ortasında kesilmiş olabilir; uyarı yeni satırda.
			fmt.Fprintln(opts.Stdout)
			err = r.cut(ctx)
			if err == nil {
				continue
			}
		}
		switch {
		case err != nil:
			// Paket yüklendi ve kurulum başladı; süre sınırı ya da kopma
			// yalnızca İZLEMEYİ bitiriyor.
			return fmt.Errorf("bootstrap: could not follow the install, it may still be running on the server; its log: %s: %w",
				logPath, err)
		case code == 0:
			return nil
		case code == installDied:
			return fmt.Errorf("bootstrap: the install process on the server ended without leaving a completion marker "+
				"(it may have run out of memory or been killed); its log: %s", logPath)
		default:
			return fmt.Errorf("bootstrap: install failed (exit %d)", code)
		}
	}
}
