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

// -build-arg değerleri de -env gibi arıtılır: Go'nun flag paketinin kabul
// ettiği her yazımda (tek/çift tire, ayrı ya da "=" ile, tekrarlı).
func TestRedactBuildArg(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"app", "create", "-build-arg", "NPM_TOKEN=s3cr3t", "web"}, "app create -build-arg NPM_TOKEN=*** web"},
		{[]string{"app", "create", "-build-arg=NPM_TOKEN=s3cr3t", "web"}, "app create -build-arg=NPM_TOKEN=*** web"},
		{[]string{"app", "create", "--build-arg", "NPM_TOKEN=s3cr3t", "web"}, "app create --build-arg NPM_TOKEN=*** web"},
		{[]string{"app", "create", "--build-arg=NPM_TOKEN=s3cr3t", "web"}, "app create --build-arg=NPM_TOKEN=*** web"},
		{[]string{"app", "create", "-build-arg", "A=s3cr3t", "--build-arg=B=s3cr3t", "-env", "C=s3cr3t", "web"}, "app create -build-arg A=*** --build-arg=B=*** -env C=*** web"},
		{[]string{"app", "create", "-build-arg", "s3cr3t", "web"}, "app create -build-arg *** web"},
		{[]string{"app", "create", "-build-arg=s3cr3t", "web"}, "app create -build-arg=*** web"},
	}
	for _, c := range cases {
		got := Redact(c.args)
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.args, got, c.want)
		}
		if strings.Contains(got, "s3cr3t") {
			t.Errorf("%q: değer sızdı: %q", c.args, got)
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

// login ve totp alt komutlarına yazılan konumsal değerler (kod, kurtarma
// kodu) -env değeri gibi arıtılır; olağan komutlar değişmez.
func TestRedactLoginAndTotpPositionals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"login", "123456"}, "login ***"},
		{[]string{"login", "-recovery", "ABCD-EFGH"}, "login -recovery ***"},
		{[]string{"login", "-recovery=ABCD-EFGH"}, "login -recovery=***"},
		{[]string{"kadran", "login", "123456"}, "kadran login ***"},
		{[]string{"totp", "disable", "123456"}, "totp disable ***"},
		{[]string{"totp", "enable", "123456"}, "totp enable ***"},
		{[]string{"login"}, "login"},
		{[]string{"app", "list"}, "app list"},
		{[]string{"deploy", "web"}, "deploy web"},
		{[]string{"app", "create", "-env", "A=1", "web"}, "app create -env A=*** web"},
	}
	for _, c := range cases {
		if got := Redact(c.args); got != c.want {
			t.Errorf("%q: got %q, want %q", c.args, got, c.want)
		}
	}
}
