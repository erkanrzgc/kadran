package main

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// yalnizDagitim, dağıtım anahtarıyla bağlanmış bir sunucuyu taklit eder:
// GetApp'i K-131'deki gibi reddeder. Diğer yöntemler çağrılırsa panikler.
type yalnizDagitim struct{ kadranv1.KadranServiceClient }

func (yalnizDagitim) GetApp(context.Context, *kadranv1.GetAppRequest, ...grpc.CallOption) (*kadranv1.GetAppResponse, error) {
	return nil, status.Error(codes.PermissionDenied,
		"bu anahtar yalnızca dağıtım yapabilir (/kadran.v1.KadranService/GetApp)")
}

// TestDeployKeyWithoutCommitGetsAHint: dağıtım anahtarı uygulama tanımını
// okuyamaz, yani dalı çözemez. Çıplak bir "PermissionDenied" CI'da neyin
// yanlış olduğunu söylemez; ipucu çözümü söylemeli.
func TestDeployKeyWithoutCommitGetsAHint(t *testing.T) {
	c, _, _ := newTestCLI("")
	_, err := c.resolveCommit(context.Background(), yalnizDagitim{}, "web", "", "")
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	for _, s := range []string{"-commit", "GITHUB_SHA"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("hata %q içermiyor: %v", s, err)
		}
	}
}

// TestDeployKeyWithCommitSkipsGetApp: -commit verilince GetApp HİÇ
// çağrılmamalı; çağrılsaydı dağıtım anahtarı hiç çalışmazdı.
func TestDeployKeyWithCommitSkipsGetApp(t *testing.T) {
	c, _, _ := newTestCLI("")
	sha := strings.Repeat("a", 40)
	got, err := c.resolveCommit(context.Background(), yalnizDagitim{}, "web", sha, "")
	if err != nil || got != sha {
		t.Fatalf("resolveCommit = %q, %v; GetApp'e gitmeden %q dönmeliydi", got, err, sha)
	}
}
