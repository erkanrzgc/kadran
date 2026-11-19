package confirm

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
	"github.com/erkanrzgc/kadran/internal/cli"
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
		{Kind: KindGitHubDisconnect, Target: "user@host"},
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
		// İki dilde aynı değer yalnız dilden bağımsız anahtarlarda.
		if same := en[k] == tr[k]; same != (k == "title" || k == "line.dockerfile") {
			t.Errorf("%s: iki dilde aynı mı = %v (çevrilmemiş ya da listede eskimiş)", k, same)
		}
	}
}

func TestGitHubDisconnectBoxTexts(t *testing.T) {
	req := Request{Kind: KindGitHubDisconnect, Target: "user@host"}
	en := Build(English, req)
	if en.Title != "Disconnect GitHub" || en.Action != "Disconnect" || en.Cancel != "Cancel" ||
		en.Message != "Disconnect GitHub from user@host? Private builds stop until you connect again." {
		t.Fatalf("%+v", en)
	}
	tr := Build(Turkish, req)
	if tr.Title == en.Title || tr.Action == en.Action || !strings.Contains(tr.Message, "user@host") {
		t.Fatalf("%+v", tr)
	}
	// Hedefteki görünmez karakter kutuda kaçışla görünür.
	hostile := Build(English, Request{Kind: KindGitHubDisconnect, Target: "h" + cp(0x202e) + "x"})
	if strings.ContainsRune(hostile.Message, 0x202e) || !strings.Contains(hostile.Message, esc(0x202e)) {
		t.Fatalf("%q", hostile.Message)
	}
}

// consoleEffectKeys, onaylı sınıflardaki (değiştirme, tehlikeli, root) her
// yaprağın yolunu cli.Commands()'tan toplar; yeni bir komut etkisi yazılmadan
// eklenirse liste büyür ve test düşer. Ağacın göstermediği iki anahtar
// (prune'ın dry-run kipi ve yönetici ayarı) elle eklenir.
func consoleEffectKeys() []string {
	keys := []string{"prune -dry-run", "console admin"}
	var walk func(prefix []string, cmds []cli.Command)
	walk = func(prefix []string, cmds []cli.Command) {
		for _, c := range cmds {
			path := append(slices.Clone(prefix), c.Name)
			if len(c.Subs) > 0 {
				walk(path, c.Subs)
				continue
			}
			switch c.Class {
			case cli.ClassChange, cli.ClassDanger, cli.ClassRoot:
				keys = append(keys, strings.Join(path, " "))
			}
		}
	}
	walk(nil, cli.Commands())
	return keys
}

func TestBuildConsoleEffectsComplete(t *testing.T) {
	keys := consoleEffectKeys()
	// 5 değiştirme yaprağı + prune -dry-run, 6 tehlikeli, 4 root, 1 ayar.
	if len(keys) < 17 {
		t.Fatalf("yalnız %d anahtar: %v", len(keys), keys)
	}
	for _, key := range keys {
		enText, enOK := ConsoleEffect(English, key)
		trText, trOK := ConsoleEffect(Turkish, key)
		if !enOK || !trOK || enText == "" || trText == "" {
			t.Errorf("%q: etki metni eksik (en %v, tr %v)", key, enOK, trOK)
			continue
		}
		if enText == trText {
			t.Errorf("%q: çevrilmemiş", key)
		}
		// safe ortadan keserdi: etki cümlesi kutuda yarım görünmesin.
		if units(enText) > maxValueChars || units(trText) > maxValueChars {
			t.Errorf("%q: etki metni %d karakterden uzun", key, maxValueChars)
		}
	}
	if _, ok := ConsoleEffect(English, "status"); ok {
		t.Error("okuma komutunun etki metni olmamalı")
	}
}

func TestBuildConsoleShowsFieldsSafely(t *testing.T) {
	req := ConsoleRequest{Server: "hetzner", Line: "deploy web" + cp(0x202e), Effect: "E.", Admin: "-sudo ops@h"}
	box := BuildConsole(English, req)
	for _, want := range []string{"Server: hetzner", "Admin target: -sudo ops@h", "Command: deploy web" + esc(0x202e), "E."} {
		if !strings.Contains(box.Detail, want) {
			t.Errorf("%q yok:\n%s", want, box.Detail)
		}
	}
	if strings.ContainsRune(box.Detail+box.Message, 0x202e) {
		t.Fatal("bidi karakteri kutuya girdi")
	}
	if box.Cancel != "Cancel" || box.Action == "" || !strings.Contains(box.Message, "hetzner") {
		t.Fatalf("%+v", box)
	}
	trBox := BuildConsole(Turkish, req)
	if trBox.Cancel != "Vazgeç" || !strings.Contains(trBox.Detail, "Sunucu: hetzner") || trBox.Action == box.Action {
		t.Fatalf("%+v", trBox)
	}
	// Komut satırı HİÇ kesilmez (R10): 500 karakterlik sözcük ve sonrası
	// kutuda bütün, "…" yok.
	line := strings.Repeat("a", 500) + " web" + cp(0x202e) + " -domain evil.example"
	long := BuildConsole(English, ConsoleRequest{Server: "s", Line: line, Effect: "E."})
	whole := "Command: " + strings.Repeat("a", 500) + " web" + esc(0x202e) + " -domain evil.example"
	if !strings.Contains(unwrap(long.Detail), whole) {
		t.Fatalf("satır bütün değil:\n%s", long.Detail)
	}
	if strings.ContainsRune(long.Detail, ellipsis) || strings.Contains(long.Detail, "more") {
		t.Fatalf("satır kesildi ya da kısaltıldı:\n%s", long.Detail)
	}
	noAdmin := BuildConsole(English, ConsoleRequest{Server: "s", Line: "deploy web", Effect: "E."})
	if strings.Contains(noAdmin.Detail, "Admin target") {
		t.Fatalf("root olmayan kutuda yönetici hedefi: %q", noAdmin.Detail)
	}
}

// unwrap, Detail'in satır kırmalarını kaldırır: kırma hiçbir karakter
// düşürmediği için mantıksal satırlar art arda birleşir.
func unwrap(detail string) string { return strings.ReplaceAll(detail, "\n", "") }

func TestBuildConsoleRowsAreShort(t *testing.T) {
	line := strings.Repeat("deploy web -branch feature/x ", 20) + strings.Repeat(cp(0x202e), 40) + strings.Repeat("b", 300)
	req := ConsoleRequest{
		Server: strings.Repeat("s", 300),
		Admin:  "-sudo " + strings.Repeat("u", 150) + "@host.example",
		Line:   line,
		Effect: strings.Repeat("effect ", 40),
	}
	for _, lang := range []Language{English, Turkish} {
		box := BuildConsole(lang, req)
		for i, row := range strings.Split(box.Detail, "\n") {
			if units(row) > 96 {
				t.Fatalf("%s satır %d: %d birim > 96: %q", lang, i, units(row), row)
			}
		}
		// Kaçışlar satır sınırında bölünmez: 40 kaçış bütün sayılır.
		if n := strings.Count(unwrap(box.Detail), esc(0x202e)); n != 40 {
			t.Fatalf("%s: %d bütün kaçış, 40 olmalı", lang, n)
		}
		if !strings.Contains(unwrap(box.Detail), strings.Repeat("b", 300)) {
			t.Fatalf("%s: uzun sözcük kayboldu", lang)
		}
	}
	// Kırma boşlukta: sözcükler satır sınırında bölünmez.
	words := BuildConsole(English, ConsoleRequest{Line: strings.TrimSpace(strings.Repeat("word ", 60))})
	wholeWords := regexp.MustCompile(`^(Command: )?(word ?)+$`)
	for i, row := range strings.Split(words.Detail, "\n") {
		if !wholeWords.MatchString(row) {
			t.Fatalf("satır %d sözcüğü böldü: %q", i, row)
		}
	}
	if got, want := ConsoleLineUnits("a"+cp(0x202e)), 1+6; got != want {
		t.Fatalf("ConsoleLineUnits %d, want %d (kaçış 6 birim)", got, want)
	}
	if ConsoleTooLong(English) == ConsoleTooLong(Turkish) || ConsoleTooLong(English) != "This line is too long to confirm here; run it in the CLI." {
		t.Fatalf("ret metni: %q / %q", ConsoleTooLong(English), ConsoleTooLong(Turkish))
	}
}

func TestBuildConsoleSettingBox(t *testing.T) {
	effect, _ := ConsoleEffect(English, "console admin")
	box := BuildConsole(English, ConsoleRequest{Effect: effect})
	if strings.Contains(box.Detail, "Server:") || strings.Contains(box.Detail, "Command:") {
		t.Fatalf("ayar kutusunda sunucu/komut satırı: %q", box.Detail)
	}
	if !strings.Contains(unwrap(box.Detail), effect) || box.Message == "" || box.Cancel != "Cancel" {
		t.Fatalf("%+v", box)
	}
}
