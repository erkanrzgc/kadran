package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runAudit, `audit` alt komutlarını dağıtır.
func (c *cli) runAudit(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`audit` bir alt komut ister: list veya verify")
	}

	switch args[0] {
	case "list":
		return c.runAuditList(ctx, args[1:])
	case "verify":
		return c.runAuditVerify(ctx, args[1:])
	default:
		return c.usageError("bilinmeyen audit alt komutu %q — list veya verify", args[0])
	}
}

// runAuditList, denetim zincirini sayfalı olarak listeler.
func (c *cli) runAuditList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("audit list")
	after := fs.Uint64("after", 0, "bu sıra numarasından sonrasını getir")
	limit := fs.Uint("limit", 50, "en fazla kaç kayıt (üst sınır 1000)")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`audit list` en fazla bir hedef alır, %d verildi", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListAuditRecords(ctx, &panelyv1.ListAuditRecordsRequest{
		AfterSeq: *after,
		Limit:    uint32(*limit), //nolint:gosec // sunucu üst sınırı zaten uyguluyor
	})
	if err != nil {
		return c.fail(fmt.Errorf("denetim kayıtları alınamadı: %w", err))
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

func (c *cli) printAuditRecords(resp *panelyv1.ListAuditRecordsResponse) {
	records := resp.GetRecords()
	if len(records) == 0 {
		fmt.Fprintln(c.stdout, "Denetim zincirinde kayıt yok.")
		return
	}

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEQ\tZAMAN\tAKTÖR\tEYLEM\tHEDEF\tSONUÇ")

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
	fmt.Fprintf(c.stdout, "\n%d kayıt gösterildi · zincirdeki son sıra: %d\n",
		len(records), resp.GetLatestSeq())
	if shown < resp.GetLatestSeq() {
		fmt.Fprintf(c.stdout, "Devamı için: panely audit list --after %d\n", shown)
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
// tutulur: ele geçirilmiş bir panelyd kendi yaptığı ayrıcalıklı çağrıları
// hiç kaydetmeyebilir, ama executor'ın günlüğüne YAZAMAZ (0640 root:panely).
// İkisini tek bir "geçerli" satırında birleştirmek, modelin tamamının
// dayandığı ayrımı gizlerdi.
func (c *cli) runAuditVerify(ctx context.Context, args []string) int {
	fs := c.newFlagSet("audit verify")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`audit verify` en fazla bir hedef alır, %d verildi", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().VerifyAuditChain(ctx, &panelyv1.VerifyAuditChainRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("zincir doğrulanamadı: %w", err))
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
	return verifyExitCode(resp)
}

func (c *cli) printVerifyResult(target string, resp *panelyv1.VerifyAuditChainResponse) {
	fmt.Fprintf(c.stdout, "Denetim zinciri doğrulaması — %s\n\n", target)

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  daemon zinciri\t%s\t%d kayıt\n",
		chainStatusLabel(resp.GetDaemonStatus()), resp.GetRecordsChecked())
	fmt.Fprintf(tw, "  executor zinciri\t%s\t%d kayıt\n",
		chainStatusLabel(resp.GetExecutorStatus()), resp.GetExecutorRecordsChecked())
	_ = tw.Flush()

	fmt.Fprintln(c.stdout)
	if d := resp.GetDetail(); d != "" {
		fmt.Fprintf(c.stdout, "  daemon   : %s\n", d)
	}
	if d := resp.GetExecutorDetail(); d != "" {
		fmt.Fprintf(c.stdout, "  executor : %s\n", d)
	}

	if resp.GetDaemonStatus() == panelyv1.ChainStatus_CHAIN_STATUS_INVALID {
		fmt.Fprintf(c.stderr,
			"\nZİNCİR KIRIK: ilk bozulan kayıt #%d.\n"+
				"Denetim günlüğü yalnızca eklemeye açıktır; bir kaydın değişmesi "+
				"kendisinden sonraki tüm hash'leri geçersiz kılar.\n"+
				"Bu bir kurcalama göstergesidir ve araştırılmalıdır.\n",
			resp.GetFirstInvalidSeq())
	}
	if resp.GetExecutorStatus() == panelyv1.ChainStatus_CHAIN_STATUS_INVALID {
		fmt.Fprintln(c.stderr,
			"\nEXECUTOR ZİNCİRİ KIRIK. panelyd bu dosyaya yazamaz "+
				"(0640 root:panely); bozulmuşsa root yetkisi kullanılmış demektir.")
	}
}

// verifyExitCode, doğrulama sonucunu çıkış koduna çevirir.
//
// Üç durum ayrı ayrı kodlanır çünkü çağıranın tepkisi farklıdır:
// KIRIK zincir araştırma gerektirir, DOĞRULANAMADI ise yalnızca servisin
// ayakta olmadığını söyler. Cron'a konulan bir doğrulama bu ikisini
// karıştırırsa ya sahte alarm üretir ya da gerçek olanı boğar.
func verifyExitCode(resp *panelyv1.VerifyAuditChainResponse) int {
	invalid := panelyv1.ChainStatus_CHAIN_STATUS_INVALID
	if resp.GetDaemonStatus() == invalid || resp.GetExecutorStatus() == invalid {
		return exitChainInvalid
	}
	if resp.GetDaemonStatus() != panelyv1.ChainStatus_CHAIN_STATUS_VALID ||
		resp.GetExecutorStatus() != panelyv1.ChainStatus_CHAIN_STATUS_VALID {
		return exitError
	}
	return exitOK
}
