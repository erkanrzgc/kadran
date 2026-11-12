// Package bridge, arayüze açılan yöntemler (K-165).
//
// # Bu yapı bir beyaz listedir
//
// Wails, bağlanan yapının DIŞA AÇIK her yöntemini arayüzdeki JavaScript'e
// açar. Bridge'in dışa açık yöntemleri bu yüzden tam olarak Electron
// köprüsünün (preload) listesidir; fazlası bridge_test.go'da düşer. Arayüze
// genel bir "şu yöntemi çağır" köprüsü verilmez: sunucudaki tipli RPC
// sınırını istemci tarafından delmek olurdu.
//
// Arayüz güvenilmez bir kaynaktır. Her yöntem girdiyi doğrular (guard),
// durum değiştiren her işlem yerel onay ister (management.go) ve hata
// metni arıtılmış gider (redact).
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/guard"
	"github.com/erkanrzgc/kadran/desktop/internal/redact"
	"github.com/erkanrzgc/kadran/internal/sidecar"
)

// Dispatcher, sunucu yöntemlerini çalıştıran dağıtıcı (internal/sidecar).
type Dispatcher interface {
	Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *sidecar.Error)
}

// Host, pencere ve işletim sistemi tarafı (Wails ve Windows çağrıları main'de).
type Host interface {
	Minimise()
	ToggleMaximise()
	Close()
	// SetLook, temaya göre pencerenin zeminini ve camın tonunu ayarlar.
	SetLook(look appearance.WindowLook)
	SetClipboard(text string) error
	// Confirm, yerel onay kutusunu gösterir; true: eylem onaylandı.
	// Varsayılan düğme Cancel olmalı.
	Confirm(box confirm.Box) (bool, error)
}

// Deps, köprünün dışarıdan aldıkları.
type Deps struct {
	Dispatcher Dispatcher
	Host       Host
	Settings   appearance.Store
	// Material, pencerenin bu sistemdeki malzemesi (açılışta belirlenir).
	Material func() appearance.Material
	// Fixture, yalnız duman testi ikilisinde true: fake@test hedefinde onay
	// kutusu atlanır. Dağıtılan ikilide hep false (smoke etiketi).
	Fixture bool
	Warn    func(message string)
}

// Bridge, arayüzün çağırabildiği yöntemler.
type Bridge struct {
	deps Deps

	gateMu sync.Mutex
	active map[string]bool
	// dialogOpen: kapı (hedef, uygulama) başına; bu bayrak ise BÜTÜN
	// işlemlerde aynı anda tek yerel kutu bırakır. Ele geçmiş bir arayüz
	// farklı uygulama adlarıyla kutu yağdırıp kullanıcıyı onay yorgunluğuna
	// itemesin.
	dialogOpen bool
}

// New, köprüyü kurar.
func New(deps Deps) *Bridge {
	return &Bridge{deps: deps, active: map[string]bool{}}
}

func (b *Bridge) warn(message string) {
	if b.deps.Warn != nil {
		b.deps.Warn(message)
	}
}

// dispatch, dağıtıcıyı çağırır; hata arayüze arıtılmış gider. Süre sınırı
// dağıtıcının kendisinde (yöntem başına): kapı, sunucudaki iş bitmeden ya
// da dağıtıcı vazgeçmeden serbest kalmaz.
func (b *Bridge) dispatch(method string, params map[string]any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("could not encode the call parameters")
	}
	result, failure := b.deps.Dispatcher.Dispatch(context.Background(), method, raw)
	if failure != nil {
		return nil, errors.New(redact.Text(failure.Message, failure.Data))
	}
	return result, nil
}

// ── Okuma yöntemleri ────────────────────────────────────────────────

// Version, uygulamanın sürüm ve protokol bilgisi. Sunucuya dokunmaz.
func (b *Bridge) Version() (any, error) {
	return b.dispatch("version", nil)
}

// Status, hedef sunucunun durumu.
func (b *Bridge) Status(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("status", map[string]any{"target": t})
}

// ResourceUsage, hostun (ve includeApps ise uygulamaların) kümülatif
// kaynak sayaçları (K-166). Okuma: kapı ve onay yok. Hızı arayüz iki
// yanıtın farkından hesaplıyor (lib/resources.ts).
func (b *Bridge) ResourceUsage(target string, includeApps bool) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("resources", map[string]any{"target": t, "include_apps": includeApps})
}

// AuditList, denetim kayıtlarını sayfalı okur.
func (b *Bridge) AuditList(target string, afterSeq, limit json.RawMessage) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("audit.list", map[string]any{"target": t, "after_seq": guard.Uint(afterSeq, 0), "limit": guard.Uint(limit, 50)})
}

// AuditVerify, iki denetim zincirini de doğrular.
func (b *Bridge) AuditVerify(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("audit.verify", map[string]any{"target": t})
}

// ── Görünüm ve pencere ──────────────────────────────────────────────

// AppearanceBundle, kayıtlı ayarlar, varsayılanlar ve bu sistemin malzemesi.
type AppearanceBundle struct {
	Appearance appearance.Appearance `json:"appearance"`
	Defaults   appearance.Appearance `json:"defaults"`
	Material   appearance.Material   `json:"material"`
}

// LoadAppearance, görünüm ayarlarını okur.
func (b *Bridge) LoadAppearance() AppearanceBundle {
	return AppearanceBundle{
		Appearance: b.deps.Settings.Load(b.warn),
		Defaults:   appearance.Defaults(),
		Material:   b.deps.Material(),
	}
}

// SaveAppearance, ayarları doğrulayıp kaydeder. Cam önce, dosya sonra:
// diske yazmayı beklerse cam arayüzden geç dönerdi (K-163).
func (b *Bridge) SaveAppearance(raw json.RawMessage) (appearance.Appearance, error) {
	b.deps.Host.SetLook(appearance.Look(appearance.Sanitize(raw).Theme, b.deps.Material()))
	return b.deps.Settings.Save(raw)
}

// WindowControl, özel başlık çubuğunun düğmeleri: minimize | maximize | close.
func (b *Bridge) WindowControl(action string) error {
	a, err := guard.WindowAction(action)
	if err != nil {
		return err
	}
	switch a {
	case "minimize":
		b.deps.Host.Minimise()
	case "maximize":
		b.deps.Host.ToggleMaximise()
	default:
		b.deps.Host.Close()
	}
	return nil
}

// CopyText, parmak izi gibi kısa bir metni panoya yazar (en fazla 1024
// karakter). Arayüze pano izni verilmez; yazma burada yapılır.
func (b *Bridge) CopyText(text string) error {
	t, err := guard.CopyText(text)
	if err != nil {
		return err
	}
	return b.deps.Host.SetClipboard(t)
}
