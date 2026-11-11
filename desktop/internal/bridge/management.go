package bridge

import (
	"encoding/json"
	"errors"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/guard"
	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

// Yönetim yöntemleri. Durum değiştiren HER yöntemde sıra: girdiyi doğrula,
// kapıya gir, yerel onay göster, onaylanırsa dağıtıcıyı çağır. Süre sınırı
// onaydan SONRA başlar (dağıtıcıda). İptal bir hata değil, tipli sonuçtur.

// fixtureTarget, duman testinin sahte sunucusu (yalnız smoke ikilisinde).
const fixtureTarget = "fake@test"

// Cancelled, kullanıcı yerel kutuda vazgeçti; sunucuya hiç gidilmedi.
type Cancelled struct {
	Cancelled bool `json:"cancelled"`
}

// enter, (hedef, uygulama) kapısına girer: düğmeler, tekrarlanan çağrılar
// ve yerel kutular aynı işi iki kez başlatamasın.
func (b *Bridge) enter(target, app string) (func(), error) {
	key := target + "\x00" + app
	b.gateMu.Lock()
	defer b.gateMu.Unlock()
	if b.active[key] {
		return nil, errors.New("an operation for this app is already running")
	}
	b.active[key] = true
	return func() {
		b.gateMu.Lock()
		delete(b.active, key)
		b.gateMu.Unlock()
	}, nil
}

// confirmed, yerel onay kutusunu gösterir. Metin doğrulanmış değerlerden
// ve kayıtlı dilden kurulur. Aynı anda tek kutu.
func (b *Bridge) confirmed(req confirm.Request) (bool, error) {
	if b.deps.Fixture && req.Target == fixtureTarget {
		return true, nil
	}
	b.gateMu.Lock()
	if b.dialogOpen {
		b.gateMu.Unlock()
		return false, errors.New("another confirmation is open; answer it first")
	}
	b.dialogOpen = true
	b.gateMu.Unlock()
	defer func() {
		b.gateMu.Lock()
		b.dialogOpen = false
		b.gateMu.Unlock()
	}()
	language := confirm.Language(b.deps.Settings.Load(b.warn).Language)
	return b.deps.Host.Confirm(confirm.Build(language, req))
}

// mutate, onaylı bir işlemin ortak akışı.
func (b *Bridge) mutate(gateApp string, req confirm.Request, method string, params map[string]any) (any, error) {
	release, err := b.enter(req.Target, gateApp)
	if err != nil {
		return nil, err
	}
	defer release()
	ok, err := b.confirmed(req)
	if err != nil {
		return nil, err
	}
	if !ok {
		return Cancelled{Cancelled: true}, nil
	}
	return b.dispatch(method, params)
}

func targetAndApp(target, appID string) (string, string, error) {
	t, err := guard.Target(target)
	if err != nil {
		return "", "", err
	}
	id, err := guard.AppID(appID)
	return t, id, err
}

// AppsList, sunucudaki uygulamalar.
func (b *Bridge) AppsList(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("apps.list", map[string]any{"target": t})
}

// AppGet, bir uygulamanın tanımı ve sürümleri.
func (b *Bridge) AppGet(target, appID string) (any, error) {
	t, id, err := targetAndApp(target, appID)
	if err != nil {
		return nil, err
	}
	return b.dispatch("apps.get", map[string]any{"target": t, "app_id": id})
}

// AppLogs, uygulamanın son log satırları (takip yok).
func (b *Bridge) AppLogs(target, appID string, tail int) (any, error) {
	t, id, err := targetAndApp(target, appID)
	if err != nil {
		return nil, err
	}
	n, err := guard.Tail(tail)
	if err != nil {
		return nil, err
	}
	return b.dispatch("apps.logs", map[string]any{"target": t, "app_id": id, "tail": n})
}

// AlarmsList, açık alarmlar.
func (b *Bridge) AlarmsList(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("alarms.list", map[string]any{"target": t})
}

// BackupsList, alınmış yedekler.
func (b *Bridge) BackupsList(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("backups.list", map[string]any{"target": t})
}

func encoded(o *jsonv.Object) (json.RawMessage, error) {
	raw, err := jsonv.Encode(o)
	if err != nil {
		return nil, errors.New("could not encode the app payload")
	}
	return raw, nil
}

// AppCreate, yeni bir uygulama tanımlar.
func (b *Bridge) AppCreate(target string, spec json.RawMessage) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	s, err := guard.AppPayload(spec, guard.Create)
	if err != nil {
		return nil, err
	}
	raw, err := encoded(s)
	if err != nil {
		return nil, err
	}
	id, _ := s.Get("app_id")
	app, _ := id.(string)
	return b.mutate(app, confirm.Request{Kind: confirm.KindCreate, Target: t, Spec: s}, "apps.create", map[string]any{"target": t, "spec": raw})
}

// AppUpdate, uygulamanın değişen alanlarını uygular.
func (b *Bridge) AppUpdate(target, appID string, update json.RawMessage) (any, error) {
	t, id, err := targetAndApp(target, appID)
	if err != nil {
		return nil, err
	}
	u, err := guard.AppPayload(update, guard.Update)
	if err != nil {
		return nil, err
	}
	raw, err := encoded(u)
	if err != nil {
		return nil, err
	}
	return b.mutate(id, confirm.Request{Kind: confirm.KindUpdate, Target: t, App: id, Update: u}, "apps.update", map[string]any{"target": t, "app_id": id, "update": raw})
}

// AppDeploy, bir dalı, etiketi ya da commit'i dağıtır.
func (b *Bridge) AppDeploy(target, appID, ref string) (any, error) {
	t, id, err := targetAndApp(target, appID)
	if err != nil {
		return nil, err
	}
	r, err := guard.Ref(ref)
	if err != nil {
		return nil, err
	}
	return b.mutate(id, confirm.Request{Kind: confirm.KindDeploy, Target: t, App: id, Ref: r}, "apps.deploy", map[string]any{"target": t, "app_id": id, "ref": r})
}

// AppDelete, uygulamayı kalıcı olarak siler; onay metni uygulama adıyla aynı olmalı.
func (b *Bridge) AppDelete(target, appID, confirmation string) (any, error) {
	return b.lifecycle(confirm.KindDelete, "apps.delete", target, appID, confirmation)
}

// AppRollback, trafiği önceki sürüme taşır; onay metni uygulama adıyla aynı olmalı.
func (b *Bridge) AppRollback(target, appID, confirmation string) (any, error) {
	return b.lifecycle(confirm.KindRollback, "apps.rollback", target, appID, confirmation)
}

func (b *Bridge) lifecycle(kind confirm.Kind, method, target, appID, confirmation string) (any, error) {
	t, id, err := targetAndApp(target, appID)
	if err != nil {
		return nil, err
	}
	if err := guard.Confirmation(id, confirmation); err != nil {
		return nil, err
	}
	return b.mutate(id, confirm.Request{Kind: kind, Target: t, App: id}, method, map[string]any{"target": t, "app_id": id, "confirmation": confirmation})
}

// BackupCreate, şimdi bir yedek alır.
func (b *Bridge) BackupCreate(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	// "$" uygulama adında olamaz: yedek kapısı hiçbir uygulamanınkiyle çakışmaz.
	return b.mutate("$backup", confirm.Request{Kind: confirm.KindBackup, Target: t}, "backups.create", map[string]any{"target": t})
}
