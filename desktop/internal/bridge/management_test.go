package bridge

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/sidecar"
)

const (
	target = "kadran-client@203.0.113.7"
	secret = "s3cret-value-9f2"
)

func validSpec() string {
	return `{"app_id":"blog","git_host":"github.com","git_owner":"acme","git_repo":"blog","git_branch":"release/1.x","dockerfile_path":"Dockerfile","build_args":{},
		"container_port":3000,"replicas":2,"health_path":"/healthz","domain":"blog.example.com",
		"limits":{"memory_bytes":"536870912","cpu_millis":1000,"blkio_weight":100},
		"env":{"API_TOKEN":"` + secret + `"},"volumes":[{"name":"data","mount_path":"/app/data","read_only":false}]}`
}

// mutating, durum değiştiren altı yöntem ve geçerli çağrıları.
type mutation struct {
	name, method string
	run          func(b *Bridge, target string) (any, error)
}

func mutations() []mutation {
	update := `{"domain":"new.example.com","env":{"DB_PASSWORD":"` + secret + `"},"env_remove":["OLD_KEY"],"volumes":[{"name":"cache","mount_path":"/app/cache","read_only":true}],"volume_remove":["scratch"]}`
	return []mutation{
		{"AppCreate", "apps.create", func(b *Bridge, t string) (any, error) { return b.AppCreate(t, json.RawMessage(validSpec())) }},
		{"AppUpdate", "apps.update", func(b *Bridge, t string) (any, error) { return b.AppUpdate(t, "blog", json.RawMessage(update)) }},
		{"AppDeploy", "apps.deploy", func(b *Bridge, t string) (any, error) {
			return b.AppDeploy(t, "blog", "0123456789abcdef0123456789abcdef01234567")
		}},
		{"AppDelete", "apps.delete", func(b *Bridge, t string) (any, error) { return b.AppDelete(t, "blog", "blog") }},
		{"AppRollback", "apps.rollback", func(b *Bridge, t string) (any, error) { return b.AppRollback(t, "blog", "blog") }},
		{"BackupCreate", "backups.create", func(b *Bridge, t string) (any, error) { return b.BackupCreate(t) }},
	}
}

func isCancelled(v any) bool {
	c, ok := v.(Cancelled)
	return ok && c.Cancelled
}

func TestMutationsCancelledNeverReachTheServer(t *testing.T) {
	for _, m := range mutations() {
		hs := newHarness(t, false)
		got, err := m.run(hs.b, target)
		if err != nil || !isCancelled(got) {
			t.Errorf("%s: %v %v", m.name, got, err)
		}
		body, _ := json.Marshal(got)
		if string(body) != `{"cancelled":true}` {
			t.Errorf("%s: iptal biçimi %s", m.name, body)
		}
		if hs.h.boxCount() != 1 || hs.d.count() != 0 {
			t.Errorf("%s: kutu %d, çağrı %d", m.name, hs.h.boxCount(), hs.d.count())
		}
	}
}

func TestMutationsConfirmedCallWithValidatedParams(t *testing.T) {
	for _, m := range mutations() {
		hs := newHarness(t, true)
		got, err := m.run(hs.b, "  "+target+" ")
		if err != nil || isCancelled(got) {
			t.Fatalf("%s: %v %v", m.name, got, err)
		}
		if hs.d.count() != 1 || hs.d.calls[0].method != m.method || hs.d.calls[0].params["target"] != target {
			t.Errorf("%s: çağrılar %+v", m.name, hs.d.calls)
		}
		box := hs.h.boxes[0]
		if box.Cancel != "Cancel" || !strings.Contains(box.Detail, "Server: "+target) {
			t.Errorf("%s: kutu %+v", m.name, box)
		}
	}
}

func TestMutationsFollowSavedLanguage(t *testing.T) {
	for _, m := range mutations() {
		hs := newHarness(t, false)
		hs.language(t, "tr")
		if _, err := m.run(hs.b, target); err != nil {
			t.Fatal(err)
		}
		if box := hs.h.boxes[0]; box.Cancel != "Vazgeç" || !strings.Contains(box.Detail, "Sunucu: "+target) {
			t.Errorf("%s: %+v", m.name, box)
		}
	}
}

func TestFixtureSkipsOnlyFakeTest(t *testing.T) {
	for _, m := range mutations() {
		other := newHarness(t, false)
		other.b.deps.Fixture = true
		if got, _ := m.run(other.b, "kadran-client@fixture-other"); !isCancelled(got) || other.h.boxCount() != 1 {
			t.Errorf("%s: başka hedefte kutu atlandı", m.name)
		}
		real := newHarness(t, false)
		if got, _ := m.run(real.b, fixtureTarget); !isCancelled(got) || real.h.boxCount() != 1 {
			t.Errorf("%s: düzenek dışında fake@test atlandı", m.name)
		}
		skipped := newHarness(t, false)
		skipped.b.deps.Fixture = true
		if _, err := m.run(skipped.b, fixtureTarget); err != nil || skipped.h.boxCount() != 0 || skipped.d.count() != 1 {
			t.Errorf("%s: düzenekte fake@test atlanmadı: %v", m.name, err)
		}
	}
}

func TestCreateBoxShowsDefinitionButNeverEnvValues(t *testing.T) {
	hs := newHarness(t, true)
	if _, err := hs.b.AppCreate(target, json.RawMessage(validSpec())); err != nil {
		t.Fatal(err)
	}
	text := hs.h.boxes[0].Message + "\n" + hs.h.boxes[0].Detail
	for _, part := range []string{"blog", "github.com/acme/blog", "release/1.x", "blog.example.com", "3000", "data", "/app/data", "API_TOKEN"} {
		if !strings.Contains(text, part) {
			t.Errorf("%q yok", part)
		}
	}
	if strings.Contains(text, secret) {
		t.Fatal("env değeri kutuda")
	}
	// Sunucuya giden, kutuda gösterilenle aynı yük (sırası korunmuş).
	spec, _ := json.Marshal(hs.d.calls[0].params["spec"])
	if !strings.Contains(string(spec), `"API_TOKEN":"`+secret+`"`) {
		t.Errorf("yük değeri kayboldu: %s", spec)
	}
}

func TestUpdateBoxOnlyChangedFields(t *testing.T) {
	for _, lang := range []string{"en", "tr"} {
		hs := newHarness(t, true)
		hs.language(t, lang)
		if _, err := hs.b.AppUpdate(target, "blog", json.RawMessage(`{"domain":"new.example.com","env":{"DB_PASSWORD":"`+secret+`"},"env_remove":["OLD_KEY"],"volume_remove":["scratch"]}`)); err != nil {
			t.Fatal(err)
		}
		text := hs.h.boxes[0].Detail
		for _, part := range []string{target, "new.example.com", "DB_PASSWORD", "OLD_KEY", "scratch"} {
			if !strings.Contains(text, part) {
				t.Errorf("%s: %q yok", lang, part)
			}
		}
		if strings.Contains(text, secret) || strings.Contains(text, "Branch:") || strings.Contains(text, "Dal:") {
			t.Errorf("%s: %s", lang, text)
		}
		if got := hs.d.calls[0].params; got["app_id"] != "blog" || got["update"] == nil {
			t.Errorf("parametreler %+v", got)
		}
	}
}

func TestDeployBoxAndParams(t *testing.T) {
	hs := newHarness(t, true)
	if _, err := hs.b.AppDeploy(target, "blog", "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hs.h.boxes[0].Detail, "Ref: v1.2.3") || !strings.Contains(hs.h.boxes[0].Message, "blog") {
		t.Errorf("%+v", hs.h.boxes[0])
	}
	want := map[string]any{"target": target, "app_id": "blog", "ref": "v1.2.3"}
	if !reflect.DeepEqual(hs.d.calls[0].params, want) {
		t.Errorf("%+v", hs.d.calls[0].params)
	}
}

func TestDeleteAndBackupBoxesWarn(t *testing.T) {
	hs := newHarness(t, true)
	_, _ = hs.b.AppDelete(target, "blog", "blog")
	_, _ = hs.b.BackupCreate(target)
	if !strings.Contains(hs.h.boxes[0].Detail, "cannot be undone") || !strings.Contains(hs.h.boxes[1].Detail, "oldest backups may be deleted") {
		t.Errorf("%+v", hs.h.boxes)
	}
	tr := newHarness(t, true)
	tr.language(t, "tr")
	_, _ = tr.b.AppDelete(target, "blog", "blog")
	if !strings.Contains(tr.h.boxes[0].Detail, "geri alınamaz") {
		t.Errorf("%+v", tr.h.boxes[0])
	}
}

func TestInvisibleCharactersEscapedInBox(t *testing.T) {
	hs := newHarness(t, true)
	health := "/ok" + string(rune(0x202e)) + "evil"
	raw, _ := json.Marshal(map[string]string{"health_path": health})
	if _, err := hs.b.AppUpdate(target, "blog", raw); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(hs.h.boxes[0].Detail, 0x202e) || !strings.Contains(hs.h.boxes[0].Detail, string(rune(92))+"u202e") {
		t.Fatalf("%q", hs.h.boxes[0].Detail)
	}
}

func TestReadsNeverAsk(t *testing.T) {
	hs := newHarness(t, false)
	for _, f := range []func() (any, error){
		func() (any, error) { return hs.b.AppsList(target) },
		func() (any, error) { return hs.b.AppGet(target, "blog") },
		func() (any, error) { return hs.b.AppLogs(target, "blog", 100) },
		func() (any, error) { return hs.b.AlarmsList(target) },
		func() (any, error) { return hs.b.BackupsList(target) },
	} {
		if _, err := f(); err != nil {
			t.Fatal(err)
		}
	}
	var methods []string
	for _, c := range hs.d.calls {
		methods = append(methods, c.method)
	}
	if hs.h.boxCount() != 0 || strings.Join(methods, ",") != "apps.list,apps.get,apps.logs,alarms.list,backups.list" {
		t.Fatalf("kutu %d, %v", hs.h.boxCount(), methods)
	}
}

func TestBadConfirmationTargetOrPayloadNeverAsks(t *testing.T) {
	hs := newHarness(t, true)
	if _, err := hs.b.AppDelete(target, "blog", "BLOG"); err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Errorf("silme onayı: %v", err)
	}
	if _, err := hs.b.AppRollback(target, "blog", ""); err == nil {
		t.Error("boş geri alma onayı kabul edildi")
	}
	for _, bad := range []string{"host\nServer: evil", "host" + string(rune(0x202e)) + "gpj", "host" + string(rune(0x200b)) + "x", "-oProxyCommand=x"} {
		if _, err := hs.b.AppDeploy(bad, "blog", "main"); err == nil {
			t.Errorf("hedef %q kabul edildi", bad)
		}
		if _, err := hs.b.BackupCreate(bad); err == nil {
			t.Errorf("hedef %q kabul edildi", bad)
		}
	}
	if _, err := hs.b.AppCreate(target, json.RawMessage(`{"app_id":"blog","git_host":"Evil.com"}`)); err == nil {
		t.Error("geçersiz yük kabul edildi")
	}
	for _, key := range []string{"domain", "git_branch", "health_path", "replicas"} {
		if _, err := hs.b.AppUpdate(target, "blog", json.RawMessage(`{"`+key+`":null}`)); err == nil || !strings.Contains(err.Error(), "must not be null") {
			t.Errorf("%s null: %v", key, err)
		}
	}
	if hs.h.boxCount() != 0 || hs.d.count() != 0 {
		t.Fatalf("kutu %d, çağrı %d", hs.h.boxCount(), hs.d.count())
	}
}

// outcome, bir çağrıyı arka planda koşturur.
func outcome(f func() (any, error)) chan error {
	ch := make(chan error, 1)
	go func() {
		_, err := f()
		ch <- err
	}()
	return ch
}

func TestGateOneOperationPerAppAndOneBoxAtATime(t *testing.T) {
	hs := newHarness(t, true)
	hs.h.hold, hs.h.shown = make(chan struct{}), make(chan struct{}, 4)
	first := outcome(func() (any, error) { return hs.b.AppDeploy(target, "blog", "main") })
	<-hs.h.shown
	for name, f := range map[string]func() (any, error){
		"aynı uygulama deploy": func() (any, error) { return hs.b.AppDeploy(target, "blog", "other") },
		"aynı uygulama silme":  func() (any, error) { return hs.b.AppDelete(target, "blog", "blog") },
	} {
		if err := <-outcome(f); err == nil || !strings.Contains(err.Error(), "already running") {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, f := range map[string]func() (any, error){
		"başka uygulama": func() (any, error) { return hs.b.AppDeploy(target, "shop", "main") },
		"başka hedef":    func() (any, error) { return hs.b.BackupCreate("kadran-client@198.51.100.9") },
	} {
		if err := <-outcome(f); err == nil || !strings.Contains(err.Error(), "another confirmation is open") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if hs.h.boxCount() != 1 || hs.d.count() != 0 {
		t.Fatalf("kutu %d, çağrı %d", hs.h.boxCount(), hs.d.count())
	}
	close(hs.h.hold)
	select {
	case err := <-first:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ilk işlem bitmedi")
	}
	hs.h.hold, hs.h.shown = nil, nil
	// Reddedilen çağrı kendi uygulamasının kapısını da bırakmış olmalı.
	if _, err := hs.b.AppDeploy(target, "shop", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.b.AppDeploy(target, "blog", "main"); err != nil {
		t.Fatal(err)
	}
	if hs.d.count() != 3 {
		t.Fatalf("çağrı %d", hs.d.count())
	}
}

func TestDispatcherErrorReachesUIRedacted(t *testing.T) {
	failure := &sidecar.Error{Code: sidecar.CodeInternalError, Message: "call failed", Data: "clone https://user:hunter2@git.example/x.git failed" + string(rune(0x202e)) + ": ghp_abcdef123456"}
	want := "call failed: clone https://***@git.example/x.git failed: ***"
	calls := map[string]func(b *Bridge) (any, error){
		"Status":      func(b *Bridge) (any, error) { return b.Status(target) },
		"AuditList":   func(b *Bridge) (any, error) { return b.AuditList(target, json.RawMessage(`0`), json.RawMessage(`50`)) },
		"AuditVerify": func(b *Bridge) (any, error) { return b.AuditVerify(target) },
		"AppsList":    func(b *Bridge) (any, error) { return b.AppsList(target) },
		"AppGet":      func(b *Bridge) (any, error) { return b.AppGet(target, "blog") },
		"AppLogs":     func(b *Bridge) (any, error) { return b.AppLogs(target, "blog", 100) },
		"AlarmsList":  func(b *Bridge) (any, error) { return b.AlarmsList(target) },
		"BackupsList": func(b *Bridge) (any, error) { return b.BackupsList(target) },
		"Version":     func(b *Bridge) (any, error) { return b.Version() },
	}
	for _, m := range mutations() {
		calls[m.name] = func(b *Bridge) (any, error) { return m.run(b, target) }
	}
	for name, f := range calls {
		hs := newHarness(t, true)
		hs.d.fail = failure
		if _, err := f(hs.b); err == nil || err.Error() != want {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAuditListParamsAreLenient(t *testing.T) {
	hs := newHarness(t, true)
	if _, err := hs.b.AuditList(target, json.RawMessage(`-5`), json.RawMessage(`"20"`)); err != nil {
		t.Fatal(err)
	}
	if p := hs.d.calls[0].params; p["after_seq"] != float64(0) || p["limit"] != float64(20) {
		t.Fatalf("%+v", p)
	}
}
