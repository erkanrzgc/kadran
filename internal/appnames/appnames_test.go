package appnames

import (
	"reflect"
	"testing"
)

func TestPartner(t *testing.T) {
	cases := []struct{ domain, want string }{
		{"example.com", "www.example.com"},
		{"www.example.com", "example.com"},
		{"app.example.com", ""},
		{"example.co.uk", "www.example.co.uk"},
		{"www.example.co.uk", "example.co.uk"},
		{"shop.example.co.uk", ""},
		{"home.duckdns.org", "www.home.duckdns.org"},
		{"", ""},
		{"com", ""},
		{"127.0.0.1", ""},
	}
	for _, c := range cases {
		if got := Partner(c.domain); got != c.want {
			t.Errorf("Partner(%q) = %q, beklenen %q", c.domain, got, c.want)
		}
	}
}

func TestNames(t *testing.T) {
	cases := []struct {
		name      string
		domain    string
		www       bool
		redirects []string
		want      []string
	}{
		{"eş ve açık adlar, tekrarsız, domain çıkarılmış", "example.com", true, []string{"old.com", "old.com", "example.com"}, []string{"www.example.com", "old.com"}},
		{"www kapalı", "example.com", false, []string{"old.com", "old.com", "example.com"}, []string{"old.com"}},
		{"açık ad eşe eşitse eş bir kez", "example.com", true, []string{"www.example.com"}, []string{"www.example.com"}},
		{"alt ad eş türetmez", "app.example.com", true, nil, nil},
		{"boş domain", "", true, []string{"a.com"}, nil},
	}
	for _, c := range cases {
		got := Names(c.domain, c.www, c.redirects)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Names = %#v, beklenen %#v", c.name, got, c.want)
		}
	}
}

func TestConflict(t *testing.T) {
	site := Owner{AppID: "site", Domain: "example.com", WWW: true}

	t.Run("yeni domain başkasının eşi", func(t *testing.T) {
		name, owner, found := Conflict(Owner{AppID: "new", Domain: "www.example.com"}, []Owner{site})
		if !found || name != "www.example.com" || owner != "site" {
			t.Fatalf("Conflict = (%q, %q, %v)", name, owner, found)
		}
	})
	t.Run("yeni açık ad başkasının domaini", func(t *testing.T) {
		c := Owner{AppID: "new", Domain: "other.org", Redirects: []string{"example.com"}}
		name, owner, found := Conflict(c, []Owner{site})
		if !found || name != "example.com" || owner != "site" {
			t.Fatalf("Conflict = (%q, %q, %v)", name, owner, found)
		}
	})
	t.Run("kendi kaydı çakışma sayılmaz", func(t *testing.T) {
		if name, owner, found := Conflict(site, []Owner{site}); found {
			t.Fatalf("kendi kaydı çakıştı: (%q, %q)", name, owner)
		}
	})
	t.Run("çakışma yok", func(t *testing.T) {
		c := Owner{AppID: "new", Domain: "other.org", WWW: true, Redirects: []string{"old.org"}}
		if name, owner, found := Conflict(c, []Owner{site}); found {
			t.Fatalf("çakışma yokken (%q, %q) döndü", name, owner)
		}
	})
	t.Run("domainsiz c, domainsiz sahiplerle çakışmaz", func(t *testing.T) {
		others := []Owner{{AppID: "x"}, {AppID: "y", WWW: true}}
		if name, owner, found := Conflict(Owner{AppID: "new"}, others); found {
			t.Fatalf("domainsizler çakıştı: (%q, %q)", name, owner)
		}
	})
	t.Run("domainsiz sahipler yanlış çakışma üretmez, sonraki gerçek çakışma bulunur", func(t *testing.T) {
		c := Owner{AppID: "new", Domain: "www.example.com"}
		others := []Owner{{AppID: "x"}, {AppID: "y"}, site}
		name, owner, found := Conflict(c, others)
		if !found || name != "www.example.com" || owner != "site" {
			t.Fatalf("Conflict = (%q, %q, %v)", name, owner, found)
		}
		if name, owner, found := Conflict(Owner{AppID: "new", Domain: "other.org"}, []Owner{{AppID: "x"}}); found {
			t.Fatalf("yanlış çakışma: (%q, %q)", name, owner)
		}
	})
	t.Run("iki sahip aynı adı kullanıyorsa ilki döner", func(t *testing.T) {
		a := Owner{AppID: "a", Domain: "a.org", Redirects: []string{"shared.org"}}
		b := Owner{AppID: "b", Domain: "b.org", Redirects: []string{"shared.org"}}
		c := Owner{AppID: "new", Domain: "shared.org"}
		name, owner, found := Conflict(c, []Owner{a, b})
		if !found || name != "shared.org" || owner != "a" {
			t.Fatalf("Conflict = (%q, %q, %v)", name, owner, found)
		}
	})
}
