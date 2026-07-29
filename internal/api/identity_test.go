package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"github.com/erkanrzgc/panely/internal/connproto"
	"github.com/erkanrzgc/panely/internal/peercred"
)

func ctxWithCaller(id connproto.Identity) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: CallerInfo{
			Unix:     peercred.Cred{PID: 42, UID: 1001, GID: 1002},
			Identity: id,
		},
	})
}

func TestActorFromContextExtractsAllFields(t *testing.T) {
	actor := actorFromContext(ctxWithCaller(connproto.Identity{
		Fingerprint: "SHA256:AAAABBBB",
		SourceIP:    "203.0.113.7",
		Label:       "erkan@laptop",
		Origin:      "ssh",
	}))

	if actor.KeyFingerprint != "SHA256:AAAABBBB" {
		t.Errorf("parmak izi = %q", actor.KeyFingerprint)
	}
	if actor.SourceIP != "203.0.113.7" {
		t.Errorf("kaynak IP = %q", actor.SourceIP)
	}
	if actor.Label != "erkan@laptop" {
		t.Errorf("etiket = %q", actor.Label)
	}
	if actor.Origin != "ssh" {
		t.Errorf("köken = %q", actor.Origin)
	}
}

// TestActorFromContextIgnoresGRPCMetadata, kimliğin metadata'dan
// OKUNMADIĞINI doğrular.
//
// Bu, düzeltilen gerçek bir güvenlik hatasının regresyon testidir.
// panely-connect bir bayt pompasıdır; gRPC metadata'sını yazan o değil,
// SSH'ın diğer ucundaki uzak istemcidir. Metadata'dan okunsaydı istemci
// kendi parmak izini uydurabilir ve denetim günlüğü "kim yaptı" alanında
// yalan söylerdi.
func TestActorFromContextIgnoresGRPCMetadata(t *testing.T) {
	// Bağlantı kimliği "gerçek" değerleri taşıyor.
	ctx := ctxWithCaller(connproto.Identity{
		Fingerprint: "SHA256:GERCEK",
		SourceIP:    "203.0.113.7",
		Origin:      "ssh",
	})

	// İstemci metadata ile başka biri gibi görünmeye çalışıyor.
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		"panely-key-fingerprint", "SHA256:SAHTE",
		"panely-source-ip", "198.51.100.99",
	))

	actor := actorFromContext(ctx)

	if actor.KeyFingerprint != "SHA256:GERCEK" {
		t.Errorf("metadata parmak izini ezdi: %q", actor.KeyFingerprint)
	}
	if actor.SourceIP != "203.0.113.7" {
		t.Errorf("metadata kaynak IP'yi ezdi: %q", actor.SourceIP)
	}
}

// TestActorFromContextDoesNotFabricate, kimlik bilgisi yokken değer
// UYDURULMADIĞINI doğrular.
func TestActorFromContextDoesNotFabricate(t *testing.T) {
	actor := actorFromContext(context.Background())

	if actor.KeyFingerprint != "" {
		t.Errorf("parmak izi uyduruldu: %q", actor.KeyFingerprint)
	}
	if actor.SourceIP != "" {
		t.Errorf("kaynak IP uyduruldu: %q", actor.SourceIP)
	}
	if actor.Origin != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", actor.Origin)
	}
}

func TestActorFromContextHandlesForeignAuthInfo(t *testing.T) {
	// Bağlantı bizim kimlik bilgimizle kurulmamış (örneğin testte
	// bellek içi dinleyici). Çökmemeli, "bilinmiyor" demeli.
	ctx := peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: peercred.AuthInfo{Cred: peercred.Cred{UID: 1000}},
	})

	if got := actorFromContext(ctx).Origin; got != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", got)
	}
}

func TestActorFromContextEmptyOriginBecomesUnknown(t *testing.T) {
	actor := actorFromContext(ctxWithCaller(connproto.Identity{
		Fingerprint: "SHA256:X",
	}))

	if actor.Origin != "unknown" {
		t.Errorf("köken = %q, beklenen \"unknown\"", actor.Origin)
	}
	// Diğer alanlar korunmalı.
	if actor.KeyFingerprint != "SHA256:X" {
		t.Errorf("parmak izi kayboldu: %q", actor.KeyFingerprint)
	}
}

func TestCallerFromContextReportsUnixCred(t *testing.T) {
	info, ok := callerFromContext(ctxWithCaller(connproto.Identity{Origin: "ssh"}))
	if !ok {
		t.Fatal("çağıran bilgisi bulunamadı")
	}
	if info.Unix.UID != 1001 || info.Unix.GID != 1002 || info.Unix.PID != 42 {
		t.Errorf("unix kimliği yanlış: %+v", info.Unix)
	}
	if info.AuthType() != "panely-caller" {
		t.Errorf("AuthType = %q", info.AuthType())
	}
}
