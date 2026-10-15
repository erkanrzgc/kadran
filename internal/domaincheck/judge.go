package domaincheck

import (
	"context"
	"net/netip"
	"slices"
)

// judge, bir kayıt kümesini sunucunun adresleriyle karşılaştırır: K-128
// kuralları + K-151'in Cloudflare ve karışık küme kuralları.
func judge(rep *Report, a, aaaa []netip.Addr, noRecords bool, serverHost string) {
	if noRecords {
		rep.raise(Stop, "%s has no A or AAAA record", rep.Domain)
		return
	}
	all := append(slices.Clone(a), aaaa...)
	if len(rep.Server) == 0 {
		if serverHost == "" {
			rep.raise(Warn, "the server's public address is unknown (local target); records: %s", join(all))
		} else {
			rep.raise(Warn, "the server's public address is unknown (%s is on a private network or did not resolve); records: %s",
				serverHost, join(all))
		}
		return
	}
	if cf := cloudflareAddrs(all); len(cf) > 0 {
		if rep.HasProvider && rep.Provider.Name == providerCloudflare {
			rep.Proxied = true
			rep.raise(Stop, "the record points at Cloudflare (%s): it is proxied (orange cloud)", join(cf))
		} else {
			rep.OnCloudflare = true
			rep.raise(Stop, "the record points at a Cloudflare-hosted page (%s), e.g. the registrar's parking page", join(cf))
			rep.Fix.Remove = append(rep.Fix.Remove, recordsOf(rep.RecordName, cf)...)
		}
		return
	}
	checkFamily(rep, "A", "IPv4", a, family(rep.Server, true))
	checkFamily(rep, "AAAA", "IPv6", aaaa, family(rep.Server, false))
}

// checkFamily, bir ailenin kayıtlarını sunucunun o ailedeki adresleriyle
// karşılaştırır. Sunucunun o ailedeki adresi bilinmiyorsa durmaz, uyarır.
// Sunucunun adresiyle BAŞKA adres birlikte dönerse durur (K-151): bugün
// ziyaretçilerin ve Let's Encrypt'in bir kısmı fazlalığa düşüyordu.
func checkFamily(rep *Report, record, fam string, records, server []netip.Addr) {
	switch {
	case len(records) == 0:
		return
	case len(server) == 0:
		rep.raise(Warn, "there is a %s record (%s) but the server's %s address is unknown; "+
			"if it points elsewhere no certificate can be issued", record, join(records), fam)
	case overlaps(records, server):
		if stray := without(records, server); len(stray) > 0 {
			rep.raise(Stop, "the %s records are %s (server) and %s: visitors and Let's Encrypt reach the other addresses part of the time",
				record, join(without(records, stray)), join(stray))
			rep.Fix.Remove = append(rep.Fix.Remove, recordsOf(rep.RecordName, stray)...)
		}
	default:
		rep.raise(Stop, "the %s record points at %s, the server is %s", record, join(records), join(server))
		rep.Fix.Remove = append(rep.Fix.Remove, recordsOf(rep.RecordName, records)...)
	}
}

// judgeAuthoritative, kararı DNS firmasının kendi ad sunucularının
// cevabına göre verir (K-151): firmada doğru olan kayıt, yerel önbellek
// eski olsa da doğrudur; firmada yanlış olan, yerel doğru görünse de
// yanlıştır.
func (c Checker) judgeAuthoritative(ctx context.Context, rep *Report, serverHost string) {
	addrs := rep.Auth.Addrs
	if len(addrs) == 0 && rep.Auth.CNAME != "" {
		rep.CNAME = rep.Auth.CNAME
		ta, _, _ := lookup(ctx, c.Resolver, "ip4", rep.CNAME)
		taaaa, _, _ := lookup(ctx, c.Resolver, "ip6", rep.CNAME)
		addrs = append(ta, taaaa...)
	}
	judge(rep, family(addrs, true), family(addrs, false), len(addrs) == 0, serverHost)
	if rep.Verdict == Stop {
		if rep.CNAME != "" {
			rep.raise(Stop, "%s is a CNAME to %s; replace it with an A record", rep.Domain, rep.CNAME)
			rep.Fix.Remove = append(rep.Fix.Remove, Record{Type: "CNAME", Name: rep.RecordName, Value: rep.CNAME})
		}
		rep.raise(Stop, "this is the answer of the DNS host's own nameservers")
		return
	}
	local := append(slices.Clone(rep.A), rep.AAAA...)
	if rep.Verdict == OK && !sameSet(local, addrs) {
		rep.raise(Warn, "your DNS host%s already answers %s; this machine still sees %s from its cache "+
			"(the new record's TTL is %d s, how long the old answer stays cached is unknown). "+
			"Let's Encrypt usually sees the new record; to be sure, wait with: kadran domain check -wait 10m",
			hostSuffix(*rep), join(addrs), orNone(local), rep.Auth.TTL)
	}
}

// fixRecords, durulan denetimde eklenecek kayıtlar: sunucunun bilinen her
// adresi için A ya da AAAA. Proxy'li Cloudflare kaydında boş: orada kayıt
// değil proxy ayarı değişiyor.
func fixRecords(rep Report) []Record {
	if rep.Verdict != Stop || rep.Proxied {
		return nil
	}
	return recordsOf(rep.RecordName, rep.Server)
}

func recordsOf(name string, addrs []netip.Addr) []Record {
	out := make([]Record, 0, len(addrs))
	for _, a := range addrs {
		typ := "A"
		if !a.Is4() {
			typ = "AAAA"
		}
		out = append(out, Record{Type: typ, Name: name, Value: a.String()})
	}
	return out
}

// without, addrs'tan drop'takileri çıkarır.
func without(addrs, drop []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range addrs {
		if !slices.Contains(drop, a) {
			out = append(out, a)
		}
	}
	return out
}

func hostSuffix(rep Report) string {
	if rep.HasProvider {
		return " (" + rep.Provider.Name + ")"
	}
	return ""
}

func orNone(addrs []netip.Addr) string {
	if len(addrs) == 0 {
		return "no record"
	}
	return join(addrs)
}
