package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/anchor"
	"github.com/erkanrzgc/kadran/internal/audit"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── Zincir çapası denetimi (K-126 C) ─────────────────────────────────
//
// `audit verify` zinciri SUNUCUDA doğruluyor; ele geçirilmiş bir daemon
// hem zincirini baştan yazıp hem de "geçerli" diyebilir. `-anchors` ile
// zincir istemcide SIFIRDAN hesaplanıyor ve R2'den indirilen kilitli
// çapalarla karşılaştırılıyor. Sunucunun gönderdiği hash alanlarına
// güvenilmiyor.

// auditPager, ListAuditRecords'un bir sayfası. Testte sahte sunucu.
type auditPager func(ctx context.Context, after uint64) ([]*kadranv1.AuditRecord, error)

// errBadAnchor, okunamayan ya da kanonik olmayan çapa: kurcalama
// göstergesi sayılır, sessizce atlanmaz.
var errBadAnchor = errors.New("bozuk çapa")

// maxAnchorFile, bir çapa dosyasından okunacak en çok bayt. Çapa ~90 bayt;
// fazlasını Parse zaten reddediyor, burada yalnızca belleği koruyoruz.
const maxAnchorFile = 4096

func (c *cli) runAnchorCheck(ctx context.Context, page auditPager, dir string, since time.Time) int {
	anchors, err := loadAnchors(dir)
	if err != nil {
		fmt.Fprintf(c.stderr, "\nÇAPA: %v\n", err)
		if errors.Is(err, errBadAnchor) {
			return exitChainInvalid
		}
		return exitError
	}
	records, err := fetchAllAudit(ctx, page)
	if err != nil {
		fmt.Fprintf(c.stderr, "\nÇAPA: %v\n", err)
		return exitError
	}
	hashes, err := anchor.Recompute(records)
	if err != nil {
		fmt.Fprintf(c.stderr, "\nÇAPA: %v\n", err)
		return exitChainInvalid
	}
	res := anchor.Check(hashes, anchors, since, time.Now())

	fmt.Fprintf(c.stdout, "\nÇapa denetimi (%s)\n", dir)
	fmt.Fprintf(c.stdout, "  zincir istemcide hesaplandı: %d kayıt\n", len(hashes))
	fmt.Fprintf(c.stdout, "  %d çapa denetlendi, %d atlandı (-anchors-since)", res.Checked, res.Skipped)
	if !res.Newest.IsZero() {
		fmt.Fprintf(c.stdout, "; en yenisi %s", res.Newest.Format(time.RFC3339))
	}
	fmt.Fprintln(c.stdout)
	if len(res.MissingDays) > 0 {
		fmt.Fprintf(c.stdout, "  ⚠ çapası olmayan günler: %s (uzak yedek o gün koşmamış olabilir)\n",
			strings.Join(res.MissingDays, ", "))
	}

	switch {
	case len(res.Conflicts) > 0:
		fmt.Fprintln(c.stderr, "\nÇAPA ÇELİŞİYOR: daemon zincirinin geçmişi çapadan SONRA değişmiş.")
		for _, k := range res.Conflicts {
			fmt.Fprintf(c.stderr, "  %s\n", k)
		}
		fmt.Fprintln(c.stderr, "Veritabanı eski bir yedekten geri yüklendiyse bu beklenir: geri\n"+
			"yüklemeden önceki çapaları -anchors-since <zaman> ile ayırın. Yoksa\n"+
			"bu bir kurcalama göstergesidir ve araştırılmalıdır.")
		return exitChainInvalid
	case res.Checked == 0:
		fmt.Fprintln(c.stderr, "\nÇAPA: hiçbir çapa denetlenmedi — bu bir doğrulama değil.")
		return exitError
	}
	fmt.Fprintln(c.stdout, "  ✓ bütün çapalar zincirle tutuyor")
	return exitOK
}

// loadAnchors, dizindeki `.capa` dosyalarını okur. Biri bozuksa hepsi
// reddedilir: daemon kullanıcısı R2 token'ını okuyabildiği için çapalar
// saldırganın yazabildiği girdi.
func loadAnchors(dir string) ([]anchor.Anchor, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("çapa dizini okunamadı: %w", err)
	}
	var out []anchor.Anchor
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), anchor.Ext) {
			continue
		}
		data, err := readLimited(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%w: %s okunamadı: %w", errBadAnchor, e.Name(), err)
		}
		a, err := anchor.Parse(e.Name(), data)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errBadAnchor, err)
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s içinde %s dosyası yok", dir, anchor.Ext)
	}
	return out, nil
}

func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxAnchorFile))
}

// fetchAllAudit, zincirin tamamını sayfa sayfa çeker. after_seq HARİÇ:
// canlı sunucuda ölçüldü, 1 verilince ilk dönen kayıt #2.
func fetchAllAudit(ctx context.Context, page auditPager) ([]audit.Record, error) {
	var (
		out   []audit.Record
		after uint64
	)
	for {
		recs, err := page(ctx, after)
		if err != nil {
			return nil, fmt.Errorf("denetim kayıtları okunamadı: %w", err)
		}
		if len(recs) == 0 {
			return out, nil
		}
		for _, p := range recs {
			// Sırayı geri saran bir sunucu döngüyü bitirmezdi.
			if p.GetSeq() <= after {
				return nil, fmt.Errorf("sunucu sırayı geri sardı (#%d, öncesi #%d)", p.GetSeq(), after)
			}
			out = append(out, auditRecordFromProto(p))
			after = p.GetSeq()
		}
	}
}

// auditRecordFromProto, sunucudan gelen kaydı iç kayda çevirir. Hash ve
// PrevHash KOPYALANMAZ: çapa denetimi onları kendisi hesaplıyor.
//
// pbconv'da DEĞİL: o paket executor'a da giriyor ve ayrıcalıklı yüzey
// bütçesine sayılıyor; bu dönüşümü yalnızca CLI kullanıyor.
func auditRecordFromProto(p *kadranv1.AuditRecord) audit.Record {
	return audit.Record{
		Seq: p.GetSeq(),
		TS:  p.GetTs().AsTime(),
		Actor: audit.Actor{
			KeyFingerprint: p.GetActor().GetSshKeyFingerprint(),
			SourceIP:       p.GetActor().GetSourceIp(),
			Label:          p.GetActor().GetLabel(),
			Origin:         p.GetActor().GetOrigin(),
		},
		Action:     p.GetAction(),
		Target:     p.GetTarget(),
		ParamsJSON: p.GetParamsJson(),
		Outcome:    outcomeFromProto(p.GetOutcome()),
		Detail:     p.GetDetail(),
		Source:     sourceFromProto(p.GetSource()),
	}
}

func outcomeFromProto(o kadranv1.AuditOutcome) audit.Outcome {
	switch o {
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS:
		return audit.OutcomeSuccess
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_FAILURE:
		return audit.OutcomeFailure
	case kadranv1.AuditOutcome_AUDIT_OUTCOME_DENIED:
		return audit.OutcomeDenied
	default:
		return 0
	}
}

func sourceFromProto(s kadranv1.AuditSource) audit.Source {
	switch s {
	case kadranv1.AuditSource_AUDIT_SOURCE_DAEMON:
		return audit.SourceDaemon
	case kadranv1.AuditSource_AUDIT_SOURCE_EXECUTOR:
		return audit.SourceExecutor
	default:
		return 0
	}
}

// parseAnchorsSince, `-anchors-since` değerini okur: tarih (UTC gün
// başı) ya da RFC3339.
func parseAnchorsSince(s string) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("-anchors-since: %q tarih (2006-01-02) ya da RFC3339 değil", s)
	}
	return t.UTC(), nil
}
