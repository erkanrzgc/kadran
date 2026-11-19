package console

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/cli"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name   string
		in     []string
		server string
		want   []string
		err    error
	}{
		{"kadran atılır, -s eklenir", []string{"kadran", "deploy", "web"}, "hetzner", []string{"deploy", "web", "-s", "hetzner"}, nil},
		{"kendi -s taşınır", []string{"deploy", "-s", "hetzner", "web"}, "hetzner", []string{"deploy", "web", "-s", "hetzner"}, nil},
		{"-s= biçimi", []string{"deploy", "-s=hetzner", "web"}, "hetzner", []string{"deploy", "web", "-s", "hetzner"}, nil},
		{"--s biçimi", []string{"deploy", "--s", "hetzner", "web"}, "hetzner", []string{"deploy", "web", "-s", "hetzner"}, nil},
		{"--s= biçimi", []string{"deploy", "--s=hetzner", "web"}, "hetzner", []string{"deploy", "web", "-s", "hetzner"}, nil},
		{"başka sunucu", []string{"deploy", "-s=gcp", "web"}, "hetzner", nil, ErrOtherServer},
		{"başka sunucu ayrık", []string{"deploy", "-s", "gcp", "web"}, "hetzner", nil, ErrOtherServer},
		{"değersiz -s", []string{"deploy", "web", "-s"}, "hetzner", nil, ErrOtherServer},
		{"hedefsiz komut", []string{"server", "list"}, "hetzner", []string{"server", "list"}, nil},
		{"yalnız ilk kadran", []string{"kadran", "kadran", "status"}, "hetzner", []string{"kadran", "status"}, nil},
		{"bilinmeyen komut", []string{"nope", "x"}, "hetzner", []string{"nope", "x"}, nil},
		{"sunucu boş", []string{"status"}, "", []string{"status"}, nil},
		{"boş satır", nil, "hetzner", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := append([]string(nil), c.in...)
			got, err := Normalize(c.in, c.server)
			if !errors.Is(err, c.err) {
				t.Fatalf("hata = %v, istenen %v", err, c.err)
			}
			if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
				t.Errorf("got %q, want %q", got, c.want)
			}
			if len(in) != len(c.in) || (len(in) > 0 && !reflect.DeepEqual(in, c.in)) {
				t.Errorf("girdi değişti: %q", c.in)
			}
		})
	}
}

func TestOtherServerErrorPointsToUse(t *testing.T) {
	_, err := Normalize([]string{"deploy", "-s", "gcp", "web"}, "hetzner")
	if !errors.Is(err, ErrOtherServer) || !strings.Contains(err.Error(), "use <name>") {
		t.Errorf("hata use <name> göstermiyor: %v", err)
	}
}

// TestNormalizeAcceptsLoginHint: oturum ipucunun biçimi
// internal/cli/serverflag_test.go:294 ve serverhints_test.go:102 içinde
// sabitlenmiş ("kadran login -s hetzner"); konsol bu satırı olduğu gibi kabul
// etmeli.
func TestNormalizeAcceptsLoginHint(t *testing.T) {
	words, err := ParseLine("kadran login -s hetzner")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(words, "hetzner")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"login", "-s", "hetzner"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRejectTarget(t *testing.T) {
	cases := []struct {
		name string
		args []string
		err  error
	}{
		{"ssh hedefi", []string{"deploy", "web", "root@x"}, ErrTarget},
		{"soket yolu", []string{"status", "/run/k.sock"}, ErrTarget},
		{"temiz", []string{"deploy", "web"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv, err := cli.Inspect(c.args)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if got := RejectTarget(inv); !errors.Is(got, c.err) {
				t.Errorf("RejectTarget = %v, istenen %v", got, c.err)
			}
		})
	}
}
