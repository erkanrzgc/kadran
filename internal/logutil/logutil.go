// Package logutil, üç binary'nin günlük seviyesini aynı kurallarla belirler.
//
// # Neden ayrı bir paket?
//
// panelyd, panely-exec ve panely bu kararı aynı şekilde vermeli. Üç yerde
// ayrı ayrı yazmak, birinin diğerlerinden sapmasıyla biter — ve sapan
// binary büyük ihtimalle en çok ihtiyaç duyulanı olur.
package logutil

import (
	"log/slog"
	"strconv"
)

// EnvVar, ayrıntılı günlüğü açan ortam değişkenidir.
//
// Bayrağa ek olarak var, çünkü sunucudaki binary'ler systemd tarafından
// başlatılıyor: bayrak eklemek unit dosyasını düzenleyip daemon-reload
// yapmayı gerektirir. `systemctl set-environment` veya bir drop-in ile
// PANELY_DEBUG=1 vermek çok daha kısa bir yoldur.
const EnvVar = "PANELY_DEBUG"

// Level, ayrıntılı günlüğün açık olup olmadığına karar verir.
//
// Bayrak VEYA ortam değişkeni yeterlidir; bayrak açıksa ortam değişkenine
// bakılmaz. getenv parametresi test edilebilirlik içindir.
//
// # Neden varsayılan KAPALI?
//
// Ayrıntılı günlük bu projede sıradan bir kolaylık değil: panelyd ve
// executor konteyner ortam değişkenlerini, istek parametrelerini ve
// çağıran kimliklerini işliyor. Bunlar systemd journal'ına düşerse
// `journalctl` okuyabilen herkes görür — SECURITY.md'de çizilen sınırın
// dışına taşar.
//
// Bu yüzden debug bir TANILAMA anahtarıdır, varsayılan bir çalışma kipi
// değil: operatör açar, sorunu görür, kapatır.
//
// Debug seviyesi denetim günlüğüne YAZILANI DEĞİŞTİRMEZ. Denetim zinciri
// ne olursa olsun aynı kaydı tutar; bu anahtar yalnızca stderr'e ne kadar
// ayrıntı gittiğini belirler. İkisini birbirine bağlamak, tanılama için
// açılan bir anahtarın kalıcı kayda sır yazmasına yol açardı.
func Level(flagEnabled bool, getenv func(string) string) slog.Level {
	if flagEnabled || Enabled(getenv) {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// Enabled, ortam değişkeninin ayrıntılı günlüğü açıp açmadığını söyler.
//
// "1", "true", "yes", "on" (ve büyük harfli hâlleri) açar. Tanınmayan bir
// değer KAPALI sayılır: `PANELY_DEBUG=hayir` yazan biri kapalı bekler.
func Enabled(getenv func(string) string) bool {
	switch v := getenv(EnvVar); v {
	case "", "0":
		return false
	case "yes", "YES", "on", "ON":
		return true
	default:
		enabled, err := strconv.ParseBool(v)
		return err == nil && enabled
	}
}
