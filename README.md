<div align="center">

# Kadran

**A self-hosted deployment platform whose control panel never runs as root.**

[![CI](https://github.com/erkanrzgc/kadran/actions/workflows/ci.yml/badge.svg)](https://github.com/erkanrzgc/kadran/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/erkanrzgc/kadran.svg)](https://pkg.go.dev/github.com/erkanrzgc/kadran)
[![Go Report Card](https://goreportcard.com/badge/github.com/erkanrzgc/kadran)](https://goreportcard.com/report/github.com/erkanrzgc/kadran)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
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
>
> **Naming.** The project was called Panely until October 2026. v0.4.0 renames the
> server side too: `bootstrap` migrates an existing install in place (K-136).

---

## Why another panel?

Coolify, Dokploy, CapRover and friends do their job well. Kadran exists because of
a single design decision they all share and Kadran rejects:

**Every mainstream self-hosted panel gives itself the Docker socket.**

Access to `/var/run/docker.sock` is root access — not "almost root", not "root-ish".
Anyone holding it can start a container with `--privileged`, bind-mount `/` and write
to the host filesystem as uid 0. That means **any remote code execution bug in the
panel is a full host compromise**, including a bug in a template, a webhook parser, or
a transitive npm dependency.

Kadran takes the opposite approach. The panel daemon is unprivileged and cannot reach
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
| **Automatic HTTPS** | Let's Encrypt certificates via a custom Caddy build that contains no file server. Config reloads are atomic. If the proxy restarts or crashes, the daemon notices within 10 s and restores the routes (measured: sites back after 3 s on a restart, 7 s on a crash; without the watcher they stayed down) |
| **Live logs** | `kadran logs -f <app>`; container logs are capped at 3 × 10 MiB |
| **Health supervisor** | Restarts failed releases with backoff, and keeps running when no client is connected. After a reboot it starts the apps again (measured: sites answer ~19 s after the kernel boots) |
| **Hang detection** | If kadrand's background loops stop making progress or its database pool runs dry, it stops pinging systemd's watchdog (`WatchdogSec=60s`). systemd then restarts it, and a goroutine dump lands in the journal. Measured under real systemd in CI: a frozen daemon was killed and brought back, and a normal run caused no restarts (K-115) |
| **Scaling, env vars, volumes** | `app update -replicas/-env/-volume`. Volumes are mounted `nodev,nosuid` |
| **Pruning** | Removes old releases' containers, always keeping the rollback target |
| **Backups** | Hourly SQLite snapshots with a tested restore path. Optional **encrypted offsite copy** ([`deploy/offsite`](deploy/offsite/README.md)): `age` public-key encryption, so the server cannot decrypt its own past backups |
| **Volume backups (optional)** | A separate unit archives app volumes nightly and encrypts them before anything leaves it. It can read every volume but has no network and no sockets, so the daemon still cannot read app data. Measured: an archive restored from R2, decrypted on another machine, matched the original file for file (owner, mode, checksum) |
| **Alarm detection** | Four failure conditions (heal exhausted, backup failed, proxy not reconciled, low disk), edge-triggered and persisted so that a restart does not fire them again. Shown by `kadran alarms` and in the journal |
| **Alarm delivery (optional)** | A separate unit forwards alarms, offsite-backup failures, and crashes or stops of the core services (kadrand, kadran-exec, kadran-caddy) to Telegram ([`deploy/notify`](deploy/notify/README.md)). The daemon still has no network access and cannot read the bot token. Measured: a real alarm reached Telegram within 34 s, its recovery too; a killed kadrand was reported in 69 s and its recovery a minute later, while the site answered 937 of 937 requests; failed sends are retried, not lost |
| **Heartbeat (optional)** | A Cloudflare Worker on the free tier ([`deploy/nabiz`](deploy/nabiz/README.md)) tells you on Telegram when the alarm sender goes silent for 15 minutes, meaning the sender or the whole server is down. Measured: the alarm came 17 minutes after the last heartbeat, recovery on the next check, one message per change of state |
| **Audit log** | Hash-chained, append-only logs on both sides of the privilege boundary. `kadran audit verify` checks both |

---

## Install

### Requirements

- **Server:** a fresh Linux host with systemd, OpenSSH and Docker Engine, reachable
  over SSH as root or as a user with passwordless sudo (`-sudo`). On Ubuntu, Docker from the distribution is enough:
  `apt-get install -y docker.io`. The installer stops early if Docker is missing. Tested on Ubuntu 24.04 and Debian 13, x86_64. Debian 13 was tested on a GCP e2-micro with 1 GB RAM and 2 GB swap, where a Node build peaked with 237 MiB free (K-121). arm64 binaries are
  built, and CI runs the tests on real ARM hardware, but no server install on
  arm64 has been done yet.
- **Workstation:** Go 1.25+, an OpenSSH client, and a key pair
  (`~/.ssh/id_ed25519.pub` is used by default; `-client-key` picks another).
- Ports 80 and 443 must be free on the server. The installer stops if a `caddy`,
  `nginx`, `apache2`, `httpd` or `lighttpd` service is running.
- **Cloud images that disable root login** (GCP's Debian ships `PermitRootLogin no`):
  use `kadran bootstrap -sudo user@server` with a user that has passwordless sudo.
  Root SSH stays closed; see step 2. Open 80/443 in the provider's firewall; on GCP,
  use a rule with a target tag so that only this machine is exposed.
- **GCP: never add `kadran-client` to instance or project SSH metadata.** The guest
  agent manages the keys of every metadata user and puts them in the `docker` and
  `google-sudoers` groups. `kadran-client` would lose its forced command and gain
  root. Unrelated metadata users, an agent restart and a reboot were measured
  not to touch `kadran-client` (K-121).

### 1. Build

Generated protobuf code is not committed, so a fresh clone does not compile until
`buf generate` has run. The plugins are local on purpose: the schema is the
security boundary and is never uploaded to a remote code generator.

```bash
go install github.com/bufbuild/buf/cmd/buf@v1.47.2      # the version CI pins
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.4
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

git clone https://github.com/erkanrzgc/kadran && cd kadran
buf generate
scripts/build-release.sh          # bin/linux-{amd64,arm64}/… and bin/kadran
```

### 2. Bootstrap the server

```bash
bin/kadran bootstrap root@your-server          # root SSH with a key
bin/kadran bootstrap -sudo you@your-server     # or: passwordless sudo, root SSH stays closed
```

This copies the binaries and systemd units, creates the unprivileged users, installs
your public key for the `kadran-client` user **bound to a forced command**, and
checks its own work, for example by confirming that the daemon's user cannot reach
Docker. It is idempotent and safe to run again, and the same command upgrades an
existing install. After this, you never need root for day-to-day work.

Before uploading anything, the installer checks that it will actually run as root, in
the exact form it will use.
- With `-sudo`, the privilege check and the install itself run under `sudo -n`, and
  sudo never asks for a password. If one is required, the install stops with sudo's
  own message before the upload. The upload and the log follow run as your user.
- The privilege is the same as a root key. What changes is that sshd's policy is left
  alone, root SSH stays closed, and sudo's log records the command.
- Measured on Debian 13 on GCP: an upgrade with `-sudo` passed 17/17 checks, root login
  stayed refused before and after, and sites answered throughout (K-122).

A dropped connection does not restart the install (K-127).
- The package goes to `~/.kadran-upload` on the server, named by its SHA-256. After
  a drop, only the missing part is sent. The server checks the SHA-256 before it
  installs anything.
- The install runs detached from the SSH session and logs to
  `~/.kadran-upload/<sha256>.log`. The CLI follows that log and reconnects if the link
  drops. If the install process dies before it finishes, the CLI says so instead of
  waiting.

The installer bundle is gzip-compressed, about 28 MiB (K-119), so upload speed matters.
To a US server from a home line in Turkey, it took 16–22 minutes. The time limit is
30 minutes; raise it with `-timeout 60m` if needed.

The installer was re-measured on a fresh Ubuntu 24.04 server (Hetzner cx23, x86_64)
on 26–27 September 2026: fresh install, upgrades across commits, a same-build
reinstall, reverse-proxy restart and crash recovery, and a reboot. That run found and
fixed real problems, among them upgrades that left the old binaries running and a
reverse-proxy restart that kept every site down until the daemon restarted
([K-112](docs/decisions.md)). A fresh ARM server was not bootstrapped in that run;
arm64 builds and tests run on real ARM hardware in CI.

Optional hardening: restrict builds to specific repositories with the executor's
`--allow-repo owner/name,…` flag (see the comment at the top of
[`deploy/systemd/kadran-exec.service`](deploy/systemd/kadran-exec.service)).

### 3. Deploy an app

```bash
bin/kadran app create -repo github.com/you/site -domain site.example.com -port 8080 site kadran-client@your-server
bin/kadran deploy site kadran-client@your-server
bin/kadran status kadran-client@your-server
```

Point the domain's DNS at the server before deploying, so that Let's Encrypt can
reach it. Without `-domain` the app is only reachable from the server's internal
network.

`app create` and `app update` check the domain's DNS before they contact the
server (K-128).
- They stop if the domain has no records, or if its A or AAAA records point
  somewhere other than the server.
- They warn and continue when they can't tell, for example when the server sits on
  a private or Tailscale address, or when the DNS query itself fails.
- `.localhost` names are not checked.
- Behind a proxy such as Cloudflare the records show the proxy, not the server.
  Pass `-skip-dns-check` in that case.

If a certificate still doesn't arrive, `kadran domain check site.example.com
kadran-client@your-server` checks each step from your machine and exits non-zero on
a problem. When the DNS points elsewhere, it says that the port and certificate
lines describe that other host.

> Flags go **after the command and before positional arguments** (Go's `flag`
> package stops at the first positional one): `kadran app update -replicas 2 site host`.

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
| `domain check <domain>` | Diagnose a missing certificate: DNS, ports 80/443, the HTTP redirect and the certificate, checked from your machine |
| `audit list\|verify` | Read and verify both audit chains |
| `bootstrap root@server` | One-time server install |
| `key add\|list\|remove root@server` | Manage deploy-only keys for CI (root path, like `bootstrap`) |

Targets: empty means the local socket; `user@host[:port]` or `host` means SSH, with
`kadran-client` as the default user.
Exit codes: `0` success · `1` error · `2` usage · `3` **audit chain broken**.
`alarms` also exits `1` when alarms are active.

---

## Architecture

```
┌─ WORKSTATION ─────────────────┐        ┌─ SERVER ──────────────────────────────────────┐
│                               │        │                                               │
│  Electron GUI (read-only)     │        │  kadrand            user: kadran              │
│      ↕ stdio JSON-RPC         │        │    • business logic, supervisor, SQLite       │
│  kadran (Go CLI / sidecar) ───┼──SSH───┼──► • api.sock (0660, group kadran-client)     │
│                               │        │    • CANNOT reach Docker, no network access   │
└───────────────────────────────┘        │          ↕ exec.sock — typed gRPC             │
                                         │  kadran-exec        user: root                │
                                         │    • whitelisted schemas only                 │
                                         │    • Docker + constrained filesystem writes   │
                                         │                                               │
                                         │  kadran-caddy       user: kadran-caddy        │
                                         │    • :80/:443 for deployed apps               │
                                         │    • configured by kadrand over a unix socket │
                                         └───────────────────────────────────────────────┘
```

| Binary | Runs as | Privilege | Responsibility |
|---|---|---|---|
| `kadrand` | `kadran` | Not in the `docker` group, empty capability set, `IPAddressDeny=any` | Business logic, SQLite, supervisor, alarms, backups, audit chain |
| `kadran-exec` | `root` | Privileged, but accepts **only** typed schemas | Docker Engine API, constrained filesystem writes |
| `kadran-caddy` | `kadran-caddy` | Binds 80/443; no file server compiled in | Reverse proxy and ACME for deployed apps |
| `kadran-connect` | `kadran-client` | None. ~120 lines, forced command | Byte pump between sshd and `api.sock`; writes the caller's key and role first |
| `kadran` | workstation | — | CLI, and sidecar for the Electron GUI |

### The schema *is* the whitelist

Dangerous container options are not validated and rejected. They are **not
representable**. There is no `privileged` field, no `cap_add`, no `host_network`,
no `devices`, no free-form `argv`, and no host path anywhere in the protocol.

A compromised `kadrand` cannot ask for them, because the request cannot be encoded.

```protobuf
// proto/kadran/v1/exec.proto — this file is the security boundary.
//
// Before adding a field, the question is:
//   "If kadrand were fully compromised, what would it do with this field?"
```

Enforced invariants, each covered by a test that has been verified to fail when the
protection is removed:

- **No container handle.** RPCs address containers as `(app_id, release_id[, replica])`.
  The executor resolves that against its own `kadran.app_id=` labels and touches
  nothing else. A free container ID would be a root-level pointer to *any* container
  on the host.
- **No image reference.** The tag `kadran/<app>:<commit_sha>` is *constructed* by the
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
  import graph* of `cmd/kadran-exec` rather than a hand-maintained path list — otherwise
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

`kadran audit verify` walks both chains and exits `3` if either is broken. Environment
values and build arguments are written as `[REDACTED]`.

The executor's journal lives in a root-only directory (`/var/lib/kadran-exec`, `0700`);
the daemon can neither read nor replace it, and asks the executor for it over RPC.
It used to sit in the daemon's own directory, where the daemon could not write the
root-owned file but *could* delete it and put its own chain in its place — a directory
write permission covers unlinking. That was found by measurement and fixed
([K-100, K-102](docs/decisions.md)).

Identity is the client's **SSH public-key fingerprint**, transmitted in a connection
preamble written by `kadran-connect` before any remote byte is read — not in gRPC
metadata, which the remote client controls and could forge. Servers running v0.2.0
or earlier record only the source IP: they read the fingerprint from a variable
OpenSSH never passes to the session, so that field stayed empty. Records written
after the upgrade carry it (K-134).

**What the audit log does not do yet:** the two chains are verified *separately*. No
code compares them, so a compromised `kadrand` that drops its own records produces
two chains that both verify. Closing this needs the executor to return its record
hash to the daemon — see [Known gaps](#known-gaps).

---

## Threat model

### The control plane has no open port

| Surface | Listens on |
|---|---|
| kadrand API | `/run/kadran/api.sock` (unix socket) |
| Executor | `/run/kadran-exec/exec.sock` (unix socket, directory `0750 root:kadran`) |
| Reverse proxy admin | `/run/kadran-caddy/admin.sock` (unix socket) |
| GUI ↔ sidecar | stdio (process pipes) |

On the live server the only listening TCP ports are 22 (sshd) and 80/443 (the reverse
proxy, for deployed apps). The only route to the control plane is **sshd**. There is
no management endpoint to firewall and no bearer token to leak.

### The client is not root either

`kadran bootstrap root@server` is a **one-time** setup command. Day-to-day, the
client never connects as root — if it did, the operator's own shell could run
`docker run --privileged` and the executor split would be decorative.

Bootstrap creates a separate unprivileged SSH user whose key is bound to a forced
command:

```
command="/usr/local/lib/kadran/kadran-connect",restrict ssh-ed25519 AAAA... kadran-client
```

`restrict` disables port, agent and X11 forwarding, PTY allocation, and `~/.ssh/rc`.
The key can only execute `kadran-connect`, which does nothing but connect to
`api.sock` and shuttle bytes.

It does **not** disable environment processing — a common and load-bearing
misreading. The audit trail records which key acted: with `ExposeAuthInfo yes`,
sshd writes the authenticating key to a file and passes its path in
`SSH_USER_AUTH`. An `environment=` entry in `authorized_keys` would override
sshd's own value and point it at a forged file, letting a caller forge who did
what. That is closed by `PermitUserEnvironment no`, pinned explicitly in the sshd
drop-in rather than left to a distribution default.

> **Measured, not assumed.** Until K-134 the code read `SSH_AUTH_INFO_0`, a PAM-internal
> variable OpenSSH deliberately keeps out of the session. Tests set it themselves,
> so they passed; the live server's audit log showed 56 SSH records and not one
> fingerprint. It was found by reading the real server's log, not by a test.

A key for CI can be limited to deploying named apps. The role lives in the same
line as the key, so there is no second list to drift out of sync:

```
command="/usr/local/lib/kadran/kadran-connect -deploy=site,api",restrict ssh-ed25519 AAAA... ci
```

Such a key can call `Ping` and `Deploy` for `site` and `api`, nothing else. It
cannot read app definitions (they carry environment values), so CI passes the
commit explicitly. An empty, unknown, or malformed role is refused at connection
time instead of falling back to admin. What the scope does and does not protect is
in [SECURITY.md](SECURITY.md) (K-131).

Keys are managed over the same root path as `bootstrap`, because only root can
write that file:

```bash
ssh-keygen -t ed25519 -N '' -C ci@github -f ci_deploy
bin/kadran key add -deploy site ci_deploy.pub root@your-server   # or -sudo you@your-server
bin/kadran key list root@your-server
bin/kadran key remove SHA256:... root@your-server
```

`key add` refuses a key that already has a line (sshd uses the first matching line,
so one key on two lines would get whichever role comes first). `key remove` refuses
to delete the last admin key. `key list` exits non-zero if any line is not forced to
`kadran-connect`.

In GitHub Actions, store the private key as a secret and deploy the exact commit
that triggered the run. Trigger it only on pushes to your own branch (and by hand),
never from `pull_request_target`, where secrets sit next to code from forks. This
workflow deploys the author's own site on every push to `main` (K-135):

```yaml
name: Deploy
on:
  push:
    branches: [main]
  workflow_dispatch:

permissions:
  contents: read

concurrency:              # deploys queue up instead of running side by side
  group: deploy-production
  cancel-in-progress: false

jobs:
  deploy:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    env:
      KADRAN_VERSION: v0.3.0   # the release your server runs
    steps:
      - name: Download and verify kadran
        working-directory: ${{ runner.temp }}
        run: |
          base="https://github.com/erkanrzgc/kadran/releases/download/${KADRAN_VERSION}"
          curl -fsSLO "$base/kadran-${KADRAN_VERSION}-linux-amd64"
          curl -fsSLO "$base/SHA256SUMS"
          sha256sum -c --ignore-missing SHA256SUMS   # fails if nothing was verified
          install -m 755 "kadran-${KADRAN_VERSION}-linux-amd64" kadran

      - name: SSH identity
        env:
          KEY: ${{ secrets.KADRAN_DEPLOY_KEY }}
          HOST_KEY: ${{ secrets.KADRAN_HOST_KEY }}   # output of: ssh-keyscan your-server
        run: |
          install -m 700 -d ~/.ssh
          printf '%s\n' "$KEY" > ~/.ssh/id_ed25519 && chmod 600 ~/.ssh/id_ed25519
          printf '%s\n' "$HOST_KEY" > ~/.ssh/known_hosts

      - name: Deploy
        env:
          TARGET: ${{ secrets.KADRAN_TARGET }}       # kadran-client@your-server
        run: |
          "$RUNNER_TEMP/kadran" deploy -commit "$GITHUB_SHA" site "$TARGET"
```

This deploys whatever lands on `main`; it does not run your tests. If you want only
tested commits to go live, run the tests in an earlier job and add `needs:` to
`deploy`. Keeping the target in a secret keeps the server address out of a public
repository. The CLI must come from the same release as your server.
Deploy-only keys need a server upgraded past v0.2.0: an older `kadran-connect` does
not know `-deploy`, refuses to start, and the key simply cannot connect. It fails
closed and never falls back to admin rights.

**Skipping a deploy.** GitHub does not start push-triggered workflows when the
commit message contains `[skip ci]` (or `[ci skip]`, `[no ci]`, `[skip actions]`,
`[actions skip]`). Only the **last** commit of the push counts. If a push carries a
code commit followed by a `[skip ci]` commit, the code is not deployed (measured,
K-129). Use the marker only when the whole push is documentation. Otherwise deploy the
pushed head by hand: Actions → Deploy → Run workflow.

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
- **The last link is unwatched.** The heartbeat Worker reports a dead alarm sender
  or server, but if the Worker itself stops (Cloudflare outage, account problem),
  nobody is told.
- **Hang detection covers what is watched.** kadrand pings systemd's watchdog only
  while its four background loops make progress and its database pool can hand out
  a connection. An RPC handler stuck on something no loop or probe touches still
  goes unnoticed. The thresholds (15 minutes, 3 hours for backups) are derived from
  the code's timeouts. Under normal load (deploys, killed containers), the longest
  gaps measured 7 s for the supervisor and 10 s for the proxy watcher. The hung-executor
  cases that the floor exists for were not measured (K-115).
- **No secret store.** Environment variables are stored in the daemon's database and
  are visible to `docker inspect` on the host. Do not put secrets you cannot rotate in
  them.
- **Volume backups are not snapshots.** The optional volume archiver reads files while
  the app keeps running, so a database's files can come from different moments. Dump
  the database into the volume (`pg_dump`, `sqlite3 .backup`); the dump is consistent.
  Archives are full copies every night, not incremental.
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
| 3 | Metrics, alerting, PTY bridge, file manager, editor | 🔨 alarm detection, Telegram delivery, core-service crash notices and an external heartbeat done |
| 4 | Webhook receiver, deploy-on-push, cron manager | ⏳ |
| 5 | Offsite backups, Litestream, warm standby, DNS failover | 🔨 hourly local + encrypted offsite snapshots and volume backups done |
| 6 | Multi-node: `kadrand --mode=agent`, mTLS gRPC | ⏳ |
| 7 | Octópus integration (local security LLM) | ⏸ on hold (running cost) |

Deliberately **not** planned: a web panel. The management interface stays behind
SSH, so there is no browser-facing attack surface and no session cookie to steal.

---

## Repository layout

```
proto/kadran/v1/     Single source of contract (api, exec)
cmd/kadrand/         Server daemon
cmd/kadran-exec/     Privileged executor — deliberately small
cmd/kadran-connect/  Forced-command stdio proxy
cmd/kadran/          Workstation CLI + `kadran sidecar`
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
| `sudo -u kadran docker ps` | permission denied | ✅ denied (root sees the containers) |
| `kadran` CLI as `root` or as the `kadran` user on the server | connection refused | ✅ reset by peer; `kadran-client` succeeds |
| `ssh kadran-client@server <anything>` | no shell | ✅ only gRPC protocol bytes come back — the forced command ignores the requested command |
| `systemd-analyze security <unit>` | low exposure | kadrand **1.3**, kadran-caddy 1.6, kadran-offsite 1.5, kadran-exec 2.4 |

### Verbose logging

`kadrand` and `kadran-exec` take `-debug`, or read `KADRAN_DEBUG=1`. The environment
variable exists because they are started by systemd, where
adding a flag means editing a unit and reloading:

```bash
sudo systemctl set-environment KADRAN_DEBUG=1 && sudo systemctl restart kadrand
```

**It is off by default and should stay that way outside of diagnosis.**
`kadrand` and the executor handle container environment variables, request
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

Kadran is developed in the open by one person. If it is useful to you, or you just
want the privilege-separation model to exist in this space:

<a href="https://ko-fi.com/erkanrzgc">
  <img src="https://img.shields.io/badge/Support%20on%20Ko--fi-FF5E5B?style=for-the-badge&logo=ko-fi&logoColor=white" alt="Support on Ko-fi">
</a>

Starring the repository and reporting real-world findings help just as much.

---

## Contributing

Contributions are welcome — please read [CONTRIBUTING.md](CONTRIBUTING.md) first.
Anything touching `proto/kadran/v1/exec.proto` or `internal/exec` is held to a
higher bar: new privileged surface needs a written threat rationale and a test that
has been **observed to fail** when the protection is removed.

Security issues: **do not open a public issue.** See [SECURITY.md](SECURITY.md).

---

## License

[Apache License 2.0](LICENSE) © [erkanrzgc](https://github.com/erkanrzgc). See
[NOTICE](NOTICE).

Releases up to and including v0.2.0 were published under the MIT License, and those
releases stay MIT. Later releases are Apache-2.0 (K-132).

Releases after v0.2.0 also carry `THIRD_PARTY_LICENSES.txt`: the license of every module
compiled into the shipped binaries, including the Go standard library and, for
`kadran-caddy`, Caddy.
