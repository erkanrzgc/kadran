package api

import (
	"context"

	"google.golang.org/grpc/peer"

	"github.com/erkanrzgc/panely/internal/audit"
)

// callerFromContext, bağlantıya bağlı doğrulanmış çağıran bilgisini çıkarır.
func callerFromContext(ctx context.Context) (CallerInfo, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return CallerInfo{}, false
	}
	info, ok := p.AuthInfo.(CallerInfo)
	return info, ok
}

// actorFromContext, denetim kaydı için aktörü üretir.
//
// # Kimlik neden gRPC metadata'sından OKUNMUYOR?
//
// İlk tasarımda öyleydi ve delik bir tasarımdı. `panely-connect` bir bayt
// pompasıdır: gRPC metadata'sını yazan o değil, SSH'ın diğer ucundaki UZAK
// İSTEMCİDİR. İstemci kendi parmak izini istediği gibi uydurabilir ve
// denetim günlüğü "kim yaptı" alanında yalan söylerdi — hem de tam olarak
// inkâr edilemezlik için tutulan kayıtta.
//
// Şimdi kimlik, bağlantı kurulurken okunan önsözden geliyor
// (bkz. internal/connproto). Önsözü panely-connect yazar, sshd'nin kendi
// ortam değişkenlerinden türeterek; uzak istemcinin baytları ondan sonra
// geldiği için onu değiştiremez.
//
// Kimlik yoksa UYDURULMAZ. Denetim izinde boş bir parmak izi "bilinmiyor"
// demektir ve bu dürüst bir kayıttır; yer tutucu bir değer, sonradan bakan
// birini gerçek bir kimlik gördüğüne inandırırdı.
func actorFromContext(ctx context.Context) audit.Actor {
	info, ok := callerFromContext(ctx)
	if !ok {
		return audit.Actor{Origin: "unknown"}
	}

	actor := audit.Actor{
		KeyFingerprint: info.Identity.Fingerprint,
		SourceIP:       info.Identity.SourceIP,
		Label:          info.Identity.Label,
		Origin:         info.Identity.Origin,
	}
	if actor.Origin == "" {
		actor.Origin = "unknown"
	}
	return actor
}
