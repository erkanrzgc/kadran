package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/anchor"
	"github.com/erkanrzgc/kadran/internal/audit"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/pbconv"
)

// ── Zincir çapası denetimi (K-126 C) ─────────────────────────────────

func testZinciri(t *testing.T, n int) []audit.Record {
	t.Helper()
	var out []audit.Record
	prev := audit.GenesisHash
	for i := 1; i <= n; i++ {
		r := audit.Seal(audit.Record{
			TS:      time.Date(2026, 10, 3, 10, 0, i, 123456789, time.UTC),
			Actor:   audit.Actor{KeyFingerprint: "SHA256:abc", SourceIP: "203.0.113.5", Label: "ci", Origin: "cli"},
			Action:  "app.deploy",
			Target:  "app/portfolio",
			Outcome: audit.OutcomeSuccess,
			Detail:  "r10",
			Source:  audit.SourceDaemon,
		}, uint64(i), prev)
		out = append(out, r)
		prev = r.Hash
	}
	return out
}

// sahteSayfa, ListAuditRecords gibi davranır: after_seq HARİÇ (canlı
// sunucuda ölçüldü), sayfa başına en fazla `boy` kayıt.
func sahteSayfa(kayitlar []*kadranv1.AuditRecord, boy int) auditPager {
	return func(_ context.Context, after uint64) ([]*kadranv1.AuditRecord, error) {
		var out []*kadranv1.AuditRecord
		for _, r := range kayitlar {
			if r.GetSeq() > after && len(out) < boy {
				out = append(out, r)
			}
		}
		return out, nil
	}
}

func capaYaz(t *testing.T, dir string, r audit.Record, taken time.Time) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, anchor.FileName(taken)), anchor.Format(r.Seq, r.Hash), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAuditRecordFromProtoKeepsEveryHashedField(t *testing.T) {
	for _, r := range testZinciri(t, 3) {
		geri := auditRecordFromProto(pbconv.AuditRecordToProto(r))
		geri.PrevHash = r.PrevHash
		if audit.ComputeHash(geri) != r.Hash {
			t.Fatalf("#%d: proto gidiş-dönüşünde hash'e giren bir alan kayboldu", r.Seq)
		}
	}
}

// Ölçülen tuzak: after_seq HARİÇ. Dahil sanılıp 1'den başlanınca ilk kayıt
// atlanıyor ve bütün zincir "uyuşmaz" çıkıyordu.
func TestFetchAllAuditStartsFromTheFirstRecord(t *testing.T) {
	zincir := testZinciri(t, 5)
	got, err := fetchAllAudit(t.Context(), sahteSayfa(pbconv.AuditRecordsToProto(zincir), 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].Seq != 1 || got[4].Seq != 5 {
		t.Fatalf("sayfalama kayıt kaçırdı: %d kayıt", len(got))
	}
}

func TestFetchAllAuditStopsIfTheServerRewinds(t *testing.T) {
	zincir := pbconv.AuditRecordsToProto(testZinciri(t, 2))
	// Sınırlı: koruma bozulursa test sonsuz döngüde belleği tüketmesin
	// (CI'da mutasyon işini öldürdü). 5 turdan sonra boş sayfa: bozuk kod
	// o zaman hatasız döner ve test kırmızı olur.
	cagri := 0
	geriSar := func(context.Context, uint64) ([]*kadranv1.AuditRecord, error) {
		cagri++
		if cagri > 5 {
			return nil, nil
		}
		return zincir, nil
	}
	if _, err := fetchAllAudit(t.Context(), geriSar); err == nil {
		t.Fatal("sırayı geri saran sunucu sonsuz döngüye soktu ya da kabul edildi")
	}
}

func TestAnchorCheckPassesOnUntouchedChain(t *testing.T) {
	zincir := testZinciri(t, 4)
	dir := t.TempDir()
	capaYaz(t, dir, zincir[3], time.Now().Add(-time.Hour))
	c, stdout, stderr := newTestCLI("")

	code := c.runAnchorCheck(t.Context(), sahteSayfa(pbconv.AuditRecordsToProto(zincir), 100), dir, time.Time{})

	if code != exitOK {
		t.Fatalf("çıkış %d, beklenen %d\n%s%s", code, exitOK, stdout, stderr)
	}
	if !strings.Contains(stdout.String(), "1 çapa") {
		t.Errorf("çıktı denetlenen çapayı söylemiyor:\n%s", stdout)
	}
}

// Asıl iddia: sunucu bir kaydın İÇERİĞİNİ değiştirip hash alanlarını eski
// (çapalı) değerlerde bırakıyor. İstemci o alanlara güvenseydi geçerdi.
func TestAnchorCheckIgnoresTheServersHashFields(t *testing.T) {
	zincir := testZinciri(t, 4)
	dir := t.TempDir()
	capaYaz(t, dir, zincir[3], time.Now().Add(-time.Hour))
	sahte := pbconv.AuditRecordsToProto(zincir)
	sahte[1].Target = "app/baska" // hash ve prev_hash alanları DEĞİŞMEDİ
	c, stdout, stderr := newTestCLI("")

	code := c.runAnchorCheck(t.Context(), sahteSayfa(sahte, 100), dir, time.Time{})

	if code != exitChainInvalid {
		t.Fatalf("içeriği değişmiş zincir geçti (çıkış %d)\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr.String(), "çelişiyor") {
		t.Errorf("çelişki açıklanmadı:\n%s", stderr)
	}
}

// Çapa dosyaları saldırganın yazabildiği girdi: bozuk dosya sessizce
// atlanmaz, kurcalama göstergesi sayılır.
func TestAnchorCheckRejectsAMalformedAnchor(t *testing.T) {
	zincir := testZinciri(t, 2)
	dir := t.TempDir()
	capaYaz(t, dir, zincir[1], time.Now().Add(-time.Hour))
	if err := os.WriteFile(filepath.Join(dir, "kadran-20261001T000000Z.capa"), []byte("kadran-capa 1\nseq 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, stderr := newTestCLI("")

	if code := c.runAnchorCheck(t.Context(), sahteSayfa(pbconv.AuditRecordsToProto(zincir), 100), dir, time.Time{}); code != exitChainInvalid {
		t.Fatalf("bozuk çapa kabul edildi (çıkış %d)\n%s", code, stderr)
	}
}

func TestAnchorCheckWithNoAnchorsIsAnError(t *testing.T) {
	c, _, stderr := newTestCLI("")
	if code := c.runAnchorCheck(t.Context(), sahteSayfa(nil, 100), t.TempDir(), time.Time{}); code != exitError {
		t.Fatalf("çapasız dizin yeşil ya da yanlış kodla döndü (%d)\n%s", code, stderr)
	}
}

func TestAuditVerifyRejectsAnchorsWithJSON(t *testing.T) {
	c, _, _ := newTestCLI("")
	if code := c.runAuditVerify(t.Context(), []string{"-json", "-anchors", t.TempDir()}); code != exitUsage {
		t.Fatalf("-json ve -anchors birlikte kabul edildi (%d)", code)
	}
}

func TestParseAnchorsSinceAcceptsDateAndRFC3339(t *testing.T) {
	for _, s := range []string{"2026-10-01", "2026-10-01T12:00:00Z"} {
		if _, err := parseAnchorsSince(s); err != nil {
			t.Errorf("%q reddedildi: %v", s, err)
		}
	}
	if _, err := parseAnchorsSince("dün"); err == nil {
		t.Error("geçersiz zaman kabul edildi")
	}
}
