package main

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runBackup, `backup` alt komutlarını dağıtır.
func (c *cli) runBackup(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`backup` bir alt komut ister: create veya list")
	}

	switch args[0] {
	case "create":
		return c.runBackupCreate(ctx, args[1:])
	case "list":
		return c.runBackupList(ctx, args[1:])
	default:
		return c.usageError(
			"bilinmeyen backup alt komutu %q — create veya list", args[0])
	}
}

// runBackupCreate, anlık bir yedek aldırır.
//
// Zamanlı yedekleme daemon'da zaten koşuyor; bu komut riskli bir
// işlemden hemen ÖNCE taze bir yedek istemek içindir.
func (c *cli) runBackupCreate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("backup create")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("kullanım: panely backup create [hedef]")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().CreateBackup(ctx, &panelyv1.CreateBackupRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("backup create: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON([]any{body})
	}

	b := resp.GetBackup()
	fmt.Fprintf(c.stdout, "yedek alındı: %s (%s)\n",
		b.GetPath(), humanBytes(uint64(b.GetBytes()))) //nolint:gosec // boyut negatif olamaz
	printVolumeScopeWarning(c, resp.GetVolumesExcluded())
	return exitOK
}

// runBackupList, diskteki yedekleri listeler.
//
// ── Neden bu komut var ──────────────────────────────────────────────
//
// Yedeklemenin çalıştığını GÖRMENİN tek yolu bu. "Zamanlayıcı koşuyor
// olmalı" varsayımı, geri yüklemeye ihtiyaç duyulan güne kadar
// sınanmaz — ve o gün öğrenmek için en kötü gündür.
func (c *cli) runBackupList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("backup list")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("kullanım: panely backup list [hedef]")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListBackups(ctx, &panelyv1.ListBackupsRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("backup list: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON([]any{body})
	}

	backups := resp.GetBackups()
	if len(backups) == 0 {
		// ⚠ Bu satır bir UYARI, bilgilendirme değil. Yedeği olmayan bir
		// sunucu, bir disk arızasında her şeyi kaybeder.
		fmt.Fprintln(c.stdout,
			"HİÇ YEDEK YOK — daemon --backup-interval ile koşuyor mu?")
		return exitOK
	}

	w := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ALINDI\tBOYUT\tYOL")
	for _, b := range backups {
		taken := time.Unix(b.GetTakenUnix(), 0).UTC()
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			taken.Format("2006-01-02 15:04:05Z"),
			humanBytes(uint64(b.GetBytes())), //nolint:gosec // boyut negatif olamaz
			b.GetPath())
	}
	if err := w.Flush(); err != nil {
		return c.fail(err)
	}

	fmt.Fprintf(c.stdout, "\n%d yedek (en fazla %d saklanır)\n",
		len(backups), resp.GetKeep())
	printVolumeScopeWarning(c, true)
	fmt.Fprintln(c.stdout,
		"geri yükleme sunucuda, daemon KAPALIYKEN:\n"+
			"  systemctl stop panelyd\n"+
			"  sudo -u panely /usr/local/lib/panely/panelyd --restore <yol>\n"+
			"  systemctl start panelyd")
	return exitOK
}

// printVolumeScopeWarning, yedeğin neyi KAPSAMADIĞINI yazar.
//
// K-088'in dersi: kullanıcı "yedek aldım" deyince her şeyin
// yedeklendiğini varsayar. Kapsam dışı olanı söylemeyen bir çıktı,
// olmayan bir korumaya güven üretir.
func printVolumeScopeWarning(c *cli, excluded bool) {
	if !excluded {
		return
	}
	fmt.Fprintln(c.stderr,
		"UYARI: yedek yalnızca kontrol düzlemi veritabanını kapsıyor "+
			"(uygulama tanımları, ortam değişkenleri, denetim zinciri). "+
			"Uygulamaların kalıcı disk verisi KAPSAM DIŞI; onun için "+
			"hacim yedeği ayrıca kurulmalı (deploy/offsite/README.md, K-111).")
}
