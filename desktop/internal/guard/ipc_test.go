package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func mustFail(t *testing.T, err error, want, what string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("%s: hata %v, beklenen %q", what, err, want)
	}
}

func TestTargetEmptyLongAndTrimmed(t *testing.T) {
	for _, in := range []string{"", "   "} {
		_, err := Target(in)
		mustFail(t, err, "target is empty", in)
	}
	_, err := Target(strings.Repeat("a", 501))
	mustFail(t, err, "target is too long", "501")
	if got, err := Target(strings.Repeat("a", 500)); err != nil || len(got) != 500 {
		t.Errorf("500 karakter reddedildi: %v", err)
	}
	if got, err := Target("  kadran-client@host  "); err != nil || got != "kadran-client@host" {
		t.Errorf("kırpma: %q %v", got, err)
	}
}

func TestTargetLeadingDashRejectedAfterTrim(t *testing.T) {
	for _, in := range []string{"-oProxyCommand=x", "  -x@host"} {
		_, err := Target(in)
		mustFail(t, err, "must not start with", in)
	}
	for _, in := range []string{"user@host-1", "a-b@c"} {
		if got, err := Target(in); err != nil || got != in {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
}

func TestTargetControlAndFormatCharacters(t *testing.T) {
	for _, in := range []string{"a\nb@host", "a\u0000b", "a\u202ec@host", "a\u200bb", "a\u2066b", "a\u0085b", "a\u007fb"} {
		_, err := Target(in)
		mustFail(t, err, "control or invisible", in)
	}
	if got, err := Target("  user@host\n"); err != nil || got != "user@host" {
		t.Errorf("kırpılan uç: %q %v", got, err)
	}
	if got, err := Target("kadran-client@ünlü.example"); err != nil || got != "kadran-client@ünlü.example" {
		t.Errorf("ünlü: %q %v", got, err)
	}
	// JavaScript'in trim'i U+0085'i atmaz: uçta da reddedilir.
	_, err := Target("user@host\u0085")
	mustFail(t, err, "control or invisible", "uçta U+0085")
}

func TestTargetLengthCountsUTF16Units(t *testing.T) {
	// "😀" JavaScript'te iki birim: 250 tanesi 500, 251 tanesi 502.
	if _, err := Target(strings.Repeat("😀", 250)); err != nil {
		t.Errorf("500 birim reddedildi: %v", err)
	}
	_, err := Target(strings.Repeat("😀", 251))
	mustFail(t, err, "target is too long", "502 birim")
}

func TestUintLenient(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want uint64
	}{
		{`-1`, 7}, {`1e400`, 7}, {`"x"`, 7}, {`"12"`, 12}, {`3.9`, 3}, {`{}`, 7}, {`null`, 0}, {`9007199254740992`, 7}, {`9007199254740991`, 9007199254740991},
	} {
		if got := Uint(json.RawMessage(tc.raw), 7); got != tc.want {
			t.Errorf("Uint(%s) = %d, beklenen %d", tc.raw, got, tc.want)
		}
	}
}

func TestWindowActionWhitelist(t *testing.T) {
	for _, a := range []string{"minimize", "maximize", "close"} {
		if got, err := WindowAction(a); err != nil || got != a {
			t.Errorf("%s: %v", a, err)
		}
	}
	for _, a := range []string{"devtools", ""} {
		_, err := WindowAction(a)
		mustFail(t, err, "unknown window action", a)
	}
}

func TestCopyTextBounds(t *testing.T) {
	if got, err := CopyText("SHA256:abc"); err != nil || got != "SHA256:abc" {
		t.Errorf("geçerli metin: %v", err)
	}
	if _, err := CopyText(strings.Repeat("x", MaxCopy)); err != nil {
		t.Errorf("1024 reddedildi: %v", err)
	}
	for _, in := range []string{"", strings.Repeat("x", MaxCopy+1)} {
		_, err := CopyText(in)
		mustFail(t, err, "1-1024", "uzunluk")
	}
}

func TestAppIDConfirmationRefTail(t *testing.T) {
	for _, id := range []string{"", "../other", "-other", "App", strings.Repeat("a", 33)} {
		if _, err := AppID(id); err == nil {
			t.Errorf("%q kabul edildi", id)
		}
	}
	if got, err := AppID("blog-2"); err != nil || got != "blog-2" {
		t.Errorf("blog-2: %v", err)
	}
	if err := Confirmation("blog", "BLOG"); err == nil {
		t.Error("büyük harfli onay kabul edildi")
	}
	if err := Confirmation("blog", "blog"); err != nil {
		t.Errorf("doğru onay: %v", err)
	}
	if _, err := Ref("x\ncmd"); err == nil {
		t.Error("satır sonlu ref kabul edildi")
	}
	if got, err := Ref(""); err != nil || got != "" {
		t.Errorf("boş ref: %v", err)
	}
	for _, n := range []int{0, 10001} {
		if _, err := Tail(n); err == nil {
			t.Errorf("tail %d kabul edildi", n)
		}
	}
	if got, err := Tail(100); err != nil || got != 100 {
		t.Errorf("tail 100: %v", err)
	}
}

func TestServerNameAndProvider(t *testing.T) {
	for _, in := range []string{"", "  "} {
		_, err := ServerName(in)
		mustFail(t, err, "server name is empty", in)
	}
	_, err := ServerName(strings.Repeat("a", MaxName+1))
	mustFail(t, err, "server name is too long", "201")
	if got, err := ServerName(" hetzner "); err != nil || got != " hetzner " {
		t.Errorf("ad kırpılmadan gitmeli (kural Go'da): %q %v", got, err)
	}
	if _, err := Provider(strings.Repeat("ğ", MaxProvider)); err != nil {
		t.Errorf("64 kod noktası reddedildi: %v", err)
	}
	_, err = Provider(strings.Repeat("ğ", MaxProvider+1))
	mustFail(t, err, "provider is too long", "65")
}
