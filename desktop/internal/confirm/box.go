package confirm

import (
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
	case KindCreate:
		lines = append(lines, createLines(t, names, req.Spec)...)
	case KindUpdate:
		lines = append(lines, updateLines(t, names, req.Update)...)
	}

	return Box{
		Title:   t("title", nil),
		Message: t("message."+string(req.Kind), params{"app": app}),
		Detail:  strings.Join(lines, "\n"),
		Cancel:  t("cancel", nil),
		Action:  t("action."+string(req.Kind), nil),
	}
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
