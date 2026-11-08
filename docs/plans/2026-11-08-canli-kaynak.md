# Canlı Kaynak Kullanımı — Uygulama Planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sunucunun ve her uygulamanın CPU, RAM, disk, ağ ve görev kullanımını masaüstünde canlı göstermek.

**Architecture:** kadrand host sayaçlarını `/proc` ve `statfs`'ten okur; yürütücü Kadran konteynerlerinin Docker istatistiğini okuyup yalnız sayıları döner. Yeni `GetResourceUsage` RPC kümülatif sayaç + ölçüm anı döner; masaüstü iki yoklamanın farkından hızları hesaplar.

**Tech Stack:** Go (kadrand, kadran-exec, dockerdrv), protobuf/buf, Wails Go köprüsü, React + TypeScript (renderer), elle SVG çizgi grafikleri.

**Spec:** `docs/decisions.md` → K-166.

## Global Constraints

- Yasak alan adları `exec.proto`'da kullanılmaz (`scripts/check-exec-surface.sh --list-forbidden`): `container_id`, `image`, `cgroup_parent`, `devices`… Konteyner kimliği yürütücüden ÇIKMAZ.
- Host alanları `optional`; okunamayan değer alanı boş bırakır, 0 yazılmaz.
- Proto değişince `buf generate` VE `buf lint` ikisi de koşulur.
- Hata metinleri İngilizce (`desktop/internal/english` testi); arayüz metinleri `en.json` + `tr.json`.
- Commit: `update <birkaç İngilizce kelime>`, gövde yok, Co-Authored-By yok. Kod commit'inden önce `/verify`.
- Yürütücü bütçesi ölçülmeden değişmez; değişince `check-exec-surface.sh`'ye gerekçe (K-166).
- Yoklama: sunucu sayfası 2000 ms, filo kartı 5000 ms; `document.hidden` iken hiç.

## Review Focus

1. Sayaç geri gider (konteyner yeniden başladı, dağıtım, host yeniden başladı): hız "—" olmalı, eksi ya da dev sıçrama değil. → Görev 6 testi `negative delta yields null`.
2. Eski sunucu (RPC yok, `Unimplemented`): kartta ve panelde "sunucuyu yükseltin", bildirim yağmuru yok. → Görev 7 testi `unsupported server is not an error toast`.
3. Yürütücü kapalı: host sayaçları yine gelir, uygulama bölümü "ölçülemedi". → Görev 4 testi `TestResourceUsageWithoutExecutor`.
4. İlk yoklama: hız yok, "—" gösterilir; ikinci yoklamada dolar. → Görev 6 testi `first snapshot has no rates`.
5. Çalışan konteyneri olmayan uygulama listede yok; tablo boşsa "çalışan uygulama yok". → Görev 3 testi (durmuş konteyner atlanır) + Görev 7 smoke.

---

### Görev 1: Şema

**Files:**
- Modify: `proto/kadran/v1/api.proto` (RPC + `GetResourceUsageRequest/Response`, `HostUsage`, `AppUsage`)
- Modify: `proto/kadran/v1/exec.proto` (RPC `ContainerStats`, `ContainerStatsRequest/Response`, `ContainerUsage`)
- Regenerate: `internal/pb/kadran/v1/*`

**Interfaces — Produces:**
- `rpc GetResourceUsage(GetResourceUsageRequest) returns (GetResourceUsageResponse)`; istek `bool include_apps = 1`; yanıt `int64 sampled_at_unix_ms = 1; HostUsage host = 2; repeated AppUsage apps = 3; bool apps_measured = 4`.
- `HostUsage` (hepsi `optional` dışında `cpu_count`): `uint32 cpu_count`, `cpu_total_ticks`, `cpu_idle_ticks` (idle+iowait), `cpu_iowait_ticks`, `cpu_steal_ticks`, `memory_total_bytes`, `memory_available_bytes`, `swap_total_bytes`, `swap_free_bytes`, `double load1/load5/load15`, `uint32 tasks`, `disk_total_bytes`, `disk_available_bytes`, `inodes_total`, `inodes_free`, `disk_read_bytes`, `disk_write_bytes`, `net_rx_bytes`, `net_tx_bytes`, `double cpu_pressure/memory_pressure/io_pressure` (some avg10, yüzde), `uptime_seconds`.
- `AppUsage`: `string app_id`, `uint32 containers`, `uint64 cpu_usage_ns`, `memory_bytes`, `memory_limit_bytes`, `net_rx_bytes`, `net_tx_bytes`, `disk_read_bytes`, `disk_write_bytes`, `uint32 tasks`.
- `ContainerUsage` (exec): `ContainerRef ref` + AppUsage'ın sayaçlarının aynısı (app_id ve containers hariç).

- [ ] **Step 1:** Mesajları yaz; yorumlar: sayaçlar kümülatif, hız istemcide, `optional` = ölçülemedi.
- [ ] **Step 2:** `buf generate && buf lint` → temiz; `bash scripts/check-exec-surface.sh` → yasak alan yok.
- [ ] **Step 3:** `go build ./...` (kök) geçer. Commit `update resource usage schema`.

### Görev 2: Host sayaçları (kadrand)

**Files:**
- Create: `internal/hostmetrics/parse.go` (taşınabilir ayrıştırıcılar), `internal/hostmetrics/read.go` (`Reader`), `internal/hostmetrics/statfs_linux.go`, `statfs_other.go`
- Test: `internal/hostmetrics/parse_test.go`, `read_test.go`, `testdata/proc/{stat,meminfo,loadavg,net/dev,diskstats,uptime,pressure/cpu,pressure/memory,pressure/io}`, `testdata/sys/block/{vda,loop0,dm-0}/`

**Interfaces — Produces:**
- `type Reader struct { Proc, Sys, StateDir string }`; `func Default() Reader` → `{"/proc", "/sys", "/var/lib/kadran"}`.
- `func (r Reader) Read() *kadranv1.HostUsage` — her dosya ayrı; okunamayan dosya yalnız kendi alanlarını boş bırakır.
- `func (r Reader) Memory() (total, available uint64, ok bool)` (GetSystemInfo için).

- [ ] **Step 1: Testler** — `TestReadFromFixture`: `cpu_total_ticks` = stat satırındaki ilk 8 sütunun toplamı, `cpu_idle_ticks` = idle+iowait, `cpu_steal_ticks` 8. sütun; `memory_*`, `swap_*` kB×1024; `load1/5/15`; `tasks` = loadavg 4. alanın `/`'den sonrası; `net_*` = `lo`, `veth*`, `docker*`, `br-*` HARİÇ toplam; `disk_*` = yalnız `Sys/block` altındaki diskler, `loop*`, `ram*`, `dm-*`, `zram*`, `sr*`, `md*` hariç, sektör×512; pressure `some avg10`; `uptime_seconds` tam saniye. `TestMissingFileLeavesFieldsUnset`: `pressure` dizini yokken üç baskı alanı nil, diğerleri dolu. `TestMemoryHelper`.
- [ ] **Step 2:** `go test ./internal/hostmetrics/` → FAIL (paket yok).
- [ ] **Step 3:** Uygula. `statfs` durum dizininden: `Blocks*Bsize`, `Bavail*Bsize` (Bfree değil; gerekçe `internal/exec/hostinfo_linux.go` yorumunda), `Files`, `Ffree`.
- [ ] **Step 4:** Test geçer (Windows'ta statfs alanları yok sayılır). Commit `update host metrics reader`.

### Görev 3: Yürütücü konteyner istatistiği

**Files:**
- Modify: `internal/dockerdrv/container.go` → `type Usage struct { CPUNanos, MemoryBytes, MemoryLimit, RxBytes, TxBytes, ReadBytes, WriteBytes uint64; Tasks uint32 }`, `func (c *Client) ContainerStats(ctx context.Context, id string) (Usage, error)`
- Modify: `internal/exec/container.go` → `func (s *Server) ContainerStats(ctx, *kadranv1.ContainerStatsRequest) (*kadranv1.ContainerStatsResponse, error)`
- Modify: `internal/exec/hostinfo_linux.go` → `readMemInfo` ve `parseKilobytes` kalkar (kadrand dolduruyor)
- Modify: `internal/execclient/*` → `func (c *Client) ContainerStats(ctx) ([]*kadranv1.ContainerUsage, error)`
- Modify: `scripts/check-exec-surface.sh` → `MAX_EXEC_LINES` ölçülen değere, K-166 gerekçesi
- Test: `internal/dockerdrv/dockerdrv_test.go` (sahte daemon), `internal/exec/*_test.go`

- [ ] **Step 1: Testler** — sürücü: sahte daemon `stats` yanıtından `Usage` (RAM = `usage - stats.inactive_file`; `inactive_file` > usage ise usage; ağ arayüzleri toplanır; `io_service_bytes_recursive` `op` büyük/küçük harf duyarsız `read`/`write`); istek yolu `stream=false&one-shot=true`. Yürütücü: yalnız `running` durumundaki etiketli konteynerler; istatistiği hata veren konteyner atlanır, diğerleri döner; etiketsiz konteyner hiç sorulmaz.
- [ ] **Step 2:** FAIL. **Step 3:** Uygula (dar JSON yapısı, `listEntry` gibi). **Step 4:** PASS.
- [ ] **Step 5:** `bash scripts/check-exec-surface.sh` → yeni satır sayısını oku; `MAX_EXEC_LINES`'ı o sayıya çek, yorum: ne eklendi, ne çıktı (bellek okuma), reddedilen küçültmeler (K-166). `bash scripts/check-exec-surface-test.sh` geçer.
- [ ] **Step 6:** Commit `update executor container stats`.

### Görev 4: kadrand API

**Files:**
- Create: `internal/api/resources.go` → `func (s *Server) GetResourceUsage(ctx, *kadranv1.GetResourceUsageRequest) (*kadranv1.GetResourceUsageResponse, error)`; `func aggregateApps([]*kadranv1.ContainerUsage) []*kadranv1.AppUsage` (app_id'ye göre toplar, app_id'ye göre sıralar)
- Modify: `internal/api/server.go` → `Server`'a `host hostReader` (`interface{ Read() *kadranv1.HostUsage; Memory() (uint64, uint64, bool) }`); `GetSystemInfo` bellek alanlarını `host.Memory()`'den doldurur (Host nil ise oluşturmaz)
- Modify: `cmd/kadrand/*` → `hostmetrics.Default()` bağlanır
- Modify: `internal/api/interceptor.go` yorumu → okuma listesine `GetResourceUsage`
- Test: `internal/api/resources_test.go`

- [ ] **Step 1: Testler** — `TestResourceUsageHostOnly` (`include_apps=false` iken yürütücü çağrılmaz, `apps_measured=false`); `TestResourceUsageAggregatesReplicas` (iki replika toplanır, `containers=2`); `TestResourceUsageWithoutExecutor` (yürütücü hata → host dolu, `apps` boş, `apps_measured=false`, RPC hatası YOK); `TestSystemInfoMemoryFromDaemon`; `TestDeployKeyCannotReadResources` (dağıtım anahtarı `PermissionDenied`).
- [ ] **Step 2–4:** FAIL → uygula → PASS. Commit `update resource usage rpc`.

### Görev 5: kadrand birimi, sidecar, CLI

**Files:**
- Modify: `deploy/systemd/kadrand.service` → `ProcSubset=pid` kalkar, yerinde K-166 yorumu; `ProtectProc=invisible` kalır
- Modify: `internal/sidecar/dispatcher.go` → `case "resources"`: params `{target, include_apps}`, `ProtoToJSON(resp)`
- Test: `internal/sidecar/dispatcher_test.go` (`resources` sahte sunucuya `include_apps`'i geçirir), `internal/bootstrap/bootstrap_test.go` (birimde `ProcSubset` yok, `ProtectProc=invisible` var)

- [ ] **Step 1–4:** test → FAIL → uygula → PASS. `kadran status` bellek satırı yeni kadrand ile kendiliğinden gelir (Görev 4). Commit `update daemon unit and sidecar resources`.

### Görev 6: Masaüstü köprüsü ve hız hesabı

**Files:**
- Modify: `desktop/internal/bridge/bridge.go` → `func (b *Bridge) ResourceUsage(target string, includeApps bool) (any, error)` (okuma, kapı yok; `guard.Target`)
- Modify: `desktop/internal/bridge/*_test.go` (beyaz liste 24), `desktop/src/renderer/bridge.ts` → `resources(target: string, includeApps: boolean): Promise<ResourcesPayload>`
- Create: `desktop/src/renderer/lib/resources.ts` → `parseResources(raw: unknown): Snapshot` (dizeleri sayıya çevirir, biçim bozuksa fırlatır), `rates(prev: Snapshot | null, next: Snapshot): Rates`
- Test: `desktop/test/resources.test.ts`

**Interfaces — Produces:** `Snapshot = { at: number; host: HostCounters; apps: AppCounters[] | null }`; `Rates = { cpu: number | null; iowait: number | null; steal: number | null; netRx, netTx, diskRead, diskWrite: number | null; apps: Map<string, { cpu, netRx, netTx, diskRead, diskWrite: number | null }> }` (CPU yüzdeleri 0–100; uygulama CPU'su hostun yüzdesi = Δns / (Δms·1e6·cpu_count) · 100).

- [ ] **Step 1: Testler** — `first snapshot has no rates`; `cpu percent from ticks` (Δtotal 200, Δidle 50 → 75); `negative delta yields null` (sayaç geri gidince ilgili hız null, diğerleri dolu); `zero or negative interval yields null`; `app cpu is a share of the host`; `missing optional field stays null`; `malformed payload throws`.
- [ ] **Step 2–4:** FAIL → uygula → PASS (`npm test`, `go test ./desktop/...`). Commit `update desktop resource bridge`.

### Görev 7: Arayüz

**Files:**
- Create: `desktop/src/renderer/hooks/useResources.ts` → `useResources(target: string | null, intervalMs: number, includeApps: boolean): { snapshot, rates, history, unsupported, error }` (tek uçuş, `document.hidden` iken yok, geçmiş en çok 90 nokta)
- Create: `desktop/src/renderer/components/{Sparkline,ResourceMeters,ResourcePanel,AppUsageTable}.tsx`, `desktop/src/renderer/styles/resources.css`
- Modify: `ServerCards.tsx` (ölçerler), `useFleet.ts` ya da yeni `useFleetResources` (5 sn, `include_apps=false`), `StatusScreen.tsx` (panel + tablo en üstte), `i18n/en.json`, `i18n/tr.json`
- Modify: `desktop/test/fixtures/fake-sidecar.mjs` (`resources`: her çağrıda artan sayaçlar; `fake@noalarms` → `Unimplemented`), `desktop/test/smoke/journey.ts`
- Test: `desktop/test/resources-hook.test.ts` (varsa mevcut desene göre), smoke

- [ ] **Step 1:** Grafik kodundan önce `dataviz` becerisini oku; renkler `tokens.css`'ten, iki temada.
- [ ] **Step 2: Testler** — `unsupported server is not an error toast` (Unimplemented → `unsupported=true`, `error=null`); smoke: kartta üç ölçer, sunucu sayfasında panel ve en az bir uygulama satırı, ikinci yoklamadan sonra CPU "—" değil, 760×520'de yatay taşma yok, eski sunucuda "upgrade" metni.
- [ ] **Step 3–4:** FAIL → uygula → PASS (`npm test`, `npm run typecheck`, `node scripts/smoke.ts`). Commit `update live resource panels`.

### Görev 8: Doğrulama, mutasyon, belge

- [ ] **Step 1:** Mutantlar: `mutate-desktop.sh`'e `rates` (eksi fark null'ı kaldırılır, CPU formülünde idle çıkarılmaz), köprü beyaz listesi; sunucu tarafında `aggregateApps` ve hostmetrics ağ/disk süzgeci için uygun betiğe (yoksa `mutate-desktop.sh` deseniyle yeni) birer mutant. Hepsi yakalanmalı.
- [ ] **Step 2:** `/verify`; kök `go test ./...`, `desktop` testleri, iki lint, `bash scripts/check-exec-surface-test.sh`, smoke.
- [ ] **Step 3:** K-166'ya ölçümler (yüzey önce/sonra, yoklama maliyeti). Commit, push, PR (`main`'e #34'ten sonra).
- [ ] **Step 4 (canlı, ayrı adım):** sürüm + iki sunucu yükseltmesi; her canlı adımdan önce kısa durum; yükseltme sonrası `kadran status` RAM satırı ve yeniden başlatmayı geçen birim (journal'da döngü yok).
