// Package sshenv, sshd'nin oturuma verdiği ortam değişkenlerinden çağıranın
// kimliğini türetir.
//
// # Neden ortam değişkenleri güvenilir?
//
// Bu değişkenleri sshd, kimlik doğrulaması TAMAMLANDIKTAN SONRA ayarlar.
// Uzak istemcinin bunlara karışabileceği İKİ yol vardır ve ikisi de ayrı
// ayrı kapatılmıştır:
//
//  1. authorized_keys'teki `environment="AD=deger"` seçeneği. sshd(8):
//     "Environment variables set this way override other default
//     environment values" — yani açık olsaydı sshd'nin KENDİ yazdığı
//     SSH_AUTH_INFO_0'ı EZERDİ. Bunu kapatan `PermitUserEnvironment no`'dur.
//  2. İstemcinin `SendEnv` ile gönderdiği değişkenler. Bunları `AcceptEnv`
//     süzer ve varsayılanı "hiçbirini kabul etme"dir. Eklemeli çalıştığı
//     için boş bir değerle sıfırlanamaz; bu yüzden bootstrap bilerek hiç
//     AcceptEnv satırı yazmaz.
//
// DİKKAT — burada kolay bir yanlış var: bunları kapatan şey `restrict`
// DEĞİLDİR. sshd(8), restrict'i "disable port, agent and X11 forwarding,
// as well as disabling PTY allocation and execution of ~/.ssh/rc" diye
// tanımlar; ortam işlemesi bu listede YOKTUR. Panely bir zamanlar bunun
// tersini iddia eden bir yorum taşıyordu (docs/decisions.md K-031).
//
// `PermitUserEnvironment no`, bootstrap'ın sshd drop-in'inde GENEL kapsamda
// açıkça yazılır — `Match` bloğunun içinde değil, çünkü o anahtar kelime
// Match içinde geçerli değildir ve oraya konması sshd yapılandırmasını
// bozar. Varsayılanı zaten `no` olsa da yazılır: bu değer denetim izinin
// doğruluğunu taşır ve dağıtımın genel yapılandırmasına bırakılamaz.
//
// Yani `panely-connect` çalıştığında ortamındaki SSH_* değişkenleri sshd'nin
// kendi gözlemidir — istemcinin iddiası değil.
package sshenv

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Identity, sshd'nin bildirdiği bağlantı kimliğidir.
type Identity struct {
	// Fingerprint, "SHA256:..." biçiminde açık anahtar parmak izi.
	// SSH_AUTH_INFO_0 yoksa boş kalır.
	Fingerprint string

	// SourceIP, istemcinin IP adresi.
	SourceIP string

	// KeyType, "ssh-ed25519" gibi anahtar türü.
	KeyType string
}

// ErrNoConnectionInfo, SSH_CONNECTION bulunamadığında döner.
var ErrNoConnectionInfo = errors.New("sshenv: SSH_CONNECTION tanımlı değil")

// Parse, verilen ortam okuyucusundan kimliği çıkarır.
//
// getenv parametresi test edilebilirlik içindir; üretimde os.Getenv geçilir.
//
// SSH_AUTH_INFO_0 eksikse hata DÖNMEZ, yalnızca parmak izi boş kalır.
// Bu değişken sshd_config'de `ExposeAuthInfo yes` gerektirir ve eksikliği
// bağlantıyı reddetmek için bir sebep değildir — denetim kaydı sadece daha
// az bilgi taşır. Boş parmak izi "bilinmiyor" demektir ve bu dürüsttür.
func Parse(getenv func(string) string) (Identity, error) {
	conn := getenv("SSH_CONNECTION")
	if conn == "" {
		return Identity{}, ErrNoConnectionInfo
	}

	id := Identity{SourceIP: clientIP(conn)}

	if authInfo := getenv("SSH_AUTH_INFO_0"); authInfo != "" {
		keyType, fingerprint, err := parseAuthInfo(authInfo)
		if err == nil {
			id.KeyType = keyType
			id.Fingerprint = fingerprint
		}
	}
	return id, nil
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

// parseAuthInfo, SSH_AUTH_INFO_0 satırından anahtar türünü ve parmak izini
// çıkarır.
//
// Biçim: "publickey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA..."
//
// Parmak izi, ham anahtar blobunun SHA-256 özetinin dolgusuz base64
// kodlamasıdır — `ssh-keygen -lf` ile birebir aynı biçim, böylece kullanıcı
// denetim günlüğündeki değeri kendi anahtarıyla doğrudan karşılaştırabilir.
func parseAuthInfo(line string) (keyType, fingerprint string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", fmt.Errorf("sshenv: beklenmedik SSH_AUTH_INFO biçimi")
	}
	if fields[0] != "publickey" {
		// Panely yalnızca açık anahtarla girişe izin verir; başka bir
		// yöntem görülürse parmak izi üretilmez.
		return "", "", fmt.Errorf("sshenv: açık anahtar dışı yöntem: %s", fields[0])
	}

	keyType = fields[1]
	blob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return "", "", fmt.Errorf("sshenv: anahtar çözümlenemedi: %w", err)
	}

	sum := sha256.Sum256(blob)
	return keyType, "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}
