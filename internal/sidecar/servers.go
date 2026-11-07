package sidecar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── Masaüstünün sunucu listesi (K-162) ──────────────────────────────
//
// Masaüstü servers.json'u kendisi okuyup yazmaz: biçimi yalnız
// internal/servers bilir. Her yöntem yazmadan önce dosyayı yeniden okur ve
// bütün listeyi değil tek değişikliği uygular; CLI'ın o arada eklediği kayıt
// silinmez. Hiçbir yöntem ağa çıkmaz.

// maxImport, bir içe aktarmadaki profil sınırı (liste sınırıyla aynı).
const maxImport = servers.MaxServers

type serverAddParams struct {
	Name     string `json:"name"`
	Target   string `json:"target"`
	Provider string `json:"provider"`
}

type serverUpdateParams struct {
	Target   string `json:"target"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

type serverRemoveParams struct {
	Target string `json:"target"`
}

type serverImportParams struct {
	Profiles []serverAddParams `json:"profiles"`
}

// serverList, bütün yöntemlerin sonucu: güncel liste. İçe aktarmada ayrıca
// eklenen sayısı ve alınamayan profiller (masaüstü kullanıcıya söyler).
type serverList struct {
	Servers  []servers.Server  `json:"servers"`
	Imported *int              `json:"imported,omitempty"`
	Skipped  []servers.Skipped `json:"skipped,omitempty"`
}

func (d *Dispatcher) dispatchServers(method string, params json.RawMessage) (any, *Error) {
	// İstekler eşzamanlı işlenir (Serve, masaüstü); oku-değiştir-yaz sırayla
	// olmazsa iki ekleme birbirininkini silerdi. Başka bir sürece (CLI) karşı
	// kilit yok: orada son yazan kazanır (K-162).
	d.serversMu.Lock()
	defer d.serversMu.Unlock()
	store := d.deps.Servers
	var (
		list []servers.Server
		err  error
	)
	switch method {
	case "servers.list":
		if perr := decodeStrict(params, &struct{}{}, true); perr != nil {
			return nil, perr
		}
		list, err = store.Load()
	case "servers.add":
		var p serverAddParams
		if perr := decodeStrict(params, &p, false); perr != nil {
			return nil, perr
		}
		list, err = store.Update(func(l []servers.Server) ([]servers.Server, error) {
			return servers.Add(l, servers.Server{Name: p.Name, Target: p.Target, Provider: p.Provider})
		})
	case "servers.update":
		var p serverUpdateParams
		if perr := decodeStrict(params, &p, false); perr != nil {
			return nil, perr
		}
		list, err = store.Update(func(l []servers.Server) ([]servers.Server, error) {
			return servers.UpdateByTarget(l, p.Target, p.Name, p.Provider)
		})
	case "servers.remove":
		var p serverRemoveParams
		if perr := decodeStrict(params, &p, false); perr != nil {
			return nil, perr
		}
		list, err = store.Update(func(l []servers.Server) ([]servers.Server, error) {
			return servers.RemoveByTarget(l, p.Target)
		})
	case "servers.import":
		var p serverImportParams
		if perr := decodeStrict(params, &p, false); perr != nil {
			return nil, perr
		}
		if len(p.Profiles) > maxImport {
			return nil, &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("at most %d profiles can be imported", maxImport)}
		}
		in := make([]servers.Server, len(p.Profiles))
		for i, pr := range p.Profiles {
			in[i] = servers.Server{Name: pr.Name, Target: pr.Target, Provider: pr.Provider}
		}
		imported := 0
		var skipped []servers.Skipped
		list, err = store.Update(func(l []servers.Server) ([]servers.Server, error) {
			next, n, sk := servers.Import(l, in)
			imported, skipped = n, sk
			return next, nil
		})
		if err == nil {
			return serverList{Servers: nonNil(list), Imported: &imported, Skipped: skipped}, nil
		}
	}
	if err != nil {
		return nil, &Error{Code: CodeInternalError, Message: err.Error(), Data: serverErrorReason(err)}
	}
	return serverList{Servers: nonNil(list)}, nil
}

// decodeStrict, yalnız tanımlı alanları kabul eder (iç içe olanlar dahil):
// bilinmeyen alan diske de ssh'a da ulaşmaz. optional, boş parametreye izin.
func decodeStrict(raw json.RawMessage, dst any, optional bool) *Error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if optional {
			return nil
		}
		return &Error{Code: CodeInvalidParams, Message: "params object is required"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &Error{Code: CodeInvalidParams, Message: fmt.Sprintf("could not parse the parameters: %v", err)}
	}
	return nil
}

// serverErrorReason, masaüstünün çevirebileceği kısa sebep: ileti İngilizce
// kalır, arayüz sebebe göre kendi dilinde söyler.
func serverErrorReason(err error) string {
	switch {
	case errors.Is(err, servers.ErrDuplicateName):
		return "duplicate_name"
	case errors.Is(err, servers.ErrDuplicateTarget):
		return "duplicate_target"
	case errors.Is(err, servers.ErrNotFound):
		return "not_found"
	case errors.Is(err, servers.ErrNewerFile):
		return "newer_file"
	}
	return ""
}

func nonNil(list []servers.Server) []servers.Server {
	if list == nil {
		return []servers.Server{}
	}
	return list
}
