package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// runAudit, `audit` alt komutlarını dağıtır.
func (c *cli) runAudit(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`audit` needs a subcommand: list or verify")
	}

	switch args[0] {
	case "list":
		return c.runAuditList(ctx, args[1:])
	case "verify":
		return c.runAuditVerify(ctx, args[1:])
	default:
		return c.usageError("unknown audit subcommand %q — list or verify", args[0])
	}
}

// runAuditList, denetim zincirini sayfalı olarak listeler.
func (c *cli) runAuditList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("audit list")
	after := fs.Uint64("after", 0, "fetch records after this sequence number")
	limit := fs.Uint("limit", 50, "maximum number of records (at most 1000)")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`audit list` takes at most one target, got %d", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListAuditRecords(ctx, &kadranv1.ListAuditRecordsRequest{
		AfterSeq: *after,
		Limit:    uint32(*limit), //nolint:gosec // sunucu üst sınırı zaten uyguluyor
	})
	if err != nil {
		return c.fail(fmt.Errorf("could not get audit records: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	c.printAuditRecords(resp)
	return exitOK
}

func (c *cli) printAuditRecords(resp *kadranv1.ListAuditRecordsResponse) {
	records := resp.GetRecords()
	if len(records) == 0 {
		fmt.Fprintln(c.stdout, "The audit chain has no records.")
		return
	}

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEQ\tTIME\tACTOR\tACTION\tTARGET\tOUTCOME")

	for _, r := range records {
		ts := "—"
		if t := r.GetTs(); t != nil {
			// Yerel saat: operatör kendi saat diliminde okur. Kanonik
			// UTC değeri --json çıktısında bozulmadan duruyor.
			ts = t.AsTime().Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			r.GetSeq(), ts, describeActor(r.GetActor()),
			r.GetAction(), orDash(r.GetTarget()), outcomeLabel(r.GetOutcome()))
	}
	_ = tw.Flush()

	shown := records[len(records)-1].GetSeq()
	fmt.Fprintf(c.stdout, "\n%d records shown · latest sequence in the chain: %d\n",
		len(records), resp.GetLatestSeq())
	if shown < resp.GetLatestSeq() {
		fmt.Fprintf(c.stdout, "For more: kadran audit list --after %d\n", shown)
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// runAuditVerify, iki denetim zincirini de doğrular.
//
// # İki zincir neden AYRI raporlanıyor?
//
// Daemon'ın SQLite zinciri ile executor'ın dosya zinciri bilerek ayrı
// tutulur: ele geçirilmiş bir kadrand kendi yaptığı ayrıcalıklı çağrıları
// hiç kaydetmeyebilir, ama executor'ın günlüğüne DOKUNAMAZ (root'un 0700
// dizininde; K-102'ye kadar kadrand'nin dizinindeydi ve silinebiliyordu).
// İkisini tek bir "geçerli" satırında birleştirmek, modelin tamamının
// dayandığı ayrımı gizlerdi.
func (c *cli) runAuditVerify(ctx context.Context, args []string) int {
	fs := c.newFlagSet("audit verify")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	anchorsDir := fs.String("anchors", "",
		"directory of kadran-*.capa files downloaded from R2: the chain is recomputed on this machine and compared with the anchors (K-126)")
	anchorsSince := fs.String("anchors-since", "",
		"skip anchors older than this (after a database restore): 2006-01-02 or RFC3339")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`audit verify` takes at most one target, got %d", fs.NArg())
	}
	if *anchorsDir != "" && *asJSON {
		return c.usageError("-anchors and -json cannot be used together")
	}
	var since time.Time
	if *anchorsSince != "" {
		if *anchorsDir == "" {
			return c.usageError("-anchors-since only makes sense with -anchors")
		}
		t, err := parseAnchorsSince(*anchorsSince)
		if err != nil {
			return c.usageError("%v", err)
		}
		since = t
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().VerifyAuditChain(ctx, &kadranv1.VerifyAuditChainRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("could not verify the chain: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		c.writeJSON(json.RawMessage(body))
		return verifyExitCode(resp)
	}

	c.printVerifyResult(conn.Target().String(), resp)
	code := verifyExitCode(resp)
	if *anchorsDir == "" {
		return code
	}
	page := func(ctx context.Context, after uint64) ([]*kadranv1.AuditRecord, error) {
		r, err := conn.RPC().ListAuditRecords(ctx, &kadranv1.ListAuditRecordsRequest{AfterSeq: after, Limit: 1000})
		return r.GetRecords(), err
	}
	return worseExit(code, c.runAnchorCheck(ctx, page, *anchorsDir, since))
}

// worseExit, iki çıkış kodundan ağır olanı seçer: kırık zincir, erişim
// hatasından; erişim hatası, başarıdan ağır.
func worseExit(a, b int) int {
	for _, k := range []int{exitChainInvalid, exitError} {
		if a == k || b == k {
			return k
		}
	}
	return exitOK
}

func (c *cli) printVerifyResult(target string, resp *kadranv1.VerifyAuditChainResponse) {
	fmt.Fprintf(c.stdout, "Audit chain verification — %s\n\n", target)

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  daemon chain\t%s\t%d records\n",
		chainStatusLabel(resp.GetDaemonStatus()), resp.GetRecordsChecked())
	fmt.Fprintf(tw, "  executor chain\t%s\t%d records\n",
		chainStatusLabel(resp.GetExecutorStatus()), resp.GetExecutorRecordsChecked())
	_ = tw.Flush()

	fmt.Fprintln(c.stdout)
	if d := resp.GetDetail(); d != "" {
		fmt.Fprintf(c.stdout, "  daemon   : %s\n", d)
	}
	if d := resp.GetExecutorDetail(); d != "" {
		fmt.Fprintf(c.stdout, "  executor : %s\n", d)
	}

	if resp.GetDaemonStatus() == kadranv1.ChainStatus_CHAIN_STATUS_INVALID {
		fmt.Fprintf(c.stderr,
			"\nCHAIN BROKEN: first bad record #%d.\n"+
				"The audit log is append-only; changing one record "+
				"invalidates every hash after it.\n"+
				"This indicates tampering and should be investigated.\n",
			resp.GetFirstInvalidSeq())
	}
	if resp.GetExecutorStatus() == kadranv1.ChainStatus_CHAIN_STATUS_INVALID {
		fmt.Fprintln(c.stderr,
			"\nEXECUTOR CHAIN BROKEN. kadrand cannot touch this log "+
				"(it lives in a root-only 0700 directory); if it is broken, either root "+
				"access was used or the file was corrupted on disk.")
	}
}

// verifyExitCode, doğrulama sonucunu çıkış koduna çevirir.
//
// Üç durum ayrı ayrı kodlanır çünkü çağıranın tepkisi farklıdır:
// KIRIK zincir araştırma gerektirir, DOĞRULANAMADI ise yalnızca servisin
// ayakta olmadığını söyler. Cron'a konulan bir doğrulama bu ikisini
// karıştırırsa ya sahte alarm üretir ya da gerçek olanı boğar.
func verifyExitCode(resp *kadranv1.VerifyAuditChainResponse) int {
	invalid := kadranv1.ChainStatus_CHAIN_STATUS_INVALID
	if resp.GetDaemonStatus() == invalid || resp.GetExecutorStatus() == invalid {
		return exitChainInvalid
	}
	if resp.GetDaemonStatus() != kadranv1.ChainStatus_CHAIN_STATUS_VALID ||
		resp.GetExecutorStatus() != kadranv1.ChainStatus_CHAIN_STATUS_VALID {
		return exitError
	}
	return exitOK
}
