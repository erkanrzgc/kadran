// Package audit, Panely'nin kurcalamaya-duyarlı denetim zincirini uygular
// (şartname §1.3).
//
// Bu paket kasıtlı olarak G/Ç içermez. Zincir matematiği saf fonksiyonlardan
// oluşur; kalıcılık internal/store'un işidir. Bu ayrım sayesinde zincirin
// doğruluğu veritabanı olmadan test edilebilir.
//
// # Kurcalama modeli
//
// Zincir, geçmişin değiştirilmesini *engellemez* — dosyaya yazma erişimi olan
// biri satırları değiştirebilir. Zincirin sağladığı şey, bunun *tespit
// edilebilir* olmasıdır: bir satır değişirse kendisinden sonraki her hash
// geçersiz olur ve saldırganın tüm zinciri yeniden hesaplaması gerekir.
//
// Veritabanı katmanındaki AFTER UPDATE/DELETE tetikleyicileri ikinci savunma
// hattıdır: uygulama hataları ve SQL enjeksiyonu geçmişi değiştiremez.
// Faz 5'te zincir başının R2'ye (object lock ile) periyodik olarak
// çıpalanması üçüncü hattı ekleyecek — o noktada tam yeniden hesaplama da
// işe yaramaz hale gelir.
package audit

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// HashSize, zincirde kullanılan SHA-256 özet uzunluğudur.
const HashSize = 32

// GenesisHash, ilk kaydın prev_hash değeridir: 32 sıfır bayt.
var GenesisHash = [HashSize]byte{}

// Source, kaydı hangi bileşenin ürettiğini belirtir.
//
// Executor kendi eylemlerini kendi günlüğüne yazar. panelyd ele geçirilip
// bir kaydı düşürse bile, iki zincirin karşılaştırılması farkı ortaya
// çıkarır — ayrıcalıklı işlemler kayıtsız kalamaz.
type Source uint8

const (
	SourceDaemon   Source = 1
	SourceExecutor Source = 2
)

func (s Source) Valid() bool { return s == SourceDaemon || s == SourceExecutor }

func (s Source) String() string {
	switch s {
	case SourceDaemon:
		return "daemon"
	case SourceExecutor:
		return "executor"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(s))
	}
}

// Outcome, eylemin nasıl sonuçlandığını belirtir.
type Outcome uint8

const (
	OutcomeSuccess Outcome = 1
	OutcomeFailure Outcome = 2
	// OutcomeDenied, isteğin şema veya değişmez doğrulamasında
	// reddedildiğini gösterir. Güvenlik modelinin devreye girdiği an budur
	// ve ayrıca izlenir.
	OutcomeDenied Outcome = 3
)

func (o Outcome) Valid() bool { return o >= OutcomeSuccess && o <= OutcomeDenied }

func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeFailure:
		return "failure"
	case OutcomeDenied:
		return "denied"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(o))
	}
}

// Actor, eylemi kimin başlattığını tanımlar.
//
// Panely'de parola girişi yoktur; gerçek kimlik SSH açık anahtarının
// parmak izidir. SourceIP, şartname §1.3'ün "IP adresiyle kaydedilir"
// gereksinimini karşılar ve hash yüküne dahildir.
type Actor struct {
	KeyFingerprint string // "SHA256:..." — sistem eylemlerinde boş
	SourceIP       string // sistem eylemlerinde boş
	Label          string // authorized_keys yorumu
	Origin         string // "cli" | "gui" | "scheduler" | "executor"
}

// SystemActor, zamanlayıcı gibi insan tetiklemeyen eylemler içindir.
func SystemActor(origin string) Actor {
	return Actor{Label: "system", Origin: origin}
}

// Record, zincirdeki tek bir halkadır.
type Record struct {
	Seq        uint64
	TS         time.Time
	Actor      Actor
	Action     string // "app.deploy", "container.create", ...
	Target     string // "app/blog", "volume/blog-data", ...
	ParamsJSON string // sırlar redakte edilmiş hâlde
	Outcome    Outcome
	Detail     string
	Source     Source
	PrevHash   [HashSize]byte
	Hash       [HashSize]byte
}

// ComputeHash, kaydın hash'ini alanlarından ve PrevHash'ten hesaplar.
// Record.Hash alanı hesaba KATILMAZ.
//
// # Kanonik kodlama neden JSON değil?
//
// JSON kanonikleştirmesi ince tuzaklarla doludur: anahtar sıralaması,
// unicode kaçışları, ondalık gösterim, boşluk. Farklı bir Go sürümü veya
// farklı bir kütüphane aynı kayıt için farklı bayt üretebilir ve zincir
// sebepsiz kopar.
//
// Bunun yerine açık, uzunluk-önekli ikili kodlama kullanılır. Uzunluk
// öneki klasik zincir hatasını da engeller: önek olmadan ("ab","c") ve
// ("a","bc") aynı baytlara serileşir ve iki farklı kayıt aynı hash'i
// üretebilirdi.
func ComputeHash(r Record) [HashSize]byte {
	h := sha256.New()

	writeUint(h, r.Seq)
	writeUint(h, uint64(r.TS.UnixNano()))

	writeStr(h, r.Actor.KeyFingerprint)
	writeStr(h, r.Actor.SourceIP)
	writeStr(h, r.Actor.Label)
	writeStr(h, r.Actor.Origin)

	writeStr(h, r.Action)
	writeStr(h, r.Target)
	writeStr(h, r.ParamsJSON)

	writeUint(h, uint64(r.Outcome))
	writeStr(h, r.Detail)
	writeUint(h, uint64(r.Source))

	h.Write(r.PrevHash[:])

	var out [HashSize]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Seal, kaydın sırasını ve önceki hash'ini yerleştirip hash'ini hesaplar.
// Kalıcılaştırmadan hemen önce çağrılır.
func Seal(r Record, seq uint64, prev [HashSize]byte) Record {
	r.Seq = seq
	r.PrevHash = prev
	r.Hash = ComputeHash(r)
	return r
}

// hashWriter, sha256.New()'in döndürdüğü hash.Hash arayüzünün ihtiyaç
// duyduğumuz kadarıdır. Write hiçbir zaman hata döndürmez.
type hashWriter interface{ Write([]byte) (int, error) }

func writeUint(h hashWriter, v uint64) {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	_, _ = h.Write(buf[:])
}

func writeStr(h hashWriter, s string) {
	writeUint(h, uint64(len(s)))
	_, _ = h.Write([]byte(s))
}

// ── Doğrulama ────────────────────────────────────────────────────────

// ErrChainBroken, zincirin bir noktada geçersiz olduğunu belirtir.
var ErrChainBroken = errors.New("denetim zinciri geçersiz")

// Verifier, zinciri akış hâlinde doğrular; tüm kayıtları belleğe almaz.
//
// Kullanım:
//
//	v := audit.NewVerifier()
//	for rec := range kayitlar {
//	    if err := v.Next(rec); err != nil { ... }
//	}
type Verifier struct {
	prev    [HashSize]byte
	nextSeq uint64
	count   uint64
}

// NewVerifier, zincirin başından başlayan bir doğrulayıcı döndürür.
func NewVerifier() *Verifier {
	return &Verifier{prev: GenesisHash, nextSeq: 1}
}

// Next, sıradaki kaydı doğrular. İlk uyumsuzlukta hata döner ve
// doğrulayıcı artık kullanılmamalıdır.
func (v *Verifier) Next(r Record) error {
	if r.Seq != v.nextSeq {
		return fmt.Errorf("%w: sıra numarası atlanmış, beklenen %d, gelen %d",
			ErrChainBroken, v.nextSeq, r.Seq)
	}
	if r.PrevHash != v.prev {
		return fmt.Errorf("%w: kayıt %d'in prev_hash değeri zincirle uyuşmuyor",
			ErrChainBroken, r.Seq)
	}
	if !r.Outcome.Valid() {
		return fmt.Errorf("%w: kayıt %d geçersiz outcome değeri taşıyor: %d",
			ErrChainBroken, r.Seq, uint8(r.Outcome))
	}
	if !r.Source.Valid() {
		return fmt.Errorf("%w: kayıt %d geçersiz source değeri taşıyor: %d",
			ErrChainBroken, r.Seq, uint8(r.Source))
	}
	if want := ComputeHash(r); want != r.Hash {
		return fmt.Errorf("%w: kayıt %d kurcalanmış (hash uyuşmuyor)",
			ErrChainBroken, r.Seq)
	}

	v.prev = r.Hash
	v.nextSeq = r.Seq + 1
	v.count++
	return nil
}

// Count, şu ana kadar doğrulanan kayıt sayısını döndürür.
func (v *Verifier) Count() uint64 { return v.count }

// Head, doğrulanan son kaydın hash'ini döndürür. Hiç kayıt yoksa
// GenesisHash döner. Faz 5'te bu değer R2'ye çıpalanacak.
func (v *Verifier) Head() [HashSize]byte { return v.prev }

// NextSeq, eklenecek bir sonraki kaydın sıra numarasını döndürür.
func (v *Verifier) NextSeq() uint64 { return v.nextSeq }

// VerifyAll, bellekteki bir dilimi doğrular. Küçük zincirler ve testler
// için pratiktir; büyük zincirlerde Verifier'ı doğrudan kullanın.
func VerifyAll(records []Record) (checked uint64, err error) {
	v := NewVerifier()
	for _, r := range records {
		if err := v.Next(r); err != nil {
			return v.Count(), err
		}
	}
	return v.Count(), nil
}
