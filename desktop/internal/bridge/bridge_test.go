package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
)

// Whitelist, arayüze açılan yöntemler: Electron köprüsünün (preload)
// listesi. Wails dışa açık HER yöntemi açtığı için bu liste ile yapının
// yöntemleri birebir aynı olmalı; yeni bir yöntem bilinçli eklenir.
var Whitelist = []string{
	"AddServer", "AlarmsList", "AppCreate", "AppDelete", "AppDeploy", "AppGet", "AppLogs", "AppRollback", "AppUpdate", "AppsList",
	"AuditList", "AuditVerify", "BackupCreate", "BackupsList", "CopyText", "ListServers", "LoadAppearance", "RemoveServer",
	"SaveAppearance", "Status", "UpdateServer", "Version", "WindowControl",
}

// goCall, bridge.ts'teki bir Go yöntemi çağrısı: go.Status(…).
var goCall = regexp.MustCompile(`\bgo\.([A-Z][A-Za-z]*)\(`)

func readRendererBridge() (string, error) {
	body, err := os.ReadFile(filepath.Join("..", "..", "src", "renderer", "bridge.ts"))
	return string(body), err
}

func TestExportedMethodsAreExactlyTheWhitelist(t *testing.T) {
	typ := reflect.TypeOf(&Bridge{})
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if !slices.Equal(got, Whitelist) {
		t.Fatalf("arayüze açılan yöntemler değişti:\n got %v\nwant %v", got, Whitelist)
	}
}

// Arayüzün köprüsü (src/renderer/bridge.ts) yalnız bu adları çağırır.
func TestRendererBridgeCallsOnlyWhitelistedMethods(t *testing.T) {
	src, err := readRendererBridge()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range Whitelist {
		if !strings.Contains(src, "go."+name+"(") {
			t.Errorf("bridge.ts %s'i çağırmıyor", name)
		}
	}
	calls := goCall.FindAllStringSubmatch(src, -1)
	for _, c := range calls {
		if !slices.Contains(Whitelist, c[1]) {
			t.Errorf("bridge.ts listede olmayan %s'i çağırıyor", c[1])
		}
	}
}

func TestWindowControlAndCopy(t *testing.T) {
	hs := newHarness(t, true)
	for _, a := range []string{"minimize", "maximize", "close"} {
		if err := hs.b.WindowControl(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := hs.b.WindowControl("devtools"); err == nil {
		t.Error("devtools kabul edildi")
	}
	if strings.Join(hs.h.actions, ",") != "minimise,maximise,close" {
		t.Errorf("%v", hs.h.actions)
	}
	if err := hs.b.CopyText(strings.Repeat("x", 1025)); err == nil || hs.h.clipboard != "" {
		t.Error("uzun metin panoya yazıldı")
	}
	if err := hs.b.CopyText("SHA256:abc"); err != nil || hs.h.clipboard != "SHA256:abc" {
		t.Errorf("%v %q", err, hs.h.clipboard)
	}
}

func TestAppearanceGlassBeforeFile(t *testing.T) {
	hs := newHarness(t, true)
	bundle := hs.b.LoadAppearance()
	if bundle.Appearance != appearance.Defaults() || bundle.Defaults != appearance.Defaults() || bundle.Material != appearance.Accent {
		t.Fatalf("%+v", bundle)
	}
	saved, err := hs.b.SaveAppearance(json.RawMessage(`{"version":2,"theme":"dark","language":"tr","extra":1}`))
	if err != nil || saved.Theme != "dark" || saved.Language != "tr" {
		t.Fatalf("%+v %v", saved, err)
	}
	if len(hs.h.looks) != 1 || hs.h.looks[0].Scheme != "dark" || hs.h.looks[0].Material != appearance.Accent {
		t.Fatalf("cam: %+v", hs.h.looks)
	}
	if got := hs.b.LoadAppearance().Appearance; got != saved {
		t.Fatalf("okunan %+v", got)
	}
	body, _ := json.Marshal(hs.b.LoadAppearance())
	if !strings.Contains(string(body), `"material":"accent"`) || !strings.Contains(string(body), `"uiFont":{"family":"","weight":400}`) {
		t.Errorf("arayüzün beklediği biçim: %s", body)
	}
}

func TestVersionGoesThroughTheDispatcher(t *testing.T) {
	hs := newHarness(t, true)
	hs.d.results["version"] = map[string]any{"version": "dev", "protocol": 3}
	got, err := hs.b.Version()
	if err != nil || !reflect.DeepEqual(got, map[string]any{"version": "dev", "protocol": 3}) {
		t.Fatalf("%v %v", got, err)
	}
}
