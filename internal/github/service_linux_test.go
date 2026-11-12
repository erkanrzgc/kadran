//go:build linux

package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Durum dizini taranamazsa (üst öğe bir dosya) servis yine kurulur: Status FAILED/INTERNAL,
// Decide NotConnected; daemon çalışmaya devam eder.
func TestNewSurvivesUnusableStateDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{ClientID: testClientID, AppSlug: "kadran-deploy", StateFile: filepath.Join(file, "state.json")})
	if err != nil {
		t.Fatalf("New = %v, want a usable Service", err)
	}
	defer s.Close()
	c, _ := s.Status(context.Background())
	if c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL {
		t.Fatalf("status = %v", c)
	}
	if d, err := s.Decide(context.Background(), "octo", "priv"); d != DecisionNotConnected || err != nil {
		t.Fatalf("Decide = %v, %v", d, err)
	}
}
