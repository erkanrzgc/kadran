package anchor

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/audit"
)

// zincir, n kayıtlık geçerli bir daemon zinciri kurar.
func zincir(t *testing.T, n int) []audit.Record {
	t.Helper()
	out := make([]audit.Record, 0, n)
	prev := audit.GenesisHash
	for i := 1; i <= n; i++ {
		r := audit.Seal(audit.Record{
			TS:      time.Date(2026, 10, 3, 12, 0, i, 0, time.UTC),
			Actor:   audit.SystemActor("scheduler"),
			Action:  "app.deploy",
			Target:  "app/portfolio",
			Outcome: audit.OutcomeSuccess,
			Source:  audit.SourceDaemon,
		}, uint64(i), prev)
		out = append(out, r)
		prev = r.Hash
	}
	return out
}

func capaOf(t *testing.T, r audit.Record, taken time.Time) Anchor {
	t.Helper()
	a, err := Parse(FileName(taken), Format(r.Seq, r.Hash))
	if err != nil {
		t.Fatalf("kendi ürettiğimiz çapa ayrıştırılamadı: %v", err)
	}
	return a
}

var simdi = time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)

func TestFormatParseRoundTrip(t *testing.T) {
	r := zincir(t, 3)[2]
	a := capaOf(t, r, simdi)
	if a.Seq != 3 || a.Hash != r.Hash || !a.Taken.Equal(simdi) {
		t.Fatalf("gidiş-dönüş bozuk: %+v", a)
	}
}

// Çapa dosyaları saldırganın yazabildiği girdi: daemon kullanıcısı R2
// token'ını okuyabiliyor (rclone.conf 640 root:kadran). Biçimden en küçük
// sapma reddedilir.
func TestParseRejectsMalformed(t *testing.T) {
	r := zincir(t, 1)[0]
	iyi := string(Format(r.Seq, r.Hash))
	ad := FileName(simdi)
	durumlar := []struct{ ad, adi, icerik string }{
		{"yanlış ad", "baska-20261003T200000Z.capa", iyi},
		{"adda yol", "../" + ad, iyi},
		{"bozuk damga", "kadran-2026-10-03.capa", iyi},
		{"sürüm farklı", ad, strings.Replace(iyi, "kadran-capa 1", "kadran-capa 2", 1)},
		{"büyük harf hash", ad, strings.Replace(iyi, hex.EncodeToString(r.Hash[:]), strings.ToUpper(hex.EncodeToString(r.Hash[:])), 1)},
		{"kısa hash", ad, iyi[:len(iyi)-3] + "\n"},
		{"seq 0", ad, strings.Replace(iyi, "seq 1\n", "seq 0\n", 1)},
		{"seq başında sıfır", ad, strings.Replace(iyi, "seq 1\n", "seq 01\n", 1)},
		{"fazla satır", ad, iyi + "not ekle\n"},
		{"sonda satır sonu yok", ad, strings.TrimSuffix(iyi, "\n")},
		{"CRLF", ad, strings.ReplaceAll(iyi, "\n", "\r\n")},
		{"boş", ad, ""},
	}
	for _, d := range durumlar {
		if _, err := Parse(d.adi, []byte(d.icerik)); err == nil {
			t.Errorf("%s: kabul edildi, reddedilmeliydi", d.ad)
		}
	}
}

// Asıl iddia: ele geçirilmiş bir daemon bir kaydın İÇERİĞİNİ değiştirip
// hash alanını eski (çapalı) değerde bırakabilir. Denetim dönen hash
// alanına güvenseydi bu geçerdi.
func TestRecomputeIgnoresTheReturnedHashField(t *testing.T) {
	kayitlar := zincir(t, 5)
	capa := capaOf(t, kayitlar[4], simdi)

	sahte := append([]audit.Record(nil), kayitlar...)
	sahte[1].Target = "app/baska" // içerik değişti, Hash/PrevHash alanları AYNI

	hashes, err := Recompute(sahte)
	if err != nil {
		t.Fatalf("Recompute: %v", err)
	}
	sonuc := Check(hashes, []Anchor{capa}, time.Time{}, simdi)
	if sonuc.OK() {
		t.Fatal("içeriği değişmiş zincir çapayı GEÇTİ: dönen hash alanına güveniliyor")
	}
}

func TestCheckPassesOnUntouchedChain(t *testing.T) {
	kayitlar := zincir(t, 5)
	hashes, err := Recompute(kayitlar)
	if err != nil {
		t.Fatal(err)
	}
	capalar := []Anchor{capaOf(t, kayitlar[1], simdi.Add(-48*time.Hour)), capaOf(t, kayitlar[4], simdi.Add(-time.Hour))}
	if s := Check(hashes, capalar, time.Time{}, simdi); !s.OK() || s.Checked != 2 {
		t.Fatalf("dokunulmamış zincir geçmedi: %+v", s)
	}
}

// Baştan yazılmış ama kendi içinde TUTARLI bir zincir: yalnızca çapa bunu
// görebilir (K-126'nın sorunu).
func TestCheckDetectsAConsistentlyRewrittenChain(t *testing.T) {
	asil := zincir(t, 4)
	capa := capaOf(t, asil[3], simdi)

	yeni := zincir(t, 4)
	yeni[2] = audit.Seal(audit.Record{TS: yeni[2].TS, Actor: yeni[2].Actor, Action: "app.delete",
		Target: "app/portfolio", Outcome: audit.OutcomeSuccess, Source: audit.SourceDaemon}, 3, yeni[1].Hash)
	yeni[3] = audit.Seal(yeni[3], 4, yeni[2].Hash)
	if _, err := audit.VerifyAll(yeni); err != nil {
		t.Fatalf("düzenek: yeniden yazılan zincir kendi içinde tutarlı olmalı: %v", err)
	}

	hashes, err := Recompute(yeni)
	if err != nil {
		t.Fatal(err)
	}
	if Check(hashes, []Anchor{capa}, time.Time{}, simdi).OK() {
		t.Fatal("yeniden yazılmış zincir çapayı geçti")
	}
}

func TestCheckDetectsTruncation(t *testing.T) {
	kayitlar := zincir(t, 5)
	capa := capaOf(t, kayitlar[4], simdi)
	hashes, err := Recompute(kayitlar[:3])
	if err != nil {
		t.Fatal(err)
	}
	s := Check(hashes, []Anchor{capa}, time.Time{}, simdi)
	if s.OK() || len(s.Conflicts) != 1 || !strings.Contains(s.Conflicts[0], "shorter") {
		t.Fatalf("kısaltılmış zincir yakalanmadı: %+v", s)
	}
}

// Çelişen TEK çapa yeter: daemon sahte çapa ekleyebiliyor, ama gerçeklerden
// biri tutmuyorsa sonuç kırmızı olmalı ("biri tutuyor" yetmez).
func TestCheckFailsIfAnyAnchorConflicts(t *testing.T) {
	kayitlar := zincir(t, 5)
	hashes, err := Recompute(kayitlar)
	if err != nil {
		t.Fatal(err)
	}
	iyi := capaOf(t, kayitlar[4], simdi)
	kotu := capaOf(t, kayitlar[2], simdi.Add(-time.Hour))
	kotu.Hash[0] ^= 0xff
	if Check(hashes, []Anchor{iyi, kotu}, time.Time{}, simdi).OK() {
		t.Fatal("çelişen çapa varken sonuç yeşil")
	}
}

// Hiç çapa denetlenmediyse bu bir doğrulama DEĞİL: yeşil sayılmaz.
func TestCheckWithNoAnchorsIsNotOK(t *testing.T) {
	hashes, err := Recompute(zincir(t, 2))
	if err != nil {
		t.Fatal(err)
	}
	if Check(hashes, nil, time.Time{}, simdi).OK() {
		t.Fatal("sıfır çapa yeşil sayıldı")
	}
}

// Veritabanı eski bir yedekten geri yüklenince zincir çatallanır; o andan
// eski çapalar artık çelişir. Kullanıcı bunu açıkça kabul eder (since).
func TestCheckSinceSkipsAnchorsFromBeforeARestore(t *testing.T) {
	asil := zincir(t, 4)
	eskiCapa := capaOf(t, asil[3], simdi.Add(-72*time.Hour))

	geriYuklenen := zincir(t, 2)
	yeni := append(geriYuklenen, audit.Seal(audit.Record{TS: simdi, Actor: audit.SystemActor("cli"),
		Action: "backup.restore", Outcome: audit.OutcomeSuccess, Source: audit.SourceDaemon}, 3, geriYuklenen[1].Hash))
	yeniCapa := capaOf(t, yeni[2], simdi.Add(-time.Hour))

	hashes, err := Recompute(yeni)
	if err != nil {
		t.Fatal(err)
	}
	if Check(hashes, []Anchor{eskiCapa, yeniCapa}, time.Time{}, simdi).OK() {
		t.Fatal("düzenek: çatal, since verilmeden çelişki olarak görünmeliydi")
	}
	s := Check(hashes, []Anchor{eskiCapa, yeniCapa}, simdi.Add(-24*time.Hour), simdi)
	if !s.OK() || s.Checked != 1 || s.Skipped != 1 {
		t.Fatalf("since eski çapayı ayırmadı: %+v", s)
	}
}

func TestRecomputeRequiresContiguousSeq(t *testing.T) {
	kayitlar := zincir(t, 3)
	if _, err := Recompute([]audit.Record{kayitlar[0], kayitlar[2]}); err == nil {
		t.Fatal("atlanmış sıra kabul edildi")
	}
	if _, err := Recompute(kayitlar[1:]); err == nil {
		t.Fatal("1'den başlamayan zincir kabul edildi")
	}
}

func TestCheckReportsMissingDays(t *testing.T) {
	kayitlar := zincir(t, 2)
	hashes, err := Recompute(kayitlar)
	if err != nil {
		t.Fatal(err)
	}
	var capalar []Anchor
	for gun := 1; gun <= 30; gun++ {
		if gun == 5 || gun == 6 {
			continue // iki gün çapa yok
		}
		capalar = append(capalar, capaOf(t, kayitlar[1], simdi.AddDate(0, 0, -gun)))
	}
	s := Check(hashes, capalar, time.Time{}, simdi)
	if !s.OK() {
		t.Fatalf("eksik gün çelişki değil, uyarı olmalı: %+v", s)
	}
	if len(s.MissingDays) != 2 || s.MissingDays[0] != "2026-09-27" || s.MissingDays[1] != "2026-09-28" {
		t.Fatalf("eksik günler yanlış: %v", s.MissingDays)
	}
}
