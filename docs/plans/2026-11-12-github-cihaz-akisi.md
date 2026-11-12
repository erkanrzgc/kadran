# GitHub cihaz akışı — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Kadran'ın özel GitHub depolarını tek adımlık bir cihaz akışıyla bağlaması (`kadran github connect`). Jeton yetkisiz bir birimde (`kadran-git`) durur; root yürütücü ağa çıkmaz.

**Architecture:**
- Yeni ikili `kadran-git`, systemd soket etkinleştirmesiyle iki soket dinler:
  - `admin.sock`, yalnız kadrand'ye açık;
  - `fetch.sock`, yalnız root yürütücüye açık.
- `kadran-git`'in işleri:
  - cihaz akışı;
  - jeton deposu ve 8 saatlik yenileme;
  - kurulumun depo listesi;
  - dal çözümü;
  - arşiv indirme.
- Yürütücü, yalnız kurulum listesinde `private: true` olan depoların arşivini `fetch.sock`'tan alır ve bugünkü arşiv denetimleriyle derler. Geri kalan her depo bugünkü yolda kalır.
- kadrand, CLI ve masaüstü yalnız kodu ve durumu görür.

**Tech Stack:**
- Go 1.26.11, gRPC + protobuf (`buf generate`, yerel eklentiler);
- systemd soket birimleri;
- React + Wails (masaüstü);
- bash mutasyon betikleri.

**Spec:** `docs/specs/2026-11-12-github-cihaz-akisi-design.md` (K-167). Plan spec'ten tartışır; ikisi birlikte okunur.

## Global Constraints

- **Uygulama bilgileri koddaki sabitler:**
  - Client ID `Iv23lizBm3zlCdYH3NQ5`;
  - kısa ad `kadran-deploy`;
  - kurulum adresi `https://github.com/apps/kadran-deploy/installations/new`.
  - Üzerine yazmak için `kadran-git --client-id` ve `--app-slug` var; birim dosyası bunları geçmez.
- **Bağlanılan hostlar yalnız sabitler:** `github.com`, `api.github.com`, `codeload.github.com`.
  - İstekten adres alınmaz.
  - Testler uç noktaları yalnız dışa açılmamış bir alan üzerinden değiştirir; bayrak ya da ortam değişkeni yok.
- **Jeton (`ghu_`/`ghr_`), cihaz kodu ve imzalı indirme adresi asla görünmez:**
  - günlükte;
  - hata metninde;
  - denetim kaydında;
  - RPC yanıtında;
  - iş istasyonunda.
  - Kullanıcı kodu (8 hane) ve doğrulama adresi bu yasağın dışında: kullanıcıya gösterilmek için var.
- **İstemci sırrı yok.** Cihaz akışı ve yenileme istekleri `client_secret` göndermez.
- **Yenileme tek uçuş.** Yeni çift diske atomik yazılmadan kullanılmaz; yazma hatası bağlantıyı koparır.
- **Tar yolu yalnız kurulum listesinde `private: true` olan depolar için.**
  - Karar asla tarball'ın HTTP durum koduna göre verilmez.
  - Tar yolundaki her hata derlemeyi düşürür; anonim yola düşülmez.
- **`kadran-git` çökse de herkese açık derlemeler etkilenmez.**
  - İlk karar iletisinden ÖNCEKİ her başarısızlık bugünkü herkese açık yola düşer. Bağlanma ve ilk iletinin toplam süre sınırı 5 sn.
  - Bu başarısızlıklar şunlar: bağlanılamıyor, soket kabul ediyor ama cevap yok (yeniden başlatma sınırı), zaman aşımı, karardan önce gRPC hatası.
  - Bu güvenli: herkese açık yol kimlik bilgisi taşımaz; özel depo Docker'da ipucuyla düşer.
  - Yalnız `DECISION_ARCHIVE` geldikten SONRAKİ hatalar derlemeyi düşürür.
- **Yürütücüye eklenen her ayar koddaki varsayılan olur, birimin `ExecStart`'ına yazılmaz.** İki canlı sunucudaki `--allow-repo` drop-in'i `ExecStart`'ı yeniden yazıyor.
- **Yürütücünün ayrıcalıklı satır sayısı (`scripts/check-exec-surface.sh`) düşmeli.** Yeni sayı ölçülür, `MAX_EXEC_LINES` ona çekilir.
- **Protokol 3 kalır.** Değişiklikler eklemeli. Eski sunucu `Unimplemented` döner; istemci "upgrade the server" der.
- **Yeni kadrand çağrıları yalnız yöneticiye açık** (K-131 izin listesi `deployAllowed`'a EKLENMEZ).
- **Metinler:** sunucu hata metinleri İngilizce (K-141); kod yorumları Türkçe (deponun deseni). Masaüstü metinleri en/tr.
- **Dosya ve fonksiyon boyu:** dosya ≤ 800 satır, fonksiyon ≤ 50 satır, iç içe derinlik ≤ 4.
- **Commit mesajı:** her commit `update <birkaç İngilizce kelime>`, tek satır, gövde ve atıf satırı yok. Yazar depo yerel yapılandırmasındaki gibi.
- **Ortam:**
  - `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:$PATH"`;
  - her dal değişiminden sonra `buf generate`;
  - proto değişince `buf lint` de koşulur;
  - mutasyon betikleri tek tek koşulur, commit sırasında koşmaz.
- **Canlıya dokunan her adım** (sunucu, push, PR, birleştirme) ayrı kullanıcı onayı ister ve öncesinde kısa bir durum satırı verilir.

## Review Focus

0. **`kadran-git` çöküyor, yeniden başlatma sınırına takıldı ya da soket kabul edip cevap vermiyor.**
   - portfolio, web ve pfprobe gibi herkese açık uygulamaların derlemesi 5 sn içinde bugünkü yola düşmeli; asılı kalmamalı, düşmemeli.
   - Test: Görev 8 `TestPublicBuildSurvivesUnresponsiveGitService`: kabul eden ama hiç yazmayan dinleyici kullanılır.
1. **Uygulama hiç kurulmamışken cihaz kodu onaylandı.**
   - Bağlantı kurulur ama kurulum listesi boştur.
   - `status`, kurulum adresini ve "no installation" uyarısını göstermeli.
   - Özel deponun dağıtımı Docker hatası + ipucuyla düşer, asılı kalmaz.
   - Test: Görev 5 `TestStatusWithoutInstallationShowsInstallURL`.
2. **Akış sürerken `kadran-git` yeniden başladı** (sunucu yeniden başlatıldı ya da birim çöktü).
   - Bekleyen akış kaybolur.
   - `kadran github connect` sonsuza dek beklememeli: durum `NOT_CONNECTED`'a döner ya da kodun süresi geçerse "code expired, run connect again" der ve 1 ile çıkar.
   - Test: Görev 10 `TestConnectStopsWhenPendingFlowDisappears`.
3. **Yerel saate göre geçerli jeton GitHub'dan 401 aldı** (saat kayması ya da kullanıcı yetkiyi geri aldı).
   - Bir kez zorunlu yenileme ve tek tekrar.
   - Yine 401 ise derleme düşer, durum "reconnect" der.
   - Test: Görev 4 `TestUnauthorizedForcesOneRefresh`.
4. **Eğik çizgili dal adı** (`feature/x`).
   - Dal çözümü çalışmalı.
   - `..`, `//`, baştaki `-` ya da `/`, sondaki `.lock` reddedilmeli.
   - Test: Görev 5 `TestResolveBranchAcceptsSlashes` / `TestResolveBranchRejectsUnsafeNames`.
Görev 3'teki `TestMovedRepositoryIsReported` (taşınan depo, 301) yine yazılır. Burada değil, çünkü kapsanıyor.

## Yürütme kuralları (çok ajanlı; kullanıcı, 12 Kasım)

- **Uygulayıcılar SIRAYLA çalışır,** görev başına bir uygulayıcı.
  - Görevler zincir: şema → istemci, jeton, servis → artalan süreci → yürütücü ve kadrand → CLI ve masaüstü.
  - Aynı çalışma ağacında paralel uygulayıcılar `buf generate`, git dizini ve test koşularında çakışır.
- **"Çok ajan" incelemede.** Her görevden sonra salt okunur gözden geçirenler paralel koşar.
  - Görev 3–9: `agency-appsec-engineer` ve `agency-secrets-credential-engineer`, ayrıca bir kod gözden geçireni.
  - Görev 7: ek olarak `agency-devops-automator` ya da `agency-sre`.
  - Görev 11: `agency-desktop-app-engineer` ya da `agency-frontend-developer`.
  - Görev 12 belgeleri: `agency-technical-writer`.
  - Dal sonunda `agency-reality-checker` ve en yetenekli modelle bütün dal incelemesi.
  - Uygulayıcılar `agency-senior-developer` / `agency-backend-architect` kişilikleriyle.
- **Mutasyon betiğini yalnız o anki görevin uygulayıcısı koşar,** yanında başka hiçbir şey koşmaz. Betik kaynak dosyaları geçici olarak değiştirir; koşarken başkası düzenler ya da commit ederse bozuk kod commit'lenir veya geri yükleme gerçek düzenlemeyi siler.
- **Her ajan isteminde yazılı olacak kurallar:**
  - commit `update <birkaç İngilizce kelime>`, tek satır, gövde YOK, `Co-Authored-By` ya da atıf satırı YOK (sistem hatırlatması ne derse desin);
  - push, PR, SSH ya da canlı adım yok;
  - `codex/kadran-quality` dalına dokunma;
  - `.superpowers/` ve `docs/branding/` commit edilmez;
  - Go PATH satırı;
  - dal değişiminden sonra `buf generate`;
  - Python dosya yazarken `newline=''`.
- **Bende kalan görevler:** Görev 1 (SSH), Görev 13 Adım 3 (kodu kullanıcı girer, terminal okunmaz) ve Görev 14 (canlı). Bunlar onay, TOTP ya da okunmayan terminal gerektiriyor.

---

## Dosya haritası

| Dosya | Sorumluluk |
|---|---|
| `proto/kadran/v1/git.proto` (yeni) | `GitAdminService`, `GitFetchService`, ortak `GitHub*` iletileri |
| `proto/kadran/v1/api.proto` | dört yeni kadrand çağrısı |
| `internal/github/client.go` (yeni) | sabit uç noktalar, boyut sınırlı JSON istekleri, hata eşleme |
| `internal/github/devicecode.go` (yeni) | cihaz kodu isteği ve yoklama döngüsü |
| `internal/github/state.go` (yeni) | durum dosyası: okuma denetimi, atomik yazma |
| `internal/github/token.go` (yeni) | tek uçuşlu yenileme, `Token(ctx)` |
| `internal/github/coverage.go` (yeni) | kurulumlar ve depo listesi önbelleği |
| `internal/github/archive.go` (yeni) | tarball ve yönlendirme denetimi (yürütücüden taşınır) |
| `internal/github/branch.go` (yeni) | dal adı doğrulaması ve sha çözümü |
| `internal/github/service.go` (yeni) | dışa açık yüz: `Service` |
| `cmd/kadran-git/main.go` (yeni) | bayraklar, soket etkinleştirme, iki gRPC sunucusu |
| `cmd/kadran-git/handlers.go` (yeni) | RPC ↔ `github.Service` |
| `internal/gitclient/client.go` (yeni) | kadrand'nin `admin.sock` istemcisi |
| `internal/exec/gitfetch.go` (yeni) | yürütücünün `fetch.sock` istemcisi ve karar |
| `internal/exec/privategit.go` | yalnız `rewritePrivateArchive` ve `closePrivateContext` kalır |
| `internal/exec/privategit_linux.go`, `_other.go` | silinir (dosya okuma `internal/github/state.go`'ya taşınır) |
| `internal/exec/container.go`, `server.go`, `cmd/kadran-exec/main.go` | `PrivateGit` → `GitFetcher`; `--git-credentials` kalkar |
| `internal/store/settings.go` (yeni) | `Setting` / `SetSetting` (tablo `0001_init.sql`'de zaten var) |
| `internal/api/github.go` (yeni) | dört kadrand çağrısı + denetim |
| `cmd/kadrand/main.go` | `gitclient` bağlantısı, varsayılan `/run/kadran-git/admin.sock` |
| `internal/gitref/commit.go` | dal çözümünde sunucuya düşme |
| `cmd/kadran/github.go` (yeni) | `kadran github connect|status|disconnect` |
| `internal/sidecar/github.go` (yeni) | `github.status`, `github.connect`, `github.disconnect` |
| `desktop/internal/bridge/github.go` (yeni) | Wails yöntemleri |
| `desktop/src/renderer/screens/GitHubScreen.tsx` (yeni) | GitHub bölümü |
| `desktop/src/renderer/lib/github.ts` (yeni) | yanıt ayrıştırma |
| `deploy/systemd/kadran-git.service`, `kadran-git-admin.socket`, `kadran-git-fetch.socket` (yeni) | birimler |
| `internal/bootstrap/install.sh` | kullanıcı, birimler, `--git-credentials` ön denetimi |
| `scripts/build-release.sh` | `kadran-git` sunucu ikilisi |
| `scripts/mutate-github.sh` (yeni) | mutasyonlar |
| `docs/guides/private-git.md`, `README.md`, `CHANGELOG.md`, `SECURITY.md`, `docs/decisions.md` | belgeler |

---

### Görev 1: İki sunucuda DNS ölçümü (salt okunur, canlı) — BİTTİ 12 Kasım

**Sonuç (K-167 "Ölçüm"):**
- `systemd-run` sembolik aralıkları (`localhost`, `link-local`) ayrıştırmıyor; ölçüm sayısal aralıklarla tekrarlandı. Birim dosyası sembolik adları kabul eder.
- Hetzner `resolv.conf` 127.0.0.53'ü gösteriyor; GCP doğrudan `169.254.169.254`'ü.
- Kısıtlı birimde Go'nun yolu (`getent -s dns`) GCP'de çözemiyor (çıkış 2). Kısıtlı birime `stub-resolv.conf` bağlanınca iki sunucuda da çözüyor.
- İki sunucuda kısıtlı birimden:
  - HTTPS `api.github.com` 200;
  - meta veri kapalı (kısıtsız kontrol 200);
  - 1.1.1.1 açık;
  - 127.0.0.1 kapalı.
- Görev 7'nin birimine `BindReadOnlyPaths=/run/systemd/resolve/stub-resolv.conf:/etc/resolv.conf` eklendi.

**Files:**
- Modify: `docs/decisions.md` (K-167'ye "Ölçüm" satırı).

- [ ] **Adım 1: Kullanıcıya kısa durum ver ve onay al.** İki sunucuya salt okunur SSH yapılacak, hiçbir şey değişmeyecek.
- [ ] **Adım 2: İki sunucuda (Hetzner `root@`, GCP `panely-kurulum@` + `sudo`) şunları koş.**

```sh
readlink -f /etc/resolv.conf; grep -E '^nameserver' /etc/resolv.conf
systemctl is-active systemd-resolved
resolvectl status 2>/dev/null | sed -n '1,15p'
systemctl show kadran-offsite.service -p ExecMainStatus -p ActiveEnterTimestamp
journalctl -u kadran-offsite -u kadran-notify --since -7d --no-pager | grep -ciE 'error|fail'
sudo systemd-run --wait --pipe -p IPAddressDeny='localhost link-local multicast' -p IPAddressAllow=127.0.0.53 getent hosts api.github.com
```

- Beklenen: `nameserver 127.0.0.53` ve `systemd-resolved` active; son satır bir IP basar.
- [ ] **Adım 3: Kontrol grubu.**
  - Aynı `getent` kısıtsız koşar, aynı adı çözer.
  - Kısıtlı koşu başarısızsa ama kısıtsız başarılıysa, kısıt DNS'i kesiyor demektir.
- [ ] **Adım 4: Sonucu K-167'ye yaz.**
  - İki sunucu da 127.0.0.53 ise birim satırları spec'teki gibi kalır.
  - GCP `169.254.169.254` kullanıyorsa Görev 7'deki birime `IPAddressAllow=169.254.169.254/32` YAZILMAZ. Kurulum betiği `resolv.conf`'ta link-local çözücü görürse açık bir mesajla durur ve `systemd-resolved`'ı önerir.
  - Seçilen yol K-167'de gerekçesiyle yazılır.
- [ ] **Adım 5: Commit.** `git commit -m "update dns measurement"`

### Görev 2: Şema

**Files:**
- Create: `proto/kadran/v1/git.proto`
- Modify: `proto/kadran/v1/api.proto`

**Interfaces — Produces** (üretilen Go adları `kadranv1` paketinde):

```proto
service GitAdminService {
  rpc StartDeviceFlow(StartDeviceFlowRequest) returns (StartDeviceFlowResponse); // DeviceCode
  rpc GetGitStatus(GetGitStatusRequest) returns (GetGitStatusResponse);          // GitHubConnection
  rpc DisconnectGit(DisconnectGitRequest) returns (DisconnectGitResponse);
  rpc ResolveBranch(ResolveBranchRequest) returns (ResolveBranchResponse);       // owner, repo, branch -> commit_sha
}
service GitFetchService {
  rpc FetchArchive(FetchArchiveRequest) returns (stream FetchArchiveResponse);   // owner, repo, commit_sha
}
message GitHubUserCodePrompt { string user_code = 1; string verification_uri = 2; string install_url = 3; google.protobuf.Timestamp expires_at = 4; }
message GitHubRepository { string full_name = 1; bool private = 2; }
message GitHubInstallation { string account = 1; bool all_repositories = 2; repeated GitHubRepository repositories = 3; bool truncated = 4; }
message GitHubConnection {
  enum State { STATE_UNSPECIFIED = 0; STATE_NOT_CONNECTED = 1; STATE_PENDING = 2; STATE_CONNECTED = 3; STATE_FAILED = 4; }
  State state = 1; string login = 2; GitHubUserCodePrompt pending = 3; repeated GitHubInstallation installations = 4;
  google.protobuf.Timestamp connected_at = 5; GitHubFailure failure = 6; bool reconnect_required = 7; string install_url = 8;
}
message FetchArchiveResponse {
  enum Decision { DECISION_UNSPECIFIED = 0; DECISION_ARCHIVE = 1; DECISION_NOT_CONNECTED = 2; DECISION_NOT_COVERED = 3; }
  oneof kind { Decision decision = 1; bytes chunk = 2; }  // ilk ileti karar; ARCHIVE ise ardından parçalar
}
```

`api.proto`'ya eklenenler (`KadranService`):
- `StartGitHubConnect` → `GitHubUserCodePrompt`;
- `GetGitHubStatus` → `GitHubConnection`;
- `DisconnectGitHub`;
- `ResolveGitHubBranch(app_id, branch)` → `commit_sha`.

- [ ] **Adım 1: İki dosyayı yaz.** Her çağrının üstünde Türkçe yorum olsun: kim çağırır, ne döner, jeton neden yok.
- [ ] **Adım 2:** Koş: `buf lint && buf generate`. Beklenen: çıktı yok, `internal/pb/kadran/v1/git.pb.go` ve `git_grpc.pb.go` oluşur.
- [ ] **Adım 3:** Koş: `go build ./...`. Beklenen: geçmez, çünkü `api.Server` yeni yöntemleri uygulamıyor. Bu doğru.
  - Görev 9'a kadar `internal/api/github.go`'da dört yöntemi `codes.Unimplemented` dönen taslaklar olarak ekle.
  - Sonra `go build ./... && go test ./internal/api/...` geçer.
- [ ] **Adım 4:** `scripts/check-exec-surface.sh` geçer: `exec.proto` değişmedi.
- [ ] **Adım 5: Commit.** `git commit -m "update github proto"`

### Görev 3: GitHub HTTP istemcisi ve arşiv

**Files:**
- Create: `internal/github/client.go`, `internal/github/archive.go`
- Test: `internal/github/client_test.go`, `internal/github/archive_test.go`
- Taşı: `internal/exec/privategit_test.go` içindeki yönlendirme testleri → `archive_test.go`

**Interfaces — Produces:**

```go
type endpoints struct{ login, api string }        // dışa açık değil; varsayılan https://github.com, https://api.github.com
type client struct{ http *http.Client; ep endpoints; now func() time.Time }
func newClient(ep endpoints, h *http.Client) *client
func (c *client) postForm(ctx context.Context, url string, form url.Values, out any) error  // Accept: application/json, gövde ≤ 64 KiB
func (c *client) getJSON(ctx context.Context, path, token string, out any) error           // api host'una, ≤ 1 MiB
var ErrUnauthorized, ErrNotFound, ErrMoved, ErrRateLimited error                            // durum koduna göre eşleme
func (c *client) tarball(ctx context.Context, owner, repo, sha, token string) (io.ReadCloser, error)
func validArchiveRedirect(raw, owner, repo, sha string) bool                                 // exec'teki kural aynen
```

- [ ] **Adım 1: Başarısız testleri yaz (`httptest.Server`).**
  - `TestGetJSONRejectsOversizedBody`: 1 MiB + 1 bayt → hata.
  - `TestErrorTextNeverContainsToken`: sunucu 500 döner ve gövdesinde `ghu_SECRET` geçer. `err.Error()` `ghu_` İÇERMEZ.
  - `TestMovedRepositoryIsReported`: 301 → `errors.Is(err, ErrMoved)`. Metin "repository moved: update the app definition" içerir.
  - `TestTarballFollowsOnlyCodeloadRedirect`: 302 ile `https://codeload.github.com/o/r/legacy.tar.gz/<sha>?token=x` gelince ikinci istek Authorization başlığı TAŞIMAZ. Başka bir host'a yönlendirme reddedilir.
  - `TestRedirectRules`: `privategit_test.go`'daki yönlendirme tablosu aynen taşınır.
  - `TestRateLimitedIsDistinct`: 403 + `X-RateLimit-Remaining: 0` → `ErrRateLimited`.
- [ ] **Adım 2:** Koş: `go test ./internal/github/ -run 'TestGetJSON|TestErrorText|TestMoved|TestTarball|TestRedirect|TestRateLimited' -v`. Beklenen: derleme hatası (paket yok).
- [ ] **Adım 3: Uygula.**
  - `http.Client`'ın `CheckRedirect`'i `http.ErrUseLastResponse` döner; yönlendirmeyi `tarball` kendisi doğrular.
  - Taşıma hataları sabit bir metne çevrilir (adres ve sorgu metne girmez; bugünkü `get()` deseni).
  - Zaman aşımları bugünkü değerler: bağlantı 10 sn, TLS 10 sn, başlık 15 sn, istek başına 2 dk.
- [ ] **Adım 4:** Aynı komut. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update github http client"`

### Görev 4: Durum dosyası ve jeton yenileme

**Files:**
- Create: `internal/github/state.go`, `internal/github/token.go`
- Test: `internal/github/state_test.go`, `internal/github/token_test.go`, `internal/github/state_linux_test.go`

**Interfaces — Produces:**

```go
type state struct {
  Version int `json:"version"`; ClientID string `json:"client_id"`; Login string `json:"login"`; UserID int64 `json:"user_id"`
  AccessToken string `json:"access_token"`; AccessExpiresAt time.Time `json:"access_expires_at"` // sıfır = süresiz
  RefreshToken string `json:"refresh_token"`; RefreshExpiresAt time.Time `json:"refresh_expires_at"`; ConnectedAt time.Time `json:"connected_at"`
}
func loadState(path, clientID string) (state, bool, error)   // bool: bağlı; client_id uyuşmazsa ya da dosya yoksa false; bozuksa false + günlük
func saveState(path string, s state) error                    // geçici dosya 0600, fsync, rename, dizin fsync
func removeState(path string) error
type tokens struct{ mu sync.Mutex; path, clientID string; c *client; now func() time.Time; st state; ok bool }
func (t *tokens) Token(ctx context.Context) (string, error)   // bitime < 5 dk ise önce yenile
func (t *tokens) ForceRefresh(ctx context.Context) (string, error)
var ErrNotConnected, ErrReconnect error
```

- [ ] **Adım 1: Başarısız testleri yaz.**
  - `TestRefreshPersistsBeforeUse`: sahte yenileme uç noktası yeni çifti döner; `saveState` yerine geçen kanca hata dönerse `Token` hata döner ve yeni erişim jetonunu DÖNMEZ. Ardından durum `ErrReconnect`.
  - `TestRefreshIsSingleFlight`: 20 eşzamanlı `Token` çağrısı → yenileme uç noktasına tam 1 istek.
  - `TestRefreshRotatesBothTokens`: dosyada yeni `refresh_token` var, eskisi yok.
  - `TestDeadRefreshTokenRequiresReconnect`: uç nokta `{"error":"bad_refresh_token"}` → `ErrReconnect`.
  - `TestNonExpiringTokenIsNeverRefreshed`: `AccessExpiresAt` sıfır → yenileme isteği yok.
  - `TestRefreshSendsNoClientSecret`: form alanlarında `client_secret` yok; `grant_type=refresh_token` ve `client_id=Iv23lizBm3zlCdYH3NQ5` var.
  - `TestClientIDMismatchMeansNotConnected`.
  - `TestUnauthorizedForcesOneRefresh` (Review Focus 3). Bu davranış `Service.Archive` ve `Service.ResolveBranch` içinde: ilk 401 → `ForceRefresh` → tek tekrar. İkinci 401 → `ErrReconnect`. Bu test Görev 5'teki servis yüzüyle yazılır, burada yer tutucu değil, Görev 5 Adım 1'e eklenir.
  - Linux: `TestStateFileRejectsSymlinkAndLoosePerms` (0644 ya da symlink → bağlı değil + hata günlüğü), `TestSavedStateIs0600`.
- [ ] **Adım 2:** Koş: `go test ./internal/github/ -run 'TestRefresh|TestDead|TestNonExpiring|TestClientID|TestState|TestSaved' -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.**
  - `loadState` bugünkü `readPrivateGitFile` sıkılığında: `O_NOFOLLOW`, normal dosya, kip tam 0600, ≤ 64 KiB. Fark: sahibi süreç kullanıcısı (`os.Geteuid()`), root değil.
  - Yenileme isteği `POST {login}/login/oauth/access_token`, form `client_id`, `grant_type=refresh_token`, `refresh_token`.
- [ ] **Adım 4:** Aynı komut. Beklenen: PASS. Ayrıca `go vet ./internal/github/`.
- [ ] **Adım 5: Commit.** `git commit -m "update github token store"`

### Görev 5: Cihaz akışı, kapsam, dal çözümü, servis yüzü

**Files:**
- Create: `internal/github/devicecode.go`, `internal/github/coverage.go`, `internal/github/branch.go`, `internal/github/service.go`
- Test: `internal/github/devicecode_test.go`, `internal/github/coverage_test.go`, `internal/github/branch_test.go`, `internal/github/service_test.go`

**Interfaces — Consumes:** `client` (Görev 3), `tokens`, `state`, `saveState` (Görev 4).

**Interfaces — Produces:**

```go
const DefaultClientID = "Iv23lizBm3zlCdYH3NQ5"; const DefaultAppSlug = "kadran-deploy"
type Config struct{ ClientID, AppSlug, StateFile string; Now func() time.Time; HTTP *http.Client; ep endpoints }
type Decision int  // DecisionArchive, DecisionNotConnected, DecisionNotCovered
type Service struct{ /* tokens, akış, kapsam önbelleği */ }
func New(cfg Config) (*Service, error)
func (s *Service) StartDeviceFlow(ctx context.Context) (*kadranv1.GitHubUserCodePrompt, error)
func (s *Service) Status(ctx context.Context) (*kadranv1.GitHubConnection, error)
func (s *Service) Disconnect(ctx context.Context) error
func (s *Service) ResolveBranch(ctx context.Context, owner, repo, branch string) (string, error)
func (s *Service) Archive(ctx context.Context, owner, repo, sha string) (Decision, io.ReadCloser, error)
func validBranch(name string) error
```

- [ ] **Adım 1: Başarısız testleri yaz.** Sahte GitHub ve sahte saat kullanılır; testte gerçek uyku yok, yoklama aralığı `Config.Now` ve enjekte edilen `sleep` ile ilerletilir.
  - **Akış:**
    - `TestDeviceFlowPendingThenSuccess`;
    - `TestSlowDownAddsFiveSeconds`: aralık 5 → 10 sn;
    - `TestAccessDeniedFails`, `TestExpiredTokenFails`, `TestDeviceFlowDisabledFails`: durum `STATE_FAILED`, `failure` doğru `GitHubFailure` değeri (serbest metin alanı yok, Ruling 4);
    - `TestNewStartCancelsOldFlow`;
    - `TestDeviceCodeRequestHasNoSecret`.
  - **Bağlanma sonrası:**
    - `TestSuccessFetchesLoginAndInstallations`: `/user`, `/user/installations`, `/user/installations/{id}/repositories?per_page=100` sayfalı;
    - `TestInstallationListTruncatesAt1000`: `truncated=true`;
    - `TestStatusWithoutInstallationShowsInstallURL` (Review Focus 1): bağlı, `installations` boş, `install_url` dolu.
  - **Kapsam:**
    - `TestArchiveDecisionUsesPrivateFlag`: listede `private:false` → `DecisionNotCovered`, tarball isteği YAPILMAZ;
    - `TestArchiveNotListedRefreshesAtMostOncePerMinute`;
    - `TestArchiveNotConnected`;
    - `TestPrivateArchiveErrorIsNotFallback`: `private:true` + tarball 404 → `Decision` önemsiz, `err != nil`.
  - **Yenileme:** `TestUnauthorizedForcesOneRefresh` (Görev 4'ten).
  - **Dal:**
    - `TestResolveBranchAcceptsSlashes` (`feature/x`);
    - `TestResolveBranchRejectsUnsafeNames`: `..`, `a//b`, `-x`, `/x`, `x/`, `x.lock`, 256 bayt, boşluk, `:`;
    - `TestResolveBranchRequiresFullSHA`: yanıt `abc` → hata.
  - **Kesme:** `TestDisconnectRemovesStateAndCancelsFlow`.
- [ ] **Adım 2:** Koş: `go test ./internal/github/ -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.**
  - Yoklama, `Service` ömrüne bağlı tek bir goroutine'de; `Disconnect` ve yeni `Start` onu iptal eder.
  - Dal çözümü `GET /repos/{o}/{r}/commits/{branch}` ve `Accept: application/vnd.github.sha`. Dal adı doğrulandıktan sonra `/` korunur, segmentler `url.PathEscape` ile kaçırılır.
- [ ] **Adım 4:** Aynı komut + `go test -race ./internal/github/`. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update github device flow service"`

### Görev 6: `kadran-git` ikilisi

**Files:**
- Create: `cmd/kadran-git/main.go`, `cmd/kadran-git/handlers.go`, `cmd/kadran-git/activation.go`
- Test: `cmd/kadran-git/handlers_test.go`, `cmd/kadran-git/activation_test.go`, `cmd/kadran-git/peer_linux_test.go`

**Interfaces — Consumes:** `github.Service` (Görev 5), `peercred.TransportCredentials`, `grpcserve.Run`, `sdnotify.Ready`.

**Interfaces — Produces:**
- Soket adları: `admin` ve `fetch`. systemd `FileDescriptorName=` ile `LISTEN_FDNAMES`'te gelir.
- `func activated(getenv func(string) string, pid int) (map[string]net.Listener, error)`. `LISTEN_PID` ≠ pid ise ya da bir ad eksikse hata.
- `fetch` yanıtı: önce karar iletisi, `ARCHIVE` ise 64 KiB'lık parçalar.
- Bayraklar: `--state /var/lib/kadran-git/github.json`, `--client-id`, `--app-slug`, `--version`.
- Açılışta süreç root ise reddeder: "kadran-git must not run as root".

- [ ] **Adım 1: Başarısız testleri yaz.**
  - `TestActivatedNamesListeners`, `TestActivatedRejectsForeignPID`, `TestActivatedRejectsMissingName`.
  - `TestFetchSendsDecisionFirst`, `TestFetchStreamsArchiveInChunks`, `TestFetchErrorIsStatusNotDecision` (sahte servisle).
  - Linux, `peer_linux_test.go`: gerçek unix soketler ve `peercred.Policy{AllowUIDs}`. `admin` politikası root'u ve başka uid'i reddeder; `fetch` politikası `kadran` uid'ini reddeder. Test kendi uid'iyle hangi politikanın kabul ettiğini ölçer: kendi uid'i listede değilken bağlantı `PermissionDenied`.
- [ ] **Adım 2:** Koş: `go test ./cmd/kadran-git/ -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** `admin` için `peercred.Policy{AllowUIDs: [uid(kadran)]}`, `fetch` için `peercred.Policy{AllowUIDs: [0]}`. İki ayrı `grpc.Server`.
- [ ] **Adım 4:** Koş: `go test ./cmd/kadran-git/ && GOOS=linux go vet ./cmd/kadran-git/`. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update kadran-git daemon"`

### Görev 7: Birimler, kurulum, sürüm paketi

**Files:**
- Create: `deploy/systemd/kadran-git.service`, `deploy/systemd/kadran-git-admin.socket`, `deploy/systemd/kadran-git-fetch.socket`
- Modify:
  - `internal/bootstrap/install.sh`
  - `scripts/build-release.sh` (`SERVER_BINARIES`)
  - `scripts/package-release.sh` (gerekiyorsa)
  - `scripts/check-install-sh.sh`
  - `scripts/check-install-order.sh`
  - `deploy/systemd/kadran-exec.service` (yorum: K-146 yolu kalktı)
  - `deploy/systemd/kadrand.service` (`Wants=`/`After=kadran-git-admin.socket`; `Requires` DEĞİL)

**Birim değerleri:**
- `kadran-git-admin.socket`:
  - `ListenStream=/run/kadran-git/admin.sock`, `SocketUser=kadran-git`, `SocketGroup=kadran`, `SocketMode=0660`;
  - `FileDescriptorName=admin`, `Service=kadran-git.service`.
- `kadran-git-fetch.socket`:
  - `ListenStream=/run/kadran-git/fetch.sock`, `SocketUser=root`, `SocketGroup=root`, `SocketMode=0600`;
  - `FileDescriptorName=fetch`, `Service=kadran-git.service`.
- `kadran-git.service`:
  - `User=kadran-git`, `Group=kadran-git`, `Requires=` iki soket;
  - `StateDirectory=kadran-git`, `StateDirectoryMode=0700`, `UMask=0077`;
  - `CapabilityBoundingSet=`, `NoNewPrivileges=yes`;
  - `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`;
  - `kadran-proxy.service`'teki `Protect*`, `Restrict*`, `SystemCallFilter` satırları;
  - `MemoryDenyWriteExecute=yes`;
  - `InaccessiblePaths=-/run/kadran-exec -/run/docker.sock -/var/lib/kadran -/var/lib/kadran-exec -/var/lib/kadran-caddy`;
  - ağ satırları (Görev 1'de ölçüldü):
    - `IPAddressDeny=localhost link-local multicast`;
    - `IPAddressAllow=127.0.0.53`;
    - `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`;
    - `BindReadOnlyPaths=/run/systemd/resolve/stub-resolv.conf:/etc/resolv.conf`. Başında `-` YOK: dosya yoksa birim açıkça başlamasın.
    - `After=systemd-resolved.service`.

- [ ] **Adım 1: Başarısız denetimleri ekle.**
  - `scripts/check-install-sh.sh`:
    - `kadran-git` kullanıcısının `docker`, `kadran`, `kadran-client`, `kadran-proxy` gruplarında olmadığını denetleyen bir `die` satırı var;
    - kurulum, `systemd-resolved` etkin değilse ya da `/run/systemd/resolve/stub-resolv.conf` 127.0.0.53'ü göstermiyorsa "kadran-git needs systemd-resolved (stub resolver at 127.0.0.53)" der ve durur;
    - kurulum ikilileri değiştirmeden ÖNCE `/etc/systemd/system/kadran-exec.service.d/*.conf`'ta `--git-credentials` arar ve bulursa "remove --git-credentials from the kadran-exec drop-in; private repositories now use `kadran github connect`" der.
  - `scripts/check-install-order.sh`: `kadran-git-*.socket` kadrand yeniden başlamadan önce etkin.
- [ ] **Adım 2:** Koş: `bash scripts/check-install-sh.sh; bash scripts/check-install-order.sh`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** `install.sh`'te `kadran-proxy` deseniyle `git_birimi_kullanicisi`, `git_birimleri` ve sürüm başına `systemctl enable --now kadran-git-admin.socket kadran-git-fetch.socket`. `build-release.sh`'e `kadran-git` eklenir.
- [ ] **Adım 4:** Aynı komutlar + `systemd-analyze verify deploy/systemd/kadran-git*` (WSL) + `bash scripts/build-release.sh` sonrası `bin/linux-amd64/kadran-git` var. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update kadran-git units"`

### Görev 8: Yürütücü

**Files:**
- Create: `internal/exec/gitfetch.go`
- Test: `internal/exec/gitfetch_test.go`
- Modify:
  - `internal/exec/privategit.go` (yalnız arşiv yeniden yazma kalır)
  - `internal/exec/container.go:393-400`
  - `internal/exec/server.go:25,54-55,94,110`
  - `cmd/kadran-exec/main.go`
  - `scripts/check-exec-surface.sh` (`MAX_EXEC_LINES` ve gerekçe paragrafı)
- Delete: `internal/exec/privategit_linux.go`, `internal/exec/privategit_other.go`, `internal/exec/privategit_linux_test.go` (dosya okuma denetimi Görev 4'te taşındı)

**Interfaces — Consumes:** `kadranv1.GitFetchServiceClient`, `FetchArchiveResponse` (Görev 2).

**Interfaces — Produces:**

```go
const defaultGitFetchSocket = "/run/kadran-git/fetch.sock"   // bayrak DEĞİL
const gitDecisionTimeout = 5 * time.Second                   // bağlanma + ilk ileti
type GitFetcher interface {
  // decision NOT_CONNECTED / NOT_COVERED ise arşiv nil, err nil. Karardan ÖNCEKİ
  // her başarısızlık (bağlanamama, 5 sn içinde ileti yok, gRPC hatası) da
  // NOT_CONNECTED sayılır ve günlüğe uyarı yazılır. ARCHIVE ise yeniden yazılmış,
  // denetlenmiş geçici tar döner; ARCHIVE'dan sonraki her başarısızlık err.
  Fetch(ctx context.Context, src *kadranv1.GitSource) (kadranv1.FetchArchiveResponse_Decision, *os.File, error)
}
func NewGitFetcher(socketPath string) GitFetcher
```

- [ ] **Adım 1: Başarısız testleri yaz** (sahte `GitFetchService` gerçek unix soketinde).
  - `TestBuildUsesPublicPathWhenNotConnected`: soket yok ya da `NOT_CONNECTED` → `spec.ContextURL` dolu, `ContextTar` nil.
  - `TestBuildUsesPublicPathWhenNotCovered`.
  - `TestPublicBuildSurvivesUnresponsiveGitService` (Review Focus 0):
    - dinleyici bağlantıyı kabul eder ama hiç yazmaz → 5 sn içinde herkese açık yol;
    - aynı test karardan önce gRPC hatası dönen bir sunucuyla da koşar.
  - `TestBuildUsesTarWhenPrivate`: `ARCHIVE` → `ContextTar` dolu; denetim kaydında `source=github-app`.
  - `TestPrivateFetchErrorFailsBuild`: `ARCHIVE` sonrası akış kesilir ya da gRPC hatası gelir → derleme başarısız, Docker'a hiç gidilmez.
  - `TestArchiveChecksStillApply`: kötü arşiv (symlink, `..`, tekrar eden yol) → red. Bugünkü `rewritePrivateArchive` testleri yeşil kalır.
  - `TestNoGitCredentialsFlag`: `kadran-exec --git-credentials x` → bilinmeyen bayrak hatası.
  - `TestPublicFailureGetsHintWhenConnected`: `NOT_COVERED` sonrası derleme başarısız → hata metninde "if it is private, install the Kadran Deploy app on it".
- [ ] **Adım 2:** Koş: `go test ./internal/exec/ ./cmd/kadran-exec/ -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** `container.go`'daki `hasCredential` dalı `s.gitFetch.Fetch` ile değişir. Karar yalnız `src.GetHost() == DefaultGitHost` iken sorulur. `ServerOptions.PrivateGit` → `GitFetch GitFetcher`; `main.go` `NewGitFetcher(defaultGitFetchSocket)` geçer.
- [ ] **Adım 4:** Koş: `go test ./internal/exec/ ./cmd/kadran-exec/ && bash scripts/check-exec-surface.sh`. Beklenen: PASS. Satır sayısı 2982'nin altında.
  - Ölçülen sayıyı `MAX_EXEC_LINES`'a yaz ve "2990 → N: K-167, ağ ve jeton kodu kadran-git'e taşındı" paragrafını ekle.
  - Ayrıca `bash scripts/check-exec-surface-test.sh`.
- [ ] **Adım 5: Commit.** `git commit -m "update exec git fetch"`

### Görev 9: kadrand

**Files:**
- Create: `internal/store/settings.go`, `internal/gitclient/client.go`
- Modify: `internal/api/github.go` (taslaklar gerçek olur), `internal/api/server.go` (`Git GitAdmin` bağımlılığı), `cmd/kadrand/main.go`
- Test: `internal/store/settings_test.go`, `internal/api/github_test.go`, `internal/api/authz_test.go`

**Interfaces — Produces:**

```go
// store
func (s *Store) Setting(ctx context.Context, key string) (string, bool, error)
func (s *Store) SetSetting(ctx context.Context, key, value string) error
const settingGitHubConnectedAt = "github.connected_at"   // internal/api içinde
// gitclient (kadrand → admin.sock)
func Dial(socketPath string) (*Client, error)
func (c *Client) StartDeviceFlow(ctx) (*kadranv1.GitHubUserCodePrompt, error)
func (c *Client) Status(ctx) (*kadranv1.GitHubConnection, error)
func (c *Client) Disconnect(ctx) error
func (c *Client) ResolveBranch(ctx, owner, repo, branch string) (string, error)
// api
type GitAdmin interface { /* gitclient.Client'ın dört yöntemi */ }
```

**Denetim eylem adları:**
- `github.connect.start`: parametre yok.
- `github.connected`: `login` ve `installations` ("hesap:all|selected:N").
- `github.disconnect`.
- Hiçbirinde kod ya da jeton yok.

- [ ] **Adım 1: Başarısız testleri yaz.**
  - `TestDeployRoleCannotUseGitHubCalls`: dört çağrı, dağıtım kimliği → `PermissionDenied`. `authz_test.go`'daki tablo deseni.
  - `TestConnectedIsAuditedOnceOnFirstSight`: sahte `GitAdmin` `connected_at=T` döner, iki `GetGitHubStatus` → tek `github.connected` kaydı. `connected_at=T2` → ikinci kayıt.
  - `TestAuditHasNoCodes`: kayıtların `ParamsJSON`'unda kullanıcı kodu ve `ghu_` yok.
  - `TestResolveBranchUsesStoredRepo`: istek yalnız `app_id` ve `branch` taşır; sahteye giden owner/repo uygulama kaydından gelir.
  - `TestGitHubUnavailableIsFailedPrecondition`: soket yok → `codes.FailedPrecondition`, "the kadran-git service is not running".
  - `TestSettingRoundTrip` (store).
- [ ] **Adım 2:** Koş: `go test ./internal/api/ ./internal/store/ -run 'GitHub|Setting|DeployRole' -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** kadrand `admin.sock`'a tembel bağlanır: açılışta yoksa başlar, çağrıda `FailedPrecondition` döner.
- [ ] **Adım 4:** Koş: `go test ./internal/api/ ./internal/store/ ./cmd/kadrand/`. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update kadrand github calls"`

### Görev 10: CLI ve dal çözümü

**Files:**
- Create: `cmd/kadran/github.go`
- Test: `cmd/kadran/github_test.go`, `internal/gitref/commit_test.go`
- Modify: `cmd/kadran/main.go` (komut tablosu: `{"github", "<connect|status|disconnect> [target]", "connects private GitHub repositories", (*cli).runGitHub}`), `internal/gitref/commit.go`

**connect çıktısı (İngilizce, sırayla):**
1. "1. Install the Kadran Deploy app on the repositories Kadran may build:" + kurulum adresi.
2. "2. Open https://github.com/login/device and enter the code:" + kod.
3. "Waiting for approval (expires HH:MM)…"
4. Sonuç: "Connected as <login>." + kurulum başına "account: N repositories (selected)" ya da "(ALL repositories — consider Only select repositories)".
5. Kurulum yoksa: "No installation yet: install the app, then run `kadran github status`."

**disconnect çıktısı:** iki iptal adresi (spec "Bağlantıyı kesmek").

- [ ] **Adım 1: Başarısız testleri yaz** (`cli_test.go`'daki sahte sunucu deseni).
  - `TestGitHubConnectPrintsCodeAndWaits`.
  - `TestConnectStopsWhenPendingFlowDisappears` (Review Focus 2): durum `PENDING` → `NOT_CONNECTED` → çıkış 1, "the sign-in flow ended on the server; run connect again".
  - `TestConnectStopsAtExpiry`: sahte saat `expires_at`'i geçince çıkış 1.
  - `TestGitHubStatusWarnsAllRepositories`.
  - `TestGitHubOnOldServerSaysUpgrade`: `Unimplemented` → "upgrade the server".
  - `TestResolveCommitFallsBackToServer`: anonim çözüm hata verir → `ResolveGitHubBranch` çağrılır ve sha döner.
  - `TestResolveCommitKeepsAnonymousErrorOnOldServer`: `Unimplemented` → ilk hata korunur.
  - `TestResolveCommitDeployKeyStillNeedsCommit`: değişmeyen davranış.
- [ ] **Adım 2:** Koş: `go test ./cmd/kadran/ ./internal/gitref/ -run 'GitHub|Connect|ResolveCommit' -v`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** Yoklama 3 sn arayla `GetGitHubStatus`; Ctrl-C bağlamı iptal eder ve "the flow continues on the server until the code expires" yazar.
- [ ] **Adım 4:** Koş: `go test ./cmd/kadran/ ./internal/gitref/ && go test ./cmd/kadran/ -run TestEnglish`. Beklenen: PASS.
- [ ] **Adım 5: Commit.** `git commit -m "update github cli"`

### Görev 11: Masaüstü

**Files:**
- Create:
  - `internal/sidecar/github.go`
  - `desktop/internal/bridge/github.go`
  - `desktop/src/renderer/screens/GitHubScreen.tsx`
  - `desktop/src/renderer/lib/github.ts`
  - `desktop/test/github.test.ts`
- Modify:
  - `internal/sidecar/dispatcher.go` (yöntemler: `github.status`, `github.connect`, `github.disconnect`)
  - `desktop/src/renderer/bridge.ts`
  - `desktop/src/renderer/components/ServerSubnav.tsx` (`ServerView` += `"github"`, güvenlik bölümünde)
  - `desktop/src/renderer/App.tsx`
  - `desktop/src/renderer/i18n/en.json`, `tr.json`
  - `desktop/test/fixtures/fake-sidecar.mjs`
  - smoke yolculuğu (`desktop/scripts/smoke.ts` ve yolculuk dosyası)
  - `scripts/mutate-desktop.sh`

**Interfaces — Consumes:** `GetGitHubStatus`, `StartGitHubConnect`, `DisconnectGitHub` (Görev 9); mevcut `confirm` paketi.

**Interfaces — Produces:**
- Köprü yöntemleri: `GitHubStatus(target) (any, error)`, `GitHubConnect(target) (any, error)`, `GitHubDisconnect(target, confirmation string) (any, error)`.
- `GitHubDisconnect` yalnız `confirmation == "disconnect"` iken ve yerel onay penceresinden sonra çalışır (`AppDelete` deseni).

**Ekran:**
- Bağlı değilken "Connect GitHub" düğmesi.
- Akış sırasında: büyük kod, "Copy", "Open github.com/login/device", kurulum bağlantısı ve kalan süre. Ekran 3 sn arayla durum yoklar; pencere gizliyse yoklama yok (K-166 deseni).
- Bağlıyken: hesap, kurulum seçimi, depo listesi ("private" rozeti) ve "Disconnect".
- Eski sunucuda "Upgrade the server".

- [ ] **Adım 1: Başarısız testleri yaz.**
  - `github.test.ts`: `parseGitHubConnection` dört durumu ayrıştırır; bilinmeyen durum "failed" sayılır; kod biçimi `XXXX-XXXX` dışındaysa gösterilmez.
  - i18n testi: yeni anahtarlar iki dilde var; `SAME_IN_BOTH` gerekirse güncellenir.
  - Smoke:
    - sahte yan süreç `github.status` sırayla bağlı değil → bekliyor → bağlı döner;
    - yolculuk kodu görür, "Copy"ye basar, bağlı ekranını ve eski sunucu metnini görür;
    - 760×520'de taşma yok.
  - Go: `TestBridgeGitHubDisconnectNeedsConfirmation`.
- [ ] **Adım 2:** Koş: `cd desktop && npm test && npm run build && node scripts/smoke.ts`. Beklenen: FAIL.
- [ ] **Adım 3: Uygula.** Görünüm K-161/K-163 düzeninde: mevcut `Tile`, `KeyValues`, `EmptyState` bileşenleri; yeni renk yok.
- [ ] **Adım 4:** Aynı komut + `go test ./internal/sidecar/ ./desktop/...`. Beklenen: PASS.
- [ ] **Adım 5: Mutantlar.** `mutate-desktop.sh`'e üç mutant ekle: kod gizlenmiyor; onaysız kesme; yoklama gizliyken sürüyor. Koş: `bash scripts/mutate-desktop.sh`. Beklenen: hepsi yakalandı.
- [ ] **Adım 6: Commit.** `git commit -m "update desktop github screen"`

### Görev 12: Mutasyon betiği ve belgeler

**Files:**
- Create: `scripts/mutate-github.sh`
- Modify:
  - `scripts/mutation-shard.sh` (SURELER: ölçülünce)
  - `docs/guides/private-git.md` (yeniden yazılır)
  - `README.md` (özel depolar bölümü, yol haritası satırı)
  - `CHANGELOG.md` (`## Unreleased`)
  - `SECURITY.md` (güven notu: uygulama sahibi ve kendi uygulamanı kaydetmek)
  - `docs/decisions.md` (K-167 "Uygulama")

**mutate-github.sh FILES:**
- `internal/github/*.go` (test hariç)
- `cmd/kadran-git/*.go`
- `internal/exec/gitfetch.go`, `internal/exec/container.go`
- `internal/api/github.go`, `internal/api/authz.go`
- `deploy/systemd/kadran-git.service`, `deploy/systemd/kadran-git-admin.socket`, `deploy/systemd/kadran-git-fetch.socket`

**Mutantlar, her biri bir testin yakalaması gereken tek değişiklik:**
1. `slow_down` aralığa 5 eklemiyor.
2. Yeni jeton diske yazılmadan dönüyor.
3. Yenileme kilidi kaldırıldı.
4. `private:false` tar yoluna gidiyor.
5. Tar yolu hatası anonim yola düşüyor.
6. Yönlendirme host denetimi kaldırıldı.
7. İkinci istekte `Authorization` taşınıyor.
8. `admin` politikası root'a açık.
9. `fetch` politikası `kadran`'a açık.
10. Dal adında `..` kabul ediliyor.
11. Hata metnine gövde ekleniyor (jeton sızar).
12. `deployAllowed`'a `StartGitHubConnect` eklendi.
13. Birimden `IPAddressDeny` satırı silindi.
14. Bayrak denetimi: `kadran-git` root'ta çalışıyor.
15. Karardan önceki başarısızlık derlemeyi düşürüyor; herkese açık yola düşmüyor.
16. Karar süre sınırı kaldırıldı.

Birim satırı mutantları (13 ve 14) bir denetim betiğinin ya da testin birim dosyasını okumasını gerektirir. `scripts/check-units` deseni yoksa Görev 7'nin `check-install-sh.sh` denetimine eklenir.

- [ ] **Adım 1: Betiği yaz.** `mutate-resources.sh` deseni: yedek al, `trap` ile geri yükle, her mutant için ilgili `go test -run` koş, "yakalandı / SAĞ KALDI" yaz.
- [ ] **Adım 2:** Koş: `bash scripts/mutate-github.sh`. Beklenen: "Bütün mutasyonlar yakalandı."
  - Sağ kalan varsa testi güçlendir, betiği zayıflatma.
  - Ağaç geri yüklenmeli: `git status` temiz.
- [ ] **Adım 3:** Koş: `bash scripts/mutation-select.sh` (değişen dosyalarla). Beklenen: `mutate-github.sh` seçiliyor. `bash scripts/check-mutation-shard.sh`: geçer.
- [ ] **Adım 4: Belgeleri yaz.** Rehber üç adım:
  - uygulamayı yalnız seçili depolara kur;
  - `kadran github connect`;
  - dal adıyla dağıt.
  - Ayrıca sınırlar (tar yolu: `.git` yok, alt modül yok, LFS GitHub ayarına bağlı), iptal adresleri ve "kendi uygulamanı kaydet" bölümü.
- [ ] **Adım 5: Commit.** `git commit -m "update github mutations and docs"`

### Görev 13: Yerel uçtan uca, doğrulama, inceleme, PR

**Files:** yalnız ölçüm notları (`docs/decisions.md` K-167).

- [ ] **Adım 1: WSL'de gerçek systemd + Docker** (`wsl.exe -u root`, betik dosyadan, `MSYS_NO_PATHCONV=1`). Yerel derlemeyle kurulum. Ölç:
  - `systemctl is-active kadran-git-admin.socket kadran-git-fetch.socket`;
  - ilk bağlantıda `kadran-git` başlıyor;
  - `systemctl restart kadran-git` ve WSL kapat-aç sonrası soketler yine etkin;
  - `journalctl -u kadran-git` döngü yok;
  - `stat -c '%U %G %a' /run/kadran-git/*.sock` → `kadran-git kadran 660` ve `root root 600`.
- [ ] **Adım 2: Ağ kısıtı, kontrol grubuyla.**

```sh
systemd-run --wait --pipe -p User=kadran-git -p IPAddressDeny='localhost link-local multicast' -p IPAddressAllow=127.0.0.53 -p RestrictAddressFamilies='AF_UNIX AF_INET AF_INET6' curl -sS -o /dev/null -w '%{http_code}' https://api.github.com/zen
systemd-run --wait --pipe -p User=kadran-git -p IPAddressDeny='localhost link-local multicast' curl -sS -m 3 http://169.254.169.254/ ; echo "exit=$?"
curl -sS -m 3 http://169.254.169.254/ ; echo "control exit=$?"
```

- Beklenen:
  - ilki `200`;
  - ikincisi bağlantı hatası;
  - üçüncü (kısıtsız), WSL'de meta veri sunucusu olmadığı için zaman aşımı.
- Kontrol grubu olarak ikinci satır aynı kısıtla `1.1.1.1:443`'e `curl -m 3 https://1.1.1.1` ile bağlanabilmeli. Ölçüm, engelin yalnız link-local'e uygulandığını kanıtlar.
- [ ] **Adım 3: Gerçek GitHub'a karşı akış** (yerel, gerçek uygulama).
  - Kullanıcı uygulamayı henüz kurmadı (12 Kasım kararı). Bu adım o hâliyle yalnız "kurulum yok" yolunu sınar.
  - Özel arşiv yolunu yerelde de sınamak için bu noktada kullanıcıya sorulur: uygulamayı tek bir test deposuna kurar mı?
  - `kadran github connect`'i kullanıcı onaylar.
  - Kod kullanıcının, transkripte alınmaz; terminal okunmaz.
  - Sonra `status`; `journalctl -u kadran-git` ve iki denetim zincirinde `ghu_`/`ghr_` taraması: 0 satır.
- [ ] **Adım 4: `/verify`.** Kök `go test ./...`, masaüstü Go ve TS, iki lint, `check-exec-surface-test.sh`, `node scripts/smoke.ts`, `buf lint`. Hepsi yeşil.
- [ ] **Adım 5: Bağımsız inceleme.**
  - Dal üzerinde en yetenekli modelle.
  - Kritik ve yüksek bulgular düzeltilir, ayrı commit'lerle (`update review fixes`).
- [ ] **Adım 6: Push + PR.**
  - Kullanıcı onayıyla.
  - PR başlığı `update github device flow`.
  - Gövde: özet + test planı.
  - CI kullanıcı söyleyince bir kez okunur; `--auto` yok.
  - Birleştirme ayrı onay, squash konusu `update github device flow`, gövdesiz.

### Görev 14: Canlı (her adım ayrı onay, öncesinde kısa durum)

**Files:** `docs/decisions.md` (K-167 "Canlı"), sürüm notu.

- [ ] **Adım 1: Sürüm.**
  - Yayın akışı memory'deki gibi:
    - not;
    - tam CI;
    - iki LF klonu, `SHA256SUMS` aynı;
    - etiket;
    - taslak;
    - geri indirme.
  - Yayımlama ayrı onay.
- [ ] **Adım 2: Hetzner.**
  - Önce yükseltme (`bootstrap root@`), yoklamalarla kesinti ölçülür.
  - Ardından herkese açık üç uygulama (`portfolio`, `web`, `pfprobe`) bugünkü yoldan yeniden dağıtılır. Denetim kaydında `source=public`.
- [ ] **Adım 3: Uygulama kurulumu.**
  - Kullanıcı github.com'da "Only select repositories" ile yalnız test deposunu seçer.
  - Depo yoksa yeni depo açmak onun hesabında bir iş, önce sorulur.
  - Kurulum özel anahtar isterse anahtar üretilir, kurulumdan sonra uygulama ayarlarından silinir ve yerel dosyası silinir. Ayrı onay.
- [ ] **Adım 4: Bağlan ve dağıt.**
  - `kadran github connect -s hetzner`; kodu kullanıcı girer.
  - Ardından `kadran deploy <özel-uygulama> -s hetzner` dal adıyla. Denetim kaydında `source=github-app`.
  - Günlükte ve iki zincirde jeton taraması: 0.
- [ ] **Adım 5: GCP**, aynı sıra (`bootstrap -sudo`).
- [ ] **Adım 6: 8 saat sonra.** `status` ya da bir dağıtım yenilemenin çalıştığını gösterir: durum dosyasında `access_expires_at` ilerledi, `connected_at` aynı.
- [ ] **Adım 7: Kayıtlar.**
  - K-167'ye "Canlı" bölümü yazılır, push onaylı (`update github live notes [skip ci]`).
  - Memory güncellenir.
- [ ] **Adım 8: Cloudflare security-audit.** Kullanıcı kararı (12 Kasım): cihaz akışı bitince. Ayrı onay ve ayrı plan.
