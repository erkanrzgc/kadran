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

[Why](#why-another-panel) · [What works](#what-works-today) · [Install](#install) · [Architecture](#architecture) · [Threat model](#threat-model) · [Known gaps](#known-gaps) · [Security](SECURITY.md)

</div>

---

> **Status: pre-release.** The core deployment loop is complete and is serving a real
> site on a live server. It is maintained by one person, the
> CLI's messages are currently **Turkish only**, and several gaps listed under
> [Known gaps](#known-gaps) matter for production use. Read them before you rely on it.

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
Docker at all. Everything privileged happens in a separate, deliberately small binary
that accepts only typed, schema-whitelisted requests — and that binary's size is
enforced by CI as a hard budget.

This is not a feature. It is the reason the project exists.

---

## What works today

Every row below was measured on a live server, not only in tests. The evidence for
each lives in [`docs/decisions.md`](docs/decisions.md).

| Capability | Notes |
|---|---|
| **Git → container deploys** | Builds a commit's `Dockerfile` from a public repository. Only `github.com` is accepted by default; builds can be further restricted to a per-repository allowlist |
| **Blue-green releases with a health gate** | Traffic moves only after the new release answers its HTTP health check |
| **Rollback** | Back to the previous release without rebuilding. Measured: ~18 s end to end, 55 of 55 probes returned `200` during the switch |
| **Automatic HTTPS** | Let's Encrypt certificates via a custom Caddy build that contains no file server. Config reloads are atomic |
| **Live logs** | `panely logs -f <app>`; container logs are capped at 3 × 10 MiB |
| **Health supervisor** | Restarts failed releases with backoff, and keeps running when no client is connected |
| **Scaling, env vars, volumes** | `app update -replicas/-env/-volume`. Volumes are mounted `nodev,nosuid` |
| **Pruning** | Removes old releases' containers, always keeping the rollback target |
| **Backups** | Hourly SQLite snapshots with a tested restore path. Optional **encrypted offsite copy** ([`deploy/offsite`](deploy/offsite/README.md)): `age` public-key encryption, so the server cannot decrypt its own past backups |
| **Alarm detection** | Four failure conditions (heal exhausted, backup failed, proxy not reconciled, low disk), edge-triggered and persisted so that a restart does not fire them again. Shown by `panely alarms` and in the journal |
| **Audit log** | Hash-chained, append-only logs on both sides of the privilege boundary. `panely audit verify` checks both |

---

## Install

### Requirements

- **Server:** a fresh Linux host with systemd, OpenSSH and Docker Engine, reachable
  as root over SSH **once**. Tested on Ubuntu 24.04, x86_64. arm64 binaries are
  built, and CI runs the tests on real ARM hardware, but no server install on
  arm64 has been done yet.
- **Workstation:** Go 1.25+, an OpenSSH client, and a key pair
  (`~/.ssh/id_ed25519.pub` is used by default; `-client-key` picks another).
- Ports 80 and 443 must be free on the server. The installer stops if a `caddy`,
  `nginx`, `apache2`, `httpd` or `lighttpd` service is running.

### 1. Build

Generated protobuf code is not committed, so a fresh clone does not compile until
`buf generate` has run. The plugins are local on purpose: the schema is the
security boundary and is never uploaded to a remote code generator.

```bash
go install github.com/bufbuild/buf/cmd/buf@v1.47.2      # the version CI pins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.4
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

git clone https://github.com/erkanrzgc/panely && cd panely
buf generate
scripts/build-release.sh          # bin/linux-{amd64,arm64}/… and bin/panely
```

### 2. Bootstrap the server (one time, as root)

```bash
bin/panely bootstrap root@your-server
```

This copies the binaries and systemd units, creates the unprivileged users, installs
your public key for the `panely-client` user **bound to a forced command**, and
checks its own work, for example by confirming that the daemon's user cannot reach
Docker. It is idempotent and safe to run again. After this, you never need root for
day-to-day work.

The installer was verified on fresh Ubuntu 24.04 servers in early August 2026. It
has not been re-measured on a fresh server since; the live server has been updated
in place.

Optional hardening: restrict builds to specific repositories with the executor's
`--allow-repo owner/name,…` flag (see the comment at the top of
[`deploy/systemd/panely-exec.service`](deploy/systemd/panely-exec.service)).

### 3. Deploy an app

```bash
bin/panely app create -repo github.com/you/site -domain site.example.com -port 8080 site panely-client@your-server
bin/panely deploy site panely-client@your-server
bin/panely status panely-client@your-server
```

Point the domain's DNS at the server before deploying, so that Let's Encrypt can
reach it. Without `-domain` the app is only reachable from the server's internal
network.

> Flags go **after the command and before positional arguments** (Go's `flag`
> package stops at the first positional one): `panely app update -replicas 2 site host`.

### Commands

| Command | What it does |
|---|---|
| `status` | Server and daemon status |
| `app create\|update\|list\|show\|delete` | Manage app definitions (`delete` only for apps that are not live) |
| `deploy <app>` | Build a commit and switch traffic to it; `-commit <sha>` pins one |
| `rollback <app>` | Switch traffic back to the previous release |
| `logs [-f] <app>` | Stream the live release's output |
| `prune [-dry-run] <app>\|-all` | Remove old releases' containers |
| `alarms` | List active failure conditions |
| `backup create\|list` | Database snapshots |
| `audit list\|verify` | Read and verify both audit chains |
| `bootstrap root@server` | One-time server install |

Targets: empty means the local socket; `user@host[:port]` or `host` means SSH, with
`panely-client` as the default user.
Exit codes: `0` success · `1` error · `2` usage · `3` **audit chain broken**.
`alarms` also exits `1` when alarms are active.

---

## Architecture

```
┌─ WORKSTATION ─────────────────┐        ┌─ SERVER ──────────────────────────────────────┐
│                               │        │                                               │
│  Electron GUI (read-only)     │        │  panelyd            user: panely              │
│      ↕ stdio JSON-RPC         │        │    • business logic, supervisor, SQLite       │
│  panely (Go CLI / sidecar) ───┼──SSH───┼──► • api.sock (0660, group panely-client)     │
│                               │        │    • CANNOT reach Docker, no network access   │
└───────────────────────────────┘        │          ↕ exec.sock — typed gRPC             │
                                         │  panely-exec        user: root                │
                                         │    • whitelisted schemas only                 │
                                         │    • Docker + constrained filesystem writes   │
                                         │                                               │
                                         │  panely-caddy       user: panely-caddy        │
                                         │    • :80/:443 for deployed apps               │
                                         │    • configured by panelyd over a unix socket │
                                         └───────────────────────────────────────────────┘
```

| Binary | Runs as | Privilege | Responsibility |
|---|---|---|---|
| `panelyd` | `panely` | Not in the `docker` group, empty capability set, `IPAddressDeny=any` | Business logic, SQLite, supervisor, alarms, backups, audit chain |
| `panely-exec` | `root` | Privileged, but accepts **only** typed schemas | Docker Engine API, constrained filesystem writes |
| `panely-caddy` | `panely-caddy` | Binds 80/443; no file server compiled in | Reverse proxy and ACME for deployed apps |
| `panely-connect` | `panely-client` | None. ~90 lines, forced command | Byte pump between sshd and `api.sock` |
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
  alone are not trusted. Measured: the socket's own *owner* is refused if it is not
  in the allowed group.
- **No git URL.** `ImageBuild` takes `{host, owner, repo, commit_sha}` and the executor
  *constructs* `https://<host>/<owner>/<repo>.git#<sha>`. Scheme is fixed, there is no
  userinfo field, and the ref must be exactly 40 hex characters — so `ext::sh -c`,
  `ssh://`, embedded credentials, and the `#ref:subdir` traversal of CVE-2026-33748 are
  all unrepresentable, independently of BuildKit's own validation.
- **Every container is confined** — empty capability bounding set, `no-new-privileges`,
  a PID limit, and memory/CPU/IO limits from the app definition.
- **Privileged code is size-capped** at 2500 lines of code, measured from the *actual
  import graph* of `cmd/panely-exec` rather than a hand-maintained path list — otherwise
  the budget is walked around by putting code in a new package and importing it.
  Comments are excluded so the budget never rewards deleting the explanations that make
  the surface auditable. CI fails the build otherwise, because a least-privilege
  boundary that keeps growing stops being one.

### Audit log

Both the daemon and the executor keep independent hash-chained, append-only logs:

```
hash = SHA256(canonical(seq, ts, actor, source_ip, ssh_fingerprint,
                        action, target, params, outcome) ‖ prev_hash)
```

`panely audit verify` walks both chains and exits `3` if either is broken. Environment
values and build arguments are written as `[REDACTED]`.

The executor's journal lives in a root-only directory (`/var/lib/panely-exec`, `0700`);
the daemon can neither read nor replace it, and asks the executor for it over RPC.
It used to sit in the daemon's own directory, where the daemon could not write the
root-owned file but *could* delete it and put its own chain in its place — a directory
write permission covers unlinking. That was found by measurement and fixed
([K-100, K-102](docs/decisions.md)).

Identity is the client's **SSH public-key fingerprint**, transmitted in a connection
preamble written by `panely-connect` before any remote byte is read — not in gRPC
metadata, which the remote client controls and could forge.

**What the audit log does not do yet:** the two chains are verified *separately*. No
code compares them, so a compromised `panelyd` that drops its own records produces
two chains that both verify. Closing this needs the executor to return its record
hash to the daemon — see [Known gaps](#known-gaps).

---

## Threat model

### The control plane has no open port

| Surface | Listens on |
|---|---|
| panelyd API | `/run/panely/api.sock` (unix socket) |
| Executor | `/run/panely-exec/exec.sock` (unix socket, directory `0750 root:panely`) |
| Reverse proxy admin | `/run/panely-caddy/admin.sock` (unix socket) |
| GUI ↔ sidecar | stdio (process pipes) |

On the live server the only listening TCP ports are 22 (sshd) and 80/443 (the reverse
proxy, for deployed apps). The only route to the control plane is **sshd**. There is
no management endpoint to firewall and no bearer token to leak.

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
can be falsified by an experiment, write the experiment.* That rule has caught real
bugs that no unit test could see — a transport that died on connect, a systemd unit
that refused to start on a fresh host, an `ssh` argument-injection vector that
shell-less exec did not close, and mutation tests that reported "caught" while
measuring nothing because the mutant did not compile.

It applies to the project's own records too: decisions have been retracted when a
later measurement contradicted them, and the retraction is kept next to the original
rather than deleted. All of it is in [`docs/decisions.md`](docs/decisions.md).

---

## Known gaps

Tracked in the open rather than hidden. Each one is a real limitation today.

- **Audit chains are not cross-checked** (see [Audit log](#audit-log)).
- **Alarm delivery is new and not yet proven end to end.** A separate unit
  ([`deploy/notify`](deploy/notify/README.md)) forwards panelyd's alarms and
  offsite-backup failures to Telegram; the daemon still has no network access and
  cannot read the bot token. Its sandbox, network path and failure trigger were
  measured on the live server, but no real message has been delivered there yet.
  If the delivery unit itself stops, nothing on a single host notices — that needs
  an external heartbeat.
- **No secret store.** Environment variables are stored in the daemon's database and
  are visible to `docker inspect` on the host. Do not put secrets you cannot rotate in
  them.
- **App volume data is not included in backups** — only Panely's own database is.
- **The desktop app is read-only.** It shows version, status and the audit log. All
  management is done with the CLI.
- **CLI messages are in Turkish.** Commands and flags are English words; the output
  and help text are not yet.
- **Dockerfile builds only**, from public repositories. No buildpacks, no private
  repositories.
- **Single node.** No TOTP on destructive actions, no rate limiting, no Cloudflare
  integration.

---

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **0** | Foundation: proto contract, store, audit chain, executor, SSH transport, bootstrap, CI | ✅ done, verified on a real server |
| **1** | Deployment loop: Docker driver, build engine, blue-green deploy, Caddy, rollback, live logs, health supervisor | ✅ done, verified on a real server |
| — | Operations added along the way: env vars, scaling, pruning, log caps | ✅ done |
| 2 | Cloudflare (DNS/WAF/DNS-01), secret vault, one-click services, volumes, TOTP | 🔨 volumes done |
| 3 | Metrics, alerting, PTY bridge, file manager, editor | 🔨 alarm detection done, Telegram delivery built |
| 4 | Webhook receiver, deploy-on-push, cron manager | ⏳ |
| 5 | Offsite backups, Litestream, warm standby, DNS failover | 🔨 hourly local + encrypted offsite snapshots done |
| 6 | Multi-node: `panelyd --mode=agent`, mTLS gRPC | ⏳ |
| 7 | Octópus integration (local security LLM) | ⏳ |

Deliberately **not** planned: a web panel. The management interface stays behind
SSH, so there is no browser-facing attack surface and no session cookie to steal.

---

## Repository layout

```
proto/panely/v1/     Single source of contract (api, exec)
cmd/panelyd/         Server daemon
cmd/panely-exec/     Privileged executor — deliberately small
cmd/panely-connect/  Forced-command stdio proxy
cmd/panely/          Workstation CLI + `panely sidecar`
build/caddy/         Custom Caddy build (no file server)
internal/            Implementation packages
desktop/             Electron + React
deploy/              systemd units, offsite backup
docs/                Architecture decision records
scripts/             Surface checks, mutation tests — and the tests for those checks
```

---

## Development

Requirements: Go 1.25+, Node 20+ (desktop only), [`buf`](https://buf.build/docs/installation),
Docker for integration tests.

```bash
go test -race ./...
go vet ./...
buf lint && buf generate            # after changing proto/
scripts/check-exec-surface.sh       # privileged-surface budget and invariants
scripts/check-exec-surface-test.sh  # ...and proof that the check actually fires
for s in scripts/mutate-*.sh; do bash "$s"; done   # mutation tests (also run in CI)
```

The mutation scripts break a protection on purpose and require at least one test to
fail. Each mutant must compile first; a mutant that does not compile is reported as
*not measured*, never as *caught*.

### Security verification

Run against a live server. Measured results from the current deployment:

| Check | Expected | Measured |
|---|---|---|
| `sudo -u panely docker ps` | permission denied | ✅ denied (root sees the containers) |
| `panely` CLI as `root` or as `panely` on the server | connection refused | ✅ reset by peer; `panely-client` succeeds |
| `ssh panely-client@server <anything>` | no shell | ✅ only gRPC protocol bytes come back — the forced command ignores the requested command |
| `systemd-analyze security <unit>` | low exposure | panelyd **1.3**, panely-caddy 1.6, panely-offsite 1.5, panely-exec 2.4 |

### Verbose logging

`panelyd` and `panely-exec` take `-debug`, or read `PANELY_DEBUG=1`. The environment
variable exists because they are started by systemd, where
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
