# Fleet part 2: shared server list — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One `servers.json` that both the CLI and the desktop use: `kadran server add|list|rm|rename`, `-s <name>` on every command, and a one-time import of the desktop's own profile list.

**Architecture:** A new package `internal/servers` owns the file: format, validation, atomic read-modify-write. `cmd/kadran` gets the `server` command and resolves `-s <name>` once in `run()` by rewriting the arguments (the saved target goes where a positional target goes), so every command, session file and hint sees a normal target. The desktop never touches the file: the sidecar gets `servers.*` methods, the Electron main process imports `profiles.json` through them once, and the renderer's profile store calls them one change at a time.

**Tech Stack:** Go standard library; Electron main (TypeScript, `node:test`); React renderer.

**Spec:** `docs/specs/2026-11-05-filo-2-sunucu-listesi-design.md` (approved 2026-11-05). Parent: `docs/specs/2026-10-30-filo-design.md`, part 2.

## Global Constraints

- No new dependency: `go.mod`, `go.sum`, `desktop/package.json` and its lock file unchanged.
- Server side untouched: `proto/`, `internal/api`, `internal/exec`, `cmd/kadrand`, `cmd/kadran-exec`. Protocol stays 3.
- File: `os.UserConfigDir()/kadran/servers.json`; directory 0700, file 0600; JSON `{"version": 1, "servers": [...]}` with fields `name`, `provider` (omitempty), `target`, `admin` (omitempty), `sudo` (omitempty).
- Limits: name 1–64 runes, trimmed, no control characters, must not start with `-`; provider ≤ 64 runes; at most 100 servers. Name uniqueness ignores case (`strings.EqualFold`); name lookup is exact. Target unique (exact string).
- `version` > 1: reading works, writing refuses. A file that does not parse is an error naming its path and is never overwritten.
- No key, password or token in the file.
- CLI output and errors in English (`go test ./cmd/kadran -run English` passes). Desktop strings through i18n, `en.json` and `tr.json` both. Code comments in Turkish, matching the surrounding code.
- Commit messages: one line `update <few English words>`, no body, no Co-Authored-By.
- Before each commit: `gofmt -l ./cmd ./internal` empty, `go vet ./...`, `go test -count=1 ./...`, `golangci-lint run ./...`; for desktop tasks also `npm run typecheck` and `npm test` in `desktop/`; `npm run build` before `npm run e2e` (e2e does not build).
- Go needs `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:$PATH"`. In PowerShell put `C:\Program Files\Git\bin` first in `PATH` before `go test`.

## Review Focus

1. **`-s` and a positional target together** (`kadran status -s hetzner kadran-client@other`). Expect a usage error that says `-s` already names the server, and no connection to either. Pinned in Task 3 (`TestServerFlagWithTargetNeverDials`).
2. **A name that differs only in case or is a prefix** (`-s Hetzner`, `-s hetz` when only `hetzner` is saved). Expect "no saved server" with the saved names listed, and no connection. Pinned in Task 3 (`TestServerFlagExactName`).
3. **A hand-edited file whose target looks like an ssh option** (`"target": "-oProxyCommand=calc@x"`). Expect every `-s` command and `server list` to fail naming that entry, and `ssh` never started. Pinned in Task 1 (`TestLoadRejectsOptionLikeTarget`) and Task 3 (`TestServerFlagBadFileNeverDials`).
4. **A damaged `servers.json`** (truncated JSON). Expect `server add` to fail with the path and the file to stay byte-identical. Pinned in Task 1 (`TestUpdateLeavesDamagedFile`).
5. **A desktop `profiles.json` with duplicate names, a 200-character name and a name starting with `-`.** Expect every target imported once with a valid unique name, the marker written, and a second start importing nothing. Pinned in Task 5 (`migration normalizes names and runs once`).

---

### Task 1: `internal/servers` package

**Files:**
- Create: `internal/servers/servers.go`, `internal/servers/file.go`
- Test: `internal/servers/servers_test.go`, `internal/servers/file_test.go`

**Interfaces:**
- Produces:
  ```go
  const FileVersion = 1
  const MaxServers, MaxName, MaxProvider = 100, 64, 64
  type Server struct { Name, Provider, Target, Admin string; Sudo bool } // json: name, provider,omitempty, target, admin,omitempty, sudo,omitempty
  var ErrNotFound, ErrNewerFile, ErrDuplicateName, ErrDuplicateTarget error
  func DefaultPath() (string, error)                 // os.UserConfigDir()/kadran/servers.json
  type Store struct { Path string }                  // Path "" → DefaultPath()
  func (s Store) Load() ([]Server, error)            // missing file → nil, nil
  func (s Store) Update(fn func([]Server) ([]Server, error)) ([]Server, error)
  func NormalizeName(name string) (string, error)    // trims, then validates
  func Validate(list []Server) error                 // every entry + uniqueness + count
  func Find(list []Server, name string) (Server, bool) // exact match only
  func Add(list []Server, s Server) ([]Server, error)
  func Remove(list []Server, name string) ([]Server, error)
  func Rename(list []Server, old, new string) ([]Server, error)
  func UpdateByTarget(list []Server, target, name, provider string) ([]Server, error) // provider "" clears it
  func RemoveByTarget(list []Server, target string) ([]Server, error)
  func Import(list []Server, in []Server) ([]Server, int)  // returns new list and how many were added
  ```
  All list helpers return a new slice and never modify their input.

- [ ] **Step 1: Write the failing tests** (`servers_test.go`):
  - `TestNormalizeName`: `"  hetzner "` → `"hetzner"`; `""`, `"   "`, `"-x"`, `"a\tb"`, 65 × `"ş"` → error; 64 × `"ş"` and `"gcp (2)"` → OK.
  - `TestValidateEntry`: target `"-oProxyCommand=x@h"`, user `"-x@h"`, empty target → error naming the entry; `admin` `"unix:///run/x.sock"` → error (admin must be SSH); `unix:///run/kadran/api.sock` as target → OK; provider of 65 runes → error.
  - `TestValidateUniqueness`: names `hetzner` and `Hetzner` → `ErrDuplicateName`; same target twice → `ErrDuplicateTarget`; 101 entries → error.
  - `TestFindIsExact`: list with `hetzner`; `Find(list, "Hetzner")`, `Find(list, "hetz")`, `Find(list, "hetzner ")` → not found; `Find(list, "hetzner")` → found.
  - `TestHelpersDoNotMutate`: copy the input, call each helper, input unchanged.
  - `TestImportNormalizesAndSkips`: existing `{hetzner, kadran-client@a}`; import `{hetzner, kadran-client@b}`, `{hetzner, kadran-client@c}`, `{"-gcp", kadran-client@d}`, `{200 × "x", kadran-client@e}`, `{dup, kadran-client@a}` → names `hetzner (2)`, `hetzner (3)`, `gcp`, 64-rune name, count 4, `kadran-client@a` not added twice; a name that would be empty after trimming `-` and spaces becomes the target's host.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/servers`: package does not exist.
- [ ] **Step 3: Implement `servers.go`.** Target and admin go through `client.ParseTarget`; admin must not be local. `Import` truncates the base name so `base + " (n)"` fits 64 runes.
- [ ] **Step 4: Write the failing tests** (`file_test.go`, `Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}`):
  - `TestLoadMissingIsEmpty`: nil list, nil error.
  - `TestUpdateRoundTrip`: `Update` with `Add` twice, `Load` returns both in order; file starts with `{"version": 1`.
  - `TestUpdatePermissions` (skip on Windows): directory 0700, file 0600.
  - `TestUpdateLeavesDamagedFile`: write `{"version":1,"servers":[`; `Update` returns an error containing the path; file bytes unchanged.
  - `TestNewerFileReadOnly`: write version 2 with one valid server; `Load` returns it; `Update` returns `ErrNewerFile`, file unchanged.
  - `TestLoadRejectsOptionLikeTarget`: file with `"target": "-oProxyCommand=calc@x"` → `Load` error naming `-oProxyCommand`.
  - `TestUpdateFnErrorWritesNothing`: `fn` returns an error → file unchanged.
  - `TestConcurrentUpdatesStayValid`: 8 goroutines × 10 `Update` calls each adding a unique server; afterwards `Load` succeeds (every intermediate write was whole JSON; lost updates are allowed by the spec).
- [ ] **Step 5: Run, expect FAIL.**
- [ ] **Step 6: Implement `file.go`.** Write: temp file in the same directory (`os.CreateTemp`), chmod 0600, write, `Sync`, `Close`, `os.Rename`. On Windows retry the rename up to 5 times, 20 ms × attempt apart, when it fails with an access error. `Update` = `Load` (refuse `ErrNewerFile`) → `fn` → `Validate` → write. Unknown JSON fields are ignored when reading.
- [ ] **Step 7: Run `go test -count=1 ./internal/servers`, expect PASS.**
- [ ] **Step 8: Commit** — `update server list store`.

---

### Task 2: `kadran server` command

**Files:**
- Create: `cmd/kadran/server.go`
- Modify: `cmd/kadran/main.go` (`cli` struct: `servers servers.Store`; `commands()`: `{"server", "<add|list|rm|rename> …", "manages the saved server list", (*cli).runServer}`)
- Modify: `cmd/kadran/cli_test.go` (`newTestCLI` sets `servers: servers.Store{Path: filepath.Join(os.TempDir(), "kadran-cli-test-servers", "servers.json")}` so no test reads the real file)
- Test: `cmd/kadran/server_test.go`

**Interfaces:**
- Consumes: Task 1 (`Store`, `Add`, `Remove`, `Rename`, `Server`).
- Produces: `func (c *cli) serverStore() servers.Store` (returns `c.servers`; tests set `Path`).

- [ ] **Step 1: Write the failing tests** (each with its own `t.TempDir()` store):
  - `TestServerAddAndList`: `server add -provider Hetzner -admin root@203.0.113.10 hetzner kadran-client@203.0.113.10`, then `server add -sudo -admin ops@198.51.100.7 gcp kadran-client@198.51.100.7`; `server list` stdout has the header `NAME  PROVIDER  TARGET  ADMIN`, a `hetzner` row and `ops@198.51.100.7 (sudo)`.
  - `TestServerAddRefusesDuplicates`: same name (any case) or same target → exit 1, list unchanged.
  - `TestServerAddValidates`: target `-oProxyCommand=x@h` → exit 1; `-sudo` without `-admin` → usage error.
  - `TestServerListEmpty`: stdout says the list is empty and shows `kadran server add <name> <target>`.
  - `TestServerRmAndRename`: `rm hetzner` removes only that row; `rename gcp google` keeps target, admin and sudo; `rm missing` → exit 1 naming the saved servers.
  - `TestServerDoesNotDial`: `c.dial` set to a func that calls `t.Fatal`; all four subcommands succeed without it.
- [ ] **Step 2: Run, expect FAIL** — `go test ./cmd/kadran -run TestServer`.
- [ ] **Step 3: Implement `runServer`** with subcommands `add`, `list`, `rm`, `rename`; each change goes through `c.serverStore().Update`. Usage text lists the four forms from the spec.
- [ ] **Step 4: Run, expect PASS**, then the whole `go test ./cmd/kadran` (the English and usage tests must still pass).
- [ ] **Step 5: Commit** — `update server command`.

---

### Task 3: `-s <name>` for every command

**Files:**
- Create: `cmd/kadran/serverflag.go`
- Modify: `cmd/kadran/main.go` (`run()`; `cli.serverName string`; `fail()` hint; `usageError` hint)
- Test: `cmd/kadran/serverflag_test.go`

**Interfaces:**
- Consumes: Task 1 (`Find`, `Server`), Task 2 (`serverStore`).
- Produces: `func expandServerFlag(args []string, load func() ([]servers.Server, error)) (out []string, name string, err error)`; the error is `*serverFlagError{usage bool}` so `run()` can tell a usage error from a lookup error.

Rules (the spec's decisions, here as code facts):
- Scan `args[1:]` up to a literal `--` for `-s v`, `--s v`, `-s=v`, `--s=v`. None → return `args` unchanged.
- Two `-s`, a missing value, or `-s` on `version`, `sidecar`, `server`, `help` → usage error.
- Admin commands are `bootstrap`, `key …`, `totp reset`. For them the saved `admin` is appended, and when `sudo` is true `-sudo` is inserted right after the command (after the subcommand for `key` and `totp`). No `admin` → error saying the server has no admin target and how to add one.
- Every other command: the `-s` tokens are removed and the saved `target` is appended as the last argument.
- Name not found → error listing the saved names (or saying the list is empty). No fuzzy match.

- [ ] **Step 1: Write the failing tests** (unit tests on `expandServerFlag`, table-driven, `load` returns `hetzner` (admin root@…, no sudo) and `gcp` (admin ops@…, sudo)):
  - `status -s hetzner` → `status kadran-client@203.0.113.10`.
  - `deploy -s gcp blog` → `deploy blog kadran-client@198.51.100.7`.
  - `app list --s=hetzner` → `app list kadran-client@203.0.113.10`.
  - `bootstrap -s gcp` → `bootstrap -sudo ops@198.51.100.7`; `key list -s hetzner` → `key list root@203.0.113.10`; `totp reset -s gcp` → `totp reset -sudo ops@198.51.100.7`; `totp status -s gcp` → client target.
  - `bootstrap -s nosudo-noadmin` → error mentioning `-admin`.
  - `bootstrap -s gcp -sudo=false` → `bootstrap -sudo -sudo=false ops@198.51.100.7` (the user's flag comes later and wins, as the spec says).
  - `status -s hetzner -s gcp`, `status -s`, `version -s hetzner` → usage errors.
  - `status -- -s hetzner` → unchanged.
- [ ] **Step 2: Write the failing integration tests** (`c.run` with a `t.TempDir()` store and `c.dial` recording the raw target it was given):
  - `TestServerFlagWithTargetNeverDials`, table-driven over every client command with its required positionals plus one extra target: `status`, `alarms`, `app list`, `app show blog`, `app delete blog`, `app update -domain a.example.com blog`, `deploy blog`, `rollback blog`, `logs blog`, `prune blog`, `backup list`, `backup create`, `audit list`, `audit verify`, `domain check example.com`, `login`, `logout`, `totp status`. Each `<cmd> -s hetzner … kadran-client@other` → `exitUsage`, stderr mentions `-s`, dial never called. The rewrite cannot see positionals, so each command's own argument count check is what refuses; a command without one fails this test.
  - `TestServerFlagExactName`: `status -s Hetzner` and `status -s hetz` → exit 1, stderr lists `hetzner`, dial never called.
  - `TestServerFlagDialsSavedTarget`: `status -s hetzner` → dial called with `kadran-client@203.0.113.10`.
  - `TestServerFlagBadFileNeverDials`: store file with an option-like target → exit 1, dial never called.
  - `TestDeployWithoutAppNeverDialsElsewhere`: `deploy -s hetzner` → non-zero exit and dial never called with a target other than the saved one.
  - `TestSessionHintUsesServerFlag`: dial returns a session error → stderr contains `kadran login -s hetzner`.
- [ ] **Step 3: Run, expect FAIL** — `go test ./cmd/kadran -run 'ServerFlag|DeployWithoutApp|SessionHint'`.
- [ ] **Step 4: Implement** `expandServerFlag`; call it at the top of `run()` after the help check (`load` is `c.serverStore().Load`); set `c.serverName`. In `fail()`, when `c.serverName != ""`, the login hint is `kadran login -s <name>`. In `usageError`, when `c.serverName != ""`, add one line: `-s <name> already names the server; do not also give a target`.
- [ ] **Step 5: Run, expect PASS**, then the whole `go test ./cmd/kadran`.
- [ ] **Step 6: Update `usage()`** with one line under the target forms: `-s name                   a server saved with kadran server add`.
- [ ] **Step 7: Commit** — `update server flag`.

---

### Task 4: Sidecar `servers.*` methods

**Files:**
- Create: `cmd/kadran/sidecar_servers.go`
- Modify: `cmd/kadran/sidecar.go` (`dispatch`: `case "servers.list", "servers.add", "servers.update", "servers.remove", "servers.import": return s.servers(req)`)
- Test: `cmd/kadran/sidecar_servers_test.go`

**Interfaces:**
- Consumes: Task 1, Task 2 (`serverStore`).
- Produces (JSON over the existing JSON-RPC):

  | Method | Params (only these fields) | Result |
  |---|---|---|
  | `servers.list` | none or `{}` | `{"servers": [Server…]}` |
  | `servers.add` | `name`, `target`, `provider?` | `{"servers": […]}` |
  | `servers.update` | `target`, `name`, `provider` | `{"servers": […]}` |
  | `servers.remove` | `target` | `{"servers": […]}` |
  | `servers.import` | `profiles: [{name, target, provider?}]` (≤ 100) | `{"servers": […], "imported": n}` |

  Errors use the existing `rpcError` shape; the message is the `internal/servers` error text. No method touches the network.

- [ ] **Step 1: Write the failing tests** (`runSidecarWith`-style, store in `t.TempDir()`):
  - add → list returns it; update changes name and provider, keeps `admin`/`sudo` written by the CLI beforehand; remove by target.
  - unknown field (`{"name":"a","target":"b","admin":"root@x"}` on add) → error `unknown parameter "admin"`.
  - import of three profiles where one target already exists → `imported: 2`.
  - a damaged file → every method returns an error and the file is unchanged.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** with a per-method field whitelist, the same way `decodeManagement` does it.
- [ ] **Step 4: Run, expect PASS.**
- [ ] **Step 5: Commit** — `update sidecar server list`.

---

### Task 5: Desktop main process: server list channels and the one-time import

**Files:**
- Create: `desktop/src/main/server-list.ts`
- Modify: `desktop/src/shared/channels.ts`, `desktop/src/preload/index.cjs` (new channels `serversList`, `serversAdd`, `serversUpdate`, `serversRemove` = `"kadran:servers-list"`, `"kadran:servers-add"`, `"kadran:servers-update"`, `"kadran:servers-remove"`; the old profile channels stay until Task 6)
- Modify: `desktop/src/main/index.ts` (register the channels; run the import after `migrateLegacyProfiles`, before the window loads)
- Test: `desktop/test/server-list.test.ts`

**Interfaces:**
- Consumes: Task 4 methods through `SidecarCall`.
- Produces:
  ```ts
  export const IMPORT_MARKER = "servers-imported";
  export function registerServerList(deps: { handle: ReadDeps["handle"]; call: SidecarCall }): void;
  export async function importProfiles(userDataDir: string, call: SidecarCall, warn: (m: string) => void): Promise<"imported" | "already" | "failed">;
  ```
  Channel handlers validate types before calling the sidecar: `target` through `requireTarget`, `name` a string of 1–200 characters, `provider` a string of at most 64. Each returns the `servers` array of the result as `Profile[]` (`name`, `target`, `provider` only).

- [ ] **Step 1: Write the failing tests** (fake `call` that keeps a list in memory and records calls):
  - `migration normalizes names and runs once`: `profiles.json` with `[{name:"a",target:"t1"},{name:"a",target:"t2"},{name:"-x",target:"t3"},{name:"x".repeat(200),target:"t4"}]` → `servers.import` called once with all four, marker file exists; a second `importProfiles` → `"already"`, no call.
  - `no profiles file`: marker written, `servers.import` not called.
  - `damaged profiles file`: returns `"failed"`, no marker, `warn` called (a damaged file is not "nothing to import").
  - `sidecar error`: returns `"failed"`, no marker.
  - `handlers reject bad input`: non-string `name`, 65-character provider → rejected before `call`.
- [ ] **Step 2: Run, expect FAIL** — `npm test` in `desktop/`.
- [ ] **Step 3: Implement.** `importProfiles` reads `profiles.json` itself so it can tell "missing" from "damaged" (`loadProfiles` turns both into `[]`); `profiles.json` is never written or deleted. A `"failed"` result shows the existing warning toast on the next window (i18n key `servers.import_failed`, en + tr).
- [ ] **Step 4: Run, expect PASS**; `npm run typecheck`.
- [ ] **Step 5: Commit** — `update desktop server list import`.

---

### Task 6: Desktop renderer on the shared list

**Files:**
- Modify: `desktop/src/renderer/bridge.ts` (replace `loadProfiles`/`saveProfiles` with `listServers()`, `addServer(p)`, `updateServer(target, change)`, `removeServer(target)`, each `Promise<Profile[]>`)
- Modify: `desktop/src/preload/index.cjs`, `desktop/src/shared/channels.ts`, `desktop/src/main/index.ts` (remove the old profile channels and their handlers)
- Modify: `desktop/src/renderer/lib/profile-store.ts`, `desktop/src/renderer/App.tsx`
- Modify: `desktop/test/fixtures/fake-sidecar.mjs` (`servers.*` against a JSON file at `KADRAN_FAKE_SERVERS_FILE`, empty if missing), `desktop/src/main/smoke.ts`
- Modify: i18n `en.json`, `tr.json` (duplicate name/target errors shown in the add and edit dialogs)
- Test: `desktop/test/profile-store.test.ts`

**Interfaces:**
- Consumes: Task 5 channels.
- Produces: `ProfileIO` becomes `{ load(): Promise<Profile[]>; add(p): Promise<Profile[]>; update(target, change): Promise<Profile[]>; remove(target): Promise<Profile[]>; onChange(list): void }`; `ProfileStore` gains `reload(): Promise<void>` (always re-reads). `add`/`update`/`remove` keep their signatures.

- [ ] **Step 1: Write the failing tests** (`profile-store.test.ts`):
  - `add` calls `io.add` once with only the new profile (never the whole list) and applies the returned list.
  - `reload` re-reads even when already loaded; changes made between two reloads by "another writer" appear.
  - an `io.add` error leaves the current list as it was and rejects.
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement** the store change; in `App.tsx` call `store.reload()` on `window` `focus`. Remove the old channels everywhere.
- [ ] **Step 4: Fake sidecar and smoke.** Smoke sets `KADRAN_FAKE_SERVERS_FILE` to a file in its temp user-data directory before the sidecar starts. The existing smoke `profiles.json` (11 profiles) now reaches the cards through the import. Add one step: write `{version:1, servers:[…current…, {name:"from-cli", target:"fake@cli"}]}` to that file, dispatch `focus` on the window, wait for `[data-testid="card-fake@cli"]`.
- [ ] **Step 5: Run** `npm run typecheck`, `npm test`, `npm run build`, `npm run e2e`. Expected: all pass; the e2e log shows the import and the `from-cli` card.
- [ ] **Step 6: Commit** — `update desktop shared server list`.

---

### Task 7: Mutation tests and documents

**Files:**
- Create: `scripts/mutate-servers.sh` (structure from `scripts/mutate-release.sh`: single-match guard (K-127), green baseline, `go vet` gate (K-096), restore on exit)
- Modify: `README.md` (CLI section: `kadran server …`, `-s`), `docs/guides/desktop.md` (list shared with the CLI, file location), `CHANGELOG.md` (Unreleased), `docs/decisions.md` (K-162: what was built, the calls listed in the spec, measurements)

- [ ] **Step 1: Mutants.** Each must make `go test -count=1 ./internal/servers ./cmd/kadran` fail:
  - `Find` uses `strings.EqualFold` instead of `==`.
  - `Find` uses `strings.HasPrefix(s.Name, name)`.
  - `runStatus` drops its `fs.NArg() > 1` check (then `status -s hetzner other` would dial `other`).
  - `expandServerFlag` accepts a second `-s` and keeps the last one.
  - the admin path appends `target` instead of `admin`.
  - `-sudo` is not inserted when `sudo` is true.
  - `Load` skips target validation.
  - `Update` writes despite `version` > 1.
  - the temp file is created with 0644 (Linux only; skip the mutant on Windows and say so in the output).
- [ ] **Step 2: Run** `bash scripts/mutate-servers.sh`. Expected: `Bütün mutasyonlar yakalandı.` and `git status` shows the source files unchanged. A surviving mutant means a missing test: strengthen the test, not the mutant. Then `bash scripts/check-mutation-select.sh` (the new script must be selected for `internal/servers` and `cmd/kadran/serverflag.go`).
- [ ] **Step 3: Documents.** README: one short subsection with the four commands, one `-s` example for a client command and one for `bootstrap`. Desktop guide: the list is shared with the CLI; where the file is; the first start imports the old list and leaves `profiles.json` alone. CHANGELOG Unreleased: user-facing summary. K-162: the measured results of Steps 1–2 and of the e2e run.
- [ ] **Step 4: Commit** — `update server list mutation tests and docs`.
