package guard

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Sınırlar (Electron'daki ipc-validate.ts ve server-list.ts ile aynı).
const (
	MaxTarget   = 500
	MaxCopy     = 1024
	MaxName     = 200
	MaxProvider = 64
	MaxTail     = 10000
)

// maxSafeInteger, JavaScript'te kayıpsız tam sayı sınırı (2^53 - 1).
const maxSafeInteger = 1<<53 - 1

// WindowActions, özel başlık çubuğunun düğmeleri.
var WindowActions = []string{"minimize", "maximize", "close"}

// invisible: denetim (Cc) ve biçim (Cf, bidi dahil) karakterleri yerel onay
// kutusunda "Server:" satırını yanıltabilir; ssh hedefinde zaten anlamsızlar.
var invisible = regexp.MustCompile(`[\p{Cc}\p{Cf}]`)

// HasControlOrFormat, metinde denetim (Cc) ya da biçim (Cf, bidi dahil)
// karakteri olup olmadığını söyler. Konsol satır ayrıştırıcısı da aynı kuralı
// kullanır; kural tek yerde durur.
func HasControlOrFormat(s string) bool {
	return invisible.MatchString(s)
}

var (
	appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// BranchPattern, dal, etiket ya da tam commit; sunucunun kuralıyla aynı.
	BranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

// Target, bir sunucu hedefini doğrular ve kırpılmış hâlini döner.
func Target(value string) (string, error) {
	if jsTrim(value) == "" {
		return "", errors.New("target is empty")
	}
	if utf16Len(value) > MaxTarget {
		return "", errors.New("target is too long")
	}
	trimmed := jsTrim(value)
	if HasControlOrFormat(trimmed) {
		return "", errors.New("target must not contain control or invisible characters")
	}
	// ssh seçeneği olarak okunur (-oProxyCommand=…): savunma derinliği, ssh
	// katmanı da reddeder.
	if strings.HasPrefix(trimmed, "-") {
		return "", errors.New(`target must not start with "-"`)
	}
	return trimmed, nil
}

// Uint, arayüzün gönderdiği sayfa numarası gibi bir değeri yumuşakça okur:
// negatif, sonsuz ya da sayı olmayan değer varsayılana döner, kesir aşağı
// yuvarlanır. Sayı metni de kabul edilir (JavaScript'teki Number()).
func Uint(raw json.RawMessage, fallback uint64) uint64 {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		s := jsTrim(t)
		if s == "" {
			f = 0
		} else if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			f = parsed
		} else {
			return fallback
		}
	case bool:
		if t {
			f = 1
		}
	case nil:
		f = 0
	default:
		return fallback
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return fallback
	}
	f = math.Floor(f)
	if f > maxSafeInteger {
		return fallback
	}
	return uint64(f)
}

// WindowAction, başlık çubuğu düğmesinin beyaz listede olduğunu denetler.
func WindowAction(value string) (string, error) {
	for _, a := range WindowActions {
		if value == a {
			return value, nil
		}
	}
	return "", errors.New("unknown window action")
}

// CopyText, panoya yazılacak kısa metin (parmak izi gibi).
func CopyText(value string) (string, error) {
	if value == "" || utf16Len(value) > MaxCopy {
		return "", errors.New("copy text must be 1-1024 characters")
	}
	return value, nil
}

// AppID, uygulama adı: küçük harfle başlar, en çok 32 karakter.
func AppID(value string) (string, error) {
	if !appIDPattern.MatchString(value) {
		return "", errors.New("invalid app ID")
	}
	return value, nil
}

// Confirmation, silme ve geri alma için yazılan onay uygulama adıyla AYNI olmalı.
func Confirmation(appID, value string) error {
	if value != appID {
		return errors.New("confirmation must match the app ID exactly")
	}
	return nil
}

// GitHubDisconnectWord, GitHub bağlantısını kesmek için yazılan onay sözcüğü.
const GitHubDisconnectWord = "disconnect"

// GitHubDisconnectConfirmation, onay sözcüğü TAM "disconnect" olmalı.
func GitHubDisconnectConfirmation(value string) error {
	if value != GitHubDisconnectWord {
		return errors.New(`confirmation must be "disconnect"`)
	}
	return nil
}

// githubInstallURL, uygulamanın kurulum adresi (kısa ad küçük harf).
var githubInstallURL = regexp.MustCompile(`^https://github\.com/apps/[a-z0-9-]{1,100}/installations/new$`)

// githubFixedURLs, GitHub bağlantı akışının sabit adresleri.
var githubFixedURLs = map[string]bool{
	"https://github.com/login/device":                 true,
	"https://github.com/settings/apps/authorizations": true,
	"https://github.com/settings/installations":       true,
}

// GitHubURL, tarayıcıda açılabilecek TEK adres biçimleri: cihaz kodu sayfası,
// kurulum bağlantısı ve iki ayar sayfası. Sorgu, parça, kimlik bilgisi, kapı
// ve başka host reddedilir; genel bir "adres aç" köprüsü yok.
func GitHubURL(value string) (string, error) {
	if githubFixedURLs[value] || githubInstallURL.MatchString(value) {
		return value, nil
	}
	return "", errors.New("only GitHub app addresses can be opened")
}

// Ref, dağıtılacak dal, etiket ya da tam commit; boşsa sunucunun varsayılanı.
func Ref(value string) (string, error) {
	if value != "" && !BranchPattern.MatchString(value) {
		return "", errors.New("invalid branch, tag or full commit")
	}
	return value, nil
}

// Tail, okunacak log satırı sayısı.
func Tail(value int) (int, error) {
	if value < 1 || value > MaxTail {
		return 0, errors.New("log tail must be 1-10000 lines")
	}
	return value, nil
}

// ServerName, kayıtlı sunucunun adı. Asıl kural (64 karakter, tekillik) Go
// tarafında internal/servers'ta; burada yalnız kaba bir üst sınır.
func ServerName(value string) (string, error) {
	if jsTrim(value) == "" {
		return "", errors.New("server name is empty")
	}
	if utf16Len(value) > MaxName {
		return "", errors.New("server name is too long")
	}
	return value, nil
}

// Konsol girdi sınırları (Kadran Konsolu).
const (
	// MaxConsoleLine, konsol satırının en çok bayt sayısı (ayrıştırıcıyla aynı).
	MaxConsoleLine = 4096
	// MaxConsoleInput, çalışan komutun istemine verilen satırın en çok bayt sayısı.
	MaxConsoleInput = 256
)

// runIDPattern, çalıştırma kimliği: 16 küçük onaltılık hane (console.Runner).
var runIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ConsoleServer, konsolun seçili sunucu adı. Boş ad "sunucu seçili değil"
// demektir (yalnız help ve server list çalışır); doluysa ServerName kuralı
// geçerli. Geçmiş bu adla anahtarlandığı için doğrulama köprüde yapılır.
func ConsoleServer(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return ServerName(value)
}

// ConsoleLine, konsol satırı: yalnız bayt sınırı. Denetim ve biçim
// karakterlerini (satır sonu dahil) satır ayrıştırıcısı reddeder ve nedenini
// çalıştırmanın stderr akışına yazar; burada hata olarak kesilmez.
func ConsoleLine(value string) (string, error) {
	if len(value) > MaxConsoleLine {
		return "", errors.New("console line is too long (at most 4096 bytes)")
	}
	return value, nil
}

// ConsoleInput, komutun istemine verilen tek satır: en çok 256 bayt, denetim
// ya da biçim (Cc, Cf) karakteri yok. İçerik sır olabilir; hata metni onu
// yansıtmaz.
func ConsoleInput(value string) (string, error) {
	if len(value) > MaxConsoleInput {
		return "", errors.New("console input is too long (at most 256 bytes)")
	}
	if HasControlOrFormat(value) {
		return "", errors.New("console input must not contain control or invisible characters")
	}
	return value, nil
}

// RunID, konsol çalıştırma kimliği: yalnız 16 küçük onaltılık hane.
func RunID(value string) (string, error) {
	if !runIDPattern.MatchString(value) {
		return "", errors.New("invalid run ID")
	}
	return value, nil
}

// ConsoleCursor, imlecin satır içinde (UTF-16 birimi) olduğunu denetler.
func ConsoleCursor(line string, cursor int) (int, error) {
	if cursor < 0 || cursor > utf16Len(line) {
		return 0, errors.New("cursor is outside the line")
	}
	return cursor, nil
}

// Provider, elle yazılan sağlayıcı etiketi; boş olabilir.
func Provider(value string) (string, error) {
	if runeCount(value) > MaxProvider {
		return "", errors.New("provider is too long")
	}
	return value, nil
}
