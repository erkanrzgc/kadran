package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/vault"
)

// ── Kasa (K-123) ─────────────────────────────────────────────────────
//
// Ortam değişkeni değerleri veritabanına yalnız executor'ın açık
// anahtarıyla mühürlenmiş olarak giriyor. Daemon değerleri yazar ama
// okuyamaz; executor konteyneri kurarken açıyor.

// ErrVaultRecipientChanged, veritabanındaki değerlerin yapılandırılandan
// başka bir alıcıya mühürlü olduğunu söyler: kayıp anahtarın yerine yenisi
// üretilmiş olabilir.
var ErrVaultRecipientChanged = errors.New("kasa alıcısı değişmiş")

// sealedHead, mühürlü bir değerin başı: önek + base64("age-encryption.org/v1").
// Yalnız ÇİFT mühürlemeyi önlemek için bakılıyor; bir değerin mühürlü olup
// olmadığına işaret (env_seal) karar veriyor.
const sealedHead = vault.Prefix + "YWdlLWVuY3J5cHRpb24ub3JnL3Yx"

// EnableVault, kasayı açar ve gerekiyorsa var olan değerleri mühürler.
//
// İşaret (env_seal) yoksa (eski sürüm, eski yedek, geri dönüş betiği)
// bütün değerler, boşlar dahil, tek transaction'da mühürlenir ve işaret
// yazılır. İşaret varsa ve alıcı farklıysa ErrVaultRecipientChanged döner:
// devam etmek, çözülemeyen değerlerle dağıtım demekti. İşaret varsa ama bir
// değer mühürlü değilse (geri dönüş betiği koşmadan kurulan eski bir sürüm
// yazmış) o değer de mühürlenir. Her mühürlemeden sonra eski düz metin
// dosyalardan temizlenir; temizlik ancak bitince işarete yazılır, araya
// giren bir çökme bir sonraki açılışta temizliği tekrarlatır. Dönen sayı
// mühürlenen değerlerdir.
func (s *Store) EnableVault(ctx context.Context, sealer *vault.Sealer) (int, error) {
	var recipient string
	var scrubbed sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT recipient, scrubbed_at FROM env_seal WHERE id = 1`).Scan(&recipient, &scrubbed)
	marked := err == nil
	switch {
	case marked && recipient != sealer.Recipient():
		return 0, fmt.Errorf("%w: değerler %s için mühürlü, yapılandırılan alıcı %s",
			ErrVaultRecipientChanged, recipient, sealer.Recipient())
	case !marked && !errors.Is(err, sql.ErrNoRows):
		return 0, fmt.Errorf("kasa işareti okunamadı: %w", err)
	}

	n, err := s.sealAllEnv(ctx, sealer, marked)
	if err != nil {
		return 0, err
	}
	if !marked || n > 0 || !scrubbed.Valid {
		if err := s.scrub(ctx); err != nil {
			return 0, err
		}
	}
	s.sealer = sealer
	return n, nil
}

// scrub, eski düz metni veritabanı dosyalarından temizler ve bunu işarete
// yazar.
//
// Silinmiş uygulamaların ve eski değerlerin sayfaları boş listede
// içerikleriyle duruyor (secure_delete kapalı); VACUUM dosyayı baştan
// kuruyor. WAL kipinde yeni sayfalar WAL'a yazılıyor; checkpoint onları
// dosyaya aktarıp WAL'ı kesiyor. İkisi de ölçüldü (mutate-kasa.sh):
// VACUUM'suz silinen bir uygulamanın sırrının 30 parçasından 17'si,
// checkpoint'siz WAL'daki düz metin kalıyor. Checkpoint engellenirse
// sonucunu satırda bildiriyor, hata olarak değil; o yüzden okunuyor.
func (s *Store) scrub(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("eski düz metin temizlenemedi (VACUUM): %w", err)
	}
	var busy, logFrames, moved int
	if err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &moved); err != nil {
		return fmt.Errorf("eski düz metin temizlenemedi (WAL): %w", err)
	}
	if busy != 0 {
		return errors.New("eski düz metin temizlenemedi: WAL kesilemedi (meşgul)")
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE env_seal SET scrubbed_at = ? WHERE id = 1`, time.Now().UnixNano()); err != nil {
		return fmt.Errorf("temizlik işarete yazılamadı: %w", err)
	}
	return nil
}

// sealAllEnv, mühürlenmemiş değerleri mühürler. İşaret yoksa bütün değerler
// düz sayılır; mühürlü görünen bir değer çift mühürlemeye yol açacağı için
// durulur. İşaret varsa yalnız mühürlü OLMAYAN değerler mühürlenir.
func (s *Store) sealAllEnv(ctx context.Context, sealer *vault.Sealer, marked bool) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("kasa transaction'ı açılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	envs, err := readAllEnv(ctx, tx)
	if err != nil {
		return 0, err
	}
	n := 0
	for id, env := range envs {
		plain := map[string]string{}
		for k, v := range env {
			switch {
			case !strings.HasPrefix(v, sealedHead):
				plain[k] = v
			case !marked:
				return 0, fmt.Errorf("%s/%s zaten mühürlü görünüyor ama kasa işareti yok; "+
					"ikinci kez mühürlenmedi, elle bakılmalı", id, k)
			}
		}
		if len(plain) == 0 {
			continue
		}
		sealed, err := sealer.SealEnv(id, plain)
		if err != nil {
			return 0, err
		}
		merged := make(map[string]string, len(env))
		for k, v := range env {
			merged[k] = v
		}
		for k, v := range sealed {
			merged[k] = v
		}
		data, err := json.Marshal(merged)
		if err != nil {
			return 0, fmt.Errorf("ortam değişkenleri serileştirilemedi: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE apps SET env_json = ? WHERE id = ?`, string(data), id); err != nil {
			return 0, fmt.Errorf("%s mühürlenemedi: %w", id, err)
		}
		n += len(plain)
	}
	if !marked {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO env_seal (id, recipient, sealed_at) VALUES (1, ?, ?)`,
			sealer.Recipient(), time.Now().UnixNano()); err != nil {
			return 0, fmt.Errorf("kasa işareti yazılamadı: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mühürlenen değerler yazılamadı: %w", err)
	}
	return n, nil
}

func readAllEnv(ctx context.Context, tx *sql.Tx) (map[string]map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, env_json FROM apps`)
	if err != nil {
		return nil, fmt.Errorf("ortam değişkenleri okunamadı: %w", err)
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("ortam değişkenleri okunamadı: %w", err)
		}
		var env map[string]string
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			return nil, fmt.Errorf("%s ortam değişkenleri çözümlenemedi: %w", id, err)
		}
		out[id] = env
	}
	return out, rows.Err()
}

// sealEnv, yazılacak değerleri mühürler. Kasa açılmadan değer yazılamaz:
// düz metin veritabanına hiç girmemeli. Boş harita kasasız da geçer.
func (s *Store) sealEnv(appID string, env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	if s.sealer == nil {
		return nil, errors.New("kasa açılmadı: ortam değişkeni yazılamaz")
	}
	return s.sealer.SealEnv(appID, env)
}
