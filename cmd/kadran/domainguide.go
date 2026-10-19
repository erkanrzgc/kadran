package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/erkanrzgc/kadran/internal/domaincheck"
)

// domainsGuideURL, firma firma adımların yazılı olduğu rehber (K-151).
const domainsGuideURL = "https://github.com/erkanrzgc/kadran/blob/main/docs/guides/domains.md"

// writeDNSGuide, durulan denetimde kullanıcıya ne yapacağını söyler
// (K-151): DNS'in nerede olduğu, silinecek ve eklenecek kayıtlar, firmaya
// özel yer, CLI komutu ve resmi yardım sayfası.
func writeDNSGuide(w io.Writer, rep domaincheck.Report) {
	if rep.Verdict != domaincheck.Stop {
		return
	}
	switch {
	case rep.HasProvider:
		fmt.Fprintf(w, "  DNS host  %s (%s)\n", rep.Provider.Name, strings.Join(rep.NameServers, ", "))
	case len(rep.NameServers) > 0:
		fmt.Fprintf(w, "  DNS host  %s\n", strings.Join(rep.NameServers, ", "))
	case rep.ZoneChecked && len(rep.A) == 0 && len(rep.AAAA) == 0:
		fmt.Fprintln(w, "  DNS host  unknown: no nameservers found; the domain may not be registered or delegated yet")
	}
	if rep.Proxied {
		fmt.Fprintln(w, "  At Cloudflare: DNS → Records → edit the record → Proxy status: DNS only (grey cloud)")
		fmt.Fprintf(w, "  Keeping the proxy is possible: SSL/TLS mode Full (strict), then -%s.\n", skipDNSCheckFlag)
		return
	}
	writeRemove(w, rep.Fix.Remove)
	switch {
	case len(rep.Fix.Add) > 0:
		fmt.Fprintln(w, "  Set this record and delete other A, AAAA, ALIAS or CNAME records for the same name:")
		for _, r := range rep.Fix.Add {
			writeRecord(w, r)
		}
	case len(rep.Fix.Remove) == 0:
		fmt.Fprintln(w, "  Add an A record pointing the name at the server's public IPv4 address.")
	}
	if !rep.HasProvider {
		fmt.Fprintln(w, "  At your DNS host: control panel → DNS / Zone editor → add the record above")
		fmt.Fprintf(w, "  Guide: %s\n", domainsGuideURL)
		return
	}
	p := rep.Provider
	fmt.Fprintf(w, "  At %s: %s\n", p.Name, p.Panel)
	for _, r := range rep.Fix.Add {
		if cmd := p.CommandFor(rep.Zone, r); cmd != "" {
			fmt.Fprintf(w, "      %s\n", cmd)
		}
	}
	if p.Note != "" {
		fmt.Fprintf(w, "  %s\n", p.Note)
	}
	fmt.Fprintf(w, "  Guide: %s\n", p.Guide)
}

// writeRemove, adın bugün verdiği cevapları spec'teki biçimle yazar: ad
// başına bir satır, türe göre gruplu. Satırlar bölgedeki kayıtlarla birebir
// eşleşmeyebilir (ALIAS, joker), bu yüzden A girdisi "A/ALIAS" diye anılır
// ve adres cevabı varsa jokere işaret eden bir not eklenir.
func writeRemove(w io.Writer, remove []domaincheck.Record) {
	var names []string
	byName := map[string][]domaincheck.Record{}
	hasAddr := false
	for _, r := range remove {
		if _, ok := byName[r.Name]; !ok {
			names = append(names, r.Name)
		}
		byName[r.Name] = append(byName[r.Name], r)
		hasAddr = hasAddr || r.Type == "A" || r.Type == "AAAA"
	}
	for _, name := range names {
		var types []string
		values := map[string][]string{}
		for _, r := range byName[name] {
			if _, ok := values[r.Type]; !ok {
				types = append(types, r.Type)
			}
			values[r.Type] = append(values[r.Type], r.Value)
		}
		groups := make([]string, 0, len(types))
		for _, t := range types {
			label := t
			if t == "A" {
				label = "A/ALIAS"
			}
			groups = append(groups, fmt.Sprintf("%s → %s", label, strings.Join(values[t], ", ")))
		}
		fmt.Fprintf(w, "  Delete these records for %q: %s\n", name, strings.Join(groups, "; "))
	}
	if hasAddr {
		fmt.Fprintln(w, "  If there is no record by that name, a wildcard (*) record is answering for it: change or delete that one.")
	}
}

func writeRecord(w io.Writer, r domaincheck.Record) {
	fmt.Fprintf(w, "      %-5s %-12s %s\n", r.Type, r.Name, r.Value)
}
