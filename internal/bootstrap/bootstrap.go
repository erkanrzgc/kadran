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
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed install.sh
var installScript embed.FS

// serverBinaries, sunucuya kurulan üç binary'dir.
//
// `panely` (iş istasyonu aracı) burada YOK: sunucuda işi olmayan bir
// binary'yi kurmak, ayrıcalıklı makinedeki yüzeyi gereksiz büyütür.
var serverBinaries = []string{"panelyd", "panely-exec", "panely-connect"}

// unitFiles, depodan kopyalanan systemd varlıkları.
var unitFiles = map[string]string{
	"panelyd.service":     "deploy/systemd/panelyd.service",
	"panely-exec.service": "deploy/systemd/panely-exec.service",
	// Hacim kökünü nodev,nosuid ile bağlar. Adı systemd'nin mount birimi
	// adlandırmasına UYMAK ZORUNDA (`systemd-escape -p --suffix=mount
	// /var/lib/panely/volumes`); farklı bir ad verilirse systemd birimi
	// bağlar ama Where= ile eşleştiremez ve birim asla etkin olmaz.
	"var-lib-panely-volumes.mount": "deploy/systemd/var-lib-panely-volumes.mount",
	"panely-tmpfiles.conf":         "deploy/systemd/panely-tmpfiles.conf",
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

	archive, err := buildArchive(opts, arch)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "==> Kurulum paketi hazır (%s)\n", humanSize(len(archive)))

	return runInstaller(ctx, opts, archive)
}

func validate(opts *Options) error {
	if opts.Host == "" {
		return fmt.Errorf("bootstrap: hedef sunucu belirtilmedi")
	}
	// `-` ile başlayan hedef, ssh tarafından konumsal argüman değil
	// SEÇENEK olarak okunur; `-oProxyCommand=<komut>` iş istasyonunda
	// keyfî yerel komut çalıştırır. Kabuk kullanılmadığı için kabuk
	// enjeksiyonu yok, ama argüman enjeksiyonu ayrı bir sınıf.
	//
	// `--` ile ayırmak yerine reddetmenin nedeni: `--` desteği OpenSSH
	// sürümüne göre değişir. Meşru hiçbir hedef `-` ile başlamaz.
	if strings.HasPrefix(opts.Host, "-") {
		return fmt.Errorf(
			"bootstrap: hedef `-` ile başlayamaz (%q) — "+
				"ssh bunu seçenek olarak yorumlar", opts.Host)
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
func buildArchive(opts Options, arch string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	add := func(name string, mode int64, content []byte) error {
		header := &tar.Header{
			Name:    name,
			Mode:    mode,
			Size:    int64(len(content)),
			ModTime: time.Now(),
			Format:  tar.FormatPAX,
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		_, err := tw.Write(content)
		return err
	}

	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		return nil, fmt.Errorf("bootstrap: kurulum betiği okunamadı: %w", err)
	}
	if err := add("install.sh", 0o755, script); err != nil {
		return nil, err
	}

	// Binary'ler mimariye göre alt dizinden okunur:
	//   <BinaryDir>/linux-arm64/panelyd
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

	for name, rel := range unitFiles {
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

	key, err := os.ReadFile(opts.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: istemci anahtarı okunamadı: %w", err)
	}
	if err := validatePublicKey(key); err != nil {
		return nil, err
	}
	if err := add("client_key.pub", 0o644, normalizeLineEndings(key)); err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
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

// runInstaller, paketi gönderir ve kurulum betiğini çalıştırır.
//
// Uzak kabuk komutu bilerek küçük: geçici dizin aç, tar'ı çöz, betiği
// çalıştır, dizini temizle. Betiğin kendisi paketin içinde olduğu için
// buradaki tek satır güncellenmek zorunda kalmıyor.
func runInstaller(ctx context.Context, opts Options, archive []byte) error {
	const remote = `set -e
d="$(mktemp -d /tmp/panely-bootstrap.XXXXXX)"
trap 'rm -rf "$d"' EXIT
tar -x -C "$d"
bash "$d/install.sh" "$d"`

	// G204 bastırılıyor. Bastırılan şey tam olarak şu: gosec, argv'nin
	// sabit olmamasını bayrak ediyor. Komut adı sabit ("ssh"), kabuk
	// kullanılmıyor ve argv dizi olarak veriliyor — kabuk enjeksiyonu
	// burada temsil EDİLEMEZ. Geriye kalan gerçek sınıf argüman
	// enjeksiyonuydu (`-` ile başlayan hedefi ssh seçenek sanar);
	// validate() onu reddediyor, bkz. TestRejectsOptionLikeHost.
	// remote sabit bir dize.
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(opts, remote)...) //nolint:gosec
	cmd.Stdin = bytes.NewReader(archive)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bootstrap: kurulum başarısız: %w", err)
	}
	return nil
}

func sshArgs(opts Options, remoteCommand string) []string {
	args := []string{
		"-T",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
	}
	if opts.Port != 0 {
		args = append(args, "-p", fmt.Sprint(opts.Port))
	}
	return append(args, opts.Host, remoteCommand)
}

func sshOutput(ctx context.Context, opts Options, remoteCommand string) (string, error) {
	// G204 gerekçesi için runInstaller'daki nota bakın: sabit komut adı,
	// kabuksuz argv, ve `-` ile başlayan hedef validate()'te reddediliyor.
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(opts, remoteCommand)...) //nolint:gosec
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
