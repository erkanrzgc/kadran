package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/erkanrzgc/kadran/internal/appnames"
)

// ErrNameTaken, bir alan adının (domain, türetilmiş www eşi ya da açık
// yönlendirme adı) BAŞKA bir uygulamanın adlarıyla çakıştığını bildirir.
//
// ErrDomainTaken'dan ayrı: o, benzersiz indeksin domain ↔ domain çakışmasıdır;
// bu ise indeksin göremediği çapraz çakışmadır (eş ↔ domain, açık ad ↔ eş).
var ErrNameTaken = errors.New("name already in use")

// sortedNames, adları sıralar, tekrarları atar ve boşsa nil döner.
//
// nil (boş dilim DEĞİL): "ad yok" durumu yazma yoluna göre iki farklı
// temsile ayrışmasın. Kopya döner, girdiye dokunmaz.
func sortedNames(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := slices.Clone(in)
	sort.Strings(out)
	return slices.Compact(out)
}

// marshalNames, adları sütun biçimine serileştirir ("[]" = ad yok).
func marshalNames(names []string) (string, error) {
	if len(names) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(names)
	if err != nil {
		return "", fmt.Errorf("could not serialize redirect names: %w", err)
	}
	return string(b), nil
}

// unmarshalNames, sütundan adları okur; "[]" ve "null" nil üretir.
func unmarshalNames(raw string) ([]string, error) {
	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, fmt.Errorf("could not decode redirect names: %w", err)
	}
	return sortedNames(names), nil
}

// rowsQuerier, *sql.DB ve *sql.Tx'in çoklu satır okuma yüzeyidir.
type rowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// checkNames, uygulamanın adlarını diğer tüm uygulamalarınkiyle karşılaştırır.
//
// Okuma, yazmayla AYNI işlemde yapılır (DSN _txlock=immediate ve tek bağlantı
// olduğundan arada başka yazar giremez).
//
// Domaini aynı olan uygulama karşılaştırmadan ÇIKARILIR: o çakışmayı benzersiz
// indeks ErrDomainTaken ile zaten doğru adlandırır ve burada ErrNameTaken'a
// çevirmek eski davranışı bozardı.
func checkNames(ctx context.Context, q rowsQuerier, c appnames.Owner) error {
	rows, err := q.QueryContext(ctx, `SELECT id, domain, www, redirects_json FROM apps`)
	if err != nil {
		return fmt.Errorf("could not read the app names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var others []appnames.Owner
	for rows.Next() {
		var (
			o   appnames.Owner
			raw string
		)
		if err := rows.Scan(&o.AppID, &o.Domain, &o.WWW, &raw); err != nil {
			return fmt.Errorf("could not read an app name row: %w", err)
		}
		if o.AppID == c.AppID || (c.Domain != "" && o.Domain == c.Domain) {
			continue
		}
		if o.Redirects, err = unmarshalNames(raw); err != nil {
			return err
		}
		others = append(others, o)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("could not read the app names: %w", err)
	}

	if name, owner, found := appnames.Conflict(c, others); found {
		return fmt.Errorf("%w: %q is already used by app %q", ErrNameTaken, name, owner)
	}
	return nil
}
