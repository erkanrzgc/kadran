package client

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

// sahtePing, yalnızca Ping'i uygular; gömülü arayüz nil, başka bir RPC
// çağrılırsa test panikle düşer — CheckProtocol'ün başka bir şeye
// dokunmadığını da böylece doğruluyor.
type sahtePing struct {
	kadranv1.KadranServiceClient
	protokol uint32
	gelen    string
}

func (s *sahtePing) Ping(_ context.Context, in *kadranv1.PingRequest, _ ...grpc.CallOption) (*kadranv1.PingResponse, error) {
	s.gelen = in.GetClientVersion()
	return &kadranv1.PingResponse{ProtocolVersion: s.protokol}, nil
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

// eskiSunucu, servis adını tanımayan bir sunucuyu taklit eder: v0.4.0'dan
// önceki sunucular `panely.v1` konuşuyor; `kadran.v1` çağrısına gRPC
// `Unimplemented` (unknown service) dönüyorlar (K-136).
type eskiSunucu struct {
	kadranv1.KadranServiceClient
}

func (eskiSunucu) Ping(context.Context, *kadranv1.PingRequest, ...grpc.CallOption) (*kadranv1.PingResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unknown service kadran.v1.KadranService")
}

// TestCheckProtocolExplainsAnOldServer: ham gRPC hatası ("unknown service")
// kullanıcıya ne yapacağını söylemiyordu. Ad değişikliği (K-136) her eski
// kurulumu bu duruma düşürüyor; mesaj yükseltme yolunu göstermeli.
func TestCheckProtocolExplainsAnOldServer(t *testing.T) {
	_, err := (&Client{rpc: eskiSunucu{}}).CheckProtocol(context.Background())
	if err == nil {
		t.Fatal("servisi tanımayan sunucu kabul edildi")
	}
	for _, want := range []string{"v0.4.0", "bootstrap"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hata %q demiyor: %v", want, err)
		}
	}
}
