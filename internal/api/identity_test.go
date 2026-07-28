package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func ctxWithMD(pairs ...string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs(pairs...))
}

func TestActorFromContextExtractsAllFields(t *testing.T) {
	ctx := ctxWithMD(
		MDKeyFingerprint, "SHA256:AAAABBBB",
		MDKeySourceIP, "203.0.113.7",
		MDKeyLabel, "erkan@laptop",
		MDKeyOrigin, "cli",
	)

	actor := actorFromContext(ctx)

	if actor.KeyFingerprint != "SHA256:AAAABBBB" {
		t.Errorf("parmak izi = %q", actor.KeyFingerprint)
	}
	if actor.SourceIP != "203.0.113.7" {
		t.Errorf("kaynak IP = %q", actor.SourceIP)
	}
	if actor.Label != "erkan@laptop" {
		t.Errorf("etiket = %q", actor.Label)
	}
	if actor.Origin != "cli" {
		t.Errorf("köken = %q", actor.Origin)
	}
}

// TestActorFromContextDoesNotFabricate, metadata eksikken değer
// UYDURULMADIĞINI doğrular.
//
// Denetim kaydında boş bir parmak izi "kimliği bilinmiyor" demektir ve bu
// dürüst bir kayıttır. Yer tutucu bir değer ("system", "local") yazmak,
// sonradan denetim izine bakan birini gerçek bir kimlik gördüğüne
// inandırırdı.
func TestActorFromContextDoesNotFabricate(t *testing.T) {
	actor := actorFromContext(context.Background())

	if actor.KeyFingerprint != "" {
		t.Errorf("parmak izi uyduruldu: %q", actor.KeyFingerprint)
	}
	if actor.SourceIP != "" {
		t.Errorf("kaynak IP uyduruldu: %q", actor.SourceIP)
	}
	if actor.Label != "" {
		t.Errorf("etiket uyduruldu: %q", actor.Label)
	}
	// Köken tek istisna: "bilinmiyor" olduğunu açıkça söylemek, boş
	// bırakıp belirsizlik yaratmaktan iyidir.
	if actor.Origin != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", actor.Origin)
	}
}

func TestActorFromContextHandlesPartialMetadata(t *testing.T) {
	ctx := ctxWithMD(MDKeySourceIP, "198.51.100.4")

	actor := actorFromContext(ctx)

	if actor.SourceIP != "198.51.100.4" {
		t.Errorf("kaynak IP = %q", actor.SourceIP)
	}
	if actor.KeyFingerprint != "" {
		t.Errorf("eksik parmak izi dolduruldu: %q", actor.KeyFingerprint)
	}
	if actor.Origin != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", actor.Origin)
	}
}

func TestActorFromContextTrimsWhitespace(t *testing.T) {
	// panely-connect değerleri SSH ortam değişkenlerinden alır; oradan
	// gelen satır sonu veya boşluk parmak izini bozmamalı.
	ctx := ctxWithMD(MDKeyFingerprint, "  SHA256:XYZ\n")

	if got := actorFromContext(ctx).KeyFingerprint; got != "SHA256:XYZ" {
		t.Errorf("parmak izi = %q, boşluklar temizlenmemiş", got)
	}
}

func TestActorFromContextEmptyMetadataIsUnknown(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})

	if got := actorFromContext(ctx).Origin; got != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", got)
	}
}
