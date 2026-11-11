package redact

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGoldenErrorShapeShowsClassAndReason(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "sidecar-error.json"))
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]struct {
		Message string `json:"message"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"connect", "call"} {
		g := golden[kind]
		if g.Data == "" {
			t.Fatalf("%s: altın dosyada data yok", kind)
		}
		if got := Text(g.Message, g.Data); got != g.Message+": "+g.Data {
			t.Errorf("%s: %q", kind, got)
		}
	}
}

func TestMessageOnlyAndPlainErrors(t *testing.T) {
	if got := Text("could not parse JSON", ""); got != "could not parse JSON" {
		t.Errorf("got %q", got)
	}
	if got := Error(errors.New("düz metin")); got != "düz metin" {
		t.Errorf("got %q", got)
	}
}

func TestRedaction(t *testing.T) {
	zwsp := string(rune(0x200b))
	for _, tc := range []struct{ name, in, want string }{
		{"URL kimlik bilgisi", "clone https://user:hunter2@git.example.com/org/repo.git failed", "clone https://***@git.example.com/org/repo.git failed"},
		{"yalnız kullanıcı adı", "ssh://deploy@host.example/x", "ssh://***@host.example/x"},
		{"x-access-token", "fatal: x-access-token:abc123@github.com denied", "fatal: x-access-token:***@github.com denied"},
		{"x-access-token URL içinde", "https://x-access-token:ghs_AAA111@github.com/o/r", "https://***@github.com/o/r"},
		{"ghp_", "token ghp_AbC123xyz rejected", "token *** rejected"},
		{"gho_", "gho_AbC123 bad", "*** bad"},
		{"ghu_", "ghu_AbC123 bad", "*** bad"},
		{"ghs_", "ghs_AbC123 bad", "*** bad"},
		{"ghr_", "ghr_AbC123 bad", "*** bad"},
		{"github_pat_", "pat github_pat_11ABCDEF_0123456789abcdef used", "pat *** used"},
		{"sıfır genişlikle bölünmüş jeton", "tok gh" + zwsp + "p_secretvalue99 x", "tok *** x"},
		{"büyük harfli şema", "HTTPS://u:p@h/x", "HTTPS://***@h/x"},
		{"sırsız metin", "connection refused at host:22", "connection refused at host:22"},
	} {
		got := Text("call failed", tc.in)
		if got != "call failed: "+tc.want {
			t.Errorf("%s: %q", tc.name, got)
		}
		if regexp.MustCompile(`hunter2|abc123|AbC123|secretvalue99|11ABCDEF`).MatchString(got) {
			t.Errorf("%s: sır kaldı: %q", tc.name, got)
		}
	}
}

func TestControlAndBidiStripped(t *testing.T) {
	for _, r := range []rune{0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069, 0x200e, 0x200f, 0x061c, 0x200b, 0xfeff} {
		if got := Text("call failed", "a"+string(r)+"b"); got != "call failed: ab" {
			t.Errorf("U+%04X: %q", r, got)
		}
	}
	if got := Text("call failed", "a\x00b\x1bc\x7fd"); got != "call failed: abcd" {
		t.Errorf("kontrol: %q", got)
	}
	if got := Text("call failed", "line1\nline2\r\n\tline3"); got != "call failed: line1 line2 line3" {
		t.Errorf("satır sonu: %q", got)
	}
}

func TestLengthLimitInCodePoints(t *testing.T) {
	exact := strings.Repeat("x", MaxErrorChars-len("call failed: "))
	if got := Text("call failed", exact); utf8.RuneCountInString(got) != MaxErrorChars || strings.HasSuffix(got, "…") {
		t.Errorf("tam sınır: %d", utf8.RuneCountInString(got))
	}
	long := Text("call failed", strings.Repeat("y", 5000))
	if utf8.RuneCountInString(long) != MaxErrorChars+1 || !strings.HasSuffix(long, "…") {
		t.Errorf("uzun: %d", utf8.RuneCountInString(long))
	}
	emoji := Text("call failed", strings.Repeat("😀", 1000))
	if utf8.RuneCountInString(emoji) != MaxErrorChars+1 || !utf8.ValidString(emoji) {
		t.Errorf("emoji bölündü")
	}
}
