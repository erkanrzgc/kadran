// Package confirm, durum değiştiren işlemlerden önce gösterilen yerel onay
// kutusunun içeriği. Metin DOĞRULANMIŞ değerlerden kurulur: arayüzün ne
// yazdığı değil, sunucuya gidecek olan gösterilir. Ortam değişkeni ve
// derleme argümanı DEĞERLERİ hiçbir koşulda kutuya girmez; yalnız adları.
package confirm

import "regexp"

// Language, kutunun dili (görünüm ayarındaki dil).
type Language string

const (
	English Language = "en"
	Turkish Language = "tr"
)

var en = map[string]string{
	"title":                "Kadran",
	"cancel":               "Cancel",
	"action.create":        "Create application",
	"action.update":        "Apply changes",
	"action.deploy":        "Deploy",
	"action.delete":        "Delete permanently",
	"action.rollback":      "Roll back",
	"action.backup":        "Create backup",
	"message.create":       "Create application {app}?",
	"message.update":       "Change application {app}?",
	"message.deploy":       "Deploy {app}?",
	"message.delete":       "Delete application {app}?",
	"message.rollback":     "Roll back {app}?",
	"message.backup":       "Create a backup now?",
	"line.server":          "Server: {target}",
	"line.ref":             "Ref: {ref}",
	"line.ref_default":     "Ref: server default",
	"line.source":          "Source: {host}/{owner}/{repo}",
	"line.branch":          "Branch: {branch}",
	"line.domain":          "Domain: {domain}",
	"line.no_domain":       "Domain: none",
	"line.port":            "Container port: {port}",
	"line.replicas":        "Replicas: {count}",
	"line.health":          "Health path: {path}",
	"line.health_none":     "Health path: none",
	"line.dockerfile":      "Dockerfile: {path}",
	"line.limits":          "Limits: memory {memory}, CPU {cpu} millicores, I/O weight {io}",
	"line.build_args":      "Build arguments set (values are not shown): {keys}",
	"line.volume":          "Volume: {name} at {path} ({mode})",
	"mode.ro":              "read-only",
	"mode.rw":              "read-write",
	"line.env_set":         "Environment variables set (values are not shown): {keys}",
	"line.env_remove":      "Environment variables removed: {keys}",
	"line.volume_detach":   "Volumes detached (their data stays on disk): {names}",
	"line.more":            "and {count} more",
	"line.deploy_effect":   "Builds this ref and moves traffic once it is healthy.",
	"line.rollback_effect": "Moves traffic back to the previous release.",
	"line.delete_effect":   "This cannot be undone. The definition, release history and containers are removed; named volume data stays on disk.",
	"line.backup_effect":   "The oldest backups may be deleted to stay within the retention limit.",
}

var tr = map[string]string{
	"title":                "Kadran",
	"cancel":               "Vazgeç",
	"action.create":        "Uygulamayı oluştur",
	"action.update":        "Değişiklikleri uygula",
	"action.deploy":        "Dağıt",
	"action.delete":        "Kalıcı olarak sil",
	"action.rollback":      "Geri al",
	"action.backup":        "Yedek oluştur",
	"message.create":       "{app} uygulaması oluşturulsun mu?",
	"message.update":       "{app} uygulaması değiştirilsin mi?",
	"message.deploy":       "{app} dağıtılsın mı?",
	"message.delete":       "{app} uygulaması silinsin mi?",
	"message.rollback":     "{app} geri alınsın mı?",
	"message.backup":       "Şimdi yedek oluşturulsun mu?",
	"line.server":          "Sunucu: {target}",
	"line.ref":             "Sürüm referansı: {ref}",
	"line.ref_default":     "Sürüm referansı: sunucu varsayılanı",
	"line.source":          "Kaynak: {host}/{owner}/{repo}",
	"line.branch":          "Dal: {branch}",
	"line.domain":          "Alan adı: {domain}",
	"line.no_domain":       "Alan adı: yok",
	"line.port":            "Konteyner portu: {port}",
	"line.replicas":        "Kopya sayısı: {count}",
	"line.health":          "Sağlık yolu: {path}",
	"line.health_none":     "Sağlık yolu: yok",
	"line.dockerfile":      "Dockerfile: {path}",
	"line.limits":          "Sınırlar: bellek {memory}, CPU {cpu} milicore, G/Ç ağırlığı {io}",
	"line.build_args":      "Ayarlanan derleme argümanları (değerler gösterilmez): {keys}",
	"line.volume":          "Hacim: {name}, {path} yolunda ({mode})",
	"mode.ro":              "salt okunur",
	"mode.rw":              "okuma-yazma",
	"line.env_set":         "Ayarlanan ortam değişkenleri (değerler gösterilmez): {keys}",
	"line.env_remove":      "Kaldırılan ortam değişkenleri: {keys}",
	"line.volume_detach":   "Ayrılan hacimler (verileri diskte kalır): {names}",
	"line.more":            "ve {count} tane daha",
	"line.deploy_effect":   "Bu referansı derler ve sağlıklı olunca trafiği taşır.",
	"line.rollback_effect": "Trafiği önceki sürüme geri taşır.",
	"line.delete_effect":   "Bu işlem geri alınamaz. Tanım, sürüm geçmişi ve konteynerler kaldırılır; adlandırılmış hacim verisi diskte kalır.",
	"line.backup_effect":   "Saklama sınırı içinde kalmak için en eski yedekler silinebilir.",
}

var catalog = map[Language]map[string]string{English: en, Turkish: tr}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// text, anahtarın metnini doldurur. Tek geçişte: eklenen değerin içindeki
// {…} yeniden işlenmez. Bilinmeyen dil İngilizceye düşer.
func text(language Language, key string, params map[string]string) string {
	table, ok := catalog[language]
	if !ok {
		table = en
	}
	return placeholder.ReplaceAllStringFunc(table[key], func(whole string) string {
		name := whole[1 : len(whole)-1]
		if v, ok := params[name]; ok {
			return v
		}
		return whole
	})
}
