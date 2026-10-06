package main

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
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
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("usage: kadran alarms [target]")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListAlarms(ctx, &kadranv1.ListAlarmsRequest{})
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
		fmt.Fprintln(c.stdout, "no active alarms")
		return exitOK
	}

	w := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SEVERITY\tFOR\tKIND\tTARGET\tDETAIL")
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
			"WARNING: alarms are NOT SENT anywhere — only this list and the "+
				"server journal. Telegram/webhook delivery is not set up.")
	}

	// Etkin alarm varken sıfır dönmek, `kadran alarms && echo tamam`
	// gibi bir kabuk zincirinde arızayı görünmez kılardı.
	return exitError
}

// severityLabel, ciddiyeti okunur etikete çevirir.
func severityLabel(s kadranv1.AlarmSeverity) string {
	switch s {
	case kadranv1.AlarmSeverity_ALARM_SEVERITY_CRITICAL:
		return "CRITICAL"
	case kadranv1.AlarmSeverity_ALARM_SEVERITY_WARNING:
		return "warning"
	default:
		// Tanınmayan ciddiyeti "uyarı" saymak, gerçekten kritik bir
		// koşulu düşük göstermek olurdu.
		return "UNKNOWN"
	}
}
