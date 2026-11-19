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
	"title":                         "Kadran",
	"cancel":                        "Cancel",
	"action.create":                 "Create application",
	"action.update":                 "Apply changes",
	"action.deploy":                 "Deploy",
	"action.delete":                 "Delete permanently",
	"action.rollback":               "Roll back",
	"action.backup":                 "Create backup",
	"action.github-disconnect":      "Disconnect",
	"title.github-disconnect":       "Disconnect GitHub",
	"message.github-disconnect":     "Disconnect GitHub from {target}? Private builds stop until you connect again.",
	"line.github_disconnect_effect": "The token is deleted on the server. The grant on GitHub stays until you revoke it there.",
	"message.create":                "Create application {app}?",
	"message.update":                "Change application {app}?",
	"message.deploy":                "Deploy {app}?",
	"message.delete":                "Delete application {app}?",
	"message.rollback":              "Roll back {app}?",
	"message.backup":                "Create a backup now?",
	"line.server":                   "Server: {target}",
	"line.ref":                      "Ref: {ref}",
	"line.ref_default":              "Ref: server default",
	"line.source":                   "Source: {host}/{owner}/{repo}",
	"line.branch":                   "Branch: {branch}",
	"line.domain":                   "Domain: {domain}",
	"line.no_domain":                "Domain: none",
	"line.port":                     "Container port: {port}",
	"line.replicas":                 "Replicas: {count}",
	"line.health":                   "Health path: {path}",
	"line.health_none":              "Health path: none",
	"line.dockerfile":               "Dockerfile: {path}",
	"line.limits":                   "Limits: memory {memory}, CPU {cpu} millicores, I/O weight {io}",
	"line.build_args":               "Build arguments set (values are not shown): {keys}",
	"line.volume":                   "Volume: {name} at {path} ({mode})",
	"mode.ro":                       "read-only",
	"mode.rw":                       "read-write",
	"line.env_set":                  "Environment variables set (values are not shown): {keys}",
	"line.env_remove":               "Environment variables removed: {keys}",
	"line.volume_detach":            "Volumes detached (their data stays on disk): {names}",
	"line.more":                     "and {count} more",
	"line.deploy_effect":            "Builds this ref and moves traffic once it is healthy.",
	"line.rollback_effect":          "Moves traffic back to the previous release.",
	"line.delete_effect":            "This cannot be undone. The definition, release history and containers are removed; named volume data stays on disk.",
	"line.backup_effect":            "The oldest backups may be deleted to stay within the retention limit.",
	// Konsol kutusu (BuildConsole) ve komut yoluna göre etki metinleri.
	"console.message.command":  "Run this command on {server}?",
	"console.message.setting":  "Change this console setting?",
	"console.action.command":   "Run",
	"console.action.setting":   "Continue",
	"console.line.admin":       "Admin target: {admin}",
	"console.line.command":     "Command: {line}",
	"console.too_long":         "This line is too long to confirm here; run it in the CLI.",
	"effect.app create":        "Saves a new app definition on the server. Nothing is built until you deploy it.",
	"effect.app update":        "Changes the app definition on the server. Some changes take effect on the next deploy.",
	"effect.deploy":            "Builds the given commit, or the commit the branch resolves to after connecting, and moves traffic once it is healthy.",
	"effect.backup create":     "Takes a database backup now. The oldest backups may be deleted to stay within the retention limit.",
	"effect.prune -dry-run":    "Lists the containers of old releases that would be removed. Removes nothing.",
	"effect.github connect":    "Signs in to GitHub with a device code and saves the token on the server for private builds.",
	"effect.app delete":        "This cannot be undone. The definition, release history and containers are removed; named volume data stays on disk.",
	"effect.rollback":          "Moves traffic back to the previous release.",
	"effect.prune":             "Removes the containers of old releases. This cannot be undone.",
	"effect.github disconnect": "The token is deleted on the server. Private builds stop until you connect again.",
	"effect.totp enable":       "Turns on two-step sign-in. The new secret and recovery codes are shown once, in a separate card.",
	"effect.totp disable":      "Turns off two-step sign-in on this server.",
	"effect.key list":          "Reads the server's authorized keys as the admin user, over your own SSH key. Changes nothing.",
	"effect.key add":           "Adds a deploy-only key to the server's authorized keys as the admin user.",
	"effect.key remove":        "Removes a key from the server's authorized keys as the admin user.",
	"effect.totp reset":        "Turns off two-step sign-in as the admin user, without a code. For a lost authenticator.",
	"effect.console admin":     "Admin commands (key, totp reset) can then run from the console as the admin user over your SSH key, each after typing the server name and confirming here.",
}

var tr = map[string]string{
	"title":                         "Kadran",
	"cancel":                        "Vazgeç",
	"action.create":                 "Uygulamayı oluştur",
	"action.update":                 "Değişiklikleri uygula",
	"action.deploy":                 "Dağıt",
	"action.delete":                 "Kalıcı olarak sil",
	"action.rollback":               "Geri al",
	"action.backup":                 "Yedek oluştur",
	"action.github-disconnect":      "Bağlantıyı kes",
	"title.github-disconnect":       "GitHub bağlantısını kes",
	"message.github-disconnect":     "{target} sunucusunda GitHub bağlantısı kesilsin mi? Yeniden bağlanana kadar özel depo derlemeleri durur.",
	"line.github_disconnect_effect": "Jeton sunucuda silinir. GitHub üzerindeki izin, orada kaldırana dek durur.",
	"message.create":                "{app} uygulaması oluşturulsun mu?",
	"message.update":                "{app} uygulaması değiştirilsin mi?",
	"message.deploy":                "{app} dağıtılsın mı?",
	"message.delete":                "{app} uygulaması silinsin mi?",
	"message.rollback":              "{app} geri alınsın mı?",
	"message.backup":                "Şimdi yedek oluşturulsun mu?",
	"line.server":                   "Sunucu: {target}",
	"line.ref":                      "Sürüm referansı: {ref}",
	"line.ref_default":              "Sürüm referansı: sunucu varsayılanı",
	"line.source":                   "Kaynak: {host}/{owner}/{repo}",
	"line.branch":                   "Dal: {branch}",
	"line.domain":                   "Alan adı: {domain}",
	"line.no_domain":                "Alan adı: yok",
	"line.port":                     "Konteyner portu: {port}",
	"line.replicas":                 "Kopya sayısı: {count}",
	"line.health":                   "Sağlık yolu: {path}",
	"line.health_none":              "Sağlık yolu: yok",
	"line.dockerfile":               "Dockerfile: {path}",
	"line.limits":                   "Sınırlar: bellek {memory}, CPU {cpu} milicore, G/Ç ağırlığı {io}",
	"line.build_args":               "Ayarlanan derleme argümanları (değerler gösterilmez): {keys}",
	"line.volume":                   "Hacim: {name}, {path} yolunda ({mode})",
	"mode.ro":                       "salt okunur",
	"mode.rw":                       "okuma-yazma",
	"line.env_set":                  "Ayarlanan ortam değişkenleri (değerler gösterilmez): {keys}",
	"line.env_remove":               "Kaldırılan ortam değişkenleri: {keys}",
	"line.volume_detach":            "Ayrılan hacimler (verileri diskte kalır): {names}",
	"line.more":                     "ve {count} tane daha",
	"line.deploy_effect":            "Bu referansı derler ve sağlıklı olunca trafiği taşır.",
	"line.rollback_effect":          "Trafiği önceki sürüme geri taşır.",
	"line.delete_effect":            "Bu işlem geri alınamaz. Tanım, sürüm geçmişi ve konteynerler kaldırılır; adlandırılmış hacim verisi diskte kalır.",
	"line.backup_effect":            "Saklama sınırı içinde kalmak için en eski yedekler silinebilir.",
	"console.message.command":       "Bu komut {server} sunucusunda çalıştırılsın mı?",
	"console.message.setting":       "Bu konsol ayarı değiştirilsin mi?",
	"console.action.command":        "Çalıştır",
	"console.action.setting":        "Devam et",
	"console.line.admin":            "Yönetici hedefi: {admin}",
	"console.line.command":          "Komut: {line}",
	"console.too_long":              "Bu satır burada onaylanamayacak kadar uzun; CLI'da çalıştırın.",
	"effect.app create":             "Sunucuya yeni bir uygulama tanımı kaydeder. Dağıtana kadar hiçbir şey derlenmez.",
	"effect.app update":             "Sunucudaki uygulama tanımını değiştirir. Bazı değişiklikler sonraki dağıtımda geçerli olur.",
	"effect.deploy":                 "Verilen commit'i ya da dalın bağlandıktan sonra çözüleceği commit'i derler ve sağlıklı olunca trafiği taşır.",
	"effect.backup create":          "Şimdi bir veritabanı yedeği alır. Saklama sınırı içinde kalmak için en eski yedekler silinebilir.",
	"effect.prune -dry-run":         "Eski sürümlerin kaldırılacak konteynerlerini listeler. Hiçbir şey kaldırmaz.",
	"effect.github connect":         "Cihaz koduyla GitHub'a girer ve özel depo derlemeleri için jetonu sunucuya kaydeder.",
	"effect.app delete":             "Bu işlem geri alınamaz. Tanım, sürüm geçmişi ve konteynerler kaldırılır; adlandırılmış hacim verisi diskte kalır.",
	"effect.rollback":               "Trafiği önceki sürüme geri taşır.",
	"effect.prune":                  "Eski sürümlerin konteynerlerini kaldırır. Bu işlem geri alınamaz.",
	"effect.github disconnect":      "Jeton sunucuda silinir. Yeniden bağlanana kadar özel depo derlemeleri durur.",
	"effect.totp enable":            "İki adımlı girişi açar. Yeni sır ve kurtarma kodları bir kez, ayrı bir kartta gösterilir.",
	"effect.totp disable":           "Bu sunucuda iki adımlı girişi kapatır.",
	"effect.key list":               "Sunucunun yetkili anahtarlarını kendi SSH anahtarınızla, yönetici kullanıcı olarak okur. Hiçbir şeyi değiştirmez.",
	"effect.key add":                "Sunucunun yetkili anahtarlarına, yönetici kullanıcı olarak, yalnız dağıtım yapabilen bir anahtar ekler.",
	"effect.key remove":             "Sunucunun yetkili anahtarlarından, yönetici kullanıcı olarak, bir anahtarı kaldırır.",
	"effect.totp reset":             "İki adımlı girişi kod sormadan, yönetici kullanıcı olarak kapatır. Doğrulayıcı kaybolduğunda kullanılır.",
	"effect.console admin":          "Yönetici komutları (key, totp reset) konsolda SSH anahtarınızla yönetici kullanıcı olarak çalışabilir; her biri sunucu adını yazıp burada onaylamayı ister.",
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
