package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── Oturum jetonu önbelleği (K-155) ───────────────────────────────────
//
// `kadran login` aldığı jetonu hedef başına bir dosyaya yazar; sonraki
// komutlar onu okuyup her çağrıya metadata olarak ekler. Jeton bir taşıyıcı
// kimlik bilgisi, ama tek başına işe yaramaz: kadrand onu yalnız aynı SSH
// anahtarının bağlantısında kabul ediyor.

// SessionStore, oturum jetonlarının tutulduğu dizindir.
type SessionStore struct {
	Dir string
}

type sessionFile struct {
	Target  string    `json:"target"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

// DefaultSessionStore, kullanıcının yapılandırma dizinindeki
// kadran/sessions'ı döner. Masaüstü sidecar da aynı ikiliyi ve aynı dizini
// kullanıyor; CLI'daki `kadran login` masaüstüne de geçer.
func DefaultSessionStore() (SessionStore, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return SessionStore{}, fmt.Errorf("could not find the user config directory: %w", err)
	}
	return SessionStore{Dir: filepath.Join(base, "kadran", "sessions")}, nil
}

// path, hedefin dosyasıdır. Ad, hedef dizesinin özetinden: hedef bir dosya
// adına yazılamayacak karakterler (`:`, `@`, `[`) taşıyabiliyor.
func (s SessionStore) path(target string) string {
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])[:16]+".json")
}

// Load, hedefin jetonunu döner. Dosya yoksa, süresi dolmuşsa ya da başka
// bir hedefe aitse ok false; okunamayan ya da bozuk dosya hatadır.
func (s SessionStore) Load(target string, now time.Time) (string, bool, error) {
	p := s.path(target)
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("could not read the saved session (%s): %w", p, err)
	}
	var f sessionFile
	if err := json.Unmarshal(b, &f); err != nil {
		return "", false, fmt.Errorf("saved session is damaged (%s): %w; sign in again with kadran login", p, err)
	}
	// Dosya adı kısaltılmış bir özet: içindeki hedef de eşleşmeli, yoksa
	// bir sunucunun jetonu başka bir sunucuya gönderilirdi.
	if f.Target != target || f.Token == "" || !now.Before(f.Expires) {
		return "", false, nil
	}
	return f.Token, true, nil
}

// Save, hedefin jetonunu yazar. Dizin 0700, dosya 0600; yazma geçici dosya
// ve rename ile, yarım kalan bir yazma bozuk bir oturum bırakmasın.
func (s SessionStore) Save(target, token string, expires time.Time) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("could not create the session directory (%s): %w", s.Dir, err)
	}
	b, err := json.Marshal(sessionFile{Target: target, Token: token, Expires: expires.UTC()})
	if err != nil {
		return fmt.Errorf("could not encode the session: %w", err)
	}
	tmp, err := os.CreateTemp(s.Dir, ".session-*")
	if err != nil {
		return fmt.Errorf("could not create a temporary session file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("could not set session file permissions: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		return fmt.Errorf("could not write the session: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("could not sync the session to disk: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not close the session file: %w", err)
	}
	if err := os.Rename(tmpName, s.path(target)); err != nil {
		return fmt.Errorf("could not save the session: %w", err)
	}
	return nil
}

// Delete, hedefin jetonunu siler. Olmayan dosyayı silmek hata değil.
func (s SessionStore) Delete(target string) error {
	err := os.Remove(s.path(target))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("could not delete the saved session: %w", err)
	}
	return nil
}

// sessionErrorPrefixes, kadrand'nin oturum hatalarının başları
// (internal/api/session.go). internal/api testleri bu eşleşmeyi gerçek
// sunucu hatalarıyla sınıyor.
var sessionErrorPrefixes = []string{"TOTP is on", "session expired", "invalid session token"}

// IsSessionError, hatanın "oturum yok / süresi doldu / geçersiz" olduğunu
// söyler: çözüm `kadran login`.
func IsSessionError(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		return false
	}
	for _, p := range sessionErrorPrefixes {
		if strings.HasPrefix(st.Message(), p) {
			return true
		}
	}
	return false
}

// SetSession, sonraki çağrılara eklenecek jetonu ayarlar; boş dize jetonu
// kaldırır.
func (c *Client) SetSession(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = token
}

func (c *Client) withSession(ctx context.Context) context.Context {
	c.mu.Lock()
	token := c.session
	c.mu.Unlock()
	if token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, totp.SessionMetadataKey, token)
}

func (c *Client) unarySession(
	ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
) error {
	return invoker(c.withSession(ctx), method, req, reply, cc, opts...)
}

func (c *Client) streamSession(
	ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
	streamer grpc.Streamer, opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
	return streamer(c.withSession(ctx), desc, cc, method, opts...)
}
