package confirm

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

func decode(t *testing.T, raw string) *jsonv.Object {
	t.Helper()
	v, err := jsonv.Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	o, ok := v.(*jsonv.Object)
	if !ok {
		t.Fatalf("nesne değil: %s", raw)
	}
	return o
}

// cp, kod noktalarından metin; kaynakta görünmez karakter durmasın.
func cp(points ...rune) string { return string(points) }

// esc, kutudaki görünür kaçış: ters bölü, "u", dört onaltılık hane.
func esc(r rune) string { return fmt.Sprintf("%cu%04x", '\\', r) }

func requests(t *testing.T) []Request {
	return []Request{
		{Kind: KindDeploy, Target: "user@host", App: "blog", Ref: "main"},
		{Kind: KindRollback, Target: "user@host", App: "blog"},
		{Kind: KindDelete, Target: "user@host", App: "blog"},
		{Kind: KindBackup, Target: "user@host"},
		{Kind: KindUpdate, Target: "user@host", App: "blog", Update: decode(t, `{"domain":"x.example.com"}`)},
		{Kind: KindCreate, Target: "user@host", Spec: decode(t, `{"app_id":"blog","git_host":"github.com","git_owner":"a","git_repo":"b","git_branch":"main","container_port":3000}`)},
	}
}

func TestBoxFollowsLanguage(t *testing.T) {
	for _, req := range requests(t) {
		tr, en := Build(Turkish, req), Build(English, req)
		if tr.Cancel != "Vazgeç" || en.Cancel != "Cancel" {
			t.Errorf("%s: vazgeç düğmesi %q / %q", req.Kind, tr.Cancel, en.Cancel)
		}
		if !strings.HasPrefix(tr.Detail, "Sunucu: user@host") || !strings.HasPrefix(en.Detail, "Server: user@host") {
			t.Errorf("%s: sunucu satırı %q / %q", req.Kind, tr.Detail, en.Detail)
		}
		if tr.Action == en.Action || tr.Action == "" {
			t.Errorf("%s: eylem düğmesi %q / %q", req.Kind, tr.Action, en.Action)
		}
	}
	if got := Build(Language("de"), requests(t)[0]); got.Cancel != "Cancel" {
		t.Errorf("bilinmeyen dil İngilizceye düşmeli: %q", got.Cancel)
	}
}

func TestValuesCannotReexpandPlaceholdersOrHideInvisible(t *testing.T) {
	out := Build(English, Request{Kind: KindDeploy, Target: "user@host", App: "blog", Ref: "{app}" + cp(0x202e)})
	if !strings.Contains(out.Detail, "Ref: {app}"+esc(0x202e)) {
		t.Fatalf("detail %q", out.Detail)
	}
	if strings.ContainsRune(out.Detail, 0x202e) {
		t.Fatal("bidi karakteri kutuya girdi")
	}
}

func TestLongListsAreCounted(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"env":{`)
	for i := range 30 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"KEY_%d":"v"`, i)
	}
	b.WriteString(`}}`)
	out := Build(English, Request{Kind: KindUpdate, Target: "t", App: "blog", Update: decode(t, b.String())})
	if !strings.Contains(out.Detail, "KEY_0, KEY_1, KEY_2") || !strings.Contains(out.Detail, "KEY_9, and 20 more") {
		t.Fatalf("sıra ya da sayım: %q", out.Detail)
	}
	if strings.Contains(out.Detail, "KEY_29") || strings.Contains(out.Detail, "KEY_10") {
		t.Fatalf("on birinci öğe gösterildi: %q", out.Detail)
	}
}

func TestCreateShowsLimitsAndNamesNeverValues(t *testing.T) {
	spec := `{"app_id":"blog","git_host":"github.com","git_owner":"a","git_repo":"b","git_branch":"main","container_port":3000,
		"replicas":64,"health_path":"/healthz","limits":{"memory_bytes":"8589934592","cpu_millis":4000,"blkio_weight":500},
		"build_args":{"NPM_TOKEN":"build-secret-7c1"},"env":{"DB_PASS":"env-secret-9"}}`
	en := Build(English, Request{Kind: KindCreate, Target: "user@host", Spec: decode(t, spec)}).Detail
	for _, want := range []string{"Replicas: 64", "Health path: /healthz", "Limits: memory 8192 MiB, CPU 4000 millicores, I/O weight 500", "Build arguments set (values are not shown): NPM_TOKEN", "Environment variables set (values are not shown): DB_PASS", "Domain: none", "Container port: 3000"} {
		if !strings.Contains(en, want) {
			t.Errorf("%q yok:\n%s", want, en)
		}
	}
	tr := Build(Turkish, Request{Kind: KindCreate, Target: "user@host", Spec: decode(t, spec)}).Detail
	if !strings.Contains(tr, "Kopya sayısı: 64") {
		t.Errorf("tr: %s", tr)
	}
	for _, secret := range []string{"build-secret-7c1", "env-secret-9"} {
		if strings.Contains(en, secret) || strings.Contains(tr, secret) {
			t.Fatalf("değer kutuya girdi: %s", secret)
		}
	}
	bare := Build(English, Request{Kind: KindCreate, Target: "user@host", Spec: decode(t, `{"app_id":"blog","health_path":"","build_args":{}}`)}).Detail
	if !strings.Contains(bare, "Health path: none") || strings.Contains(bare, "Build arguments") {
		t.Errorf("boş alanlar: %s", bare)
	}
}

func TestLimitsInEveryAcceptedForm(t *testing.T) {
	base := `"app_id":"blog","git_host":"github.com","git_owner":"a","git_repo":"b","git_branch":"main","container_port":3000`
	asNumber := Build(English, Request{Kind: KindCreate, Target: "u@h", Spec: decode(t, `{`+base+`,"replicas":1,"limits":{"memory_bytes":9007199254740991,"cpu_millis":"0100","blkio_weight":10}}`)}).Detail
	if !strings.Contains(asNumber, "Limits: memory 9007199254740991 B, CPU 100 millicores, I/O weight 10") {
		t.Errorf("sayı: %s", asNumber)
	}
	asText := Build(English, Request{Kind: KindCreate, Target: "u@h", Spec: decode(t, `{`+base+`,"replicas":"064","limits":{"memory_bytes":"1048576","cpu_millis":1,"blkio_weight":10}}`)}).Detail
	if !strings.Contains(asText, "Limits: memory 1 MiB, CPU 1 millicores") || !strings.Contains(asText, "Replicas: 64") {
		t.Errorf("metin: %s", asText)
	}
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

func TestLongTargetKeepsHostVisible(t *testing.T) {
	target := strings.Repeat("u", 400) + "@prod.example.com"
	line := firstLine(Build(English, Request{Kind: KindBackup, Target: target}).Detail)
	if !strings.HasSuffix(line, "@prod.example.com") || !strings.ContainsRune(line, ellipsis) {
		t.Fatalf("satır %q", line)
	}
	if units(line) > len("Server: ")+201 {
		t.Fatalf("satır %d birim", units(line))
	}
}

func TestEscapeBeforeTruncate(t *testing.T) {
	ref := strings.Repeat(cp(0x202e), 500)
	detail := Build(English, Request{Kind: KindDeploy, Target: "user@host", App: "blog", Ref: ref}).Detail
	var line string
	for _, l := range strings.Split(detail, "\n") {
		if strings.HasPrefix(l, "Ref: ") {
			line = l
		}
	}
	if units(line) > len("Ref: ")+201 {
		t.Fatalf("satır %d birim", units(line))
	}
	for _, side := range strings.Split(strings.TrimPrefix(line, "Ref: "), string(rune(ellipsis))) {
		if strings.ReplaceAll(side, esc(0x202e), "") != "" {
			t.Fatalf("bir kaçış yarıda kesildi: %q", side)
		}
	}
}

func TestBlankLookingCombiningAndUnassignedAreEscaped(t *testing.T) {
	// Hedef yalnız Cc/Cf'yi reddeder: U+3164 ile host'u boş gösteren bir hedef kutuda görünmeli.
	target := "user@" + cp(0x3164, 0x2800, 0x115f) + "e" + cp(0x301, 0xe000, 0x378)
	want := "user@" + esc(0x3164) + esc(0x2800) + esc(0x115f) + "e" + esc(0x301) + esc(0xe000) + esc(0x378)
	if got := Build(English, Request{Kind: KindBackup, Target: target}).Detail; !strings.HasPrefix(got, "Server: "+want) {
		t.Fatalf("detail %q", got)
	}
}

func TestLiteralEllipsisIsEscaped(t *testing.T) {
	got := Build(English, Request{Kind: KindBackup, Target: "user@a" + cp(ellipsis) + "b"}).Detail
	if !strings.HasPrefix(got, "Server: user@a"+esc(ellipsis)+"b") {
		t.Fatalf("detail %q", got)
	}
}

func TestSeparatorsAndUnusualSpacesAreEscaped(t *testing.T) {
	ref := "a" + cp(0x2028) + "b" + cp(0x2029) + "c" + cp(0xa0) + "d" + cp(0x3000) + "e f"
	want := "a" + esc(0x2028) + "b" + esc(0x2029) + "c" + esc(0xa0) + "d" + esc(0x3000) + "e f"
	if got := Build(English, Request{Kind: KindDeploy, Target: "user@host", App: "blog", Ref: ref}).Detail; !strings.Contains(got, "Ref: "+want) {
		t.Fatalf("detail %q", got)
	}
}

func TestUpdateLinesForEveryField(t *testing.T) {
	u := decode(t, `{"domain":"","git_branch":"dev","health_path":"/h","replicas":2,"env":{"A":"1"},"env_remove":["B"],"volumes":[{"name":"d","mount_path":"/d","read_only":true}],"volume_remove":["old"]}`)
	got := Build(English, Request{Kind: KindUpdate, Target: "t", App: "blog", Update: u}).Detail
	want := strings.Join([]string{"Server: t", "Domain: none", "Branch: dev", "Health path: /h", "Replicas: 2",
		"Environment variables set (values are not shown): A", "Environment variables removed: B",
		"Volume: d at /d (read-only)", "Volumes detached (their data stays on disk): old"}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Katalog: iki dilde aynı anahtarlar, aynı yer tutucular, İngilizcede Türkçe harf yok.
func TestCatalogParity(t *testing.T) {
	enKeys, trKeys := slices.Sorted(maps.Keys(en)), slices.Sorted(maps.Keys(tr))
	if !slices.Equal(enKeys, trKeys) {
		t.Fatalf("anahtarlar farklı:\n%v\n%v", enKeys, trKeys)
	}
	turkish := regexp.MustCompile(`[çğıöşüÇĞİÖŞÜ]`)
	for _, k := range enKeys {
		ph := func(s string) []string { return slices.Sorted(slices.Values(placeholder.FindAllString(s, -1))) }
		if !slices.Equal(ph(en[k]), ph(tr[k])) {
			t.Errorf("%s: yer tutucular farklı", k)
		}
		if turkish.MatchString(en[k]) {
			t.Errorf("%s: İngilizce metinde Türkçe harf", k)
		}
	}
}
