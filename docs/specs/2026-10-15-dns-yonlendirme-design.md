# DNS yönlendirme — domaini nereden alırsan al, sunucuya bağla

**Tarih:** 15 Ekim 2026
**Durum:** TASARIM. Kullanıcı sohbette onayladı ("Uygun, spec'e dök"); bu belgenin onayı bekleniyor.
**İlgili:** K-128 (DNS önkontrolü, `domain check`), K-151 (bu iş için açılacak karar kaydı).
**Sıra:** A (bu belge) → B (yönlendirme adları / www) → C (erkanrzgc.dev'in Hetzner'a taşınması).

## Amaç

Kullanıcı domaini Vercel'den, Cloudflare'den, Namecheap'ten ya da bir Türk firmasından
(Natro, İsimtescil…) almış olabilir. Kadran ona **DNS'in hangi firmada olduğunu**, **tam olarak
hangi kaydı ekleyeceğini** ve **o firmada bunu nerede yapacağını** söylemeli, sonra kaydın
gerçekten sunucuyu gösterdiğini doğrulamalı.

Başarı ölçütleri:

1. Yanlış ya da eksik DNS'te `kadran app create/update -domain` ve `kadran domain check`,
   DNS barındırıcısını adıyla, eklenecek kaydı (`A @ 46.225.95.35` biçiminde) ve firmaya özel
   adımı basar. Tanınmayan firmada genel tarif basar.
2. Cloudflare'in proxy'li (turuncu bulut) kaydı "başka yeri gösteriyor" diye değil, sebebi ve
   çözümüyle raporlanır.
3. Kayıt DNS firmasında doğru ama yerel çözümleyicinin önbelleği eskiyse komut **durmaz**,
   uyarır; `-skip-dns-check` gerekmez.
4. `kadran domain check -wait <süre>` kayıt doğru olana kadar bekler, sonra port, HTTP ve
   sertifika denetimlerine geçer.
5. Hiçbir firmanın API anahtarı istenmez (kullanıcı kararı: "yönlendir + doğrula").

## Kapsam dışı

- DNS kaydını firmanın API'siyle otomatik eklemek (kullanıcı reddetti).
- `www` ve diğer yönlendirme adları (B projesi).
- Masaüstü arayüzü: masaüstü bugün DNS önkontrolü yapmıyor; aynı rehberin oraya taşınması
  ayrı iş.
- Uluslararası alan adları (IDN): bugünkü doğrulayıcı yalnız ASCII kabul ediyor, değişmiyor.

## Kullanıcının gördüğü

Yanlış DNS, firma tanınıyor (`app update -domain` hatası ve `domain check` aynı bloğu basar):

```
✗ DNS       the A record points at 64.29.17.65, 216.198.79.65, the server is 46.225.95.35
  DNS host  Vercel (ns1.vercel-dns.com, ns2.vercel-dns.com)
  Set this record and delete other A, AAAA, ALIAS or CNAME records for the same name:
      A   @   46.225.95.35
  At Vercel: vercel dns add erkanrzgc.dev @ A 46.225.95.35
             or Dashboard → Domains → erkanrzgc.dev → DNS Records → Add
  If the domain is assigned to a Vercel project, remove it from the project as well.
```

Kayıt firmada doğru, önbellek eski:

```
! DNS       your DNS host (Vercel) already answers 46.225.95.35; this machine still sees
            64.29.17.65 (cached, up to 600 s). Continuing.
```

Cloudflare proxy'si:

```
✗ DNS       the A record points at Cloudflare (104.21.x.x): the record is proxied (orange
            cloud) or the site is hosted on Cloudflare
  At Cloudflare: DNS → Records → edit the record → Proxy status: DNS only (grey cloud)
  Keeping the proxy is possible: SSL/TLS mode Full (strict), then -skip-dns-check.
```

Tanınmayan firma: aynı blok, "At Vercel" satırı yerine genel tarif ("your DNS host's control
panel → DNS / Zone editor"), NS adları gösterilir.

## Tasarım

Tüm mantık `internal/domaincheck` içinde, küçük dosyalara bölünmüş. CLI (`cmd/kadran`) yalnız
basar. Sunucu tarafı değişmiyor; protokol 3 kalır.

| Dosya | Görev |
|---|---|
| `zone.go` | Adın bölgesini bulur: adın kendisinden başlayıp üst etiketlere NS sorgusuyla çıkar (`net.Resolver.LookupNS`). İlk NS bulunan ad bölgedir. Kaydın adını hesaplar: bölgenin kendisi → `@`, değilse bölgeye göre sol kısım (`kadran`). |
| `providers.go` | NS son eki → firma tablosu. Her girdide: ad, panelde nereye gidileceği, varsa CLI komutu, firmaya özel uyarılar. NS'ler iki farklı firmaya bölünmüşse ya da hiçbiri eşleşmezse genel tarif. |
| `authoritative.go` | Bölgenin NS'lerine doğrudan A/AAAA sorar (`golang.org/x/net/dns/dnsmessage`, UDP; kesilirse TCP). Cevabı ve TTL'yi döndürür. `x/net` zaten dolaylı bağımlılık; yeni modül yok. |
| `cloudflare.go` | Cloudflare'in yayımladığı IPv4/IPv6 aralıkları (sabit liste, kaynağı yorumda). A/AAAA bu aralıktaysa "proxied". |
| `domaincheck.go` | `Check` yukarıdakileri birleştirir; `Report`'a alanlar eklenir: `Zone`, `NameServers`, `Provider`, `RecordName`, `Authoritative` (adresler + TTL), `Proxied`, `Fix` (eklenecek kayıtlar). |

Karar kuralları (bugünküler korunur, iki ekleme):

1. `.localhost` → Skipped (bugünkü gibi).
2. Sorgu bozuk ya da zaman aşımı → Warn; hiçbir zaman engellemez (bugünkü gibi).
3. **Yetkili cevap varsa öncelik onda:** yetkili sunucu sunucunun adresini veriyorsa ve yerel
   çözümleyici eskiyi veriyorsa → Warn ("propagating"). Yetkili yanlışsa → Stop, yerel doğru
   görünse bile (Let's Encrypt yetkili cevaba yakın olanı görür).
4. Yetkili sunucuya ulaşılamazsa bugünkü yerel-çözümleyici kuralları aynen geçerli.
5. A/AAAA Cloudflare aralığındaysa → Stop, `Proxied=true`, Cloudflare'e özel çözüm.
6. AAAA başka yeri gösteriyorsa → Stop (bugünkü gibi); `Fix` bloğu "AAAA kaydını sil" der.

`Fix` bloğu: sunucunun bilinen IPv4'ü için `A`, IPv6'sı biliniyorsa `AAAA`. Aynı ad için
silinmesi gerekenler hep birlikte söylenir (A/AAAA/ALIAS/CNAME, firma "URL forward" ya da park
sayfası kaydı).

### Firma tablosu (ilk sürüm)

NS son ekleri 15 Ekim'de herkese açık DNS'te doğrulandı. IONOS kalıbı ve Squarespace'in
yeni domainlerde de Google NS'lerini kullandığı uygulamada yeniden doğrulanacak.

| Firma | NS son eki | Not |
|---|---|---|
| Vercel | `vercel-dns.com` | `vercel dns add <bölge> <ad> A <ip>`; domain bir projeye bağlıysa oradan da kaldır; `*` joker kaydı varsayılan |
| Cloudflare | `ns.cloudflare.com` | Proxy status: DNS only |
| Namecheap | `registrar-servers.com` | Advanced DNS; "URL Redirect Record" / park kaydını sil |
| GoDaddy | `domaincontrol.com` | DNS → Records; "Parked" A kaydını değiştir |
| Porkbun | `porkbun.com` | DNS Records; varsayılan ALIAS/CNAME park kayıtlarını sil |
| Google Cloud DNS / Squarespace | `googledomains.com` | Google Domains'ten Squarespace'e geçen domainler bu NS'lerde |
| AWS Route 53 | `awsdns-*` (`.com/.net/.org/.co.uk`) | Hosted zone → Create record |
| Hostinger | `dns-parking.com` | DNS / Nameservers → park kaydını sil |
| Hetzner DNS | `ns.hetzner.com` | DNS Console |
| DigitalOcean | `digitalocean.com` | Networking → Domains |
| OVH | `ovh.net` | DNS zone |
| Name.com | `name.com` | Manage DNS Records |
| Gandi | `gandi.net` | DNS Records |
| IONOS | `ui-dns.*` | DNS ayarları |
| Wix | `wixdns.net` | Domains → Manage DNS Records |
| Natro | `natrohost.com` | DNS Yönetimi |
| İsimtescil | `isimtescil.net` | DNS Yönetimi |
| Turhost | `turhost.com` | DNS Yönetimi |

Firma adımları kısa tutulur (2–3 satır); ayrıntı `docs/guides/domains.md`'de.

### `-wait`

- Yalnız `kadran domain check`'te: `-wait 10m`. Varsayılan 0, yani beklemez.
- 15 sn'de bir DNS denetimi. Yalnız durum değişince satır basar.
- Denetim OK olunca ya da yalnız "propagating" uyarısı kalınca port/HTTP/sertifikaya geçer.
- Süre dolarsa son raporla çıkış kodu 1.
- `app create/update` beklemez; Stop mesajı `kadran domain check -wait 10m <domain> <target>`
  önerir.

## Hata ve güvenlik

- DNS'ten gelen metin (NS adları) güvenilmez veridir: basılmadan önce `[a-z0-9.-]` dışı
  karakterler atılır, uzunluk sınırlanır. Terminale kaçış dizisi basılamaz.
- Yetkili sorgu yalnız 53/udp ve 53/tcp; NS'nin adresi sistem çözümleyicisinden alınır.
  Sorgu başına 3 sn, toplamda komutun kendi süresi.
- NS bulunamazsa (ör. henüz kayıtlı olmayan domain) firma "unknown". Bugünkü A/AAAA kuralları
  geçerli, "domain may not be registered or delegated" notu eklenir.
- Bölgenin içinde yetki devredilmiş alt bölge varsa zone yürüyüşü onu bulur (ilk NS'li ad).
- Sunucu tarafına, kadrand'ın ağ kısıtına ve vekil aracısına hiçbir değişiklik yok.

## Test

- `providers_test.go`: her NS son eki için tablo testi; büyük/küçük harf ve sondaki nokta;
  iki firmaya bölünmüş NS → genel tarif; benzer ama farklı son ek (`notvercel-dns.com.evil`)
  eşleşmez.
- `zone_test.go`: sahte çözümleyiciyle `kadran.erkanrzgc.dev` → bölge `erkanrzgc.dev`, kayıt
  `kadran`; kök → `@`; NS hiç yoksa unknown.
- `authoritative_test.go`: sahte DNS sunucusu (yerel UDP dinleyici). A/AAAA cevabı ve TTL,
  kesilmiş cevapta TCP'ye geçiş, zaman aşımı.
- `domaincheck_test.go`: karar tablosuna yeni satırlar (yetkili doğru/yerel eski → Warn,
  yetkili yanlış/yerel doğru → Stop, Cloudflare → Stop+Proxied, yetkiliye ulaşılamıyor → eski
  kurallar).
- `cmd/kadran`: `domain check` ve `app update` çıktısında firma bloğu; `-wait` sahte saatle.
- `cmd/kadran/english_test.go` yeni dizeleri kapsar.
- `scripts/mutate-domaincheck.sh`'a yeni kurallar için mutantlar (öncelik kuralı, Cloudflare
  aralığı, son ek eşleşmesi, NS temizleme).

## Belgeler

- `docs/guides/domains.md`: firma firma adımlar (ekran yolları), sık tuzaklar (park kaydı, eski
  AAAA, joker kayıt, Cloudflare proxy'si, TTL).
- README'deki DNS paragrafı rehbere bağlanır.
- CHANGELOG `## Unreleased`, K-151 karar kaydı.
