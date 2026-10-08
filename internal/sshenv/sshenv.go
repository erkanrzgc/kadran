// Package sshenv, sshd'nin oturuma verdiği bilgiden çağıranın kimliğini
// türetir.
//
// # Parmak izi nereden geliyor (K-134)
//
// `ExposeAuthInfo yes` ile sshd, kimlik doğrulamada kullanılan yöntemleri
// ve açık anahtarı geçici bir DOSYAYA yazar ve yolunu oturuma
// SSH_USER_AUTH değişkeniyle verir (sshd_config(5), sunucuda okundu).
//
// ⚠ Bu paket eskiden SSH_AUTH_INFO_0'ı okuyordu. O değişken sshd'nin PAM'e
// verdiği İÇ değişkendir; OpenSSH onu oturum ortamına bilerek GEÇİRMEZ
// (openssh-portable session.c: `PAM_ENV_DENYLIST "SSH_AUTH_INFO*,…"`;
// aynı dosya oturuma `SSH_USER_AUTH`'ı koyuyor — kaynaktan okundu).
// Sonuç ölçüldü: canlıda SSH kökenli 56 denetim kaydının 0'ında parmak izi
// vardı. Testler değişkeni kendileri koyduğu için hiçbir şey kızarmadı;
// hata ancak gerçek sunucunun denetim kaydına bakınca görüldü.
//
// # Neden güvenilir?
//
// SSH_USER_AUTH'ı ve SSH_CONNECTION'ı sshd, kimlik doğrulaması
// TAMAMLANDIKTAN SONRA ayarlar. Uzak istemcinin bunlara karışabileceği İKİ
// yol vardır ve ikisi de ayrı ayrı kapatılmıştır:
//
//  1. authorized_keys'teki `environment="AD=deger"` seçeneği. sshd(8):
//     "Environment variables set this way override other default
//     environment values" — yani açık olsaydı SSH_USER_AUTH sahte bir
//     dosyayı gösterebilirdi. Bunu kapatan `PermitUserEnvironment no`'dur.
//  2. İstemcinin `SendEnv` ile gönderdiği değişkenler. Bunları `AcceptEnv`
//     süzer ve varsayılanı "hiçbirini kabul etme"dir. Eklemeli çalıştığı
//     için boş bir değerle sıfırlanamaz; bu yüzden bootstrap bilerek hiç
//     AcceptEnv satırı yazmaz.
//
// Dosyanın kendisini istemci değiştiremez: zorlanmış komut yüzünden
// sunucuda kod çalıştıramaz.
//
// DİKKAT — burada kolay bir yanlış var: bunları kapatan şey `restrict`
// DEĞİLDİR. sshd(8), restrict'i "disable port, agent and X11 forwarding,
// as well as disabling PTY allocation and execution of ~/.ssh/rc" diye
// tanımlar; ortam işlemesi bu listede YOKTUR. Kadran bir zamanlar bunun
// tersini iddia eden bir yorum taşıyordu (docs/decisions.md K-031).
//
// `PermitUserEnvironment no`, bootstrap'ın sshd drop-in'inde GENEL kapsamda
// açıkça yazılır — `Match` bloğunun içinde değil, çünkü o anahtar kelime
// Match içinde geçerli değildir ve oraya konması sshd yapılandırmasını
// bozar. Varsayılanı zaten `no` olsa da yazılır: bu değer denetim izinin
// doğruluğunu taşır ve dağıtımın genel yapılandırmasına bırakılamaz.
package sshenv

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// maxAuthFile, okunacak kimlik dosyasının üst sınırı. Gerçeği tek satır,
// birkaç yüz bayt; büyüğü bozuk sayılır.
const maxAuthFile = 64 << 10

// Identity, sshd'nin bildirdiği bağlantı kimliğidir.
type Identity struct {
	// Fingerprint, "SHA256:..." biçiminde açık anahtar parmak izi.
	// SSH_USER_AUTH dosyası yoksa ya da açık anahtar satırı taşımıyorsa
	// boş kalır.
	Fingerprint string

	// SourceIP, istemcinin IP adresi.
	SourceIP string

	// KeyType, "ssh-ed25519" gibi anahtar türü.
	KeyType string
}

// ErrNoConnectionInfo, SSH_CONNECTION bulunamadığında döner.
var ErrNoConnectionInfo = errors.New("sshenv: SSH_CONNECTION is not set")

// Parse, ortamdan ve sshd'nin kimlik dosyasından kimliği çıkarır.
//
// getenv ve readFile test edilebilirlik içindir; üretimde os.Getenv ve
// os.ReadFile geçilir.
//
// Kimlik dosyası yoksa, okunamıyorsa ya da bozuksa hata DÖNMEZ, yalnızca
// parmak izi boş kalır: bu, bağlantıyı reddetmek için bir sebep değildir —
// denetim kaydı sadece daha az bilgi taşır. Boş parmak izi "bilinmiyor"
// demektir ve bu dürüsttür.
func Parse(getenv func(string) string, readFile func(string) ([]byte, error)) (Identity, error) {
	conn := getenv("SSH_CONNECTION")
	if conn == "" {
		return Identity{}, ErrNoConnectionInfo
	}

	id := Identity{SourceIP: clientIP(conn)}
	if keyType, fingerprint, ok := authFileKey(getenv("SSH_USER_AUTH"), readFile); ok {
		id.KeyType, id.Fingerprint = keyType, fingerprint
	}
	return id, nil
}

// authFileKey, sshd'nin kimlik dosyasındaki açık anahtarı okur. Dosya
// yoksa, okunamıyorsa ya da çok büyükse ok=false: bunlar bilerek hata
// sayılmıyor, parmak izi yalnızca "bilinmiyor" kalır.
func authFileKey(path string, readFile func(string) ([]byte, error)) (keyType, fingerprint string, ok bool) {
	if path == "" {
		return "", "", false
	}
	content, err := readFile(path)
	if err != nil || len(content) > maxAuthFile {
		return "", "", false
	}
	return firstPublickey(content)
}

// firstPublickey, kimlik dosyasının ilk açık anahtar satırını çözer.
//
// Dosyada kullanılan her yöntem bir satırdır (AuthenticationMethods birden
// fazlasını isteyebilir); açık anahtar dışındakiler atlanır.
func firstPublickey(content []byte) (keyType, fingerprint string, ok bool) {
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "publickey ") {
			continue
		}
		kt, fp, err := parseAuthLine(line)
		if err != nil {
			return "", "", false
		}
		return kt, fp, true
	}
	return "", "", false
}

// clientIP, SSH_CONNECTION'ın ilk alanını döndürür.
//
// Biçim: "<istemci_ip> <istemci_port> <sunucu_ip> <sunucu_port>"
func clientIP(sshConnection string) string {
	fields := strings.Fields(sshConnection)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// parseAuthLine, kimlik dosyasının bir satırından anahtar türünü ve parmak
// izini çıkarır.
//
// Biçim: "publickey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA..."
//
// Parmak izi, ham anahtar blobunun SHA-256 özetinin dolgusuz base64
// kodlamasıdır — `ssh-keygen -lf` ile birebir aynı biçim, böylece kullanıcı
// denetim günlüğündeki değeri kendi anahtarıyla doğrudan karşılaştırabilir.
func parseAuthLine(line string) (keyType, fingerprint string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", errors.New("sshenv: unexpected auth line format")
	}
	if fields[0] != "publickey" {
		// Kadran yalnızca açık anahtarla girişe izin verir; başka bir
		// yöntem görülürse parmak izi üretilmez.
		return "", "", fmt.Errorf("sshenv: non-publickey method: %s", fields[0])
	}

	keyType = fields[1]
	blob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return "", "", fmt.Errorf("sshenv: could not parse the key: %w", err)
	}

	sum := sha256.Sum256(blob)
	return keyType, "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}
