package domaincheck

import (
	"regexp"
	"strings"
)

// providerCloudflare, Cloudflare satırının adı. Cloudflare aralığındaki bir
// kaydın "turuncu bulut" mu, yoksa başka firmanın Cloudflare'de barınan
// sayfası mı olduğu buna bakılarak ayrılıyor (K-151).
const providerCloudflare = "Cloudflare"

// Provider, bir DNS barındırıcısı (K-151). NS adlarından tanınır ve
// kullanıcıya kaydı nerede ekleyeceğini söyler. Metinler İngilizce (K-141).
type Provider struct {
	// Name, kullanıcıya gösterilen ad.
	Name string
	// Suffixes, ETİKET SINIRINDA eşleşen NS son ekleri: "he.net"
	// "ns1.he.net"i tutar, "ns.the.net"i tutmaz.
	Suffixes []string
	// Pattern, son ekle anlatılamayan NS adları için tam eşleşen ifade
	// (AWS: ns-123.awsdns-45.com).
	Pattern string
	// Panel, panelde kaydın eklendiği yer; tek satır.
	Panel string
	// Command, varsa firmanın CLI komutu; yer tutucular {zone} {name}
	// {type} {value}.
	Command string
	// Note, varsa firmaya özel uyarı (park kaydı, projeye bağlı domain…).
	Note string
	// Guide, firmanın resmi yardım sayfası (https). Panel menüleri
	// değişse de kullanıcı doğru sayfaya gider.
	Guide string
}

// providers, ~50 firma (spec "Firma tablosu"). NS son ekleri 15 Ekim
// 2026'da herkese açık DNS'te çözüldü; bağlantılar Görev 2'de açıldı.
// Yeni firma: bir satır; TestEveryProviderSuffixIdentifiesItsProvider onu
// kendiliğinden sınar.
var providers = []Provider{
	// ── Domain firmaları ─────────────────────────────────────────────
	{Name: "Vercel", Suffixes: []string{"vercel-dns.com"},
		Panel:   "Dashboard → Domains → the domain → DNS Records → Add",
		Command: "vercel dns add {zone} '{name}' {type} {value}",
		Note:    "If the domain is assigned to a Vercel project, remove it there too (Project → Settings → Domains); Vercel keeps a default ALIAS for it.",
		Guide:   "https://vercel.com/docs/domains/managing-dns-records"},
	{Name: providerCloudflare, Suffixes: []string{"ns.cloudflare.com"},
		Panel: "DNS → Records → Add record; Proxy status: DNS only (grey cloud)",
		Guide: "https://developers.cloudflare.com/dns/manage-dns-records/how-to/create-dns-records/"},
	{Name: "GoDaddy", Suffixes: []string{"domaincontrol.com"},
		Panel: "My Products → the domain → DNS → Add New Record",
		Note:  "Replace the default \"Parked\" A record for this name.",
		Guide: "https://www.godaddy.com/help/add-an-a-record-19238"},
	{Name: "Namecheap", Suffixes: []string{"registrar-servers.com"},
		Panel: "Domain List → Manage → Advanced DNS → Add New Record",
		Note:  "Delete the default URL Redirect and parking records for this name.",
		Guide: "https://www.namecheap.com/support/knowledgebase/article.aspx/319/2237/how-can-i-set-up-an-a-address-record-for-my-domain/"},
	{Name: "Network Solutions", Suffixes: []string{"worldnic.com"},
		Panel: "Account Manager → My Domain Names → Manage → Advanced DNS → A records",
		Guide: "https://www.networksolutions.com/help/article/manage-dns-adns-records"},
	{Name: "Porkbun", Suffixes: []string{"porkbun.com"},
		Panel: "Domain Management → the domain → DNS → add a record",
		Note:  "Delete the default ALIAS and CNAME parking records for this name.",
		Guide: "https://kb.porkbun.com/article/68-how-to-edit-dns-records"},
	{Name: "Squarespace", Suffixes: []string{"squarespacedns.com"},
		Panel: "Domains → the domain → DNS → Custom records",
		Guide: "https://support.squarespace.com/hc/en-us/articles/360002101888"},
	{Name: "Google Cloud DNS", Suffixes: []string{"googledomains.com"},
		Panel: "Cloud DNS → the zone → Add standard",
		Guide: "https://docs.cloud.google.com/dns/docs/records"},
	{Name: "Name.com", Suffixes: []string{"name.com"},
		Panel: "My Domains → the domain → Manage DNS Records → Add Record",
		Guide: "https://www.name.com/support/articles/205188538-pointing-your-domain-to-hosting-with-a-records"},
	{Name: "Hover", Suffixes: []string{"hover.com"},
		Panel: "the domain → DNS → Add a record",
		Guide: "https://help.hover.com/hc/en-us/articles/217282457-Managing-DNS-records"},
	{Name: "Dynadot", Suffixes: []string{"dyna-ns.net"},
		Panel: "My Domains → Manage Domains → the domain → DNS Settings",
		Guide: "https://www.dynadot.com/help/question/set-DNS"},
	{Name: "NameSilo", Suffixes: []string{"dnsowl.com"},
		Panel: "Domain Manager → the domain → Manage DNS",
		Guide: "https://www.namesilo.com/support/v2/articles/domain-manager/dns-manager"},
	{Name: "Spaceship", Suffixes: []string{"spaceship.net"},
		Panel: "Domain Manager → the domain → Advanced DNS → Add record",
		Guide: "https://www.spaceship.com/knowledgebase/dns-records-types/"},
	{Name: "Epik", Suffixes: []string{"epik.com"},
		Panel: "Domains → the domain → DNS Records",
		Guide: "https://www.epik.com/support/"},
	{Name: "Gandi", Suffixes: []string{"gandi.net"},
		Panel: "Domain → DNS Records → Add record",
		Guide: "https://docs.gandi.net/en/domain_names/common_operations/dns_records.html"},
	{Name: "OVHcloud", Suffixes: []string{"ovh.net"},
		Panel: "Web Cloud → Domain names → the domain → DNS zone → Add an entry",
		Guide: "https://help.ovhcloud.com/csm/en-dns-edit-dns-zone"},
	{Name: "IONOS", Suffixes: []string{"ui-dns.com", "ui-dns.de", "ui-dns.org", "ui-dns.biz"},
		Panel: "Domains & SSL → the domain → DNS → Add record",
		Guide: "https://www.ionos.com/help/index.php?id=408"},
	{Name: "Strato", Suffixes: []string{"rzone.de"},
		Panel: "Domains → Manage domain → DNS → A record",
		Guide: "https://www.strato.de/faq/domains/wie-kann-ich-bei-strato-meine-dns-eintraege-verwalten/"},
	{Name: "one.com", Suffixes: []string{"one.com"},
		Panel: "DNS settings → DNS records → Create new record",
		Guide: "https://www.one.com/en/support/guide/manage-your-dns-settings"},
	{Name: "Wix", Suffixes: []string{"wixdns.net"},
		Panel: "Domains → the domain → Manage DNS Records → A (Host)",
		Guide: "https://support.wix.com/en/article/adding-or-updating-a-records-in-your-wix-account"},
	{Name: "Reg.ru", Suffixes: []string{"reg.ru"},
		Panel: "Domains → the domain → DNS servers and zone management → Add record",
		Guide: "https://www.reg.ru/support/dns"},
	{Name: "Alibaba Cloud", Suffixes: []string{"hichina.com", "alidns.com"},
		Panel: "Alibaba Cloud DNS → the domain → Add record",
		Guide: "https://www.alibabacloud.com/help/en/dns/"},
	{Name: "Tencent DNSPod", Suffixes: []string{"dnspod.net"},
		Panel: "DNSPod console → the domain → Add record",
		Guide: "https://docs.dnspod.com/dns/help-change-domain-ns/"},

	// ── Hosting firmaları (domain paketle alınınca NS onlarda kalıyor) ──
	{Name: "Hostinger", Suffixes: []string{"dns-parking.com", "main-hosting.eu"},
		Panel: "hPanel → Domains → the domain → DNS / Nameservers → DNS records",
		Note:  "Delete the default parking A and CNAME records for this name.",
		Guide: "https://support.hostinger.com/en/articles/1583249-how-to-manage-dns-records-at-hostinger"},
	{Name: "Bluehost", Suffixes: []string{"bluehost.com"},
		Panel: "Domains → the domain → DNS → A records → Add",
		Guide: "https://www.bluehost.com/help/article/dns-management-add-edit-or-delete-dns-entries"},
	{Name: "HostGator", Suffixes: []string{"hostgator.com"},
		Panel: "Customer Portal → Domains → Manage → DNS → Add record",
		Guide: "https://www.hostgator.com/help/article/manage-dns-records-with-hostgatorenom"},
	{Name: "DreamHost", Suffixes: []string{"dreamhost.com"},
		Panel: "Websites → Manage Websites → DNS → Add Record",
		Guide: "https://help.dreamhost.com/hc/en-us/articles/360035516812-Adding-custom-DNS-records"},
	{Name: "SiteGround", Suffixes: []string{"siteground.net"},
		Panel: "Site Tools → Domain → DNS Zone Editor",
		Guide: "https://world.siteground.com/kb/manage-dns-records/"},
	{Name: "InMotion Hosting", Suffixes: []string{"inmotionhosting.com"},
		Panel: "cPanel → Zone Editor",
		Guide: "https://www.inmotionhosting.com/support/edu/cpanel/advanced-dns-editor-cpanel/"},
	{Name: "A2 Hosting", Suffixes: []string{"a2hosting.com"},
		Panel: "cPanel → Zone Editor",
		Guide: "https://www.a2hosting.com/kb/"},
	{Name: "Namecheap Hosting", Suffixes: []string{"namecheaphosting.com"},
		Panel: "cPanel → Zone Editor",
		Guide: "https://www.namecheap.com/support/knowledgebase/article.aspx/9434/10/using-default-nameservers-vs-hosting-nameservers/"},

	// ── Bulut ve DNS servisleri ───────────────────────────────────────
	{Name: "AWS Route 53", Pattern: `^ns-\d+\.awsdns-\d{2}\.(com|net|org|co\.uk)$`,
		Panel: "Route 53 → Hosted zones → the zone → Create record",
		Guide: "https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-creating.html"},
	{Name: "Azure DNS", Suffixes: []string{"azure-dns.com", "azure-dns.net", "azure-dns.org", "azure-dns.info"},
		Panel: "DNS zones → the zone → + Record set",
		Guide: "https://learn.microsoft.com/en-us/azure/dns/dns-operations-recordsets-portal"},
	{Name: "DigitalOcean", Suffixes: []string{"digitalocean.com"},
		Panel:   "Networking → Domains → the domain → Create new record",
		Command: "doctl compute domain records create {zone} --record-type {type} --record-name {name} --record-data {value}",
		Guide:   "https://docs.digitalocean.com/products/networking/dns/how-to/manage-records/"},
	{Name: "Linode / Akamai", Suffixes: []string{"linode.com"},
		Panel: "Domains → the domain → Add an A/AAAA Record",
		Guide: "https://techdocs.akamai.com/cloud-computing/docs/manage-dns-records"},
	{Name: "Vultr", Suffixes: []string{"vultr.com"},
		Panel: "Network → DNS → the domain → Add record",
		Guide: "https://docs.vultr.com/products/network/dns"},
	{Name: "Hetzner", Suffixes: []string{"ns.hetzner.com", "ns.hetzner.de", "first-ns.de", "second-ns.de", "second-ns.com"},
		Panel: "DNS Console → the zone → Add record",
		Note:  "Older Robot nameservers (first-ns.de, second-ns.de): edit the zone in Robot → DNS.",
		Guide: "https://docs.hetzner.com/networking/dns/overview/"},
	{Name: "Netlify / NS1", Suffixes: []string{"nsone.net"},
		Panel: "Netlify: Domains → the domain → DNS records → Add new record",
		Guide: "https://docs.netlify.com/manage/domains/manage-domains/manage-dns-records/"},
	{Name: "Bunny DNS", Suffixes: []string{"bunny.net"},
		Panel: "DNS → the zone → Add record",
		Guide: "https://docs.bunny.net/docs/dns-overview"},
	{Name: "ClouDNS", Suffixes: []string{"cloudns.net"},
		Panel: "DNS Hosting → the zone → Add new record",
		Guide: "https://www.cloudns.net/wiki/article/10/"},
	{Name: "DNSimple", Suffixes: []string{"dnsimple.com", "dnsimple-edge.com", "dnsimple-edge.net", "dnsimple-edge.io", "dnsimple-edge.org"},
		Panel: "Domains → the domain → DNS → Manage → Add record",
		Guide: "https://support.dnsimple.com/articles/manage-a-record/"},
	{Name: "DNS Made Easy", Suffixes: []string{"dnsmadeeasy.com"},
		Panel: "DNS → Managed DNS → the domain → A records",
		Guide: "https://support.dnsmadeeasy.com/hc/articles/34327020373659"},
	{Name: "easyDNS", Suffixes: []string{"easydns.com", "easydns.net", "easydns.org", "easydns.info"},
		Panel: "DNS → Edit → A records",
		Guide: "https://kb.easydns.com/"},
	{Name: "Hurricane Electric", Suffixes: []string{"he.net"},
		Panel: "dns.he.net → the zone → New A",
		Guide: "https://dns.he.net/"},
	{Name: "FreeDNS (afraid.org)", Suffixes: []string{"afraid.org"},
		Panel: "Subdomains → Add",
		Guide: "https://freedns.afraid.org/faq/"},

	// ── Türkiye (adlar ASCII: CLI metni İngilizce, K-141) ────────────
	{Name: "Natro", Suffixes: []string{"natrohost.com"},
		Panel: "Domain list → the domain → DNS management → Add record",
		Guide: "https://www.natro.com/"},
	{Name: "IsimTescil", Suffixes: []string{"isimtescil.net"},
		Panel: "My domains → the domain → DNS management",
		Guide: "https://www.isimtescil.net/destek"},
	{Name: "Turhost", Suffixes: []string{"turhost.com"},
		Panel: "Domains → the domain → DNS management",
		Guide: "https://www.turhost.com/"},
	{Name: "Guzel Hosting", Suffixes: []string{"guzelhosting.com"},
		Panel: "Client area → Domains → the domain → DNS management",
		Guide: "https://www.guzel.net.tr/"},
	{Name: "Turkticaret.net", Suffixes: []string{"turkticaret.net"},
		Panel: "Domains → the domain → DNS management",
		Guide: "https://www.turkticaret.net/"},
	{Name: "Hosting.com.tr", Suffixes: []string{"hosting.com.tr"},
		Panel: "Client area → Domains → the domain → DNS management",
		Guide: "https://www.hosting.com.tr"},
	{Name: "Markum", Suffixes: []string{"markum.net"},
		Panel: "Client area → Domains → the domain → DNS management",
		Guide: "https://www.markum.net"},
}

// providerPatterns, Pattern'i olan firmaların derlenmiş ifadeleri.
var providerPatterns = compilePatterns(providers)

func compilePatterns(ps []Provider) map[string]*regexp.Regexp {
	out := make(map[string]*regexp.Regexp)
	for _, p := range ps {
		if p.Pattern != "" {
			out[p.Name] = regexp.MustCompile(p.Pattern)
		}
	}
	return out
}

// matches, temizlenmiş ns adının bu firmaya ait olup olmadığını söyler.
func (p Provider) matches(ns string) bool {
	for _, suf := range p.Suffixes {
		if ns == suf || strings.HasSuffix(ns, "."+suf) {
			return true
		}
	}
	re := providerPatterns[p.Name]
	return re != nil && re.MatchString(ns)
}

// IdentifyProvider, bölgenin ad sunucularından DNS barındırıcısını bulur.
// HEPSİ aynı firmaya ait olmalı: iki firmaya bölünmüş ya da tanınmayan
// küme tanınmaz ve kullanıcı genel tarifi görür.
func IdentifyProvider(nameservers []string) (Provider, bool) {
	clean := make([]string, 0, len(nameservers))
	for _, ns := range nameservers {
		if n := CleanName(ns); n != "" {
			clean = append(clean, n)
		}
	}
	if len(clean) == 0 {
		return Provider{}, false
	}
	for _, p := range providers {
		all := true
		for _, ns := range clean {
			if !p.matches(ns) {
				all = false
				break
			}
		}
		if all {
			return p, true
		}
	}
	return Provider{}, false
}

// CommandFor, firmanın r kaydını ekleyen CLI komutunu verir; komutu yoksa "".
func (p Provider) CommandFor(zone string, r Record) string {
	if p.Command == "" {
		return ""
	}
	return strings.NewReplacer("{zone}", zone, "{name}", r.Name, "{type}", r.Type, "{value}", r.Value).Replace(p.Command)
}

// maxNameLen, bir DNS adının en uzun hâli (RFC 1035).
const maxNameLen = 253

// CleanName, DNS cevabından gelen bir adı güvenli ve basılabilir yapar:
// küçük harf, sondaki nokta yok, yalnız [a-z0-9.-], en çok 253 bayt. DNS
// cevabı güvenilmez veridir; bu, terminale kaçış dizisi basılmasını önler.
func CleanName(s string) string {
	s = strings.TrimSuffix(strings.ToLower(s), ".")
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= maxNameLen {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
