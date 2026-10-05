<div align="center">

# KΛDRΛN

### Self-hosted Git deployments, with a control panel that never runs as root.

[![CI](https://github.com/erkanrzgc/kadran/actions/workflows/ci.yml/badge.svg)](https://github.com/erkanrzgc/kadran/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/erkanrzgc/kadran?color=F59E0B)](https://github.com/erkanrzgc/kadran/releases)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache_2.0-0E1116.svg)](LICENSE)
[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)

[Quick start](#quick-start) · [Why](#why-kadran) · [Features](#what-it-does) · [Security model](#security-model) · [Upgrading](#upgrading-from-v030) · [Known gaps](#known-gaps)

</div>

---

> [!NOTE]
> **Pre-release.** The core deployment loop is complete and serves a real site on a live
> server. Kadran is maintained by one person, some server-side messages are still in
> Turkish, and the [known gaps](#known-gaps) matter for production use. Read them first.
>
> The project was called Panely until October 2026. Since v0.4.0 every name on the server
> is `kadran` too, and `bootstrap` migrates an existing install in place
> ([Upgrading](#upgrading-from-v030)).

## Why Kadran

Coolify, Dokploy, CapRover and friends do their job well. Kadran exists because of one
design decision they all share and Kadran rejects:

**Every mainstream self-hosted panel gives itself the Docker socket.**

Access to `/var/run/docker.sock` is root access, not "almost root". Anyone holding it can
start a `--privileged` container, bind-mount `/` and write to the host as uid 0. So **any
remote code execution bug in the panel is a full host compromise**, including one in a
template, a webhook parser or a transitive npm dependency.

Kadran takes the opposite approach. The panel daemon is unprivileged and cannot reach
Docker at all. Everything privileged happens in a separate, deliberately small binary that
accepts only typed, schema-whitelisted requests. CI enforces that binary's size as a hard
budget.

This is not a feature. It is the reason the project exists.

<table>
<tr>
<td width="25%" valign="top">

**Never root**

The daemon runs as its own user, outside the `docker` group, with no capabilities and no
network access.

</td>
<td width="25%" valign="top">

**The schema is the whitelist**

There is no `privileged` field, no host path, no free-form argv. A compromised daemon
cannot even encode the request.

</td>
<td width="25%" valign="top">

**No open control port**

The control plane listens on unix sockets only. The way in is sshd and a key bound to a
forced command.

</td>
<td width="25%" valign="top">

**Measured, not asserted**

Every claim here has a test or a measurement on a real server, recorded in
[`docs/decisions.md`](docs/decisions.md).

</td>
</tr>
</table>

## Quick start

```bash
# 1. Build (the protobuf code is generated locally, never uploaded)
git clone https://github.com/erkanrzgc/kadran && cd kadran
buf generate && scripts/build-release.sh

# 2. Install on a server: once, as root or with passwordless sudo
bin/kadran bootstrap root@your-server

# 3. Deploy: day to day, as the unprivileged kadran-client user
bin/kadran app create -repo github.com/you/site -domain site.example.com -port 8080 site kadran-client@your-server
bin/kadran deploy site kadran-client@your-server
```

Point the domain's DNS at the server first, so that Let's Encrypt can reach it. The
[install guide](#install) covers requirements, the `-sudo` mode and what `bootstrap`
checks on the way.

## What it does

Every row was measured on a live server, not only in tests. The evidence for each is in
[`docs/decisions.md`](docs/decisions.md).

| Capability | Notes |
|---|---|
| **Git → container deploys** | Builds a commit's `Dockerfile` from a public repository. Only `github.com` by default; builds can be limited to a per-repository allowlist |
| **Blue-green releases** | Traffic moves only after the new release answers its HTTP health check |
| **Rollback** | Back to the previous release without rebuilding: ~18 s end to end, 55 of 55 probes returned `200` during the switch |
| **Automatic HTTPS** | Let's Encrypt through a custom Caddy build with no file server compiled in. Atomic config reloads. A restarted or crashed proxy gets its routes back within 10 s (measured: 3 s after a restart, 7 s after a crash) |
| **Live logs** | `kadran logs -f <app>`; container logs are capped at 3 × 10 MiB |
| **Health supervisor** | Restarts failed releases with backoff and works without a connected client. After a reboot the apps come back on their own (sites answer ~19 s after the kernel boots) |
| **Hang detection** | If the daemon's loops stall or its database pool runs dry, it stops pinging systemd's watchdog (`WatchdogSec=60s`); systemd restarts it and a goroutine dump lands in the journal. Measured under real systemd in CI (K-115) |
| **Scaling, env vars, volumes** | `app update -replicas/-env/-volume`. Volumes are mounted `nodev,nosuid` |
| **Pruning** | Removes old releases' containers and always keeps the rollback target |
| **Backups** | Hourly SQLite snapshots with a tested restore path. Optional [encrypted offsite copy](deploy/offsite/README.md) with `age` public-key encryption: the server cannot decrypt its own past backups |
| **Volume backups** *(optional)* | Nightly, encrypted before anything leaves the unit. It can read every volume but has no network and no sockets. An archive restored from R2 on another machine matched the original file for file (owner, mode, checksum) |
| **Alarms** | Heal exhausted, backup failed, proxy not reconciled, low disk. Edge-triggered and persisted, so a restart does not fire them again. `kadran alarms` and the journal |
| **Alarm delivery** *(optional)* | Alarms, offsite failures and core-service crashes go to [Telegram](deploy/notify/README.md). The daemon still has no network and cannot read the bot token. A real alarm arrived within 34 s; a killed daemon was reported in 69 s while the site answered 937 of 937 requests |
| **Heartbeat** *(optional)* | A free-tier Cloudflare Worker ([`deploy/nabiz`](deploy/nabiz/README.md)) tells you when the alarm sender goes silent for 15 minutes, meaning it or the whole server is down |
| **Audit log** | Hash-chained, append-only logs on both sides of the privilege boundary. `kadran audit verify` checks both |
| **CI deploys** | Deploy-only SSH keys scoped to named apps; a GitHub Actions workflow that deploys on every push ([below](#continuous-deployment)) |

## Install

<details>
<summary><b>Requirements</b></summary>

- **Server:** Linux with systemd, OpenSSH and Docker Engine, reachable over SSH as root
  or as a user with passwordless sudo. On Ubuntu the distribution's Docker is enough:
  `apt-get install -y docker.io`. The installer stops early if Docker is missing.
- **Tested on** Ubuntu 24.04 (Hetzner cx23) and Debian 13 (GCP e2-micro with 1 GB RAM and
  2 GB swap, where a Node build peaked with 237 MiB free, K-121), both x86_64. arm64
  binaries are built and CI runs the tests on real ARM hardware, but no arm64 server has
  been installed yet.
- **Workstation:** Go 1.25+, an OpenSSH client and a key pair. `~/.ssh/id_ed25519.pub` is
  used by default; `-client-key` picks another.
- **Ports 80 and 443** must be free. The installer stops if `caddy`, `nginx`, `apache2`,
  `httpd` or `lighttpd` is running.
- **Cloud images that disable root login** (GCP's Debian ships `PermitRootLogin no`): use
  `bootstrap -sudo user@server`. Root SSH stays closed. Open 80/443 in the provider's
  firewall; on GCP, use a rule with a target tag so only this machine is exposed.
- **GCP: never add `kadran-client` to instance or project SSH metadata.** The guest agent
  manages the keys of every metadata user and puts them in the `docker` and
  `google-sudoers` groups: `kadran-client` would lose its forced command and gain root.
  Unrelated metadata users, an agent restart and a reboot were measured not to touch it
  (K-121).

</details>

### 1. Build

Generated protobuf code is not committed. The plugins are local on purpose: the schema is
the security boundary and is never uploaded to a remote code generator.

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
bin/kadran bootstrap -sudo you@your-server     # or passwordless sudo; root SSH stays closed
```

`bootstrap` copies the binaries and systemd units, creates the unprivileged users, installs
your public key for `kadran-client` **bound to a forced command**, and then checks its own
work, for example that the daemon's user cannot reach Docker. It is idempotent, and the
same command upgrades an existing install. After this you never need root day to day.

<details>
<summary><b>What bootstrap guarantees along the way</b></summary>

- **Privilege is checked before anything is uploaded,** in the exact form the install will
  use. With `-sudo`, both the check and the install run under `sudo -n` and never ask for
  a password; the upload and the log run as your user. The privilege equals a root key,
  but sshd's policy is left alone and sudo's log records the command. On Debian 13 an
  upgrade with `-sudo` passed 17/17 checks, root login stayed refused and sites answered
  throughout (K-122).
- **A dropped connection does not restart the install** (K-127). The package lands in
  `~/.kadran-upload`, named by its SHA-256; after a drop only the missing part is sent, and
  the server verifies the hash before installing. The install runs detached and logs to
  `~/.kadran-upload/<sha256>.log`; the CLI follows the log and reconnects.
- **The bundle is about 28 MiB** gzip-compressed (K-119). From a home line in Turkey to a US
  server it took 16–22 minutes. The limit is 30 minutes; raise it with `-timeout 60m`.
- **It has been re-measured on fresh servers:** fresh install, upgrades across commits, a
  same-build reinstall, proxy restart and crash recovery, and a reboot. That run found
  and fixed real problems, among them upgrades that left old binaries running
  ([K-112](docs/decisions.md)).

</details>

Optional hardening: limit builds to specific repositories with the executor's
`--allow-repo owner/name,…` flag (see the top of
[`deploy/systemd/kadran-exec.service`](deploy/systemd/kadran-exec.service)).

### 3. Deploy an app

```bash
bin/kadran app create -repo github.com/you/site -domain site.example.com -port 8080 site kadran-client@your-server
bin/kadran deploy site kadran-client@your-server
bin/kadran status kadran-client@your-server
```

Without `-domain` an app is reachable only from the server's internal network.
`app create` and `app update` check the domain's DNS before they contact the server
(K-128): they stop if the A/AAAA records point elsewhere or don't exist, and warn when they
can't tell (a private or Tailscale address, a failed query). Behind a proxy such as
Cloudflare, pass `-skip-dns-check`. If a certificate still doesn't arrive,
`kadran domain check site.example.com kadran-client@your-server` checks DNS, ports 80/443,
the redirect and the certificate from your machine.

> [!TIP]
> Flags go **after the command and before positional arguments** (Go's `flag` package
> stops at the first positional one): `kadran app update -replicas 2 site host`.

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
| `domain check <domain>` | Diagnose a missing certificate from your machine |
| `audit list\|verify` | Read and verify both audit chains |
| `bootstrap root@server` | Install or upgrade the server |
| `key add\|list\|remove root@server` | Manage deploy-only keys for CI |

Targets: empty means the local socket; `user@host[:port]` or `host` means SSH, with
`kadran-client` as the default user. Exit codes: `0` success · `1` error · `2` usage ·
`3` **audit chain broken**. `alarms` also exits `1` while alarms are active.

## Upgrading from v0.3.0

v0.4.0 renames everything on the server from `panely` to `kadran`: services, users,
directories, the database file, Docker labels and the wire protocol (`kadran.v1`).
Run `bootstrap` with v0.4.0 as usual; it detects the old install and **migrates it in
place** (K-136):

- Users are renamed with `usermod`, so uid/gid and every file's ownership stay the same.
  The database, backups, volumes, keys, TLS certificates, audit chains and the optional
  backup and alarm units move to the new names; a timer that was off stays off.
- Your own systemd drop-ins (for example an `--allow-repo` list) follow the units, and the
  migration stops before starting the new executor if its effective allowlist differs
  from the old one.
- The old proxy and containers keep serving until the new control plane has started its
  own containers. The site is down only while the proxy switches: **about 0.6 s** on the
  live server (Ubuntu 24.04, three routed apps, read from the journal; 188 of 189 probe
  requests succeeded) and about 1.3 s in the rehearsals on Debian 13.
- During that overlap the old and new replicas of an app mount **the same volume**, as in
  any blue-green deploy, only for longer. Stop apps that keep a single-writer database
  in a volume before migrating.
- If new containers do not come up, the migration stops and the site keeps running on
  the old stack. Rerunning `bootstrap` continues where it stopped.
- A rollback script is installed before anything moves:
  `/usr/local/lib/kadran/kadran-geri-donus.sh`. It restores the old names; then run
  `bootstrap` from v0.3.0. Unlike the migration, rollback takes the site down until the
  old stack is back (~75 s in the rehearsal).

> [!IMPORTANT]
> - **CLI and server must match.** v0.3.0 and v0.4.0 cannot talk to each other (protocol 3).
>   The new CLI recognizes an old server and tells you to run `bootstrap`; a
>   `panely-client@` target is answered with the new `kadran-client@` form.
> - **CI:** change the workflow's CLI version and the `kadran-client@` target together
>   with the server, and don't push during the migration.
> - **Offsite backups:** new objects are named `kadran-…`. Add the bucket lock and
>   lifecycle rules for that prefix before migrating ([offsite guide](deploy/offsite/README.md)).
> - **Desktop app:** saved profiles move to the new app directory on first start, and their
>   targets become `kadran-client@…`.

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
| `kadrand` | `kadran` | Not in `docker`, empty capability set, `IPAddressDeny=any` | Business logic, SQLite, supervisor, alarms, backups, audit chain |
| `kadran-exec` | `root` | Privileged, but accepts **only** typed schemas | Docker Engine API, constrained filesystem writes |
| `kadran-caddy` | `kadran-caddy` | Binds 80/443; no file server compiled in | Reverse proxy and ACME |
| `kadran-connect` | `kadran-client` | None; ~120 lines, forced command | Byte pump between sshd and `api.sock`; writes the caller's key and role first |
| `kadran` | workstation | — | CLI, and sidecar for the Electron GUI |

## Security model

### The schema *is* the whitelist

Dangerous container options are not validated and rejected. They are **not representable**:
there is no `privileged` field, no `cap_add`, no `host_network`, no `devices`, no free-form
`argv` and no host path anywhere in the protocol. A compromised `kadrand` cannot ask for
them, because the request cannot be encoded.

```protobuf
// proto/kadran/v1/exec.proto — this file is the security boundary.
//
// Before adding a field, the question is:
//   "If kadrand were fully compromised, what would it do with this field?"
```

Each invariant below is covered by a test that has been seen to fail when the protection is
removed:

- **No container handle.** Containers are addressed as `(app_id, release_id[, replica])`,
  resolved against the executor's own `kadran.app_id=` labels. A free container ID would be
  a root-level pointer to *any* container on the host.
- **No image reference.** The tag `kadran/<app>:<commit_sha>` is *constructed* by the
  executor from validated inputs.
- **No host path, ever.** A mount takes an app-scoped *volume name* and the executor builds
  the path. Validating a supplied path is TOCTOU-prone; refusing the input deletes the class.
- **No free-form argv and no `sh -c`,** anywhere.
- **The caller is authenticated per connection** via `SO_PEERCRED`, not by socket
  permissions alone. Measured: the socket's own *owner* is refused if not in the allowed
  group.
- **No git URL.** `ImageBuild` takes `{host, owner, repo, commit_sha}` and builds
  `https://<host>/<owner>/<repo>.git#<sha>` itself: fixed scheme, no userinfo, a 40-hex ref.
  `ext::sh -c`, `ssh://`, embedded credentials and the `#ref:subdir` traversal of
  CVE-2026-33748 are unrepresentable, independently of BuildKit's own validation.
- **Every container is confined:** empty capability bounding set, `no-new-privileges`, a PID
  limit and memory/CPU/IO limits from the app definition.
- **Privileged code is size-capped** at 2600 lines (raised from 2500 for the vault, K-123), measured from the *actual import graph*
  of `cmd/kadran-exec` (not a hand-kept path list a new package could walk around), comments
  excluded. CI fails the build otherwise: a least-privilege boundary that keeps growing
  stops being one.

### The control plane has no open port

| Surface | Listens on |
|---|---|
| kadrand API | `/run/kadran/api.sock` (unix socket) |
| Executor | `/run/kadran-exec/exec.sock` (unix socket, directory `0750 root:kadran`) |
| Proxy admin | `/run/kadran-caddy/admin.sock` (unix socket) |
| GUI ↔ sidecar | stdio (process pipes) |

On the live server the only listening TCP ports are 22 (sshd) and 80/443 (the proxy, for
deployed apps). There is no management endpoint to firewall and no bearer token to leak.

### The client is not root either

`bootstrap` is the only command that needs root. Day to day, the client connects as a
separate unprivileged user whose key is bound to a forced command:

```
command="/usr/local/lib/kadran/kadran-connect",restrict ssh-ed25519 AAAA... you@laptop
```

`restrict` disables port, agent and X11 forwarding, PTY allocation and `~/.ssh/rc`; the key
can only run `kadran-connect`, which connects to `api.sock` and shuttles bytes.

`restrict` does **not** disable environment processing, a common and load-bearing
misreading. The audit trail records which key acted: with `ExposeAuthInfo yes`, sshd writes
the authenticating key to a file and passes its path in `SSH_USER_AUTH`. An `environment=`
entry in `authorized_keys` could point that at a forged file. `PermitUserEnvironment no`
closes it, pinned in Kadran's sshd drop-in rather than left to a distribution default.

> [!NOTE]
> **Measured, not assumed.** Until K-134 the code read `SSH_AUTH_INFO_0`, a PAM-internal
> variable OpenSSH keeps out of the session. Tests set it themselves and passed; the live
> server's audit log showed 56 SSH records and not one fingerprint. It was found by
> reading the real server's log.

<details>
<summary><b>Design note: why a forced command and not socket forwarding</b></summary>

An earlier draft allowed unix-socket forwarding via `direct-streamlocal`. The forced
command is simpler and stricter: socket forwarding needs the `port-forwarding` permission,
which would let the client tunnel to **every TCP port on the server**. `restrict` plus a
forced command closes that class entirely.

</details>

### Secrets are sealed with the executor's key

Environment values are sealed with [age](https://age-encryption.org) to the executor's
public key before they reach the database (v0.5.0, [K-123](docs/decisions.md)). The daemon
writes them but cannot read them; the executor opens them only while it creates a
container. A leaked disk, database or backup taken on v0.5.0 gives away no values, and a
compromised daemon cannot read past ones, **once the copies made before the upgrade are
gone**: hourly backups rotate out within a day, but the pre-migration copies
`/var/lib/kadran/kadran.db.pre-*` (the upgrade writes one more) stay until you delete
them, and the daemon can read them.

- Each value is bound to its app and variable name. The executor refuses a sealed value
  moved to another variable or app, so a compromised daemon cannot get one app's password
  opened under a name another app logs. Restrict builds with `--allow-repo` too: a daemon
  that can build an app from any repository could ship an image that prints its own
  environment.
- `bootstrap` creates the key once, as `/var/lib/kadran-exec/vault.key` (root `0600`), and
  reminds you to save it. **Keep a copy in a password manager**, not next to the offsite
  backup key: without it the values cannot be recovered. `sudo cat` that file to copy it.
  Upgrading a server that already has values: create and save the key **before**
  `bootstrap` seals them ([v0.5.0 notes](CHANGELOG.md)).
- `kadran app show --json` returns variable names only.
- To go back to v0.4.x, stop `kadrand` and run `/usr/local/lib/kadran/kadran-kasa-coz.sh`
  first; it writes the values back as plaintext using the key and `age`.

### Audit log

Both the daemon and the executor keep independent hash-chained, append-only logs:

```
hash = SHA256(canonical(seq, ts, actor, source_ip, ssh_fingerprint,
                        action, target, params, outcome) ‖ prev_hash)
```

`kadran audit verify` walks both chains and exits `3` if either is broken. Environment values
and build arguments are written as `[REDACTED]`. The executor's journal lives in a root-only
directory (`/var/lib/kadran-exec`, `0700`) that the daemon can neither read nor replace; it
used to sit in the daemon's directory, where the daemon could delete it and put its own
chain in its place. That was found by measurement and fixed ([K-100, K-102](docs/decisions.md)).

Identity is the client's **SSH key fingerprint**, written by `kadran-connect` in a connection
preamble before any remote byte is read, not taken from gRPC metadata the client controls.
Servers before v0.3.0 recorded only the source IP (K-134).

**Anchors (v0.4.1, K-126).** A compromised `kadrand` could rewrite its own chain into one
that still verifies. With offsite backups on, every daemon backup now carries an *anchor*:
the chain's head (`seq` and hash), uploaded unencrypted under the bucket-locked `kadran-`
prefix. Check the live chain against them from your own machine:

```bash
rclone copy kadran-offsite:<bucket> ./anchors --include 'kadran-*.capa'
kadran audit verify -anchors ./anchors kadran-client@your-server
```

The CLI recomputes the daemon chain itself and ignores the hashes the server sends. Any
anchor that disagrees, including a chain shorter than an anchor, exits `3`. After restoring
the database from an older backup, older anchors conflict by design; pass
`-anchors-since <restore time>`. Limits: it covers the daemon chain only, back to the lock
period (30 days) and up to the newest anchor. The daemon can read the upload token, so it
can add fake anchors, but it cannot change or delete locked ones.

### Verified, not asserted

The project has a standing rule: *if a comment about a security property can be falsified
by an experiment, write the experiment.* It has caught bugs no unit test could see: a
transport that died on connect, a unit that refused to start on a fresh host, an `ssh`
argument-injection vector that shell-less exec did not close, and mutation tests that
reported "caught" while measuring nothing because the mutant did not compile. Decisions are
retracted when a later measurement contradicts them, and the retraction stays next to the
original. All of it is in [`docs/decisions.md`](docs/decisions.md).

## Continuous deployment

A CI key can be limited to deploying named apps. The role sits in the same line as the key,
so there is no second list to drift:

```
command="/usr/local/lib/kadran/kadran-connect -deploy=site,api",restrict ssh-ed25519 AAAA... ci
```

Such a key can call `Ping` and `Deploy` for `site` and `api`, nothing else. It cannot read app
definitions (they carry environment values), so CI passes the commit explicitly. An empty,
unknown or malformed role is refused at connection time instead of falling back to admin.
What the scope does and does not protect is in [SECURITY.md](SECURITY.md) (K-131).

```bash
ssh-keygen -t ed25519 -N '' -C ci@github -f ci_deploy
bin/kadran key add -deploy site ci_deploy.pub root@your-server   # or -sudo you@your-server
bin/kadran key list root@your-server
bin/kadran key remove SHA256:... root@your-server
```

`key add` refuses a key that already has a line (sshd uses the first matching line, so one
key on two lines would get whichever role comes first). `key remove` refuses to delete the
last admin key. `key list` exits non-zero if any line is not forced to `kadran-connect`.

In GitHub Actions, store the private key as a secret and deploy the exact commit that
triggered the run. Trigger it only on pushes to your own branch and by hand, never from
`pull_request_target`, where secrets sit next to code from forks. This workflow deploys the
author's own site on every push to `main` (K-135):

<details>
<summary><b><code>.github/workflows/deploy.yml</code></b></summary>

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
      KADRAN_VERSION: v0.5.1   # the release your server runs
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

</details>

This deploys whatever lands on `main`; it does not run your tests. To ship only tested
commits, run the tests in an earlier job and add `needs:` to `deploy`. Keeping the target in
a secret keeps the server address out of a public repository. The CLI must come from the
same release as the server.

> [!WARNING]
> **Skipping a deploy.** GitHub does not start push-triggered workflows when the commit
> message contains `[skip ci]` (or `[ci skip]`, `[no ci]`, `[skip actions]`,
> `[actions skip]`), and only the **last** commit of the push counts. A code commit followed
> by a `[skip ci]` commit in the same push is not deployed (measured, K-129). Use the marker
> only when the whole push is documentation; otherwise run the workflow by hand
> (Actions → Deploy → Run workflow).

## Known gaps

Tracked in the open rather than hidden. Each is a real limitation today.

- **Only the daemon chain is anchored, and only with offsite backups** (see
  [Audit log](#audit-log)). The executor's chain is verified on its own.
- **The last link is unwatched.** The heartbeat Worker reports a dead alarm sender or
  server, but if the Worker itself stops (Cloudflare outage, account problem), nobody is
  told.
- **Hang detection covers what is watched.** The daemon pings systemd's watchdog only while
  its four loops make progress and its database pool can hand out a connection. An RPC
  handler stuck on something no loop touches goes unnoticed. The hung-executor cases the
  thresholds exist for were not measured (K-115).
- **Secrets are sealed at rest, not in the container.** Environment values are sealed
  with the executor's key in the database and its backups (K-123), but a running
  container still gets them as environment variables: `docker inspect` on the host and
  every process in the container can read them. Backups from before v0.5.0 hold them in
  plaintext.
- **Volume backups are not snapshots.** Files are read while the app runs, so a database's
  files can come from different moments. Dump the database into the volume (`pg_dump`,
  `sqlite3 .backup`). Archives are full copies every night.
- **Replicas share a volume.** Every replica, and both releases during a blue-green deploy,
  mount the same directory. Apps that keep a single-writer database in a volume should run
  one replica and accept that a deploy overlaps two writers for a few seconds.
- **The desktop app is read-only:** version, status and the audit log. Management is CLI only.
- **Some messages are still in Turkish.** The CLI's own output, help and errors are
  English. The install output, errors the server sends back, the desktop app and alarm
  texts are being translated.
- **Dockerfile builds only**, from public repositories. No buildpacks, no private
  repositories.
- **Single node.** No TOTP on destructive actions, no rate limiting, no Cloudflare
  integration.

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **0** | Foundation: proto contract, store, audit chain, executor, SSH transport, bootstrap, CI | ✅ done, verified on a real server |
| **1** | Deployment loop: Docker driver, build engine, blue-green deploy, Caddy, rollback, live logs, health supervisor | ✅ done, verified on a real server |
| — | Operations: env vars, scaling, pruning, log caps | ✅ done |
| 2 | Cloudflare (DNS/WAF/DNS-01), secret vault, one-click services, volumes, TOTP | 🔨 volumes and the secret vault done |
| 3 | Metrics, alerting, PTY bridge, file manager, editor | 🔨 alarms, Telegram delivery, crash notices and an external heartbeat done |
| 4 | Webhook receiver, deploy-on-push, cron manager | 🔨 deploy-on-push through CI with deploy-only keys done |
| 5 | Offsite backups, Litestream, warm standby, DNS failover | 🔨 hourly local + encrypted offsite snapshots and volume backups done |
| 6 | Multi-node: `kadrand --mode=agent`, mTLS gRPC | ⏳ |
| 7 | Octópus integration (local security LLM) | ⏸ on hold (running cost) |

Deliberately **not** planned: a web panel. Management stays behind SSH, so there is no
browser-facing attack surface and no session cookie to steal.

## Development

```
proto/kadran/v1/     Single source of contract (api, exec)
cmd/kadrand/         Server daemon
cmd/kadran-exec/     Privileged executor — deliberately small
cmd/kadran-connect/  Forced-command stdio proxy
cmd/kadran/          Workstation CLI + `kadran sidecar`
build/caddy/         Custom Caddy build (no file server)
internal/            Implementation packages
desktop/             Electron + React
deploy/              systemd units, offsite backup, alarm delivery, heartbeat
docs/                Architecture decision records
scripts/             Surface checks, mutation tests — and the tests for those checks
```

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

The mutation scripts break a protection on purpose and require at least one test to fail.
Each mutant must compile first; one that does not is reported as *not measured*, never as
*caught*.

<details>
<summary><b>Security checks against a live server</b></summary>

| Check | Expected | Measured |
|---|---|---|
| `sudo -u kadran docker ps` | permission denied | ✅ denied (root sees the containers) |
| `kadran` CLI as `root` or as the `kadran` user on the server | connection refused | ✅ reset by peer; `kadran-client` succeeds |
| `ssh kadran-client@server <anything>` | no shell | ✅ only gRPC protocol bytes come back — the forced command ignores the requested command |
| `systemd-analyze security <unit>` | low exposure | kadrand **1.3**, kadran-caddy 1.6, kadran-offsite 1.5, kadran-exec 2.4 |

</details>

<details>
<summary><b>Verbose logging</b></summary>

`kadrand` and `kadran-exec` take `-debug`, or read `KADRAN_DEBUG=1` (they are started by
systemd, where adding a flag means editing a unit):

```bash
sudo systemctl set-environment KADRAN_DEBUG=1 && sudo systemctl restart kadrand
```

**Keep it off outside of diagnosis.** At debug level, container environment variables,
request parameters and caller identities reach the systemd journal, where anyone who can
read `journalctl` can see them, outside the boundary [SECURITY.md](SECURITY.md) draws. Debug
level never changes what is written to the audit chain: wiring the two together would let
a switch flipped for troubleshooting write secrets into a permanent, hash-chained log.

</details>

## Contributing

Contributions are welcome; please read [CONTRIBUTING.md](CONTRIBUTING.md) first. Anything
touching `proto/kadran/v1/exec.proto` or `internal/exec` is held to a higher bar: new
privileged surface needs a written threat rationale and a test that has been **observed to
fail** when the protection is removed.

Security issues: **do not open a public issue.** See [SECURITY.md](SECURITY.md).

If Kadran is useful to you, starring the repository and reporting real-world findings help a
lot. You can also [support it on Ko-fi](https://ko-fi.com/erkanrzgc).

## License

[Apache License 2.0](LICENSE) © [erkanrzgc](https://github.com/erkanrzgc). See [NOTICE](NOTICE).
Releases up to and including v0.2.0 were published under the MIT License and stay MIT
(K-132). Releases after v0.2.0 carry `THIRD_PARTY_LICENSES.txt`: the license of every module
compiled into the shipped binaries, including the Go standard library and, for
`kadran-caddy`, Caddy.
