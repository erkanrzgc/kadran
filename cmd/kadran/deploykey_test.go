package main

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/kadran/internal/pb/panely/v1"
)

// yalnizDagitim, dağıtım anahtarıyla bağlanmış bir sunucuyu taklit eder:
// GetApp'i K-131'deki gibi reddeder. Diğer yöntemler çağrılırsa panikler.
type yalnizDagitim struct{ panelyv1.PanelyServiceClient }

func (yalnizDagitim) GetApp(context.Context, *panelyv1.GetAppRequest, ...grpc.CallOption) (*panelyv1.GetAppResponse, error) {
	return nil, status.Error(codes.PermissionDenied,
		"bu anahtar yalnızca dağıtım yapabilir (/panely.v1.PanelyService/GetApp)")
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
