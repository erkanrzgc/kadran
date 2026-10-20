# www ve yönlendirme adları — bir uygulama, birden çok ad

**Tarih:** 20 Ekim 2026
**Durum:** TASARIM. Kullanıcı soru istemedi ("sorma direkt başla"). Biçimi 15 Ekim'de sohbette
verdiği kararlar belirliyor; aşağıda **(kullanıcı)** diye işaretli. Geri kalan her şey benim
kararım; **(karar)** diye işaretli ve K-152'ye yazılacak.
**İlgili:** K-151 (DNS rehberi, A projesi), K-152 (bu iş için açılacak karar kaydı).
**Sıra:** A (bitti, PR #14) → **B (bu belge)** → v0.8.0 (A+B) → C (erkanrzgc.dev'in Hetzner'e
taşınması).

## Amaç

Bir uygulama `example.com`'da yayındayken `www.example.com`'a gelen ziyaretçi bugün Caddy'nin
"rota yok" cevabını alıyor. Kullanıcının istediği:

1. `www.example.com` → `https://example.com/<aynı yol ve sorgu>` **308** yönlendirmesi
   **(kullanıcı)**.
2. www otomatik gelir, `-no-www` ile kapatılır **(kullanıcı)**.
3. Başka adlar da (eski domain, yazım farkı) `-redirect <ad>` ile aynı uygulamaya yönlenir
   **(kullanıcı)**.
4. Yönlendirme ters vekilin **rotasının içinde** yapılır; hedef **her zaman uygulamanın kendi
   domaini**dir, kullanıcı ya da kadrand rastgele bir hedef URL veremez **(kullanıcı)**.

Başarı ölçütleri:

1. `kadran app create site -domain example.com …` sonrası `https://www.example.com/a?b=1`
   isteği `308 Location: https://example.com/a?b=1` döner; `www.example.com` için ayrı bir
   Let's Encrypt sertifikası alınır.
2. `app update -domain yeni.com` yönlendirmeleri de yeni domaine taşır (yeniden dağıtım yok).
3. Yükseltmeden sonra mevcut uygulamaların hiçbiri kendiliğinden www kazanmaz.
4. Bir yönlendirme adı başka bir uygulamanın adıyla çakışırsa API reddeder; yine de içeri
   sızan bir çakışma YALNIZ o yönlendirmeyi düşürür, hiçbir uygulamanın trafiğini kesmez.
5. Yönlendirme adının DNS'i sunucuyu göstermiyorsa `app create/update` DURMAZ; uyarır ve
   eklenecek kaydı (A rehberinin biçiminde) basar.

## Kapsam dışı

- Rastgele hedefe yönlendirme (`a.com` → `b.com/yol`): hedef her zaman uygulamanın domaini.
- Yol yeniden yazma, 301/302 seçimi: hep 308 (yöntem ve gövde korunur).
- Joker adlar (`*.example.com`).
- Masaüstü arayüzü: yalnız CLI ve API. Masaüstü yeni alanları gösterebilir ama bu iş onu
  değiştirmez **(karar)**.
- Erkanrzgc.dev'in taşınması (C projesi).

## Kullanıcının gördüğü

```
$ kadran app create site -repo github.com/u/site -domain example.com kadran-client@sunucu
…
  www       www.example.com → example.com (308)
  ⚠ www.example.com does not point at the server yet; its certificate will be issued once it does.
  DNS host  Vercel (ns1.vercel-dns.com, ns2.vercel-dns.com)
  Set this record:
      A     www          203.0.113.10
```

- `app create`: `-no-www` www'yi kapatır; `-redirect <ad>` (birden çok kez) ek ad.
- `app update`: `-www` / `-no-www` aç/kapa; `-redirect <ad>` ekler, `-redirect-rm <ad>`
  çıkarır (`-env-rm`/`-volume-rm` deseni). Verilmeyen değişmez.
- `app show` ve `app list`: "redirects" satırı (`www.example.com, eski.com → example.com`);
  `-json` alanları taşır.
- Eski sunucuya yeni CLI: sunucu cevabında alan yoksa CLI "this server does not support
  redirects; upgrade kadrand" uyarır ve yönlendirme var demez **(karar)**.

## Tasarım

### Ad modeli

- **www bir bayraktır, ad değildir (karar).** Saklanan şey `www = true/false`; www adı rota
  kurulurken türetilir. Sebep: `app update -domain` trafiği dağıtımsız taşıyor; saklanmış
  `www.eski.com` eski domaine yönlendirmeye devam ederdi.
- **Simetrik eş (karar).** Türetilen ad:
  - domain kayıt edilebilir köke (eTLD+1, `golang.org/x/net/publicsuffix`) eşitse →
    `www.<domain>`;
  - domain `www.<eTLD+1>` ise → `<eTLD+1>` (kök www'ye yönlenir; erkanrzgc.dev bugün böyle);
  - başka her alt ad için (`app.example.com`) eş YOK; bayrak açık olsa da rota üretilmez.
- **Açık adlar** (`-redirect`) ayrı bir liste olarak saklanır; uygulama başına en çok 8
  **(karar)**.
- **Varsayılanlar (karar):** yeni CLI `app create`'te bayrağı açık gönderir (`-no-www` yoksa).
  Sunucuda alan yoksa (eski CLI, mevcut uygulamalar, göç) değer **kapalı**dır. Proto alanları
  `optional` (varlık taşır).
- Ortak işlev: türetme kuralı kadrand (rota + çakışma denetimi) ve CLI (gösterim + DNS
  denetimi) tarafından AYNI işlevle yapılır; küçük bir paket (ör. `internal/appnames`).
  `proxydrv`'e konmaz: aracı publicsuffix'e muhtaç olmamalı **(karar)**.

### API ve depo

- `AppSpec`: `optional bool www`, `repeated string redirects`. `UpdateAppRequest`:
  `optional bool www`, `repeated string redirect_add`, `repeated string redirect_remove`
  (env/volume deseni).
- Doğrulama (create ve update, aynı yazma işleminin içinde):
  - her ad `proxydrv.ValidDomain`'den geçer (küçük harf, IP değil);
  - uygulamanın kendi domainine eşit olamaz; listede tekrar yok; en çok 8;
  - domaini boş bir uygulama yönlendirme adı alamaz;
  - **çakışma kuralı:** bir ad (açık ya da türetilmiş www eşi) hiçbir uygulamanın domainine,
    açık adına ya da türetilmiş eşine eşit olamaz. Kaçırılması kolay durum: yeni uygulamanın
    domaini mevcut bir uygulamanın türetilmiş www eşine eşit. Bir domain değişikliği ya da www
    açılması da aynı kuraldan geçer.
- Depo: `apps` tablosuna `www` (0/1, varsayılan 0) ve açık adların listesi; göç dosyası
  `0010_…`. Biçim (sütun mu tablo mu) planlayıcının seçimi; çakışma denetimi yarışsız olmalı.

### Uzlaştırma (reconcile)

- Rota kümesi = her uygulamanın domain rotası + her yönlendirme adı için `{From, To}` (To =
  uygulamanın domaini). Türetilmiş eş ve açık adlar aynı yoldan geçer.
- `www` ya da yönlendirme listesi değişen bir `app update` uzlaştırmayı tetikler (domain ve
  replika gibi); `app create` tetiklemez (rota ilk dağıtımda gelir).
- **Çakışma bütün yapılandırmayı düşürmez (karar, zorunlu).** Bugün yinelenen bir ana bilgisayar
  `BuildConfig`'i bütün kümeyi reddettiriyor, yani bütün uygulamalar trafiksiz kalır. Bir
  yönlendirme adı bir domain rotasıyla ya da başka bir yönlendirmeyle çakışırsa YALNIZ o
  yönlendirme atlanır ve AYRI bir alana (`Result.SkippedRedirects`, ad → sebep) yazılır;
  domain rotaları her zaman öncelikli. `Skipped`'e YAZILMAZ: `moveTraffic` orayı "trafik
  taşınmadı" diye okur ve sağlam bir domain taşınmasını hata gösterirdi **(karar)**.
- Yeni sürümü rotalanamayan bir uygulamanın canlı rotası bugün taşınıyor (`carrySkippedRoutes`);
  o rotanın canlı yönlendirmeleri de aynı yolla taşınır, yoksa başarısız bir dağıtım www'yi
  düşürürdü **(karar)**.

### Ters vekil aracısı (kadran-proxy) — güvenlik sınırı

- `RouteSet`'e `Redirects []ProxyRedirect{From, To}` (`json:"redirects,omitempty"`).
- Aracı kadrand'a güvenmez; `FixedConfig` şunları uygular:
  - `From` ve `To` `ValidDomain`'den geçer;
  - **`To` aynı kümedeki bir domain rotasına eşit olmalı** (açık yönlendirme yok);
  - `From` hiçbir domain rotasına ve başka bir `From`'a eşit olamaz;
  - toplam yönlendirme sayısı sınırlı (ör. `MaxRedirects = 1024`).
- Üretilen rota: `match host [From]` → tek tutamaç `static_response`, `status_code 308`,
  başlık yalnız `Location: https://<To>{http.request.uri}`, gövde yok, `terminal`.
  Şablon sabit; değişken kısım yalnız `To` (doğrulanmış alan adı, `{`/`}` içeremez).
- `SafeRoutes` canlı yapılandırmayı okurken yönlendirme rotasını bu BİREBİR biçimde tanır ve
  `FixedConfig` ile yeniden kurup `DeepEqual` karşılaştırır: fazladan başlık, gövde ya da başka
  yer tutucu reddedilir.
- **Ölçüldü (20 Ekim, `build/caddy/redirect_test.go`, Caddy v2.11.7):** `static_response`
  değiştiricisi değerleri yeniden açmıyor. İstek yolu ya da sorgusu `{file.<yol>}`, `%7B…%7D`,
  `{env.PATH}`, `{http.request.uri}` taşısa da Location'a dosya içeriği girmiyor; kontrol
  grubu (şablonun kendisinde `{file.…}`) sızdırıyor, yani test sızıntıyı görebiliyor. Yol ve
  sorgu korunuyor, `%0d%0a` kodlu kalıyor, `//evil.example/x` aynı host'ta kalıyor. Bu yüzden
  özel bir Caddy modülü GEREKMİYOR **(karar)**; test kalıcı gerileme testi olarak kalır.
- Yükseltme sırası: `redirects` anahtarı yalnız doluyken gönderilir; eski aracı yönlendirmesiz
  kümeyi kabul eder. Hem aracı hem istemci `DisallowUnknownFields` kullanıyor; yeni kadrand +
  eski aracı penceresinde yönlendirmeli küme reddedilir ve canlı yapılandırma DEĞİŞMEZ (trafik
  kesilmez). Bootstrap'ın yeniden başlatma sırası plan aşamasında doğrulanır.

### Sertifikalar

- Caddy her rota host'u için kendiliğinden sertifika alır; yönlendirme adı da. DNS'i olmayan
  bir www'nin ACME denemeleri yalnız o adı etkiler; kök domainin sertifikası ayrı **(ölçülecek:
  e2e ya da canlı)**.

### CLI DNS denetimi

- Ana domain denetimi bugünkü gibi (A projesi): Stop'ta komut durur.
- Her yönlendirme adı (türetilmiş eş dahil) aynı denetimden geçer ama **asla durdurmaz**:
  sonuç uyarı + A rehberinin kayıt satırı (`A www 203.0.113.10`). www'nin köke CNAME olması
  da geçerli (A projesinin denetçisi CNAME'i izliyor).
- Adlar **paralel** denetlenir; her biri kendi 10 sn bütçesiyle, toplam süre ~en yavaşı.
- `-skip-dns-check` bütün adları atlar.

## Hata ve güvenlik

- Açık yönlendirme yok: hedefi aracı doğruluyor (kümedeki bir domain), kadrand değil.
- Location'a giren tek değişken doğrulanmış bir alan adı; istek URI'si yer tutucu olarak
  yeniden açılmıyor (ölçüldü).
- Yasak alanlar ve ayrıcalıklı yüzey: kadran-exec bu işe dokunmaz; `check-exec-surface.sh`
  değişmemeli.
- Bir çakışma ne API'de ne aracıda bütün rota kümesini düşürür.

## Test

- `internal/appnames`: eş türetme tablosu (kök, www, alt ad, `co.uk` gibi çok etiketli son ek,
  özel son ek `duckdns.org`).
- API/depo: çakışma kuralının her kolu (yeni domain = mevcut türetilmiş eş dahil), sınır 8,
  boş domain, update ekle/çıkar/aç/kapa, domain taşınınca eş de taşınır, göç sonrası mevcut
  uygulamalar `www=false`.
- Reconcile: çakışan yönlendirme yalnız kendini düşürür, `Skipped`'e yazılır.
- proxydrv/proxyguard: `To` kümede değilse ret; `From` bir domain ya da tekrar ise ret;
  sınır; `SafeRoutes` fazladan başlık/gövde/yer tutucu taşıyan yönlendirme rotasını reddeder;
  yönlendirmesiz küme bugünkü JSON'u birebir üretir (`omitempty`).
- `build/caddy/redirect_test.go`: yukarıdaki ölçüm, kalıcı.
- CLI: bayraklar, `app show`/`-json`, eski sunucu uyarısı (cevapta alan yok), yönlendirme
  adı DNS'i yanlışken create durmaz ve kayıt satırını basar, paralel denetim.
- Uçtan uca (gerçek kadrand, `scripts/e2e-cli*.sh`): `curl --resolve` ile www → 308, yol ve
  sorgu korunur; `app update -domain` sonrası yönlendirme yeni domaine gider.
- Mutasyon: `scripts/mutate-*.sh`'a aracının `To` denetimi, `From` çakışması, reconcile atlama
  ve varsayılan kapalı için mutantlar; yeni test adlarının `TESTS` düzenine uyduğu
  denetlenir.

## Belgeler

- `docs/guides/domains.md`: "www and other names" bölümü (bayraklar, DNS kaydı, sertifika).
- README: `-no-www` / `-redirect` bir cümle.
- CHANGELOG `## Unreleased`: Added.
- K-152 karar kaydı (Türkçe): yukarıdaki **(karar)** maddeleri ve ölçüm.
