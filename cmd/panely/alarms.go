package main

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runAlarms, etkin arıza koşullarını listeler.
//
// ── Neden bu komut var ──────────────────────────────────────────────
//
// Teslimat (Telegram/webhook) henüz yok ve ayrı bir karar. O karar
// verilene kadar alarmların görülebileceği tek yer journal'dı — yani
// operatörün bakmayı akıl etmesi gerekiyordu. Sessiz arızaya karşı
// yazılmış bir mekanizmanın kendisinin sessiz kalması, onu anlamsız
// kılardı.
func (c *cli) runAlarms(ctx context.Context, args []string) int {
	fs := c.newFlagSet("alarms")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("kullanım: panely alarms [hedef]")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListAlarms(ctx, &panelyv1.ListAlarmsRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("alarms: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON([]any{body})
	}

	alarms := resp.GetAlarms()
	if len(alarms) == 0 {
		fmt.Fprintln(c.stdout, "etkin alarm yok")
		return exitOK
	}

	w := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CİDDİYET\tSÜREDİR\tTÜR\tHEDEF\tAYRINTI")
	for _, a := range alarms {
		since := time.Unix(a.GetSinceUnix(), 0).UTC()
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			severityLabel(a.GetSeverity()),
			humanDuration(time.Since(since)),
			a.GetKind(), a.GetTarget(), a.GetDetail())
	}
	if err := w.Flush(); err != nil {
		return c.fail(err)
	}

	if resp.GetDeliveryIsLocalOnly() {
		// K-088'in dersi: kullanıcı "alarm var" deyince haberdar
		// edileceğini varsayar. Gönderilmediğini SÖYLEMEYEN bir çıktı,
		// olmayan bir korumaya güven üretir.
		fmt.Fprintln(c.stderr,
			"UYARI: alarmlar DIŞARI GÖNDERİLMİYOR — yalnızca bu liste ve "+
				"sunucu journal'ı. Telegram/webhook teslimatı henüz yok.")
	}

	// Etkin alarm varken sıfır dönmek, `panely alarms && echo tamam`
	// gibi bir kabuk zincirinde arızayı görünmez kılardı.
	return exitError
}

// severityLabel, ciddiyeti okunur etikete çevirir.
func severityLabel(s panelyv1.AlarmSeverity) string {
	switch s {
	case panelyv1.AlarmSeverity_ALARM_SEVERITY_CRITICAL:
		return "KRİTİK"
	case panelyv1.AlarmSeverity_ALARM_SEVERITY_WARNING:
		return "uyarı"
	default:
		// Tanınmayan ciddiyeti "uyarı" saymak, gerçekten kritik bir
		// koşulu düşük göstermek olurdu.
		return "BİLİNMEYEN"
	}
}
