package servers

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	got, err := NormalizeName("  hetzner ")
	if err != nil || got != "hetzner" {
		t.Fatalf("NormalizeName kırpmadı: %q, %v", got, err)
	}
	for _, ok := range []string{strings.Repeat("ş", 64), "gcp (2)", "Hetzner Almanya"} {
		if _, err := NormalizeName(ok); err != nil {
			t.Errorf("%q reddedildi: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", "-x", " -x", "a\tb", "a\nb", strings.Repeat("ş", 65)} {
		if _, err := NormalizeName(bad); err == nil {
			t.Errorf("%q kabul edildi", bad)
		}
	}
}

func TestValidateEntry(t *testing.T) {
	good := Server{Name: "hetzner", Target: "kadran-client@203.0.113.10"}
	for _, tc := range []struct {
		name string
		srv  Server
		want string // hata metninde geçmeli
	}{
		{"seçenek gibi kullanıcı", Server{Name: "a", Target: "-oProxyCommand=x@h"}, "-oProxyCommand"},
		{"tire ile başlayan kullanıcı", Server{Name: "a", Target: "-x@h"}, "-x"},
		{"boş hedef", Server{Name: "a", Target: ""}, "target"},
		{"yerel yönetici", Server{Name: "a", Target: good.Target, Admin: "unix:///run/x.sock"}, "admin"},
		{"seçenek gibi yönetici", Server{Name: "a", Target: good.Target, Admin: "-oProxyCommand=y@h"}, "-oProxyCommand"},
		{"uzun sağlayıcı", Server{Name: "a", Target: good.Target, Provider: strings.Repeat("p", 65)}, "provider"},
		{"sudo ama yönetici yok", Server{Name: "a", Target: good.Target, Sudo: true}, "admin"},
	} {
		err := Validate([]Server{tc.srv})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: hata %v, %q içermeli", tc.name, err, tc.want)
		}
	}
	for _, srv := range []Server{
		good,
		{Name: "yerel", Target: "unix:///run/kadran/api.sock"},
		{Name: "gcp", Target: "kadran-client@198.51.100.7", Admin: "ops@198.51.100.7", Sudo: true, Provider: "Google Cloud"},
	} {
		if err := Validate([]Server{srv}); err != nil {
			t.Errorf("%+v reddedildi: %v", srv, err)
		}
	}
}

func TestValidateUniqueness(t *testing.T) {
	a := Server{Name: "hetzner", Target: "kadran-client@a"}
	if err := Validate([]Server{a, {Name: "Hetzner", Target: "kadran-client@b"}}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("büyük harfli aynı ad: %v", err)
	}
	if err := Validate([]Server{a, {Name: "b", Target: a.Target}}); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("aynı hedef: %v", err)
	}
	var many []Server
	for i := range MaxServers + 1 {
		many = append(many, Server{Name: "s" + strconv.Itoa(i), Target: "kadran-client@h" + strconv.Itoa(i)})
	}
	if err := Validate(many); err == nil {
		t.Errorf("%d sunucu kabul edildi", len(many))
	}
	if err := Validate(many[:MaxServers]); err != nil {
		t.Errorf("%d sunucu reddedildi: %v", MaxServers, err)
	}
}

func TestFindIsExact(t *testing.T) {
	list := []Server{{Name: "hetzner", Target: "kadran-client@a"}}
	for _, miss := range []string{"Hetzner", "hetz", "hetzner ", " hetzner", "hetzner2"} {
		if _, ok := Find(list, miss); ok {
			t.Errorf("%q bulundu; yalnız tam eşleşme bulunmalı", miss)
		}
	}
	if s, ok := Find(list, "hetzner"); !ok || s.Target != "kadran-client@a" {
		t.Errorf("tam ad bulunamadı: %+v %v", s, ok)
	}
}

func TestHelpersDoNotMutate(t *testing.T) {
	in := []Server{
		{Name: "a", Target: "kadran-client@a", Provider: "P"},
		{Name: "b", Target: "kadran-client@b"},
	}
	want := slices.Clone(in)
	steps := []func() error{
		func() error { _, err := Add(in, Server{Name: "c", Target: "kadran-client@c"}); return err },
		func() error { _, err := Remove(in, "a"); return err },
		func() error { _, err := Rename(in, "a", "z"); return err },
		func() error { _, err := UpdateByTarget(in, "kadran-client@a", "y", ""); return err },
		func() error { _, err := RemoveByTarget(in, "kadran-client@b"); return err },
		func() error { _, _ = Import(in, []Server{{Name: "a", Target: "kadran-client@d"}}); return nil },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("adım %d: %v", i, err)
		}
		if !slices.Equal(in, want) {
			t.Fatalf("adım %d girdiyi değiştirdi: %+v", i, in)
		}
	}
}

func TestListHelpers(t *testing.T) {
	list := []Server{{Name: "a", Target: "kadran-client@a", Admin: "root@a", Sudo: false, Provider: "P"}}
	if _, err := Add(list, Server{Name: "A", Target: "kadran-client@x"}); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("Add aynı ad: %v", err)
	}
	if _, err := Add(list, Server{Name: "x", Target: "kadran-client@a"}); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("Add aynı hedef: %v", err)
	}
	if _, err := Remove(list, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove yok: %v", err)
	}
	got, err := Rename(list, "a", "z")
	if err != nil || got[0].Name != "z" || got[0].Admin != "root@a" || got[0].Provider != "P" {
		t.Errorf("Rename: %+v %v", got, err)
	}
	got, err = UpdateByTarget(list, "kadran-client@a", "y", "")
	if err != nil || got[0].Name != "y" || got[0].Provider != "" || got[0].Admin != "root@a" {
		t.Errorf("UpdateByTarget: %+v %v", got, err)
	}
	if _, err := UpdateByTarget(list, "kadran-client@zz", "y", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateByTarget yok: %v", err)
	}
	if got, err := RemoveByTarget(list, "kadran-client@a"); err != nil || len(got) != 0 {
		t.Errorf("RemoveByTarget: %+v %v", got, err)
	}
}

func TestImportNormalizesAndSkips(t *testing.T) {
	list := []Server{{Name: "hetzner", Target: "kadran-client@a"}}
	in := []Server{
		{Name: "hetzner", Target: "kadran-client@b"},
		{Name: "hetzner", Target: "kadran-client@c"},
		{Name: "-gcp", Target: "kadran-client@d"},
		{Name: strings.Repeat("x", 200), Target: "kadran-client@e"},
		{Name: "dup", Target: "kadran-client@a"},
		{Name: " - ", Target: "kadran-client@host-f"},
	}
	got, n := Import(list, in)
	if n != 5 {
		t.Fatalf("eklenen %d, beklenen 5: %+v", n, got)
	}
	names := map[string]string{}
	for _, s := range got {
		names[s.Target] = s.Name
	}
	want := map[string]string{
		"kadran-client@a":      "hetzner",
		"kadran-client@b":      "hetzner (2)",
		"kadran-client@c":      "hetzner (3)",
		"kadran-client@d":      "gcp",
		"kadran-client@e":      strings.Repeat("x", 64),
		"kadran-client@host-f": "host-f",
	}
	for target, name := range want {
		if names[target] != name {
			t.Errorf("%s adı %q, beklenen %q", target, names[target], name)
		}
	}
	if len(got) != 6 {
		t.Errorf("liste %d kayıt, beklenen 6", len(got))
	}
	if err := Validate(got); err != nil {
		t.Errorf("içe aktarılan liste geçersiz: %v", err)
	}
}

func TestImportSuffixFitsLimit(t *testing.T) {
	long := strings.Repeat("ş", 64)
	got, n := Import([]Server{{Name: long, Target: "kadran-client@a"}}, []Server{{Name: long, Target: "kadran-client@b"}})
	if n != 1 || len([]rune(got[1].Name)) > MaxName || !strings.HasSuffix(got[1].Name, " (2)") {
		t.Errorf("ek sığmadı: %q (%d)", got[1].Name, len([]rune(got[1].Name)))
	}
}
