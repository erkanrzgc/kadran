package wire

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/servers"
)

func TestSessionUsesTheCLIKey(t *testing.T) {
	store := client.SessionStore{Dir: t.TempDir()}
	s := Stores{Sessions: &store}
	if tok, err := s.LoadSession("srv.example"); err != nil || tok != "" {
		t.Fatalf("oturum yokken %q %v", tok, err)
	}
	if err := store.Save("kadran-client@srv.example", "v1.a.b", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if tok, err := s.LoadSession("srv.example"); err != nil || tok != "v1.a.b" {
		t.Fatalf("kısa hedef CLI'ın anahtarını bulmalı: %q %v", tok, err)
	}
	if _, err := s.LoadSession("erkan@"); err == nil {
		t.Fatal("geçersiz hedef kabul edildi")
	}
}

func TestDispatcherUsesTheSharedServerList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kadran", "servers.json")
	d := Dispatcher(Stores{Servers: servers.Store{Path: path}}, nil)
	defer d.Close()
	if _, failure := d.Dispatch(context.Background(), "servers.add", json.RawMessage(`{"name":"a","target":"kadran-client@a","provider":""}`)); failure != nil {
		t.Fatal(failure)
	}
	list, err := (servers.Store{Path: path}).Load()
	if err != nil || len(list) != 1 || list[0].Target != "kadran-client@a" {
		t.Fatalf("CLI listeyi görmüyor: %+v %v", list, err)
	}
}

func TestProgressGoesToWarnings(t *testing.T) {
	var got []string
	w := writer(func(s string) { got = append(got, s) })
	if n, err := w.Write([]byte("resolving branch \"main\"…\n")); err != nil || n == 0 {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("  \n"))
	if len(got) != 1 || got[0] != "resolving branch \"main\"…" {
		t.Fatalf("%q", got)
	}
}

func TestDesktopHidesTheSSHConsole(t *testing.T) {
	d := deps(Stores{}, nil)
	if !d.HideConsole {
		t.Fatal("masaüstünde ssh konsol penceresi açar")
	}
	if d.CallTimeout != CallTimeout || d.LoadSession == nil || d.ResolveCommit == nil || d.Warn == nil {
		t.Fatalf("eksik bağımlılık: %+v", d)
	}
}
