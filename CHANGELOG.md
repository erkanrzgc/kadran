# Changelog

All notable changes are recorded here. Every claim links back to a measured
decision record (`K-…`) in [`docs/decisions.md`](docs/decisions.md).

## Unreleased

- Hang detection. panelyd pings systemd's watchdog (`WatchdogSec=60s`) only while
  its background loops (health supervisor, proxy watcher, disk check, backups) make
  progress and its database pool can hand out a connection. A plain ping goroutine
  would keep pinging through a deadlock. Measured under real systemd in CI:
  - a normal run caused no restarts;
  - a frozen daemon was killed and restarted;
  - SIGABRT left a goroutine dump showing the loops in the journal;
  - a clean stop was not counted as a watchdog failure.
  The unit and the binary must be upgraded together (K-115).
- panelyd's unit allows 180 s to start (was systemd's default 90 s). With a hung
  executor or Docker daemon, startup can take up to 103 s before the daemon
  reports ready. systemd used to kill it just before that point and restart it
  into the same wall, so `panely status` never answered. The bound is derived from
  the code's timeouts; the hang itself was not reproduced (K-117).

- `panely app show` marks which release is live. The old status column showed the
  build status only; after a rollback the top "built" release does not get the
  traffic. A separate line names the live release even when it is older than the
  listed ones. Against an older server that does not report the live release,
  it says "unknown" instead of claiming nothing is live (K-112).
- The reverse-proxy unit no longer has a reload command. It never worked, and a
  working one would have loaded the base configuration, which has no routes, and
  taken every site down. The next upgrade restarts the proxy once because its unit
  file changed (K-112).

## v0.1.0 — 2026-09-27

First tagged release. A single-node, security-first deployment panel: one server,
Git → container deploys, and a hard privilege boundary between the control plane
and the part that touches Docker.

### Architecture

- **Two daemons, one boundary.** `panelyd` (unprivileged, no network access,
  `IPAddressDeny=any`) decides; `panely-exec` (the only process that can reach
  Docker) executes a small, audited gRPC surface. The privileged code is held to a
  line budget that CI enforces (2498 / 2500).
- **SSH as the only door.** The CLI talks to the daemon over SSH with a forced
  command; no port is opened for the panel itself.
- **Hash-chained audit logs** on both sides of the boundary; `panely audit verify`
  checks both.

### Deploys and traffic

- Git → container builds from a commit's `Dockerfile`; `github.com` only by default,
  optional per-repository allowlist.
- Blue-green releases behind an HTTP health gate; rollback without rebuilding
  (~18 s, 55 / 55 probes answered `200` during the switch).
- Automatic HTTPS via a custom Caddy build without a file server; atomic reloads.
- The daemon watches the reverse proxy and restores its routes after a restart or
  crash: sites back after 3 s and 7 s; without the watcher they stayed down until
  the daemon itself restarted (K-112).
- Live logs, scaling, environment variables, persistent volumes mounted
  `nodev,nosuid`, pruning that always keeps the rollback target.
- A health supervisor that restarts failed releases with backoff.

### Backups

- Hourly SQLite snapshots with a tested restore path (K-091).
- Optional encrypted offsite copy with `age` public-key encryption; the server
  cannot decrypt its own past backups (K-098, K-104…K-107).
- Optional volume backups: a separate unit that can read every volume but has no
  network and no sockets, and emits only ciphertext. Restored end to end from
  Cloudflare R2 (K-111).

### Alarms

- Four failure conditions, edge-triggered and persisted (K-092).
- Optional delivery to Telegram from a separate unit; the daemon still cannot reach
  the network or read the bot token (K-108).
- Crashes and stops of the core services are reported; OnFailure was measured and
  rejected because it floods during a crash loop (K-110).
- The startup "traffic not flowing" alarm closes once the apps are routed again;
  it used to stay open after every reboot. It closes only when no app is skipped
  and the live proxy config matches exactly, so a route someone else added keeps
  it open (K-112).
- Optional external heartbeat on a Cloudflare Worker, for when the sender or the
  whole server is down (K-109).

### Install

- `panely bootstrap root@server` installs everything and checks its own work.
  Re-measured on a fresh Ubuntu 24.04 server before this release: fresh install,
  upgrades across commits, reverse-proxy restart and crash, reboot (K-112).
- Upgrades restart the control plane; they used to leave the old binaries running.
  The reverse proxy is restarted only when its binary or configuration changed, and
  its binary no longer changes with every commit (K-112).
- The installer stops early and clearly when Docker is missing or the upload runs
  past its time limit (K-112).

### Known gaps

Listed in full in the README. In short: audit chains are not cross-checked, hangs
are not detected, volume backups are not snapshots, no secret store, Dockerfile
builds from public repositories only, single node, CLI messages in Turkish.
