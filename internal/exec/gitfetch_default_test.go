package exec

import (
	"testing"
	"time"
)

func TestDecisionTimeoutDefaultIsFiveSeconds(t *testing.T) {
	g, ok := NewGitFetcher("/x").(*gitFetcher)
	if !ok || g.decisionTimeout != 5*time.Second || gitDecisionTimeout != 5*time.Second {
		t.Fatalf("decision timeout is not 5s: %+v", g)
	}
	if defaultGitFetchSocket != "/run/kadran-git/fetch.sock" {
		t.Fatal("wrong default socket")
	}
}
