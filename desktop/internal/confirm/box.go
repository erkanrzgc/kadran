package confirm

import (
	"slices"
	"strconv"
	"strings"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

type params = map[string]string

// Build, isteğin onay kutusunu kurar. Değerler safe'den geçer; listeler
// en çok on öğe gösterir ve kalanını sayar.
func Build(language Language, req Request) Box {
	t := func(key string, p params) string { return text(language, key, p) }
	names := func(items []string) string {
		shown := ""
		for i, item := range items {
			if i == maxListItems {
				break
			}
			if i > 0 {
				shown += ", "
			}
			shown += safe(item)
		}
		if len(items) > maxListItems {
			shown += ", " + t("line.more", params{"count": strconv.Itoa(len(items) - maxListItems)})
		}
		return shown
	}

	lines := []string{t("line.server", params{"target": safe(req.Target)})}
	app := safe(req.App)
	switch req.Kind {
	case KindCreate:
		app = safe(get(req.Spec, "app_id"))
	case KindBackup:
		app = ""
	}

	switch req.Kind {
	case KindDeploy:
		if req.Ref == "" {
			lines = append(lines, t("line.ref_default", nil))
		} else {
			lines = append(lines, t("line.ref", params{"ref": safe(req.Ref)}))
		}
		lines = append(lines, t("line.deploy_effect", nil))
	case KindRollback:
		lines = append(lines, t("line.rollback_effect", nil))
	case KindDelete:
		lines = append(lines, t("line.delete_effect", nil))
	case KindBackup:
		lines = append(lines, t("line.backup_effect", nil))
	case KindGitHubDisconnect:
		lines = append(lines, t("line.github_disconnect_effect", nil))
	case KindCreate:
		lines = append(lines, createLines(t, names, req.Spec)...)
	case KindUpdate:
		lines = append(lines, updateLines(t, names, req.Update)...)
	}

	title := t("title", nil)
	if req.Kind == KindGitHubDisconnect {
		title = t("title."+string(req.Kind), nil)
	}
	return Box{
		Title:   title,
		Message: t("message."+string(req.Kind), params{"app": app, "target": safe(req.Target)}),
		Detail:  strings.Join(lines, "\n"),
		Cancel:  t("cancel", nil),
		Action:  t("action."+string(req.Kind), nil),
	}
}

// ConsoleRequest, konsoldan çalıştırılacak bir komutun onay bilgisi.
//   - Line arıtılmış satırdır (-env değerleri ***), eklenen -s olmadan.
//   - Admin root hedefidir (root@… ya da -sudo kullanıcı@…); root dışında boş.
//   - Effect, komut yoluna göre ConsoleEffect'ten gelir.
//
// Line boşsa kutu bir konsol ayarı içindir (yönetici komutlarını açmak).
type ConsoleRequest struct{ Server, Line, Effect, Admin string }

const (
	// MaxConsoleLineUnits, kutuda gösterilebilen (kaçışlı, arıtılmış) komut
	// satırının en çok UTF-16 birimi. Daha uzun satır kutuya hiç gelmez:
	// çalıştırıcı onu kutu kurmadan reddeder (ConsoleTooLong).
	MaxConsoleLineUnits = 1024
	// maxRowUnits, Detail'in bir satırının en çok UTF-16 birimi (girinti dahil).
	maxRowUnits = 96
	// maxWordUnits, Detail içinde boşluksuz bir koşunun en çok UTF-16 birimi.
	// TaskDialog boşluksuz sözcüğü sarmaz, sığmazsa sonunu "…" ile kısaltır:
	// canlıda (%100 ölçek) 96 birimlik W koşusunda ~55 W sığdı. 32, tam genişlik
	// harflerde de pay bırakır; ölçek büyüdükçe kutu ve harf birlikte büyüdüğü
	// için oran değişmez. Daha uzun koşu bu uzunlukta parçalara bölünür.
	maxWordUnits = 32
	// rowIndent, sarılan bir alanın devam satırlarının girintisi. 0. sütunda
	// yalnız alanların etiket satırları başlar: kullanıcı değeri ("Server:
	// staging" içeren bir -health-path gibi) devam satırında etiket gibi
	// okunamaz.
	rowIndent = "    "
)

// BuildConsole, konsol komutunun onay kutusunu kurar. Konsol, GUI işlemleri
// için de bu tek biçimi kullanır (plan kararı): bayraklardan Spec/Update
// yeniden kurmak, CLI'ın bayrak → istek çevirisini ikinci kez yazmak olurdu.
// Ortak kalan: kaçış ve varsayılan düğme Cancel.
//
// Komut satırı HİÇ kesilmez (R10): değiştirme sınıfında tek kapı bu kutu ve
// bayraklarda son yazılan kazanır; ortadan kesilen satırda ikinci bir
// `-domain` görünmeden onaylanırdı. Satır kaçıştan geçer, kesilmeden en çok
// maxRowUnits birimlik satırlara bölünür; uzunluk sınırı çalıştırıcıda
// (MaxConsoleLineUnits). Sunucu, yönetici hedefi ve etki kısa: safe'den geçer.
// Her alan etiketiyle başlar (etki de); devam satırları rowIndent ile girintili.
func BuildConsole(language Language, req ConsoleRequest) Box {
	t := func(key string, p params) string { return text(language, key, p) }
	var rows []string
	add := func(label string, parts []string) {
		rows = append(rows, wrapParts(append(escapeParts(label), parts...))...)
	}
	if req.Server != "" {
		add(t("line.server", params{"target": ""}), safeParts(req.Server))
	}
	if req.Admin != "" {
		add(t("console.line.admin", params{"admin": ""}), safeParts(req.Admin))
	}
	if req.Line != "" {
		add(t("console.line.command", params{"line": ""}), escapeParts(req.Line))
	}
	if req.Effect != "" {
		add(t("console.line.effect", params{"effect": ""}), safeParts(req.Effect))
	}
	kind := "command"
	if req.Line == "" {
		kind = "setting"
	}
	return Box{
		Title:   t("title", nil),
		Message: t("console.message."+kind, params{"server": safe(req.Server)}),
		Detail:  strings.Join(rows, "\n"),
		Cancel:  t("cancel", nil),
		Action:  t("console.action."+kind, nil),
	}
}

// ConsoleLineUnits, satırın kutuda göstereceği kaçışlı uzunluk (UTF-16).
func ConsoleLineUnits(line string) int { return partsUnits(escapeParts(line)) }

// ConsoleTooLong, MaxConsoleLineUnits'i aşan satırın ret metni.
func ConsoleTooLong(language Language) string { return text(language, "console.too_long", nil) }

// wrapParts, parçaları en çok maxRowUnits birimlik satırlara dizer; ilk
// satırdan sonrakiler rowIndent ile başlar (girinti sınıra dahil). Kırma son
// boşluktan sonra yapılır (boşluk satır sonunda kalır: hiçbir karakter
// düşmez); taşınan sözcük girintili satıra sığmıyorsa ya da boşluk yoksa
// parça sınırında sert kırılır, kaçış bölünmez. Boşluksuz koşu ayrıca
// maxWordUnits birimde sert kırılır (TaskDialog uzun sözcüğü sarmaz, kısaltır):
// parça yeni bir girintili satırda sürer. Parçalar rune ya da kaçış olduğundan
// vekil çift ortadan bölünmez.
func wrapParts(parts []string) []string {
	var rows, row []string
	// base, satırın baştaki girinti parçası sayısı (ilk satırda 0); run,
	// satırın sonundaki boşluksuz koşunun birimi.
	width, lastSpace, base, run := 0, -1, 0, 0
	newRow := func(carry []string) {
		rows = append(rows, strings.Join(row, ""))
		row = append([]string{rowIndent}, carry...)
		width, base, lastSpace, run = partsUnits(row), 1, -1, partsUnits(carry)
	}
	for _, p := range parts {
		w := units(p)
		if p != " " && run+w > maxWordUnits && len(row) > base {
			newRow(nil)
		}
		if width+w > maxRowUnits && len(row) > base {
			var carry []string
			if p != " " && lastSpace >= 0 && units(rowIndent)+partsUnits(row[lastSpace+1:])+w <= maxRowUnits {
				carry = slices.Clone(row[lastSpace+1:])
				row = row[:lastSpace+1]
			}
			newRow(carry)
		}
		row, width = append(row, p), width+w
		if p == " " {
			run = 0
			if len(row) > base+1 {
				lastSpace = len(row) - 1
			}
		} else {
			run += w
		}
	}
	if len(row) > base {
		rows = append(rows, strings.Join(row, ""))
	}
	return rows
}

// ConsoleEffect, konsol komutunun ne yapacağını anlatan metni döner. key,
// komut yoludur ("deploy", "app delete"; prune'ın dry-run kipi için
// "prune -dry-run", yönetici ayarı için "console admin"). ok false ise metin
// yok: çalıştırıcı bunu onaysız geçiş değil, ret sayar.
func ConsoleEffect(language Language, key string) (string, bool) {
	table, ok := catalog[language]
	if !ok {
		table = en
	}
	v, ok := table["effect."+key]
	return v, ok && v != ""
}

type textFunc = func(key string, p params) string

func createLines(t textFunc, names func([]string) string, s *jsonv.Object) []string {
	lines := []string{
		t("line.source", params{"host": safe(get(s, "git_host")), "owner": safe(get(s, "git_owner")), "repo": safe(get(s, "git_repo"))}),
		t("line.branch", params{"branch": safe(get(s, "git_branch"))}),
	}
	if truthy(get(s, "domain")) {
		lines = append(lines, t("line.domain", params{"domain": safe(get(s, "domain"))}))
	} else {
		lines = append(lines, t("line.no_domain", nil))
	}
	lines = append(lines, t("line.port", params{"port": count(get(s, "container_port"))}))
	// Kopya sayısı ve sınırlar kaynak tüketimini belirler: 64 kopya ya da
	// dev bir bellek sınırı kutuda görünmeden onaylanmasın.
	lines = append(lines, t("line.replicas", params{"count": count(get(s, "replicas"))}))
	if truthy(get(s, "health_path")) {
		lines = append(lines, t("line.health", params{"path": safe(get(s, "health_path"))}))
	} else {
		lines = append(lines, t("line.health_none", nil))
	}
	limits := record(get(s, "limits"))
	lines = append(lines, t("line.limits", params{"memory": memory(get(limits, "memory_bytes")), "cpu": count(get(limits, "cpu_millis")), "io": count(get(limits, "blkio_weight"))}))
	if truthy(get(s, "dockerfile_path")) {
		lines = append(lines, t("line.dockerfile", params{"path": safe(get(s, "dockerfile_path"))}))
	}
	lines = append(lines, volumeLines(t, list(get(s, "volumes")))...)
	if keys := record(get(s, "env")).Keys(); len(keys) > 0 {
		lines = append(lines, t("line.env_set", params{"keys": names(keys)}))
	}
	if keys := record(get(s, "build_args")).Keys(); len(keys) > 0 {
		lines = append(lines, t("line.build_args", params{"keys": names(keys)}))
	}
	return lines
}

func updateLines(t textFunc, names func([]string) string, u *jsonv.Object) []string {
	var lines []string
	has := func(key string) bool { return u != nil && u.Has(key) }
	if has("domain") {
		if truthy(get(u, "domain")) {
			lines = append(lines, t("line.domain", params{"domain": safe(get(u, "domain"))}))
		} else {
			lines = append(lines, t("line.no_domain", nil))
		}
	}
	if has("git_branch") {
		lines = append(lines, t("line.branch", params{"branch": safe(get(u, "git_branch"))}))
	}
	if has("health_path") {
		if truthy(get(u, "health_path")) {
			lines = append(lines, t("line.health", params{"path": safe(get(u, "health_path"))}))
		} else {
			lines = append(lines, t("line.health_none", nil))
		}
	}
	if has("replicas") {
		lines = append(lines, t("line.replicas", params{"count": count(get(u, "replicas"))}))
	}
	if keys := record(get(u, "env")).Keys(); len(keys) > 0 {
		lines = append(lines, t("line.env_set", params{"keys": names(keys)}))
	}
	if removed := asTexts(list(get(u, "env_remove"))); len(removed) > 0 {
		lines = append(lines, t("line.env_remove", params{"keys": names(removed)}))
	}
	lines = append(lines, volumeLines(t, list(get(u, "volumes")))...)
	if detached := asTexts(list(get(u, "volume_remove"))); len(detached) > 0 {
		lines = append(lines, t("line.volume_detach", params{"names": names(detached)}))
	}
	return lines
}

// asTexts, listenin öğeleri metin olarak (JavaScript'te map(String)).
func asTexts(items []any) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = jsString(item)
	}
	return out
}

func volumeLines(t textFunc, volumes []any) []string {
	var lines []string
	for i, item := range volumes {
		if i == maxListItems {
			break
		}
		v := record(item)
		mode := t("mode.rw", nil)
		if ro, _ := get(v, "read_only").(bool); ro {
			mode = t("mode.ro", nil)
		}
		lines = append(lines, t("line.volume", params{"name": safe(get(v, "name")), "path": safe(get(v, "mount_path")), "mode": mode}))
	}
	if len(volumes) > maxListItems {
		lines = append(lines, t("line.more", params{"count": strconv.Itoa(len(volumes) - maxListItems)}))
	}
	return lines
}
