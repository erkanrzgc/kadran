package exec

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/erkanrzgc/panely/internal/audit"
)

// Journal, executor'ın kendi hash-zincirli denetim günlüğüdür.
//
// # Neden ayrı bir günlük?
//
// Denetim zincirinin ana kopyası panelyd'nin SQLite veritabanındadır. Ama
// panelyd'nin ele geçirilmesi tehdit modelimizin merkezinde: eğer ayrıcalıklı
// işlemlerin kaydı yalnızca panelyd'de tutulsaydı, ele geçirilmiş bir panelyd
// kendi yaptığı çağrıları hiç kaydetmeyebilirdi.
//
// Executor bu yüzden kendi eylemlerini kendi dosyasına yazar. Dosya
// 0640 root:panely'dir: panelyd OKUYABİLİR, YAZAMAZ. VerifyAuditChain iki
// zinciri karşılaştırır ve panelyd'nin düşürdüğü her kayıt fark olarak
// ortaya çıkar.
//
// # Neden SQLite değil?
//
// SQLite'ı buraya bağlamak, ~200 bin satırlık bir SQL motorunu ayrıcalıklı
// sürecin içine sokmak demek. Bu günlük ~150 satır. Ayrıcalıklı yüzeyi
// küçük tutmak modelin tamamının dayandığı kural; kolaylık için ondan
// taviz verilmez.
//
// # Depolama biçimi
//
// Satır başına bir JSON nesnesi. Hash JSON üzerinden DEĞİL, audit paketinin
// kanonik ikili kodlaması üzerinden hesaplanır — böylece depolama biçimi
// değişse bile zincir geçerli kalır.
type Journal struct {
	path string

	// mu yalnızca yazmayı korur. Okuma ayrı bir salt-okunur tanıtıcı
	// açtığı için uzun bir okuma eklemeleri bloklamaz.
	mu   sync.Mutex
	f    *os.File
	head [audit.HashSize]byte
	seq  uint64
}

// JournalOptions, günlük açılışını yapılandırır.
type JournalOptions struct {
	// Path, günlük dosyasının yoludur.
	Path string

	// GroupGID, dosya yeni oluşturulduğunda atanacak gruptur.
	// panelyd'nin okuyabilmesi için `panely` grubu verilir.
	// Sıfır ise grup değiştirilmez.
	GroupGID int
}

// OpenJournal, günlüğü açar veya oluşturur, mevcut zinciri doğrular ve
// yazmaya hazır bir Journal döndürür.
//
// Açılışta zincir bozuksa hata döner ve executor BAŞLAMAZ. Bu kasıtlıdır:
// denetim bütünlüğü doğrulanamıyorsa ayrıcalıklı işlem yapılmamalıdır.
func OpenJournal(opts JournalOptions) (*Journal, error) {
	if opts.Path == "" {
		return nil, errors.New("journal: yol boş olamaz")
	}

	created := false
	if _, err := os.Stat(opts.Path); errors.Is(err, os.ErrNotExist) {
		created = true
	}

	// 0640: sahibi (root) yazar, grubu (panely) okur, diğerleri hiçbir şey.
	f, err := os.OpenFile(opts.Path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("journal: dosya açılamadı: %w", err)
	}

	if created && opts.GroupGID > 0 {
		if err := f.Chown(0, opts.GroupGID); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("journal: grup atanamadı: %w", err)
		}
	}

	j := &Journal{path: opts.Path, f: f}
	if err := j.loadAndVerify(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return j, nil
}

// loadAndVerify, mevcut zinciri baştan sona okuyup doğrular ve başı belirler.
func (j *Journal) loadAndVerify() error {
	if _, err := j.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("journal: başa sarılamadı: %w", err)
	}

	v := audit.NewVerifier()
	sc := bufio.NewScanner(j.f)
	// Denetim kayıtları params_json taşıyabilir; varsayılan 64 KB sınırı
	// yetmeyebilir.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		rec, err := decodeLine(line)
		if err != nil {
			return fmt.Errorf("journal: kayıt %d çözümlenemedi: %w", v.NextSeq(), err)
		}
		if err := v.Next(rec); err != nil {
			return fmt.Errorf("journal: zincir doğrulanamadı: %w", err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("journal: okuma hatası: %w", err)
	}

	j.head = v.Head()
	j.seq = v.NextSeq() - 1
	return nil
}

// Append, kaydı mühürleyip günlüğe ekler ve diske yazar.
//
// Sıra numarası ve önceki hash çağırandan alınmaz; günlüğün o anki
// başından türetilir.
func (j *Journal) Append(rec audit.Record) (audit.Record, error) {
	if rec.Action == "" {
		return audit.Record{}, errors.New("journal: action alanı boş olamaz")
	}
	if !rec.Outcome.Valid() {
		return audit.Record{}, fmt.Errorf("journal: geçersiz outcome: %d", uint8(rec.Outcome))
	}
	// Executor'ın yazdığı her kayıt executor kaynaklıdır; çağıran bunu
	// değiştiremez.
	rec.Source = audit.SourceExecutor
	if rec.TS.IsZero() {
		rec.TS = time.Now().UTC()
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	sealed := audit.Seal(rec, j.seq+1, j.head)

	line, err := encodeLine(sealed)
	if err != nil {
		return audit.Record{}, err
	}
	if _, err := j.f.Write(line); err != nil {
		return audit.Record{}, fmt.Errorf("journal: yazılamadı: %w", err)
	}
	// Denetim kaydı çökme sonrası kaybolmamalı: ayrıcalıklı bir işlemin
	// yapılıp kaydının kaybolması, hiç yapılmamasından daha kötüdür.
	if err := j.f.Sync(); err != nil {
		return audit.Record{}, fmt.Errorf("journal: diske yazılamadı: %w", err)
	}

	j.head = sealed.Hash
	j.seq = sealed.Seq
	return sealed, nil
}

// Read, afterSeq'ten sonraki kayıtları döndürür ve döndürdüğü her kaydın
// zincir bütünlüğünü OKUMA ANINDA yeniden doğrular.
//
// # Neden açılıştaki doğrulama yetmiyor?
//
// loadAndVerify yalnızca executor başlarken çalışır. Bu metodun tek gerçek
// tüketicisi panelyd'nin ReadAuditJournal çağrısıdır ve amacı kendi
// zincirini bu zincirle ÇAPRAZ DOĞRULAMAKTIR. Executor çalışırken dosyaya
// sahte bir satır eklenirse, okuma anında doğrulama olmadan bu satır
// "gerçek" gibi teslim edilirdi ve çapraz doğrulama iki doğrulanmamış
// zinciri karşılaştırmış olurdu — yani hiçbir şey kanıtlamazdı.
//
// Doğrulama daima seq 1'den başlar: zincir ancak baştan takip edilerek
// kanıtlanabilir. Bu O(n)'dir, ama executor günlüğü yapısı gereği küçüktür
// (yalnızca ayrıcalıklı işlemler) ve doğruluk burada hızdan önce gelir.
//
// Doğrulama, döndürülen son kaydı KAPSAYACAK şekilde yapılır. Ondan
// sonraki kayıtlar döndürülmediği için geçerlilikleri sonucu etkilemez.
func (j *Journal) Read(afterSeq uint64, limit int) ([]audit.Record, error) {
	if limit <= 0 {
		limit = 100
	}

	// Ayrı salt-okunur tanıtıcı: yazma tanıtıcısının konumuna dokunmaz ve
	// j.mu'yu tutmadığı için uzun bir okuma eklemeleri bloklamaz.
	f, err := os.Open(j.path)
	if err != nil {
		return nil, fmt.Errorf("journal: okuma için açılamadı: %w", err)
	}
	defer func() { _ = f.Close() }()

	v := audit.NewVerifier()
	var out []audit.Record

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}

		rec, err := decodeLine(line)
		if err != nil {
			return nil, fmt.Errorf("journal: kayıt %d çözümlenemedi: %w", v.NextSeq(), err)
		}
		if err := v.Next(rec); err != nil {
			return nil, fmt.Errorf("journal: okuma sırasında zincir doğrulanamadı: %w", err)
		}

		if rec.Seq > afterSeq {
			out = append(out, rec)
			if len(out) >= limit {
				break
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("journal: okuma hatası: %w", err)
	}
	return out, nil
}

// Head, son kaydın sıra numarasını ve hash'ini döndürür.
func (j *Journal) Head() (uint64, [audit.HashSize]byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.seq, j.head
}

// Close, günlüğü kapatır.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.f.Close()
}

// ── Satır kodlaması ──────────────────────────────────────────────────

// journalLine, günlüğün disk üzerindeki gösterimidir.
//
// Hash bu yapı üzerinden DEĞİL, audit.ComputeHash ile kanonik ikili
// kodlamadan hesaplanır. JSON burada yalnızca depolama ve insan tarafından
// okunabilirlik içindir; zincirin doğruluğu JSON'un ayrıntılarına bağlı
// değildir.
type journalLine struct {
	Seq         uint64 `json:"seq"`
	TSUnixNano  int64  `json:"ts"`
	ActorFP     string `json:"actor_fp,omitempty"`
	ActorIP     string `json:"actor_ip,omitempty"`
	ActorLabel  string `json:"actor_label,omitempty"`
	ActorOrigin string `json:"actor_origin,omitempty"`
	Action      string `json:"action"`
	Target      string `json:"target,omitempty"`
	ParamsJSON  string `json:"params,omitempty"`
	Outcome     uint8  `json:"outcome"`
	Detail      string `json:"detail,omitempty"`
	Source      uint8  `json:"source"`
	PrevHash    string `json:"prev"`
	Hash        string `json:"hash"`
}

func encodeLine(r audit.Record) ([]byte, error) {
	l := journalLine{
		Seq:         r.Seq,
		TSUnixNano:  r.TS.UnixNano(),
		ActorFP:     r.Actor.KeyFingerprint,
		ActorIP:     r.Actor.SourceIP,
		ActorLabel:  r.Actor.Label,
		ActorOrigin: r.Actor.Origin,
		Action:      r.Action,
		Target:      r.Target,
		ParamsJSON:  r.ParamsJSON,
		Outcome:     uint8(r.Outcome),
		Detail:      r.Detail,
		Source:      uint8(r.Source),
		PrevHash:    hex.EncodeToString(r.PrevHash[:]),
		Hash:        hex.EncodeToString(r.Hash[:]),
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("journal: kayıt kodlanamadı: %w", err)
	}
	return append(b, '\n'), nil
}

func decodeLine(b []byte) (audit.Record, error) {
	var l journalLine
	if err := json.Unmarshal(b, &l); err != nil {
		return audit.Record{}, err
	}

	prev, err := decodeHash(l.PrevHash)
	if err != nil {
		return audit.Record{}, fmt.Errorf("prev_hash: %w", err)
	}
	h, err := decodeHash(l.Hash)
	if err != nil {
		return audit.Record{}, fmt.Errorf("hash: %w", err)
	}

	return audit.Record{
		Seq: l.Seq,
		TS:  time.Unix(0, l.TSUnixNano).UTC(),
		Actor: audit.Actor{
			KeyFingerprint: l.ActorFP,
			SourceIP:       l.ActorIP,
			Label:          l.ActorLabel,
			Origin:         l.ActorOrigin,
		},
		Action:     l.Action,
		Target:     l.Target,
		ParamsJSON: l.ParamsJSON,
		Outcome:    audit.Outcome(l.Outcome),
		Detail:     l.Detail,
		Source:     audit.Source(l.Source),
		PrevHash:   prev,
		Hash:       h,
	}, nil
}

func decodeHash(s string) ([audit.HashSize]byte, error) {
	var out [audit.HashSize]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	if len(b) != audit.HashSize {
		return out, fmt.Errorf("uzunluk %d, beklenen %d", len(b), audit.HashSize)
	}
	copy(out[:], b)
	return out, nil
}
