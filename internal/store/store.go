// Package store, Panely kontrol düzleminin kalıcı durumunu yönetir.
//
// # Neden SQLite?
//
// Kontrol düzleminde yüksek eşzamanlı yazma yoktur: bir avuç uygulama, tek
// bir zamanlayıcı, seyrek dağıtımlar. Postgres bu iş yükü için ayrı bir
// servis, ayrı bir yedekleme yolu ve bir tavuk-yumurta problemi getirirdi —
// "Panely'nin veritabanını Panely mi yönetsin?"
//
// SQLite bunların hepsini siler: gömülüdür, yedeği tek dosyadır ve Faz 5'te
// Litestream ile R2'ye sürekli replike edilebilir.
//
// Sürücü olarak modernc.org/sqlite (saf Go) kullanılır. CGO'suz olması
// Windows iş istasyonundan linux/amd64 ve linux/arm64 hedeflerine çapraz
// derleme yapabilmemizi sağlar; `panely bootstrap`'in "tek komut kurulum"
// vaadi buna bağlıdır.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite" // sqlite sürücüsü

	"github.com/erkanrzgc/panely/internal/audit"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store, kontrol düzlemi veritabanına erişimi kapsüller.
type Store struct {
	db *sql.DB

	// appendMu, denetim zincirine eklemeyi serileştirir.
	//
	// BEGIN IMMEDIATE zaten süreçler arası yarışı engeller, ancak bu
	// mutex süreç içi çekişmeyi SQLite'ın meşgul-bekleme döngüsüne
	// girmeden çözer. Zincir sırası doğruluğun parçası olduğu için
	// burada ucuz olmak yerine bariz olmak tercih edilir.
	appendMu sync.Mutex
}

// Open, veritabanını açar, göçleri uygular ve kullanıma hazır bir Store
// döndürür. path dosya yoludur; ":memory:" testlerde kullanılabilir.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := buildDSN(path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite açılamadı: %w", err)
	}

	// SQLite tek yazarlıdır; bağlantı havuzunu büyütmek çekişmeyi
	// azaltmaz, yalnızca "database is locked" olasılığını artırır.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite'a bağlanılamadı: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("göçler uygulanamadı: %w", err)
	}
	return s, nil
}

func buildDSN(path string) string {
	if path == ":memory:" {
		// Paylaşımlı önbellek olmadan her bağlantı ayrı bir bellek
		// veritabanı görür; MaxOpenConns=1 ile bu güvenlidir.
		return "file::memory:?_pragma=foreign_keys(1)"
	}
	// _txlock=immediate: her transaction BEGIN IMMEDIATE ile başlar.
	//
	// Varsayılan (deferred) modda transaction yazma kilidini ilk yazmada
	// alır. Denetim eklemesi önce zincir başını OKUR, sonra YAZAR; deferred
	// modda iki eşzamanlı ekleme aynı başı okuyup yazmada çakışabilir ve
	// SQLite bunu yükseltemeyeceği için SQLITE_BUSY döner (bekleme değil,
	// anında hata). IMMEDIATE bu sınıfı tamamen kapatır.
	return "file:" + filepath.ToSlash(path) +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
}

// Close, veritabanını kapatır.
func (s *Store) Close() error { return s.db.Close() }

// DB, alt seviye erişim gerektiren paketler için kullanılır. Faz 1'de
// uygulama tabloları buradan yararlanacak.
func (s *Store) DB() *sql.DB { return s.db }

// ── Göçler ───────────────────────────────────────────────────────────

func (s *Store) migrate(ctx context.Context) error {
	const createTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		) STRICT;`
	if _, err := s.db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("schema_migrations oluşturulamadı: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("göç dizini okunamadı: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	// Dosya adları sıfır dolgulu olduğu için sözlük sırası = uygulama sırası.
	sort.Strings(names)

	for _, name := range names {
		applied, err := s.migrationApplied(ctx, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := s.applyMigration(ctx, name); err != nil {
			return fmt.Errorf("göç %s uygulanamadı: %w", name, err)
		}
	}
	return nil
}

func (s *Store) migrationApplied(ctx context.Context, name string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = ?`, name).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("göç durumu okunamadı: %w", err)
	default:
		return true, nil
	}
}

func (s *Store) applyMigration(ctx context.Context, name string) error {
	body, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("göç dosyası okunamadı: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		name, time.Now().UnixNano(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// ── Denetim zinciri ──────────────────────────────────────────────────

// AppendAudit, kaydı zincirin sonuna mühürleyerek ekler.
//
// Sıra numarası ve önceki hash çağırandan alınmaz; veritabanının o anki
// başından okunur. Böylece çağıran zinciri yanlış sıraya sokamaz.
//
// Mühürlenmiş kayıt geri döndürülür; Seq ve Hash alanları doludur.
func (s *Store) AppendAudit(ctx context.Context, rec audit.Record) (audit.Record, error) {
	if rec.Action == "" {
		return audit.Record{}, errors.New("denetim kaydının action alanı boş olamaz")
	}
	if !rec.Outcome.Valid() {
		return audit.Record{}, fmt.Errorf("geçersiz outcome: %d", uint8(rec.Outcome))
	}
	if !rec.Source.Valid() {
		return audit.Record{}, fmt.Errorf("geçersiz source: %d", uint8(rec.Source))
	}
	if rec.TS.IsZero() {
		rec.TS = time.Now().UTC()
	}

	s.appendMu.Lock()
	defer s.appendMu.Unlock()

	// DSN'deki _txlock=immediate sayesinde bu BEGIN IMMEDIATE'tir:
	// yazma kilidi baştan alınır, oku-sonra-yaz yarışı oluşmaz.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return audit.Record{}, err
	}
	defer func() { _ = tx.Rollback() }()

	seq, prev, err := auditHeadTx(ctx, tx)
	if err != nil {
		return audit.Record{}, err
	}

	sealed := audit.Seal(rec, seq+1, prev)

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_log (
			seq, ts_unix_nano,
			actor_fp, actor_ip, actor_label, actor_origin,
			action, target, params_json,
			outcome, detail, source,
			prev_hash, hash
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sealed.Seq, sealed.TS.UnixNano(),
		sealed.Actor.KeyFingerprint, sealed.Actor.SourceIP,
		sealed.Actor.Label, sealed.Actor.Origin,
		sealed.Action, sealed.Target, sealed.ParamsJSON,
		uint8(sealed.Outcome), sealed.Detail, uint8(sealed.Source),
		sealed.PrevHash[:], sealed.Hash[:],
	); err != nil {
		return audit.Record{}, fmt.Errorf("denetim kaydı eklenemedi: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return audit.Record{}, err
	}
	return sealed, nil
}

// AuditHead, zincirdeki son sıra numarasını ve hash'ini döndürür.
// Zincir boşsa (0, GenesisHash) döner.
func (s *Store) AuditHead(ctx context.Context) (uint64, [audit.HashSize]byte, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, audit.GenesisHash, err
	}
	defer func() { _ = tx.Rollback() }()
	return auditHeadTx(ctx, tx)
}

func auditHeadTx(ctx context.Context, tx *sql.Tx) (uint64, [audit.HashSize]byte, error) {
	var (
		seq  sql.NullInt64
		hash []byte
	)
	err := tx.QueryRowContext(ctx,
		`SELECT seq, hash FROM audit_log ORDER BY seq DESC LIMIT 1`).Scan(&seq, &hash)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, audit.GenesisHash, nil
	}
	if err != nil {
		return 0, audit.GenesisHash, fmt.Errorf("zincir başı okunamadı: %w", err)
	}
	if len(hash) != audit.HashSize {
		return 0, audit.GenesisHash, fmt.Errorf(
			"bozuk zincir başı: hash uzunluğu %d, beklenen %d", len(hash), audit.HashSize)
	}

	var head [audit.HashSize]byte
	copy(head[:], hash)
	return uint64(seq.Int64), head, nil
}

// DefaultAuditLimit, ListAudit için varsayılan sayfa boyutudur.
const DefaultAuditLimit = 100

// MaxAuditLimit, ListAudit'in döndürebileceği en fazla kayıt sayısıdır.
const MaxAuditLimit = 1000

// ListAudit, afterSeq'ten sonraki kayıtları sıra numarasına göre döndürür.
func (s *Store) ListAudit(ctx context.Context, afterSeq uint64, limit int) ([]audit.Record, error) {
	switch {
	case limit <= 0:
		limit = DefaultAuditLimit
	case limit > MaxAuditLimit:
		limit = MaxAuditLimit
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT seq, ts_unix_nano,
		       actor_fp, actor_ip, actor_label, actor_origin,
		       action, target, params_json,
		       outcome, detail, source,
		       prev_hash, hash
		FROM audit_log
		WHERE seq > ?
		ORDER BY seq ASC
		LIMIT ?`, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("denetim kayıtları okunamadı: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []audit.Record
	for rows.Next() {
		rec, err := scanAuditRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// scanner, *sql.Row ve *sql.Rows'un ortak yüzeyidir.
type scanner interface{ Scan(dest ...any) error }

func scanAuditRecord(sc scanner) (audit.Record, error) {
	var (
		rec              audit.Record
		tsNano           int64
		outcome, source  uint8
		prevHash, hashBs []byte
	)
	if err := sc.Scan(
		&rec.Seq, &tsNano,
		&rec.Actor.KeyFingerprint, &rec.Actor.SourceIP,
		&rec.Actor.Label, &rec.Actor.Origin,
		&rec.Action, &rec.Target, &rec.ParamsJSON,
		&outcome, &rec.Detail, &source,
		&prevHash, &hashBs,
	); err != nil {
		return audit.Record{}, fmt.Errorf("denetim kaydı çözümlenemedi: %w", err)
	}

	if len(prevHash) != audit.HashSize || len(hashBs) != audit.HashSize {
		return audit.Record{}, fmt.Errorf(
			"kayıt %d bozuk hash uzunluğu taşıyor", rec.Seq)
	}

	rec.TS = time.Unix(0, tsNano).UTC()
	rec.Outcome = audit.Outcome(outcome)
	rec.Source = audit.Source(source)
	copy(rec.PrevHash[:], prevHash)
	copy(rec.Hash[:], hashBs)
	return rec, nil
}

// VerifyAuditChain, zincirin tamamını baştan sona yürür.
//
// Kayıtlar sayfa sayfa okunur; milyonlarca kayıtlık bir zincir de belleği
// doldurmadan doğrulanabilir.
func (s *Store) VerifyAuditChain(ctx context.Context) (checked uint64, err error) {
	const pageSize = 500

	v := audit.NewVerifier()
	var after uint64

	for {
		page, err := s.ListAudit(ctx, after, pageSize)
		if err != nil {
			return v.Count(), err
		}
		if len(page) == 0 {
			return v.Count(), nil
		}
		for _, rec := range page {
			if err := v.Next(rec); err != nil {
				return v.Count(), err
			}
		}
		after = page[len(page)-1].Seq
	}
}
