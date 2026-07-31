package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runStatus, sunucunun ve daemon'ın durumunu gösterir.
func (c *cli) runStatus(ctx context.Context, args []string) int {
	fs := c.newFlagSet("status")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`status` en fazla bir hedef alır, %d verildi", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, ping, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	info, err := conn.RPC().GetSystemInfo(ctx, &panelyv1.GetSystemInfoRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("sistem bilgisi alınamadı: %w", err))
	}

	if *asJSON {
		return c.printStatusJSON(conn.Target().String(), ping, info)
	}
	c.printStatus(conn.Target().String(), ping, info)
	return exitOK
}

func (c *cli) printStatus(target string, ping *panelyv1.PingResponse, info *panelyv1.GetSystemInfoResponse) {
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 3, ' ', 0)
	row := func(key, value string) { fmt.Fprintf(tw, "%s\t%s\n", key, value) }

	row("Hedef", target)
	row("Daemon", fmt.Sprintf("%s (protokol %d)", info.GetDaemonVersion(), ping.GetProtocolVersion()))
	row("Çalışma süresi", humanDuration(time.Duration(info.GetDaemonUptimeSeconds())*time.Second))
	if h := info.GetHostname(); h != "" {
		row("Sunucu adı", h)
	}
	row("Çalışan kullanıcı", daemonUserCell(info.GetRunningAsUser()))
	row("Executor", executorCell(info))

	if host := info.GetHost(); host != nil {
		fmt.Fprintf(tw, "\t\n")
		if os := host.GetOs(); os != "" {
			row("İşletim sistemi", os)
		}
		if k := host.GetKernelVersion(); k != "" {
			row("Çekirdek", k)
		}
		if a := host.GetArchitecture(); a != "" {
			row("Mimari", a)
		}
		if n := host.GetCpuCount(); n > 0 {
			row("CPU", fmt.Sprintf("%d çekirdek", n))
		}
		if total := host.GetMemoryTotalBytes(); total > 0 {
			row("Bellek", fmt.Sprintf("%s kullanılabilir / %s toplam",
				humanBytes(host.GetMemoryAvailableBytes()), humanBytes(total)))
		}
		if d := host.GetDockerVersion(); d != "" {
			row("Docker", d)
		} else {
			row("Docker", "yok — executor Docker'a ulaşamıyor")
		}
	}
	_ = tw.Flush()

	if w := ping.GetCompatibilityWarning(); w != "" {
		fmt.Fprintf(c.stderr, "\nuyarı: %s\n", w)
	}
}

// daemonUserCell, daemon'ın hangi kullanıcı olarak çalıştığını gösterir.
//
// root ise bu SESSİZ GEÇİLMEZ. panelyd root çalışıyorsa executor ayrımı
// dekoratiftir ve ürünün merkezî iddiası çökmüş demektir. panelyd zaten
// root ile başlamayı reddediyor; bu satır o kontrolün yedeği ve aynı
// zamanda değişmezin ekrandaki belgesi.
func daemonUserCell(u string) string {
	if u == "root" {
		return "root  ⚠ KURULUM BOZUK — panelyd root çalışmamalı"
	}
	if u == "" {
		return "bilinmiyor"
	}
	return u
}

func executorCell(info *panelyv1.GetSystemInfoResponse) string {
	if !info.GetExecutorReachable() {
		return "ERİŞİLEMİYOR — ayrıcalıklı işlemler çalışmayacak"
	}
	if v := info.GetExecutorVersion(); v != "" {
		return "erişilebilir · " + v
	}
	return "erişilebilir"
}

func (c *cli) printStatusJSON(target string, ping *panelyv1.PingResponse, info *panelyv1.GetSystemInfoResponse) int {
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
