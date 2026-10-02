package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// ── findRef: GERÇEK yanıt üzerinde ───────────────────────────────────
//
// testdata/github-info-refs.bin, GitHub'dan ÖLÇÜLEN bir yanıttır
// (github.com/erkanrzgc/panely, 7 Ağustos 2026). Elle uydurulmuş bir
// pkt-line akışı, ayrıştırıcının gerçek biçimi anlayıp anlamadığını
// söylemezdi: kendi varsayımımı kendi varsayımıma karşı sınamış olurdum.

const measuredMainSHA = "e60b4d22063f2b627e60bc699bd31bb0ba572557"

func realRefs(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/github-info-refs.bin")
	if err != nil {
		t.Fatalf("ölçülmüş yanıt okunamadı: %v", err)
	}
	return body
}

func TestFindRefOnMeasuredResponse(t *testing.T) {
	sha, err := findRef(realRefs(t), "main")
	if err != nil {
		t.Fatalf("dal bulunamadı: %v", err)
	}
	if sha != measuredMainSHA {
		t.Errorf("sha = %q, ölçülen %q", sha, measuredMainSHA)
	}
}

func TestFindRefReportsMissingBranch(t *testing.T) {
	_, err := findRef(realRefs(t), "boyle-bir-dal-yok")
	if err == nil {
		t.Fatal("olmayan dal için sha döndü")
	}
	if !strings.Contains(err.Error(), "boyle-bir-dal-yok") {
		t.Errorf("hata hangi dalın aranadığını söylemiyor: %v", err)
	}
}

// TestFindRefIgnoresTheCapabilityLine, ilk ref satırındaki NUL'dan sonraki
// yetenek listesinin ref adına karışmadığını doğrular.
//
// Ölçülen yanıtta ilk satır şöyle: "<sha> HEAD\0multi_ack thin-pack …".
// NUL kırpılmasaydı, "HEAD" hiçbir zaman eşleşmez ama daha kötüsü, uzun
// yetenek listesi bir ref adıymış gibi taşınırdı.
func TestFindRefIgnoresTheCapabilityLine(t *testing.T) {
	body := realRefs(t)
	if !strings.Contains(string(body), "\x00multi_ack") {
		t.Skip("ölçülen yanıtta yetenek satırı yok — fixture değişmiş")
	}
	// "HEAD" ile başlayan satır yetenek taşıyor; ref olarak aranan
	// "refs/heads/main" yine de bulunmalı.
	if _, err := findRef(body, "main"); err != nil {
		t.Fatalf("yetenek satırı ayrıştırmayı bozdu: %v", err)
	}
}

func TestFindRefRejectsGarbage(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"onaltılık olmayan uzunluk", "zzzz# service=git-upload-pack\n"},
		{"uzunluk gövdeden büyük", "00ff# kisa\n"},
		{"uzunluk 4'ten küçük", "0002abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := findRef([]byte(tc.body), "main"); err == nil {
				t.Fatal("bozuk pkt-line kabul edildi")
			}
		})
	}
}

// TestFindRefPrefersPeeledTag, annotated etiketin GÖSTERDİĞİ commit'in
// seçildiğini doğrular.
//
// Annotated etiketin kendi nesnesi bir commit DEĞİLDİR; onu derlemeye
// göndermek "böyle bir commit yok" hatası verirdi. `^{}` sonekli satır
// dereferans edilmiş nesnedir ve derlenmesi gereken odur.
func TestFindRefPrefersPeeledTag(t *testing.T) {
	const (
		tagObj = "1111111111111111111111111111111111111111"
		commit = "2222222222222222222222222222222222222222"
	)
	body := pktLine(tagObj+" refs/tags/v1.0\n") +
		pktLine(commit+" refs/tags/v1.0^{}\n") + "0000"

	sha, err := findRef([]byte(body), "v1.0")
	if err != nil {
		t.Fatalf("etiket çözülemedi: %v", err)
	}
	if sha != commit {
		t.Errorf("sha = %q, beklenen dereferans edilmiş commit %q", sha, commit)
	}
}

// TestFindRefPrefersBranchOverTag, aynı ada sahip bir dal ve etiket
// varsa dalın kazandığını doğrular. `-branch` bayrağının adı dal diyor.
func TestFindRefPrefersBranchOverTag(t *testing.T) {
	const (
		branchSHA = "3333333333333333333333333333333333333333"
		tagSHA    = "4444444444444444444444444444444444444444"
	)
	body := pktLine(tagSHA+" refs/tags/ayni\n") +
		pktLine(branchSHA+" refs/heads/ayni\n") + "0000"

	sha, err := findRef([]byte(body), "ayni")
	if err != nil {
		t.Fatalf("çözülemedi: %v", err)
	}
	if sha != branchSHA {
		t.Errorf("sha = %q, beklenen dal %q", sha, branchSHA)
	}
}

// pktLine, bir satırı pkt-line kaydına sarar (4 haneli onaltılık uzunluk,
// kendisi dahil).
func pktLine(s string) string {
	return fmt.Sprintf("%04x%s", len(s)+4, s)
}

// ── splitRepo ────────────────────────────────────────────────────────

func TestSplitRepo(t *testing.T) {
	host, owner, name, err := splitRepo("github.com/erkanrzgc/panely")
	if err != nil {
		t.Fatalf("ayrıştırılamadı: %v", err)
	}
	if host != "github.com" || owner != "erkanrzgc" || name != "panely" {
		t.Errorf("üçlü = %q/%q/%q", host, owner, name)
	}

	// ".git" soneki kırpılır: kopyala-yapıştır alışkanlığı.
	if _, _, name, err = splitRepo("github.com/erkanrzgc/panely.git"); err != nil || name != "panely" {
		t.Errorf(".git soneki kırpılmadı: %q (%v)", name, err)
	}
}

// TestSplitRepoRejectsURLs, şemalı girdinin SESSİZCE kırpılmadığını
// doğrular.
//
// Bu tasarımda hiçbir yerde URL ALINMIYOR — bağlam URL'ini executor
// kendisi kuruyor. Şemayı sessizce atmak, o sınırı bulanık gösterir ve
// kullanıcıyı "buraya URL verebiliyorum" sanısına iterdi.
func TestSplitRepoRejectsURLs(t *testing.T) {
	cases := []string{
		"https://github.com/user/repo",
		"git://github.com/user/repo",
		"ssh://git@github.com/user/repo",
	}
	for _, in := range cases {
		if _, _, _, err := splitRepo(in); err == nil {
			t.Errorf("URL kabul edildi: %q", in)
		}
	}
}

func TestSplitRepoRejectsWrongShape(t *testing.T) {
	for _, in := range []string{"", "github.com", "github.com/user", "a/b/c/d"} {
		if _, _, _, err := splitRepo(in); err == nil {
			t.Errorf("biçimsiz değer kabul edildi: %q", in)
		}
	}
}

// ── parseSize ────────────────────────────────────────────────────────

// TestParseSizeUsesBinaryUnits, "M" ekinin MiB anlamına geldiğini
// doğrular.
//
// Ondalık yorumlamak (512 × 10⁶) limiti sessizce ~%5 küçültürdü ve bir
// bellek limitindeki %5, OOM ile çalışan konteyner arasındaki fark
// olabilir. cgroups'ta bu değer her zaman ikili.
func TestParseSizeUsesBinaryUnits(t *testing.T) {
	cases := map[string]uint64{
		"512Mi":      512 << 20,
		"512M":       512 << 20,
		"2Gi":        2 << 30,
		"2G":         2 << 30,
		"1024Ki":     1 << 20,
		"1073741824": 1 << 30,
		"4096B":      4096,
	}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %d, beklenen %d", in, got, want)
		}
	}
}

func TestParseSizeRejectsZeroAndGarbage(t *testing.T) {
	for _, in := range []string{"", "0", "0Mi", "abc", "-1", "12Xi", "1.5Gi"} {
		if v, err := parseSize(in); err == nil {
			t.Errorf("%q kabul edildi (%d) — limitsiz konteyner yoktur", in, v)
		}
	}
}

// TestParseSizeRejectsOverflow, taşmanın YAKALANDIĞINI doğrular.
//
// Denetim olmasaydı çarpım sessizce sarmalanır ve "çok büyük limit" bir
// anda "çok küçük limit" olurdu — kullanıcının istediğinin tam tersi.
func TestParseSizeRejectsOverflow(t *testing.T) {
	if v, err := parseSize("99999999999Gi"); err == nil {
		t.Errorf("taşan boyut kabul edildi: %d", v)
	}
}

func TestFormatSizeRoundTrips(t *testing.T) {
	for _, in := range []string{"512Mi", "2Gi", "4Ki"} {
		n, err := parseSize(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := formatSize(n); got != in {
			t.Errorf("formatSize(parseSize(%q)) = %q", in, got)
		}
	}
}

// ── ANAHTAR=DEĞER bayrağı ────────────────────────────────────────────

func TestStringMapRejectsDuplicates(t *testing.T) {
	m := stringMap{}
	if err := m.Set("A=1"); err != nil {
		t.Fatalf("ilk atama: %v", err)
	}
	// Sessizce üzerine yazmak, hangi değerin geçerli olduğunu belirsiz
	// bırakırdı.
	if err := m.Set("A=2"); err == nil {
		t.Error("aynı anahtar iki kez kabul edildi")
	}
	if m["A"] != "1" {
		t.Errorf("değer değişti: %q", m["A"])
	}
}

func TestStringMapRequiresEquals(t *testing.T) {
	m := stringMap{}
	for _, in := range []string{"NOEQUALS", "=deger"} {
		if err := m.Set(in); err == nil {
			t.Errorf("biçimsiz değer kabul edildi: %q", in)
		}
	}
	// Boş DEĞER geçerlidir: `-build-arg DEBUG=` bilinçli bir kullanım.
	if err := m.Set("DEBUG="); err != nil {
		t.Errorf("boş değer reddedildi: %v", err)
	}
}
