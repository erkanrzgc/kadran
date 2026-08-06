# Contributing to Panely

Thanks for considering it. This document is short on ceremony and specific about
the one thing that matters most in this codebase: **the privileged surface**.

## The house rule

> If you write a comment claiming a security property, and an experiment could
> falsify that comment, write the experiment.

This is not a style preference. It has caught fourteen real bugs in this project,
including several where the comment and the code disagreed and only the comment was
read. A few examples now living in `docs/decisions.md`:

- A comment said transactions were immediate; the code opened deferred ones.
- A generated-code setting's comment said the opposite of what the setting did,
  which would have let a new privileged RPC silently return `Unimplemented`.
- A forbidden-field check could not match `map<...>` or `optional` fields, so it
  passed on schemas it was written to reject.

A test that has never been observed to fail is not evidence. **Break the fix,
watch the test go red, put it back.** If you can't make a test fail, say so in the
PR rather than shipping a green check that proves nothing.

## Changes to the privileged surface

Anything touching these is held to a higher bar:

- `proto/panely/v1/exec.proto`
- `internal/exec/**`
- `cmd/panely-exec/**`
- `deploy/systemd/**`

A PR here must include:

1. **A written threat rationale.** Answer the question the schema file asks:
   *"If `panelyd` were fully compromised, what would it do with this field?"*
2. **A test for each new invariant,** including escape attempts, not just the
   happy path.
3. **Evidence the test fires.** Describe the mutation you applied and the failure
   you observed.
4. **Room in the budget.** `scripts/check-exec-surface.sh` caps the privileged code
   at 2000 lines. Raising that cap is a decision, not a fix — open an issue first.

New forbidden field patterns must be added to `scripts/check-exec-surface.sh`. The
list is exported via `--list-forbidden` and the test script generates a proof for
every entry, so a pattern without a proof is structurally impossible. Keep it that way.

## Everyday workflow

```bash
go build ./...
go test -race ./...
gofmt -l .                          # must print nothing
golangci-lint run ./...             # and GOOS=windows golangci-lint run ./...
buf lint && buf format --diff --exit-code   # after editing proto/
buf generate                        # after editing proto/
scripts/check-exec-surface.sh
scripts/check-exec-surface-test.sh
```

`buf generate` succeeding does **not** mean `buf lint` will. Style rules —
for example, a streaming RPC's response message must be named
`<Rpc>Response` — are only enforced by `buf lint`, and CI runs it.

Generated protobuf code is gitignored — run `buf generate` after cloning.

Adding an RPC to `ExecutorService` **breaks the build on purpose**:
`require_unimplemented_servers=false` means the server struct must implement it.
That tripwire is load-bearing; do not embed `UnimplementedExecutorServiceServer`
to silence it.

### Cross-platform notes

Development happens on Windows; the target is Linux. Two things bite:

- Go can `Listen` on an `AF_UNIX` socket on Windows but cannot `Dial` one. Socket
  tests are build-tagged for Linux, with pipe-based equivalents running everywhere.
- Use `path`, not `filepath`, for anything describing a *container-side* path.
  `filepath` follows host separator rules and will quietly pass `C:\evil` on Windows.

## Commits and pull requests

Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`,
`perf:`, `ci:`.

A good PR body says what changed, why, and **what you measured**. "Tests pass" is
not a measurement. "Reverted the `path.Clean` check and `TestMountPathRejectsEscapeAttempts`
failed on two cases" is.

CI must be green before review: build for linux/amd64 and linux/arm64, `go vet`,
`golangci-lint` on both GOOS targets, unit tests with `-race`, the Electron build,
and the privileged-surface checks.

## Reporting bugs

Use the issue templates. For anything that crosses a privilege boundary, **do not
open an issue** — see [SECURITY.md](SECURITY.md).

## Scope

Panely deliberately does not have a web panel. The desktop app is the interface;
adding a browser-facing control surface would reintroduce exactly the attack surface
the architecture removes. PRs adding one will be declined regardless of quality.

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
