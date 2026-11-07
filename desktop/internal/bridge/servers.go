package bridge

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/erkanrzgc/kadran/desktop/internal/guard"
	"github.com/erkanrzgc/kadran/desktop/internal/redact"
)

// Ortak sunucu listesi (servers.json, K-162). Dosyayı bu paket okuyup
// yazmaz; biçimi yalnız internal/servers bilir ve dağıtıcı servers.*
// yöntemleriyle uygular. Her değişiklik tek kayıt gönderir, bütün listeyi
// değil: CLI'ın o arada eklediği sunucu silinmez.
//
// Kayıt bir SIR DEĞİLDİR: yalnızca `kadran-client@1.2.3.4` gibi bir hedef
// dizesi. Anahtar malzemesi ssh-agent'ta kalır ve bu uygulamaya hiç girmez.

// Profile, arayüzün gördüğü kayıt: yönetici hedefi ve sudo dosyada kalır.
type Profile struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	Provider string `json:"provider,omitempty"`
}

// reasons, dağıtıcının çevrilebilir sebepleri (internal/sidecar/servers.go).
var reasons = map[string]bool{"duplicate_name": true, "duplicate_target": true, "not_found": true, "newer_file": true}

// runServers, bir servers.* yöntemini çağırır ve listeyi arayüzün
// alanlarına indirir. Bilinen sebep arayüze sabit bir kodla gider
// ("server-list:duplicate_name"), arayüz onu kendi dilinde söyler; geri
// kalan hata arıtılmış metniyle.
func (b *Bridge) runServers(method string, params map[string]any) ([]Profile, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("could not encode the call parameters")
	}
	result, failure := b.deps.Dispatcher.Dispatch(context.Background(), method, raw)
	if failure != nil {
		if reasons[failure.Data] {
			return nil, errors.New("server-list:" + failure.Data)
		}
		return nil, errors.New(redact.Text(failure.Message, failure.Data))
	}
	return toProfiles(result)
}

func toProfiles(result any) ([]Profile, error) {
	invalid := errors.New("invalid server list")
	body, err := json.Marshal(result)
	if err != nil {
		return nil, invalid
	}
	var list struct {
		Servers []struct {
			Name     *string `json:"name"`
			Target   *string `json:"target"`
			Provider string  `json:"provider"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(body, &list); err != nil || list.Servers == nil {
		return nil, invalid
	}
	out := make([]Profile, 0, len(list.Servers))
	for _, s := range list.Servers {
		if s.Name == nil || s.Target == nil {
			return nil, invalid
		}
		out = append(out, Profile{Name: *s.Name, Target: *s.Target, Provider: s.Provider})
	}
	return out, nil
}

// serverFields, arayüzden gelen kayıt nesnesinin ad ve sağlayıcısı.
func serverFields(raw json.RawMessage) (map[string]any, string, string, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, "", "", errors.New("invalid server")
	}
	name, _ := obj["name"].(string)
	name, err := guard.ServerName(name)
	if err != nil {
		return nil, "", "", err
	}
	provider := ""
	switch p := obj["provider"].(type) {
	case nil:
	case string:
		if provider, err = guard.Provider(p); err != nil {
			return nil, "", "", err
		}
	default:
		return nil, "", "", errors.New("provider must be text")
	}
	return obj, name, provider, nil
}

// ListServers, kayıtlı sunucular.
func (b *Bridge) ListServers() ([]Profile, error) {
	return b.runServers("servers.list", nil)
}

// AddServer, bir sunucu kaydı ekler ve güncel listeyi döner.
func (b *Bridge) AddServer(profile json.RawMessage) ([]Profile, error) {
	obj, name, provider, err := serverFields(profile)
	if err != nil {
		return nil, err
	}
	target, _ := obj["target"].(string)
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.runServers("servers.add", map[string]any{"name": name, "target": t, "provider": provider})
}

// UpdateServer, bir kaydın adını ve sağlayıcısını değiştirir.
func (b *Bridge) UpdateServer(target string, change json.RawMessage) ([]Profile, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	_, name, provider, err := serverFields(change)
	if err != nil {
		return nil, err
	}
	return b.runServers("servers.update", map[string]any{"target": t, "name": name, "provider": provider})
}

// RemoveServer, bir kaydı siler.
func (b *Bridge) RemoveServer(target string) ([]Profile, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.runServers("servers.remove", map[string]any{"target": t})
}
