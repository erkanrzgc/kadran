package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/erkanrzgc/kadran/internal/connproto"
)

// ── Anahtar yönetimi: `kadran key` (K-131) ───────────────────────────
//
// Dağıtım anahtarının rolü authorized_keys satırında duruyor (K-131); o
// dosyaya yalnızca root yazabiliyor. Bu yüzden anahtar işlemleri bootstrap
// ile AYNI yoldan gidiyor: root'a SSH ya da -sudo ile parolasız sudo.
// kadrand hiç dahil değil.
//
// İş bölümü:
//   - İş istasyonu: anahtarı ve kapsamı doğrular, satırı kurar, listeyi
//     ayrıştırır, parmak izini hesaplar. Hepsi Go'da ve test altında.
//   - Sunucu: SABİT bir betik. Satırı YENİDEN doğrular (istemciye
//     güvenmez), aynı anahtar gövdesini ikinci kez eklemez, son yönetici
//     satırının silinmesini reddeder ve dosyayı geçici dosya + mv ile
//     değiştirir.

var (
	// libDir ve clientAuthorizedKeys install.sh'in LIB_DIR ve CLIENT_HOME'u
	// (TestClientPathsMatchInstallScript). Değişken: testler geçici bir
	// dosyaya yönlendiriyor.
	libDir               = "/usr/local/lib/kadran"
	clientAuthorizedKeys = "/var/lib/kadran-client/.ssh/authorized_keys"
)

// Uzak betiğin çıkış kodları.
const (
	keysBadLine    = 2
	keysNoFile     = 3
	keysDuplicate  = 4
	keysLastAdmin  = 5
	keysNotFound   = 6
	keysMaxComment = 64
)

// remoteKeys; argümanlar: $1 işlem (list|add|remove), $2 authorized_keys,
// add için $3 satır $4 anahtar gövdesi, remove için $3 anahtar gövdesi.
//
// Desenler Go tarafındakilerle aynı karakter kümesini kullanıyor; satır
// sonu taşıyan bir satır `=~` ile eşleşmez (desen tek satır).
const remoteKeys = `set -euo pipefail
op="$1"; f="$2"
admin_re='^command="/usr/local/lib/kadran/kadran-connect",restrict '
[ -f "$f" ] || { echo "authorized_keys yok ($f) — sunucu kurulmamış mı? önce kadran bootstrap" >&2; exit 3; }
replace() {
    chown --reference="$f" "$1"
    chmod 600 "$1"
    mv -f "$1" "$f"
}
case "$op" in
list)
    cat "$f"
    ;;
add)
    line="$3"; body="$4"
    re='^command="/usr/local/lib/kadran/kadran-connect -deploy=[a-z][a-z0-9-]*(,[a-z][a-z0-9-]*)*",restrict [a-z0-9@.-]+ [A-Za-z0-9+/]+=*( [A-Za-z0-9@._+-]+)?$'
    [[ "$line" =~ $re ]] || { echo "geçersiz anahtar satırı" >&2; exit 2; }
    case "$line" in *",restrict $body"|*",restrict $body "*) ;; *) echo "satır anahtar gövdesini taşımıyor" >&2; exit 2 ;; esac
    if grep -qF -- "$body" "$f"; then echo "bu anahtar zaten kayıtlı" >&2; exit 4; fi
    tmp="$(mktemp "$f.XXXXXX")"
    trap 'rm -f "$tmp"' EXIT
    cat "$f" > "$tmp"
    printf '%s\n' "$line" >> "$tmp"
    replace "$tmp"
    ;;
remove)
    body="$3"
    body_re='^[a-z0-9@.-]+ [A-Za-z0-9+/]+=*$'
    [[ "$body" =~ $body_re ]] || { echo "geçersiz anahtar gövdesi" >&2; exit 2; }
    grep -qF -- "$body" "$f" || { echo "anahtar bulunamadı" >&2; exit 6; }
    tmp="$(mktemp "$f.XXXXXX")"
    trap 'rm -f "$tmp"' EXIT
    grep -vF -- "$body" "$f" > "$tmp" || true
    grep -qE "$admin_re" "$tmp" || { echo "son yönetici anahtarı kaldırılamaz" >&2; exit 5; }
    replace "$tmp"
    ;;
*)
    exit 2
    ;;
esac`

// KeyOptions, anahtar işlemlerinin hedefi. Yetki bootstrap'takiyle aynı:
// root'a SSH ya da Sudo ile parolasız sudo.
type KeyOptions struct {
	Host string
	Port int
	Sudo bool
}

func (k KeyOptions) options() Options {
	return Options{Host: k.Host, Port: k.Port, Sudo: k.Sudo, Stdout: io.Discard, Stderr: io.Discard}
}

func (k KeyOptions) validate() error {
	return validateTarget(k.Host)
}

// AuthorizedKey, authorized_keys'in bir satırı.
type AuthorizedKey struct {
	// Role: connproto.RoleAdmin, connproto.RoleDeploy ya da "" (Kadran'ın
	// tanımadığı satır: başka bir komuta zorlanmış ya da hiç zorlanmamış).
	Role        string
	Apps        []string
	Type        string
	Fingerprint string
	Comment     string

	// forced: kadran-connect'e zorlanmış ve restrict'li mi.
	forced bool
	// body: tür + base64; silme bununla yapılıyor (install.sh'in eşleşme
	// ölçütüyle aynı). Dışa açık değil: tip görüntülenecek alanları taşıyor.
	body string
}

// Restricted, satırın kadran-connect'e zorlanıp restrict taşıdığını söyler.
// Kısıtsız bir satır kadran-client'a kabuk açar.
func (k AuthorizedKey) Restricted() bool { return k.forced }

// publicKey, doğrulanmış bir açık anahtar.
type publicKey struct {
	Type        string
	Base64      string
	Comment     string
	Fingerprint string
}

func (p publicKey) body() string { return p.Type + " " + p.Base64 }

// parsePublicKey, tek satırlık bir açık anahtarı doğrular ve ayrıştırır.
//
// validatePublicKey'in denetimlerine (tek satır, özel anahtar değil,
// bilinen tür) ek olarak gövdenin GERÇEKTEN o türde bir anahtar olduğunu
// denetler: authorized_keys'e yazılacak her bayt doğrulanıyor.
func parsePublicKey(content []byte) (publicKey, error) {
	if err := validatePublicKey(content); err != nil {
		return publicKey{}, err
	}
	fields := strings.Fields(strings.TrimSpace(string(content)))
	typ, b64 := fields[0], fields[1]
	fp, err := fingerprintOf(typ, b64)
	if err != nil {
		return publicKey{}, err
	}
	return publicKey{Type: typ, Base64: b64, Comment: strings.Join(fields[2:], " "), Fingerprint: fp}, nil
}

// fingerprintOf, gövdenin türü taşıdığını doğrular ve `ssh-keygen -lf`
// biçiminde parmak izini döner (sshenv'deki hesapla aynı).
func fingerprintOf(typ, b64 string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("anahtar gövdesi base64 değil: %w", err)
	}
	if len(blob) < 4 {
		return "", errors.New("anahtar gövdesi çok kısa")
	}
	n := binary.BigEndian.Uint32(blob[:4])
	if uint64(n) > uint64(len(blob)-4) || string(blob[4:4+n]) != typ {
		return "", fmt.Errorf("anahtar gövdesi %s türünde değil", typ)
	}
	// Türden sonrası uzunluk önekli alanlar dizisi (ed25519: anahtar;
	// rsa: e, n; ecdsa: eğri, nokta). Dizi gövdeyi TAM tüketmeli: kesik ya
	// da sonuna bayt eklenmiş bir gövde reddediliyor.
	rest, parts := blob[4+n:], 0
	for len(rest) > 0 {
		if len(rest) < 4 {
			return "", errors.New("anahtar gövdesi kesik")
		}
		m := binary.BigEndian.Uint32(rest[:4])
		if uint64(m) > uint64(len(rest)-4) {
			return "", errors.New("anahtar gövdesi kesik")
		}
		rest, parts = rest[4+m:], parts+1
	}
	if parts == 0 {
		return "", errors.New("anahtar gövdesinde anahtar yok")
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

// commentPattern, satıra girebilen yorum. Boşluk, tırnak ve satır sonu
// yok: yorum uzak betiğe argüman olarak gidiyor ve oradaki desenle de
// eşleşmeli.
var commentPattern = regexp.MustCompile(`^[A-Za-z0-9@._+-]{1,64}$`)

// deployLine, dağıtım anahtarının authorized_keys satırını kurar.
//
// name boşsa anahtarın kendi yorumu kullanılır; o da satıra giremeyecek
// karakterler taşıyorsa yorumsuz yazılır (yorum sshd için anlamsız, yalnız
// insanlar için).
func deployLine(k publicKey, apps []string, name string) (string, error) {
	if err := (connproto.Identity{Role: connproto.RoleDeploy, Apps: apps}).CheckRole(); err != nil {
		return "", err
	}
	comment := k.Comment
	if name != "" {
		if !commentPattern.MatchString(name) {
			return "", fmt.Errorf("ad %q geçersiz: harf, rakam ve @._+- olabilir, en fazla %d karakter",
				name, keysMaxComment)
		}
		comment = name
	}
	line := fmt.Sprintf(`command="%s/kadran-connect -deploy=%s",restrict %s`,
		libDir, strings.Join(apps, ","), k.body())
	if commentPattern.MatchString(comment) {
		line += " " + comment
	}
	return line, nil
}

// ParseAuthorizedKeys, authorized_keys içeriğini ayrıştırır. Boş ve yorum
// satırları atlanır; tanınmayan satırlar Role "" ile döner, YOK SAYILMAZ:
// kısıtsız bir satırı göstermek listenin asıl işi.
func ParseAuthorizedKeys(content string) []AuthorizedKey {
	var out []AuthorizedKey
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, parseAuthorizedLine(line))
	}
	return out
}

func parseAuthorizedLine(line string) AuthorizedKey {
	var k AuthorizedKey
	rest := line
	command, hasCommand := "", false
	if after, ok := strings.CutPrefix(line, `command="`); ok {
		if end := strings.IndexByte(after, '"'); end >= 0 {
			command, hasCommand = after[:end], true
			rest = after[end+1:]
		}
	}

	restricted := false
	if after, ok := strings.CutPrefix(rest, ",restrict "); ok {
		rest, restricted = after, true
	}

	fields := strings.Fields(rest)
	if len(fields) >= 2 {
		k.Type = fields[0]
		if fp, err := fingerprintOf(fields[0], fields[1]); err == nil {
			k.Fingerprint, k.body = fp, fields[0]+" "+fields[1]
		}
		k.Comment = strings.Join(fields[2:], " ")
	}

	if !hasCommand || !restricted {
		return k
	}
	connect := libDir + "/kadran-connect"
	switch {
	case command == connect:
		k.Role, k.forced = connproto.RoleAdmin, true
	case strings.HasPrefix(command, connect+" -deploy="):
		if apps, err := connproto.ParseDeployScope(strings.TrimPrefix(command, connect+" -deploy=")); err == nil {
			k.Role, k.Apps, k.forced = connproto.RoleDeploy, apps, true
		}
	}
	return k
}

// ListKeys, sunucudaki istemci anahtarlarını listeler.
func ListKeys(ctx context.Context, opts KeyOptions) ([]AuthorizedKey, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := runKeys(ctx, opts, &out, "list", clientAuthorizedKeys); err != nil {
		return nil, err
	}
	return ParseAuthorizedKeys(out.String()), nil
}

// AddDeployKey, yalnızca apps'i dağıtabilen bir anahtar ekler.
func AddDeployKey(ctx context.Context, opts KeyOptions, pub []byte, apps []string, name string) (AuthorizedKey, error) {
	if err := opts.validate(); err != nil {
		return AuthorizedKey{}, err
	}
	k, err := parsePublicKey(pub)
	if err != nil {
		return AuthorizedKey{}, err
	}
	line, err := deployLine(k, apps, name)
	if err != nil {
		return AuthorizedKey{}, err
	}
	if err := runKeys(ctx, opts, nil, "add", clientAuthorizedKeys, line, k.body()); err != nil {
		return AuthorizedKey{}, err
	}
	return parseAuthorizedLine(line), nil
}

// RemoveKey, parmak izi verilen anahtarın satır(lar)ını kaldırır. Son
// yönetici satırını sunucu reddeder.
func RemoveKey(ctx context.Context, opts KeyOptions, fingerprint string) (AuthorizedKey, error) {
	keys, err := ListKeys(ctx, opts)
	if err != nil {
		return AuthorizedKey{}, err
	}
	for _, k := range keys {
		// Parmak izi yalnız gövdesi çözülebilen satırlarda dolu; boş parmak
		// izi hiçbir girdiyle eşleşmiyor.
		if k.Fingerprint == "" || k.Fingerprint != fingerprint {
			continue
		}
		if err := runKeys(ctx, opts, nil, "remove", clientAuthorizedKeys, k.body); err != nil {
			return AuthorizedKey{}, err
		}
		return k, nil
	}
	return AuthorizedKey{}, fmt.Errorf("anahtar bulunamadı: %s — `kadran key list` parmak izlerini gösterir", fingerprint)
}

// runKeys, uzak betiği root olarak koşturur ve çıkış kodunu hataya çevirir.
func runKeys(ctx context.Context, opts KeyOptions, stdout io.Writer, args ...string) error {
	var stderr bytes.Buffer
	o := opts.options()
	o.Stderr = &stderr
	if stdout == nil {
		stdout = io.Discard
	}
	code, err := sshRun(ctx, o, privileged(o, remoteKeys, args...), nil, stdout)
	if err != nil {
		return fmt.Errorf("anahtar işlemi: %w", err)
	}
	msg := strings.TrimSpace(stderr.String())
	switch code {
	case 0:
		return nil
	case keysDuplicate, keysLastAdmin, keysNotFound, keysNoFile, keysBadLine:
		return errors.New(msg)
	case sshTransportFailure:
		return fmt.Errorf("sunucuya bağlanılamadı: %s", msg)
	default:
		if opts.Sudo && msg != "" {
			return fmt.Errorf("anahtar işlemi başarısız (kod %d; -sudo parola SORMAZ, NOPASSWD gerekir): %s", code, msg)
		}
		return fmt.Errorf("anahtar işlemi başarısız (kod %d): %s", code, msg)
	}
}
