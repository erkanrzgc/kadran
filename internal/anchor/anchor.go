// Package anchor, daemon denetim zincirinin host dışına çıkan çapalarını
// üretir ve denetler (K-126 C).
//
// # Sorun
//
// Ele geçirilmiş bir kadrand kendi zincirini baştan yazabilir. Yeni zincir
// kendi içinde tutarlı olduğu için `audit verify` onu "geçerli" bulur.
//
// # Çapa
//
// Daemon her yedeğin yanına zincirin o anki ucunu (seq, hash) yazar; uzak
// yedek bu dosyayı R2'ye `kadran-` önekiyle yükler ve kova kilidi onu 30
// gün değiştirilemez kılar. Sonradan yazılmış bir geçmiş, o çapadaki
// hash'i üretemez: bunun için eski kayıtların kendisi gerekir.
//
// # Neyi KORUMAZ
//
//   - Yalnızca daemon zinciri; executor'ınki çapalanmıyor.
//   - En fazla kilit süresi (30 gün) geriye ve en son çapaya kadar.
//   - Daemon kullanıcısı R2 token'ını okuyabildiği için SAHTE çapa
//     ekleyebilir. Bu yüzden çelişen TEK bir çapa bile sonucu kırmızı yapar
//     ve dosyalar katı ayrıştırılır.
package anchor

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/audit"
)

// Ext, çapa dosyasının uzantısı. Ad, yanındaki yedekle aynı damgayı taşır:
// kadran-20261003T193000Z.db ↔ kadran-20261003T193000Z.capa.
const Ext = ".capa"

const (
	adOnEki    = "kadran-"
	damga      = "20060102T150405Z"
	surum      = "kadran-capa 1"
	maxBoyut   = 256
	pencereGun = 30
)

// Anchor, zincirin bir anındaki ucu.
type Anchor struct {
	Seq   uint64
	Hash  [audit.HashSize]byte
	Taken time.Time // dosya adındaki damgadan
	Name  string
}

// FileName, verilen anda alınan çapanın dosya adı.
func FileName(taken time.Time) string {
	return adOnEki + taken.UTC().Format(damga) + Ext
}

// Format, çapa dosyasının içeriği. Biçim bilerek tek ve sabit: Parse
// içeriği yeniden üretip BAYT BAYT karşılaştırıyor.
func Format(seq uint64, hash [audit.HashSize]byte) []byte {
	return []byte(fmt.Sprintf("%s\nseq %d\nhash %s\n", surum, seq, hex.EncodeToString(hash[:])))
}

// Parse, çapayı adından ve içeriğinden okur. Çapalar saldırganın
// yazabildiği girdi: biçimden en küçük sapma hata.
func Parse(name string, data []byte) (Anchor, error) {
	if filepath.Base(name) != name || !strings.HasPrefix(name, adOnEki) || !strings.HasSuffix(name, Ext) {
		return Anchor{}, fmt.Errorf("çapa: beklenmeyen dosya adı %q", name)
	}
	taken, err := time.Parse(damga, strings.TrimSuffix(strings.TrimPrefix(name, adOnEki), Ext))
	if err != nil {
		return Anchor{}, fmt.Errorf("çapa: %q adındaki zaman okunamadı", name)
	}
	if len(data) > maxBoyut {
		return Anchor{}, fmt.Errorf("çapa: %s çok büyük (%d bayt)", name, len(data))
	}
	satirlar := strings.Split(string(data), "\n")
	if len(satirlar) != 4 || satirlar[0] != surum || satirlar[3] != "" ||
		!strings.HasPrefix(satirlar[1], "seq ") || !strings.HasPrefix(satirlar[2], "hash ") {
		return Anchor{}, fmt.Errorf("çapa: %s biçimi tanınmıyor", name)
	}
	seq, err := strconv.ParseUint(strings.TrimPrefix(satirlar[1], "seq "), 10, 64)
	if err != nil || seq == 0 {
		return Anchor{}, fmt.Errorf("çapa: %s sıra numarası geçersiz", name)
	}
	ham, err := hex.DecodeString(strings.TrimPrefix(satirlar[2], "hash "))
	if err != nil || len(ham) != audit.HashSize {
		return Anchor{}, fmt.Errorf("çapa: %s hash'i geçersiz", name)
	}
	a := Anchor{Seq: seq, Taken: taken.UTC(), Name: name}
	copy(a.Hash[:], ham)
	// Kanonik biçim: başında sıfırlı sayı, büyük harfli hex gibi aynı
	// değerin başka yazımları reddedilir.
	if !bytes.Equal(Format(a.Seq, a.Hash), data) {
		return Anchor{}, fmt.Errorf("çapa: %s kanonik biçimde değil", name)
	}
	return a, nil
}

// Recompute, kayıtların hash'lerini zincirin başından SIFIRDAN hesaplar.
// Kayıtlardaki Hash ve PrevHash alanlarına GÜVENMEZ: onları denetlenen
// daemon gönderiyor. Dönen dilimde i. eleman, seq'i i+1 olan kaydın
// hash'i.
func Recompute(records []audit.Record) ([][audit.HashSize]byte, error) {
	out := make([][audit.HashSize]byte, 0, len(records))
	prev := audit.GenesisHash
	for i, r := range records {
		if r.Seq != uint64(i)+1 {
			return nil, fmt.Errorf("çapa: zincir aralıksız değil (beklenen sıra %d, gelen %d)", i+1, r.Seq)
		}
		r.PrevHash = prev
		prev = audit.ComputeHash(r)
		out = append(out, prev)
	}
	return out, nil
}

// Result, çapa denetiminin sonucu.
type Result struct {
	Checked     int      // denetlenen çapa
	Skipped     int      // since'tan eski olduğu için atlanan
	Conflicts   []string // zincirle çelişen çapalar
	MissingDays []string // pencerede çapası olmayan günler (uyarı)
	Newest      time.Time
}

// OK, en az bir çapa denetlendiyse ve hiçbiri çelişmediyse doğru. Hiç
// çapa denetlenmediyse bu bir doğrulama değil, yeşil sayılmaz.
func (r Result) OK() bool { return r.Checked > 0 && len(r.Conflicts) == 0 }

// Check, yeniden hesaplanmış hash'leri çapalarla karşılaştırır. since
// sıfır değilse ondan eski çapalar atlanır: veritabanı eski bir yedekten
// geri yüklendiyse zincir o noktada çatallanır ve eski çapalar artık
// çelişir; bunu kullanıcı açıkça kabul eder.
func Check(hashes [][audit.HashSize]byte, anchors []Anchor, since, now time.Time) Result {
	var r Result
	gunler := map[string]bool{}
	var ilk time.Time
	for _, a := range anchors {
		if !since.IsZero() && a.Taken.Before(since) {
			r.Skipped++
			continue
		}
		r.Checked++
		gunler[a.Taken.Format(time.DateOnly)] = true
		if ilk.IsZero() || a.Taken.Before(ilk) {
			ilk = a.Taken
		}
		if a.Taken.After(r.Newest) {
			r.Newest = a.Taken
		}
		switch {
		case a.Seq > uint64(len(hashes)):
			r.Conflicts = append(r.Conflicts, fmt.Sprintf(
				"%s: zincir çapadan kısa (çapa #%d, zincir %d kayıt) — kayıt silinmiş", a.Name, a.Seq, len(hashes)))
		case hashes[a.Seq-1] != a.Hash:
			r.Conflicts = append(r.Conflicts, fmt.Sprintf(
				"%s: #%d çapayla çelişiyor — geçmiş sonradan yazılmış", a.Name, a.Seq))
		}
	}
	r.MissingDays = eksikGunler(gunler, ilk, since, now)
	return r
}

// eksikGunler, son 30 günde (ilk çapadan ve since'tan önce değil, bugün
// hariç) çapası olmayan günler. Kilitli bir çapa silinemez; boşluk, çapanın
// hiç yüklenmediğini gösterir.
func eksikGunler(olan map[string]bool, ilk, since, now time.Time) []string {
	if ilk.IsZero() {
		return nil
	}
	bugun := now.UTC().Truncate(24 * time.Hour)
	bas := bugun.AddDate(0, 0, -pencereGun)
	for _, t := range []time.Time{ilk, since} {
		if g := t.UTC().Truncate(24 * time.Hour); g.After(bas) {
			bas = g
		}
	}
	var out []string
	for g := bas; g.Before(bugun); g = g.AddDate(0, 0, 1) {
		if !olan[g.Format(time.DateOnly)] {
			out = append(out, g.Format(time.DateOnly))
		}
	}
	sort.Strings(out)
	return out
}
