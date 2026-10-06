package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// runStatus, sunucunun ve daemon'ın durumunu gösterir.
func (c *cli) runStatus(ctx context.Context, args []string) int {
	fs := c.newFlagSet("status")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`status` takes at most one target, got %d", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, ping, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	info, err := conn.RPC().GetSystemInfo(ctx, &kadranv1.GetSystemInfoRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("could not get system info: %w", err))
	}

	if *asJSON {
		return c.printStatusJSON(conn.Target().String(), ping, info)
	}
	c.printStatus(conn.Target().String(), ping, info)
	return exitOK
}

func (c *cli) printStatus(target string, ping *kadranv1.PingResponse, info *kadranv1.GetSystemInfoResponse) {
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 3, ' ', 0)
	row := func(key, value string) { fmt.Fprintf(tw, "%s\t%s\n", key, value) }

	row("Target", target)
	row("Daemon", fmt.Sprintf("%s (protocol %d)", info.GetDaemonVersion(), ping.GetProtocolVersion()))
	row("Uptime", humanDuration(time.Duration(info.GetDaemonUptimeSeconds())*time.Second))
	if h := info.GetHostname(); h != "" {
		row("Hostname", h)
	}
	row("Running as", daemonUserCell(info.GetRunningAsUser()))
	row("Executor", executorCell(info))

	if host := info.GetHost(); host != nil {
		fmt.Fprintf(tw, "\t\n")
		if os := host.GetOs(); os != "" {
			row("OS", os)
		}
		if k := host.GetKernelVersion(); k != "" {
			row("Kernel", k)
		}
		if a := host.GetArchitecture(); a != "" {
			row("Architecture", a)
		}
		if n := host.GetCpuCount(); n > 0 {
			row("CPU", fmt.Sprintf("%d cores", n))
		}
		if total := host.GetMemoryTotalBytes(); total > 0 {
			row("Memory", fmt.Sprintf("%s available / %s total",
				humanBytes(host.GetMemoryAvailableBytes()), humanBytes(total)))
		}
		row("Disk", diskCell(host))
		if d := host.GetDockerVersion(); d != "" {
			row("Docker", d)
		} else {
			row("Docker", "none — the executor cannot reach Docker")
		}
	}
	_ = tw.Flush()

	if w := ping.GetCompatibilityWarning(); w != "" {
		fmt.Fprintf(c.stderr, "\nwarning: %s\n", w)
	}
}

// daemonUserCell, daemon'ın hangi kullanıcı olarak çalıştığını gösterir.
//
// root ise bu SESSİZ GEÇİLMEZ. kadrand root çalışıyorsa executor ayrımı
// dekoratiftir ve ürünün merkezî iddiası çökmüş demektir. kadrand zaten
// root ile başlamayı reddediyor; bu satır o kontrolün yedeği ve aynı
// zamanda değişmezin ekrandaki belgesi.
func daemonUserCell(u string) string {
	if u == "root" {
		return "root  ⚠ BROKEN INSTALL — kadrand must not run as root"
	}
	if u == "" {
		return "unknown"
	}
	return u
}

func executorCell(info *kadranv1.GetSystemInfoResponse) string {
	if !info.GetExecutorReachable() {
		return "UNREACHABLE — privileged operations will not work"
	}
	if v := info.GetExecutorVersion(); v != "" {
		return "reachable · " + v
	}
	return "reachable"
}

func (c *cli) printStatusJSON(target string, ping *kadranv1.PingResponse, info *kadranv1.GetSystemInfoResponse) int {
	pingJSON, err := protoToJSON(ping)
	if err != nil {
		return c.fail(err)
	}
	infoJSON, err := protoToJSON(info)
	if err != nil {
		return c.fail(err)
	}

	payload := struct {
		Target string          `json:"target"`
		Ping   json.RawMessage `json:"ping"`
		System json.RawMessage `json:"system"`
	}{Target: target, Ping: pingJSON, System: infoJSON}

	return c.writeJSON(payload)
}

// diskCell, disk satırını üretir.
//
// ── İki ayrım taşıyor ───────────────────────────────────────────────
//
//  1. ÖLÇÜLEMEDİ ≠ BOŞ. statfs başarısız olduysa alanlar sıfır kalır.
//     Sıfırı "disk boş" diye göstermek, ölçülemeyen bir diski sağlıklı
//     gösterirdi — yani en çok bilgiye ihtiyaç duyulan anda en yanıltıcı
//     çıktıyı verirdi.
//
//  2. Mutlak sayı tek başına bilgi taşımıyor. "8 GB kullanılabilir"
//     40 GB'lık diskte rahat, 500 GB'lık diskte alarm demek. Operatörün
//     baktığı şey oran, o yüzden yüzde de yazılıyor.
func diskCell(host *kadranv1.HostInfo) string {
	total := host.GetDiskTotalBytes()
	if total == 0 {
		return "could not be measured"
	}
	avail := host.GetDiskAvailableBytes()
	pct := avail * 100 / total
	line := fmt.Sprintf("%s available / %s total (%d%% free)",
		humanBytes(avail), humanBytes(total), pct)
	if pct < diskWarnPercent {
		line += "  ⚠ NEARLY FULL"
	}
	return line
}

// diskWarnPercent, uyarı eşiğidir.
//
// %10 seçildi: tek sunuculu bir kurulumda bir derleme birkaç GB imaj
// katmanı yazabiliyor, dolayısıyla eşik "bir derlemelik pay kaldı mı"
// sorusunu yanıtlayacak kadar erken olmalı.
const diskWarnPercent = 10
