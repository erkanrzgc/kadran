package console

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
		err  error
	}{
		{"düz", "deploy web", []string{"deploy", "web"}, nil},
		{"çift tırnak", `app create "a b"`, []string{"app", "create", "a b"}, nil},
		{"tek tırnak", `'x"y'`, []string{`x"y`}, nil},
		{"boru", "deploy web | cat", nil, ErrShell},
		{"tırnaklı boru", `"a|b"`, []string{"a|b"}, nil},
		{"komut yerine koyma", "$(x)", nil, ErrShell},
		{"ters tırnak", "a `x`", nil, ErrShell},
		{"yönlendirme", "a > f", nil, ErrShell},
		{"noktalı virgül", "a; b", nil, ErrShell},
		{"arka plan", "a &", nil, ErrShell},
		{"çok uzun", strings.Repeat("a", MaxLineBytes+1), nil, ErrTooLong},
		{"sınırda", strings.Repeat("a", MaxLineBytes), []string{strings.Repeat("a", MaxLineBytes)}, nil},
		{"bidi", "\u202e", nil, ErrControl},
		{"sıfır genişlik", "a\u200bb", nil, ErrControl},
		{"kapanmayan tırnak", `"abc`, nil, ErrQuote},
		{"boş", "   ", nil, nil},
		{"boş tırnak", `a ""`, []string{"a", ""}, nil},
		{"tırnak bitişik", `a"b c"d`, []string{"ab cd"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseLine(c.in)
			if !errors.Is(err, c.err) {
				t.Fatalf("hata = %v, istenen %v", err, c.err)
			}
			if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseLineRejectsPastedNewline(t *testing.T) {
	_, err := ParseLine("deploy web\nprune -all")
	if !errors.Is(err, ErrControl) {
		t.Fatalf("hata = %v, istenen ErrControl", err)
	}
}
