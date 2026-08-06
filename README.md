<div align="center">

# Panely

**A self-hosted deployment platform whose control panel never runs as root.**

[![CI](https://github.com/erkanrzgc/panely/actions/workflows/ci.yml/badge.svg)](https://github.com/erkanrzgc/panely/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/erkanrzgc/panely.svg)](https://pkg.go.dev/github.com/erkanrzgc/panely)
[![Go Report Card](https://goreportcard.com/badge/github.com/erkanrzgc/panely)](https://goreportcard.com/report/github.com/erkanrzgc/panely)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![Security Policy](https://img.shields.io/badge/security-policy-brightgreen.svg)](SECURITY.md)
[![Ko-fi](https://img.shields.io/badge/Ko--fi-support-FF5E5B?logo=ko-fi&logoColor=white)](https://ko-fi.com/erkanrzgc)

[Why](#why-another-panel) · [Architecture](#architecture) · [Threat model](#threat-model) · [Roadmap](#roadmap) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md)

</div>

---

> **Status: pre-release.** Phase 0 (the walking skeleton) is complete and has been
> verified end-to-end on a real server. Phase 1 — the actual deployment loop — is in
> progress. Do not run this in production yet.

---

## Why another panel?

Coolify, Dokploy, CapRover and friends do their job well. Panely exists because of
a single design decision they all share and Panely rejects:

**Every mainstream self-hosted panel gives itself the Docker socket.**

Access to `/var/run/docker.sock` is root access — not "almost root", not "root-ish".
Anyone holding it can start a container with `--privileged`, bind-mount `/` and write
to the host filesystem as uid 0. That means **any remote code execution bug in the
panel is a full host compromise**, including a bug in a template, a webhook parser, or
a transitive npm dependency.

Panely takes the opposite approach. The panel daemon is unprivileged and cannot reach
Docker at all. Everything privileged happens in a separate, deliberately tiny binary
that accepts only typed, schema-whitelisted requests — and that schema is enforced by
CI as a hard budget.

This is not a feature. It is the reason the project exists.

---

## Architecture

```
┌─ WORKSTATION ─────────────────┐        ┌─ SERVER ──────────────────────────────────┐
│                               │        │                                           │
│  Electron GUI                 │        │  panelyd            user: panely          │
│      ↕ stdio JSON-RPC         │        │    • business logic, scheduler, SQLite    │
│  panely (Go CLI / sidecar) ───┼──SSH───┼──► • api.sock (0660, group panely)        │
│                               │        │    • CANNOT reach Docker                  │
└───────────────────────────────┘        │          ↕ exec.sock — typed gRPC         │
                                         │  panely-exec        user: root            │
                                         │    • whitelisted schemas only             │
                                         │    • enforces invariants                  │
                                         │    • Docker + filesystem                  │
                                         └───────────────────────────────────────────┘
```

### Three binaries, three privilege levels

| Binary | Runs as | Privilege | Responsibility |
|---|---|---|---|
| `panelyd` | `panely` | Unprivileged. Not in the `docker` group, empty capability bounding set | Business logic, SQLite, scheduler, health supervisor, audit chain |
| `panely-exec` | `root` | Privileged, but accepts **only** typed schemas | Docker Engine API, constrained filesystem writes |
| `panely-connect` | `panely-client` | None. ~50 lines, forced command | Byte pump between sshd and `api.sock` |
| `panely` | workstation | — | CLI, and sidecar for the Electron GUI |

### The schema *is* the whitelist

Dangerous container options are not validated and rejected. They are **not
representable**. There is no `privileged` field, no `cap_add`, no `host_network`,
no `devices`, no free-form `argv`, and no host path anywhere in the protocol.

A compromised `panelyd` cannot ask for them, because the request cannot be encoded.

```protobuf
// proto/panely/v1/exec.proto — this file is the security boundary.
//
// Before adding a field, the question is:
//   "If panelyd were fully compromised, what would it do with this field?"
```

Enforced invariants, each covered by a test that has been verified to fail when the
protection is removed:

- **No container handle.** RPCs address containers as `(app_id, release_id[, replica])`.
  The executor resolves that against its own `panely.app_id=` labels and touches
  nothing else. A free container ID would be a root-level pointer to *any* container
  on the host.
- **No image reference.** The tag `panely/<app>:<commit_sha>` is *constructed* by the
  executor from validated inputs. Otherwise an arbitrary image could be pulled and run.
- **No host path, ever.** A mount takes an app-scoped *volume name*; the executor
  builds the path. Validating a supplied path is TOCTOU-prone — a symlink can change
  between check and use. Refusing the input deletes the entire class.
- **No free-form argv and no `sh -c`,** anywhere.
- **Caller is authenticated per connection** via `SO_PEERCRED`; socket permissions
  alone are not trusted.
- **Privileged code is size-capped** at 2600 lines, and the cap is measured from the
  *actual import graph* of `cmd/panely-exec` rather than a hand-maintained path list —
  otherwise the budget can be walked around by putting code in a new package and
  importing it. CI fails the build otherwise, because a least-privilege boundary that
  keeps growing stops being one.

### Dual-chain audit log

Both the daemon and the executor keep independent hash-chained, append-only logs:

```
hash = SHA256(canonical(seq, ts, actor, source_ip, ssh_fingerprint,
                        action, target, params, outcome) ‖ prev_hash)
```

The executor's journal is `0640 root:panely` — the daemon can read it but cannot
write to it. If `panelyd` is compromised and drops records from its own chain,
`panely audit verify` still detects the divergence, because privileged actions are
recorded by the privileged side.

Identity is the client's **SSH public-key fingerprint**, transmitted in a connection
preamble written by `panely-connect` before any remote byte is read — not in gRPC
metadata, which the remote client controls and could forge.

---

## Threat model

### No open ports

| Surface | Listens on |
|---|---|
| panelyd API | `/run/panely/api.sock` (unix socket) |
| Executor | `/run/panely-exec/exec.sock` (unix socket) |
| Caddy admin | `/run/caddy/admin.sock` (unix socket) |
| GUI ↔ sidecar | stdio (process pipes) |

The only route to the control plane is **sshd**. There is no port to hide, no
management endpoint to firewall, and no bearer token to leak.

### The client is not root either

`panely bootstrap root@server` is a **one-time** setup command. Day-to-day, the
client never connects as root — if it did, the operator's own shell could run
`docker run --privileged` and the executor split would be decorative.

Bootstrap creates a separate unprivileged SSH user whose key is bound to a forced
command:

```
command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA... panely-client
```

`restrict` disables port, agent and X11 forwarding, PTY allocation, and `~/.ssh/rc`.
The key can only execute `panely-connect`, which does nothing but connect to
`api.sock` and shuttle bytes.

It does **not** disable environment processing — a common and load-bearing
misreading. The audit trail's actor identity comes from `SSH_AUTH_INFO_0`, and an
`environment=` entry in `authorized_keys` would override sshd's own value, letting
a caller forge who did what. That is closed by `PermitUserEnvironment no`, pinned
explicitly in the sshd drop-in rather than left to a distribution default.

> **Design note.** An earlier draft allowed unix-socket forwarding via
> `direct-streamlocal`. The forced command is both simpler and stricter: socket
> forwarding requires the `port-forwarding` permission, which would let the client
> tunnel to **every TCP port on the server**. `restrict` plus a forced command closes
> that class entirely.

### Verified, not asserted

Every security claim in this README corresponds to a test or a measurement, not a
comment. The project has a standing rule: *if a comment about a security property
can be falsified by an experiment, write the experiment.* That rule has caught
**fourteen real bugs** so far, four of them only on the first run against a real
server — including a transport that died on connect, a systemd unit that refused to
start on a fresh host, and an `ssh` argument-injection vector that shell-less exec
did not close.

Findings and their evidence live in [`docs/decisions.md`](docs/decisions.md).

---

## Feature scope

What Panely is being built to do, once Phase 1–4 land:

- **Git-to-URL deploys** — Dockerfile, static, or auto-detected Node
- **Blue-green releases** with a health gate; traffic never moves to an unhealthy release
- **Rollback in seconds** to any retained release
- **Automatic HTTPS** via Caddy with atomic, restart-free config reloads
- **Live log streaming** and metrics, in both CLI and GUI
- **A health supervisor that runs without a client connected** — restarts, backoff, event log
- **Secrets** with envelope encryption, master key via systemd `LoadCredentialEncrypted`
- **Cloudflare orchestration** — DNS, WAF rules, DNS-01 for wildcards
- **TOTP gates on irreversible actions** — volume deletion, restores, key rotation
- **Multi-node** — the same binary in agent mode over mTLS gRPC

Deliberately **not** planned: a web panel. The desktop app is the interface, so there
is no browser-facing attack surface and no session cookie to steal.

---

## Repository layout

```
proto/panely/v1/     Single source of contract (api, exec, agent)
cmd/panelyd/         Server daemon
cmd/panely-exec/     Privileged executor — deliberately small
cmd/panely-connect/  Forced-command stdio proxy (~50 lines)
cmd/panely/          Workstation CLI + `panely sidecar`
internal/            Implementation packages
desktop/             Electron + React
deploy/              systemd units, install assets
docs/                Architecture decision records
scripts/             Surface checks — and the tests for those checks
```

---

## Development

Requirements: Go 1.25+, Node 20+, [`buf`](https://buf.build/docs/installation),
Docker for Linux targets.

```bash
go test -race ./...
go build ./...
buf generate                      # after changing proto/
scripts/check-exec-surface.sh     # privileged-surface invariants
scripts/check-exec-surface-test.sh # ...and proof that the check actually fires
```

### Security verification

Run on every phase. The first three **must fail**:

```bash
sudo -u panely docker ps                    # MUST FAIL — daemon has no Docker
ssh panely-client@server docker ps          # MUST FAIL — client path is unprivileged
ssh panely-client@server                    # MUST NOT give a shell
systemd-analyze security panelyd            # target: exposure < 2.0
go test ./internal/exec                     # every escape attempt must be rejected
```

### Verbose logging

All three binaries take `-debug`, or read `PANELY_DEBUG=1`. The environment
variable exists because the server binaries are started by systemd, where
adding a flag means editing a unit and reloading:

```bash
sudo systemctl set-environment PANELY_DEBUG=1 && sudo systemctl restart panelyd
```

**It is off by default and should stay that way outside of diagnosis.**
`panelyd` and the executor handle container environment variables, request
parameters, and caller identities. At debug level those reach the systemd
journal, where anyone who can read `journalctl` can see them — outside the
boundary [SECURITY.md](SECURITY.md) draws.

Debug level never changes what is written to the audit chain. The two
channels are deliberately separate: the chain records the same entry either
way, and the flag only controls stderr detail. Wiring them together would
let a switch flipped for troubleshooting write secrets into a permanent,
hash-chained log.

---

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **0** | Foundation: proto contract, store, audit chain, executor skeleton, SSH transport, bootstrap, Electron shell, CI | ✅ **done, verified on a real server** |
| **1** | Core deployment loop: Docker driver, build engine, blue-green deploy, Caddy, rollback, live logs, health supervisor | 🔨 in progress — schema and validators landed |
| 2 | Cloudflare (DNS/WAF/DNS-01), secret vault, one-click services, volumes, TOTP | ⏳ |
| 3 | Metrics, alerting, PTY bridge, file manager, editor | ⏳ |
| 4 | Webhook receiver, deploy-on-push, cron manager | ⏳ |
| 5 | R2 backups, Litestream, warm standby, DNS failover | ⏳ |
| 6 | Multi-node: `panelyd --mode=agent`, mTLS gRPC | ⏳ |
| 7 | Octópus integration (local security LLM) | ⏳ |

Known gaps are tracked honestly rather than hidden — see [`docs/decisions.md`](docs/decisions.md).
Notably: arm64 cross-compiles in CI but has **never run on real ARM hardware**, and the
Electron shell has not yet been verified against a live server.

---

## Support this project

Panely is developed in the open by one person. If it is useful to you, or you just
want the privilege-separation model to exist in this space:

<a href="https://ko-fi.com/erkanrzgc">
  <img src="https://img.shields.io/badge/Support%20on%20Ko--fi-FF5E5B?style=for-the-badge&logo=ko-fi&logoColor=white" alt="Support on Ko-fi">
</a>

Starring the repository and reporting real-world findings help just as much.

---

## Contributing

Contributions are welcome — please read [CONTRIBUTING.md](CONTRIBUTING.md) first.
Anything touching `proto/panely/v1/exec.proto` or `internal/exec` is held to a
higher bar: new privileged surface needs a written threat rationale and a test that
has been **observed to fail** when the protection is removed.

Security issues: **do not open a public issue.** See [SECURITY.md](SECURITY.md).

---

## License

[MIT](LICENSE) © [erkanrzgc](https://github.com/erkanrzgc)
