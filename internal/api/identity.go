package api

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/erkanrzgc/panely/internal/audit"
)

// Çağıranın kimliğini taşıyan gRPC metadata anahtarları.
//
// Bu değerleri `panely-connect` doldurur. panely-connect, istemcinin SSH
// oturumu İÇİNDE, zorlanmış komut olarak çalışır; dolayısıyla değerleri
// OpenSSH'ın kendi ortam değişkenlerinden alır ve istemci bunları
// uyduramaz:
//
//	SSH_CONNECTION    → kaynak IP (sshd tarafından ayarlanır)
//	SSH_AUTH_INFO_0   → kullanılan açık anahtar (sshd tarafından ayarlanır)
//
// SSH_AUTH_INFO_0 için sshd_config'de `ExposeAuthInfo yes` gerekir;
// bootstrap bunu ayarlar.
//
// # Bu metadata neden güvenilir?
//
// api.sock'a yalnızca panely-client grubundaki bir süreç ulaşabilir ve bu
// SO_PEERCRED ile doğrulanır. O grubun tek üyesi, zorlanmış komuta
// bağlanmış istemci SSH kullanıcısıdır — kabuğu yoktur, başka bir program
// çalıştıramaz. Yani bu metadata'yı yazabilen tek şey panely-connect'tir.
const (
	MDKeyFingerprint = "panely-key-fingerprint"
	MDKeySourceIP    = "panely-source-ip"
	MDKeyLabel       = "panely-key-label"
	MDKeyOrigin      = "panely-origin"
)

// actorFromContext, çağıranın kimliğini gRPC metadata'sından çıkarır.
//
// Metadata eksikse boş alanlarla döner; UYDURMAZ. Denetim kaydında boş bir
// parmak izi "kimliği bilinmiyor" demektir ve bu dürüst bir kayıttır —
// yer tutucu bir değer yazmak, sonradan bakan birini yanıltırdı.
func actorFromContext(ctx context.Context) audit.Actor {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return audit.Actor{Origin: "unknown"}
	}

	actor := audit.Actor{
		KeyFingerprint: firstValue(md, MDKeyFingerprint),
		SourceIP:       firstValue(md, MDKeySourceIP),
		Label:          firstValue(md, MDKeyLabel),
		Origin:         firstValue(md, MDKeyOrigin),
	}
	if actor.Origin == "" {
		actor.Origin = "unknown"
	}
	return actor
}

func firstValue(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
