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
   DNS barındırıcısını adıyla, eklenecek kaydı (`A @ 203.0.113.10` biçiminde) ve firmaya özel
   adımı basar. Tanınmayan firmada genel tarif basar.
2. Cloudflare'in proxy'li (turuncu bulut) kaydı "başka yeri gösteriyor" diye değil, sebebi ve
   çözümüyle raporlanır.
3. Kayıt DNS firmasında doğru ama yerel çözümleyicinin önbelleği eskiyse komut **durmaz**,
   uyarır; `-skip-dns-check` gerekmez.
4. `kadran domain check -wait <süre>` kayıt doğru olana kadar bekler, sonra port, HTTP ve
   sertifika denetimlerine geçer.
5. Sunucunun kaydıyla birlikte başka adres de dönen karışık küme durdurulur ve silinecek
   kayıtlar adıyla söylenir.
6. Hiçbir firmanın API anahtarı istenmez (kullanıcı kararı: "yönlendir + doğrula").
7. `kadran domain check` domainin hangi firmadan alındığını (RDAP) da söyler; alındığı yer
   DNS'in olduğu yerden farklıysa kaydın nerede değişeceğini açıkça yazar.

## Kapsam dışı

- DNS kaydını firmanın API'siyle otomatik eklemek (kullanıcı reddetti).
- `www` ve diğer yönlendirme adları (B projesi).
- Masaüstü arayüzü: masaüstü bugün DNS önkontrolü yapmıyor; aynı rehberin oraya taşınması
  ayrı iş.
- Uluslararası alan adları (IDN): bugünkü doğrulayıcı yalnız ASCII kabul ediyor, değişmiyor.

## Kullanıcının gördüğü

Yanlış DNS, firma tanınıyor (`app update -domain` hatası ve `domain check` aynı bloğu basar):

```
✗ DNS       the A record points at 198.51.100.7, 198.51.100.8, the server is 203.0.113.10
  DNS host  Vercel (ns1.vercel-dns.com, ns2.vercel-dns.com)
  Set this record and delete other A, AAAA, ALIAS or CNAME records for the same name:
      A   @   203.0.113.10
  At Vercel: vercel dns add erkanrzgc.dev @ A 203.0.113.10
             or Dashboard → Domains → erkanrzgc.dev → DNS Records → Add
  If the domain is assigned to a Vercel project, remove it from the project as well.
  Guide: <Vercel'in resmi DNS kaydı yardım sayfası>
```

Sunucu kaydı eklenmiş ama eski kayıt da duruyor (bugün bu GEÇİYOR, artık durur):

```
✗ DNS       the A records are 203.0.113.10 (server) and 198.51.100.7, 198.51.100.8:
            visitors and Let's Encrypt reach the other addresses part of the time
  Delete these records for "@": A/ALIAS → 198.51.100.7, 198.51.100.8
```

Kayıt firmada doğru, önbellek eski:

```
! DNS       your DNS host (Vercel) already answers 203.0.113.10; this machine still sees
            198.51.100.7 from its cache (the new record's TTL is 600 s, the old cache time is
            unknown). Let's Encrypt usually sees the new record; to be sure, wait with
            kadran domain check -wait 10m. Continuing.
```

Cloudflare proxy'si (NS Cloudflare'de):

```
✗ DNS       the A record points at Cloudflare (104.21.x.x): the record is proxied (orange cloud)
  At Cloudflare: DNS → Records → edit the record → Proxy status: DNS only (grey cloud)
  Keeping the proxy is possible: SSL/TLS mode Full (strict), then -skip-dns-check.
```

Cloudflare aralığı ama NS başka firmada (Porkbun, Hostinger park sayfaları Cloudflare'de):

```
✗ DNS       the A record points at a Cloudflare-hosted page (104.21.x.x), e.g. the
            registrar's parking page: replace it with the record below
```

Kayıt firması (yalnız `kadran domain check`, RDAP):

```
  Registrar Name.com, Inc. (accredited registrar; resellers such as Vercel sell through one)
            the DNS is at Vercel: change the record there
```

RDAP akredite kayıt firmasını verir, alışverişin yapıldığı dükkanı değil: 15 Ekim'de
erkanrzgc.dev (Vercel'den alındı) için cevap `Name.com, Inc.` döndü. Bu yüzden metin "aldığın
yer" demez; asıl yönlendirme her zaman DNS'in olduğu yerdir. `.tr` IANA önyükleme dosyasında
yok (15 Ekim'de bakıldı), orada satır çıkmaz.

Tanınmayan firma: aynı blok, "At Vercel" satırı yerine genel tarif ("your DNS host's control
panel → DNS / Zone editor"), NS adları gösterilir.

## Tasarım

Tüm mantık `internal/domaincheck` içinde, küçük dosyalara bölünmüş. CLI (`cmd/kadran`) yalnız
basar. Sunucu tarafı değişmiyor; protokol 3 kalır.

| Dosya | Görev |
|---|---|
| `zone.go` | Adın bölgesini bulur: adın kendisinden başlayıp üst etiketlere NS sorgusuyla çıkar (`net.Resolver.LookupNS`). İlk NS bulunan ad bölgedir. Kaydın adını hesaplar: bölgenin kendisi → `@`, değilse bölgeye göre sol kısım (`kadran`). |
| `providers.go` | NS son eki → firma tablosu (~50 firma, aşağıda). Her girdide: ad, panel yolu, varsa CLI komutu, firmaya özel uyarı, resmi yardım sayfası. NS'ler iki farklı firmaya bölünmüşse ya da hiçbiri eşleşmezse genel tarif. |
| `authoritative.go` | Bölgenin NS'lerinin **hepsine** doğrudan A/AAAA sorar (`golang.org/x/net/dns/dnsmessage`, UDP; kesilirse TCP). Cevap ancak AA (yetkili) bayrağı taşıyorsa sayılır: bayraksız cevap, 53. portu araya giren bir ISS önbelleğinden gelmiş olabilir (Türk ISS'lerinde yaygın) ve "yetkili ulaşılamadı" sayılır. Cevabı ve TTL'yi döndürür. `x/net` zaten dolaylı bağımlılık; yeni modül yok. |
| `cloudflare.go` | Cloudflare'in yayımladığı IPv4/IPv6 aralıkları (sabit liste, kaynağı yorumda). A/AAAA bu aralıktaysa NS'ye göre ya "proxied" ya da "Cloudflare'de barınan sayfa". |
| `rdap.go` | Yalnız `domain check` için kayıt firması: IANA'nın RDAP önyükleme dosyasından (`https://data.iana.org/rdap/dns.json`) uzantının kayıt otoritesi bulunur, onun RDAP sunucusuna `domain/<ad>` sorulur, `registrar` rolündeki varlığın adı (vCard `fn`) alınır. Üçüncü taraf yönlendirici (rdap.org) kullanılmaz. RDAP'ı olmayan uzantıda (ör. `.tr`) ya da hata/zaman aşımında satır basılmaz; hiçbir zaman engellemez. |
| `domaincheck.go` | `Check` yukarıdakileri birleştirir; `Report`'a alanlar eklenir: `Zone`, `NameServers`, `Provider`, `RecordName`, `Authoritative` (adresler + TTL), `CNAME`, `Proxied`, `Fix` (eklenecek ve silinecek kayıtlar). |

Karar kuralları (bugünküler korunur; 3, 5, 6, 7 ve 8 yeni):

1. `.localhost` → Skipped (bugünkü gibi).
2. Sorgu bozuk ya da zaman aşımı → Warn; hiçbir zaman engellemez (bugünkü gibi).
3. **Yetkili cevap varsa öncelik onda:** yetkili sunucular sunucunun adresini veriyor, yerel
   çözümleyici eskiyi veriyorsa → Warn ("propagating"). Yetkili yanlışsa → Stop, yerel doğru
   görünse bile. Uyarı metni kesinlik iddia etmez: Let's Encrypt kendi önbellekli
   çözümleyicilerini kullanıyor; kesinlik için `-wait` önerilir. Başarısız sertifika denemesi
   Caddy'yi uzun beklemeye sokar (K-128'in gerekçesi).
4. Yetkili sunucuya ulaşılamazsa ya da AA bayrağı yoksa bugünkü yerel-çözümleyici kuralları
   aynen geçerli.
5. **NS'ler birbirini tutmuyorsa** (ör. firmanın kendi içindeki yayılımı sürüyor) → Warn;
   cevap "doğru" sayılmaz.
6. **Karışık kayıt kümesi → Stop.** Kümede sunucunun adresi VE başka adres varsa (bugün
   `overlaps` bunu geçiriyor) durulur; `Fix` silinecek fazlalıkları listeler. Ziyaretçiler ve
   Let's Encrypt'in çok noktadan doğrulaması fazlalıklara düşebilir. Bilerek birden çok sunucu
   kullanan `-skip-dns-check` ile geçer.
7. **CNAME:** yetkili cevap yalnız CNAME dönerse (www'de yaygın) hedefi sistem
   çözümleyicisiyle çözülür ve aynı kurallar hedefin adreslerine uygulanır. Hedef sunucuyu
   gösteriyorsa OK; göstermiyorsa Stop: "CNAME to X; replace it with an A record".
8. A/AAAA Cloudflare aralığındaysa → Stop. NS Cloudflare'deyse `Proxied=true` ve "DNS only"
   tarifi; NS başka firmadaysa "Cloudflare'de barınan bir sayfa (ör. park sayfası), değiştir".
9. AAAA başka yeri gösteriyorsa → Stop (bugünkü gibi); `Fix` bloğu "AAAA kaydını sil" der.

`Fix` bloğu: sunucunun bilinen IPv4'ü için `A`, IPv6'sı biliniyorsa `AAAA`. Aynı ad için
silinmesi gerekenler hep birlikte söylenir (A/AAAA/ALIAS/CNAME, firma "URL forward" ya da park
sayfası kaydı).

**Süre bütçesi:** bölge yürüyüşü ve NS sorguları `app create/update`'ten önce her `-domain`
değişikliğinde koşuyor. Tüm DNS aşaması en çok 10 sn sürer; NS sorguları paralel, sorgu başına
3 sn. Bütçe aşılırsa sessizce kural 4'e düşülür (bugünkü davranış), komut takılmaz. NS'nin
IPv4 adresi önce denenir; yalnız IPv4'lü ağda IPv6 adresinde beklenmez.

### Firma tablosu

Kullanıcı isteği (15 Ekim): "major platformların çoğu olmalı". Tablo dört grupta ~50 firma.
Domain çoğu zaman alındığı firmanın DNS'inde kalıyor. NS başka yere taşındıysa (ör. Namecheap'ten
alınıp Cloudflare'e bağlanmış) tarif DNS'in olduğu yeri söyler, çünkü kayıt orada değişir.

Her girdide: ad, NS son ekleri, panel yolu (tek satır), varsa CLI komutu, firmaya özel uyarı ve
firmanın **resmi yardım sayfası** (`Guide: <url>` olarak basılır). Panel menüleri değişse de
kullanıcı doğru sayfaya gider.

Doğrulama (15 Ekim): aşağıdaki NS adlarının hepsi herkese açık DNS'te çözülüyor. Uygulamada her
girdi için ayrıca (a) firmanın belgelerinde bu NS'nin müşteri varsayılanı olduğu, (b) yardım
sayfası bağlantısının açıldığı doğrulanır. Doğrulanamayan girdi tabloya girmez. Shopify,
Radore, Vargonen ve NameBright'ın varsayılan NS'leri doğrulanamadığı için şimdilik dışarıda.

Eşleşme etiket sınırında yapılır: NS ya son ekin kendisidir ya da `.` + son ek ile biter
(`he.net`, `ns1.he.net`'i tutar; `ns.the.net`'i tutmaz).

**Domain firmaları**

| Firma | NS son eki | Not |
|---|---|---|
| Vercel | `vercel-dns.com` | `vercel dns add <bölge> <ad> A <ip>`; domain bir projeye bağlıysa oradan da kaldır; `*` joker kaydı varsayılan |
| Cloudflare | `ns.cloudflare.com` | Proxy status: DNS only |
| GoDaddy | `domaincontrol.com` | "Parked" A kaydını değiştir |
| Namecheap | `registrar-servers.com` | Advanced DNS; "URL Redirect Record" / park kaydını sil |
| Network Solutions | `worldnic.com` | |
| Register.com | `register.com` | |
| Domain.com | `domain.com` | |
| Porkbun | `porkbun.com` | varsayılan ALIAS/CNAME park kayıtlarını sil |
| Squarespace / Google | `googledomains.com` | Google Domains'ten geçen domainler; Squarespace'in yeni domainleri uygulamada doğrulanacak |
| Name.com | `name.com` | |
| Hover | `hover.com` | |
| Dynadot | `dyna-ns.net` | |
| NameSilo | `dnsowl.com` | |
| Spaceship | `spaceship.net` | |
| Epik | `epik.com` | |
| Gandi | `gandi.net` | |
| OVHcloud | `ovh.net`, `ovh.ca` | |
| IONOS | `ui-dns.com`, `ui-dns.de`, `ui-dns.org`, `ui-dns.biz` | |
| Strato | `rzone.de` | |
| one.com | `one.com` | |
| Wix | `wixdns.net` | |
| Reg.ru | `reg.ru` | |
| Alibaba Cloud | `hichina.com` | |
| Tencent DNSPod | `dnspod.net` | |

**Hosting firmaları** (domain hosting paketiyle alınınca NS onlarda kalıyor)

| Firma | NS son eki | Not |
|---|---|---|
| Hostinger | `dns-parking.com`, `hostinger.com` | park kaydını sil |
| Bluehost | `bluehost.com` | |
| HostGator | `hostgator.com` | |
| DreamHost | `dreamhost.com` | |
| SiteGround | `siteground.net` | |
| InMotion | `inmotionhosting.com` | |
| A2 Hosting | `a2hosting.com` | |
| Namecheap Hosting | `namecheaphosting.com` | cPanel → Zone Editor |

**Bulut ve DNS servisleri**

| Firma | NS son eki | Not |
|---|---|---|
| AWS Route 53 | `awsdns-NN.com/.net/.org/.co.uk` | Hosted zone → Create record |
| Azure DNS | `azure-dns.com/.net/.org/.info` | |
| Google Cloud DNS | `googledomains.com` | Squarespace satırıyla aynı NS; ikisi birlikte adlandırılır |
| DigitalOcean | `digitalocean.com` | Networking → Domains |
| Linode / Akamai | `linode.com` | |
| Vultr | `vultr.com` | |
| Hetzner | `ns.hetzner.com`, `first-ns.de`, `second-ns.de`, `second-ns.com` | DNS Console; eski Robot NS'leri de |
| Netlify / NS1 | `nsone.net` | iki servis aynı NS'yi kullanıyor; ikisi birlikte adlandırılır |
| Bunny DNS | `bunny.net` | |
| ClouDNS | `cloudns.net` | |
| DNSimple | `dnsimple.com` | |
| DNS Made Easy | `dnsmadeeasy.com` | |
| easyDNS | `easydns.com` | |
| Hurricane Electric | `he.net` | |
| FreeDNS (afraid.org) | `afraid.org` | |

**Türkiye**

| Firma | NS son eki | Not |
|---|---|---|
| Natro | `natrohost.com` | DNS Yönetimi |
| İsimtescil | `isimtescil.net` | DNS Yönetimi |
| Turhost | `turhost.com` | DNS Yönetimi |
| Güzel Hosting | `guzelhosting.com` | |
| Turkticaret.net | `turkticaret.net` | |
| Hosting.com.tr | `hosting.com.tr` | |
| Markum | `markum.net` | |

Tanınmayan her NS genel tarife düşer: "your DNS host's control panel → DNS / Zone editor",
NS adları ve eklenecek kayıt. Tablo veri olarak tutulur (`providers.go`); yeni firma eklemek
tek satır ve bir test satırıdır.

Firma adımları kısa tutulur (1–2 satır + `Guide:` bağlantısı); ayrıntı `docs/guides/domains.md`'de.

### `-wait`

- Yalnız `kadran domain check`'te: `-wait 10m`. Varsayılan 0, yani beklemez.
- 15 sn'de bir DNS denetimi. Yalnız durum değişince satır basar.
- Denetim OK olunca ya da yalnız "propagating" uyarısı kalınca port/HTTP/sertifikaya geçer.
- Süre dolarsa son raporla çıkış kodu 1.
- `app create/update` beklemez; Stop mesajı `kadran domain check -wait 10m <domain> <target>`
  önerir.

## Hata ve güvenlik

- RDAP: yalnız HTTPS; https olmayan yönlendirme izlenmez; cevap en çok 1 MiB okunur; istek
  başına 5 sn. Kayıt otoritesine domain adı gider (zaten herkese açık bilgi); IANA dosyasının
  isteğinde domain adı yok. Firma adı güvenilmez metindir: basılabilir karakterlere indirilir,
  uzunluğu sınırlanır. `app create/update` RDAP sorgusu yapmaz.

- DNS'ten gelen metin (NS adları) güvenilmez veridir: basılmadan önce `[a-z0-9.-]` dışı
  karakterler atılır, uzunluk sınırlanır. Terminale kaçış dizisi basılamaz.
- Yetkili sorgu yalnız 53/udp ve 53/tcp; NS'nin adresi sistem çözümleyicisinden alınır.
  Süre bütçesi "Tasarım" bölümünde: sorgu başına 3 sn, DNS aşaması toplam 10 sn.
- Cevabın soru kimliği (ID) ve soru bölümü gönderilenle eşleşmezse cevap atılır.
- NS bulunamazsa (ör. henüz kayıtlı olmayan domain) firma "unknown". Bugünkü A/AAAA kuralları
  geçerli, "domain may not be registered or delegated" notu eklenir.
- Bölgenin içinde yetki devredilmiş alt bölge varsa zone yürüyüşü onu bulur (ilk NS'li ad).
- Sunucu tarafına, kadrand'ın ağ kısıtına ve vekil aracısına hiçbir değişiklik yok.

## Test

- `rdap_test.go`: `httptest` sunucusuyla önyükleme + RDAP cevabı → firma adı; uzantı
  önyüklemede yok → satır yok; `registrar` varlığı yok → satır yok; büyük cevap 1 MiB'te
  kesilir; https olmayan yönlendirme izlenmez; adı kaçış dizisi içeren firma temizlenir;
  kayıt firması ile DNS firması aynı/farklı metni.

- `providers_test.go`: tablodaki her NS son eki için test (tablodan üretilir, yeni girdi testsiz kalamaz); her girdinin yardım bağlantısı `https://` ile başlar; büyük/küçük harf ve sondaki nokta;
  iki firmaya bölünmüş NS → genel tarif; benzer ama farklı son ek (`notvercel-dns.com.evil`)
  eşleşmez.
- `zone_test.go`: sahte çözümleyiciyle `kadran.erkanrzgc.dev` → bölge `erkanrzgc.dev`, kayıt
  `kadran`; kök → `@`; NS hiç yoksa unknown.
- `authoritative_test.go`: sahte DNS sunucusu (yerel UDP dinleyici). A/AAAA cevabı ve TTL,
  kesilmiş cevapta TCP'ye geçiş, zaman aşımı, AA bayrağı olmayan cevap → "yetkili yok",
  yalnız CNAME dönen cevap.
- `domaincheck_test.go`: karar tablosuna yeni satırlar: yetkili doğru/yerel eski → Warn;
  yetkili yanlış/yerel doğru → Stop; NS'ler tutarsız → Warn; karışık küme → Stop ve `Fix`
  fazlalıkları listeler; CNAME hedefi sunucu → OK, başka yer → Stop; Cloudflare aralığı + NS
  Cloudflare → Proxied, NS başka → park sayfası; yetkiliye ulaşılamıyor → eski kurallar; 10 sn
  bütçe aşımı → eski kurallar, komut takılmaz.
- `cmd/kadran`: `domain check` ve `app update` çıktısında firma bloğu; `-wait` sahte saatle.
- `cmd/kadran/english_test.go` yeni dizeleri kapsar.
- `scripts/mutate-domaincheck.sh`'a yeni kurallar için mutantlar: öncelik kuralı, karışık
  küme, AA bayrağı, CNAME takibi, Cloudflare aralığı ve NS'ye göre metin, son ek eşleşmesi, NS
  temizleme.

## Belgeler

- `docs/guides/domains.md`: firma firma adımlar (ekran yolları), sık tuzaklar (park kaydı, eski
  AAAA, joker kayıt, Cloudflare proxy'si, TTL).
- README'deki DNS paragrafı rehbere bağlanır.
- CHANGELOG `## Unreleased`, K-151 karar kaydı.
