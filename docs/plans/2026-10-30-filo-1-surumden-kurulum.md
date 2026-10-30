# Fleet part 1: install from the release — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `kadran bootstrap root@server` installs a server without a source checkout: the client downloads the server bundle of its own version from the GitHub release and accepts it only if its SHA256 equals the hash built into the client. A one-line script installs the client itself.

**Architecture:** `scripts/package-release.sh` builds the two server tarballs first, then builds the five clients with the tarballs' SHA256 values stamped in through `-X` (`internal/version`). `internal/bootstrap/release.go` downloads, verifies and unpacks a bundle into a temporary directory; `bootstrap.Run` uses it after it has detected the server architecture. `cmd/kadran` picks release mode unless the build is `dev` or the user asked for local files. `scripts/get-kadran.sh` and `scripts/get-kadran.ps1` install the client, checked against the release's `SHA256SUMS`.

**Tech Stack:** Go standard library only (`net/http`, `archive/tar`, `compress/gzip`, `crypto/sha256`); POSIX sh with `curl` or `wget`; PowerShell 5.1+.

**Spec:** `docs/specs/2026-10-30-filo-design.md`, part 1 ("Sürümden kurulum").

## Global Constraints

- `go.mod` and `go.sum` unchanged; no new dependency.
- The executor (`cmd/kadran-exec`, `internal/exec`) is untouched; `bash scripts/check-exec-surface.sh` passes.
- Protocol stays 3 (`version.Protocol`).
- Every user-visible string (CLI output, errors, script messages) is English; `go test ./cmd/kadran -run English` passes. Code comments follow the surrounding Turkish.
- The bundle hash comes only from the client binary (`-X`), never from a downloaded file. A mismatch stops the install before anything is sent to the server.
- Release asset names are unchanged: `kadran-server-<V>-linux-<arch>.tar.gz`, `kadran-<V>-<os>-<arch>[.exe]`, `SHA256SUMS`.
- Default release base: `https://github.com/erkanrzgc/kadran/releases/download`. `KADRAN_RELEASE_BASE` (an `http`/`https` URL) overrides it for a mirror or a local test server; the hash check still applies. The install scripts also accept `file://` (scripts) or a plain directory (PowerShell).
- A `dev` build, an explicit `-binaries` or `-repo`, or a set `KADRAN_BINARY_DIR` keeps today's local path exactly as it is (`geri.sh` and the e2e scripts rely on it).
- Limits: download ≤ 256 MiB; unpacked total ≤ 512 MiB; only regular files and directories under `bin/`, `deploy/`, `LICENSE`, `THIRD_PARTY_LICENSES.txt`.
- Packaging stays deterministic: two runs from one commit give the same `SHA256SUMS` (K-118).
- The spec calls the client installers `install.sh`/`install.ps1`; they are named `scripts/get-kadran.sh`/`.ps1` so they cannot be confused with the server installer `internal/bootstrap/install.sh`.
- `get-kadran` never uses sudo. Install directory: `$HOME/.local/bin` (Linux, macOS) or `%LOCALAPPDATA%\Programs\kadran` (Windows); `KADRAN_INSTALL_DIR` overrides.
- Commit messages: one line `update <few English words>`, no body, no Co-Authored-By.
- Before each commit: `gofmt -l ./cmd ./internal` empty, `go vet ./...`, `go test -count=1 ./...`, `golangci-lint run ./...`, `bash -n` on every changed `*.sh`.
- Go needs `export PATH="/c/Users/erkanrzgc/AppData/Local/Programs/go/bin:/c/Users/erkanrzgc/go/bin:$PATH"`. In PowerShell put `C:\Program Files\Git\bin` first in `PATH` before `go test` (tests call `bash`; otherwise WSL's launcher is found).

## Review Focus

1. **A user runs a release client from inside a repository checkout that has an old `bin/`.** They expect the release, and the output must say which source was used. Pinned in Task 3 (`TestBootstrapSourceLine`).
2. **The server is arm64 but the client was built without an arm64 hash** (a CI or hand build). Expect a clear refusal naming `-binaries`, not a 404 or a silent fallback to local files. Pinned in Task 2 (`TestFetchReleaseRefusesMissingHash`).
3. **The connection drops halfway through the download.** Expect an error that names the URL and no temporary directory left behind. Pinned in Task 2 (`TestFetchReleaseCleansUpOnShortBody`).
4. **The release for this client version is not published** (draft, CI build `ci-…`). Expect "not found" with the URL and the `-binaries` hint. Pinned in Task 2 (`TestFetchReleaseNotFound`).
5. **`get-kadran` replaces an existing `kadran` while that file is in use.** Expect the old binary to stay intact if the replacement fails: write to a temporary file in the same directory, then rename. Pinned in Task 6 (`atomic replace` case) and Task 7 (same case).

---

### Task 1: Server bundle hashes in the client

**Files:**
- Modify: `internal/version/version.go`
- Modify: `cmd/kadran/main.go` (`runVersion`, near line 272)
- Test: `cmd/kadran/cli_test.go`

**Interfaces:**
- Produces: `var ServerSHA256Amd64, ServerSHA256Arm64 string` (lowercase hex, empty in dev builds).

- [ ] **Step 1: Write the failing tests**

```go
func TestVersionPrintsServerBundleHashes(t *testing.T) {
	defer func(a, b string) { version.ServerSHA256Amd64, version.ServerSHA256Arm64 = a, b }(version.ServerSHA256Amd64, version.ServerSHA256Arm64)
	version.ServerSHA256Amd64 = strings.Repeat("a", 64)
	version.ServerSHA256Arm64 = strings.Repeat("b", 64)
	// run `version`; stdout contains both lines:
	//   "server bundle linux/amd64 sha256 " + 64×"a"
	//   "server bundle linux/arm64 sha256 " + 64×"b"
}

func TestVersionOmitsEmptyServerBundleHashes(t *testing.T) {
	// with both vars empty, stdout contains no "server bundle" line
}
```

- [ ] **Step 2: Run, expect FAIL** — `go test ./cmd/kadran -run 'TestVersion'`: undefined `ServerSHA256Amd64`.
- [ ] **Step 3: Add the two variables; make `runVersion` print one line per non-empty hash after the protocol line.** Comment in Turkish: filled by `package-release.sh` with `-X`; the bootstrap trusts only these values.
- [ ] **Step 4: Run, expect PASS.**
- [ ] **Step 5: Commit** — `update server bundle hashes`.

---

### Task 2: Download, verify and unpack a release bundle

**Files:**
- Create: `internal/bootstrap/release.go`
- Test: `internal/bootstrap/release_test.go`

**Interfaces:**
- Produces:

```go
// Release says where a client's own server bundle lives and what it must hash to.
type Release struct {
	Version string            // e.g. "v0.9.3"
	BaseURL string            // without trailing slash
	SHA256  map[string]string // "amd64"/"arm64" -> lowercase hex
}

const DefaultReleaseBase = "https://github.com/erkanrzgc/kadran/releases/download"

func BundleName(version, arch string) string // "kadran-server-v0.9.3-linux-amd64.tar.gz"

// FetchRelease downloads <BaseURL>/<Version>/<BundleName>, refuses it unless its
// SHA256 equals r.SHA256[arch], and unpacks it into a new temporary directory.
// dir holds bin/linux-<arch>/… and deploy/…; cleanup removes dir.
func FetchRelease(ctx context.Context, r Release, arch string, out io.Writer) (dir string, cleanup func(), err error)
```

- Package-level `var maxBundleBytes int64 = 256 << 20` and `var maxUnpackedBytes int64 = 512 << 20` (tests lower them).

Test helper (in the test file): `bundleTar(t, entries map[string]string, extra ...*tar.Header) []byte` builds a gzipped tar; `serve(t, body []byte, status int) *httptest.Server`.

- [ ] **Step 1: Write the failing tests**

| Test | Setup | Assertions |
|---|---|---|
| `TestBundleName` | — | `BundleName("v0.9.3","arm64") == "kadran-server-v0.9.3-linux-arm64.tar.gz"` |
| `TestFetchReleaseVerifiesAndUnpacks` | bundle with `bin/linux-amd64/kadrand` = `"daemon"`, `deploy/systemd/kadrand.service` = `"unit"`, `LICENSE`; correct hash | no error; files exist under `dir` with that content; `out` contains `"verified"` and the first 12 hex chars of the hash; after `cleanup()` `dir` does not exist |
| `TestFetchReleaseRejectsTamperedBundle` | serve bundle B, hash of bundle A | error contains `"does not match the hash built into this kadran"`; returned `dir == ""`; no `kadran-release-*` dir left in `os.TempDir()` created by this test (use `t.Setenv("TMPDIR", t.TempDir())` / `TMP` on Windows) |
| `TestFetchReleaseRefusesMissingHash` | `SHA256` has only `"amd64"`, arch `"arm64"` | error contains `"built without a server bundle hash for linux/arm64"` and `"-binaries"`; the server got 0 requests |
| `TestFetchReleaseNotFound` | status 404 | error contains the full URL, `"404"` and `"-binaries"` |
| `TestFetchReleaseCleansUpOnShortBody` | handler sets `Content-Length` larger than the body and returns | error mentions the URL; no temp dir left |
| `TestFetchReleaseSizeCap` | `maxBundleBytes = 1024`, body 4096 bytes | error contains `"larger than"` |
| `TestUnpackRejectsUnsafeEntries` | table: `../evil`, `/etc/passwd`, `bin/../../x`, a symlink `bin/l -> /etc`, a hardlink, a char device, a file outside the allowed roots (`etc/x`) — each with a matching hash | each returns an error; nothing written outside `dir`; `dir` removed |
| `TestUnpackSizeCap` | `maxUnpackedBytes = 10`, one 11-byte file | error contains `"unpacked size"` |

- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/bootstrap -run 'Bundle|FetchRelease|Unpack'`: undefined `FetchRelease`.
- [ ] **Step 3: Implement.** Approach:
  - Refuse an empty hash before any request.
  - GET with `http.NewRequestWithContext`, the default client (no `Client.Timeout`: it would cut the body; the bootstrap context bounds time) and `User-Agent: kadran/<version.Version>`.
  - Stream into a temp file in a new `os.MkdirTemp("", "kadran-release-*")` dir through `io.TeeReader` into `sha256.New()`, capped by `io.LimitReader(maxBundleBytes+1)`.
  - Compare hex hashes with `subtle.ConstantTimeCompare` (no secret involved; it only makes the intent obvious to readers).
  - Unpack only after the hash matches. For each entry: `TypeDir` or `TypeReg` only; clean the name with `path.Clean`, reject absolute names, `..` segments and anything outside `bin/`, `deploy/`, `LICENSE`, `THIRD_PARTY_LICENSES.txt`; create with mode `0o755` for directories and `hdr.FileInfo().Mode().Perm()` for files; count bytes against `maxUnpackedBytes`.
  - On any error remove the temp dir before returning.
  - Progress lines to `out`: `==> Downloading <name> from <host>` and `==> Verified sha256 <first 12>… (matches the hash built into this kadran)`.
- [ ] **Step 4: Run, expect PASS.** Also `go test -race ./internal/bootstrap`.
- [ ] **Step 5: Commit** — `update release bundle fetch`.

---

### Task 3: Release mode in `bootstrap` and the CLI

**Files:**
- Modify: `internal/bootstrap/bootstrap.go` (`Options`, `Run`)
- Modify: `cmd/kadran/main.go` (`runBootstrap`, lines 287–341)
- Test: `internal/bootstrap/bootstrap_test.go`, `cmd/kadran/cli_test.go`

**Interfaces:**
- Consumes: `bootstrap.Release`, `bootstrap.FetchRelease`, `bootstrap.DefaultReleaseBase` (Task 2); `version.ServerSHA256Amd64`, `version.ServerSHA256Arm64` (Task 1).
- Produces:
  - `Options.Release *Release`. When non-nil, `Run` calls `FetchRelease` right after `detectArch` and uses `dir/bin` and `dir` as `BinaryDir` and `RepoRoot`; cleanup runs when `Run` returns.
  - In `cmd/kadran`: `func useRelease(ver string, explicit map[string]bool, binaryDirEnv string) bool`. True only when `ver != "dev"`, neither `"binaries"` nor `"repo"` is in `explicit` (filled with `fs.Visit`), and `binaryDirEnv == ""`.

- [ ] **Step 1: Write the failing tests**
  - `TestUseRelease` (table): `("v0.9.3", {}, "") → true`; `("dev", {}, "") → false`; `("v0.9.3", {"binaries":true}, "") → false`; `("v0.9.3", {"repo":true}, "") → false`; `("v0.9.3", {}, "/x") → false`.
  - `TestBootstrapSourceLine`, in `internal/bootstrap` with the existing fake ssh (`sahteSSH`, `fakessh_test.go`). With `Release` pointing at an `httptest` server that serves a valid bundle, the output contains `==> Server files: release v9.9.9 (verified)`. Without `Release` it contains `==> Server files: local (<BinaryDir>, <RepoRoot>)`.
  - `TestBootstrapReleaseMismatchSendsNothing`: with a wrong hash, `Run` returns the mismatch error and the fake ssh log has no upload or install command (only `uname -m` and the privilege check).
- [ ] **Step 2: Run, expect FAIL.**
- [ ] **Step 3: Implement.**
  - In `runBootstrap`, collect explicitly set flags with `fs.Visit`.
  - When `useRelease(...)` is true, pass `&bootstrap.Release{Version: version.Version, BaseURL: env KADRAN_RELEASE_BASE or DefaultReleaseBase, SHA256: {"amd64": version.ServerSHA256Amd64, "arm64": version.ServerSHA256Arm64}}`.
  - Update the `-binaries`/`-repo` help strings to say they select local files instead of the release.
  - Print the source line in `Run`, before `buildArchive`.
- [ ] **Step 4: Run, expect PASS.** Run the whole `go test -count=1 ./...` too: existing bootstrap tests use local files and must not change.
- [ ] **Step 5: Commit** — `update bootstrap release mode`.

---

### Task 4: Stamp the hashes in `package-release.sh`

**Files:**
- Modify: `scripts/package-release.sh`

**Interfaces:**
- Consumes: `version.ServerSHA256Amd64`, `version.ServerSHA256Arm64` (Task 1).

- [ ] **Step 1: Reorder the script.** The new order is:
  1. `build-release.sh` (server binaries);
  2. third-party licenses and `LICENSE`;
  3. the two server tarballs;
  4. `amd64=$(sha256sum … | cut -c1-64)` and the same for `arm64`;
  5. the five clients, with `-X $MOD/internal/version.ServerSHA256Amd64=$amd64 -X $MOD/internal/version.ServerSHA256Arm64=$arm64` added to `LDFLAGS`;
  6. `SHA256SUMS`.

  Update the header comment: the clients carry the tarballs' hashes, so the tarballs are built first.
- [ ] **Step 2: Add a self-check after the clients are built.** For each client, `go version -m <file>` must contain `ServerSHA256Amd64=$amd64` and `ServerSHA256Arm64=$arm64`; otherwise `die "client <name> does not carry the server bundle hashes"`.
- [ ] **Step 3: Verify in WSL Ubuntu.** Use two clean LF clones of the branch, the same as the release procedure (`git clone -c core.autocrlf=false`, `buf generate`, `GOTOOLCHAIN=go1.26.10 bash scripts/package-release.sh <out> v0.0.0-test`).
  - Expected: both runs succeed and their `SHA256SUMS` are identical.
  - `kadran-v0.0.0-test-linux-amd64 version` prints both `server bundle` lines, and they equal `sha256sum` of the two tarballs.
- [ ] **Step 4: Commit** — `update release hash stamping`. (The CI "Yayın paketi" job runs this script and now also the self-check.)

---

### Task 5: Mutation script for the bundle check

**Files:**
- Create: `scripts/mutate-release.sh` (structure copied from `scripts/mutate-goc.sh`: single-match replace guard (K-127), green baseline, `go vet` build gate (K-096), restore on exit)

- [ ] **Step 1: Add these mutants of `internal/bootstrap/release.go`.** Each one must make `go test -count=1 ./internal/bootstrap` fail:
  1. the hash comparison always succeeds;
  2. only the first 12 hex characters are compared;
  3. symlink entries are accepted;
  4. the `..` / absolute-name check is removed;
  5. the allowed-roots check is removed;
  6. unpacking happens before the hash comparison (move the comparison after the unpack loop);
  7. the empty-hash refusal is removed.

  Add one mutant of `cmd/kadran/main.go`: `useRelease` ignores `explicit["binaries"]`. It must fail `go test ./cmd/kadran -run TestUseRelease`.
- [ ] **Step 2: Run** `bash scripts/mutate-release.sh`. Expected: `Bütün mutasyonlar yakalandı.`, and `git status` shows the two source files unchanged. If a mutant survives, strengthen the Task 2/3 test, not the mutant.
- [ ] **Step 3: Commit** — `update release mutation tests`.

---

### Task 6: `get-kadran.sh` (Linux, macOS)

**Files:**
- Create: `scripts/get-kadran.sh`
- Create: `scripts/check-get-kadran.sh`
- Modify: `.github/workflows/ci.yml` (job `lint`, next to "Kurulum betiği yardımcıları": step `İstemci kurulum betiği` running `bash scripts/check-get-kadran.sh`)

**Behaviour of `get-kadran.sh`** (POSIX `sh`, `set -eu`):
- Version: `KADRAN_VERSION` or the `tag_name` of `https://api.github.com/repos/erkanrzgc/kadran/releases/latest`.
- Platform: `KADRAN_OS`/`KADRAN_ARCH`, else `uname -s` (`Linux`→`linux`, `Darwin`→`darwin`) and `uname -m` (`x86_64`/`amd64`→`amd64`, `aarch64`/`arm64`→`arm64`). Anything else: `unsupported platform: <os>/<arch>`, exit 1.
- Download `<base>/<V>/kadran-<V>-<os>-<arch>` and `<base>/<V>/SHA256SUMS` into a `mktemp -d` dir. Use `curl -fsSL` or `wget -qO`; if neither exists: `need curl or wget`. `curl` reads `file://` bases.
- Verify with `sha256sum` or `shasum -a 256` against the line for that file name. On a missing line or a mismatch: `checksum mismatch for <file>; nothing installed`, exit 1.
- Install: `mkdir -p "$dir"`, copy to `"$dir/.kadran.new.$$"`, `chmod 0755`, `mv -f` over `"$dir/kadran"`. Print `installed kadran <V> to <dir>/kadran` and, if `<dir>` is not in `PATH`, `add <dir> to your PATH`. Always remove the temp dir (`trap`).

- [ ] **Step 1: Write `check-get-kadran.sh` first** (structure like `check-install-sh.sh`: `dene` lines, `fail` flag). It builds a fake release dir `R/v9.9.9/` with `kadran-v9.9.9-linux-amd64` (content `binary-v9`) and a matching `SHA256SUMS`, then runs with `KADRAN_RELEASE_BASE=file://$R KADRAN_VERSION=v9.9.9 KADRAN_OS=linux KADRAN_ARCH=amd64 KADRAN_INSTALL_DIR=$T/bin`. Cases:
  1. installs, content identical, mode 755;
  2. tampered binary: exit ≠ 0, message contains `checksum mismatch`, no `$T/bin2/kadran`;
  3. file missing from `SHA256SUMS`: same as 2;
  4. `KADRAN_ARCH=mips`: exit ≠ 0, `unsupported platform`;
  5. **atomic replace:** an existing `$T/bin3/kadran` with content `old` and a tampered release. After the run the content is still `old` and no `.kadran.new.*` is left;
  6. **control:** an unverified `cp` would have installed the tampered file. This shows case 2 measures something.
- [ ] **Step 2: Run** `bash scripts/check-get-kadran.sh` — expected FAIL (script missing).
- [ ] **Step 3: Write `get-kadran.sh`.**
- [ ] **Step 4: Run, expect all ✓;** `bash -n` both files; also run it once against the real v0.9.2 release in WSL with `KADRAN_INSTALL_DIR=$(mktemp -d)` and check `kadran version` prints `v0.9.2`.
- [ ] **Step 5: Commit** — `update client install script`.

---

### Task 7: `get-kadran.ps1` (Windows)

**Files:**
- Create: `scripts/get-kadran.ps1`
- Create: `scripts/check-get-kadran.ps1`
- Modify: `.github/workflows/ci.yml` (job `test`, step `İstemci kurulum betiği (Windows)` with `if: runner.os == 'Windows'`, `shell: pwsh`, running `./scripts/check-get-kadran.ps1`)

**Behaviour:** the same as Task 6, with these differences:
- Asset `kadran-<V>-windows-amd64.exe`; `$env:PROCESSOR_ARCHITECTURE` other than `AMD64` is `unsupported platform`.
- The base can be a URL (`Invoke-WebRequest -UseBasicParsing`) or a local directory (`Copy-Item`), the latter for tests and offline copies.
- Verify with `Get-FileHash -Algorithm SHA256`.
- Install to `$env:KADRAN_INSTALL_DIR` or `%LOCALAPPDATA%\Programs\kadran\kadran.exe`, writing `kadran.exe.new` and then `Move-Item -Force`. If the target is locked, report `kadran.exe is in use; close it and run again`, keep the old file and exit 1.
- Add the directory to the **user** `Path` (`[Environment]::SetEnvironmentVariable('Path', …, 'User')`) only when it is missing, and print that it did so. Skip this when `KADRAN_INSTALL_DIR` is set.
- Works on Windows PowerShell 5.1 and PowerShell 7.

- [ ] **Step 1: Write `check-get-kadran.ps1`** with cases 1–5 of Task 6 (local-directory base, `KADRAN_INSTALL_DIR` under `$env:TEMP`). For case 5, hold the old file open with `[IO.File]::Open(…, 'Open', 'Read', 'None')` during the run and expect `in use`.
- [ ] **Step 2: Run** `pwsh -File scripts/check-get-kadran.ps1` — expected FAIL.
- [ ] **Step 3: Write `get-kadran.ps1`.**
- [ ] **Step 4: Run, expect PASS** in both `pwsh` and `powershell.exe`. Run it once against the real v0.9.2 release with `KADRAN_INSTALL_DIR` in a temp dir and check `kadran.exe version`.
- [ ] **Step 5: Commit** — `update windows install script`.

---

### Task 8: Documentation and decision record

**Files:**
- Modify: `README.md` ("Quick start", "Install": install the client with the one-liner, then `kadran bootstrap root@server`; keep "build from source" as a subsection with `-binaries`/`-repo`)
- Modify: `CHANGELOG.md` (`## Unreleased` → `### Added`)
- Modify: `docs/decisions.md` (new `## K-159 — Sürümden kurulum: gömülü özetle doğrulanan sunucu paketi`)

- [ ] **Step 1: README one-liners**, verified against the scripts:

```bash
curl -fsSL https://raw.githubusercontent.com/erkanrzgc/kadran/main/scripts/get-kadran.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/erkanrzgc/kadran/main/scripts/get-kadran.ps1 | iex
```

Then explain, in two sentences, why the server bundle is trusted. The client carries the bundle's hash. A mirror can be used with `KADRAN_RELEASE_BASE`, and the hash is still checked.
- [ ] **Step 2: CHANGELOG `Added`** entries for the bootstrap release mode, `kadran version` printing the bundle hashes, and the two install scripts (K-159).
- [ ] **Step 3: Write K-159.** Cover:
  - **Neden:** a supply-chain risk. `SHA256SUMS` from the same release only catches corruption.
  - **Ne yapıldı.**
  - **Ölçüm:** the red-then-green tests, the mutation result, the two-clone determinism, the real-release run of both scripts.
  - **Açık:** no signature on the client itself; the one-liner trusts GitHub and TLS. Signing comes later (spec).
- [ ] **Step 4: Commit** — `update release install docs`.

---

### Task 9: Live check after the next release (user approval for each server step)

The release path can only run against a published release that carries the new hashes, so this happens after the branch is merged and the next version (v0.9.3) is released.

- [ ] **Step 1: Negative test.** Use a local mirror whose amd64 tarball has one byte changed (`KADRAN_RELEASE_BASE=file://…` is not supported by Go's client, so serve the mirror with `python3 -m http.server` on localhost) and run `kadran bootstrap -sudo panely-kurulum@<GCP>`. Expected: the mismatch error, and the server journal shows no upload: only the read-only `uname -m` and privilege check.
- [ ] **Step 2: Upgrade GCP with the release path.** Run `kadran bootstrap -sudo panely-kurulum@<GCP>` with no `-repo`/`-binaries`, probing from two points as in the v0.9.2 upgrade (`rel092/kur.sh` pattern). Expected: `Server files: release v0.9.3 (verified)`, exit 0, all probes `200`, and the running binaries' md5s equal the release.
- [ ] **Step 3: Same for Hetzner.**
- [ ] **Step 4: Record in K-159** ("Canlıda") and commit `update release install live notes [skip ci]`.

The fresh-VPS run in the spec's acceptance (a new server, no checkout) needs a purchase. Ask the user before renting one; without it, Step 2 covers the release path on a real server.
