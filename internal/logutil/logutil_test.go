package logutil

import (
	"log/slog"
	"testing"
)

func envFunc(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// TestDefaultIsInfo, ayrıntılı günlüğün VARSAYILAN OLARAK KAPALI olduğunu
// doğrular.
//
// Bu testin varlık sebebi güvenliktir, üslup değil: panelyd ve executor
// konteyner ortam değişkenlerini ve çağıran kimliklerini işliyor. Debug
// varsayılan açık olsaydı bunlar systemd journal'ına düşer ve
// `journalctl` okuyabilen herkes görürdü.
func TestDefaultIsInfo(t *testing.T) {
	got := Level(false, envFunc(nil))
	if got != slog.LevelInfo {
		t.Errorf("varsayılan seviye %v, Info bekleniyordu — "+
			"debug varsayılan açık olursa sırlar journal'a düşer", got)
	}
}

func TestFlagEnablesDebug(t *testing.T) {
	if got := Level(true, envFunc(nil)); got != slog.LevelDebug {
		t.Errorf("-debug bayrağıyla seviye %v, Debug bekleniyordu", got)
	}
}

func TestEnvVarEnablesDebug(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "True", "yes", "YES", "on", "ON", "t"} {
		got := Level(false, envFunc(map[string]string{EnvVar: v}))
		if got != slog.LevelDebug {
			t.Errorf("%s=%q ile seviye %v, Debug bekleniyordu", EnvVar, v, got)
		}
	}
}

// TestUnrecognisedEnvValueStaysOff, tanınmayan değerin KAPALI sayıldığını
// doğrular.
//
// `PANELY_DEBUG=hayir` yazan biri kapalı bekler. "Boş değilse aç"
// mantığı bunu sessizce açardı.
func TestUnrecognisedEnvValueStaysOff(t *testing.T) {
	for _, v := range []string{"", "0", "false", "FALSE", "no", "off", "hayir", "kapali", "  "} {
		got := Level(false, envFunc(map[string]string{EnvVar: v}))
		if got != slog.LevelInfo {
			t.Errorf("%s=%q ile seviye %v, Info bekleniyordu", EnvVar, v, got)
		}
	}
}

// TestOtherEnvVarsAreIgnored, yalnızca PANELY_DEBUG'a bakıldığını doğrular.
func TestOtherEnvVarsAreIgnored(t *testing.T) {
	got := Level(false, envFunc(map[string]string{"DEBUG": "1", "VERBOSE": "1"}))
	if got != slog.LevelInfo {
		t.Errorf("ilgisiz ortam değişkenleri debug'ı açtı: %v", got)
	}
}
