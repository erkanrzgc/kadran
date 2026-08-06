## What changed

<!-- What and why. Link the issue if there is one. -->

## What you measured

<!--
"Tests pass" is not a measurement. Name the experiment and its result.

Good: "Reverted the path.Clean check; TestMountPathRejectsEscapeAttempts failed on
      '/var/./lib' and '/var/lib/app/..'. Restored it; green."
-->

## Checklist

- [ ] `go build ./...` and `go test -race ./...` pass
- [ ] `gofmt -l .` prints nothing
- [ ] `golangci-lint run ./...` clean for **both** `GOOS=linux` and `GOOS=windows`
- [ ] `buf generate` run if `proto/` changed
- [ ] New behaviour has a test that I have **observed fail** before the fix

## Privileged surface

<!-- Delete this section if you did not touch exec.proto, internal/exec,
     cmd/panely-exec, or deploy/systemd. -->

- [ ] Threat rationale written: if `panelyd` were fully compromised, what would it
      do with this? →
- [ ] Escape-attempt tests added, not just happy-path
- [ ] `scripts/check-exec-surface.sh` passes and stays under the 2000-line budget
- [ ] Any new forbidden field pattern has a matching proof in
      `scripts/check-exec-surface-test.sh`
