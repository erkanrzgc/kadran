package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── `kadran server`: kayıtlı sunucu listesi (K-162) ──────────────────
//
// Liste yalnız iş istasyonunda: hiçbir alt komut ağa çıkmaz. Bağlantıyı
// denemek için `kadran status -s <ad>`.

const serverUsage = "usage:\n" +
	"  kadran server add [-provider P] [-admin root@host] [-sudo] <name> <target>\n" +
	"  kadran server list\n" +
	"  kadran server rm <name>\n" +
	"  kadran server rename <old> <new>"

// serverStore, kayıtlı sunucu listesinin dosyası; testler geçici yol verir.
func (c *cli) serverStore() servers.Store { return c.servers }

func (c *cli) runServer(_ context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("%s", serverUsage)
	}
	switch args[0] {
	case "add":
		return c.runServerAdd(args[1:])
	case "list":
		return c.runServerList(args[1:])
	case "rm":
		return c.runServerRm(args[1:])
	case "rename":
		return c.runServerRename(args[1:])
	default:
		return c.usageError("unknown server subcommand %q — add, list, rm or rename", args[0])
	}
}

func (c *cli) runServerAdd(args []string) int {
	fs := c.newFlagSet("server add")
	provider := fs.String("provider", "", "provider label shown in the desktop app (free text)")
	admin := fs.String("admin", "", "target for install and upgrade: root@host, or user@host with -sudo")
	sudo := fs.Bool("sudo", false, "the admin user reaches root through PASSWORDLESS sudo")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 2 {
		return c.usageError("%s", serverUsage)
	}
	if *sudo && *admin == "" {
		return c.usageError("-sudo needs -admin user@host")
	}
	srv := servers.Server{Name: fs.Arg(0), Target: fs.Arg(1), Provider: strings.TrimSpace(*provider), Admin: *admin, Sudo: *sudo}
	list, err := c.serverStore().Update(func(l []servers.Server) ([]servers.Server, error) { return servers.Add(l, srv) })
	if err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "saved %s (%d servers)\n", list[len(list)-1].Name, len(list))
	return exitOK
}

func (c *cli) runServerList(args []string) int {
	if len(args) != 0 {
		return c.usageError("%s", serverUsage)
	}
	list, err := c.serverStore().Load()
	if err != nil {
		return c.fail(err)
	}
	if len(list) == 0 {
		fmt.Fprintln(c.stdout, "No saved servers. Add one: kadran server add <name> <target>")
		return exitOK
	}
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPROVIDER\tTARGET\tADMIN")
	for _, s := range list {
		admin := s.Admin
		if s.Sudo {
			admin += " (sudo)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, dashIfEmpty(s.Provider), s.Target, dashIfEmpty(admin))
	}
	_ = tw.Flush()
	return exitOK
}

func (c *cli) runServerRm(args []string) int {
	if len(args) != 1 {
		return c.usageError("%s", serverUsage)
	}
	_, err := c.serverStore().Update(func(l []servers.Server) ([]servers.Server, error) { return servers.Remove(l, args[0]) })
	if err != nil {
		return c.fail(c.withSavedNames(err))
	}
	fmt.Fprintf(c.stdout, "removed %s (the server itself and any saved sign-in are untouched)\n", args[0])
	return exitOK
}

func (c *cli) runServerRename(args []string) int {
	if len(args) != 2 {
		return c.usageError("%s", serverUsage)
	}
	_, err := c.serverStore().Update(func(l []servers.Server) ([]servers.Server, error) {
		return servers.Rename(l, args[0], args[1])
	})
	if err != nil {
		return c.fail(c.withSavedNames(err))
	}
	fmt.Fprintf(c.stdout, "renamed %s to %s\n", args[0], strings.TrimSpace(args[1]))
	return exitOK
}

// withSavedNames, "kayıt yok" hatasına kayıtlı adları ekler: yazım hatası
// hemen görülsün, ama benzer ad önerilmez ve seçilmez.
func (c *cli) withSavedNames(err error) error {
	if !errors.Is(err, servers.ErrNotFound) {
		return err
	}
	list, lerr := c.serverStore().Load()
	if lerr != nil || len(list) == 0 {
		return fmt.Errorf("%w — the list is empty; add one: kadran server add <name> <target>", err)
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return fmt.Errorf("%w — saved: %s", err, strings.Join(names, ", "))
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
