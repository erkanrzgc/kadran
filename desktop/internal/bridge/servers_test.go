package bridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/sidecar"
)

func TestServerInputRejectedBeforeDispatch(t *testing.T) {
	hs := newHarness(t, true)
	for name, f := range map[string]func() ([]Profile, error){
		"ad sayı": func() ([]Profile, error) {
			return hs.b.AddServer(json.RawMessage(`{"name":5,"target":"kadran-client@a"}`))
		},
		"uzun sağlayıcı": func() ([]Profile, error) {
			return hs.b.AddServer(json.RawMessage(`{"name":"a","target":"kadran-client@a","provider":"` + strings.Repeat("p", 65) + `"}`))
		},
		"sağlayıcı sayı": func() ([]Profile, error) {
			return hs.b.AddServer(json.RawMessage(`{"name":"a","target":"kadran-client@a","provider":3}`))
		},
		"seçenek hedef": func() ([]Profile, error) {
			return hs.b.AddServer(json.RawMessage(`{"name":"a","target":"-oProxyCommand=x"}`))
		},
		"boş ad": func() ([]Profile, error) {
			return hs.b.AddServer(json.RawMessage(`{"name":"","target":"kadran-client@a"}`))
		},
		"nesne değil": func() ([]Profile, error) { return hs.b.AddServer(json.RawMessage(`[1]`)) },
		"uzun yeni ad": func() ([]Profile, error) {
			return hs.b.UpdateServer("kadran-client@a", json.RawMessage(`{"name":"`+strings.Repeat("a", 201)+`","provider":""}`))
		},
		"boş silme hedefi": func() ([]Profile, error) { return hs.b.RemoveServer("") },
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s kabul edildi", name)
		}
	}
	if hs.d.count() != 0 {
		t.Fatalf("doğrulamadan önce %d çağrı", hs.d.count())
	}
}

func TestServerResultsCarryOnlyUIFields(t *testing.T) {
	hs := newHarness(t, true)
	list := map[string]any{"servers": []map[string]any{{"name": "a", "target": "kadran-client@a", "provider": "Hetzner", "admin": "root@x", "sudo": true}, {"name": "b", "target": "kadran-client@b"}}}
	for _, m := range []string{"servers.list", "servers.add", "servers.update", "servers.remove"} {
		hs.d.results[m] = list
	}
	got, err := hs.b.AddServer(json.RawMessage(`{"name":"a","target":" kadran-client@a ","provider":"Hetzner","extra":1}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Profile{{Name: "a", Target: "kadran-client@a", Provider: "Hetzner"}, {Name: "b", Target: "kadran-client@b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	body, _ := json.Marshal(got)
	if strings.Contains(string(body), "root@x") || strings.Contains(string(body), "sudo") || strings.Contains(string(body), `"provider":""`) {
		t.Fatalf("arayüze fazla alan: %s", body)
	}
	if p := hs.d.calls[0].params; !reflect.DeepEqual(p, map[string]any{"name": "a", "target": "kadran-client@a", "provider": "Hetzner"}) {
		t.Errorf("ekleme %+v", p)
	}
	if _, err := hs.b.UpdateServer("kadran-client@a", json.RawMessage(`{"name":"b"}`)); err != nil {
		t.Fatal(err)
	}
	if p := hs.d.calls[1].params; !reflect.DeepEqual(p, map[string]any{"target": "kadran-client@a", "name": "b", "provider": ""}) {
		t.Errorf("güncelleme %+v", p)
	}
	if _, err := hs.b.RemoveServer("kadran-client@a"); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.b.ListServers(); err != nil {
		t.Fatal(err)
	}
	if hs.d.calls[2].method != "servers.remove" || hs.d.calls[3].method != "servers.list" || hs.d.calls[3].params != nil {
		t.Errorf("%+v", hs.d.calls)
	}
}

func TestServerKnownReasonBecomesStableCode(t *testing.T) {
	hs := newHarness(t, true)
	hs.d.fail = &sidecar.Error{Code: sidecar.CodeInternalError, Message: "a server named a already exists", Data: "duplicate_name"}
	if _, err := hs.b.AddServer(json.RawMessage(`{"name":"a","target":"kadran-client@a"}`)); err == nil || err.Error() != "server-list:duplicate_name" {
		t.Fatalf("%v", err)
	}
	hs.d.fail = &sidecar.Error{Code: sidecar.CodeInternalError, Message: "disk full https://u:p@h/x"}
	if _, err := hs.b.ListServers(); err == nil || err.Error() != "disk full https://***@h/x" {
		t.Fatalf("%v", err)
	}
}

func TestServerListMalformedResult(t *testing.T) {
	hs := newHarness(t, true)
	for _, bad := range []any{map[string]any{}, map[string]any{"servers": []map[string]any{{"name": "a"}}}, "x"} {
		hs.d.results["servers.list"] = bad
		if _, err := hs.b.ListServers(); err == nil {
			t.Errorf("%v kabul edildi", bad)
		}
	}
	hs.d.results["servers.list"] = map[string]any{"servers": []any{}}
	if got, err := hs.b.ListServers(); err != nil || got == nil || len(got) != 0 {
		t.Errorf("boş liste: %v %v", got, err)
	}
}
