package client

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/grpc"

	panelyv1 "github.com/erkanrzgc/kadran/internal/pb/panely/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

// sahtePing, yalnızca Ping'i uygular; gömülü arayüz nil, başka bir RPC
// çağrılırsa test panikle düşer — CheckProtocol'ün başka bir şeye
// dokunmadığını da böylece doğruluyor.
type sahtePing struct {
	panelyv1.PanelyServiceClient
	protokol uint32
	gelen    string
}

func (s *sahtePing) Ping(_ context.Context, in *panelyv1.PingRequest, _ ...grpc.CallOption) (*panelyv1.PingResponse, error) {
	s.gelen = in.GetClientVersion()
	return &panelyv1.PingResponse{ProtocolVersion: s.protokol}, nil
}

// TestCheckProtocolRejectsAMismatch: her CLI bağlantısı bu kontrolden
// geçiyor, ama uçtan uca test yalnızca "sürümler aynı" yolunu
// çalıştırıyordu (K-114). Uyumsuz sözleşmeyle konuşmak sessizce yanlış
// davranmaktır: yeni bir alanı eski sunucu boş bırakır ve CLI onu
// "yok" diye basar.
func TestCheckProtocolRejectsAMismatch(t *testing.T) {
	for _, sunucu := range []uint32{version.Protocol + 1, version.Protocol - 1} {
		c := &Client{rpc: &sahtePing{protokol: sunucu}}
		_, err := c.CheckProtocol(context.Background())
		if err == nil {
			t.Fatalf("sunucu protokolü %d kabul edildi (istemci %d)", sunucu, version.Protocol)
		}
		if !strings.Contains(err.Error(), strconv.FormatUint(uint64(sunucu), 10)) {
			t.Errorf("hata iki sürümü de söylemiyor: %v", err)
		}
	}
}

func TestCheckProtocolAcceptsTheSameVersion(t *testing.T) {
	s := &sahtePing{protokol: version.Protocol}
	resp, err := (&Client{rpc: s}).CheckProtocol(context.Background())
	if err != nil || resp.GetProtocolVersion() != version.Protocol {
		t.Fatalf("aynı protokol reddedildi: %v", err)
	}
	if s.gelen != version.Version {
		t.Errorf("istemci sürümünü göndermedi: %q", s.gelen)
	}
}
