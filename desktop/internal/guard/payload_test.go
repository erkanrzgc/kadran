package guard

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

// obj, testte yükü JSON metninden kurar.
type obj = map[string]any

func payload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func okSpec() obj {
	return obj{
		"app_id": "blog", "git_host": "git.example.co.uk", "git_owner": "Acme_1", "git_repo": "my.repo-2", "git_branch": "feature/x-1.2", "dockerfile_path": "", "build_args": obj{"NODE_ENV": "production"},
		"container_port": 65535, "replicas": 64, "health_path": "", "domain": "", "limits": obj{"memory_bytes": "6291456", "cpu_millis": 10, "blkio_weight": 1000},
		"env": obj{"EMPTY": "", "MULTI": "line1\nline2 ünlü {x} $(y)"}, "volumes": []any{obj{"name": "data", "mount_path": "/app/data", "read_only": true}, obj{"name": "cache", "mount_path": "/app/cache2"}},
	}
}

func with(base obj, patch obj) obj {
	out := obj{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		out[k] = v
	}
	return out
}

func TestCreateAcceptsUnusualSpecsTheServerAccepts(t *testing.T) {
	manyEnv := obj{}
	for i := range 200 {
		manyEnv[fmt.Sprintf("K%d", i)] = "v"
	}
	for i, spec := range []obj{
		okSpec(),
		with(okSpec(), obj{"dockerfile_path": "docker/Prod.Dockerfile", "health_path": "/a?b=c", "domain": "a.b.c.example.com"}),
		// Sunucu ".." yolunu (tek başına) Clean sınamasından geçirir; ayna olarak biz de.
		with(okSpec(), obj{"dockerfile_path": ".."}),
		// protojson sayıları metin olarak da kabul eder.
		with(okSpec(), obj{"container_port": "3000", "replicas": "2"}),
		with(okSpec(), obj{"limits": obj{"memory_bytes": 536870912, "cpu_millis": 1, "blkio_weight": 10}}),
		with(okSpec(), obj{"env": manyEnv}),
	} {
		if _, err := AppPayload(payload(t, spec), Create); err != nil {
			t.Errorf("%d: sunucunun kabul ettiği yük reddedildi: %v", i, err)
		}
	}
	// JSON sayısının başka yazımları da (1.0, 1e3) JavaScript'teki gibi tam sayı.
	raw := `{"app_id":"blog","git_host":"github.com","git_owner":"a","git_repo":"b","git_branch":"main","container_port":3e3,"replicas":1.0,"limits":{"memory_bytes":"1","cpu_millis":1,"blkio_weight":10}}`
	if _, err := AppPayload(json.RawMessage(raw), Create); err != nil {
		t.Errorf("3e3 / 1.0 reddedildi: %v", err)
	}
}

func TestCreateMirrorsServerLimitsAtTheEdges(t *testing.T) {
	envOver := obj{}
	for i := range 201 {
		envOver[fmt.Sprintf("K%d", i)] = "v"
	}
	vols17 := []any{}
	for i := range 17 {
		vols17 = append(vols17, obj{"name": fmt.Sprintf("v%d", i), "mount_path": fmt.Sprintf("/m%d", i)})
	}
	for _, patch := range []obj{
		{"git_host": "localhost"}, {"git_host": "a.b:22"}, {"git_host": "-a.b"}, {"git_repo": "."}, {"git_owner": "a b"},
		{"replicas": 65}, {"container_port": 65536}, {"domain": strings.Repeat("a", 250) + ".com"},
		{"dockerfile_path": "a/../b"}, {"dockerfile_path": "a//b"}, {"dockerfile_path": "a/"}, {"dockerfile_path": "./a"}, {"dockerfile_path": "."},
		{"health_path": "/a\tb"}, {"health_path": "/" + strings.Repeat("a", 512)},
		{"env": envOver},
		{"env": obj{"A": strings.Repeat("x", 32*1024)}},
		{"env": obj{strings.Repeat("A", 257): "x"}},
		{"build_args": obj{"A": "a\x00b"}},
		{"volumes": vols17},
		{"volumes": []any{obj{"name": "a", "mount_path": "/a/../b"}}}, {"volumes": []any{obj{"name": "a", "mount_path": "/"}}}, {"volumes": []any{obj{"name": "a", "mount_path": "/sys"}}},
		{"volumes": []any{obj{"name": "a", "mount_path": "/a/"}}}, {"volumes": []any{obj{"name": "A", "mount_path": "/a"}}}, {"volumes": []any{obj{"name": "a", "mount_path": "/a", "extra": 1}}},
		{"limits": obj{"memory_bytes": "1", "cpu_millis": 1, "blkio_weight": 100, "extra": 1}},
		{"limits": obj{"memory_bytes": "18446744073709551616", "cpu_millis": 1, "blkio_weight": 100}},
		{"limits": obj{"memory_bytes": "0", "cpu_millis": 1, "blkio_weight": 100}},
		{"limits": obj{"memory_bytes": "1", "cpu_millis": 1, "blkio_weight": 9}},
		{"git_branch": ""},
		{"container_port": true},
		{"container_port": 1.5},
	} {
		if _, err := AppPayload(payload(t, with(okSpec(), patch)), Create); err == nil {
			s := fmt.Sprint(patch)
			t.Errorf("sunucunun reddettiği yük kabul edildi: %.120s", s)
		}
	}
}

func TestCreateRejectsShapeAndUnknownFields(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `"x"`, `{"token":"secret"}`, `{"app_id":"../x"}`} {
		if _, err := AppPayload(json.RawMessage(raw), Create); err == nil {
			t.Errorf("%s kabul edildi", raw)
		}
	}
	_, err := AppPayload(json.RawMessage(`{"app_id":"blog","token":"secret"}`), Create)
	mustFail(t, err, "unknown app field", "token")
}

func TestUpdateAcceptsPartialAndRejectsConflicts(t *testing.T) {
	good := obj{"domain": "", "git_branch": "main", "health_path": "", "replicas": 3, "env": obj{"A": ""}, "env_remove": []any{"B"}, "volumes": []any{obj{"name": "d", "mount_path": "/d", "read_only": false}}, "volume_remove": []any{"old"}}
	if _, err := AppPayload(payload(t, good), Update); err != nil {
		t.Errorf("geçerli güncelleme reddedildi: %v", err)
	}
	if _, err := AppPayload(json.RawMessage(`{}`), Update); err != nil {
		t.Errorf("boş güncelleme reddedildi: %v", err)
	}
	for _, update := range []obj{
		{"domain": "a b"}, {"git_branch": ""}, {"git_branch": "a b"}, {"replicas": 65}, {"replicas": 0},
		{"env": obj{"A": "1"}, "env_remove": []any{"A"}}, {"env_remove": []any{"a-b"}}, {"env_remove": "A"}, {"volume_remove": []any{"Up"}},
		{"volumes": []any{obj{"name": "d", "mount_path": "/d"}}, "volume_remove": []any{"d"}}, {"app_id": "x"}, {"limits": obj{"memory_bytes": "1"}},
		{"container_port": 123}, {"domain": nil}, {"replicas": nil},
		{"env": obj{"BIG": strings.Repeat("x", 600000)}},
	} {
		if _, err := AppPayload(payload(t, update), Update); err == nil {
			t.Errorf("kabul edildi: %.120s", fmt.Sprint(update))
		}
	}
	_, err := AppPayload(payload(t, obj{"env": obj{"BIG": strings.Repeat("x", 600000)}}), Update)
	mustFail(t, err, "app payload is too large", "600 KB")
}

func TestPayloadKeepsOrderAndNumberSpelling(t *testing.T) {
	raw := json.RawMessage(`{"env":{"Z":"1","A":"2"},"replicas":"02","domain":""}`)
	o, err := AppPayload(raw, Update)
	if err != nil {
		t.Fatal(err)
	}
	out, err := jsonv.Encode(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Fatalf("kopya sırası ya da yazımı değişti:\n%s\n%s", out, raw)
	}
}

// Arayüzün metin doğrulayıcısıyla aynı tablo (desktop/test/volume-rules.test.ts);
// ikisi de sunucunun internal/api/volumevalidate.go kuralıyla aynı olmalı.
func TestVolumeRulesTable(t *testing.T) {
	v := func(name, mount string, ro bool) obj { return obj{"name": name, "mount_path": mount, "read_only": ro} }
	long4096 := "/" + strings.Repeat("a", 4095)
	many := []any{}
	for i := range 17 {
		many = append(many, v(fmt.Sprintf("v%d", i), fmt.Sprintf("/m%d", i), false))
	}
	rows := []struct {
		why     string
		volumes []any
		ok      bool
	}{
		{"düz yol", []any{v("data", "/data", false)}, true},
		{"salt okunur", []any{v("c", "/a/b", true)}, true},
		{"boşluklu yol", []any{v("x", "/with space/dir", false)}, true},
		{"iki nokta", []any{v("d", "/a:b", false)}, true},
		{"64 karakterlik ad", []any{v(strings.Repeat("a", 64), "/m", false)}, true},
		{"tam 4096 bayt", []any{v("big", long4096, false)}, true},
		{"/procx", []any{v("p", "/procx", false)}, true},
		{"16 hacim", many[:16], true},
		{"kardeş yollar", []any{v("a", "/app/a", false), v("b", "/app/b", false)}, true},
		{"kök", []any{v("r", "/", false)}, false},
		{"/proc", []any{v("p", "/proc", false)}, false},
		{"/proc altı", []any{v("p", "/proc/x", false)}, false},
		{"/sys", []any{v("p", "/sys", false)}, false},
		{"/dev altı", []any{v("p", "/dev/shm", false)}, false},
		{"sondaki eğik çizgi", []any{v("a", "/a/", false)}, false},
		{"çift eğik çizgi", []any{v("a", "//a", false)}, false},
		{"ortada çift", []any{v("a", "/a//b", false)}, false},
		{"nokta parçası", []any{v("a", "/a/./b", false)}, false},
		{"üst dizin", []any{v("a", "/a/../b", false)}, false},
		{"göreli", []any{v("a", "app", false)}, false},
		{"boş yol", []any{v("a", "", false)}, false},
		{"büyük harf", []any{v("Data", "/d", false)}, false},
		{"65 karakter", []any{v(strings.Repeat("a", 65), "/m", false)}, false},
		{"tireyle başlayan", []any{v("-a", "/m", false)}, false},
		{"iç içe", []any{v("a", "/a", false), v("b", "/a/b", false)}, false},
		{"aynı yol", []any{v("a", "/a", false), v("b", "/a", false)}, false},
		{"aynı ad", []any{v("a", "/a", false), v("a", "/b", false)}, false},
		{"17 hacim", many, false},
		{"4097 bayt", []any{v("big", long4096+"a", false)}, false},
		{"2 baytlık 4097", []any{v("big", "/"+strings.Repeat("é", 2048), false)}, false},
		{"read_only metin", []any{obj{"name": "a", "mount_path": "/a", "read_only": "yes"}}, false},
	}
	for _, row := range rows {
		_, err := AppPayload(payload(t, obj{"volumes": row.volumes}), Update)
		if (err == nil) != row.ok {
			t.Errorf("%s: hata %v", row.why, err)
		}
	}
}
