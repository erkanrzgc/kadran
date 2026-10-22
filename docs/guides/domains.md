# Domain setup guide

How to point a domain at a Kadran server, how to find out where its DNS is managed, and what
`kadran domain check` looks at. The reasoning is recorded in K-151 in
[`docs/decisions.md`](../decisions.md).

Addresses below use documentation ranges (`203.0.113.10` stands for your server's public IPv4
address). Kadran never changes DNS for you: it tells you what to change and checks the result.

## Point a domain at your server

1. **Add an `A` record at the place where the domain's DNS lives.** The name is `@` (or the
   domain itself) for a root domain, or the subdomain label for a subdomain. The value is the
   server's public IPv4 address:

   ```text
   A    @      203.0.113.10        # site.example.com itself
   A    app    203.0.113.10        # app.site.example.com
   ```

   Delete every other `A`, `AAAA`, `ALIAS` and `CNAME` record for the same name first. A
   `CNAME` cannot coexist with other records, and a second address means part of your visitors
   (and part of Let's Encrypt's validation requests) reach the other machine.

2. **Wait until the record is visible and check it:**

   ```bash
   kadran domain check -wait 10m site.example.com kadran-client@your-server
   ```

   `-wait 10m` re-checks every 15 seconds and prints a line only when the reason changes. It
   stops waiting as soon as the DNS check no longer stops (everything is fine, or only warnings
   remain), then runs the port, HTTP and certificate checks. If the time runs out it still runs
   those checks, prints the last DNS report with them and exits with status 1 (the DNS check
   still fails). Ctrl-C while waiting prints
   `interrupted while waiting for DNS` and exits with status 1. A negative `-wait` is a usage error.

3. **Create or update the app:**

   ```bash
   kadran app create -repo github.com/you/site -domain site.example.com -port 8080 site kadran-client@your-server
   ```

   `app create` and `app update` run the same DNS check before they contact the server. The
   certificate is requested by Caddy on its own once the name points at the server and ports
   80 and 443 are reachable; there is nothing to upload or configure.

## Where is my DNS?

The place you bought the domain and the place that answers DNS for it are often different.
Buying a domain from a reseller and leaving its default nameservers is fine, but then the
records are edited at the reseller's DNS panel, not at the registry or at the accredited
registrar behind it.

`kadran domain check` can show both, but not always:

- `DNS host  <name> (<nameservers>)` appears only when the DNS check stops, that is when the
  record is wrong. It is the company whose nameservers answer for the domain,
  recognised by the nameserver names (see [Providers](#providers)). This is where the record
  goes. For a healthy domain the line is not printed. When Kadran does not recognise the nameservers it prints them as they are, with a
  generic instruction and a link to this guide.
- `registrar` is the domain's **accredited registrar** from RDAP, printed last on every check
  when RDAP knows the domain's extension (there is none for `.tr`). RDAP names the
  accredited registrar, not the reseller you paid. For example a domain bought through Vercel is
  registered through Name.com, so the line says Name.com while the DNS host stays Vercel. When
  the DNS host is recognised the line adds `the DNS is at <name>: change the record there`.

You only need to change nameservers if you want to manage DNS somewhere else (for example to
move it to Cloudflare). To just point the domain at your server, edit the record where the DNS
already is.

## What Kadran checks

- **Authoritative first.** Kadran finds the zone's nameservers and asks them directly, so a
  record you changed a minute ago is recognised before resolver caches expire. Only
  authoritative answers count. Two cases differ:
  - The nameservers agree and the answer is right, but this machine's resolver still returns an
    old record from its cache: the check warns instead of stopping. The warning says that your
    DNS host already answers the new record while this machine still sees the old one, and
    suggests `kadran domain check -wait 10m`.
  - The nameservers do not agree with each other yet: Kadran falls back to what the local
    resolver returns and judges that. If those records are wrong the check still stops; if they
    are fine it passes with a warning that the change is still spreading inside the DNS host.
- **Mixed record sets stop the check.** `the A records are <server> (server) and <other>:
  visitors and Let's Encrypt reach the other addresses part of the time`. Delete the other
  addresses.
- **CNAME.** `site.example.com is a CNAME to <target>; replace it with an A record`. The guide
  then says to delete exactly that CNAME and add the `A` record; the CNAME target's addresses
  are not rows in your zone, so there is nothing else to delete. If the CNAME's target cannot
  be looked up, the check only warns (`the CNAME target <target> did not resolve`); it never
  stops on a failed lookup.
- **Cloudflare.** The text depends on the nameservers. With Cloudflare's own nameservers a
  Cloudflare address means the record is proxied (orange cloud): set it to DNS only (grey
  cloud). With other nameservers it means a page hosted on Cloudflare, such as a registrar's
  parking page: replace that record. When the nameservers are unknown the message names both
  possibilities.
- **Delete list.** The `Delete these records for "<name>": A/ALIAS → …` line lists the
  address, ALIAS and CNAME answers the name serves today. If you find no record of that name in
  your panel, a wildcard (`*`) record is answering for it: change or delete that one.
  The generic `Add an A record pointing the name at the server's public IPv4 address.` line
  appears only when there is no more specific record to add or delete.
- **Time limit.** The nameserver lookup has a budget of about 10 seconds. If it runs out, the
  `DNS host` line is left out rather than guessed. Looking up a CNAME's target uses the local
  resolver and is outside that budget.
- **Private or unknown server addresses** (a Tailscale address, a local target, a failed
  query) produce a warning, never a stop.

When a stop is wrong for your setup, pass `-skip-dns-check` to `app create` / `app update`.
It is meant for deliberate cases such as a proxy you keep in front of the server, for example
Cloudflare with SSL/TLS mode Full (strict).

## www and other names

One app can answer to more than one name. Every extra name sends visitors to the app's own
domain with a permanent redirect (HTTP 308, so the method and body are kept) to
`https://<domain>`, keeping the path and the query: `https://www.example.com/a?b=1` goes to
`https://example.com/a?b=1`. The redirect target is always the app's domain; it cannot be set to
anything else. The reasoning is recorded in K-152 in [`docs/decisions.md`](../decisions.md).

**The automatic www rule.** `app create` turns www on by default, and the partner name is derived
from the domain:

- a registrable domain (`example.com`, `example.co.uk`) gets `www.example.com` as its partner;
- a domain that is `www.` plus a registrable domain gets the bare domain as its partner
  (with `www.example.com` as the domain, `example.com` redirects to it);
- any other subdomain (`app.example.com`) has no partner: the flags are accepted but nothing is
  routed, no `Redirects` line mentions a www name and no DNS check runs for one.

Pass `-no-www` to `app create` to turn it off. On an existing app, `app update -www` turns it on
and `app update -no-www` turns it off (the two flags cannot be combined). Apps created before
this feature keep www **off** after the upgrade; turning it on is a deliberate
`app update -www`. The flag is stored, not the name, so `app update -domain` moves the partner
along with the domain.

**Extra names.** `-redirect <name>` adds a name and can be repeated (on `app create` and
`app update`); `-redirect-rm <name>` removes one (on `app update`). An app can have at most 8
explicit names, they must be valid lowercase domain names, and an app without a domain cannot
have any.

```bash
kadran app create -repo github.com/you/site -domain example.com -redirect old.example.net site kadran-client@your-server
kadran app update -redirect-rm old.example.net site kadran-client@your-server
kadran app update -no-www site kadran-client@your-server
```

**Collisions.** A name (explicit or derived) cannot equal another app's domain, explicit name or
derived partner. The server rejects the request and names the app, for example
`name already in use: "www.example.com" is already used by app "site"`. This includes creating an
app whose domain is the derived www partner of an existing app.

**DNS for the extra names.** Each name needs its own record that points at the server, like the
main domain: either an `A` record or a `CNAME` to the domain.

```text
A      www      203.0.113.10
CNAME  www      example.com.      # alternative: let www follow the root domain
```

`app create` checks every redirect name (the www partner included) after the server has
accepted the request, in parallel; `app update` does so only when `-domain`, `-www`, `-no-www`
or `-redirect` is given. These checks only warn and never change the exit status: a wrong
record prints

```text
kadran: warning: www.example.com does not point at the server yet; it is redirected once it does and gets its own certificate
```

followed by the same DNS host and record guidance as the main domain. The name starts working by
itself once its DNS is right. `-skip-dns-check` skips these checks as well as the main one.

**Certificates.** Caddy requests a separate certificate for each name, the same way as for the
main domain; a name whose DNS does not point at the server cannot get one. How soon it arrives
after the DNS is fixed, and whether a www name without DNS leaves the root domain's certificate
unaffected, have not been measured on a live server yet (K-152 lists certificates as open).

**Seeing the result.** `app show` and the result of `app create` / `app update` print one line
with every name and the target:

```text
  Redirects: www.example.com, old.example.net → example.com (308)
```

The line is absent when the app has no extra names. If the server cannot apply a name,
`app update` prints `redirect <name> NOT applied (<reason>)`, for example
`collides with the domain of app "site"` or `already redirects to "example.net"`, and the other
routes keep working; `kadrand` also writes such names to its journal (at startup, on deploy, on
heal and from the proxy watcher). `app update -domain` reports the names it moved, and `-www` on
a subdomain says `<domain> is a subdomain and has no www partner`.

If the app is not routed at the moment (its current release has no running replica), removing
a name takes effect at once (`redirects removed: … — applied even though the app is not
routed`), while an added name is saved and applied once the app is routed again
(`redirects SAVED (…) but NOT applied … kadran deploy <app>`).

Against a server that predates this feature, a request that asks for www or a redirect name
prints a warning and no `Redirects` line:

```text
kadran: warning: this server does not support redirects (www, -redirect); upgrade kadrand
```

An `app update` that changes only names (`-www`, `-no-www`, `-redirect`, `-redirect-rm`) fails
with the same text instead, because the old server sees an empty update.

## Common traps

- **Parking records.** New domains often ship with a parking record that answers for the root
  name: GoDaddy's default `Parked` A record, Namecheap's URL Redirect and parking records,
  Porkbun's default ALIAS and CNAME parking records, and Hostinger's default parking A and
  CNAME records. Delete or replace them; the DNS check prints a hint for each of these hosts.
- **An old `AAAA` record.** Browsers and Let's Encrypt prefer IPv6. A leftover `AAAA` record
  pointing at the old host breaks the site and the validation even though the `A` record is
  right. Delete it, or make it the server's IPv6 address.
- **Wildcard records.** A `*` record answers for names that have no record of their own, so you
  can add the right `A` record, see nothing to delete in the panel, and still land on the wrong
  server. Vercel's default `*` ALIAS is the typical case. Add the explicit `A` record for the
  name; it takes precedence over the wildcard.
- **Cloudflare orange cloud and SSL mode.** A proxied record hides the server's address, so the
  check stops. Either switch the record to DNS only (grey cloud), or keep the proxy, set SSL/TLS
  mode to Full (strict) and pass `-skip-dns-check`.
- **TTL.** After you change a record, resolvers may cache the old answer for as long as its old
  TTL. Lower the TTL before a planned move. Kadran asks the DNS host's own nameservers, so it
  often sees the new record earlier than your machine does; `-wait` covers the gap.
- **A domain assigned to another project.** Some hosts keep their own record for domains that
  are attached to one of their projects (Vercel: remove the domain in Project → Settings →
  Domains too).

## Providers

Kadran recognises the DNS host from the domain's nameservers. Every nameserver must belong to
the same company; a domain split between two companies is not recognised and gets the generic
instruction. Suffixes match on a name boundary: `he.net` matches `ns1.he.net`, not `ns.the.net`.
The table is the one in the code (`internal/domaincheck/providers.go`); a test fails if a
provider is missing here.

For a provider that is not listed, open the control panel of the company shown on the
`DNS host` line (or the one that hosts the nameservers) and look for "DNS", "Zone editor" or
"DNS records". Add an `A` record for the name pointing at the server's IPv4 address, and delete
the records listed above.

### Domain registrars

| Provider | Nameservers end with | Where to add the record | Official guide |
|---|---|---|---|
| Vercel | `vercel-dns.com` | Dashboard → Domains → the domain → DNS Records → Add | <https://vercel.com/docs/domains/managing-dns-records> |
| Cloudflare | `cloudflare.com` | DNS → Records → Add record; Proxy status: DNS only (grey cloud) | <https://developers.cloudflare.com/dns/manage-dns-records/how-to/create-dns-records/> |
| GoDaddy | `domaincontrol.com` | My Products → the domain → DNS → Add New Record | <https://www.godaddy.com/help/add-an-a-record-19238> |
| Namecheap | `registrar-servers.com` | Domain List → Manage → Advanced DNS → Add New Record | <https://www.namecheap.com/support/knowledgebase/article.aspx/319/2237/how-can-i-set-up-an-a-address-record-for-my-domain/> |
| Network Solutions | `worldnic.com` | Account Manager → My Domain Names → Manage → Advanced DNS → A records | <https://www.networksolutions.com/help/article/manage-dns-adns-records> |
| Porkbun | `porkbun.com` | Domain Management → the domain → DNS → add a record | <https://kb.porkbun.com/article/68-how-to-edit-dns-records> |
| Squarespace | `squarespacedns.com` | Domains → the domain → DNS → Custom records | <https://support.squarespace.com/hc/en-us/articles/360002101888> |
| Google Cloud DNS / Google Domains | `googledomains.com` | Google Cloud: Cloud DNS → the zone → Add standard | <https://docs.cloud.google.com/dns/docs/records> |
| Name.com | `name.com` | My Domains → the domain → Manage DNS Records → Add Record | <https://www.name.com/support/articles/205188538-pointing-your-domain-to-hosting-with-a-records> |
| Hover | `hover.com` | the domain → DNS → Add a record | <https://help.hover.com/hc/en-us/articles/217282457-Managing-DNS-records> |
| Dynadot | `dyna-ns.net` | My Domains → Manage Domains → the domain → DNS Settings | <https://www.dynadot.com/help/question/set-DNS> |
| NameSilo | `dnsowl.com` | Domain Manager → the domain → Manage DNS | <https://www.namesilo.com/support/v2/articles/domain-manager/dns-manager> |
| Spaceship | `spaceship.net` | Domain Manager → the domain → Advanced DNS → Add record | <https://www.spaceship.com/knowledgebase/dns-records-types/> |
| Gandi | `gandi.net` | Domain → DNS Records → Add record | <https://docs.gandi.net/en/domain_names/common_operations/dns_records.html> |
| OVHcloud | `ovh.net` | Web Cloud → Domain names → the domain → DNS zone → Add an entry | <https://help.ovhcloud.com/csm/en-dns-edit-dns-zone> |
| IONOS | `ui-dns.com`, `ui-dns.de`, `ui-dns.org`, `ui-dns.biz` | Domains & SSL → the domain → DNS → Add record | <https://www.ionos.com/help/index.php?id=408> |
| Strato | `rzone.de` | Domains → Manage domain → DNS → A record | <https://www.strato.de/faq/domains/wie-kann-ich-bei-strato-meine-dns-eintraege-verwalten/> |
| one.com | `one.com` | DNS settings → DNS records → Create new record | <https://www.one.com/en/support/guide/manage-your-dns-settings> |
| Wix | `wixdns.net` | Domains → the domain → Manage DNS Records → A (Host) | <https://support.wix.com/en/article/adding-or-updating-a-records-in-your-wix-account> |
| Reg.ru | `reg.ru` | Domains → the domain → DNS servers and zone management → Add record | <https://www.reg.ru/support/dns> |
| Alibaba Cloud | `hichina.com`, `alidns.com` | Alibaba Cloud DNS → the domain → Add record | <https://www.alibabacloud.com/help/en/dns/> |
| Tencent DNSPod | `dnspod.net` | DNSPod console → the domain → Add record | <https://docs.dnspod.com/dns/help-change-domain-ns/> |

### Hosting

| Provider | Nameservers end with | Where to add the record | Official guide |
|---|---|---|---|
| Hostinger | `dns-parking.com`, `main-hosting.eu` | hPanel → Domains → the domain → DNS / Nameservers → DNS records | <https://support.hostinger.com/en/articles/1583249-how-to-manage-dns-records-at-hostinger> |
| Bluehost | `bluehost.com` | Domains → the domain → DNS → A records → Add | <https://www.bluehost.com/help/article/dns-management-add-edit-or-delete-dns-entries> |
| Contabo | `contabo.net` | Customer Control Panel → DNS Zone Management → the zone → Add Record | <https://contabo.com/blog/dns-feature-contabo-customer-control-panel/> |
| Loopia | `loopia.se` | Customer Zone → the domain → DNS editor → Add a new record | <https://support.loopia.com/wiki/a-and-cname-posts/> |
| DreamHost | `dreamhost.com` | Websites → Manage Websites → DNS → Add Record | <https://help.dreamhost.com/hc/en-us/articles/360035516812-Adding-custom-DNS-records> |
| SiteGround | `siteground.net` | Site Tools → Domain → DNS Zone Editor | <https://world.siteground.com/kb/manage-dns-records/> |
| InMotion Hosting | `inmotionhosting.com` | cPanel → Zone Editor | <https://www.inmotionhosting.com/support/edu/cpanel/advanced-dns-editor-cpanel/> |
| A2 Hosting | `a2hosting.com` | cPanel → Zone Editor | <https://www.a2hosting.com/kb/> |
| Namecheap Hosting | `namecheaphosting.com` | cPanel → Zone Editor | <https://www.namecheap.com/support/knowledgebase/article.aspx/9434/10/using-default-nameservers-vs-hosting-nameservers/> |

### Cloud and DNS services

| Provider | Nameservers end with | Where to add the record | Official guide |
|---|---|---|---|
| AWS Route 53 | names like `ns-123.awsdns-45.com` (also `.net`, `.org`, `.co.uk`) | Route 53 → Hosted zones → the zone → Create record | <https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-creating.html> |
| Azure DNS | `azure-dns.com`, `azure-dns.net`, `azure-dns.org`, `azure-dns.info` | DNS zones → the zone → + Record set | <https://learn.microsoft.com/en-us/azure/dns/dns-operations-recordsets-portal> |
| DigitalOcean | `digitalocean.com` | Networking → Domains → the domain → Create new record | <https://docs.digitalocean.com/products/networking/dns/how-to/manage-records/> |
| Linode / Akamai | `linode.com` | Domains → the domain → Add an A/AAAA Record | <https://techdocs.akamai.com/cloud-computing/docs/manage-dns-records> |
| Vultr | `vultr.com` | Network → DNS → the domain → Add record | <https://docs.vultr.com/products/network/dns> |
| Hetzner | `ns.hetzner.com`, `ns.hetzner.de`, `first-ns.de`, `second-ns.de`, `second-ns.com` | DNS Console → the zone → Add record | <https://docs.hetzner.com/networking/dns/overview/> |
| Netlify DNS / other NS1 customers | `nsone.net` | Netlify: Domains → the domain → DNS records → Add new record; other NS1 customers: the DNS panel of whoever manages this zone | <https://docs.netlify.com/manage/domains/manage-domains/manage-dns-records/> |
| Bunny DNS | `bunny.net` | DNS → the zone → Add record | <https://docs.bunny.net/docs/dns-overview> |
| ClouDNS | `cloudns.net` | DNS Hosting → the zone → Add new record | <https://www.cloudns.net/wiki/article/10/> |
| DNSimple | `dnsimple.com`, `dnsimple-edge.com`, `dnsimple-edge.net`, `dnsimple-edge.io`, `dnsimple-edge.org` | Domains → the domain → DNS → Manage → Add record | <https://support.dnsimple.com/articles/manage-a-record/> |
| DNS Made Easy | `dnsmadeeasy.com` | DNS → Managed DNS → the domain → A records | <https://support.dnsmadeeasy.com/hc/articles/34327020373659> |
| easyDNS | `easydns.com`, `easydns.net`, `easydns.org`, `easydns.info` | DNS → Edit → A records | <https://kb.easydns.com/> |
| Hurricane Electric | `he.net` | dns.he.net → the zone → New A | <https://dns.he.net/> |
| FreeDNS (afraid.org) | `afraid.org` | Subdomains → Add | <https://freedns.afraid.org/faq/> |

### Turkey

| Provider | Nameservers end with | Where to add the record | Official guide |
|---|---|---|---|
| Natro | `natrohost.com` | Domain list → the domain → DNS management → Add record | <https://www.natro.com/> |
| IsimTescil | `isimtescil.net` | My domains → the domain → DNS management | <https://www.isimtescil.net/destek> |
| Turhost | `turhost.com` | Domains → the domain → DNS management | <https://www.turhost.com/> |
| Guzel Hosting | `guzelhosting.com` | Client area → Domains → the domain → DNS management | <https://www.guzel.net.tr/> |
| Turkticaret.net | `turkticaret.net` | Domains → the domain → DNS management | <https://www.turkticaret.net/> |
| Hosting.com.tr | `hosting.com.tr` | Client area → Domains → the domain → DNS management | <https://www.hosting.com.tr> |
| Markum | `markum.net` | Client area → Domains → the domain → DNS management | <https://www.markum.net> |
