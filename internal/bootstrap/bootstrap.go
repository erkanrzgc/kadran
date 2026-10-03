// Package bootstrap, sunucuyu sıfırdan kurar.
//
// # Tek SSH bağlantısı, tek tar akışı
//
// Gereken her şey (üç binary, systemd birimleri, tmpfiles, istemci açık
// anahtarı ve kurulum betiği) tek bir tar akışında gönderilir ve uzakta
// açılıp çalıştırılır. Dosya başına ayrı `scp` çağırmak hem yavaştır hem
// de yarım kalan bir kurulum bırakabilir.
//
// # Anahtar malzemesi buradan geçmez
//
// Bağlantıyı `ssh` kuruyor: özel anahtar ssh-agent'ta veya ~/.ssh
// altında ve bu koda hiç girmiyor. Sunucuya yüklenen tek anahtar
// kullanıcının AÇIK anahtarıdır.
package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

//go:embed install.sh goc.sh geri.sh
var installScript embed.FS

// serverBinaries, sunucuya kurulan binary'lerdir.
//
// `kadran` (iş istasyonu aracı) burada YOK: sunucuda işi olmayan bir
// binary'yi kurmak, ayrıcalıklı makinedeki yüzeyi gereksiz büyütür.
//
// `kadran-caddy` AYRI bir Go modülünden geliyor (build/caddy/go.mod);
// scripts/build-release.sh onu da aynı `bin/linux-<arch>/` dizinine
// üretiyor, yani burada özel bir muamele gerekmiyor.
var serverBinaries = []string{
	"kadrand", "kadran-exec", "kadran-connect", "kadran-caddy",
}

// unitFiles, depodan kopyalanan systemd varlıkları.
//
// Hepsi tar'a DÜZ isimlerle giriyor; alt dizin yok. Ters vekil bir
// drop-in yerine KENDİ birimiyle geldiği için buna ihtiyaç da kalmadı
// (gerekçe kadran-caddy.service'in başında).
var unitFiles = map[string]string{
	"kadrand.service":     "deploy/systemd/kadrand.service",
	"kadran-exec.service": "deploy/systemd/kadran-exec.service",

	// Ters vekil: kendi birimi, kendi admin soketi, kendi tmpfiles
	// kuralı ve yol açıcı yapılandırması.
	"kadran-caddy.service":       "deploy/systemd/kadran-caddy.service",
	"kadran-caddy-admin.socket":  "deploy/systemd/kadran-caddy-admin.socket",
	"kadran-caddy-tmpfiles.conf": "deploy/systemd/kadran-caddy-tmpfiles.conf",
	"caddy.json":                 "deploy/caddy/config.json",
	// Hacim kökünü nodev,nosuid ile bağlar. Adı systemd'nin mount birimi
	// adlandırmasına UYMAK ZORUNDA (`systemd-escape -p --suffix=mount
	// /var/lib/kadran/volumes`); farklı bir ad verilirse systemd birimi
	// bağlar ama Where= ile eşleştiremez ve birim asla etkin olmaz.
	"var-lib-kadran-volumes.mount": "deploy/systemd/var-lib-kadran-volumes.mount",
	"kadran-tmpfiles.conf":         "deploy/systemd/kadran-tmpfiles.conf",
}

// migrationFiles, seçimli birimlerin (bildirim, uzak yedek, hacim yedeği)
// dosyaları. Bu birimleri `bootstrap` KURMAZ; kullanıcı kendi kurar
// (deploy/notify, deploy/offsite). Ama eski adlı bir kurulumda göç (K-136)
// eski birimleri kaldırıyor ve etkin olanların yerine yenilerini koymak
// ZORUNDA: yoksa zamanlayıcılar sessizce kaybolurdu. goc.sh yalnızca
// göçten önce etkin olanları kuruyor.
var migrationFiles = map[string]string{
	"kadran-notify.service":          "deploy/systemd/kadran-notify.service",
	"kadran-notify.timer":            "deploy/systemd/kadran-notify.timer",
	"kadran-notify-failure@.service": "deploy/systemd/kadran-notify-failure@.service",
	"kadran-notify.sh":               "deploy/notify/kadran-notify.sh",
	"notify-README.md":               "deploy/notify/README.md",
	"kadran-offsite.service":         "deploy/systemd/kadran-offsite.service",
	"kadran-offsite.timer":           "deploy/systemd/kadran-offsite.timer",
	"kadran-offsite.sh":              "deploy/offsite/kadran-offsite.sh",
	"offsite-README.md":              "deploy/offsite/README.md",
	"kadran-volume-backup.service":   "deploy/systemd/kadran-volume-backup.service",
	"kadran-volume-backup.timer":     "deploy/systemd/kadran-volume-backup.timer",
	"kadran-volume-backup.sh":        "deploy/offsite/kadran-volume-backup.sh",
}

// Options, kurulum parametreleridir.
type Options struct {
	// Host, `root@1.2.3.4` biçiminde kurulum hedefi.
	Host string
	// Port, SSH portu. 0 = varsayılan.
	Port int

	// BinaryDir, linux binary'lerinin bulunduğu dizin.
	BinaryDir string
	// RepoRoot, systemd birimlerinin okunacağı depo kökü.
	RepoRoot string
	// ClientKeyPath, sunucuya yetkilendirilecek AÇIK anahtar.
	ClientKeyPath string

	// Sudo, kurulumu root'a SSH açmadan, hedef kullanıcının PAROLASIZ
	// sudo'suyla yapar (K-122).
	Sudo bool

	// Stdout/Stderr, uzak betiğin çıktısının aktarılacağı akışlar.
	Stdout io.Writer
	Stderr io.Writer
}

// Run, kurulumu uçtan uca yürütür.
func Run(ctx context.Context, opts Options) error {
	if err := validate(&opts); err != nil {
		return err
	}

	arch, err := detectArch(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "==> Sunucu mimarisi: %s\n", arch)

	if err := checkPrivilege(ctx, opts); err != nil {
		return err
	}
	if opts.Sudo {
		fmt.Fprintln(opts.Stdout, "==> Yetki: parolasız sudo ile root (root'a SSH kullanılmıyor)")
	}

	archive, err := buildArchive(opts, arch)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "==> Kurulum paketi hazır (%s)\n", humanSize(len(archive)))

	return runInstaller(ctx, opts, archive)
}

func validate(opts *Options) error {
	if err := validateTarget(opts.Host); err != nil {
		return err
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if _, err := os.Stat(opts.ClientKeyPath); err != nil {
		return fmt.Errorf(
			"bootstrap: istemci açık anahtarı okunamadı (%s): %w\n"+
				"--client-key ile başka bir anahtar belirtebilirsiniz",
			opts.ClientKeyPath, err)
	}
	return nil
}

// validateTarget, root yetkisiyle kullanılacak SSH hedefini denetler:
// bootstrap ve `kadran key` ortak.
func validateTarget(host string) error {
	if host == "" {
		return fmt.Errorf("bootstrap: hedef sunucu belirtilmedi")
	}
	// `-` ile başlayan hedef, ssh tarafından konumsal argüman değil
	// SEÇENEK olarak okunur; `-oProxyCommand=<komut>` iş istasyonunda
	// keyfî yerel komut çalıştırır. Kabuk kullanılmadığı için kabuk
	// enjeksiyonu yok, ama argüman enjeksiyonu ayrı bir sınıf.
	//
	// `--` ile ayırmak yerine reddetmenin nedeni: `--` desteği OpenSSH
	// sürümüne göre değişir. Meşru hiçbir hedef `-` ile başlamaz.
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf(
			"bootstrap: hedef `-` ile başlayamaz (%q) — "+
				"ssh bunu seçenek olarak yorumlar", host)
	}
	// kadran-client zorlanmış komutlu, yetkisiz istemci hesabı; kurulum
	// hesabı OLAMAZ. Kullanıcı adı verilmeyen hedef ona düşüyor
	// (client.DefaultSSHUser) ve kurulum anlaşılmaz biçimde zorlanmış
	// komuta çarpardı. Sudo kipinde ayrıca: o hesaba sudo verilmemeli.
	if user, _, ok := strings.Cut(host, "@"); ok && user == clientUser {
		return fmt.Errorf("bootstrap: %s yetkisiz istemci hesabı, bu işlem onunla yapılamaz — "+
			"root@sunucu ya da -sudo kullanıcı@sunucu verin", clientUser)
	}
	return nil
}

// archiveModTime, paketteki her dosyanın zamanı: sabit, paket
// deterministik olsun diye (K-127).
var archiveModTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// clientUser, install.sh'in oluşturduğu yetkisiz istemci hesabı.
const clientUser = "kadran-client"

// checkPrivilege, paketi üretip yüklemeden ÖNCE uzakta root olunup
// olunamayacağını, kurulumun koşacağı TAM biçimle sınar (K-122).
//
// Eskiden root olmayan bir hedef 28 MB'ı yükleyip ancak install.sh'in ilk
// satırında düşüyordu. Sudo kipinde `sudo -n true` yetmezdi: `bash -c`'ye
// izin verildiğini ve uid 0'a inildiğini göstermezdi.
//
// Hata sudo'nun KENDİ mesajını taşıyor ("a password is required", "a
// terminal is required"...): genel bir cümle K-120'nin hatasını
// tekrarlardı.
func checkPrivilege(ctx context.Context, opts Options) error {
	out, err := sshOutput(ctx, opts, remoteCommand(opts, "id -u"))
	if err != nil {
		if opts.Sudo {
			return fmt.Errorf("bootstrap: %s parolasız sudo ile root olamıyor "+
				"(-sudo kipi parola SORMAZ; sudoers'ta NOPASSWD gerekir): %w", opts.Host, err)
		}
		return fmt.Errorf("bootstrap: sunucuda yetki sınanamadı: %w", err)
	}
	if uid := strings.TrimSpace(out); uid != "0" {
		if opts.Sudo {
			return fmt.Errorf("bootstrap: sudo root'a geçmedi (uid %q)", uid)
		}
		return fmt.Errorf("bootstrap: %s root değil (uid %s) — root'a SSH kapalıysa "+
			"`kadran bootstrap -sudo kullanıcı@sunucu` kullanın", opts.Host, uid)
	}
	return nil
}

// remoteCommand, bir uzak betiği kipine göre sarar.
//
// Sudo kipinde betik SABİT bir `sudo -n -- bash -c '<betik>'` satırına
// giriyor; içine kullanıcı girdisi girmiyor, betikler bu paketin
// sabitleri. `-n`: sudo asla parola sormaz, gerekiyorsa düşer — parola
// hiç alınmıyor, sırrı görmeme ilkesi korunuyor. `-E` YOK: install.sh
// hazırlık dizinini ortamla değil argümanla alıyor.
func remoteCommand(opts Options, script string) string {
	if !opts.Sudo {
		return script
	}
	return "sudo -n -- bash -c " + shellQuote(script)
}

// detectArch, sunucunun mimarisini sorar.
//
// Yanlış mimaride bir binary kurmak, servis başlamadan "exec format
// error" ile ölür ve nedeni günlükte kolayca gözden kaçar. Önce sorup
// doğru binary'yi göndermek bu sınıfı tamamen siler.
func detectArch(ctx context.Context, opts Options) (string, error) {
	out, err := sshOutput(ctx, opts, "uname -m")
	if err != nil {
		return "", fmt.Errorf("bootstrap: sunucuya bağlanılamadı: %w", err)
	}
	return archFromUname(out)
}

// archFromUname, `uname -m` çıktısını paket mimarisine çevirir.
// Tanınmayan her şey HATA: yanlış mimariye binary göndermek "exec format
// error" ile, sebebi gözden kaçan bir kurulumdur.
func archFromUname(out string) (string, error) {
	switch machine := strings.TrimSpace(out); machine {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("bootstrap: desteklenmeyen mimari: %q", machine)
	}
}

// buildArchive, kurulum paketini bellekte üretir.
//
// gzip'li (K-119): düz tar 74,7 MiB'tı ve yavaş bir bağlantıda canlıya
// yüklemesi ~13 dk sürdü, bir denemede de bağlantı koptu (K-118). Aynı
// ikililerle gzip'li paket 28,3 MiB (ölçüldü, K-119).
func buildArchive(opts Options, arch string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	// Paket DETERMİNİSTİK (K-127): sha'sı sunucudaki yarım yüklemenin adı.
	// Sabit zaman (uzakta `tar -m` zaten uygulamıyor) ve sıralı birimler;
	// yoksa her koşu farklı bir sha üretir ve kesilen yükleme bir sonraki
	// koşuda devam edemezdi.
	add := func(name string, mode int64, content []byte) error {
		header := &tar.Header{
			Name:    name,
			Mode:    mode,
			Size:    int64(len(content)),
			ModTime: archiveModTime,
			Format:  tar.FormatPAX,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		_, err := tw.Write(content)
		return err
	}

	for _, name := range []string{"install.sh", "goc.sh", "geri.sh"} {
		script, err := installScript.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: kurulum betiği okunamadı (%s): %w", name, err)
		}
		if err := add(name, 0o755, script); err != nil {
			return nil, err
		}
	}

	// Binary'ler mimariye göre alt dizinden okunur:
	//   <BinaryDir>/linux-arm64/kadrand
	archDir := filepath.Join(opts.BinaryDir, "linux-"+arch)
	for _, name := range serverBinaries {
		path := filepath.Join(archDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf(
				"bootstrap: %s bulunamadı (%s): %w\n"+
					"Derlemek için: scripts/build-release.sh",
				name, path, err)
		}
		if err := add(name, 0o755, content); err != nil {
			return nil, err
		}
	}

	for _, files := range []map[string]string{unitFiles, migrationFiles} {
		for _, name := range slices.Sorted(maps.Keys(files)) {
			rel := files[name]
			content, err := os.ReadFile(filepath.Join(opts.RepoRoot, filepath.FromSlash(rel)))
			if err != nil {
				return nil, fmt.Errorf("bootstrap: %s okunamadı: %w", rel, err)
			}
			// systemd ve kabuk dosyaları LF ister; Windows'ta üretilmiş bir
			// CRLF sessizce bozulmaya yol açar.
			if err := add(name, 0o644, normalizeLineEndings(content)); err != nil {
				return nil, err
			}
		}
	}

	key, err := os.ReadFile(opts.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: istemci anahtarı okunamadı: %w", err)
	}
	if err := validatePublicKey(key); err != nil {
		return nil, err
	}
	// Doğrulanan TEK satır gönderiliyor, dosyanın ham hâli değil: baştaki
	// boş satır burada geçer ama install.sh'in tek-satır denetimine kurulumun
	// ORTASINDA takılırdı.
	key = []byte(strings.TrimSpace(string(key)) + "\n")
	if err := add("client_key.pub", 0o644, key); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// validatePublicKey, verilen dosyanın gerçekten bir AÇIK anahtar
// olduğunu doğrular.
//
// Kazara özel anahtar verilmesi felaket olurdu: sunucuya yüklenir ve
// authorized_keys'e yazılırdı. Bu kontrol o kazayı yakalar.
func validatePublicKey(content []byte) error {
	text := strings.TrimSpace(string(content))

	if strings.Contains(text, "PRIVATE KEY") {
		return fmt.Errorf(
			"bootstrap: verilen dosya bir ÖZEL anahtar — açık anahtar (.pub) bekleniyordu")
	}

	// TEK satır (K-131'de bulundu). install.sh satırı `command=...,restrict
	// $(cat client_key.pub)` diye kuruyor: ikinci bir satır authorized_keys'e
	// AYRI ve KISITSIZ bir anahtar olarak düşer, kadran-client'a kabuk açar.
	// `https://github.com/<kullanıcı>.keys` tam olarak böyle bir dosya verir.
	// Sondaki satır sonu TrimSpace'le gitti; içeride kalan her satır sonu ret.
	if strings.ContainsAny(text, "\r\n") {
		return fmt.Errorf("bootstrap: anahtar dosyasında birden fazla satır var — " +
			"tek bir açık anahtar verin (ikinci satır zorlanmış komutsuz bir anahtar olurdu)")
	}

	fields := strings.Fields(text)
	if len(fields) < 2 {
		return fmt.Errorf("bootstrap: açık anahtar biçimi tanınmadı")
	}
	switch fields[0] {
	case "ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521", "sk-ssh-ed25519@openssh.com":
		return nil
	default:
		return fmt.Errorf("bootstrap: tanınmayan anahtar türü: %q", fields[0])
	}
}

// normalizeLineEndings, CRLF'i LF'e çevirir.
func normalizeLineEndings(content []byte) []byte {
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

// runInstaller ve uzak betikler upload.go'da (K-127).
//
// `tar -m`: dosya zamanları uygulanmıyor. İş istasyonunun saati sunucudan
// biraz ilerideyse tar "time stamp … in the future" uyarısı basıyordu
// (taze sunucu testi, K-112); geçici kurulum dosyaları için zaman önemsiz.
//
// G204 (upload.go'daki sshRun): gosec argv'nin sabit olmamasını bayrak
// ediyor. Komut adı sabit, kabuk kullanılmıyor ve argv dizi olarak
// veriliyor; kabuk enjeksiyonu burada temsil EDİLEMEZ. Geriye kalan gerçek
// sınıf argüman enjeksiyonuydu (`-` ile başlayan hedefi ssh seçenek sanar);
// validate() onu reddediyor, bkz. TestRejectsOptionLikeHost. Uzak betikler
// sabit; değişkenler doğrulanmış argüman.

// kurulumHatasi, uzak kurulumun hatasını kullanıcının anlayacağı hâle
// getirir.
//
// Süre sınırı dolduğunda ssh öldürülür ve Windows'ta öldürülen süreç
// yalnızca "exit status 1" döndürür. Taze sunucu testinde (K-112) yavaş
// bir koşuda 75 MiB'lık paket 10 dakikalık sınırı aştı ve kullanıcıya
// sebep söylenmedi. Sebep bağlamda duruyor.
func kurulumHatasi(ctx context.Context, err error, paketBoyutu int) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("bootstrap: süre sınırı aşıldı — kurulum paketi %s, "+
			"yavaş bir bağlantıda yüklemesi uzun sürebilir; -timeout ile daha "+
			"uzun süre verin (ör. -timeout 60m): %w", humanSize(paketBoyutu), ctx.Err())
	}
	return fmt.Errorf("bootstrap: kurulum başarısız: %w", err)
}

// sshCommand, çalıştırılan ssh programı; testler onu sahte bir ssh ile
// değiştiriyor.
var sshCommand = "ssh"

// shellQuote, s'yi POSIX kabuğunda TEK bir kelime olarak tek tırnağa alır.
// Uzaktaki giriş kabuğu (Debian'da useradd varsayılanı /bin/sh, yani dash)
// bu tırnağı çözer. İçerideki her tek tırnak dört karaktere dönüşür:
// tırnağı kapat, ters bölüyle kaçırılmış bir tırnak yaz, tırnağı yeniden
// aç (TestShellQuoteRoundTrips).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func sshArgs(opts Options, remoteCommand string) []string {
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		// Sessizce ölen bağlantı (RST'siz, ör. NAT zaman aşımı) ssh'ı
		// sonsuza dek bekletirdi; ~60 sn yanıtsızlıkta 255 ile çıkıyor ve
		// yükleme/izleme yeniden bağlanıyor (K-127). İzleme uzun süre
		// sessiz kalabildiği için bu şart.
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
	}
	if opts.Port != 0 {
		args = append(args, "-p", fmt.Sprint(opts.Port))
	}
	return append(args, opts.Host, remoteCommand)
}

func sshOutput(ctx context.Context, opts Options, remoteCommand string) (string, error) {
	// G204 gerekçesi için runInstaller'daki nota bakın: sabit komut adı,
	// kabuksuz argv, ve `-` ile başlayan hedef validate()'te reddediliyor.
	cmd := exec.CommandContext(ctx, sshCommand, sshArgs(opts, remoteCommand)...) //nolint:gosec
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return stdout.String(), nil
}

func humanSize(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
