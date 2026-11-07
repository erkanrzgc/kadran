# Desktop shell: Electron → Wails — Implementation Plan

**Goal:** The same desktop app (same screens, same look, same JSON to the UI) on Wails v2 instead of Electron: one Go binary, no sidecar process, the React renderer unchanged except its bridge.

**Architecture:** The sidecar dispatcher moves out of `cmd/kadran` into `internal/sidecar` behind small interfaces; `kadran sidecar` keeps its stdio transport byte for byte and the desktop calls the same dispatcher in process. The desktop gets its own Go module (`desktop/go.mod`, `replace` to the root). Trust-boundary logic that lived in the Electron main process (validation, gate, native confirmation, error redaction, appearance store, glass) is ported to Go packages that do not import Wails, so they test on Linux and in the mutation job. Only `desktop/main.go` imports Wails. The renderer's `bridge.ts` becomes an adapter over the Wails bindings.

**Tech Stack:** Go (Wails v2.17.0, golang.org/x/sys/windows); React 19 renderer built by Vite; Node only for renderer tests, the build and the CDP smoke runner.

**Decision:** K-165 in `docs/decisions.md`. Parent spec: `docs/specs/2026-10-30-filo-design.md` ("Masaüstü kabuğu: Wails").

## Global Constraints

- Server side untouched: `proto/`, `internal/api`, `internal/exec`, `cmd/kadrand`, `cmd/kadran-exec`. Protocol stays 3. The root `go.mod` gains no dependency.
- `kadran sidecar` output stays identical: `cmd/kadran/sidecar_error_test.go` and the fixture tests pass unchanged.
- The UI sees the same JSON shapes as today (`desktop/test/fixtures/*.json`).
- The bound struct's exported method set equals the old bridge whitelist; pinned by a reflection test.
- Destructive actions keep a native confirmation with named buttons and Cancel as default (TaskDialog on Windows).
- CSP text unchanged (`desktop/test/csp.test.ts`). `Permissions-Policy` header added.
- User data stays in `%APPDATA%\kadran-desktop` (`appearance.json`).
- Errors that reach the UI in English (K-141); comments in Turkish.
- Commit messages: `update <few English words>`, no body, no Co-Authored-By.
- Before each commit: `gofmt`, `go vet`, `go test -count=1` for the touched module(s); `npm run typecheck` and `npm test` in `desktop/` when the renderer changes.
- Do not launch GUI windows locally without asking the user.

## Tasks

1. **Extract the dispatcher** — `internal/sidecar` (dispatch, management, servers) with injected `SessionLoader`, `ServerStore`, `CommitResolver`, warning sink; `cmd/kadran/sidecar.go` keeps only the stdio transport and wiring. Move the tests with the code. Update `scripts/mutate-servers.sh` (and any script naming `cmd/kadran/sidecar*.go`).
2. **Desktop module and logic packages (no Wails)** — `desktop/go.mod`; packages for validation (`ipc-validate`, `management-ipc`, `management-payload`), confirmation text (`dialog-text`, `management-confirmation`), redaction (`sidecar-error`), server-list mapping, appearance (sanitize, atomic save, material/look), accent policy. Port the TS tests table-driven.
3. **Bridge** — the bound struct: reads, server list, management flow (gate, single dialog, timeouts, typed cancel), appearance, window control, copy. Fake dispatcher + fake confirmer tests (port of `management-core.test.ts`, `server-list.test.ts`, `sidecar-error.test.ts`). Reflection test pins the method set.
4. **Wails main** — window (frameless, 1280×820, min 760×520, title), glass after the window is visible (trial findings), theme switch, TaskDialog confirmer, clipboard, single instance, WebView2 data path under the user data dir, asset middleware (CSP + `Permissions-Policy`), icon/manifest resource. Renderer: `bridge.ts` adapter, `management-results` moves into the renderer, error text handles Wails' string rejections.
5. **Smoke over CDP** — `smoke` build tag adds the fake dispatcher (fixtures) and auto-confirm for `fake@test`; the journey from `smoke.ts` runs from Node against WebView2's debugging port.
6. **CI and mutation** — desktop jobs: renderer tests on Ubuntu, Go logic tests on Ubuntu, Windows builds the exe, runs the smoke, checks the package. Nested-module `go vet`, lint, govulncheck. `mutate-desktop.sh` main-process mutants become Go mutants; K-164 shard table updated.
7. **Remove Electron** — `src/main`, `src/preload`, `shared/sidecar-client.ts`, `shared/channels.ts`, packager scripts, `electron`, `@electron/packager`, `koffi`; README build steps; notices for Go modules. Only after task 5 is green on Windows CI.
8. **Measure and report** — glass after focus loss, package size, idle private memory with 5 servers. Record in K-165.

## Test mapping (Electron → Wails)

| Test | Fate |
|---|---|
| appearance-schema, appearance-store | Go (`appearance`); TS schema test stays for the renderer's copy |
| window-options, window-accent | Go (look, accent policy) |
| permissions | Go (`Permissions-Policy` header) |
| ipc-validate, management-ipc, management-ipc-payload, volume-rules (main half) | Go (validation) |
| management-confirmation, i18n (dialog-text half) | Go (confirmation text, catalog parity) |
| management-core | Go (bridge flow) |
| server-list | Go (bridge server list) |
| sidecar-error | Go (redaction) |
| channels | Go reflection test + TS test that `bridge.ts` calls exactly those names |
| management-results | stays TS, moves to `src/renderer/lib` |
| errors-english | Go test over desktop Go error strings; TS part dropped with `src/main` |
| profiles | deleted: legacy migration not ported (K-165) |
| sidecar-client, sidecar-integration | deleted: no sidecar process; dispatcher covered by `internal/sidecar` tests |
| fixtures-shape, csp, built/html, forbidden-patterns, no-raw-text, style-rules, licenses, packaging-icon, i18n-families, renderer lib tests | stay TS (licenses gains Go modules) |
| smoke.ts + scripts/e2e.mjs | CDP runner (task 5) |

`mutate-desktop.sh`: mutants on `src/main/*.ts` (window-options, window-accent, permissions, ipc-validate, management-*, server-list, sidecar-error, sidecar-reads) move to Go equivalents; renderer and `shared/appearance.ts` mutants stay.
