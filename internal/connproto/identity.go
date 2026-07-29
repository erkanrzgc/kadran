// Package connproto, api.sock üzerindeki bağlantı önsözünü (preamble)
// tanımlar.
//
// # Bu önsöz neden var?
//
// Denetim günlüğündeki "kim yaptı" alanının kaynağı budur ve doğru yerden
// gelmesi kritiktir.
//
// İlk (yanlış) tasarımda çağıranın kimliği gRPC metadata'sından okunuyordu.
// Bu delik bir tasarımdı: `panely-connect` bir bayt pompasıdır, gRPC
// metadata'sını yazan o değil, SSH'ın diğer ucundaki UZAK İSTEMCİDİR.
// İstemci kendi parmak izini istediği gibi uydurabilir ve denetim günlüğü
// "kim yaptı" alanında yalan söylerdi.
//
// Önsöz bu sorunu şöyle çözer:
//
//  1. panely-connect, api.sock'a bağlanır bağlanmaz — daha uzak istemciden
//     tek bayt okumadan — kimliği yazar.
//  2. Kimliği sshd'nin kendi ortam değişkenlerinden alır (SSH_CONNECTION,
//     SSH_AUTH_INFO_0). Bunları sshd ayarlar; istemci ayarlayamaz.
//  3. Ancak ondan sonra bayt pompası başlar.
//
// Uzak istemcinin baytları önsözden SONRA geldiği için kimliği geriye
// dönük değiştiremez. Kimlik, bağlantının kendisine bağlıdır.
//
// # Biçim
//
//	[4 bayt big-endian uzunluk][JSON gövde]
//
// Uzunluk öneki, gRPC'nin HTTP/2 akışının nerede başladığını kesin olarak
// belirler: okuyucu tam olarak o kadar bayt tüketir, fazlasına dokunmaz.
package connproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// MaxPreambleSize, önsözün kabul edilen en büyük boyutudur.
//
// Kötü niyetli veya bozuk bir çağıranın devasa bir uzunluk bildirip
// belleği tüketmesini engeller. Gerçek önsöz birkaç yüz bayttır.
const MaxPreambleSize = 4096

// ErrPreambleTooLarge, bildirilen uzunluk sınırı aştığında döner.
var ErrPreambleTooLarge = errors.New("connproto: önsöz çok büyük")

// Identity, bağlantıyı açan çağıranın doğrulanmış kimliğidir.
//
// Alanlar sshd tarafından sağlanan bilgilerden türetilir; hiçbiri uzak
// istemci tarafından belirlenemez.
type Identity struct {
	// Fingerprint, kimlik doğrulamada kullanılan SSH açık anahtarının
	// parmak izi ("SHA256:..."). SSH_AUTH_INFO_0'dan türetilir.
	Fingerprint string `json:"fp,omitempty"`

	// SourceIP, bağlantının geldiği IP. SSH_CONNECTION'dan türetilir.
	SourceIP string `json:"ip,omitempty"`

	// Label, authorized_keys satırındaki yorum alanı (varsa).
	Label string `json:"label,omitempty"`

	// Origin, bağlantıyı kuran bileşen: "ssh", "local".
	Origin string `json:"origin"`
}

// Write, kimliği uzunluk önekli olarak yazar.
func Write(w io.Writer, id Identity) error {
	body, err := json.Marshal(id)
	if err != nil {
		return fmt.Errorf("connproto: kimlik kodlanamadı: %w", err)
	}
	if len(body) > MaxPreambleSize {
		return ErrPreambleTooLarge
	}

	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))

	// Başlık ve gövde tek yazmada gider: iki ayrı Write, araya başka bir
	// yazıcı girerse bağlantıyı bozabilirdi.
	packet := make([]byte, 0, 4+len(body))
	packet = append(packet, header[:]...)
	packet = append(packet, body...)

	if _, err := w.Write(packet); err != nil {
		return fmt.Errorf("connproto: önsöz yazılamadı: %w", err)
	}
	return nil
}

// Read, önsözü okur ve TAM OLARAK önsöz kadar bayt tüketir.
//
// Kalan baytlara dokunulmaz; onlar gRPC'nin HTTP/2 akışıdır.
func Read(r io.Reader) (Identity, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Identity{}, fmt.Errorf("connproto: önsöz başlığı okunamadı: %w", err)
	}

	size := binary.BigEndian.Uint32(header[:])
	if size > MaxPreambleSize {
		return Identity{}, fmt.Errorf("%w: %d bayt bildirildi, sınır %d",
			ErrPreambleTooLarge, size, MaxPreambleSize)
	}
	if size == 0 {
		return Identity{}, errors.New("connproto: boş önsöz")
	}

	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return Identity{}, fmt.Errorf("connproto: önsöz gövdesi okunamadı: %w", err)
	}

	var id Identity
	if err := json.Unmarshal(body, &id); err != nil {
		return Identity{}, fmt.Errorf("connproto: önsöz çözümlenemedi: %w", err)
	}
	if id.Origin == "" {
		id.Origin = "unknown"
	}
	return id, nil
}
