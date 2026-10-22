# www ve yönlendirme adları (B projesi) — Uygulama Planı

> **Ajanlar için:** GEREKLİ ALT BECERİ: superpowers:subagent-driven-development (önerilen) ya da superpowers:executing-plans. Adımlar `- [ ]` ile izlenir.

**Amaç:** Bir uygulamanın domainine ek adlar (`www` eşi otomatik, `-redirect` ile açık adlar) 308 ile uygulamanın kendi domainine yönlensin; yönlendirme ters vekil aracısının rotasında, hedefi aracının doğruladığı bir domain olarak.

**Mimari:** Ad türetme kuralı tek bir küçük pakette (`internal/appnames`). Depo `www` bayrağını ve açık adları saklar, çakışmaları aynı yazma işleminde reddeder. Uzlaştırıcı adları rota kümesine `{From, To}` olarak ekler ve çakışanı yalnız kendisi olarak düşürür. `proxydrv`/`proxyguard` yeni rota biçimini (`static_response` 308 + sabit Location şablonu) birebir üretir ve tanır. CLI bayrakları, gösterimi ve yönlendirme adlarının durdurmayan DNS denetimini ekler.

**Teknoloji:** Go 1.26.9; yeni modül YOK (`golang.org/x/net/publicsuffix` zaten doğrudan bağımlılık). Caddy v2.11.7 (`build/caddy`, ayrı modül). Proto: `buf generate`.

**Spec:** [docs/specs/2026-10-20-www-yonlendirme-design.md](../specs/2026-10-20-www-yonlendirme-design.md)

## Genel kısıtlar

- Go her Bash çağrısında: `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:$PATH"` (`buf` de orada). `-race` için PATH'e `/c/msys64/mingw64/bin` eklenir.
- Proto değişince `buf generate` VE `buf lint` ikisi de yerelde koşulur; üretilen `internal/pb/...` dosyaları commit'e girer.
- CLI metni İngilizce; `cmd/kadran` ve `internal/domaincheck` test-dışı dize sabitlerinde Türkçe harf YOK (`cmd/kadran/english_test.go`). Kod yorumları Türkçe olabilir.
- Depoya gerçek sunucu IP'si yazılmaz; testler `203.0.113.0/24`, `198.51.100.0/24`, `192.0.2.0/24`, `2001:db8::/32` ve Docker için `172.16.0.0/12` adreslerini kullanır.
- Testler ağa çıkmaz (loopback `httptest`/`127.0.0.1` dinleyici serbest).
- Commit: `update <birkaç İngilizce kelime>`, tek satır, gövde YOK, Co-Authored-By YOK.
- Push, PR, birleştirme ve canlı sunucu adımları kullanıcı onayı ister.
- `kadran-exec`'e dokunulmaz; `bash scripts/check-exec-surface.sh` değişmeden geçmeli (bugün 2916/2925).
- Hem `proxyguard` hem `proxydrv` istemcisi `DisallowUnknownFields` kullanıyor: `RouteSet.Redirects` `omitempty` OLMALI; yönlendirmesiz küme bugünkü JSON'u bayt bayt üretmeli.
- Uygulama başına açık ad sınırı `maxRedirects = 8`; aracıda toplam `MaxRedirects = 1024`.
- 308 durum kodu; Location şablonu TAM OLARAK `"https://" + to + "{http.request.uri}"`.
- Aynı Bash komutunda `grep -n` ile `git commit` birlikte olmaz (kanca `--no-verify` sanıyor).
- Mutasyon koşusu sürerken başka ağır iş ya da git komutu çalıştırılmaz.

## İnceleme odağı

1. **Yeni uygulamanın domaini, mevcut bir uygulamanın türetilmiş www eşi** (`www.example.com` yeni uygulama; `example.com` www açık): create REDDEDİLİR, hata eşin sahibini adıyla söyler → Görev 2, `TestCreateAppRejectsNameUsedAsPartner`.
2. **`app update -domain` ile domain taşınırken eş de taşınır** ve yeni eş başka uygulamanınkiyle çakışıyorsa update reddedilir → Görev 2, `TestUpdateAppMovesPartnerWithDomain` + `TestUpdateAppRejectsPartnerConflict`.
3. **Yükseltme sırasında eski aracı**: yönlendirmesiz küme bugünkü JSON'la birebir; yönlendirmeli küme eski aracıda reddedilirse canlı yapılandırma değişmez → Görev 4, `TestRouteSetWithoutRedirectsKeepsWireFormat`.
4. **Başarısız dağıtım www'yi düşürmez**: yeni sürümü rotalanamayan uygulamanın canlı rotası taşınırken canlı yönlendirmesi de taşınır → Görev 5, `TestReconcileCarriesLiveRedirectsOfSkippedApp`.
5. **Alt ad uygulamasında www yok**: `app.example.com` için www bayrağı açık olsa da eş türetilmez, CLI www satırı basmaz ve DNS denetimi yapmaz → Görev 1 tablo + Görev 6, `TestAppCreateSubdomainHasNoWWW`.

---

### Görev 1: `internal/appnames` — ad türetme ve çakışma

**Dosyalar:**
- Oluştur: `internal/appnames/appnames.go`, `internal/appnames/appnames_test.go`

**Arayüzler:**
- Üretir:
  - `func Partner(domain string) string` — domain `publicsuffix.EffectiveTLDPlusOne(domain)`'e eşitse `"www."+domain`; domain `"www."+eTLD1` ise `eTLD1`; aksi hâlde (alt ad, hata, `proxydrv.ValidDomain`'den geçmeyen ya da 253'ü aşan sonuç) `""`.
  - `func Names(domain string, www bool, redirects []string) []string` — `www && Partner(domain) != ""` ise önce eş, sonra açık adlar (sıralı, tekrarsız, domainin kendisi çıkarılmış). `domain == ""` → `nil`.
  - `type Owner struct { AppID, Domain string; WWW bool; Redirects []string }`
  - `func Conflict(c Owner, others []Owner) (name, ownerID string, found bool)` — `c`'nin domaini ve `Names(...)`'inden biri, `others` içinde `AppID != c.AppID` olan bir sahibin domainine ya da `Names(...)`'ine eşitse ilk çakışan ad ve sahibi. Sıra: önce `c.Domain`, sonra `c`'nin adları; sahipler verilen sırayla.
- Bu paket `proxydrv`'i içe aktarabilir (yalnız `ValidDomain`); `proxydrv` bu paketi İÇE AKTARMAZ.

- [ ] **Adım 1: Başarısız tablo testi** — `TestPartner`:
  `example.com→www.example.com`, `www.example.com→example.com`, `app.example.com→""`, `example.co.uk→www.example.co.uk`, `www.example.co.uk→example.co.uk`, `shop.example.co.uk→""`, `home.duckdns.org→www.home.duckdns.org` (özel son ek de eTLD+1 sayılır), `""→""`, `com→""`, `127.0.0.1→""`.
  `TestNames`: `("example.com", true, ["old.com","old.com","example.com"])` → `["www.example.com","old.com"]`; `www=false` → `["old.com"]`; `("app.example.com", true, nil)` → `nil`; `("", true, ["a.com"])` → `nil`.
  `TestConflict`: (a) yeni domain = başkasının eşi → `("www.example.com", "site", true)`; (b) yeni açık ad = başkasının domaini; (c) kendi kaydı (`AppID` aynı) çakışma sayılmaz; (d) çakışma yok → `found=false`; (e) iki sahip aynı adı kullanıyorsa ilk sahip döner.
- [ ] **Adım 2:** `go test ./internal/appnames/ -count=1` → derlenmez (paket yok).
- [ ] **Adım 3:** Uygula.
- [ ] **Adım 4:** Aynı komut → PASS.
- [ ] **Adım 5:** `git add internal/appnames/` → `git commit -m "update app name rules"`

### Görev 2: Proto, göç ve depo

**Dosyalar:**
- Değiştir: `proto/kadran/v1/api.proto`, üretilen `internal/pb/kadran/v1/*.go`
- Oluştur: `internal/store/migrations/0010_app_redirects.sql`, `internal/store/appredirects_test.go`
- Değiştir: `internal/store/apps.go`, `internal/store/appupdate.go`, `internal/store/deployments.go`

**Arayüzler:**
- Proto:
  - `AppSpec`: `optional bool www = 15;` `repeated string redirects = 16;` (yorum: www bir bayrak, ad türetilir; eski sunucu alanı düşürür — K-152).
  - `UpdateAppRequest`: `optional bool www = 10;` `repeated string redirect_add = 11;` `repeated string redirect_remove = 12;`
  - `UpdateAppResponse`: `string redirect_detail = 5;`
- Göç `0010_app_redirects.sql`: `ALTER TABLE apps ADD COLUMN www INTEGER NOT NULL DEFAULT 0 CHECK (www IN (0, 1));` ve `ALTER TABLE apps ADD COLUMN redirects_json TEXT NOT NULL DEFAULT '[]';` + başta neden mevcut uygulamaların kapalı başladığını anlatan yorum.
- `store.App`: `WWW bool`, `Redirects []string` (yazarken ve okurken sıralı, tekrarsız; boşsa `[]string{}` değil `nil` — `Volumes`'un normalleştirme deseniyle aynı tutarlılık).
- `var ErrNameTaken = errors.New("name already in use")` — mesaj `%w: %q is already used by app %q`.
- `store.AppUpdate`: `WWW *bool`, `RedirectAdd []string`, `RedirectRemove []string`; `func (u AppUpdate) ChangesNames(current App) bool` (www değeri ya da sonuç ad listesi farklıysa true); `IsEmpty` bu alanları sayar; `applyTo` önce ekler sonra çıkarır (olmayan adı çıkarmak sessiz).
- `CreateApp` ve `UpdateApp`: AYNI işlem içinde `SELECT id, domain, www, redirects_json FROM apps` ile `[]appnames.Owner` kurulur, `appnames.Conflict` çakışma bulursa `ErrNameTaken`. `CreateApp` bunun için `BeginTx`'e geçer (DSN `_txlock=immediate`, `SetMaxOpenConns(1)` → yarışsız). Domain benzersiz indeksinin mevcut hatası (`ErrDomainTaken`) aynen kalır.
- `store.Deployment`: `WWW bool`, `Redirects []string`; `ActiveDeployments` `a.www, a.redirects_json` okur.

- [ ] **Adım 1: Başarısız testler** (`appredirects_test.go`):
  - `TestCreateAppStoresWWWAndRedirects` — yaz/oku turu; `Redirects` sıralı ve tekrarsız döner.
  - `TestMigrationLeavesExistingAppsWithoutWWW` — göç 0009'a kadar uygulanmış bir veritabanına bir uygulama yazılır, 0010 uygulanır → `WWW == false`, `Redirects == nil` (`migration0005_test.go`'nun deseni).
  - `TestCreateAppRejectsNameUsedAsPartner` — `site` (`example.com`, www açık) varken `blog` domaini `www.example.com` → `errors.Is(err, ErrNameTaken)` ve mesajda `"site"`.
  - `TestCreateAppRejectsRedirectThatIsAnotherDomain` — açık ad başkasının domaini → `ErrNameTaken`.
  - `TestUpdateAppMovesPartnerWithDomain` — `example.com`+www → `-domain new.com` sonrası `Names` `["www.new.com"]`.
  - `TestUpdateAppRejectsPartnerConflict` — www açılınca eş başkasının açık adıyla çakışıyor → `ErrNameTaken`, kayıt DEĞİŞMEZ.
  - `TestUpdateAppRedirectAddRemove` — ekle `a.com`, çıkar `b.com` (olmayan) → sessiz; `ChangesNames` doğru değeri verir.
  - `TestActiveDeploymentsCarryNames` — `Deployment.WWW` ve `Redirects` dolu.
- [ ] **Adım 2:** `buf generate && buf lint`; `go test ./internal/store/ -run 'WWW|Redirect|Partner|Names|Migration' -count=1` → FAIL (alanlar yok).
- [ ] **Adım 3:** Uygula (proto → göç → App/AppUpdate/Deployment → Create/Update/ActiveDeployments). Yedek/geri yükleme ve anlık görüntü kodu sütunları adıyla sayıyorsa (`snapshot.go`, `restore.go`) yeni sütunları ekle; `go test ./internal/store/ -count=1` hepsi yeşil kalmalı.
- [ ] **Adım 4:** `go test ./internal/store/ ./internal/api/ -count=1 -race` → PASS (API henüz yeni alanları kullanmıyor ama derlenmeli).
- [ ] **Adım 5:** `git add proto/ internal/pb/ internal/store/` → `git commit -m "update app redirect storage"`

### Görev 3: API — doğrulama, dönüşüm, uzlaştırma tetiği

**Dosyalar:**
- Değiştir: `internal/api/appvalidate.go`, `internal/api/apps.go`, `internal/api/appupdate.go`
- Test: `internal/api/appredirects_test.go` (yeni)

**Arayüzler:**
- Tüketir: Görev 2'nin proto/depo alanları, `store.ErrNameTaken`, `appnames.Names`.
- Üretir:
  - `const maxRedirects = 8`; `func validateRedirects(domain string, names []string) error` — her ad `validateDomain`'den geçer; `domain`'e eşit olamaz; tekrar yok; en çok 8; `domain == ""` iken ad varsa `"redirect names need a domain"`.
  - `validateAppSpec` bunu çağırır (create ve update'in birleşik tanımı için).
  - Update: aynı ad hem `redirect_add` hem `redirect_remove`'da ise `"%q is both added and removed — say which one you want"` (env deseni).
  - `appFromProto`: `WWW: spec.GetWww()`, `Redirects: spec.GetRedirects()`. `appToProto`: `Www: proto.Bool(a.WWW)` HER ZAMAN dolu (yeni sunucunun işareti), `Redirects: a.Redirects`.
  - `appError`: `store.ErrNameTaken` → `codes.AlreadyExists`.
  - `UpdateApp`: `namesChanged := upd.ChangesNames(current)`; `domainMoved || replicasChanged || namesChanged` ise uzlaştırma. Yalnız adlar değiştiyse `ProxyDetail` yerine `RedirectDetail` doldurulur: başarıda `"reverse proxy updated — redirects: <adlar virgülle> → <domain>"`, `Result.SkippedRedirects`'te bu uygulamanın adları varsa `"redirect <ad> NOT applied (<sebep>)"` satırları. Uzlaştırma hatası `moveTraffic`'in "SAVED but …" deseniyle.
  - Denetim parametreleri: create'te `www` (`"true"/"false"`) ve `redirects` (virgüllü); update'te `www`, `redirect_add`, `redirect_remove`.
- `Result.SkippedRedirects` Görev 5'te eklenir; bu görevde `deploy.Result`'a alanı (boş harita) ekle ki derlensin, doldurmayı Görev 5 yapar.

- [ ] **Adım 1: Başarısız testler:**
  - `TestValidateRedirects` tablo: büyük harf, IP, kendi domaini, tekrar, 9 ad, domain boşken ad → hata; 8 geçerli ad → nil.
  - `TestCreateAppEchoesWWW` — `Www` gönderilmeyen create'in cevabında `Www != nil && !*Www` (alan her zaman dolu).
  - `TestCreateAppNameConflictIsAlreadyExists` — gRPC kodu `AlreadyExists`.
  - `TestUpdateAppNamesOnlyReconciles` — sahte uzlaştırıcı çağrılır, `RedirectDetail` adları içerir, `ProxyDetail` boş.
  - `TestUpdateAppAddAndRemoveSameRedirect` → `InvalidArgument`.
  - `TestUpdateAppAuditCarriesRedirects` — denetim kaydında `redirect_add`.
- [ ] **Adım 2:** `go test ./internal/api/ -run 'Redirect|WWW|NameConflict' -count=1` → FAIL.
- [ ] **Adım 3:** Uygula.
- [ ] **Adım 4:** `go test ./internal/api/ ./internal/deploy/ -count=1 -race` → PASS.
- [ ] **Adım 5:** `git add internal/api/ internal/deploy/reconcile.go` → `git commit -m "update app redirect api"`

### Görev 4: Ters vekil aracısı — yönlendirme rotası (GÜVENLİK SINIRI)

**Dosyalar:**
- Değiştir: `internal/proxydrv/config.go`, `internal/proxydrv/routes.go`, `internal/proxydrv/client.go`
- Oluştur: `internal/proxydrv/redirects_test.go`, `internal/proxydrv/testdata/redirect-route.json`
- Değiştir: `internal/proxyguard/guard_test.go` (yeni durumlar)
- Oluştur: `build/caddy/redirect_route_test.go` (gerçek Caddy)

**Arayüzler:**
- `Handler`'a `Headers map[string][]string \`json:"headers,omitempty"\``.
- `AppRoute`'a `Redirects []string` (bu rotanın domainine yönlenecek adlar, verilen sırayla).
- `const redirectStatus = 308`; `func redirectLocation(to string) string` → `"https://" + to + "{http.request.uri}"`.
- `BuildConfig`: domain rotalarından SONRA, `AppID` sırasıyla her rotanın her yönlendirme adı için `Route{Match: [{Host: [from]}], Handle: [{Handler: "static_response", StatusCode: 308, Headers: {"Location": [redirectLocation(domain)]}}], Terminal: true}`. Bir ad herhangi bir domaine ya da önceki bir yönlendirmeye eşitse HATA (kesin; filtreleme Görev 5'te uzlaştırıcıda). Geçersiz ad (`ValidDomain`) HATA.
- `RouteSet`: `Redirects []ProxyRedirect \`json:"redirects,omitempty"\``; `type ProxyRedirect struct { From string \`json:"from"\`; To string \`json:"to"\` }`; `const MaxRedirects = 1024`.
- `FixedConfig(set)` ek kurallar (kadrand güvenilmez): `len(set.Redirects) <= MaxRedirects`; her `From`/`To` `ValidDomain`; `To` kümedeki bir `Routes[i].Domain`'e eşit (değilse `"redirect target %q is not a routed domain"`); `From` hiçbir domaine eşit değil; `From` tekrar yok. Yönlendirmeler `To`'ya göre ilgili `AppRoute.Redirects`'e girdi sırasıyla eklenir.
- `SafeRoutes`: rota `len(Match)==1 && len(Host)==1 && len(Handle)==1 && Handle[0].Handler=="static_response"` ise yönlendirme: `Headers` yalnız `Location` anahtarı, tek değer, `"https://"` öneki ve `"{http.request.uri}"` sonekiyle; ortası `To`. `ProxyRedirect{From: host, To}` olarak toplanır. Diğer her şey bugünkü gibi; sonunda `FixedConfig` + `DeepEqual` fazladan alanı (gövde, başka başlık, durum kodu) yakalar.
- `func LiveRedirects(live *Config, to string) []string` (`client.go`, `LiveUpstreams`'in yanında): canlı yapılandırmada `to`'ya yönlenen `From`'lar, rota sırasıyla.
- `testdata/redirect-route.json`: `From www.example.com`, `To example.com` için üretilen TEK rota nesnesi (girintili JSON).

- [ ] **Adım 1: Başarısız testler** (`redirects_test.go`):
  - `TestBuildConfigEmitsRedirectRoute` — üretilen yönlendirme rotası `json.Marshal` ile `testdata/redirect-route.json`'un sıkıştırılmış hâline eşit.
  - `TestFixedConfigRejectsRedirectToUnroutedDomain` — `To: "evil.example"` → hata, mesajda `not a routed domain`.
  - `TestFixedConfigRejectsRedirectFromRoutedDomain`, `TestFixedConfigRejectsDuplicateRedirect`, `TestFixedConfigRejectsTooManyRedirects` (1025), `TestFixedConfigRejectsInvalidRedirectName` (`"A.com"`, `"1.2.3.4"`, `"a{b}.com"`).
  - `TestSafeRoutesRoundTripsRedirects` — `FixedConfig(set)` → `SafeRoutes` → aynı `set`.
  - `TestSafeRoutesRejectsTamperedRedirect` — canlı yapılandırmada yönlendirme rotasına (a) `Body: "x"`, (b) ikinci başlık `Set-Cookie`, (c) Location `"https://example.com/{file./etc/passwd}"`, (d) `StatusCode: 302` → her biri hata.
  - `TestRouteSetWithoutRedirectsKeepsWireFormat` — yönlendirmesiz kümenin JSON'unda `"redirects"` anahtarı YOK.
  - `TestLiveRedirects` — iki yönlendirmeli canlı yapılandırmadan doğru liste.
  - `guard_test.go`: aracı `To` kümede olmayan isteği reddeder (HTTP 400/502 — mevcut ret kodu neyse); geçerli yönlendirmeli küme yüklenir ve cevapta geri döner.
  - `build/caddy/redirect_route_test.go`: `TestRedirectRouteGoldenInRealCaddy` — `../../internal/proxydrv/testdata/redirect-route.json`'u okur; `127.0.0.1:<boş port>` dinleyen, `"automatic_https": {"disable": true}` bir sunucuya bu rotayı koyup `caddy.Load` eder; `Host: www.example.com`, `GET /a?b=1` (yönlendirme izlenmeden) → `308`, `Location: https://example.com/a?b=1`; `Host: other.example` → `308` DEĞİL. `t.Cleanup(caddy.Stop)`.
- [ ] **Adım 2:** `go test ./internal/proxydrv/ ./internal/proxyguard/ -count=1` → FAIL; `(cd build/caddy && go test -run RedirectRoute -count=1 .)` → FAIL (dosya yok).
- [ ] **Adım 3:** Uygula. `build/caddy/go.mod`/`go.sum` DEĞİŞMEMELİ (yalnız mevcut bağımlılıklar).
- [ ] **Adım 4:** `go test ./internal/proxydrv/ ./internal/proxyguard/ ./internal/deploy/ -count=1 -race` ve `(cd build/caddy && go test -race -count=1 ./...)` → PASS.
- [ ] **Adım 5:** `git add internal/proxydrv/ internal/proxyguard/ build/caddy/redirect_route_test.go` → `git commit -m "update proxy redirect routes"`

### Görev 5: Uzlaştırma — adlar, çakışma, taşıma

**Dosyalar:**
- Değiştir: `internal/deploy/reconcile.go`
- Test: `internal/deploy/reconcile_redirects_test.go` (yeni)

**Arayüzler:**
- Tüketir: `appnames.Names`, `store.Deployment.WWW/Redirects`, `proxydrv.AppRoute.Redirects`, `proxydrv.LiveRedirects`.
- Üretir: `Result.SkippedRedirects map[string]string` (ad → sebep; Görev 3'te tanımlandı, burada doldurulur).
- `plan()`: aktif dağıtımlar `AppID` sırasıyla; önce BÜTÜN domainler (rotalanan ve atlanan) bir kümeye; sonra rotalanan her uygulama için `appnames.Names(d.Domain, d.WWW, d.Redirects)`: ad `ValidDomain` değilse, bir domainle ya da daha önce kabul edilmiş bir yönlendirmeyle çakışıyorsa `SkippedRedirects[ad]` (`"collides with the domain of app %q"` / `"already redirects to %q"` / `"invalid name"`) ve atlanır; diğerleri `AppRoute.Redirects`'e.
- `carrySkippedRoutes`: taşınan canlı rota `LiveRedirects(live, domain)` ile kendi canlı yönlendirmelerini de taşır (aynı çakışma filtresinden geçerek).
- `Skipped` (uygulama → sebep) yönlendirmeler için ASLA yazılmaz.

- [ ] **Adım 1: Başarısız testler:**
  - `TestReconcileAddsRedirectRoutes` — www açık uygulama → yüklenen yapılandırmada `www.example.com` yönlendirme rotası.
  - `TestReconcileSkipsOnlyTheCollidingRedirect` — `b`'nin açık adı `a`'nın domaini → iki domain rotası da var, yönlendirme yok, `SkippedRedirects["a.example.com"]` dolu, `Skipped` boş.
  - `TestReconcileSkipsPartnerOfAnotherApp` — iki uygulamanın açık adı aynı → ilki kalır, ikincisi atlanır.
  - `TestReconcileCarriesLiveRedirectsOfSkippedApp` — sağlıksız yeni sürüm: canlı domain rotası ve canlı `www` yönlendirmesi korunur.
- [ ] **Adım 2:** `go test ./internal/deploy/ -run 'Redirect' -count=1` → FAIL.
- [ ] **Adım 3:** Uygula.
- [ ] **Adım 4:** `go test ./internal/deploy/ ./internal/api/ -count=1 -race` → PASS.
- [ ] **Adım 5:** `git add internal/deploy/` → `git commit -m "update reconcile redirects"`

### Görev 6: CLI — bayraklar, gösterim, DNS uyarısı

**Dosyalar:**
- Değiştir: `cmd/kadran/app.go`, `cmd/kadran/appupdate.go`
- Oluştur: `cmd/kadran/appredirects.go`, `cmd/kadran/appredirects_test.go`

**Arayüzler:**
- `app create`: `-no-www` (bool, varsayılan false), `-redirect` (tekrarlanabilir, `c.stringSliceFlag`). İstek: domain doluysa `Www: proto.Bool(!*noWWW)`, `Redirects`.
- `app update`: `-www` ve `-no-www` (ikisi birden → kullanım hatası `"-www and -no-www cannot be used together"`), `-redirect` (ekle), `-redirect-rm` (çıkar). İstek alanları yalnız bayrak verildiyse.
- `func redirectsLine(spec *kadranv1.AppSpec) string` → boşsa `""`, değilse `"  Redirects: www.example.com, old.com → example.com (308)\n"` (`appnames.Names`'in sırası). `app show`, create ve update sonuçları basar.
- `func serverKnowsRedirects(spec *kadranv1.AppSpec) bool` → `spec.Www != nil`. İstek www/yönlendirme taşıyıp cevap `false` ise stderr: `kadran: warning: this server does not support redirects (www, -redirect); upgrade kadrand`; Redirects satırı basılmaz.
- `func (c *cli) checkRedirectNames(ctx context.Context, names []string, rawTarget string, skip bool)`: `skip` ise hiçbir şey; değilse adlar PARALEL `c.dnsChecker().Check`; Stop ya da Warn olan her ad için stderr'e `kadran: warning: <ad> does not point at the server yet; it is redirected once it does and gets its own certificate` + `writeDNSGuide` kayıt bloğu. Çıkış kodunu ASLA değiştirmez. Sunucunun cevabından sonra (cevaptaki birleşik tanımın adlarıyla) çağrılır.

- [ ] **Adım 1: Başarısız testler:**
  - `TestAppCreateSendsWWWByDefault` / `TestAppCreateNoWWW` / `TestAppCreateRepeatsRedirect` — istek alanları.
  - `TestAppUpdateWWWAndNoWWWConflict` → `exitUsage`.
  - `TestAppUpdateRedirectRm` — `RedirectRemove` dolu.
  - `TestAppShowPrintsRedirects` — çıktıda `Redirects: www.example.com, old.com → example.com (308)`.
  - `TestAppCreateWarnsOnOldServer` — sahte sunucu cevapta `Www` nil → stderr uyarısı, `Redirects:` satırı yok, çıkış 0.
  - `TestAppCreateRedirectDNSWarnsButSucceeds` — `www.example.com` yanlış adresi gösteriyor → çıkış 0, stderr'de `www.example.com does not point at the server` ve `A     www`.
  - `TestAppCreateSkipDNSCheckSkipsRedirects` — `-skip-dns-check` → yönlendirme uyarısı yok.
  - `TestRedirectChecksRunInParallel` — iki adın çözümleyici sahtesi ikinci çağrı gelene kadar bekleyen bir engel (`sync.WaitGroup`) kullanır; 2 sn içinde bitmezse FAIL.
  - `TestAppCreateSubdomainHasNoWWW` — `-domain app.example.com` → Redirects satırı yok, www için DNS denetimi yok.
- [ ] **Adım 2:** `go test ./cmd/kadran/ -run 'WWW|Redirect|OldServer' -count=1` → FAIL.
- [ ] **Adım 3:** Uygula.
- [ ] **Adım 4:** `go test ./cmd/kadran/ -count=1 -race` → PASS (english_test dahil).
- [ ] **Adım 5:** `git add cmd/kadran/` → `git commit -m "update app redirect flags"`

### Görev 7: Mutasyon betiği

**Dosyalar:**
- Oluştur: `scripts/mutate-redirects.sh` (`scripts/mutate-domaincheck.sh`'ın düzeneği: tek eşleşme, yeşil taban, derleme kapısı, geri yükleme kanıtı)

Mutantlar (her iğne hedef dosyada TAM BİR kez; ad Türkçe):
- `routes.go` "yönlendirme hedefi kümede aranmıyor" (`To` denetimi kapalı), "yönlendirme adı domainle çakışabiliyor", "yönlendirme sınırı yok" (`MaxRedirects` kontrolü kapalı).
- `routes.go`/SafeRoutes "Location şablonu gevşek" (önek ya da sonek kontrolü kapalı).
- `config.go` "yönlendirme durum kodu 302".
- `reconcile.go` "çakışan yönlendirme bütün kümeyi düşürüyor" (filtre kapalı), "atlanan uygulamanın yönlendirmesi taşınmıyor".
- `appnames.go` "www eşi alt ada da türetiliyor", "kökün eşi türetilmiyor".
- `store` "ad çakışması denetlenmiyor"; göç "mevcut uygulamalar www ile başlıyor" (`DEFAULT 0` → `DEFAULT 1`).
- `appvalidate.go` "yönlendirme sınırı 99".
- `TESTS` düzeni bu plandaki bütün yeni test adlarını kapsamalı; betiğin sonunda kapsanmayan test adı kalmadığını gösteren denetim de koşulur (`go test -list` çıktısını düzenle karşılaştır, yalnız yeni dosyalardaki testler için).

- [ ] **Adım 1:** Betiği yaz; iğne denetleyicisi (her `mutate_in` iğnesi tam bir kez) yeşil.
- [ ] **Adım 2:** Betiği TEK BAŞINA koştur (Windows Git Bash, Go PATH): `bash scripts/mutate-redirects.sh` → `Bütün mutasyonlar yakalandı.` Yakalanmayan mutant için TESTİ güçlendir.
- [ ] **Adım 3:** `git add scripts/mutate-redirects.sh` (+ güçlendirilen test) → `git commit -m "update redirect mutation script"`

### Görev 8: Belgeler

**Dosyalar:**
- Değiştir: `docs/guides/domains.md` ("www and other names" bölümü: otomatik www kuralı, `-no-www`, `-redirect`/`-redirect-rm`, `-www`, kayıt örneği `A www <server>` ya da `CNAME www → example.com`, her adın kendi sertifikası, alt adda www yok), `README.md` (bir cümle), `CHANGELOG.md` (`## Unreleased` → `### Added`), `docs/decisions.md` (K-152: spec'teki her **(karar)** ve 20 Ekim ölçümü)

- [ ] **Adım 1:** Yaz. Metin koddaki çıktıyla birebir (uydurma çıktı yok).
- [ ] **Adım 2:** `go test ./internal/domaincheck/ -run DomainsGuide -count=1` → PASS.
- [ ] **Adım 3:** `git add docs/ README.md CHANGELOG.md` → `git commit -m "update redirect docs"`

### Görev 9: Son denetim

- [ ] `go test ./... -count=1 -race`, `go vet ./...`, `golangci-lint run ./...` (0), `bash scripts/check-exec-surface.sh` (değişmedi), `(cd build/caddy && go test -race -count=1 ./...)`, `buf lint`.
- [ ] `bash scripts/mutate-redirects.sh` ve `bash scripts/mutate-domaincheck.sh` (ikisi AYRI AYRI, tek tek).
- [ ] Push/PR için kullanıcıya SOR. Canlı doğrulama (Hetzner'de www yönlendirmesi) ayrı onayla, C projesiyle birlikte.
