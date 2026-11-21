# Konsol canlı düzeltmeleri — Uygulama Planı

> **Ajanlar için:** GEREKLİ ALT BECERİ: superpowers:subagent-driven-development (önerilen) ya da superpowers:executing-plans. Adımlar `- [ ]` kutularıyla izlenir.

**Amaç:** K-169 "Canlıda bulunanlar" 1-6'yı kapatmak: konsol oturumsuz da açılsın ve `login` konsoldan yapılabilsin, bitiş rengi doğru satıra gitsin, sunucu rozeti nedenini söylesin, onay kutusu düzgün ve görünür açılsın, kullanımla bellek büyümesinin kaynağı ölçülsün.

**Mimari:** Konsolun seçili sunucusu GUI'nin bağlı sunucusundan ayrılır: App kendi `consoleServer` durumunu tutar; GUI'de sunucu açılınca o da değişir, `use <ad>` bağlantı kurulamasa da değiştirir. Panel her ekranda (liste ekranları dahil) aynı yerde durur. Kutu tarafı `desktop/internal/win`'de kalır (genişlik + öne getirme). Rozet nedeni saf bir işlevden gelir. Bellek işi ölçümdür; kod değişikliği ölçüm sonucuna bağlı ayrı karardır.

**Teknoloji:** Go (desktop, Wails v2), React + TypeScript (renderer, `node --test`), Windows TaskDialog.

**Spec:** `docs/specs/2026-11-18-kadran-konsolu-design.md` ve `docs/decisions.md` K-169 "Canlı" ile "Canlıda bulunanlar" (21 Kasım). Bu plan spec'te iki yeri değiştirir (R19, R20); değişiklik Task 6'da spec'e işlenir.

## Global Constraints

- Commit, squash konusu ve PR başlığı: `update <birkaç İngilizce kelime>`, gövde yok, Co-Authored-By/atıf satırı YOK.
- Arayüz güvenilmez (K-165): güvenlik kararı Go'da kalır; değiştiren/tehlikeli/root komutlar yine yalnız yerel kutudan `(true, nil)` ile çalışır. Hiçbir düzeltme kutuyu atlatan yol açmaz.
- Renk yalnız `desktop/src/renderer/styles/tokens.css` token'larından; yeni sabit renk yok; iki tema (koyu, açık) + cam çerçeve.
- Arayüz metinleri `i18n/en.json` ve `i18n/tr.json`'da aynı anahtarlarla.
- Bütçeler: paket ≤ 22.000.000 bayt (`desktop/scripts/dist.ts`), boşta bellek ≤ 300 MB (duman ikilisi, cam kapalı, 5 sahte sunucu, 60/75/90. sn).
- Mutasyon ve ağır koşular TEK TEK; `npm run e2e` öncesi `npm run build`.
- Belgelerde gizli değer, gerçek IP ya da sunucu adresi yok.
- Go PATH'te değil: `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:/c/msys64/mingw64/bin:$PATH"`.

## Review Focus

1. **Oturumu düşmüş sunucuda `login`:** panel açılır, `use hetzner` kabul edilir, `login` kod ister, kod sonrası GUI o sunucuyu açar (Task 1 duman senaryosu `console login without session`).
2. **GUI'de başka sunucuya geçmek komut sürerken:** çalışan komut başladığı sunucuda biter, sonra konsol yeni sunucuya geçer (mevcut `server change keeps running prompt` testi Task 1 sonrası da geçmeli; yeni durum makinesiyle yeniden yazılırsa aynı iddia korunur).
3. **Kutu arka plandayken:** Kadran öndeki pencere değilken kutu açılırsa pencere öne gelir ya da görev çubuğunda yanıp söner; kutu varsayılanı yine Vazgeç (Task 4 `TestBringOwnerForward`).
4. **Uzun etki metni:** kutu genişliği sabit, etki satırı Windows'ta kelime ortasından bölünmez (Task 4 `TestTaskDialogWidth` + Task 6 canlı bakış).
5. **Başarısız komut:** çıktının hiçbir satırı yeşile/kırmızıya boyanmaz; işaret yalnız komut satırında (Task 2 `finish marks the command line`).

---

### Task 1: Konsol bağlantıdan bağımsız (Bulgu 1)

**Files:**
- Create: `desktop/src/renderer/lib/consoleTarget.ts` (seçili konsol sunucusunun saf durum makinesi)
- Modify: `desktop/src/renderer/App.tsx:179-211, 265-270` (panel her ekranda; Ctrl+` her ekranda; `consoleServer` durumu)
- Modify: `desktop/console_smoke.go` (sahte kadrand'a `Login` RPC; "totp" sahte sunucusunda `TotpRequired: true`)
- Modify: `desktop/test/smoke/console-journey.ts`
- Test: `desktop/test/console-target.test.ts`

**Interfaces:**
- Produces: `consoleTarget.ts`:
  - `type TargetEvent = { kind: "gui"; server: string | null } | { kind: "use"; server: string } | { kind: "done" }`
  - `interface ConsoleTarget { server: string; pending: string | null; running: boolean }`
  - `nextTarget(t: ConsoleTarget, e: TargetEvent): ConsoleTarget` — çalışma sürerken gelen `gui`/`use` `pending`'e yazılır, `done`'da uygulanır; çalışma yokken hemen uygulanır. `gui` olayı `null` ise sunucu değişmez (liste ekranına dönmek konsolun sunucusunu silmez).
- Konsolun `server` girdisi artık `profiles.find(p => p.target === active)` değil, `ConsoleTarget.server`.
- `onUse(name)`: `nextTarget(..., {kind:"use"})` + GUI'de `openServer` denemesi; bağlantı kurulamasa da konsol sunucusu değişmiş kalır.
- `login` (sınıfı Session) `done` exit 0 ile bitince App seçili konsol sunucusunu GUI'de yeniden açmayı dener (`openServer`).

- [ ] **Adım 1: Başarısız testleri yaz**
  - `console-target.test.ts`:
    - `use without connection changes console server` — `{server:"", pending:null, running:false}` + `use hetzner` → `server: "hetzner"`.
    - `gui null keeps console server` — `gui null` → sunucu aynı.
    - `change while running waits for done` — `running:true` iken `gui gcp` → `server` aynı, `pending: "gcp"`; `done` → `server: "gcp"`, `pending: null`.
  - Duman (`console-journey.ts`), yeni senaryo `console login without session`:
    - Sunucular ekranında Ctrl+` → panel görünür (`[data-testid="console-panel"]`).
    - `use totp` → istem `totp ›`.
    - `login` → `console:input` istemi; kod `000000` girilir → done exit 0; kod tamponda yok.
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && npm test` → `console-target` FAIL.
- [ ] **Adım 3: Uygula**
  - `consoleTarget.ts` yukarıdaki imzayla.
  - `App.tsx`: `ConsolePanel` sunucu dalından çıkıp ana düzende her ekranın altına; Ctrl+` dinleyicisi `active === null` iken de kurulur; `consoleShown = appearance.consoleOpen` (gizli kart kuralı aynı: panel görünürken yaşar).
  - Sahte kadrand: `Login` RPC (kod `000000` kabul, başkası `Unauthenticated`); "totp" sahte sunucusunun Ping'i `TotpRequired: true`.
- [ ] **Adım 4: Doğrula** — `cd desktop && npm test && npm run typecheck && npm run build && npm run e2e` → PASS; mevcut `server change keeps running prompt` ve `panel close keeps run, closes card` testleri de geçer.
- [ ] **Adım 5: Commit** — `update console without session`

### Task 2: Bitiş işareti komut satırında (Bulgu 2)

**Karar R19 (spec "komut bitince son satır yeşil ya da kırmızı" yerine):** çıktı satırları hiç boyanmaz. Bitiş, komutun yankı satırının (`<sunucu> › <komut>`) başındaki işaretle gösterilir: exit 0 → işaret `--ok` token'ı; sıfır dışı → `--bad` token'ı ve yankı satırının sonuna ` · exit N`. Gerekçe: canlıda `app list`'in son uygulaması ve `logs`'un son satırı çıktının parçası gibi yeşile boyandı.

**Files:**
- Modify: `desktop/src/renderer/lib/consoleBuffer.ts:87-91` (`finish`), `lib/consoleSession.ts` (çalışmanın yankı satırının kimliğini tutar)
- Modify: `desktop/src/renderer/components/console/ConsoleOutput.tsx:8-13`, `styles/console.css`
- Test: `desktop/test/console-buffer.test.ts`, `desktop/test/console-card.test.ts` (oturum testleri orada)

**Interfaces:**
- `ConsoleLine.id` (mevcut, tamponda tekil) kullanılır. `finish(state, lineId: number, exit: number): BufferState` — yalnız o satıra `exit` yazar; açık son satırı kapatır ama ona `exit` yazmaz.
- `ConsoleSession` çalışma başına `echoLine: number` tutar; `finishRun` onu `finish`'e verir.

- [ ] **Adım 1: Başarısız testler**
  - `finish marks the command line, not output` — yankı + 3 çıktı satırı, `finish(echoId, 0)` → yalnız yankı satırında `exit === 0`, çıktı satırlarında `exit === null`.
  - `failed run adds exit suffix` — `finish(echoId, 2)` → yankı satırı `exit === 2`; çıktı boyanmaz.
  - `late output after done stays plain` — done sonrası gelen çıktı `exit === null`.
  - `echo line dropped by the 5000 cap` — yankı satırı tampondan düşmüşse `finish` hiçbir satırı değiştirmez, hata vermez.
- [ ] **Adım 2: Koş, düştüğünü gör** — `npm test` → FAIL.
- [ ] **Adım 3: Uygula** — `lineClass`: `is-exit-*` yalnız `stream === "echo"` satırlarında; renk token'ları; ` · exit N` metni çeviriye girmez (sayı).
- [ ] **Adım 4: Doğrula** — `npm test && npm run typecheck && npm run build && npm run e2e` → PASS.
- [ ] **Adım 5: Commit** — `update console exit marker`

### Task 3: Rozet nedenini söyler (Bulgu 3)

Canlıda rozet "Healthy" ile "Attention required" arasında gidip geldi, Alarmlar boştu. `healthOf` (`desktop/src/renderer/lib/health.ts:28-53`) dört yoldan "attention" döner: veri bayat (`stale`/`verifyStale`), `executor_reachable` false, doğrulama zinciri VALID değil. Hangisi olduğu bilinmiyor. Bu görev önce nedeni görünür yapar, sonra kök nedeni bulup düzeltir.

**Files:**
- Modify: `desktop/src/renderer/lib/health.ts` (neden döndüren işlev), rozet bileşeni (`grep -rn "healthOf(" desktop/src/renderer` ile bulunan yerler), `i18n/*`
- Test: `desktop/test/health.test.ts` (yoksa oluştur)

**Interfaces:**
- `type HealthReason = "stale" | "executor-unreachable" | "chain-not-valid" | "no-verify" | null`
- `healthReason(status, verify, stale): HealthReason` — `healthOf` ile aynı dallar; `healthOf` sonucu değişmez.
- Rozet `title` ve `aria-label`'ına nedenin çevirisi eklenir (`health.reason.<neden>`).

- [ ] **Adım 1: Başarısız testler** — her dal için `healthReason` beklenen değeri; `healthOf` mevcut testleri aynen geçer.
- [ ] **Adım 2: Koş, düştüğünü gör.**
- [ ] **Adım 3: Uygula.**
- [ ] **Adım 4: Kök neden** (superpowers:systematic-debugging): duman ikilisinde uzun bir `deploy`/`logs -f` sürerken rozet nedenini oku (`document.querySelector(<rozet>).title`). Bayatlık konsol çalışırken GUI yoklamasının zaman aşımından geliyorsa düzeltme o yoklamada yapılır (konsol bağlantısı GUI yoklamasını bekletmemeli); başka bir nedense raporda kanıtıyla yazılır ve kanıta göre düzeltilir. Düzeltilen neden için bir test eklenir.
- [ ] **Adım 5: Doğrula** — `npm test && npm run typecheck && npm run build && npm run e2e` → PASS.
- [ ] **Adım 6: Commit** — `update health badge reason`

### Task 4: Onay kutusu genişliği ve öne gelmesi (Bulgu 4, 5)

**Files:**
- Modify: `desktop/internal/win/taskdialog_windows.go` (`cxWidth`; açılmadan önce sahibi öne getirme)
- Test: `desktop/internal/win/taskdialog_windows_test.go`

**Interfaces:**
- `TASKDIALOGCONFIG.cxWidth = 320` (diyalog birimi; 0 iken Windows kendi genişliğini seçiyor ve etki satırını dağıtıyordu).
- `bringForward(owner HWND, api foregroundAPI)`:
  - `foregroundAPI` küçük arayüz: `Foreground() HWND`, `IsIconic(HWND) bool`, `Restore(HWND)`, `SetForeground(HWND) bool`, `Flash(HWND)`.
  - Sahip zaten öndeyse hiçbir şey yapmaz. Simge durumundaysa `Restore`. `SetForeground` dener; `false` dönerse (Windows ön plan kilidi) `Flash` (`FLASHW_ALL|FLASHW_TIMERNOFG`) ile görev çubuğunda yanıp söner.
  - `Confirm`, TaskDialog'u açmadan hemen önce çağırır.

- [ ] **Adım 1: Başarısız testler**
  - `TestTaskDialogWidth`: kurulan yapılandırmada `cxWidth` alanı 320 (alanın x64 ofseti testte sabitlenir, varsayılan düğme testinin yanında).
  - `TestBringOwnerForward` (sahte `foregroundAPI`): öndeyse çağrı yok; simgedeyse `Restore` + `SetForeground`; `SetForeground` false ise `Flash` bir kez.
  - Varsayılan düğmenin Vazgeç olduğu mevcut test değişmeden geçer.
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && go test ./internal/win/` → FAIL.
- [ ] **Adım 3: Uygula.**
- [ ] **Adım 4: Doğrula** — `cd desktop && go test -race ./... && golangci-lint run ./... && golangci-lint run --build-tags desktop,production,smoke ./...` → PASS.
- [ ] **Adım 5: Commit** — `update confirm box width and focus`

### Task 5: Kullanımla bellek büyümesi ölçümü (Bulgu 6)

Canlı ölçüm (K-169): yeni açılışta 275-280 MB, ~2 saat kullanım sonrası 407 MB (GPU süreci 113 → 209 MB, sayfa 36 → 54 MB), boşta 3 dk sabit. Bu görev büyümenin konsoldan mı genel arayüzden mi geldiğini ölçer. Ölçüm sonucu kod değişikliği gerektirirse bu PR'a girmez; K-169'a yazılır ve ayrı karar olur.

**Files:**
- Create: `desktop/scripts/measure-memory.mjs` (scratchpad'deki Task 9 betiğinin depoya alınmış hâli; `smoke` etiketiyle derler, süreç ağacının `PrivateMemorySize64` toplamını ve GPU/renderer ayrımını yazar)
- Modify: `docs/decisions.md` K-169 (sonuçlar)

**Interfaces:**
- `node desktop/scripts/measure-memory.mjs <idle|console-load|navigate-load>`:
  - `idle`: bütçe ölçümü (60/75/90. sn).
  - `console-load`: 200 `app list` + 5 dk `logs -f` (sahte akış), sonra 60 sn boşta ölç.
  - `navigate-load`: aynı süre boyunca yalnız ekranlar arası gezinme (konsol kapalı), sonra 60 sn boşta ölç.
  - Her biri başlangıç ve bitiş toplamını, GPU ve renderer payını basar.

- [ ] **Adım 1: Betiği yaz** ve `idle` ile koş; sonuç 300 MB altında olmalı (21 Kasım: 293-298).
- [ ] **Adım 2: `console-load` koş** (tek başına).
- [ ] **Adım 3: `navigate-load` koş** (tek başına).
- [ ] **Adım 4: Karşılaştır ve yaz:** `console-load` büyümesi `navigate-load`'dan 30 MB'tan fazlaysa kaynak konsoldur → K-169'a "açık iş" olarak yazılır; değilse genel arayüz/WebView2 → K-169'a gözlem olarak yazılır. Sayılar ve koşullar aynen.
- [ ] **Adım 5: Commit** — `update memory growth measurement`

### Task 6: Belgeler, doğrulama, PR (onaylı)

**Files:**
- Modify: `docs/specs/2026-11-18-kadran-konsolu-design.md` (R19 bitiş işareti; R20 konsol bağlantıdan bağımsız, her ekranda; işaret: "(canlıda değişti, K-169)")
- Modify: `docs/decisions.md` K-169 (R19, R20, düzeltilen bulgular, Task 3 kök nedeni, Task 5 sonuçları), `CHANGELOG.md` Unreleased

**Karar R20 (spec "Sunucu ekranının altında" yerine):** panel her ekranın altında; konsolun seçili sunucusu GUI'nin bağlı sunucusundan ayrı tutulur; bağlantı kurulamayan sunucu da `use` ile seçilebilir, böylece `login` konsoldan yapılır.

- [ ] **Adım 1: Belgeler** (Türkçe, görünmez karakter taraması).
- [ ] **Adım 2: Tam doğrulama** — kök ve `desktop`: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (desktop'ta ayrıca `--build-tags desktop,production,smoke`), `go test -race -count=1 ./...`; `npm test`, `npm run typecheck`, `npm run build`, `npm run e2e`; `bash scripts/cli-golden.sh check`; `bash scripts/check-exec-surface.sh`; `npm run dist` (≤ 22.000.000 bayt); `bash scripts/mutate-console.sh` ve `bash scripts/mutate-desktop.sh` TEK TEK.
- [ ] **Adım 3: Bütün dal incelemesi** (en yetenekli model).
- [ ] **Adım 4: Onayla:** push + PR (`update console live fixes`); CI kullanıcı haber verince bir kez okunur; birleştirme onayla.
- [ ] **Adım 5: Canlı, her adım onaylı:** oturumu düşmüş sunucuda konsoldan `use` + `login`; uzun etki metinli kutu; Kadran arka plandayken kutunun öne gelmesi; rozetin nedeni.
