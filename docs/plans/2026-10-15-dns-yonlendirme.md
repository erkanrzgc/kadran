# DNS yönlendirme (A projesi) — Uygulama Planı

> **Ajanlar için:** GEREKLİ ALT BECERİ: Bu planı görev görev uygulamak için superpowers:subagent-driven-development (önerilen) ya da superpowers:executing-plans kullan. Adımlar `- [ ]` onay kutusuyla izlenir.

**Amaç:** `kadran app create/update -domain` ve `kadran domain check`, domainin DNS'inin hangi firmada olduğunu, eklenecek/silinecek kaydı ve o firmada nerede yapılacağını söylesin; kaydı firmanın kendi ad sunucularına sorarak doğrulasın; `-wait` ile beklesin; `domain check` kayıt firmasını (RDAP) da göstersin.

**Mimari:** Bütün mantık `internal/domaincheck` içinde küçük dosyalara bölünür: firma tablosu (`providers.go`), bölge bulma (`zone.go`), Cloudflare aralıkları (`cloudflare.go`), yetkili DNS sorgusu (`authoritative.go`), karar kuralları (`domaincheck.go` + `judge.go`), RDAP (`rdap.go`). Test yardımcısı ayrı pakette (`internal/domaincheck/dnstest`). CLI (`cmd/kadran`) yalnız basar. Sunucu tarafı, protokol (3) ve vekil aracısı değişmez.

**Teknoloji:** Go 1.26.9; standart kütüphane + `golang.org/x/net/dns/dnsmessage` ve `golang.org/x/net/publicsuffix` (x/net v0.60.0 zaten `go.sum`'da, yalnız doğrudan bağımlılığa döner; yeni modül YOK).

**Spec:** [docs/specs/2026-10-15-dns-yonlendirme-design.md](../specs/2026-10-15-dns-yonlendirme-design.md)

## Genel kısıtlar

- Go her Bash çağrısında: `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:$PATH"`.
- Yeni modül eklenmez; `go.mod`'da yalnız `golang.org/x/net v0.60.0` `// indirect` bloğundan doğrudan bloğa geçer.
- CLI metni İngilizce, Türkçe harf YOK (`cmd/kadran/english_test.go` `internal/domaincheck`'i tarıyor). Türk firma adları ASCII yazılır: `IsimTescil`, `Guzel Hosting`. Kod yorumları Türkçe kalabilir.
- Depoya gerçek sunucu IP'si yazılmaz. Testler belge aralıklarını kullanır: `203.0.113.0/24` (sunucu), `198.51.100.0/24` (yanlış hedef), `192.0.2.0/24` (sahte ad sunucuları), `2001:db8::/32`. Cloudflare testleri Cloudflare'in yayımlanmış aralıklarını kullanır (`104.21.3.4`, `2606:4700::1`).
- Commit mesajı: `update <birkaç İngilizce kelime>`; gövde yok, Co-Authored-By satırı YOK; yazar `erkanrizgic2004@gmail.com` (repo-yerel ayar).
- Push, PR ve birleştirme kullanıcı onayı ister.
- Testler ağa ÇIKMAZ: gerçek DNS, gerçek ad sunucusu ve gerçek RDAP yalnız Görev 12'nin elle yapılan salt-okur doğrulamasında.
- Bir mutasyon koşusu sürerken başka ağır iş ya da git komutu çalıştırılmaz.
- Bash aracında heredoc içinde tek `'` kullanma: Python/çok satırlı metni önce Write ile dosyaya yaz.
- Aynı Bash komutunda `grep -n` ile `git commit` birlikte olursa kanca `--no-verify` sanıp engelliyor: ayrı çağır.

## İnceleme odağı

1. **Joker kayıtlı bölgede yeni alt ad** (`new.example.com`, Vercel'in `*` ALIAS'ı): "kayıt yok" DEĞİL; Vercel'i gösteren kayıt durdurulur, kayıt adı `new`, Vercel tarifi basılır → Görev 6, "joker kayıt" satırı.
2. **İki etiketli genel son ek** (`www.example.com.tr`, `shop.example.co.uk`): bölge yürüyüşü `com.tr`/`co.uk`'ya tırmanmaz → Görev 3, `TestFindZone`.
3. **53. portu araya giren ISS** (AA bayraksız cevap): "propagating" uyarısı verilmez, yerel kurallara düşülür → Görev 5 ve 6.
4. **NS adında büyük harf, sondaki nokta ya da kaçış dizisi**: yine tanınır, terminale temiz basılır → Görev 1, `TestCleanName` ve büyük harfli tablo testi.
5. **Yalnız IPv6'sı bilinen sunucu**: `Fix.Add` yalnız `AAAA` önerir, A önermez → Görev 6, "yalnız IPv6" satırı.

---

### Görev 1: Firma tablosu, NS eşleştirme ve ad temizleme

**Dosyalar:**
- Oluştur: `internal/domaincheck/record.go`
- Oluştur: `internal/domaincheck/providers.go`
- Test: `internal/domaincheck/providers_test.go`

**Arayüzler:**
- Üretir: `type Record struct{ Type, Name, Value string }`; `type Provider struct{ Name string; Suffixes []string; Pattern, Panel, Command, Note, Guide string }`; `func IdentifyProvider(nameservers []string) (Provider, bool)`; `func (p Provider) CommandFor(zone string, r Record) string`; `func CleanName(s string) string`; `const providerCloudflare = "Cloudflare"`; paket değişkeni `providers []Provider`.

- [ ] **Adım 1: Başarısız testi yaz** — `internal/domaincheck/providers_test.go`:

```go
package domaincheck

import (
	"strings"
	"testing"
)

// Tablodaki HER son ek kendi firmasını tanıtmalı; büyük harf ve sondaki
// nokta (DNS cevabındaki biçim) fark etmemeli. Test tablodan üretiliyor:
// yeni satır testsiz kalamaz.
func TestEveryProviderSuffixIdentifiesItsProvider(t *testing.T) {
	tried := 0
	for _, p := range providers {
		for _, suf := range p.Suffixes {
			for _, ns := range []string{"ns1." + suf, strings.ToUpper("ns2."+suf) + "."} {
				got, ok := IdentifyProvider([]string{ns})
				if !ok || got.Name != p.Name {
					t.Errorf("%s: tanınan %q (%v), beklenen %q", ns, got.Name, ok, p.Name)
				}
				tried++
			}
		}
	}
	if tried < 100 {
		t.Fatalf("yalnız %d ad denendi — tablo boşalmış olabilir", tried)
	}
}

func TestAWSPatternMatchesOnlyRoute53(t *testing.T) {
	for _, ns := range []string{"ns-1536.awsdns-00.co.uk", "ns-12.awsdns-01.com", "ns-700.awsdns-23.net", "ns-1200.awsdns-63.org"} {
		if p, ok := IdentifyProvider([]string{ns}); !ok || p.Name != "AWS Route 53" {
			t.Errorf("%s: tanınan %q (%v)", ns, p.Name, ok)
		}
	}
	for _, ns := range []string{"ns-1.awsdns-00.com.evil.example", "awsdns-00.com", "ns-1.awsdns-0.com"} {
		if p, ok := IdentifyProvider([]string{ns}); ok {
			t.Errorf("%s yanlışlıkla %q sayıldı", ns, p.Name)
		}
	}
}

func TestProviderTableIsUsable(t *testing.T) {
	if len(providers) < 50 {
		t.Fatalf("tabloda %d firma var; spec ~50 istiyor", len(providers))
	}
	names := map[string]bool{}
	for _, p := range providers {
		if p.Name == "" || p.Panel == "" {
			t.Errorf("adı ya da panel yolu boş satır: %+v", p)
		}
		if !strings.HasPrefix(p.Guide, "https://") {
			t.Errorf("%s: yardım bağlantısı https değil: %q", p.Name, p.Guide)
		}
		if len(p.Suffixes) == 0 && p.Pattern == "" {
			t.Errorf("%s: hiçbir NS'yi tanıyamaz", p.Name)
		}
		if names[p.Name] {
			t.Errorf("iki satır aynı adı taşıyor: %s", p.Name)
		}
		names[p.Name] = true
	}
}

// İki firmaya bölünmüş, benzer görünen ya da etiket ortasında eşleşen NS
// tanınmamalı: yanlış firma tarifi, hiç tarif vermemekten kötü.
func TestSplitOrLookalikeNameserversAreNotIdentified(t *testing.T) {
	for _, set := range [][]string{
		{"ns1.vercel-dns.com", "adam.ns.cloudflare.com"},
		{"ns1.vercel-dns.com.evil.example"},
		{"ns1.notvercel-dns.com"},
		{"ns.the.net"}, // he.net etiket sınırında eşleşmeli, burada eşleşmemeli
		{"ns1.example.org"},
		{},
	} {
		if p, ok := IdentifyProvider(set); ok {
			t.Errorf("%v tanındı: %s", set, p.Name)
		}
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"NS1.Vercel-DNS.com.":     "ns1.vercel-dns.com",
		"ns1.ex\x1b[31mample.com": "ns1.ex31mample.com",
		"ns1 .example.com":        "ns1.example.com",
		strings.Repeat("a", 300):  strings.Repeat("a", 253),
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

func TestCommandFor(t *testing.T) {
	v, _ := IdentifyProvider([]string{"ns1.vercel-dns.com"})
	got := v.CommandFor("example.com", Record{Type: "A", Name: "@", Value: "203.0.113.10"})
	if want := "vercel dns add example.com '@' A 203.0.113.10"; got != want {
		t.Errorf("Vercel komutu %q, beklenen %q", got, want)
	}
	cf, _ := IdentifyProvider([]string{"adam.ns.cloudflare.com"})
	if cmd := cf.CommandFor("example.com", Record{Type: "A", Name: "@", Value: "203.0.113.10"}); cmd != "" {
		t.Errorf("komutu olmayan firma komut döndürdü: %q", cmd)
	}
}
```

- [ ] **Adım 2: Testin derlenmediğini gör**

Çalıştır: `export PATH=...; go test ./internal/domaincheck/ -run 'Provider|CleanName|CommandFor|AWSPattern' -count=1`
Beklenen: `undefined: providers`, `undefined: IdentifyProvider` ile derleme hatası.

- [ ] **Adım 3: `record.go`'yu yaz**

```go
package domaincheck

// Record, panelde eklenecek ya da silinecek tek DNS kaydı (K-151).
type Record struct {
	// Type: A, AAAA ya da CNAME.
	Type string
	// Name: bölge içindeki ad; bölgenin kendisi "@".
	Name  string
	Value string
}
```

- [ ] **Adım 4: `providers.go`'yu yaz**

Bağlantılar Görev 2'de tek tek açılıp doğrulanacak; açılmayan bağlantı orada firmanın yardım merkezi köküyle değiştirilir.

```go
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
		Guide: "https://www.networksolutions.com/support/how-to-manage-advanced-dns-records/"},
	{Name: "Register.com", Suffixes: []string{"register.com"},
		Panel: "Domain Manager → DNS settings → A records",
		Guide: "https://www.register.com/help"},
	{Name: "Domain.com", Suffixes: []string{"domain.com"},
		Panel: "Domains → DNS & Nameservers → DNS Records → Add",
		Guide: "https://www.domain.com/help/article/dns-management-how-to-update-dns-records"},
	{Name: "Porkbun", Suffixes: []string{"porkbun.com"},
		Panel: "Domain Management → the domain → DNS → add a record",
		Note:  "Delete the default ALIAS and CNAME parking records for this name.",
		Guide: "https://kb.porkbun.com/article/68-how-to-edit-dns-records"},
	{Name: "Google Cloud DNS / Squarespace", Suffixes: []string{"googledomains.com"},
		Panel: "Squarespace: Domains → DNS → Custom records; Google Cloud: Cloud DNS → the zone → Add standard",
		Guide: "https://support.squarespace.com/hc/en-us/articles/360002101888"},
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
		Guide: "https://www.spaceship.com/knowledgebase/"},
	{Name: "Epik", Suffixes: []string{"epik.com"},
		Panel: "Domains → the domain → DNS Records",
		Guide: "https://www.epik.com/support/"},
	{Name: "Gandi", Suffixes: []string{"gandi.net"},
		Panel: "Domain → DNS Records → Add record",
		Guide: "https://docs.gandi.net/en/domain_names/common_operations/dns_records.html"},
	{Name: "OVHcloud", Suffixes: []string{"ovh.net", "ovh.ca"},
		Panel: "Web Cloud → Domain names → the domain → DNS zone → Add an entry",
		Guide: "https://help.ovhcloud.com/csm/en-dns-edit-dns-zone"},
	{Name: "IONOS", Suffixes: []string{"ui-dns.com", "ui-dns.de", "ui-dns.org", "ui-dns.biz"},
		Panel: "Domains & SSL → the domain → DNS → Add record",
		Guide: "https://www.ionos.com/help/domains/configuring-your-ip-address/"},
	{Name: "Strato", Suffixes: []string{"rzone.de"},
		Panel: "Domains → Manage domain → DNS → A record",
		Guide: "https://www.strato.de/faq/domains/"},
	{Name: "one.com", Suffixes: []string{"one.com"},
		Panel: "DNS settings → DNS records → Create new record",
		Guide: "https://help.one.com/hc/en-us/articles/360000799298"},
	{Name: "Wix", Suffixes: []string{"wixdns.net"},
		Panel: "Domains → the domain → Manage DNS Records → A (Host)",
		Guide: "https://support.wix.com/en/article/adding-or-updating-a-records-in-your-wix-account"},
	{Name: "Reg.ru", Suffixes: []string{"reg.ru"},
		Panel: "Domains → the domain → DNS servers and zone management → Add record",
		Guide: "https://www.reg.ru/support/dns"},
	{Name: "Alibaba Cloud", Suffixes: []string{"hichina.com"},
		Panel: "Alibaba Cloud DNS → the domain → Add record",
		Guide: "https://www.alibabacloud.com/help/en/dns/"},
	{Name: "Tencent DNSPod", Suffixes: []string{"dnspod.net", "dnspod.com"},
		Panel: "DNSPod console → the domain → Add record",
		Guide: "https://www.tencentcloud.com/document/product/302"},

	// ── Hosting firmaları (domain paketle alınınca NS onlarda kalıyor) ──
	{Name: "Hostinger", Suffixes: []string{"dns-parking.com", "hostinger.com"},
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
		Guide: "https://www.inmotionhosting.com/support/edu/cpanel/manage-dns-records/"},
	{Name: "A2 Hosting", Suffixes: []string{"a2hosting.com"},
		Panel: "cPanel → Zone Editor",
		Guide: "https://www.a2hosting.com/kb/"},
	{Name: "Namecheap Hosting", Suffixes: []string{"namecheaphosting.com"},
		Panel: "cPanel → Zone Editor",
		Guide: "https://www.namecheap.com/support/knowledgebase/article.aspx/9536/2216/"},

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
		Guide: "https://docs.hetzner.com/dns-console/dns/general/dns-overview/"},
	{Name: "Netlify / NS1", Suffixes: []string{"nsone.net"},
		Panel: "Netlify: Domains → the domain → DNS records → Add new record",
		Guide: "https://docs.netlify.com/manage/domains/manage-domains/manage-dns-records/"},
	{Name: "Bunny DNS", Suffixes: []string{"bunny.net"},
		Panel: "DNS → the zone → Add record",
		Guide: "https://docs.bunny.net/docs/dns-overview"},
	{Name: "ClouDNS", Suffixes: []string{"cloudns.net"},
		Panel: "DNS Hosting → the zone → Add new record",
		Guide: "https://www.cloudns.net/wiki/article/10/"},
	{Name: "DNSimple", Suffixes: []string{"dnsimple.com"},
		Panel: "Domains → the domain → DNS → Manage → Add record",
		Guide: "https://support.dnsimple.com/articles/manage-a-record/"},
	{Name: "DNS Made Easy", Suffixes: []string{"dnsmadeeasy.com"},
		Panel: "DNS → Managed DNS → the domain → A records",
		Guide: "https://support.dnsmadeeasy.com/"},
	{Name: "easyDNS", Suffixes: []string{"easydns.com"},
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
		Guide: "https://www.natro.com/destek"},
	{Name: "IsimTescil", Suffixes: []string{"isimtescil.net"},
		Panel: "My domains → the domain → DNS management",
		Guide: "https://www.isimtescil.net/destek"},
	{Name: "Turhost", Suffixes: []string{"turhost.com"},
		Panel: "Domains → the domain → DNS management",
		Guide: "https://www.turhost.com/destek"},
	{Name: "Guzel Hosting", Suffixes: []string{"guzelhosting.com"},
		Panel: "Client area → Domains → the domain → DNS management",
		Guide: "https://www.guzelhosting.com/destek"},
	{Name: "Turkticaret.net", Suffixes: []string{"turkticaret.net"},
		Panel: "Domains → the domain → DNS management",
		Guide: "https://www.turkticaret.net/destek"},
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
```

- [ ] **Adım 5: Testleri geçir**

Çalıştır: `go test ./internal/domaincheck/ -run 'Provider|CleanName|CommandFor|AWSPattern' -count=1 -v`
Beklenen: 6 test PASS. Ardından `go test ./cmd/kadran/ -run TestCLIMessagesAreEnglish -count=1` → PASS (tabloda Türkçe harf yok).

- [ ] **Adım 6: Commit**

```bash
git add internal/domaincheck/record.go internal/domaincheck/providers.go internal/domaincheck/providers_test.go
git commit -m "update dns provider table"
```

---

### Görev 2: Firma verisini gerçek kaynaklardan doğrula

**Dosyalar:**
- Oluştur: `scripts/check-provider-links.sh`
- Değiştir: `internal/domaincheck/providers.go` (yalnız doğrulanamayan `Guide`/`Suffixes` değerleri)

**Arayüzler:** Kod arayüzü değişmez. Çıktı: her `Guide` bağlantısı açılıyor; her son ek firmanın belgesinde müşteri varsayılanı olarak geçiyor ya da satır çıkarılıyor.

- [ ] **Adım 1: Bağlantı denetim betiğini yaz** — `scripts/check-provider-links.sh`:

```bash
#!/usr/bin/env bash
# Firma tablosundaki resmi yardım bağlantılarını (Guide) açmayı dener (K-151).
# CI'da KOŞMAZ: dış sitelere çıkıyor ve sonuç zamanla değişiyor. Bir firma
# menüsünü ya da yardım sayfasını değiştirince elle koşulur.
#
# 403/429 çoğu zaman bot korumasıdır (ör. Akamai): o bağlantı tarayıcıda
# elle açılarak doğrulanır, betik yalnız listeler.
set -uo pipefail
cd "$(dirname "$0")/.."
fail=0
while read -r url; do
    code="$(curl -sSL -o /dev/null -m 20 -A 'Mozilla/5.0 kadran-link-check' -w '%{http_code}' "$url" 2>/dev/null)"
    printf '%s  %s\n' "${code:-000}" "$url"
    case "$code" in 2??) ;; *) fail=1 ;; esac
done < <(grep -oE 'Guide: +"https://[^"]+"' internal/domaincheck/providers.go | sed -E 's/.*"(https:[^"]+)"/\1/')
exit $fail
```

- [ ] **Adım 2: Betiği koştur**

Çalıştır: `bash scripts/check-provider-links.sh | tee "$SP/provider-links.txt"` (`$SP` = scratchpad).
Beklenen: her satır `200  https://…`. 2xx olmayan her satır Adım 3'e gider.

- [ ] **Adım 3: Açılmayan bağlantıları düzelt**

2xx dönmeyen her bağlantıyı Claude'un tarayıcısında (`mcp__Claude_Browser__navigate` + `get_page_text`) aç:
- Sayfa açılıyor ve DNS kaydı eklemeyi anlatıyorsa (bot koruması), bağlantı kalır.
- Açılmıyorsa, firmanın yardım merkezinde "A record" / "DNS records" araması yapılır ve bulunan sayfa yazılır. Bulunamazsa yardım merkezinin kökü yazılır (ör. `https://www.natro.com/destek` yerine açılan kök).

- [ ] **Adım 4: Varsayılan NS'yi firmanın belgesinden doğrula**

Her satır için firmanın "nameservers" belgesinde (aynı yardım merkezi) tablodaki son ekin müşteri varsayılanı olarak geçtiğini doğrula. Özellikle spec'in işaretlediği: IONOS `ui-dns.*`, Squarespace'in yeni domainleri (`googledomains.com` mı?), Tencent `dnspod.com`, Hetzner `ns.hetzner.de`. Doğrulanamayan son ek satırdan silinir; son eki kalmayan firma tablodan çıkar. Sonuç `$SP/provider-ns-dogrulama.md`'ye firma başına bir satır yazılır (kaynak bağlantısıyla). `TestProviderTableIsUsable` en az 50 firma istiyor; çıkan firma sayısı onu 50'nin altına düşürürse testteki eşik GÜNCELLENMEZ, kullanıcıya sorulur.

- [ ] **Adım 5: Testleri koştur**

Çalıştır: `go test ./internal/domaincheck/ -count=1` → PASS; `bash scripts/check-provider-links.sh` → çıkış 0 ya da yalnız tarayıcıda doğrulanmış bot korumalı satırlar.

- [ ] **Adım 6: Commit**

```bash
git add scripts/check-provider-links.sh internal/domaincheck/providers.go
git commit -m "update dns provider links"
```

---

### Görev 3: Bölge bulma ve kayıt adı

**Dosyalar:**
- Oluştur: `internal/domaincheck/zone.go`
- Değiştir: `go.mod` (`golang.org/x/net` doğrudan bağımlılık)
- Test: `internal/domaincheck/zone_test.go`

**Arayüzler:**
- Tüketir: `CleanName` (Görev 1).
- Üretir: `type NSResolver interface{ LookupNS(ctx context.Context, name string) ([]*net.NS, error) }`; `type Zone struct{ Name string; NameServers []string }`; `func FindZone(ctx context.Context, r NSResolver, domain string) (Zone, bool)`; `func RecordName(domain, zone string) string`; `func guessRecordName(domain string) string`.

- [ ] **Adım 1: Başarısız testi yaz** — `internal/domaincheck/zone_test.go`:

```go
package domaincheck

import (
	"context"
	"net"
	"slices"
	"testing"
)

// fakeNS, ada göre NS kayıtları; tanımsız ad "bulunamadı".
type fakeNS map[string][]string

func (f fakeNS) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	hosts, ok := f[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	out := make([]*net.NS, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, &net.NS{Host: h})
	}
	return out, nil
}

func TestFindZone(t *testing.T) {
	ns := fakeNS{
		"example.com":    {"NS2.vercel-dns.com.", "ns1.vercel-dns.com."},
		"co.uk":          {"nsa.nic.uk."},
		"com.tr":         {"ns1.nic.tr."},
		"example.com.tr": {"ns1.natrohost.com."},
		"empty.example":  {},
	}
	tests := []struct {
		domain, zone, record string
		ok                   bool
	}{
		{"example.com", "example.com", "@", true},
		{"kadran.example.com", "example.com", "kadran", true},
		{"a.b.example.com", "example.com", "a.b", true},
		{"www.example.com.tr", "example.com.tr", "www", true},
		// example.co.uk'nın NS'si yok: co.uk bir kayıt otoritesi, kullanıcının
		// bölgesi DEĞİL; yürüyüş oraya tırmanmamalı.
		{"shop.example.co.uk", "", "", false},
		{"nothing.example.org", "", "", false},
	}
	for _, tc := range tests {
		z, ok := FindZone(t.Context(), ns, tc.domain)
		if ok != tc.ok || z.Name != tc.zone {
			t.Errorf("%s: bölge %q (%v), beklenen %q (%v)", tc.domain, z.Name, ok, tc.zone, tc.ok)
			continue
		}
		if ok && RecordName(tc.domain, z.Name) != tc.record {
			t.Errorf("%s: kayıt adı %q, beklenen %q", tc.domain, RecordName(tc.domain, z.Name), tc.record)
		}
	}
	z, _ := FindZone(t.Context(), ns, "example.com")
	if !slices.Equal(z.NameServers, []string{"ns1.vercel-dns.com", "ns2.vercel-dns.com"}) {
		t.Errorf("NS listesi temizlenip sıralanmadı: %v", z.NameServers)
	}
}

func TestGuessRecordName(t *testing.T) {
	for domain, want := range map[string]string{
		"example.com":        "@",
		"kadran.example.com": "kadran",
		"www.example.com.tr": "www",
		"example.co.uk":      "@",
	} {
		if got := guessRecordName(domain); got != want {
			t.Errorf("guessRecordName(%q) = %q, beklenen %q", domain, got, want)
		}
	}
}
```

- [ ] **Adım 2: Derlenmediğini gör**

Çalıştır: `go test ./internal/domaincheck/ -run 'FindZone|GuessRecordName' -count=1`
Beklenen: `undefined: FindZone`.

- [ ] **Adım 3: `zone.go`'yu yaz**

```go
package domaincheck

import (
	"context"
	"net"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NSResolver, *net.Resolver'ın NS sorgusu; testler sahtesini veriyor.
type NSResolver interface {
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
}

// Zone, adın DNS bölgesi ve ad sunucuları (temizlenmiş, sıralı).
type Zone struct {
	Name        string
	NameServers []string
}

// FindZone, adın kendisinden kayıt edilebilir alan adına (eTLD+1) kadar
// çıkar ve NS kaydı olan ilk adı döndürür. Genel son eke (co.uk, com.tr)
// ASLA tırmanmaz: orası kullanıcının bölgesi değil, kayıt otoritesi.
func FindZone(ctx context.Context, r NSResolver, domain string) (Zone, bool) {
	top, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return Zone{}, false
	}
	name := domain
	for {
		if ns := lookupNS(ctx, r, name); len(ns) > 0 {
			return Zone{Name: name, NameServers: ns}, true
		}
		if name == top {
			return Zone{}, false
		}
		_, rest, ok := strings.Cut(name, ".")
		if !ok {
			return Zone{}, false
		}
		name = rest
	}
}

func lookupNS(ctx context.Context, r NSResolver, name string) []string {
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	recs, err := r.LookupNS(ctx, name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(recs))
	for _, ns := range recs {
		if n := CleanName(ns.Host); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// RecordName, kaydın bölge içindeki adı: bölgenin kendisi "@".
func RecordName(domain, zone string) string {
	if domain == zone {
		return "@"
	}
	return strings.TrimSuffix(domain, "."+zone)
}

// guessRecordName, bölge bilinmeden kaydın adını tahmin eder: kayıt
// edilebilir alan adının (eTLD+1) kendisi "@". NS sorgusu kapalıyken (K-128
// yolu) ya da başarısızken de tarif bir ad verebilsin diye.
func guessRecordName(domain string) string {
	top, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return domain
	}
	return RecordName(domain, top)
}
```

- [ ] **Adım 4: `go.mod`'u düzelt ve testleri geçir**

Çalıştır: `go mod tidy && git diff go.mod`
Beklenen: yalnız `golang.org/x/net v0.60.0` satırı `// indirect` bloğundan ilk `require` bloğuna geçer; sürüm değişmez; `go.sum` değişmez. Başka satır değişirse DUR ve geri al (`git checkout go.mod go.sum`), sebebini bul.
Çalıştır: `go test ./internal/domaincheck/ -count=1` → PASS.

- [ ] **Adım 5: Commit**

```bash
git add go.mod go.sum internal/domaincheck/zone.go internal/domaincheck/zone_test.go
git commit -m "update dns zone lookup"
```

---

### Görev 4: Cloudflare aralıkları

**Dosyalar:**
- Oluştur: `internal/domaincheck/cloudflare.go`
- Test: `internal/domaincheck/cloudflare_test.go`

**Arayüzler:**
- Üretir: `func IsCloudflare(a netip.Addr) bool`; `func cloudflareAddrs(addrs []netip.Addr) []netip.Addr`.

- [ ] **Adım 1: Güncel aralıkları al**

Çalıştır: `curl -fsS https://www.cloudflare.com/ips-v4; echo; curl -fsS https://www.cloudflare.com/ips-v6`
Beklenen: aşağıdaki listeyle aynı. Farklıysa koda yayımlanan GÜNCEL liste yazılır ve yorumdaki tarih güncellenir.

- [ ] **Adım 2: Başarısız testi yaz** — `internal/domaincheck/cloudflare_test.go`:

```go
package domaincheck

import (
	"net/netip"
	"testing"
)

func TestIsCloudflare(t *testing.T) {
	for _, s := range []string{"104.16.0.1", "104.21.3.4", "172.67.1.1", "2606:4700::1", "::ffff:104.21.3.4"} {
		if !IsCloudflare(netip.MustParseAddr(s)) {
			t.Errorf("%s Cloudflare sayılmadı", s)
		}
	}
	for _, s := range []string{"203.0.113.10", "8.8.8.8", "2001:db8::1", "104.15.255.255", "172.72.0.1"} {
		if IsCloudflare(netip.MustParseAddr(s)) {
			t.Errorf("%s yanlışlıkla Cloudflare sayıldı", s)
		}
	}
}

func TestCloudflareAddrs(t *testing.T) {
	in := []netip.Addr{netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("104.21.3.4")}
	got := cloudflareAddrs(in)
	if len(got) != 1 || got[0].String() != "104.21.3.4" {
		t.Fatalf("cloudflareAddrs = %v", got)
	}
}
```

- [ ] **Adım 3: Derlenmediğini gör**

Çalıştır: `go test ./internal/domaincheck/ -run Cloudflare -count=1` → `undefined: IsCloudflare`.

- [ ] **Adım 4: `cloudflare.go`'yu yaz**

```go
package domaincheck

import "net/netip"

// cloudflareRanges, Cloudflare'in yayımladığı aralıklar
// (https://www.cloudflare.com/ips-v4 ve /ips-v6; 15 Ekim 2026'da alındı).
// Bir A/AAAA kaydı buradaysa kayıt ya Cloudflare'de proxy'li (turuncu bulut)
// ya da Cloudflare'de barınan bir sayfayı (ör. park sayfası) gösteriyor;
// iki durumda da Kadran sunucusu doğrulanamaz (K-151).
var cloudflareRanges = mustPrefixes(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
)

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// IsCloudflare, adresin Cloudflare'in aralığında olup olmadığını söyler.
func IsCloudflare(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range cloudflareRanges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func cloudflareAddrs(addrs []netip.Addr) []netip.Addr {
	var out []netip.Addr
	for _, a := range addrs {
		if IsCloudflare(a) {
			out = append(out, a)
		}
	}
	return out
}
```

- [ ] **Adım 5: Testleri geçir ve commit**

Çalıştır: `go test ./internal/domaincheck/ -count=1` → PASS.

```bash
git add internal/domaincheck/cloudflare.go internal/domaincheck/cloudflare_test.go
git commit -m "update cloudflare range check"
```

---

### Görev 5: Yetkili DNS sorgusu ve sahte yetkili sunucu

**Dosyalar:**
- Oluştur: `internal/domaincheck/dnstest/dnstest.go`
- Oluştur: `internal/domaincheck/authoritative.go`
- Test: `internal/domaincheck/authoritative_test.go`

**Arayüzler:**
- Tüketir: `lookup`, `Resolver` (mevcut `domaincheck.go`), `CleanName` (Görev 1).
- Üretir:
  - `dnstest.Answer struct{ AA bool; RCode dnsmessage.RCode; Addrs []string; CNAME string; TTL uint32; WrongID, Truncated, Block bool }`
  - `dnstest.Reply(query []byte, a Answer) ([]byte, error)`
  - `dnstest.Exchange(byServer map[string]Answer) func(context.Context, netip.AddrPort, []byte) ([]byte, error)` — anahtar: sunucu IP'si (port hariç).
  - `type Exchanger func(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error)`
  - `type Authoritative struct{ Answered, Agree bool; Addrs []netip.Addr; CNAME string; TTL uint32 }`
  - `func AskAuthoritative(ctx context.Context, r Resolver, ex Exchanger, nameservers []string, name string) Authoritative`
  - `func defaultExchange(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error)`
  - `const QueryTimeout = 3 * time.Second`

- [ ] **Adım 1: Sahte yetkili sunucuyu yaz** — `internal/domaincheck/dnstest/dnstest.go` (testlerin altyapısı; üretim kodu değil, ama `_test.go` olmadığı için hem `domaincheck` hem `cmd/kadran` testleri kullanabiliyor):

```go
// Package dnstest, domaincheck ve CLI testleri için sahte bir yetkili DNS
// sunucusu (K-151). Ağ yok: sorgu baytlarını okuyup cevap baytlarını kurar.
package dnstest

import (
	"context"
	"net/netip"

	"golang.org/x/net/dns/dnsmessage"
)

// Answer, bir ad sunucusunun vereceği cevap.
type Answer struct {
	// AA: yetkili cevap bayrağı. false, 53. portu araya giren bir ISS
	// önbelleğinin cevabını taklit eder.
	AA    bool
	RCode dnsmessage.RCode
	// Addrs: A ve AAAA birlikte; sorulan aileye göre süzülür.
	Addrs []string
	CNAME string
	TTL   uint32
	// WrongID: cevabın kimliği sorguyla eşleşmez (sahte cevap).
	WrongID bool
	// Truncated: TC bayrağı; istemci TCP'ye geçmeli.
	Truncated bool
	// Block: bağlam bitene kadar cevap vermez (süre bütçesi testi).
	Block bool
}

// Reply, query'ye a'yı cevap olarak kurar.
func Reply(query []byte, a Answer) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	id := h.ID
	if a.WrongID {
		id++
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: id, Response: true, Authoritative: a.AA, RCode: a.RCode, Truncated: a.Truncated,
	})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	if err := b.StartAnswers(); err != nil {
		return nil, err
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: a.TTL}
	if a.CNAME != "" {
		target, err := dnsmessage.NewName(a.CNAME + ".")
		if err != nil {
			return nil, err
		}
		if err := b.CNAMEResource(rh, dnsmessage.CNAMEResource{CNAME: target}); err != nil {
			return nil, err
		}
	}
	for _, s := range a.Addrs {
		addr := netip.MustParseAddr(s)
		switch {
		case addr.Is4() && q.Type == dnsmessage.TypeA:
			if err := b.AResource(rh, dnsmessage.AResource{A: addr.As4()}); err != nil {
				return nil, err
			}
		case addr.Is6() && q.Type == dnsmessage.TypeAAAA:
			if err := b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: addr.As16()}); err != nil {
				return nil, err
			}
		}
	}
	return b.Finish()
}

// Exchange, sunucu IP'sine (port hariç) göre cevap veren sahte bir
// domaincheck.Exchanger. Tanımsız sunucu bağlantı hatası verir.
func Exchange(byServer map[string]Answer) func(context.Context, netip.AddrPort, []byte) ([]byte, error) {
	return func(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
		a, ok := byServer[server.Addr().String()]
		if !ok {
			return nil, context.DeadlineExceeded
		}
		if a.Block {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return Reply(query, a)
	}
}
```

- [ ] **Adım 2: Başarısız testi yaz** — `internal/domaincheck/authoritative_test.go`:

```go
package domaincheck

import (
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
	"golang.org/x/net/dns/dnsmessage"
)

// İki ad sunucusu: ns1 → 192.0.2.1, ns2 → 192.0.2.2.
var twoNS = fakeResolver{
	"ip4 ns1.example-dns.net": {ips: []string{"192.0.2.1"}},
	"ip4 ns2.example-dns.net": {ips: []string{"192.0.2.2"}},
}

var nsNames = []string{"ns1.example-dns.net", "ns2.example-dns.net"}

func ask(t *testing.T, a1, a2 dnstest.Answer) Authoritative {
	t.Helper()
	ex := dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": a1, "192.0.2.2": a2})
	return AskAuthoritative(t.Context(), twoNS, ex, nsNames, "app.example.com")
}

func TestAskAuthoritative(t *testing.T) {
	ok := dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10", "2001:db8::10"}, TTL: 600}

	got := ask(t, ok, dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10", "2001:db8::10"}, TTL: 300})
	if !got.Answered || !got.Agree || len(got.Addrs) != 2 || got.TTL != 300 {
		t.Errorf("iki tutarlı yetkili cevap: %+v", got)
	}

	// AA bayraksız cevap (ISS araya girdi) yetkili sayılmaz.
	noAA := dnstest.Answer{Addrs: []string{"203.0.113.10"}, TTL: 600}
	if got := ask(t, noAA, noAA); got.Answered {
		t.Errorf("AA bayraksız cevap yetkili sayıldı: %+v", got)
	}

	// Biri AA, öbürü değil: yalnız AA olan sayılır, kendi içinde tutarlı.
	if got := ask(t, ok, noAA); !got.Answered || !got.Agree {
		t.Errorf("tek yetkili cevap: %+v", got)
	}

	// NS'ler farklı küme veriyor: Agree=false.
	other := dnstest.Answer{AA: true, Addrs: []string{"198.51.100.7"}, TTL: 600}
	if got := ask(t, ok, other); !got.Answered || got.Agree {
		t.Errorf("tutarsız NS'ler: %+v", got)
	}

	// Kimliği tutmayan cevap atılır.
	forged := ok
	forged.WrongID = true
	if got := ask(t, forged, forged); got.Answered {
		t.Errorf("kimliği tutmayan cevap kabul edildi: %+v", got)
	}

	// Yalnız CNAME (hedef bölge dışında).
	cn := dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}
	if got := ask(t, cn, cn); !got.Answered || got.CNAME != "target.example.net" || len(got.Addrs) != 0 {
		t.Errorf("CNAME cevabı: %+v", got)
	}

	// Yetkili NXDOMAIN: cevaplandı, kayıt yok.
	nx := dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}
	if got := ask(t, nx, nx); !got.Answered || len(got.Addrs) != 0 {
		t.Errorf("yetkili NXDOMAIN: %+v", got)
	}

	// SERVFAIL yetkili cevap sayılmaz.
	sf := dnstest.Answer{AA: true, RCode: dnsmessage.RCodeServerFailure}
	if got := ask(t, sf, sf); got.Answered {
		t.Errorf("SERVFAIL yetkili sayıldı: %+v", got)
	}
}

// Adı çözülemeyen ad sunucusu atlanır, diğeri sayılır.
func TestAskAuthoritativeSkipsUnresolvableNameserver(t *testing.T) {
	ex := dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"203.0.113.10"}}})
	got := AskAuthoritative(t.Context(), twoNS, ex, []string{"ns1.example-dns.net", "yok.example-dns.net"}, "app.example.com")
	if !got.Answered || !got.Agree || len(got.Addrs) != 1 {
		t.Errorf("tek çözülen NS: %+v", got)
	}
}

// Gerçek UDP/TCP: kesik UDP cevabında TCP'ye geçilmeli.
func TestDefaultExchangeFallsBackToTCPWhenTruncated(t *testing.T) {
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcpLn.Close() }()
	port := tcpLn.Addr().(*net.TCPAddr).Port
	udp, err := net.ListenPacket("udp", tcpLn.Addr().String())
	if err != nil {
		t.Fatalf("aynı portta UDP açılamadı: %v", err)
	}
	defer func() { _ = udp.Close() }()

	full := dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10"}, TTL: 60}
	go func() { // UDP: kesik cevap
		buf := make([]byte, 512)
		n, addr, err := udp.ReadFrom(buf)
		if err != nil {
			return
		}
		resp, _ := dnstest.Reply(buf[:n], dnstest.Answer{AA: true, Truncated: true})
		_, _ = udp.WriteTo(resp, addr)
	}()
	go func() { // TCP: tam cevap
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var n [2]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return
		}
		q := make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err := io.ReadFull(conn, q); err != nil {
			return
		}
		resp, _ := dnstest.Reply(q, full)
		out := binary.BigEndian.AppendUint16(nil, uint16(len(resp)))
		_, _ = conn.Write(append(out, resp...))
	}()

	query, id, q, err := newQuery("app.example.com", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	server := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
	resp, err := defaultExchange(t.Context(), server, query)
	if err != nil {
		t.Fatalf("defaultExchange: %v", err)
	}
	a, ok := parseAnswer(resp, id, q)
	if !ok || len(a.addrs) != 1 || a.addrs[0].String() != "203.0.113.10" {
		t.Fatalf("TCP'ye geçilmedi ya da cevap okunamadı: %+v (%v)", a, ok)
	}
}
```

- [ ] **Adım 3: Derlenmediğini gör**

Çalıştır: `go test ./internal/domaincheck/ -run 'AskAuthoritative|DefaultExchange' -count=1` → `undefined: AskAuthoritative`.

- [ ] **Adım 4: `authoritative.go`'yu yaz**

```go
package domaincheck

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// QueryTimeout, tek bir yetkili sorgunun süre sınırı.
const QueryTimeout = 3 * time.Second

const dnsPort = 53

// Exchanger, bir DNS sorgusunu sunucuya gönderip ham cevabı döndürür.
// Testler sahtesini veriyor (dnstest.Exchange).
type Exchanger func(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error)

// Authoritative, bölgenin ad sunucularının cevabı (K-151).
type Authoritative struct {
	// Answered: en az bir ad sunucusu AA bayraklı, geçerli bir cevap verdi.
	// Bayraksız cevap 53. portu araya giren bir ISS önbelleğinden gelmiş
	// olabilir; sayılmaz.
	Answered bool
	// Agree: cevap veren bütün ad sunucuları aynı kümeyi verdi.
	Agree bool
	Addrs []netip.Addr
	// CNAME: cevap yalnız CNAME ise hedefi (bölge dışı hedefte adres gelmez).
	CNAME string
	// TTL: cevaplardaki en küçük TTL (saniye); kayıt yoksa 0.
	TTL uint32
}

type nsAnswer struct {
	addrs []netip.Addr
	cname string
	ttl   uint32
}

// AskAuthoritative, bölgenin HER ad sunucusuna name'in A ve AAAA kaydını
// sorar. Sorgular paralel; her biri QueryTimeout ile sınırlı ve hepsi
// ctx'in süresine tabi. Hiçbiri cevap vermezse Answered=false döner ve
// çağıran yerel çözümleyiciye düşer.
func AskAuthoritative(ctx context.Context, r Resolver, ex Exchanger, nameservers []string, name string) Authoritative {
	if ex == nil {
		ex = defaultExchange
	}
	answers := make([]*nsAnswer, len(nameservers))
	var wg sync.WaitGroup
	for i, ns := range nameservers {
		wg.Go(func() {
			if a, ok := askOne(ctx, r, ex, ns, name); ok {
				answers[i] = &a
			}
		})
	}
	wg.Wait()

	var got []nsAnswer
	for _, a := range answers {
		if a != nil {
			got = append(got, *a)
		}
	}
	if len(got) == 0 {
		return Authoritative{}
	}
	out := Authoritative{Answered: true, Agree: true, Addrs: got[0].addrs, CNAME: got[0].cname, TTL: got[0].ttl}
	for _, a := range got[1:] {
		if !sameAnswer(a, got[0]) {
			out.Agree = false
		}
		out.TTL = minTTL(out.TTL, a.ttl)
	}
	return out
}

// askOne, tek bir ad sunucusuna A ve AAAA sorar; ikisi de geçerli
// cevaplanmalı.
func askOne(ctx context.Context, r Resolver, ex Exchanger, ns, name string) (nsAnswer, bool) {
	server, ok := nsAddress(ctx, r, ns)
	if !ok {
		return nsAnswer{}, false
	}
	var all nsAnswer
	for _, qt := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		a, ok := queryOne(ctx, ex, server, name, qt)
		if !ok {
			return nsAnswer{}, false
		}
		all.addrs = append(all.addrs, a.addrs...)
		if a.cname != "" {
			all.cname = a.cname
		}
		all.ttl = minTTL(all.ttl, a.ttl)
	}
	return all, true
}

// nsAddress, ad sunucusunun adresini bulur. IPv4 önce: yalnız IPv4'lü bir
// ağda IPv6 adresinde beklenmesin.
func nsAddress(ctx context.Context, r Resolver, ns string) (netip.AddrPort, bool) {
	for _, network := range []string{"ip4", "ip6"} {
		if addrs, _, _ := lookup(ctx, r, network, ns); len(addrs) > 0 {
			return netip.AddrPortFrom(addrs[0], dnsPort), true
		}
	}
	return netip.AddrPort{}, false
}

func queryOne(ctx context.Context, ex Exchanger, server netip.AddrPort, name string, qt dnsmessage.Type) (nsAnswer, bool) {
	query, id, q, err := newQuery(name, qt)
	if err != nil {
		return nsAnswer{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	resp, err := ex(ctx, server, query)
	if err != nil {
		return nsAnswer{}, false
	}
	return parseAnswer(resp, id, q)
}

// newQuery, rastgele kimlikli, özyinelemesiz (RD=0) tek soruluk bir sorgu
// kurar: yetkili sunucuya kendi bölgesi soruluyor.
func newQuery(name string, qt dnsmessage.Type) ([]byte, uint16, dnsmessage.Question, error) {
	qname, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return nil, 0, dnsmessage.Question{}, err
	}
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, 0, dnsmessage.Question{}, err
	}
	id := binary.BigEndian.Uint16(b[:])
	q := dnsmessage.Question{Name: qname, Type: qt, Class: dnsmessage.ClassINET}
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: id}, Questions: []dnsmessage.Question{q}}
	packed, err := msg.Pack()
	return packed, id, q, err
}

// parseAnswer, cevabı doğrular ve okur. Atılan cevaplar: yanıt değil,
// kimlik ya da soru tutmuyor, AA yok, RCODE başarı/NXDOMAIN dışı.
func parseAnswer(resp []byte, id uint16, q dnsmessage.Question) (nsAnswer, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil || !h.Response || h.ID != id || !h.Authoritative {
		return nsAnswer{}, false
	}
	if h.RCode != dnsmessage.RCodeSuccess && h.RCode != dnsmessage.RCodeNameError {
		return nsAnswer{}, false
	}
	got, err := p.Question()
	if err != nil || got.Type != q.Type || !strings.EqualFold(got.Name.String(), q.Name.String()) {
		return nsAnswer{}, false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nsAnswer{}, false
	}
	var a nsAnswer
	for {
		rh, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return a, true
		}
		if err != nil {
			return nsAnswer{}, false
		}
		a.ttl = minTTL(a.ttl, rh.TTL)
		switch rh.Type {
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.addrs = append(a.addrs, netip.AddrFrom4(r.A))
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.addrs = append(a.addrs, netip.AddrFrom16(r.AAAA).Unmap())
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.cname = CleanName(r.CNAME.String())
		default:
			if err := p.SkipAnswer(); err != nil {
				return nsAnswer{}, false
			}
		}
	}
}

// minTTL, sıfırı "henüz yok" sayan en küçük TTL.
func minTTL(a, b uint32) uint32 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	default:
		return min(a, b)
	}
}

func sameAnswer(a, b nsAnswer) bool {
	return a.cname == b.cname && sameSet(a.addrs, b.addrs)
}

// sameSet, iki adres kümesinin (sıra ve tekrar önemsiz) eşit olduğunu söyler.
func sameSet(a, b []netip.Addr) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.SortFunc(x, netip.Addr.Compare)
	slices.SortFunc(y, netip.Addr.Compare)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// defaultExchange, gerçek ağ: UDP; TC bayraklı cevapta TCP.
func defaultExchange(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
	resp, err := exchangeOver(ctx, "udp", server, query)
	if err != nil {
		return nil, err
	}
	var p dnsmessage.Parser
	if h, err := p.Start(resp); err == nil && h.Truncated {
		return exchangeOver(ctx, "tcp", server, query)
	}
	return resp, nil
}

// maxDNSMessage, UDP ve TCP'de okunacak en büyük DNS mesajı.
const maxDNSMessage = 65535

func exchangeOver(ctx context.Context, network string, server netip.AddrPort, query []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, server.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if network == "tcp" {
		msg := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
		if _, err := conn.Write(append(msg, query...)); err != nil {
			return nil, err
		}
		var n [2]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return nil, err
		}
		buf := make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxDNSMessage)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}
```

`sync.WaitGroup.Go` Go 1.25'te geldi; go.mod 1.26.9 olduğu için kullanılabilir.

- [ ] **Adım 5: Testleri geçir**

Çalıştır: `go test ./internal/domaincheck/... -count=1 -race`
Beklenen: PASS (yarış yok: `answers[i]` her goroutine'de ayrı dilim elemanı).

- [ ] **Adım 6: Commit**

```bash
git add internal/domaincheck/dnstest/dnstest.go internal/domaincheck/authoritative.go internal/domaincheck/authoritative_test.go
git commit -m "update authoritative dns query"
```

---

### Görev 6: Karar kuralları (Checker)

**Dosyalar:**
- Değiştir: `internal/domaincheck/domaincheck.go` (Report alanları, `Checker`, `Check`)
- Oluştur: `internal/domaincheck/judge.go` (`judge`, `checkFamily`, `judgeAuthoritative`, yardımcılar; `checkFamily` buraya TAŞINIR)
- Test: `internal/domaincheck/checker_test.go`; mevcut `domaincheck_test.go` DEĞİŞMEDEN geçmeli.

**Arayüzler:**
- Tüketir: Görev 1–5'in hepsi.
- Üretir:
  - `type Fix struct{ Add, Remove []Record }`
  - `Report`'a eklenen alanlar: `Zone string; NameServers []string; ZoneChecked bool; Provider Provider; HasProvider bool; RecordName string; Auth Authoritative; CNAME string; Proxied, OnCloudflare bool; Fix Fix`
  - `type Checker struct{ Resolver Resolver; NS NSResolver; Exchange Exchanger; Budget time.Duration }`
  - `func (c Checker) Check(ctx context.Context, domain, serverHost string) Report`
  - `func Check(ctx context.Context, r Resolver, domain, serverHost string) Report` (K-128 uyumlu kısayol: `Checker{Resolver: r}.Check`)
  - `const DefaultBudget = 10 * time.Second`

- [ ] **Adım 1: Başarısız testi yaz** — `internal/domaincheck/checker_test.go`:

```go
package domaincheck

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
	"golang.org/x/net/dns/dnsmessage"
)

// zoneChecker: example.com'un bölgesi ns verilen firmada; iki ad sunucusu
// 192.0.2.1 ve 192.0.2.2. local, yerel çözümleyicinin (önbelleğin) cevabı.
func zoneChecker(nsHosts []string, local fakeResolver, a1, a2 dnstest.Answer) Checker {
	res := fakeResolver{}
	for i, h := range nsHosts {
		// Anahtar TEMİZ ad: bölge yürüyüşü NS adlarını CleanName'den geçiriyor.
		res["ip4 "+CleanName(h)] = answer{ips: []string{[]string{"192.0.2.1", "192.0.2.2"}[i]}}
	}
	for k, v := range local {
		res[k] = v
	}
	return Checker{
		Resolver: res,
		NS:       fakeNS{"example.com": nsHosts},
		Exchange: dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": a1, "192.0.2.2": a2}),
	}
}

var (
	vercelNS     = []string{"ns1.vercel-dns.com.", "ns2.vercel-dns.com."}
	cloudflareNS = []string{"adam.ns.cloudflare.com.", "eve.ns.cloudflare.com."}
	porkbunNS    = []string{"curitiba.ns.porkbun.com.", "fortaleza.ns.porkbun.com."}
)

func auth(addrs ...string) dnstest.Answer {
	return dnstest.Answer{AA: true, Addrs: addrs, TTL: 600}
}

func ip4(name string, ips ...string) fakeResolver {
	return fakeResolver{"ip4 " + name: {ips: ips}}
}

func merge(fs ...fakeResolver) fakeResolver {
	out := fakeResolver{}
	for _, f := range fs {
		for k, v := range f {
			out[k] = v
		}
	}
	return out
}

func TestCheckerRules(t *testing.T) {
	right, wrong := "203.0.113.10", "198.51.100.7"
	tests := []struct {
		name    string
		c       Checker
		domain  string
		server  string
		want    Verdict
		reasons []string // gerekçede geçmesi gereken parçalar
		check   func(t *testing.T, rep Report)
	}{
		{
			name:   "yetkili doğru, yerel önbellek eski: uyarır, durmaz",
			c:      zoneChecker(vercelNS, ip4("app.example.com", wrong), auth(right), auth(right)),
			domain: "app.example.com", server: right,
			want: Warn, reasons: []string{"(Vercel) already answers 203.0.113.10", "600 s", "-wait"},
		},
		{
			name:   "yetkili yanlış, yerel doğru: durur",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right), auth(wrong), auth(wrong)),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{wrong, "own nameservers"},
			check: func(t *testing.T, rep Report) {
				if rep.Zone != "example.com" || rep.RecordName != "app" || !rep.HasProvider || rep.Provider.Name != "Vercel" {
					t.Errorf("bölge/ad/firma: %q %q %v %q", rep.Zone, rep.RecordName, rep.HasProvider, rep.Provider.Name)
				}
				want := []Record{{Type: "A", Name: "app", Value: right}}
				if !slices.Equal(rep.Fix.Add, want) {
					t.Errorf("Fix.Add = %v, beklenen %v", rep.Fix.Add, want)
				}
			},
		},
		{
			name:   "karışık küme: sunucu + eski adres → durur, fazlalık silinecekler listesinde",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right, wrong), auth(right, wrong), auth(right, wrong)),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"(server) and " + wrong},
			check: func(t *testing.T, rep Report) {
				if !slices.Contains(rep.Fix.Remove, Record{Type: "A", Name: "app", Value: wrong}) {
					t.Errorf("Fix.Remove fazlalığı içermiyor: %v", rep.Fix.Remove)
				}
			},
		},
		{
			name:   "karışık küme yerel yolda da durur (NS yok)",
			c:      Checker{Resolver: ip4("app.example.com", right, wrong)},
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"(server) and " + wrong},
		},
		{
			name:   "NS'ler tutarsız: uyarır",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right), auth(right), auth(wrong)),
			domain: "app.example.com", server: right,
			want: Warn, reasons: []string{"do not agree"},
		},
		{
			name: "AA yok (ISS araya girdi): yerel kurallar",
			c: zoneChecker(vercelNS, ip4("app.example.com", wrong),
				dnstest.Answer{Addrs: []string{right}}, dnstest.Answer{Addrs: []string{right}}),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"the A record points at " + wrong},
			check: func(t *testing.T, rep Report) {
				if rep.Auth.Answered {
					t.Errorf("AA bayraksız cevap yetkili sayıldı")
				}
			},
		},
		{
			name: "CNAME hedefi sunucuyu gösteriyor: geçer",
			c: zoneChecker(vercelNS, merge(ip4("www.example.com", right), ip4("target.example.net", right)),
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300},
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}),
			domain: "www.example.com", server: right,
			want: OK,
		},
		{
			name: "CNAME hedefi başka yer: durur, CNAME silinecekler listesinde",
			c: zoneChecker(vercelNS, merge(ip4("www.example.com", wrong), ip4("target.example.net", wrong)),
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300},
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}),
			domain: "www.example.com", server: right,
			want: Stop, reasons: []string{"CNAME to target.example.net"},
			check: func(t *testing.T, rep Report) {
				if !slices.Contains(rep.Fix.Remove, Record{Type: "CNAME", Name: "www", Value: "target.example.net"}) {
					t.Errorf("Fix.Remove CNAME içermiyor: %v", rep.Fix.Remove)
				}
			},
		},
		{
			name:   "Cloudflare aralığı, NS Cloudflare'de: turuncu bulut",
			c:      zoneChecker(cloudflareNS, ip4("app.example.com", "104.21.3.4"), auth("104.21.3.4"), auth("104.21.3.4")),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"proxied (orange cloud)"},
			check: func(t *testing.T, rep Report) {
				if !rep.Proxied || rep.OnCloudflare || len(rep.Fix.Add) != 0 {
					t.Errorf("Proxied=%v OnCloudflare=%v Fix.Add=%v", rep.Proxied, rep.OnCloudflare, rep.Fix.Add)
				}
			},
		},
		{
			name:   "Cloudflare aralığı, NS Porkbun'da: park sayfası",
			c:      zoneChecker(porkbunNS, ip4("app.example.com", "104.21.3.4"), auth("104.21.3.4"), auth("104.21.3.4")),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"Cloudflare-hosted page"},
			check: func(t *testing.T, rep Report) {
				if rep.Proxied || !rep.OnCloudflare || !slices.Contains(rep.Fix.Remove, Record{Type: "A", Name: "app", Value: "104.21.3.4"}) {
					t.Errorf("Proxied=%v OnCloudflare=%v Fix.Remove=%v", rep.Proxied, rep.OnCloudflare, rep.Fix.Remove)
				}
			},
		},
		{
			name: "yetkili NXDOMAIN, yerelde eski kayıt: durur",
			c: zoneChecker(vercelNS, ip4("app.example.com", right),
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"no A or AAAA record"},
		},
		{
			// İnceleme odağı 1: Vercel'in joker kaydı yeni adı Vercel'e götürüyor.
			name:   "joker kayıt: yeni alt ad Vercel'i gösteriyor",
			c:      zoneChecker(vercelNS, ip4("new.example.com", wrong), auth(wrong), auth(wrong)),
			domain: "new.example.com", server: right,
			want: Stop, reasons: []string{wrong},
			check: func(t *testing.T, rep Report) {
				if rep.RecordName != "new" || !slices.Equal(rep.Fix.Add, []Record{{Type: "A", Name: "new", Value: right}}) {
					t.Errorf("kayıt adı %q, Fix.Add %v", rep.RecordName, rep.Fix.Add)
				}
			},
		},
		{
			// İnceleme odağı 5: yalnız IPv6'sı bilinen sunucuya A önerilmez.
			name: "yalnız IPv6 sunucu: Fix.Add yalnız AAAA",
			c: zoneChecker(vercelNS, fakeResolver{"ip6 sunucu.example": {ips: []string{"2001:db8::10"}}},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}),
			domain: "app.example.com", server: "sunucu.example",
			want: Stop,
			check: func(t *testing.T, rep Report) {
				if !slices.Equal(rep.Fix.Add, []Record{{Type: "AAAA", Name: "app", Value: "2001:db8::10"}}) {
					t.Errorf("Fix.Add = %v", rep.Fix.Add)
				}
			},
		},
		{
			name:   "NS kapalı (K-128 yolu): bölge yok, kayıt adı tahmin edilir",
			c:      Checker{Resolver: ip4("app.example.com", wrong)},
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"the A record points at " + wrong},
			check: func(t *testing.T, rep Report) {
				if rep.Zone != "" || rep.ZoneChecked || rep.RecordName != "app" {
					t.Errorf("Zone=%q ZoneChecked=%v RecordName=%q", rep.Zone, rep.ZoneChecked, rep.RecordName)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := tc.c.Check(t.Context(), tc.domain, tc.server)
			if rep.Verdict != tc.want {
				t.Fatalf("karar %v, beklenen %v; gerekçe: %q", rep.Verdict, tc.want, rep.Reasons)
			}
			all := strings.Join(rep.Reasons, "\n")
			for _, r := range tc.reasons {
				if !strings.Contains(all, r) {
					t.Errorf("gerekçede %q yok: %q", r, rep.Reasons)
				}
			}
			if tc.check != nil {
				tc.check(t, rep)
			}
		})
	}
}

// Ad sunucusu cevap vermiyor: bütçe dolunca yerel kurallara düşülür,
// komut takılmaz (spec "Süre bütçesi").
func TestCheckerBudgetFallsBackToLocalRules(t *testing.T) {
	block := dnstest.Answer{Block: true}
	c := zoneChecker(vercelNS, ip4("app.example.com", "203.0.113.10"), block, block)
	c.Budget = 200 * time.Millisecond

	start := time.Now()
	rep := c.Check(t.Context(), "app.example.com", "203.0.113.10")
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("bütçe uygulanmadı: %v sürdü", took)
	}
	if rep.Auth.Answered || rep.Verdict != OK {
		t.Fatalf("bütçe aşımında yerel kural bekleniyordu: %+v", rep)
	}
}
```

- [ ] **Adım 2: Derlenmediğini gör**

Çalıştır: `go test ./internal/domaincheck/ -run 'CheckerRules|CheckerBudget' -count=1` → `undefined: Checker`.

- [ ] **Adım 3: `domaincheck.go`'yu güncelle**

Paket yorumuna (dosyanın başı, `//   - .localhost denetlenmez…` satırından sonra) ekle:

```go
//   - K-151: bölgenin ad sunucuları biliniyorsa karar ONLARIN cevabına
//     göre verilir (yerel önbellek eski olabilir); sunucunun adresiyle
//     başka adres birlikte dönerse durulur; Cloudflare aralığı ayrı
//     anlatılır. Hepsi DefaultBudget içinde; aşılırsa yukarıdaki yerel
//     kurallar geçerli.
```

İçe aktarmalar değişmez (`time` zaten `LookupTimeout` için var; `publicsuffix` yalnız zone.go'da). `Report` tipinin yerine:

```go
// Fix, kullanıcının DNS'te yapması gerekenler (K-151).
type Fix struct {
	Add    []Record
	Remove []Record
}

// Report, denetimin bulguları.
type Report struct {
	Domain  string
	Verdict Verdict
	// Server, sunucunun bilinen GENEL adresleri.
	Server  []netip.Addr
	A, AAAA []netip.Addr
	Reasons []string

	// ── K-151: DNS'in nerede olduğu ve ne yapılacağı ─────────────────
	Zone        string
	NameServers []string
	// ZoneChecked: NS sorgusu yapıldı (Checker.NS verildi). Zone boşsa
	// bölge bulunamadı: domain kayıtlı ya da devredilmiş olmayabilir.
	ZoneChecked bool
	Provider    Provider
	HasProvider bool
	// RecordName, kaydın bölge içindeki adı ("@" ya da "kadran").
	RecordName string
	Auth       Authoritative
	// CNAME, yetkili cevap yalnız CNAME döndüyse hedefi.
	CNAME string
	// Proxied: Cloudflare aralığı ve NS Cloudflare'de (turuncu bulut).
	Proxied bool
	// OnCloudflare: Cloudflare aralığı, NS başka firmada (ör. park sayfası).
	OnCloudflare bool
	Fix          Fix
}
```

`Check` fonksiyonunun yerine (yardımcılar `isPublic`, `lookup`, `serverAddrs`, `family`, `overlaps`, `join` olduğu gibi kalır; `checkFamily` judge.go'ya taşınır):

```go
// DefaultBudget, bölge yürüyüşü ve yetkili sorguların toplam süresi.
// Aşılırsa yalnız yerel çözümleyiciyle karar verilir; komut takılmaz.
const DefaultBudget = 10 * time.Second

// Checker, alan adı denetimini yapar. NS nil ise bölge, firma ve yetkili
// sorgu yapılmaz: K-128'in yalnız-yerel davranışı.
type Checker struct {
	Resolver Resolver
	NS       NSResolver
	// Exchange nil ise gerçek ağ (UDP/TCP 53).
	Exchange Exchanger
	// Budget sıfırsa DefaultBudget.
	Budget time.Duration
}

// Check, K-128 uyumlu kısayol: yalnız yerel çözümleyici.
func Check(ctx context.Context, r Resolver, domain, serverHost string) Report {
	return Checker{Resolver: r}.Check(ctx, domain, serverHost)
}

// Check, alan adının A/AAAA kayıtlarını sunucunun adresleriyle
// karşılaştırır. serverHost: SSH hedefinin ad ya da adresi; yerel hedefte
// boş.
func (c Checker) Check(ctx context.Context, domain, serverHost string) Report {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	rep := Report{Domain: domain, Verdict: OK, RecordName: guessRecordName(domain)}

	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		rep.raise(Skipped, "%s is a .localhost name: it is not in DNS, Caddy uses its internal certificate", domain)
		return rep
	}

	a, aNotFound, aErr := lookup(ctx, c.Resolver, "ip4", domain)
	aaaa, aaaaNotFound, aaaaErr := lookup(ctx, c.Resolver, "ip6", domain)
	rep.A, rep.AAAA = a, aaaa
	rep.Server = serverAddrs(ctx, c.Resolver, serverHost)
	c.locate(ctx, &rep)

	if rep.Auth.Answered && rep.Auth.Agree {
		c.judgeAuthoritative(ctx, &rep, serverHost)
	} else {
		for _, e := range []error{aErr, aaaaErr} {
			if e != nil {
				rep.raise(Warn, "DNS lookup did not complete (%v); records could not be checked", e)
			}
		}
		judge(&rep, a, aaaa, aNotFound && aaaaNotFound, serverHost)
		if rep.Auth.Answered && rep.Verdict < Stop {
			rep.raise(Warn, "the DNS host's nameservers do not agree yet (%s): the change is still spreading inside the DNS host",
				strings.Join(rep.NameServers, ", "))
		}
	}
	rep.Fix.Add = fixRecords(rep)
	return rep
}

// locate, bölgeyi, firmayı ve yetkili cevabı bulur; tamamı Budget içinde.
func (c Checker) locate(ctx context.Context, rep *Report) {
	if c.NS == nil {
		return
	}
	rep.ZoneChecked = true
	budget := c.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	zone, ok := FindZone(ctx, c.NS, rep.Domain)
	if !ok {
		return
	}
	rep.Zone, rep.NameServers = zone.Name, zone.NameServers
	rep.RecordName = RecordName(rep.Domain, zone.Name)
	rep.Provider, rep.HasProvider = IdentifyProvider(zone.NameServers)
	rep.Auth = AskAuthoritative(ctx, c.Resolver, c.Exchange, zone.NameServers, rep.Domain)
}
```

- [ ] **Adım 4: `judge.go`'yu yaz** (eski `checkFamily` domaincheck.go'dan SİLİNİR, buraya genişletilmiş hâliyle gelir)

```go
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
```

- [ ] **Adım 5: Testleri geçir**

Çalıştır: `go test ./internal/domaincheck/... -count=1 -race`
Beklenen: `TestCheckerRules` (14 alt test), `TestCheckerBudgetFallsBackToLocalRules` ve MEVCUT `TestCheck` (14 alt test), `TestALookupErrorIsNeverReadAsNoRecords`, `TestWrappedNotFoundIsRecognised` PASS. Mevcut testlerden biri düşerse kural değişikliği istenmeden davranışı bozmuş demektir: testi değil kodu düzelt.

Çalıştır: `go test ./cmd/kadran/ -count=1` → PASS (CLI henüz `Check` kısayolunu kullanıyor).

- [ ] **Adım 6: Commit**

```bash
git add internal/domaincheck/domaincheck.go internal/domaincheck/judge.go internal/domaincheck/checker_test.go
git commit -m "update dns check rules"
```

---

### Görev 7: CLI tarifi ve bağlantısı

**Dosyalar:**
- Oluştur: `cmd/kadran/domainguide.go`
- Değiştir: `cmd/kadran/main.go` (`cli` alanları), `cmd/kadran/domaincheck.go` (`dnsChecker`, `checkDomain`), `cmd/kadran/domain.go` (`probeDNS`)
- Test: `cmd/kadran/domainguide_test.go`

**Arayüzler:**
- Tüketir: `domaincheck.Checker`, `Report` (Görev 6), `dnstest` (Görev 5).
- Üretir: `func writeDNSGuide(w io.Writer, rep domaincheck.Report)`; `func (c *cli) dnsChecker() domaincheck.Checker`; `cli` alanları `nsResolver domaincheck.NSResolver`, `exchange domaincheck.Exchanger`; `const domainsGuideURL`.

- [ ] **Adım 1: Başarısız testi yaz** — `cmd/kadran/domainguide_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
)

// cliNS, ada göre NS kayıtları; tanımsız ad "bulunamadı".
type cliNS map[string][]string

func (f cliNS) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	hosts, ok := f[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	out := make([]*net.NS, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, &net.NS{Host: h})
	}
	return out, nil
}

// vercelCLI: example.com Vercel'de; ad sunucusu 192.0.2.1 authAddrs döner.
func vercelCLI(local cliResolver, authAddrs ...string) (*cli, *bytes.Buffer, *bytes.Buffer, *int) {
	res := cliResolver{"ip4 ns1.vercel-dns.com": {"192.0.2.1"}}
	for k, v := range local {
		res[k] = v
	}
	c, stdout, stderr, dials := dnsTestCLI(res)
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: authAddrs, TTL: 600}})
	return c, stdout, stderr, dials
}

func TestAppCreateShowsTheDNSHostAndTheRecord(t *testing.T) {
	c, _, stderr, dials := vercelCLI(cliResolver{"ip4 app.example.com": {"198.51.100.7"}}, "198.51.100.7")

	code := c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if code != exitError || *dials != 0 {
		t.Fatalf("çıkış %d, bağlantı %d\n%s", code, *dials, stderr)
	}
	for _, want := range []string{
		"DNS host  Vercel (ns1.vercel-dns.com)",
		"A     app",
		"203.0.113.10",
		"vercel dns add example.com 'app' A 203.0.113.10",
		"Guide: https://vercel.com/",
		"kadran domain check -wait 10m app.example.com kimse@203.0.113.10",
		"-skip-dns-check",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("hata %q içermiyor:\n%s", want, stderr)
		}
	}
}

// Yetkili doğru, yerel eski: bağlantı DENENİR ve uyarı stderr'de.
func TestAppCreateContinuesWhenOnlyTheCacheIsStale(t *testing.T) {
	c, stdout, stderr, dials := vercelCLI(cliResolver{"ip4 app.example.com": {"198.51.100.7"}}, "203.0.113.10")

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf("yalnız önbellek eskiyken bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
	if !strings.Contains(stderr.String(), "already answers 203.0.113.10") || stdout.Len() != 0 {
		t.Errorf("uyarı stderr'de değil ya da stdout kirlendi:\nstderr=%s\nstdout=%s", stderr, stdout)
	}
}

func TestWriteDNSGuideForProxiedCloudflare(t *testing.T) {
	c, _, stderr, _ := dnsTestCLI(cliResolver{
		"ip4 app.example.com":        {"104.21.3.4"},
		"ip4 adam.ns.cloudflare.com": {"192.0.2.1"},
	})
	c.nsResolver = cliNS{"example.com": {"adam.ns.cloudflare.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"104.21.3.4"}}})

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	for _, want := range []string{"Proxy status: DNS only (grey cloud)", "Full (strict)"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("%q yok:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr.String(), "Set this record") {
		t.Errorf("proxy'li kayıtta kayıt eklemek önerilmemeli:\n%s", stderr)
	}
}

// Sahte çözümleyiciyle (test) NS ya da yetkili sorgu verilmemişse ağa
// ÇIKILMAZ: CI gerçek DNS'e bağlı olamaz.
func TestDNSCheckerStaysOfflineInTests(t *testing.T) {
	c, _, _, _ := dnsTestCLI(cliResolver{})
	if ch := c.dnsChecker(); ch.NS != nil {
		t.Fatalf("test çözümleyicisiyle NS sorgusu açık")
	}
	c.nsResolver = cliNS{}
	ch := c.dnsChecker()
	if ch.Exchange == nil {
		t.Fatalf("NS verilmiş ama yetkili sorgu gerçek ağa açık kaldı")
	}
	server := netip.AddrPortFrom(netip.MustParseAddr("192.0.2.1"), 53)
	if _, err := ch.Exchange(t.Context(), server, nil); err == nil {
		t.Fatalf("test yetkili sorgusu hata vermedi")
	}
}
```

- [ ] **Adım 2: Derlenmediğini gör**

Çalıştır: `go test ./cmd/kadran/ -run 'DNSHost|StaleCache|OnlyTheCache|ProxiedCloudflare|StaysOffline' -count=1` → `c.nsResolver undefined`.

- [ ] **Adım 3: `cli` alanlarını ekle** — `cmd/kadran/main.go`, `keys keyManager` satırından önce:

```go
	// K-151: NS sorgusu ve yetkili DNS sorgusu. resolver bir test
	// sahtesiyken bunlar nil ise AĞA ÇIKILMAZ (dnsChecker).
	nsResolver domaincheck.NSResolver
	exchange   domaincheck.Exchanger
```

- [ ] **Adım 4: `dnsChecker` ve `checkDomain`** — `cmd/kadran/domaincheck.go`:

`dnsResolver` fonksiyonunun altına:

```go
// dnsChecker, önkontrol ve `domain check` için denetleyici (K-151). Gerçek
// çözümleyicide NS ve yetkili sorgular açık. Testte (resolver sahte) NS
// yalnız test kendi sahtesini verirse açılır, yetkili sorgu da yalnız test
// sahtesiyle: CI gerçek DNS'e çıkmasın.
func (c *cli) dnsChecker() domaincheck.Checker {
	if c.resolver == nil {
		return domaincheck.Checker{Resolver: net.DefaultResolver, NS: net.DefaultResolver}
	}
	ch := domaincheck.Checker{Resolver: c.resolver, Exchange: c.exchange}
	if c.nsResolver != nil {
		ch.NS = c.nsResolver
	}
	if ch.Exchange == nil {
		ch.Exchange = offlineExchange
	}
	return ch
}

// offlineExchange, testte yetkili sorguyu kapatır.
func offlineExchange(context.Context, netip.AddrPort, []byte) ([]byte, error) {
	return nil, errors.New("authoritative DNS queries are off in this test")
}
```

(`"net/netip"` içe aktarılır; `errors` zaten var.)

`checkDomain`'in `rep := domaincheck.Check(...)` satırından sonuna kadarki kısmı:

```go
	rep := c.dnsChecker().Check(ctx, domain, server)
	switch rep.Verdict {
	case domaincheck.OK:
		return nil
	case domaincheck.Skipped, domaincheck.Warn:
		for _, r := range rep.Reasons {
			fmt.Fprintf(c.stderr, progName+": warning: %s\n", r)
		}
		return nil
	}
	var b strings.Builder
	if len(rep.A) == 0 && len(rep.AAAA) == 0 && !rep.Auth.Answered {
		fmt.Fprintf(&b, "%s has no DNS record\n", rep.Domain)
	} else {
		fmt.Fprintf(&b, "%s does not point at this server: %s\n", rep.Domain, strings.Join(rep.Reasons, "; "))
	}
	writeDNSGuide(&b, rep)
	b.WriteString("  No certificate can be issued; Caddy may wait up to a day before retrying.\n")
	fmt.Fprintf(&b, "  After fixing it: kadran domain check -wait 10m %s %s\n", rep.Domain, rawTarget)
	fmt.Fprintf(&b, "  If the domain is behind a proxy on purpose, skip with -%s", skipDNSCheckFlag)
	return errors.New(b.String())
```

- [ ] **Adım 5: `domainguide.go`'yu yaz**

```go
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
	case rep.ZoneChecked:
		fmt.Fprintln(w, "  DNS host  unknown: no nameservers found; the domain may not be registered or delegated yet")
	}
	if rep.Proxied {
		fmt.Fprintln(w, "  At Cloudflare: DNS → Records → edit the record → Proxy status: DNS only (grey cloud)")
		fmt.Fprintf(w, "  Keeping the proxy is possible: SSL/TLS mode Full (strict), then -%s.\n", skipDNSCheckFlag)
		return
	}
	if len(rep.Fix.Remove) > 0 {
		fmt.Fprintln(w, "  Delete these records:")
		for _, r := range rep.Fix.Remove {
			writeRecord(w, r)
		}
	}
	if len(rep.Fix.Add) > 0 {
		fmt.Fprintln(w, "  Set this record and delete other A, AAAA, ALIAS or CNAME records for the same name:")
		for _, r := range rep.Fix.Add {
			writeRecord(w, r)
		}
	} else {
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

func writeRecord(w io.Writer, r domaincheck.Record) {
	fmt.Fprintf(w, "      %-5s %-12s %s\n", r.Type, r.Name, r.Value)
}
```

- [ ] **Adım 6: `domain check`'i bağla** — `cmd/kadran/domain.go`, `probeDNS`:

```go
func (c *cli) probeDNS(ctx context.Context, r *domainReport, domain, rawTarget string) domaincheck.Report {
	server, ok := c.serverHostFor(ctx, rawTarget)
	if !ok {
		r.warn("DNS", "could not parse the target; the server's address is unknown")
	}
	rep := c.dnsChecker().Check(ctx, domain, server)
	records := joinAddrs(append(rep.A, rep.AAAA...))
	switch rep.Verdict {
	case domaincheck.OK:
		r.ok("DNS", records+" (server)")
	case domaincheck.Skipped, domaincheck.Warn:
		r.warn("DNS", strings.Join(rep.Reasons, "; "))
	default:
		r.fail("DNS", strings.Join(rep.Reasons, "; "))
		writeDNSGuide(r.out, rep)
	}
	return rep
}
```

ve `runDomainCheck`'te çağrı: `if c.probeDNS(ctx, r, domain, fs.Arg(1)).Verdict == domaincheck.Stop {`. (Mutasyon betiğinin "başka sunucu ölçüldüğü söylenmiyor" mutantı bu satırı arıyor; Görev 10'da güncellenecek.)

- [ ] **Adım 7: Testleri geçir**

Çalıştır: `go test ./cmd/kadran/ ./internal/domaincheck/... -count=1 -race`
Beklenen: yeni 4 test + mevcut `TestAppCreateStopsOnWrongDNSBeforeConnecting`, `TestDomainCheckWrongDNS` ve diğerleri PASS.

- [ ] **Adım 8: Commit**

```bash
git add cmd/kadran/main.go cmd/kadran/domaincheck.go cmd/kadran/domain.go cmd/kadran/domainguide.go cmd/kadran/domainguide_test.go
git commit -m "update dns guidance in cli"
```

---

### Görev 8: Kayıt firması (RDAP), yalnız `domain check`

**Dosyalar:**
- Oluştur: `internal/domaincheck/rdap.go`
- Değiştir: `cmd/kadran/main.go` (`rdapClient *http.Client`, `rdapBootstrap string` alanları), `cmd/kadran/domain.go` (`domainReport.info`, kayıt satırı)
- Test: `internal/domaincheck/rdap_test.go`, `cmd/kadran/domain_test.go` (yeni test)

**Arayüzler:**
- Üretir: `const IANABootstrapURL = "https://data.iana.org/rdap/dns.json"`; `const RDAPTimeout = 5 * time.Second`; `func RDAPClient() *http.Client`; `func LookupRegistrar(ctx context.Context, hc *http.Client, bootstrapURL, domain string) (string, error)`; `func (c *cli) printRegistrar(ctx context.Context, r *domainReport, rep domaincheck.Report)`.

- [ ] **Adım 1: Başarısız testi yaz** — `internal/domaincheck/rdap_test.go`:

```go
package domaincheck

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rdapServer: önyükleme dosyası "com" uzantısını kendine yönlendiriyor;
// domain/<ad> cevabı registrar varlığını taşıyor.
func rdapServer(t *testing.T, domainBody string) (*httptest.Server, string) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/bootstrap.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"services":[[["net","com"],["%s/rdap/"]],[["dev"],["https://elsewhere.example/"]]]}`, srv.URL)
	})
	mux.HandleFunc("/rdap/domain/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(domainBody))
	})
	return srv, srv.URL + "/bootstrap.json"
}

func rdapTestClient(srv *httptest.Server) *http.Client {
	hc := srv.Client()
	hc.CheckRedirect = RDAPClient().CheckRedirect
	return hc
}

const registrarBody = `{"entities":[
 {"roles":["registrant"],"vcardArray":["vcard",[["fn",{},"text","Someone"]]]},
 {"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","Name.com, Inc."]]]}]}`

func TestLookupRegistrar(t *testing.T) {
	srv, boot := rdapServer(t, registrarBody)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "app.example.com")
	if err != nil || got != "Name.com, Inc." {
		t.Fatalf("LookupRegistrar = %q, %v", got, err)
	}
}

func TestLookupRegistrarWithoutRDAPForTheTLD(t *testing.T) {
	srv, boot := rdapServer(t, registrarBody)
	got, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com.tr")
	if err != nil || got != "" {
		t.Fatalf(".tr için satır beklenmiyordu: %q, %v", got, err)
	}
}

func TestLookupRegistrarWithoutRegistrarEntity(t *testing.T) {
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrant"]}]}`)
	if got, _ := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); got != "" {
		t.Fatalf("registrar varlığı yokken %q döndü", got)
	}
}

func TestLookupRegistrarCleansTheName(t *testing.T) {
	srv, boot := rdapServer(t, `{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Evil\u001b[31m Registrar"]]]}]}`)
	got, _ := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com")
	if strings.ContainsRune(got, 0x1b) || got != "Evil[31m Registrar" {
		t.Fatalf("kaçış dizisi temizlenmedi: %q", got)
	}
}

// Yönlendirme düz http'deki ÇALIŞAN bir önyükleme dosyasına gidiyor: izlenseydi
// sorgu başarılı olurdu. Hata yalnız yönlendirme reddedildiği için gelir; ağ
// hatası bu testi yanlışlıkla geçiremez (mutant "https olmayan yönlendirme
// izleniyor" böyle yakalanıyor).
func TestLookupRegistrarRefusesPlainHTTPRedirect(t *testing.T) {
	tlsSrv, _ := rdapServer(t, registrarBody)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"services":[[["com"],["%s/rdap/"]]]}`, tlsSrv.URL)
	}))
	t.Cleanup(plain.Close)
	mux := http.NewServeMux()
	redirector := httptest.NewTLSServer(mux)
	t.Cleanup(redirector.Close)
	mux.HandleFunc("/bootstrap.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/boot.json", http.StatusFound)
	})
	// httptest'in bütün TLS sunucuları aynı test sertifikasını kullanıyor:
	// bir sunucunun istemcisi ötekine de güvenir.
	got, err := LookupRegistrar(t.Context(), rdapTestClient(redirector), redirector.URL+"/bootstrap.json", "example.com")
	if err == nil {
		t.Fatalf("https olmayan yönlendirme izlendi (cevap %q)", got)
	}
}

func TestLookupRegistrarLimitsTheBody(t *testing.T) {
	huge := `{"entities":[` + strings.Repeat(`{"roles":["x"]},`, 200000) + `{"roles":["registrar"]}]}`
	srv, boot := rdapServer(t, huge)
	if _, err := LookupRegistrar(t.Context(), rdapTestClient(srv), boot, "example.com"); err == nil {
		t.Fatal("1 MiB üstü cevap okundu")
	}
}
```

- [ ] **Adım 2: Derlenmediğini gör**

Çalıştır: `go test ./internal/domaincheck/ -run Registrar -count=1` → `undefined: LookupRegistrar`.

- [ ] **Adım 3: `rdap.go`'yu yaz**

```go
package domaincheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"
)

// IANABootstrapURL, IANA'nın RDAP önyükleme dosyası (RFC 9224). Üçüncü
// taraf yönlendirici (rdap.org) KULLANILMAZ: domain adı yalnız kayıt
// otoritesine gider (K-151).
const IANABootstrapURL = "https://data.iana.org/rdap/dns.json"

// RDAPTimeout, tek RDAP isteğinin süre sınırı.
const RDAPTimeout = 5 * time.Second

// rdapMaxBody, okunacak en büyük RDAP cevabı.
const rdapMaxBody = 1 << 20

// maxRegistrarLen, basılacak firma adının en uzun hâli (rune).
const maxRegistrarLen = 100

// RDAPClient, yalnız https yönlendirmesi izleyen HTTP istemcisi.
func RDAPClient() *http.Client {
	return &http.Client{
		Timeout: RDAPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return errors.New("rdap: refusing a redirect that is not https")
			}
			if len(via) >= 5 {
				return errors.New("rdap: too many redirects")
			}
			return nil
		},
	}
}

// LookupRegistrar, domainin akredite kayıt firmasını RDAP'tan okur. RDAP'ı
// olmayan uzantıda (ör. .tr) ya da registrar varlığı yoksa "" ve nil döner.
// Bayi (Vercel gibi) değil, akredite kayıt firması döner: erkanrzgc.dev
// Vercel'den alındı, RDAP Name.com dedi (15 Ekim 2026).
func LookupRegistrar(ctx context.Context, hc *http.Client, bootstrapURL, domain string) (string, error) {
	reg, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return "", err
	}
	tld := reg[strings.LastIndex(reg, ".")+1:]

	var boot struct {
		Services [][][]string `json:"services"`
	}
	if err := getJSON(ctx, hc, bootstrapURL, &boot); err != nil {
		return "", err
	}
	base := ""
	for _, svc := range boot.Services {
		if len(svc) != 2 || !slices.Contains(svc[0], tld) {
			continue
		}
		for _, u := range svc[1] {
			if strings.HasPrefix(u, "https://") {
				base = u
				break
			}
		}
	}
	if base == "" {
		return "", nil
	}

	var dom struct {
		Entities []rdapEntity `json:"entities"`
	}
	if err := getJSON(ctx, hc, strings.TrimSuffix(base, "/")+"/domain/"+url.PathEscape(reg), &dom); err != nil {
		return "", err
	}
	for _, e := range dom.Entities {
		if slices.Contains(e.Roles, "registrar") {
			return cleanText(e.fn()), nil
		}
	}
	return "", nil
}

type rdapEntity struct {
	Roles []string          `json:"roles"`
	VCard []json.RawMessage `json:"vcardArray"`
}

// fn, vCard'ın "fn" (görünen ad) alanı: ["vcard", [[ad, {}, tip, değer], …]].
func (e rdapEntity) fn() string {
	if len(e.VCard) < 2 {
		return ""
	}
	var props [][]json.RawMessage
	if json.Unmarshal(e.VCard[1], &props) != nil {
		return ""
	}
	for _, p := range props {
		if len(p) < 4 {
			continue
		}
		var name, val string
		if json.Unmarshal(p[0], &name) != nil || name != "fn" {
			continue
		}
		if json.Unmarshal(p[3], &val) == nil {
			return val
		}
	}
	return ""
}

func getJSON(ctx context.Context, hc *http.Client, u string, v any) error {
	if !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("rdap: %s is not https", u)
	}
	ctx, cancel := context.WithTimeout(ctx, RDAPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rdap: %s answered %s", u, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, rdapMaxBody)).Decode(v)
}

// cleanText, güvenilmez metni basılabilir yapar: kontrol ve kaçış
// karakterleri atılır, uzunluk sınırlanır.
func cleanText(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxRegistrarLen {
			break
		}
		if unicode.IsPrint(r) {
			b.WriteRune(r)
			n++
		}
	}
	return strings.TrimSpace(b.String())
}
```

`TestLookupRegistrarLimitsTheBody`: 200000 × 16 bayt ≈ 3,2 MiB; LimitReader 1 MiB'te keser, JSON yarım kalır, Decode hata verir. Testin geçmesi bunu kanıtlar.

- [ ] **Adım 4: RDAP testlerini geçir**

Çalıştır: `go test ./internal/domaincheck/ -run Registrar -count=1 -v` → 6 test PASS.

- [ ] **Adım 5: CLI başarısız testi** — `cmd/kadran/domain_test.go` sonuna:

```go
// Kayıt firması satırı: DNS başka firmadaysa kaydın NEREDE değişeceğini
// açıkça söyler (K-151).
func TestDomainCheckShowsTheRegistrar(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}, "ip4 ns1.vercel-dns.com": {"192.0.2.1"}}
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"198.51.100.7"}}})

	mux := http.NewServeMux()
	rdap := httptest.NewTLSServer(mux)
	t.Cleanup(rdap.Close)
	mux.HandleFunc("/boot.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[[["com"],["` + rdap.URL + `/"]]]}`))
	})
	mux.HandleFunc("/domain/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Name.com, Inc."]]]}]}`))
	})
	c.rdapClient = rdap.Client()
	c.rdapBootstrap = rdap.URL + "/boot.json"

	_ = c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	for _, want := range []string{"registrar", "Name.com, Inc.", "the DNS is at Vercel: change the record there"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q yok:\n%s", want, out)
		}
	}
}
```

İçe aktarmaya `"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"` eklenir (`http`, `httptest` zaten var).

- [ ] **Adım 6: CLI'ı yaz**

`cmd/kadran/main.go`, `exchange` alanının altına:

```go
	// `domain check`: kayıt firması (RDAP, K-151). resolver test sahtesiyken
	// rdapClient nil ise sorgu yapılmaz.
	rdapClient    *http.Client
	rdapBootstrap string
```

(`"net/http"` içe aktarılır.)

`cmd/kadran/domain.go`: `domainReport`'a nötr satır:

```go
func (r *domainReport) info(label, detail string) { r.line("·", label, detail) }
```

`runDomainCheck`'te `probeDNS` çağrısını şöyle değiştir:

```go
	rep := c.probeDNS(ctx, r, domain, fs.Arg(1))
	c.printRegistrar(ctx, r, rep)
	if rep.Verdict == domaincheck.Stop {
```

ve dosyaya ekle:

```go
// printRegistrar, domainin akredite kayıt firmasını basar (K-151). Hata,
// zaman aşımı ya da RDAP'ı olmayan uzantıda sessiz: tanıyı asla bozmaz.
func (c *cli) printRegistrar(ctx context.Context, r *domainReport, rep domaincheck.Report) {
	hc, boot := c.rdapClient, c.rdapBootstrap
	if hc == nil {
		if c.resolver != nil {
			return // test: ağa çıkılmaz
		}
		hc = domaincheck.RDAPClient()
	}
	if boot == "" {
		boot = domaincheck.IANABootstrapURL
	}
	name, err := domaincheck.LookupRegistrar(ctx, hc, boot, rep.Domain)
	if err != nil || name == "" {
		return
	}
	detail := name + " (accredited registrar; resellers such as Vercel sell through one)"
	if rep.HasProvider {
		detail += "\n              the DNS is at " + rep.Provider.Name + ": change the record there"
	}
	r.info("registrar", detail)
}
```

- [ ] **Adım 7: Testleri geçir ve commit**

Çalıştır: `go test ./cmd/kadran/ ./internal/domaincheck/... -count=1 -race` → PASS.

```bash
git add internal/domaincheck/rdap.go internal/domaincheck/rdap_test.go cmd/kadran/main.go cmd/kadran/domain.go cmd/kadran/domain_test.go
git commit -m "update registrar lookup"
```

---

### Görev 9: `kadran domain check -wait`

**Dosyalar:**
- Değiştir: `cmd/kadran/domain.go`, `cmd/kadran/main.go` (`sleep` alanı)
- Test: `cmd/kadran/domain_test.go`

**Arayüzler:**
- Üretir: `const dnsWaitInterval = 15 * time.Second`; `func (c *cli) waitForDNS(ctx context.Context, domain, rawTarget string, wait time.Duration) bool`; `cli.sleep func(ctx context.Context, d time.Duration) error`.

- [ ] **Adım 1: Başarısız testi yaz** — `cmd/kadran/domain_test.go` sonuna:

```go
// switchingResolver: ilk n sorguda wrong, sonra right döner.
type switchingResolver struct {
	n            *int
	after        int
	wrong, right cliResolver
}

func (s switchingResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if network == "ip4" && host == "example.com" {
		*s.n++
	}
	if *s.n <= s.after {
		return s.wrong.LookupIP(ctx, network, host)
	}
	return s.right.LookupIP(ctx, network, host)
}

func TestDomainCheckWaitsUntilTheDNSIsRight(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	n := 0
	c.resolver = switchingResolver{n: &n, after: 2,
		wrong: cliResolver{"ip4 example.com": {"198.51.100.7"}},
		right: cliResolver{"ip4 example.com": {"203.0.113.10"}}}
	sleeps := 0
	c.sleep = func(context.Context, time.Duration) error { sleeps++; return nil }

	code := c.runDomain(t.Context(), []string{"check", "-wait", "10m", "example.com", "kimse@203.0.113.10"})

	if code != exitOK || sleeps != 2 {
		t.Fatalf("çıkış %d, bekleme %d (beklenen 0 ve 2):\n%s", code, sleeps, out)
	}
	if !strings.Contains(out.String(), "waiting") {
		t.Errorf("bekleme satırı yok:\n%s", out)
	}
}

func TestDomainCheckWaitGivesUp(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}}
	sleeps := 0
	c.sleep = func(context.Context, time.Duration) error {
		sleeps++
		if sleeps == 3 {
			return context.DeadlineExceeded
		}
		return nil
	}

	code := c.runDomain(t.Context(), []string{"check", "-wait", "1m", "example.com", "kimse@203.0.113.10"})

	if code != exitError || !strings.Contains(out.String(), "✗ DNS") {
		t.Fatalf("süre dolunca hata beklenirdi (çıkış %d):\n%s", code, out)
	}
}
```

- [ ] **Adım 2: Derlenmediğini gör** → `c.sleep undefined`.

- [ ] **Adım 3: Kodu yaz**

`cmd/kadran/main.go`, `rdapBootstrap` altına:

```go
	// `domain check -wait`: bekleme (nil: gerçek zamanlayıcı).
	sleep func(ctx context.Context, d time.Duration) error
```

`cmd/kadran/domain.go`:

```go
// dnsWaitInterval, `-wait` sırasında iki DNS denetimi arası.
const dnsWaitInterval = 15 * time.Second

// waitForDNS, DNS denetimi durmayı bırakana (OK ya da yalnız uyarı) kadar
// bekler. Yalnız durum değişince satır basar. Süre dolarsa false.
func (c *cli) waitForDNS(ctx context.Context, domain, rawTarget string, wait time.Duration) bool {
	server, _ := c.serverHostFor(ctx, rawTarget)
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	last := ""
	for {
		rep := c.dnsChecker().Check(ctx, domain, server)
		if rep.Verdict != domaincheck.Stop {
			return true
		}
		if now := strings.Join(rep.Reasons, "; "); now != last {
			fmt.Fprintf(c.stdout, "  … waiting for DNS (every %s): %s\n", dnsWaitInterval, now)
			last = now
		}
		if err := c.sleepFor(wctx, dnsWaitInterval); err != nil {
			return false
		}
	}
}

func (c *cli) sleepFor(ctx context.Context, d time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

`runDomainCheck`'te bayrak ve kullanım:

```go
	wait := fs.Duration("wait", 0, "wait until the DNS points at the server (e.g. 10m), checking every 15s")
```

`ctx, cancel := context.WithTimeout(ctx, *timeout)` satırını `context.WithTimeout(ctx, *timeout+*wait)` yap ve `Domain: …` satırından sonra:

```go
	if *wait > 0 {
		c.waitForDNS(ctx, domain, fs.Arg(1), *wait)
	}
```

Bekleme bitince (başarılı ya da süre dolmuş) normal tanı aynen koşar ve son durumu basar; çıkış kodu onun sonucudur.

- [ ] **Adım 4: Testleri geçir ve commit**

Çalıştır: `go test ./cmd/kadran/ -count=1 -race` → PASS.

```bash
git add cmd/kadran/main.go cmd/kadran/domain.go cmd/kadran/domain_test.go
git commit -m "update domain check wait"
```

---

### Görev 10: Mutasyon betiği

**Dosyalar:**
- Değiştir: `scripts/mutate-domaincheck.sh`

**Arayüzler:** Yok (test altyapısı).

- [ ] **Adım 1: Dosya ve test listesini güncelle**

```bash
JUDGE=internal/domaincheck/judge.go
PROV=internal/domaincheck/providers.go
ZONE=internal/domaincheck/zone.go
CF=internal/domaincheck/cloudflare.go
AUTH=internal/domaincheck/authoritative.go
RDAP=internal/domaincheck/rdap.go
GUIDE=cmd/kadran/domainguide.go
TESTS='TestDomainCheck|TestCheck|TestALookupError|TestWrappedNotFound|StopsOnWrongDNS|SkipFlag|MatchingDNS|RemovingTheDomain|LocalhostDomain|DNSWarning|SSHAlias|ParseSSHHostname|Provider|AWSPattern|CleanName|CommandFor|FindZone|GuessRecordName|Cloudflare|AskAuthoritative|DefaultExchange|CheckerRules|CheckerBudget|DNSHost|OnlyTheCache|ProxiedCloudflare|StaysOffline|Registrar|WaitsUntil|WaitGivesUp'
FILES=("$DC" "$CLI" "$APP" "$UPD" "$DOM" "$JUDGE" "$PROV" "$ZONE" "$CF" "$AUTH" "$RDAP" "$GUIDE")
```

- [ ] **Adım 2: Kayan iki mutantı düzelt** (kod judge.go'ya taşındı):

```bash
mutate_in "$JUDGE" "AAAA denetlenmiyor" \
    "s=s.replace('\tcheckFamily(rep, \"AAAA\", \"IPv6\", aaaa, family(rep.Server, false))\n','',1)"

mutate_in "$DC" "tek aile yoksa kayıt yok sayılıyor" \
    "s=s.replace('judge(&rep, a, aaaa, aNotFound && aaaaNotFound, serverHost)','judge(&rep, a, aaaa, aNotFound || aaaaNotFound, serverHost)',1)"
```

"yanlış kayıt yalnızca uyarıyor" ve "bilinmeyen aile durduruyor" mutantlarının dosyası `$DC` → `$JUDGE`. "başka sunucu ölçüldüğü söylenmiyor": `fs.Arg(1)) == domaincheck.Stop {` → yeni satır `if rep.Verdict == domaincheck.Stop {` ; mutant `s.replace('if rep.Verdict == domaincheck.Stop {','if rep.Verdict == domaincheck.Verdict(99) {',1)`. "uyarı stdout'a yazılıyor" ve "boş alan adı da denetleniyor" `$CLI`'da aynen kalır.

- [ ] **Adım 3: Yeni mutantlar** (`echo "== CLI bağlantısı =="` satırından önce):

```bash
echo "== K-151 kuralları =="

mutate_in "$JUDGE" "karışık küme geçiyor" \
    "s=s.replace('if stray := without(records, server); len(stray) > 0 {','if stray := without(records, server); len(stray) > 99 {',1)"

mutate_in "$DC" "yetkili cevap yok sayılıyor" \
    "s=s.replace('if rep.Auth.Answered && rep.Auth.Agree {','if false {',1)"

mutate_in "$DC" "NS tutarsızlığı uyarmıyor" \
    "s=s.replace('if rep.Auth.Answered && rep.Verdict < Stop {','if false {',1)"

mutate_in "$JUDGE" "CNAME izlenmiyor" \
    "s=s.replace('if len(addrs) == 0 && rep.Auth.CNAME != \"\" {','if false {',1)"

mutate_in "$JUDGE" "Cloudflare metni NS'ye bakmıyor" \
    "s=s.replace('if rep.HasProvider && rep.Provider.Name == providerCloudflare {','if true {',1)"

mutate_in "$CF" "Cloudflare aralığı tanınmıyor" \
    "s=s.replace('\t\tif p.Contains(a) {','\t\tif false && p.Contains(a) {',1)"

mutate_in "$AUTH" "AA bayrağı aranmıyor" \
    "s=s.replace(' || h.ID != id || !h.Authoritative {',' || h.ID != id {',1)"

mutate_in "$AUTH" "sorgu kimliği aranmıyor" \
    "s=s.replace(' || h.ID != id || !h.Authoritative {',' || !h.Authoritative {',1)"

mutate_in "$AUTH" "NS'ler tutarsızken Agree kalıyor" \
    "s=s.replace('\t\t\tout.Agree = false\n','',1)"

mutate_in "$DC" "süre bütçesi uygulanmıyor" \
    "s=s.replace('ctx, cancel := context.WithTimeout(ctx, budget)','ctx, cancel := context.WithCancel(ctx); _ = budget',1)"

mutate_in "$PROV" "son ek etiket sınırını aşıyor" \
    "s=s.replace('strings.HasSuffix(ns, \".\"+suf)','strings.HasSuffix(ns, suf)',1)"

mutate_in "$PROV" "NS adı temizlenmiyor" \
    "s=s.replace(\"if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {\",'if true {',1)"

mutate_in "$ZONE" "bölge genel son eke tırmanıyor" \
    "s=s.replace('\t\tif name == top {\n\t\t\treturn Zone{}, false\n\t\t}\n','',1)"

mutate_in "$RDAP" "https olmayan yönlendirme izleniyor" \
    "s=s.replace('if req.URL.Scheme != \"https\" {','if false {',1)"

mutate_in "$RDAP" "RDAP gövdesi sınırsız" \
    "s=s.replace('io.LimitReader(resp.Body, rdapMaxBody)','resp.Body',1)"

mutate_in "$GUIDE" "proxy'li kayıtta kayıt ekleme öneriliyor" \
    "s=s.replace('\tif rep.Proxied {\n','\tif false {\n',1)"

mutate_in "$DOM" "-wait beklemiyor" \
    "s=s.replace('\t\tif rep.Verdict != domaincheck.Stop {\n\t\t\treturn true\n\t\t}\n','\t\treturn true\n',1)"
```

Not: "NS adı temizlenmiyor" mutantındaki ifade tek tırnak içeriyor; betik dosyası Write ile düzenlendiği için sorun değil. Her mutantın `REPLACE n KEZ ESLESTI` vermediğini Adım 4'te gör; veren mutantın iğnesi kodla birebir eşleşecek şekilde düzeltilir.

- [ ] **Adım 4: Koştur (WSL, tek başına)**

Hafızadaki yol: WSL Ubuntu-24.04, python kısayolu, komut betik dosyasında.
Çalıştır (scratchpad'e yazılan `mut-dc.sh` içinde): `cd /mnt/c/Users/erkanrzgc/Desktop/kadran && mkdir -p /tmp/pyshim && ln -sf /usr/bin/python3 /tmp/pyshim/python && PATH=/tmp/pyshim:/usr/local/go/bin:$PATH bash scripts/mutate-domaincheck.sh`; `wsl.exe -d Ubuntu-24.04 -- bash /mnt/c/.../mut-dc.sh`.
Beklenen: `Bütün mutasyonlar yakalandı.` Yakalanmayan mutant için testi güçlendir (mutantı geçiren durumu sınayan satır ekle), betiği değil.

- [ ] **Adım 5: Commit**

```bash
git add scripts/mutate-domaincheck.sh
git commit -m "update domaincheck mutation script"
```

---

### Görev 11: Belgeler

**Dosyalar:**
- Oluştur: `docs/guides/domains.md`
- Değiştir: `README.md`, `CHANGELOG.md`, `docs/decisions.md`
- Test: `internal/domaincheck/docs_test.go`

**Arayüzler:** Yok.

- [ ] **Adım 1: Başarısız tutarlılık testi** — `internal/domaincheck/docs_test.go`:

```go
package domaincheck

import (
	"os"
	"strings"
	"testing"
)

// Rehber, koddaki her firmayı adıyla anmalı: firma eklenip rehber
// unutulursa kullanıcı "Guide" bağlantısını görür ama rehberde bulamaz.
func TestDomainsGuideListsEveryProvider(t *testing.T) {
	b, err := os.ReadFile("../../docs/guides/domains.md")
	if err != nil {
		t.Fatalf("rehber okunamadı: %v", err)
	}
	doc := string(b)
	for _, p := range providers {
		if !strings.Contains(doc, p.Name) {
			t.Errorf("docs/guides/domains.md %q firmasını anmıyor", p.Name)
		}
	}
}
```

Çalıştır: `go test ./internal/domaincheck/ -run DomainsGuide -count=1` → FAIL (dosya yok).

- [ ] **Adım 2: `docs/guides/domains.md`'yi yaz** (İngilizce; README'nin dili)

Bölümler, sırayla:
1. **Point a domain at your server** — üç adım: (1) DNS'in olduğu firmada `A` kaydı (`@` ya da alt ad → sunucunun IPv4'ü), aynı ad için başka A/AAAA/ALIAS/CNAME silinir; (2) `kadran domain check -wait 10m <domain> kadran-client@<server>`; (3) `kadran app create/update -domain <domain> …`. Sertifikanın Caddy tarafından kendiliğinden alındığı.
2. **Where is my DNS?** — domainin alındığı yer ile DNS'in olduğu yer farklı olabilir; `kadran domain check` ikisini de gösterir (RDAP'ın bayiyi değil akredite firmayı gösterdiği, Vercel → Name.com örneği). Nameserver değiştirmenin ancak DNS'i başka yerde yönetmek istenirse gerektiği.
3. **What Kadran checks** — yetkili sunucuya sorma, önbellek eski uyarısı, karışık kayıt kümesi, CNAME, Cloudflare, 10 sn bütçe; `-skip-dns-check` ne zaman.
4. **Common traps** — park kaydı (GoDaddy "Parked", Namecheap URL Redirect, Porkbun/Hostinger park sayfaları), eski AAAA kaydı, joker kayıt (Vercel `*`), Cloudflare turuncu bulut ve SSL modu, TTL.
5. **Providers** — dört grup (Domain registrars / Hosting / Cloud and DNS services / Turkey) tabloları: `| Provider | Nameservers end with | Where to add the record | Official guide |`. Her satır `providers.go`'daki `Name`, son ekler, `Panel`, `Guide` ile AYNI. Tanınmayan firma için genel tarif.

- [ ] **Adım 3: README** — "Point the domain's DNS at the server first, so that Let's Encrypt can reach it." cümlesinin sonuna: ` [Domain setup guide](docs/guides/domains.md) covers ~50 DNS hosts step by step.` ve `app create`/`app update` paragrafındaki `kadran domain check` cümlesine `-wait 10m` örneği.

- [ ] **Adım 4: CHANGELOG** — `## Unreleased` altında (yoksa en üste aç) `### Added`:

```markdown
- DNS guidance (K-151): when a domain does not point at the server, `app create/update -domain`
  and `kadran domain check` name the DNS host (~50 providers), print the exact record to add and
  the records to delete, the place in that provider's panel and its official guide.
- `kadran domain check` asks the DNS host's own nameservers, so a freshly changed record is
  recognised before caches expire; `-wait 10m` waits until the record points at the server.
- `kadran domain check` shows the domain's accredited registrar (RDAP).
```

ve `### Changed`:

```markdown
- A record set that contains the server AND other addresses now stops the DNS precheck (it used
  to pass): part of the visitors and of Let's Encrypt's validation reached the other addresses.
```

- [ ] **Adım 5: K-151** — `docs/decisions.md` sonuna (Türkçe, mevcut K kayıtlarının biçiminde): başlık `## K-151 — DNS yönlendirme: firma tanıma, yetkili sorgu, kayıt firması (2026-10-15)`; bölümler: Bağlam (kullanıcı isteği, K-128'in eksikleri), Karar (yönlendir+doğrula, API yok; ~50 firma; yetkili cevap önceliği ve AA şartı; karışık küme durur; Cloudflare NS'ye göre; 10 sn bütçe; RDAP yalnız `domain check`, rdap.org yok), Ölçülenler (15 Ekim: NS adları DNS'te çözüldü; erkanrzgc.dev RDAP = Name.com; `.tr` önyüklemede yok; Görev 12'nin canlı sonuçları), Durum.

- [ ] **Adım 6: Testler ve commit**

Çalıştır: `go test ./... -count=1` → PASS.

```bash
git add docs/guides/domains.md README.md CHANGELOG.md docs/decisions.md internal/domaincheck/docs_test.go
git commit -m "update domain setup guide"
```

---

### Görev 12: Canlı salt-okur doğrulama ve son denetim

**Dosyalar:** Yalnız `docs/decisions.md` (K-151 "Ölçülenler"e sonuçlar).

- [ ] **Adım 1: Tam test, vet, lint**

Çalıştır:
```bash
go test ./... -count=1 -race
go vet ./...
golangci-lint run ./...
bash scripts/check-exec-surface.sh
```
Beklenen: hepsi temiz. Ayrıcalıklı yüzey DEĞİŞMEMELİ (bu iş yalnız CLI); değişirse dur ve sebebini bul.

- [ ] **Adım 2: Gerçek DNS'e karşı (salt-okur, sunucuya yazmaz)**

```bash
go build -o "$SP/kadran-dns.exe" ./cmd/kadran
"$SP/kadran-dns.exe" domain check erkanrzgc.dev kadran-client@<Hetzner IP'si, hafızadan>
"$SP/kadran-dns.exe" domain check kadran.erkanrzgc.dev kadran-client@<Hetzner IP'si>
"$SP/kadran-dns.exe" domain check cloudflare.com kadran-client@<Hetzner IP'si>
```

Beklenen:
- `erkanrzgc.dev`: `✗ DNS`, `DNS host  Vercel (ns1.vercel-dns.com, ns2.vercel-dns.com)`, `A     @  <sunucu>`, `vercel dns add erkanrzgc.dev '@' A <sunucu>`, Vercel notu, `registrar  Name.com, Inc. …`, `the DNS is at Vercel`.
- `kadran.erkanrzgc.dev`: `✓ DNS`, sertifika ✓, kayıt firması satırı.
- `cloudflare.com`: `proxied (orange cloud)` ya da Cloudflare NS ile uyumlu metin.

Hepsinin süresi 10 sn bütçenin altında olmalı (`time` ile ölç). Çıktılar `$SP/dns-canli.txt`'ye; IP'ler K-151'e YAZILMAZ (`<sunucu>` olarak geçer).

- [ ] **Adım 3: Masaüstü dokunulmadı mı**

Çalıştır: `git diff main --stat -- desktop/` → boş.

- [ ] **Adım 4: K-151'e ölçüleri yaz ve commit**

```bash
git add docs/decisions.md
git commit -m "update k-151 measurements"
```

- [ ] **Adım 5: Push ve PR için kullanıcıya sor**

Dal `dns-yonlendirme`. Kullanıcı onaylarsa push + PR (başlık `update dns guidance`; gövde: ne değişti, test kanıtı, mutasyon sonucu, canlı salt-okur çıktı özeti). CI 13/13 yeşil olunca birleştirme için ayrıca onay.
