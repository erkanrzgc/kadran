package appearance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func sanitize(t *testing.T, v any) Appearance {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return Sanitize(raw)
}

type m = map[string]any

func TestDefaultsLightEnglish(t *testing.T) {
	want := Appearance{Version: 2, Language: "en", Theme: "light", UIFont: Font{"", 400}, CodeFont: Font{"", 400}}
	if Defaults() != want {
		t.Fatalf("%+v", Defaults())
	}
}

func TestThemeUnknownFallsBackFontKept(t *testing.T) {
	for theme, want := range map[string]string{"light": "light", "dark": "dark", "mica": "light", "glass": "light", "__proto__": "light"} {
		if got := sanitize(t, m{"version": 2, "theme": theme}).Theme; got != want {
			t.Errorf("%s → %s", theme, got)
		}
	}
	saved := sanitize(t, m{"version": 2, "uiFont": m{"family": "Arial", "weight": 500}})
	if saved.Theme != "light" || saved.UIFont != (Font{"Arial", 500}) {
		t.Errorf("%+v", saved)
	}
	old := sanitize(t, m{"version": 2, "language": "tr", "theme": "glass", "uiFont": m{"family": "Arial", "weight": 500}})
	if old.Language != "tr" || old.Theme != "light" || old.UIFont != (Font{"Arial", 500}) {
		t.Errorf("eski cam teması: %+v", old)
	}
}

func TestNotAnObjectGivesDefaults(t *testing.T) {
	for _, raw := range []string{`null`, `42`, `"x"`, `[]`, `[1,2]`, `{bozuk`} {
		if got := Sanitize(json.RawMessage(raw)); got != Defaults() {
			t.Errorf("%s → %+v", raw, got)
		}
	}
}

func TestOldVersionKeepsOnlyLanguage(t *testing.T) {
	old := m{"version": 1, "mode": "clear", "tint": 96, "language": "tr", "uiFont": m{"family": "JetBrainsMono Nerd Font Mono", "weight": 500}}
	want := Defaults()
	want.Language = "tr"
	if got := sanitize(t, old); got != want {
		t.Fatalf("%+v", got)
	}
	if got := Sanitize(json.RawMessage(`{"version":"2","language":"tr","theme":"dark"}`)); got.Theme != "light" || got.Language != "tr" {
		t.Errorf("metin sürüm eski sayılmalı: %+v", got)
	}
}

func TestLanguageAndFontLimits(t *testing.T) {
	if got := sanitize(t, m{"version": 2, "language": "de"}).Language; got != "en" {
		t.Errorf("de → %s", got)
	}
	a := sanitize(t, m{"version": 2, "uiFont": m{"family": strings.Repeat("x", 10000), "weight": 500}, "codeFont": m{"family": "JetBrainsMono Nerd Font", "weight": 1200}})
	if a.UIFont != (Font{"", 500}) || a.CodeFont != (Font{"JetBrainsMono Nerd Font", 900}) {
		t.Errorf("%+v", a)
	}
	if got := sanitize(t, m{"version": 2, "uiFont": "Arial"}).UIFont; got != (Font{"", 400}) {
		t.Errorf("metin yazı tipi: %+v", got)
	}
	for in, want := range map[float64]int{2.5: 100, 450.5: 451, 449.4: 449, -7: 100} {
		if got := sanitize(t, m{"version": 2, "uiFont": m{"family": "", "weight": in}}).UIFont.Weight; got != want {
			t.Errorf("kalınlık %v → %d, beklenen %d", in, got, want)
		}
	}
}

func TestMaterialAndLook(t *testing.T) {
	if MaterialFor("windows", 26200) != Accent || MaterialFor("windows", 22621) != Accent {
		t.Error("Windows 11 camı denemeli")
	}
	if MaterialFor("windows", 22000) != None || MaterialFor("windows", 19045) != None || MaterialFor("linux", 0) != None {
		t.Error("desteklemeyen sistem opak düşmeli")
	}
	if l := Look("dark", Accent); l.Background != (RGBA{}) || l.Scheme != "dark" {
		t.Errorf("%+v", l)
	}
	if l := Look("dark", None); l.Background != (RGBA{0x0a, 0x0a, 0x0a, 0xff}) {
		t.Errorf("%+v", l)
	}
	if l := Look("light", None); l.Background != (RGBA{0xed, 0xed, 0xf0, 0xff}) {
		t.Errorf("%+v", l)
	}
	// Tema değişince malzeme yeniden kurulmaz: yalnız opak zemin ve camın tonu.
	if c := Diff(Look("light", Accent), Look("dark", Accent)); c.Background || !c.Accent {
		t.Errorf("cam: %+v", c)
	}
	if c := Diff(Look("light", None), Look("dark", None)); !c.Background || c.Accent {
		t.Errorf("opak: %+v", c)
	}
	if c := Diff(Look("dark", Accent), Look("dark", Accent)); c.Background || c.Accent {
		t.Errorf("aynı görünüm titretir: %+v", c)
	}
}

func TestGlassTintNeverFullyTransparent(t *testing.T) {
	if GlassTint("light") != 0xa0f7f4f4 || GlassTint("dark") != 0x30181816 {
		t.Fatal("ton değişti (K-163)")
	}
	for _, s := range []string{"light", "dark"} {
		if GlassTint(s)>>24 == 0 {
			t.Errorf("%s: ton sıfır saydamlıkta pencere simsiyah çizilir", s)
		}
	}
}

func TestStoreMissingDamagedAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	var warnings []string
	warn := func(msg string) { warnings = append(warnings, msg) }
	if got := s.Load(warn); got != Defaults() || len(warnings) != 0 {
		t.Fatalf("yok: %+v %q", got, warnings)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{ bu json değil"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Load(warn); got != Defaults() || len(warnings) != 1 {
		t.Fatalf("bozuk: %+v %q", got, warnings)
	}
	saved, err := s.Save(json.RawMessage(`{"version":2,"language":"tr","uiFont":{"family":"<X>","weight":9999},"extra":"atılır"}`))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Language != "tr" || saved.UIFont.Weight != 900 || s.Load(nil) != saved {
		t.Fatalf("%+v", saved)
	}
	body, _ := os.ReadFile(filepath.Join(dir, FileName))
	want := "{\n  \"version\": 2,\n  \"language\": \"tr\",\n  \"theme\": \"light\",\n  \"uiFont\": {\n    \"family\": \"<X>\",\n    \"weight\": 900\n  },\n  \"codeFont\": {\n    \"family\": \"\",\n    \"weight\": 400\n  }\n}\n"
	if string(body) != want {
		t.Fatalf("dosya biçimi Electron'dakinden farklı:\n%s", body)
	}
	if _, err := s.Save(json.RawMessage(`{"version":2,"language":"en"}`)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{FileName}) || s.Load(nil).Language != "en" {
		t.Fatalf("geçici dosya kaldı ya da üstüne yazılmadı: %v", names)
	}
}

func TestStoreFailedWriteLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	// Hedef bir DİZİN: yeniden adlandırma başarısız olur.
	if err := os.Mkdir(filepath.Join(dir, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{Dir: dir}).Save(json.RawMessage(`{"version":2}`)); err == nil {
		t.Fatal("hata beklenirdi")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("geçici dosya kaldı: %d girdi", len(entries))
	}
}

// TestSolidFrameMatchesTokens: malzemesiz pencerenin açılış rengi arayüzün
// --frame-solid'iyle aynı; yoksa açılışta renk sıçrar.
func TestSolidFrameMatchesTokens(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "src", "renderer", "styles", "tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	for _, theme := range []string{"dark", "light"} {
		head := `:root[data-theme="` + theme + `"]`
		i := strings.Index(string(css), head)
		if i < 0 {
			t.Fatalf("%s bloğu yok", head)
		}
		block := string(css[i:])
		block = block[:strings.Index(block, "}")]
		m := regexp.MustCompile(`--frame-solid: (#[0-9a-f]{6});`).FindStringSubmatch(block)
		bg := Look(theme, None).Background
		if want := fmt.Sprintf("#%02x%02x%02x", bg.R, bg.G, bg.B); m == nil || m[1] != want {
			t.Errorf("%s: tokens.css %v, Go %s", theme, m, want)
		}
	}
}
