package console

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	args := []string{"app", "create", "-env", "A=1", "-env=B=2", "--env", "C=3", "--env=D", "web"}
	want := "app create -env A=*** -env=B=*** --env C=*** --env=*** web"
	got := Redact(args)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, secret := range []string{"1", "2", "3", "D"} {
		for _, tok := range strings.Fields(got) {
			if tok == secret || strings.HasSuffix(tok, "="+secret) {
				t.Errorf("değer %q sızdı: %q", secret, got)
			}
		}
	}
}

func TestRedactQuotesWordsWithSpacesOrQuotes(t *testing.T) {
	got := Redact([]string{"app", "create", "a b", `x"y`, ""})
	want := `app create "a b" "x\"y" ""`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactTrailingEnvFlagWithoutValue(t *testing.T) {
	if got := Redact([]string{"app", "create", "-env"}); got != "app create -env" {
		t.Errorf("got %q", got)
	}
}
