# Kadran Konsolu — uygulama planı

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Masaüstünde, sunucu ekranının altında açılan ve içinde CLI'ın komutlarının (önek olmadan) aynı kodla çalıştığı bir konsol paneli (K-169).

**Architecture:**
- `cmd/kadran`'ın komut kodu `internal/cli` paketine taşınır; `cmd/kadran/main.go` yalnız süreci kurar.
- `internal/cli` dışa üç şey açar:
  - `Run`: komutu çalıştırır;
  - `Commands`: komut ağacı ve sınıflar;
  - `Inspect`: satırı çalıştırmadan çözer.
- Masaüstünde yeni `desktop/internal/console` paketi şunları yapar: satırı ayrıştırır, sınıfa göre onay ister, kalıcı bağlantıyı tutar, geçmişi ve yönetici kaydını yazar, çıktıyı Wails olaylarıyla akıtır.
- Köprüye altı yöntem eklenir. Arayüzde alt panel ve gizli çıktı kartı var.
- Sunucu tarafı ve ayrıcalıklı yüzey değişmez.

**Tech Stack:**
- Go 1.26.11, gRPC (`internal/client`), Wails v2.17 (`runtime.EventsEmit`);
- React + TypeScript, testler `node --test`;
- bash mutasyon betikleri.

**Spec:** `docs/specs/2026-11-18-kadran-konsolu-design.md` (K-169). Plan spec'ten tartışır; ikisi birlikte okunur. Satır numaralı kod atıfları spec'te; burada yalnız gerekenler tekrarlanır.

## Global Constraints

- **CLI'ın davranışı değişmez.**
  - `kadran` ikilisinin stdout/stderr'i ve çıkış kodları taşıma öncesiyle bayt bayt aynı (Task 1'in altın çıktıları).
  - Yeniden üretilebilir derleme (K-112) bozulmaz.
  - `./cmd/kadran` derleme yolu aynı kalır (`build-release.sh`, `package-release.sh`, `ci.yml` değişmez).
- **Konsoldaki iki çıktı istisnası:**
  - `version` masaüstünün sürümünü yazar, sunucu paketi satırları yoktur;
  - `totp enable`'ın sırrı, `otpauth` adresi, QR'ı ve kurtarma kodları yalnız gizli çıktı kartına gider.
- **Sınıf tablosu (spec Tasarım 2, aynen):**
  - **okuma:** `status`, `app list`, `app show`, `logs`, `alarms`, `backup list`, `domain check`, `audit list`, `audit verify` (`-anchors` olmadan), `github status`, `totp status`, `server list`, `version`, `help`.
  - **oturum:** `login`, `logout`.
  - **değiştirme:** `app create`, `app update`, `deploy`, `backup create`, `prune -dry-run`, `github connect`.
  - **tehlikeli:** `app delete`, `rollback`, `prune` (dry-run değilse), `github disconnect`, `totp enable`, `totp disable`.
  - **root:** `key list`, `key add`, `key remove`, `totp reset`.
  - **yasak:** `sidecar`, `server add`, `server rm`, `server rename`, `bootstrap`, `audit verify -anchors`.
  - Sınıfı olmayan komut konsolda çalışmaz (kapalı varsayılan).
- **Satır kuralları:**
  - en çok 4096 bayt;
  - Cc ve Cf karakterleri reddedilir;
  - tırnak dışında `|`, `>`, `<`, `;`, `&`, `` ` ``, `$(` geçerse hata: "the console is not a shell" / "konsol kabuk değildir";
  - tek ve çift tırnak var, kaçış ve genişletme yok.
- **Sunucu bağlamı:**
  - satır başındaki tek `kadran` atılır;
  - seçili sunucuyla aynı `-s` etkisizdir, farklı `-s` reddedilir ("sunucu değiştirmek için `use <ad>`");
  - konsol hedef alan her komuta seçili sunucuyu `-s` olarak ekler;
  - konsola özgü komutlar yalnız `use`, `clear`, `help`.
- **Onay:**
  - Durum değiştiren her komut yerel onay kutusundan geçer: `Host.Confirm`, varsayılan düğme İptal, aynı anda tek kutu.
  - Tehlikeli ve root sınıfında önce yazılı ad istenir. Yazılan ad bir dikkat adımıdır, güvenlik kapısı değildir: kapı yerel kutudur.
  - Root komutları "konsolda yönetici komutları" ayarı kapalıyken çalışmaz. Ayar varsayılan kapalı, ayrı dosyada durur; yalnız `ConsoleSetAdmin` ile ve yerel kutudan geçerek açılır, kapatmak onaysızdır.
  - Root yolundaki `ssh` çağrılarına `-o StrictHostKeyChecking=yes` eklenir.
  - TOTP yolu root'a eklenmez.
- **Asla (spec):**
  - gizli değeri geçmişe yazmak: `-env` değerleri, istem girdisi;
  - TOTP sırrını, QR'ı ya da kurtarma kodlarını kart dışında göstermek ya da herhangi bir yere yazmak;
  - root anahtarını ya da parolayı saklamak;
  - konsol çıktısını iş istasyonu dışına göndermek.
- **Sınırlar ve kayıtlar:**
  - Geçmiş sunucu başına en çok 500 satır.
  - Panel tamponu en çok 5.000 satır.
  - Olay birleştirme ~50 ms.
  - Panel yüksekliği en az 120 px, en çok pencerenin %70'i.
  - Yönetici kaydı `console-admin.log`: JSONL; zaman (UTC), sunucu, arıtılmış komut, çıkış kodu, süre; yalnız ekleme, dosya izni kullanıcıya özel.
- **K-165:**
  - Köprü beyaz listedir. Yeni yöntemler `Whitelist`'e ve `bridge.ts`'e birlikte girer.
  - Sınıflandırma ve onay Go'da yapılır, arayüze güvenilmez.
  - Boşta özel bellek bütçesi 300 MB.
- **Değişmeyenler:** ayrıcalıklı yüzey (`kadran-exec`, `scripts/check-exec-surface.sh`) ve sunucu; protokol 3.
- **Metinler ve kod:**
  - CLI ve hata metinleri İngilizce (K-141); masaüstü metinleri en/tr; kod yorumları Türkçe.
  - Dosya ≤ 800 satır, fonksiyon ≤ 50 satır, iç içe derinlik ≤ 4.
- **Çalışma düzeni:**
  - Dal `kadran-konsolu`, main'den. Her commit mesajı `update <birkaç İngilizce kelime>`: gövde yok, ortak yazar satırı yok.
  - Go PATH'te değil; her kabukta önce:
    `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:/c/msys64/mingw64/bin:$PATH"`.
  - Dal değişince `buf generate`.
  - Mutasyon betikleri tek tek koşar; iki ağır koşu aynı anda olmaz.

## Review Focus

1. **Çok satırlı yapıştırma:** `deploy web\nprune -all` yapıştırılınca hiçbir şey çalışmamalı. Satır sonu Cc'dir; reddedilir ve "tek satır" der (Task 4 testi `TestParseLineRejectsPastedNewline`).
2. **Komut çalışırken sunucu değişirse:** `use gcp` ya da kenar çubuğu, çalışan komutun hedefini değiştirmemeli. `use` çalışma sürerken reddedilir. Kenar çubuğu değişse de komut başladığı sunucuda biter ve istem o sunucuyu gösterir (Task 6 testi `TestUseRefusedWhileRunning`, Task 8 testi `server change keeps running prompt`).
3. **Oturumu düşen sunucuda CLI ipucunu yapıştırmak:** CLI'ın yazdığı `kadran login -s hetzner` satırı konsolda olduğu gibi çalışmalı (Task 4 testi `TestNormalizeAcceptsLoginHint`, `loginHint`'in gerçek çıktısıyla).
4. **Seli andıran çıktı:** `logs -f` saniyede binlerce satır ya da tek bir 10 MB satır üretse de arayüz donmamalı. Olay başına en çok 32 KiB gider; tampon 5.000 satırı ve satır başına 8 KiB'ı aşmaz (Task 6 testi `TestBatchWriterSplitsLargeWrites`, Task 8 testi `consoleBuffer caps line length`).
5. **Panel kapanırken komut çalışıyorsa:** panelin kapanması komutu durdurmaz, gizli kartı kapatır. Uygulamanın kapanması komut bağlamını iptal eder (`deploy`'da sunucu sürümü mühürler) (Task 6 testi `TestShutdownCancelsRun`, Task 8 testi `panel close keeps run, closes card`).

---

### Task 1: CLI'ı `internal/cli`'a taşı (davranış değişmeden)

**Files:**
- Move: `cmd/kadran/*.go` (main.go hariç) ve `cmd/kadran/testdata/` → `internal/cli/` (`git mv`, paket adı `cli`)
- Modify: `cmd/kadran/main.go` (yalnız süreç kurulumu kalır)
- Create: `internal/cli/run.go` (dışa açık giriş)
- Create: `scripts/cli-golden.sh`, `scripts/testdata/cli-golden/` (altın çıktılar)
- Modify: `cmd/kadran/...` yolunu anan mutasyon betikleri (`grep -l 'cmd/kadran/' scripts/mutate-*.sh`; `cmd/kadran-*` ve `cmd/kadrand` atıflarına dokunulmaz)

**Interfaces:**
- Produces:
  - `cli.Env{Stdin io.Reader; Stdout, Stderr io.Writer}`. Task 2 alan ekler.
  - `func cli.Run(ctx context.Context, args []string, env cli.Env) int`.
  - Çıkış kodu sabitleri `cli.ExitOK`, `cli.ExitError`, `cli.ExitUsage`, `cli.ExitChainInvalid` (bugünkü `exit*` değerleri).

- [ ] **Adım 1: Taşımadan ÖNCE altın çıktıları üret**
  - `scripts/cli-golden.sh` aynı sabit argüman listesini derlenmiş bir `kadran` ikilisiyle koşar ve stdout, stderr, çıkış kodunu `scripts/testdata/cli-golden/<n>.{out,err,code}` dosyalarına yazar (`record` kipi); `check` kipi karşılaştırır.
  - Ağ istemeyen girdiler:
    - `` (boş);
    - `help`, `-h`, `version`;
    - `bilinmeyen`;
    - `status -h`, `app -h`, `app create -h`, `deploy -h`, `logs -h`, `prune -h`, `audit verify -h`, `totp -h`, `key -h`, `github -h`, `server -h`, `backup -h`, `domain -h`, `login -h`;
    - `deploy` (eksik argüman), `deploy -s yok web`;
    - `server list`.
  - Her koşuda `APPDATA` ve `XDG_CONFIG_HOME` boş bir geçici dizine çevrilir. Kayıtlı sunucu ve oturum dosyaları `os.UserConfigDir()` altında; bu yüzden kullanıcının gerçek listesi okunmaz.
    - `version extra`.
  - Kaydı `main`'in bugünkü HEAD'iyle al.
- [ ] **Adım 2: Dosyaları taşı, paket adını `cli` yap, `main.go`'yu incelt**
  - `main.go`'da sinyal bağlamı, `cli.Run(ctx, os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr})` ve `os.Exit` kalır.
  - Testler paketle birlikte taşınır; dışa açılmayan alanları kullanmaya devam ederler.
- [ ] **Adım 3: Mutasyon betiklerindeki yolları güncelle**
  - Yalnız `cmd/kadran/<dosya>.go` → `internal/cli/<dosya>.go` ve `./cmd/kadran` test hedefleri → `./internal/cli`. `cmd/kadran-exec`, `cmd/kadran-git`, `cmd/kadrand` atıflarına dokunma.
- [ ] **Adım 4: Doğrula**
  - `go build ./... && go vet ./... && go test -race ./internal/cli/... ./cmd/kadran/...` → PASS.
  - `bash scripts/cli-golden.sh check` → fark yok.
  - Adım 3'te değişen her mutasyon betiğini tek tek koş: hepsinde `survived 0`.
  - `go build -trimpath ./cmd/kadran` iki kez: SHA256 aynı.
- [ ] **Adım 5: Commit**
  - `git add -A internal/cli cmd/kadran scripts/cli-golden.sh scripts/testdata/cli-golden scripts/mutate-*.sh && git commit -m "update move cli into internal package"`

### Task 2: `Env` kancaları ve gizli kanal

**Files:**
- Modify: `internal/cli/run.go`, `internal/cli/main.go`'dan taşınan `cli` yapısı, `internal/cli/totp.go`, `internal/cli/domaincheck.go`, `internal/cli/key.go`
- Modify: `internal/bootstrap/bootstrap.go` (`KeyOptions`), `internal/bootstrap/upload.go`
- Test: `internal/cli/env_test.go`

**Interfaces:**
- Consumes: Task 1'in `cli.Env`, `cli.Run`.
- Produces: `cli.Env`'e eklenen alanlar. Hepsi isteğe bağlıdır; nil ise bugünkü davranış.
  - `Sensitive io.Writer` — nil ise `Stdout`;
  - `Dial func(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error)`;
  - `Command func(ctx context.Context, name string, args ...string) *exec.Cmd` — `ssh` başlatan her yer bundan geçer;
  - `Getenv func(string) string`;
  - `UserHomeDir func() (string, error)`.
- `bootstrap.KeyOptions` ve `bootstrap.Options`'a `Command func(ctx context.Context, name string, args ...string) *exec.Cmd` alanı.

- [ ] **Adım 1: Başarısız testleri yaz**
  - `TestSensitiveGetsEnrollmentOnly`: sahte sunucuyla `totp enable` koşar (mevcut `totp_test.go` sahtesi), `Sensitive` ayrı bir tampon. Sır, `otpauth://` adresi, QR satırları ve kurtarma kodları yalnız `Sensitive`'te; `Stdout`'ta bu dizelerin hiçbiri yok; "TOTP is on." ve "signed in until" `Stdout`'ta.
  - `TestSensitiveNilKeepsStdout`: `Sensitive` nil iken stdout, taşıma öncesi `totp enable` çıktısıyla bayt bayt aynı (beklenen metin testte sabit).
  - `TestCommandHookSeesSSH`: `domain check` ve `key list` (sahte `keys` OLMADAN, gerçek yol) `Command` kancasını `name == "ssh"` ile çağırır; kanca çalıştırılamayan bir `exec.Cmd` döner, komut hatayla biter, ağa çıkılmaz.
  - `TestGetenvHook`: `Getenv`'den gelen `KADRAN_RELEASE_BASE` `bootstrap` sürüm yoluna gider (kanca çağrıldı mı).
- [ ] **Adım 2: Koş, düştüğünü gör**
  - `go test ./internal/cli/ -run 'Sensitive|CommandHook|GetenvHook'` → FAIL (alanlar yok).
- [ ] **Adım 3: Uygula**
  - `printEnrollment` ve kurtarma kodu döngüsü (`runTotpEnable`) `c.sensitive()`'e yazar;
  - `os.Getenv`/`os.UserHomeDir` çağrıları `c.getenv`/`c.homeDir` üzerinden;
  - `domaincheck.go:70`, `bootstrap.go:495`, `upload.go:182` kancadan geçer (nil: `exec.CommandContext`);
  - `Env.Dial` bugünkü `cli.dial` alanına bağlanır.
- [ ] **Adım 4: Doğrula**
  - `go test -race ./internal/cli/... ./internal/bootstrap/...` → PASS;
  - `bash scripts/cli-golden.sh check` → fark yok.
- [ ] **Adım 5: Commit** — `update cli env hooks`

### Task 3: Komut ağacı, sınıflar ve `Inspect`

**Files:**
- Create: `internal/cli/commands.go` (ağaç, sınıflar, `Inspect`)
- Modify: `FlagSet` kuran her komut dosyası (`alarms.go`, `app.go`, `appdelete.go`, `appupdate.go`, `audit.go`, `backup.go`, `deploy.go`, `domain.go`, `github.go`, `key.go`, `logs.go`, `main.go` kalıntısı, `prune.go`, `rollback.go`, `server.go`, `sidecar.go`, `status.go`, `totp.go`)
- Test: `internal/cli/commands_test.go`

**Interfaces:**
- Produces:
  - `type Class string`, sabitleri:
    - `ClassRead = "read"`;
    - `ClassSession = "session"`;
    - `ClassChange = "change"`;
    - `ClassDanger = "danger"`;
    - `ClassRoot = "root"`;
    - `ClassBanned = "banned"`.
  - `type Flag struct{ Name, Usage string; Bool bool }`.
  - `type Command struct{ Name, Args, Summary string; Class Class; NoTarget bool; Flags []Flag; Subs []Command }`. Sınıf yaprakta; alt komutu olan komutun `Class`'ı boştur.
  - `func Commands() []Command`.
  - `type Invocation struct{ Path []string; Class Class; Flags map[string][]string; Positional []string }`. `Flags`: yalnız satırda verilen bayraklar, verildiği sırayla, tekrarlar korunur.
  - `func Inspect(args []string) (Invocation, error)`. Komutun kendi `FlagSet` kurucusuyla ayrıştırır, çıktıyı `io.Discard`'a verir, hiçbir şey çalıştırmaz. `-s` öncesinde çıkarılmış olmalıdır.
  - `var ErrUnclassified`.

- [ ] **Adım 1: Başarısız testleri yaz**
  - `TestEveryLeafClassified`: `Commands()` ağacında her yaprağın `Class`'ı boş değil. Yaprak listesi `commands()`'ın adları ve her `run…`'ın alt komut `switch`'iyle aynı küme; test iki listeyi karşılaştırır.
  - `TestClassTable`: Global Constraints'teki sınıf tablosu tablo testi olarak, her satır `Inspect([...]).Class`. Ayrıca:
    - `prune -dry-run web` → `ClassChange`, `prune web` → `ClassDanger`, `prune -all` → `ClassDanger`;
    - `audit verify -anchors f` → `ClassBanned`.
  - `TestInspectUsesRunFlags`: her yaprak için `Inspect` bilinmeyen bayrağı (`-yok`) reddediyor, `Commands()`'taki her bayrağı kabul ediyor. Bayrak listesi, komutun `-h` çıktısındaki bayraklarla aynı (altın `-h` dosyalarından okunur).
  - `TestInspectKeepsRepeatedEnv`: `app create -env A=1 -env=B=2 web` → `Flags["env"] == ["A=1","B=2"]`, `Positional == ["web"]`.
  - `TestUnknownCommand`: `Inspect(["yok"])` → `ErrUnclassified`.
- [ ] **Adım 2: Koş, düştüğünü gör** — `go test ./internal/cli/ -run 'Classified|ClassTable|Inspect|Unknown'` → FAIL.
- [ ] **Adım 3: Uygula**
  - Her komutun bayrak tanımı bir kurucuya çıkar: `func (c *cli) <komut>Flags() (*flag.FlagSet, *<komut>Opts)`.
  - `run…` ve `Inspect` aynı kurucuyu kullanır.
  - `commands()` tablosu `Commands()`'tan türetilir (ad, `Args`, `Summary` tek yerde).
  - `noTargetCommands` → `Command.NoTarget`.
- [ ] **Adım 4: Doğrula**
  - `go test -race ./internal/cli/...` → PASS;
  - `bash scripts/cli-golden.sh check` → fark yok.
- [ ] **Adım 5: Commit** — `update cli command classes`

### Task 4: Konsolun saf parçaları (ayrıştırma, sunucu bağlamı, arıtma, dosyalar)

**Files:**
- Create: `desktop/internal/console/line.go`, `normalize.go`, `redact.go`, `history.go`, `admin.go` (ayar + kayıt)
- Test: aynı adlarla `_test.go`

**Interfaces:**
- Consumes: `cli.Commands()`, `Command.NoTarget`, `cli.Inspect`.
- Produces:
  - `const MaxLineBytes = 4096`.
  - `func ParseLine(line string) ([]string, error)`. Hatalar: `ErrTooLong`, `ErrControl`, `ErrShell`, `ErrQuote`.
  - `func Normalize(words []string, server string) ([]string, error)`:
    - baştaki tek `kadran` atılır;
    - `-s X`, `-s=X`, `--s X`, `--s=X` için X == server ise çıkarılır, değilse `ErrOtherServer`;
    - komut `NoTarget` değilse ve `server` doluysa sona `-s`, `server` eklenir.
  - `func RejectTarget(inv cli.Invocation) error` → konumsal argüman `@` içeriyor ya da `/` ile başlıyorsa `ErrTarget`.
  - `func Redact(args []string) string`:
    - tırnaklı tek satır;
    - `-env`/`--env` değerleri, ayrı ya da `=` biçiminde, tekrarlı, `KEY=***` olur;
    - `=`'siz değer `***` olur.
  - `type History struct{ Dir string }`, `func (h History) Load(server string) []string`, `func (h History) Append(server, line string) error`. Dosya `console-history.json` (0600); sunucu başına en çok 500 satır, eskisi düşer.
  - `type AdminSetting struct{ Dir string }`, `Load() bool`, `Save(on bool) error`. Dosya `console-admin.json` (0600); okunamazsa `false`.
  - `type AdminEntry struct{ Time time.Time; Server, Command string; Exit int; Duration time.Duration }`.
  - `type AdminLog struct{ Dir string }`, `func (l AdminLog) Append(e AdminEntry) error`.
    - Dosya `console-admin.log`, `O_APPEND|O_CREATE`, 0600.
    - Alanlar `time` (UTC RFC3339), `server`, `command`, `exit`, `duration_ms`.

- [ ] **Adım 1: Başarısız testleri yaz**
  - `TestParseLine` (tablo):
    - `deploy web` → `[deploy web]`;
    - `app create "a b"` → `[app create a b]`;
    - `'x"y'` → `[x"y]`;
    - `deploy web | cat` → `ErrShell`;
    - `"a|b"` → `[a|b]`;
    - `$(x)` → `ErrShell`;
    - 4097 bayt → `ErrTooLong`;
    - `\u202e` → `ErrControl`;
    - `"abc` → `ErrQuote`.
  - `TestParseLineRejectsPastedNewline`: `"deploy web\nprune -all"` → `ErrControl`.
  - `TestNormalize`:
    - `kadran deploy web` + `hetzner` → `[deploy web -s hetzner]`;
    - `deploy -s hetzner web` → `[deploy web -s hetzner]`;
    - `deploy -s=gcp web` → `ErrOtherServer`;
    - `server list` → `[server list]` (NoTarget, -s eklenmez);
    - `kadran kadran status` → yalnız ilk `kadran` atılır.
  - `TestNormalizeAcceptsLoginHint`: `loginHint`'in `-s hetzner` için ürettiği dize (`kadran login -s hetzner`) `ParseLine` + `Normalize` sonrası `[login -s hetzner]`.
  - `TestRejectTarget`: `deploy web root@x` → `ErrTarget`; `status /run/k.sock` → `ErrTarget`; `deploy web` → nil.
  - `TestRedact`: `app create -env A=1 -env=B=2 --env C=3 --env=D web` → `app create -env A=*** -env=B=*** --env C=*** --env=*** web`; değerlerin hiçbiri çıktıda geçmez.
  - `TestHistoryCapAndPerms`: 501 ekleme → 500 satır, ilk düşmüş; dosya izni 0600 (Windows'ta izin denetimi atlanır, satır sınırı denetlenir); sunucular birbirini etkilemez.
  - `TestAdminSettingDefaultsOff`: dosya yok → `false`; bozuk JSON → `false`.
  - `TestAdminLogAppendsJSONL`: iki ekleme → iki satır, her biri geçerli JSON, `time` UTC.
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && go test ./internal/console/` → FAIL.
- [ ] **Adım 3: Uygula** (`ParseLine`'da denetim/bidi kuralı `guard`'daki işlevle aynı: `guard`'dan dışa aç, kopyalama).
- [ ] **Adım 4: Doğrula** — `cd desktop && go test -race ./internal/console/ ./internal/guard/` → PASS.
- [ ] **Adım 5: Commit** — `update console line parsing`

### Task 5: Konsol bağlantısı

**Files:**
- Modify: `internal/client/client.go`, `internal/client/session.go`
- Create: `desktop/internal/console/conns.go`
- Test: `internal/client/shared_test.go`, `desktop/internal/console/conns_test.go`

**Interfaces:**
- Produces:
  - `func (c *Client) Shared() *Client`. Aynı bağlantıya ikinci bir tutamaç:
    - `Close()` hiçbir şey yapmaz;
    - `SetSession`/`Session`/`RPC`/`Target` sahibe gider. Önleyiciler sahibin jetonunu okuduğu için tutamaçta ayrı oturum yok.
  - `type Conns struct`.
  - `func NewConns(dial func(client.Target) (*client.Client, error)) *Conns`. Üretimde `client.DialWith(t, client.DialOptions{HideConsole: true})`.
  - `func (c *Conns) Dial(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error)`:
    - hedef başına bir bağlantı tutar;
    - HER çağrıda `CheckProtocol` ile yeniden ping atar;
    - ping hatasında bağlantıyı kapatır, bir kez yeniden bağlanır;
    - `Shared()` döner.
  - `func (c *Conns) Close()`.

- [ ] **Adım 1: Başarısız testleri yaz**
  - `TestSharedCloseKeepsConn`: `Shared().Close()` sonrası sahibin RPC'si çalışıyor (bufconn'lu sahte sunucu).
  - `TestSharedSessionReachesOwner`: `Shared().SetSession("t")` → sunucu metadata'sında `t`.
  - `TestConnsRepingsEveryCall`: iki `Dial` → sunucuda iki `Ping`, tek bağlantı (dial sayacı 1). Arada `TotpRequired` false → true değişince ikinci çağrının ping'i true.
  - `TestConnsRedialsAfterFailure`: sahte ping bir kez hata → kapat + yeniden bağlan (dial sayacı 2), sonuç başarılı.
- [ ] **Adım 2: Koş, düştüğünü gör** — `go test ./internal/client/ -run Shared` ve `cd desktop && go test ./internal/console/ -run Conns` → FAIL.
- [ ] **Adım 3: Uygula** (`Client`'a sahip işaretçisi; `Close` sahipli tutamaçta no-op).
- [ ] **Adım 4: Doğrula** — `go test -race ./internal/client/...` ve `cd desktop && go test -race ./internal/console/` → PASS.
- [ ] **Adım 5: Commit** — `update console connection cache`

### Task 6: Çalıştırıcı ve onay akışları

**Files:**
- Create: `desktop/internal/console/runner.go`, `stream.go` (birleştirici yazıcı, istem okuyucu), `complete.go`
- Modify: `desktop/internal/confirm/box.go`, `text.go` (`BuildConsole`)
- Test: `desktop/internal/console/runner_test.go`, `stream_test.go`, `complete_test.go`; `desktop/internal/confirm/confirm_test.go`

**Interfaces:**
- Consumes:
  - Task 1-3: `cli.Run`, `cli.Env`, `cli.Inspect`, `cli.Commands`;
  - Task 4: `ParseLine`, `Normalize`, `RejectTarget`, `Redact`, `History`, `AdminSetting`, `AdminLog`;
  - Task 5: `Conns`.
- Produces:
  - `confirm` paketinde: `type ConsoleRequest struct{ Server, Line, Effect, Admin string }` ve `func BuildConsole(language Language, req ConsoleRequest) Box`.
    - `Line` arıtılmış satırdır; `Admin` root hedefidir (`root@…` ya da `-sudo kullanıcı@…`), değilse boş.
    - `Effect` komut yoluna göre en/tr tablodan gelir.
    - Değerler `confirm`'ün mevcut kaçış ve kesme kurallarından geçer (`safe`).
    - **Spec'ten sapma (plan kararı):** GUI'de karşılığı olan komutlar da `BuildConsole`'dan geçer: create, update, deploy, delete, rollback, backup, github disconnect. Spec bunlar için `confirm.Build`'in kalıbını istiyordu, ama o kalıp `jsonv.Object` biçiminde Spec/Update bekliyor. Bunu bayraklardan yeniden kurmak, CLI'ın bayrak → istek çevirisini masaüstünde ikinci kez yazmak olurdu; "aynı kod" ilkesine aykırı. Ortak kalan: kaçış, kesme ve İptal varsayılanı.
  - `type Event struct{ Name string; Data any }` adları:
    - `console:output` `{run, stream:"out"|"err", data}`;
    - `console:input` `{run, prompt}` (komut stdin okumak istiyor);
    - `console:secret` `{run, data}`;
    - `console:done` `{run, exit}`;
    - `console:use` `{server}`;
    - `console:clear` `{}`.
  - `type RunnerDeps struct{ Run func(context.Context, []string, cli.Env) int; Confirm func(confirm.Box) (bool, error); Emit func(Event); Conns *Conns; History History; Admin AdminSetting; Log AdminLog; Servers func() ([]servers.Server, error); Language func() confirm.Language; Apps func(ctx context.Context, server string) ([]string, error); Now func() time.Time }`.
  - `func NewRunner(d RunnerDeps) *Runner`.
  - Yöntemler:
    - `Run(server, line string) (runID string, err error)`;
    - `Input(runID, text string) error`;
    - `Cancel(runID string) error`;
    - `Complete(server, line string, cursor int) []string`;
    - `State(server string) State` (`State{History []string; Admin bool}`);
    - `SetAdmin(on bool) (bool, error)`;
    - `Shutdown()`.
  - Hatalar `ErrBusy` (bir çalışma sürüyor) ve `ErrAdminOff`.

- [ ] **Adım 1: Başarısız testleri yaz** (sahte `Run`, sahte `Confirm`, olay toplayıcı)
  - `TestReadRunsWithoutConfirm`: `status` → `Confirm` çağrılmadı, `Run` `[status -s hetzner]` ile çağrıldı, `console:done exit 0`.
  - `TestChangeNeedsConfirm`:
    - `deploy web`, `Confirm` false → `Run` hiç çağrılmadı, done exit `cli.ExitError`, geçmişe yazıldı;
    - true → `Run` çağrıldı;
    - kutu metninde sunucu ve `deploy web` var.
  - `TestConfirmBoxHidesEnv`: `app create -env K=sir web` → kutu metninde `sir` yok, `K=***` var.
  - `TestDangerNeedsTypedName`:
    - `app delete web` → `console:input` olayı;
    - `Input("weB")` → `Run` yok, "confirmation did not match" çıktısı;
    - `Input("web")` + `Confirm` true → `Run` çağrıldı.
    - `prune -all` ve `totp disable` için beklenen ad sunucu adı.
  - `TestRootGate`:
    - ayar kapalı → `key list` `ErrAdminOff`, `Run` yok;
    - açık → yazılı sunucu adı + kutu → `Run`;
    - sonra `console-admin.log`'da bir satır, `command` arıtılmış.
  - `TestBannedRefused`: `bootstrap root@x`, `server add`, `sidecar`, `audit verify -anchors f` → `Run` yok, nedeni stderr'de; bootstrap ve -anchors için "run it in the CLI".
  - `TestSetAdminNeedsConfirm`:
    - `SetAdmin(true)` + `Confirm` false → `Load()` false;
    - `Confirm` true → true;
    - `SetAdmin(false)` → `Confirm` çağrılmadı.
  - `TestSensitiveGoesToSecretEvent`: sahte `Run` `env.Sensitive`'e "SECRET" yazar → yalnız `console:secret` olayında var; `console:output` olaylarında, geçmişte ve yönetici kaydında yok.
  - `TestRootAddsStrictHostKey`: root komutunda `env.Command("ssh", args...)` çağrısının argümanlarında `-o StrictHostKeyChecking=yes` var; okuma komutunda kanca bu seçeneği eklemez.
  - `TestUseRefusedWhileRunning`: bloklayan sahte `Run` sürerken `Run("use gcp")` → `ErrBusy`; boştayken `console:use {gcp}`.
  - `TestOneRunAtATime`: ikinci `Run` → `ErrBusy`.
  - `TestCancelCancelsContext` ve `TestShutdownCancelsRun`: sahte `Run`'ın bağlamı iptal edildi; done gelir.
  - `TestBatchWriterSplitsLargeWrites`: 10 MiB tek yazma → her `console:output` ≤ 32 KiB, toplam bayt korunur; 50 ms içinde gelen küçük yazmalar tek olayda birleşir (sahte saat).
  - `TestEmptyServerList`: sunucu yokken yalnız `help`, `use`, `server list` çalışır; `status` → "select a server with use".
  - `TestHelpAndClear`:
    - `help` → `Run(["help"])` çıktısından sonra konsol kısayolları satırları (Enter, ↑/↓, Tab, Ctrl+C, Ctrl+L, Ctrl+`) gelir;
    - `clear` → `console:clear` olayı; `Run` çağrılmaz.
  - `TestUnknownHostHint`: root komutunda sahte `Run` stderr'e `Host key verification failed.` yazıp `cli.ExitError` dönerse ardından "connect once from the CLI first (kadran key list -s <sunucu>)" satırı gelir. Okuma komutunda bu satır gelmez.
  - `TestCompleteCandidates`:
    - `de` → `deploy`;
    - `app c` → `create`;
    - `deploy -` → `deploy`'un bayrakları;
    - `deploy w` → uygulama adları (sahte `Apps`, 30 sn önbellek: iki çağrıda bir `Apps`);
    - `use ` → kayıtlı sunucular.
  - `TestHistoryRedacted`: `app create -env K=sir web` sonrası `State().History` son satırda `K=***`; `Input` metni hiçbir satırda yok.
  - `confirm_test.go`'da `TestBuildConsoleEffectsComplete`: değiştirme, tehlikeli ve root sınıfındaki her yaprak için en ve tr `Effect` metni var.
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && go test ./internal/console/ ./internal/confirm/` → FAIL.
- [ ] **Adım 3: Uygula**
  - Akış sırası: `ParseLine` → yerleşikler (`use`, `clear`, `help`) → `Normalize` → `Inspect` → `RejectTarget` → sınıf kapısı → onay → `Run` (ayrı goroutine).
  - `Env`'i kur:
    - `Stdout`/`Stderr` birleştirici yazıcılar;
    - `Stdin` istem okuyucu (`Read`'de `console:input` yollar, `Input`'u bekler);
    - `Sensitive` gizli olay yazıcı;
    - `Dial` = `Conns.Dial`;
    - `Command` = pencere gizleyen kanca, root sınıfında `-o StrictHostKeyChecking=yes` ekler.
  - Root komutunun yönetici hedefi kayıttan okunur (`servers` listesi), kutuda gösterilir.
  - Sonda geçmiş (arıtılmış), root ise yönetici kaydı yazılır.
- [ ] **Adım 4: Doğrula** — `cd desktop && go test -race ./internal/console/ ./internal/confirm/` → PASS.
- [ ] **Adım 5: Commit** — `update console runner`

### Task 7: Köprü, olaylar ve kablolama

**Files:**
- Create: `desktop/internal/bridge/console.go`
- Modify: `desktop/internal/bridge/bridge.go` (`Deps.Console`, `Whitelist`), `desktop/main_windows.go` (Runner, `runtime.EventsEmit`, `OnShutdown` → `Runner.Shutdown`), `desktop/src/renderer/bridge.ts`
- Modify: `desktop/internal/guard/ipc.go` (girdi sınırları)
- Test: `desktop/internal/bridge/console_test.go`, `bridge_test.go`

**Interfaces:**
- Consumes: Task 6'nın `Runner` yöntemleri.
- Produces: köprü yöntemleri.
  - `ConsoleRun(server, line string) (string, error)`;
  - `ConsoleInput(runID, text string) error`;
  - `ConsoleCancel(runID string) error`;
  - `ConsoleComplete(server, line string, cursor int) ([]string, error)`;
  - `ConsoleState(server string) (console.State, error)`;
  - `ConsoleSetAdmin(on bool) (bool, error)`.
  - `bridge.ts`'te karşılıkları: `consoleRun`, `consoleInput`, `consoleCancel`, `consoleComplete`, `consoleState`, `consoleSetAdmin`.
  - Olay dinleyicisi `onConsoleEvent(handler)` (`EventsOn`, adlar Task 6).
- Guard sınırları:
  - `server` → `guard.ServerName`;
  - `line` ≤ 4096 bayt;
  - `text` ≤ 256 bayt, Cc/Cf yok;
  - `runID` yalnız `[0-9a-f]{16}`.

- [ ] **Adım 1: Başarısız testleri yaz**
  - `TestExportedMethodsAreExactlyTheWhitelist` ve `TestRendererBridgeCallsOnlyWhitelistedMethods`: altı yeni ad eklenmiş hâliyle; `bridge.ts` çağırmıyorsa düşer.
  - `TestConsoleRunNeverRunsUnconfirmed`: sahte Host `Confirm` false → `deploy web` için sahte `Run` hiç çağrılmadı.
  - `TestConsoleSetAdminOnlyViaHost`: `SaveAppearance` ile `{"consoleAdmin":true}` gibi bir alan yazmak ayarı açmaz (`State().Admin` false).
  - `TestConsoleGuards`: 4097 baytlık satır, bidi içeren girdi, `../x` runID → hata, `Runner` çağrılmadı.
  - `Fixture` (duman) bayrağı konsolun onay kutusunu ATLAMAZ; testi: `Fixture: true` iken `Confirm` yine çağrılır.
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && go test ./internal/bridge/` → FAIL.
- [ ] **Adım 3: Uygula** (köprü yöntemleri yalnız guard + `Runner` çağrısı; mantık Task 6'da).
- [ ] **Adım 4: Doğrula**
  - `cd desktop && go test -race ./... && npm run typecheck` → PASS;
  - `go build -tags desktop,production .` derleniyor.
- [ ] **Adım 5: Commit** — `update console bridge methods`

### Task 8: Panel (arayüz)

**Files:**
- Create:
  - `desktop/src/renderer/components/console/ConsolePanel.tsx`, `ConsoleOutput.tsx`, `ConsolePrompt.tsx`, `SecretCard.tsx`;
  - `desktop/src/renderer/hooks/useConsole.ts`;
  - `desktop/src/renderer/lib/consoleBuffer.ts`, `consoleHistory.ts`, `consoleKeys.ts`.
- Modify:
  - `desktop/src/renderer/App.tsx` (panel sunucu ekranlarının altında);
  - `desktop/src/renderer/i18n/*` (en/tr);
  - `desktop/src/renderer/styles/*` (token'lar);
  - `desktop/internal/appearance/appearance.go` (`ConsoleOpen bool`, `ConsoleHeight int`; varsayılan kapalı, 240; doğrulama 120..4000) ve renderer tarafındaki görünüm türü;
  - `desktop/src/renderer/screens/SettingsScreen.tsx` ("konsolda yönetici komutları" anahtarı → `consoleSetAdmin`).
- Test: `desktop/test/console-buffer.test.ts`, `console-history.test.ts`, `console-keys.test.ts`, `console-card.test.ts`; `desktop/internal/appearance/appearance_test.go`

**Interfaces:**
- Consumes: Task 7'nin `bridge.ts` yöntemleri ve olayları.
- Produces:
  - `consoleBuffer.append(state, stream, text) → state`. Satır sınırı 5.000; satır başına en çok 8 KiB, fazlası kesilir ve `…` eklenir.
  - `consoleHistory.move(state, "up"|"down") → state`.
  - `consoleKeys.action(event) → "run"|"history-up"|"history-down"|"complete"|"cancel"|"clear"|"toggle"|null`.

- [ ] **Adım 1: Başarısız testleri yaz** (`node --test`)
  - `consoleBuffer caps lines`: 5.001 satır → 5.000, ilk satır düşmüş.
  - `consoleBuffer caps line length`: 10 MiB'lık tek satır → 8 KiB + `…`.
  - `consoleHistory navigates`: ↑ son satır, ↑↑ bir önceki, ↓ yazılmakta olan taslağa döner.
  - `consoleKeys`:
    - Ctrl+` → `toggle`, Esc → `null`;
    - Ctrl+C → çalışma varken `cancel`, yokken seçili metin kopyalama için `null`;
    - Ctrl+L → `clear`, Tab → `complete`.
  - `panel close keeps run, closes card`: kart durumu panel kapanınca boşalıyor; çalışma kimliği duruyor.
  - `server change keeps running prompt`: çalışma sürerken seçili sunucu değişince istem çalışmanın sunucusunu gösteriyor; `done` sonrası yeni sunucuya geçiyor.
  - `secret never enters buffer`: `console:secret` olayı tampona hiçbir satır eklemiyor, yalnız "secret output shown in a separate card" / "gizli çıktı ayrı kartta gösterildi" satırı ekliyor; kart kapanınca kart durumu boş dize değil `null`.
  - Go: `TestAppearanceConsoleDefaults` (`false`, 240) ve `TestAppearanceConsoleHeightClamped` (50 → 120, 99999 → 4000).
- [ ] **Adım 2: Koş, düştüğünü gör** — `cd desktop && npm test && go test ./internal/appearance/` → FAIL.
- [ ] **Adım 3: Uygula**
  - Panel ve istem:
    - yazı JetBrains Mono;
    - istem `<sunucu> ›` vurgu renginde, root çalışırken `<sunucu> #` tehlike renginde;
    - komut bitince son satır yeşil (0) ya da kırmızı;
    - stderr ikincil renkte;
    - çalışırken istem kilitli ve "running · Ctrl+C to stop" / "çalışıyor · Ctrl+C ile durdur" yazar (yalnız `console:input` gelince açılır).
  - Kart, panelin üstünde açılır:
    - içerik `pre`, eş aralıklı, `line-height: 1`, `user-select: none`;
    - "Kaydettim" / "I saved it" düğmesi.
  - Sürükle-yeniden boyutla: 120 px .. pencerenin %70'i; açık/kapalı ve yükseklik `saveAppearance` ile.
  - Panel üç temada aynı token'larla.
- [ ] **Adım 4: Doğrula** — `cd desktop && npm test && npm run typecheck && npm run build && go test ./...` → PASS.
- [ ] **Adım 5: Commit** — `update console panel`

### Task 9: Duman testi, mutasyon, belgeler

**Files:**
- Create:
  - `desktop/console_smoke.go` (`//go:build smoke`): bufconn üstünde sahte kadrand, `Conns`'a verilen `dial`;
  - `scripts/mutate-console.sh`.
- Modify:
  - `desktop/scripts/smoke.ts` (senaryolar);
  - `scripts/check-mutation-shard.sh` ve süre tablosu (yeni betik matrise);
  - `docs/decisions.md` (K-169'a "Uygulama" bölümü);
  - `CHANGELOG.md`;
  - `README.md` (masaüstü bölümünde bir paragraf).

- [ ] **Adım 1: Sahte kadrand**
  - Sahte sunucu şu RPC'leri yanıtlar: `Ping`, `ListApps`, `GetApp`, `Deploy` (akış, 3 satır), `StreamLogs` (iptale kadar satır), `BeginTotpEnrollment`/`ConfirmTotpEnrollment` (sabit sahte sır `JBSWY3DPEHPK3PXP`, kurtarma kodları `aaaa-bbbb` biçiminde).
  - Duman ikilisinde `Conns` bu sahteyle kurulur. Dağıtılan ikilide dosya yok.
- [ ] **Adım 2: Duman senaryoları**
  - `app list` çıktısı panelde.
  - `deploy web`: onay kutusu sahte Host'ta, 3 satır akar, son satır yeşil.
  - `logs -f web` + Ctrl+C: done gelir, istem açılır.
  - `totp enable` (danger: sunucu adı yazılır):
    - kart açılır, sahte sır kartta;
    - `document.body.innerText` içinde kart dışında YOK;
    - "Kaydettim" sonrası DOM'da hiç yok.
  - `key list`, ayar kapalıyken reddedilir.
  - 760×520'de taşma yok.
- [ ] **Adım 3: Mutasyon betiği** (`mutate-desktop.sh`'nin deseniyle; her mutant yakalanmalı)
  - Bir yaprağın sınıfını `ClassRead` yap.
  - `Redact`'te `=`'li biçimi atla.
  - Yazılı ad karşılaştırmasını `strings.EqualFold` yap.
  - `ErrAdminOff` denetimini kaldır.
  - `Sensitive`'i `Stdout`'a bağla.
  - `Normalize`'da farklı `-s`'i kabul et.
  - `StrictHostKeyChecking` eklemesini kaldır.
  - Birleştirici sınırını 32 KiB'tan kaldır.
- [ ] **Adım 4: Belgeler**
  - K-169 "Uygulama": commit listesi, alınan kararlar, ölçümler (bellek: konsol açık ve 5.000 satır doluyken boşta özel bellek; 300 MB altında olmalı).
  - CHANGELOG'da "Unreleased" girdisi.
- [ ] **Adım 5: Doğrula**
  - `cd desktop && npm run e2e` → PASS.
  - `bash scripts/mutate-console.sh` → survived 0. Tek başına koşar.
  - `bash scripts/check-mutation-shard.sh` → PASS.
- [ ] **Adım 6: Commit** — `update console smoke and docs`

### Task 10: Bütün dal doğrulaması, inceleme, PR (onaylı)

- [ ] **Adım 1:**
  - `/verify`: build, vet, `golangci-lint`, `go test -race ./...` (kök ve `desktop`), `npm test`, `npm run typecheck`, `npm run e2e`, `scripts/cli-golden.sh check`, `scripts/check-exec-surface.sh` (değişmemiş).
  - Taşınan mutasyon betikleri ve `mutate-console.sh` tek tek koşar.
- [ ] **Adım 2: Bütün dal incelemesi** (en yetenekli model; güvenlik odağı):
  - onaysız durum değiştirme;
  - gizli değer sızıntısı (geçmiş, kayıt, olaylar, günlük);
  - K-165 beyaz listesi;
  - CLI çıktısının değişmediği.
- [ ] **Adım 3: Onayla açılacaklar**
  - Push ve PR (başlık `update kadran console`). CI kullanıcı haber verince bir kez okunur. Birleştirme onayla.
- [ ] **Adım 4: Elle canlı, her adım ayrı onay, öncesinde kısa durum**
  - Masaüstü main'den derlenir. İki sunucuda `login`, `app list`, `deploy ozel-deneme`, `logs -f` + Ctrl+C, `rollback ozel-deneme` (yazılı onay); ayar açıkken `key list`.
  - Konsoldan `domain check` ve `key list` çalışırken konsol penceresi açılmıyor.
  - Bellek ölçümü K-169'a yazılır.
