package connproto

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseDeployScope(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"web", []string{"web"}},
		{"web,api", []string{"web", "api"}},
		{"a1,b-2,c", []string{"a1", "b-2", "c"}},
	}
	for _, c := range cases {
		got, err := ParseDeployScope(c.in)
		if err != nil {
			t.Errorf("ParseDeployScope(%q) hata verdi: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseDeployScope(%q) = %q, beklenen %q", c.in, got, c.want)
		}
	}
}

// TestParseDeployScopeRejects, kapsam satırının authorized_keys'ten
// geldiğini ve sshd'nin onu kullanıcının kabuğuna `-c` ile verdiğini
// hatırlatır: karakter kümesi aynı zamanda kabuk enjeksiyonu sınırı.
func TestParseDeployScopeRejects(t *testing.T) {
	for _, in := range []string{
		"",                      // kapsamsız dağıtım anahtarı = her uygulama
		",",                     // boş öğeler
		"web,",                  // sondaki boş öğe
		",web",                  // baştaki boş öğe
		"web,,api",              // ortadaki boş öğe
		"web,web",               // tekrar
		"Web",                   // büyük harf: uygulama adı olamaz
		"1web",                  // rakamla başlıyor
		"web api",               // boşluk
		"web;id",                // kabuk metakarakteri
		"$(id)",                 // komut ikamesi
		"web\napi",              // satır sonu
		"-web",                  // seçenek gibi görünen ad
		"*",                     // joker YOK: kapsam açıkça yazılır
		strings.Repeat("a", 33), // ad sınırı 32
	} {
		if got, err := ParseDeployScope(in); err == nil {
			t.Errorf("ParseDeployScope(%q) kabul edildi (%q)", in, got)
		} else if !errors.Is(err, ErrInvalidRole) {
			t.Errorf("ParseDeployScope(%q) hatası ErrInvalidRole sarmıyor: %v", in, err)
		}
	}
}

func TestParseDeployScopeLimitsCount(t *testing.T) {
	apps := make([]string, MaxDeployApps+1)
	for i := range apps {
		apps[i] = "a" + strings.Repeat("b", i%5) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if _, err := ParseDeployScope(strings.Join(apps, ",")); err == nil {
		t.Fatalf("%d uygulamalık kapsam kabul edildi, sınır %d", len(apps), MaxDeployApps)
	}
	if _, err := ParseDeployScope(strings.Join(apps[:MaxDeployApps], ",")); err != nil {
		t.Fatalf("sınırdaki kapsam reddedildi: %v", err)
	}
}

func TestCheckRole(t *testing.T) {
	gecerli := []Identity{
		{Role: RoleAdmin},
		{Role: RoleDeploy, Apps: []string{"web"}},
		{Role: RoleDeploy, Apps: []string{"web", "api"}},
	}
	for _, id := range gecerli {
		if err := id.CheckRole(); err != nil {
			t.Errorf("%+v reddedildi: %v", id, err)
		}
	}

	gecersiz := []Identity{
		{},                 // rol yok: eski kadran-connect ya da hata — KAPALI
		{Role: "root"},     // bilinmeyen rol
		{Role: "Admin"},    // büyük/küçük harf duyarlı
		{Role: RoleDeploy}, // kapsamsız dağıtım
		{Role: RoleDeploy, Apps: []string{}},
		{Role: RoleDeploy, Apps: []string{"Web"}},
		{Role: RoleDeploy, Apps: []string{"web", "web"}},
		{Role: RoleAdmin, Apps: []string{"web"}}, // yönetici kapsam taşımaz
	}
	for _, id := range gecersiz {
		err := id.CheckRole()
		if err == nil {
			t.Errorf("%+v kabul edildi", id)
		} else if !errors.Is(err, ErrInvalidRole) {
			t.Errorf("%+v hatası ErrInvalidRole sarmıyor: %v", id, err)
		}
	}
}

func TestCanDeploy(t *testing.T) {
	admin := Identity{Role: RoleAdmin}
	deploy := Identity{Role: RoleDeploy, Apps: []string{"web", "api"}}

	cases := []struct {
		id   Identity
		app  string
		want bool
	}{
		{admin, "web", true},
		{admin, "baska", true},
		{deploy, "web", true},
		{deploy, "api", true},
		{deploy, "baska", false},
		{deploy, "", false},
		{deploy, "we", false}, // önek eşleşmesi değil
		{deploy, "web2", false},
		{Identity{}, "web", false},                 // rolsüz
		{Identity{Role: "root"}, "web", false},     // bilinmeyen rol
		{Identity{Role: RoleDeploy}, "web", false}, // kapsamsız
		// Tutarsız yönetici (kapsam taşıyan): geçersiz kimlik hiçbir şey
		// dağıtamaz.
		{Identity{Role: RoleAdmin, Apps: []string{"web"}}, "web", false},
	}
	for _, c := range cases {
		if got := c.id.CanDeploy(c.app); got != c.want {
			t.Errorf("%+v.CanDeploy(%q) = %v, beklenen %v", c.id, c.app, got, c.want)
		}
	}
}

// TestRoleRoundTrips, rolün ve kapsamın önsözden geçtiğini doğrular.
func TestRoleRoundTrips(t *testing.T) {
	want := Identity{Origin: "ssh", Role: RoleDeploy, Apps: []string{"web", "api"}}
	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatalf("yazılamadı: %v", err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gidiş-dönüşte değişti:\nyazılan: %+v\nokunan:  %+v", want, got)
	}
}
