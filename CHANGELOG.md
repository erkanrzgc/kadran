# Changelog

All notable changes are recorded here. Every claim links back to a measured
decision record (`K-…`) in [`docs/decisions.md`](docs/decisions.md).

## Unreleased (v0.4.1)

### Audit chain anchors (K-126)

A compromised daemon could rewrite its own audit chain into one that still verifies.
With offsite backups on, every daemon backup now carries an anchor, the chain's head
(`seq` and hash), uploaded unencrypted under the bucket-locked `kadran-` prefix.
`kadran audit verify -anchors <dir>` recomputes the daemon chain on your machine,
ignores the hashes the server sends, and exits `3` if any anchor disagrees or the
chain is shorter than an anchor. `-anchors-since` sets aside anchors from before a
database restore. It covers the daemon chain only, back to the lock period.

- `ListAuditRecordsRequest.after_seq` is exclusive; its comment said inclusive.

### Security

- **The install followed symlinks in the client user's `.ssh`** (K-137), the same
  class the v0.4.0 migration fixed. Writing the admin key used a fixed temporary name
  and appended through the path; creating `.ssh` followed a symlink and handed the
  target directory to the client user. Both now stop on a symlink, and the new
  `authorized_keys` is written to a `mktemp` copy and moved into place.

## v0.4.0 — 2026-10-03

The second half of the rename: every name on the server is now `kadran`, and
`bootstrap` migrates an existing install in place.

### Upgrading from v0.3.0

Run `kadran bootstrap` with the v0.4.0 files, as `root@server` or `-sudo user@server`.
It detects the old (`panely`) install and migrates it in place (K-136).

- **Before you start:** if you use offsite backups, add the bucket lock and lifecycle
  rules for the `kadran-` prefix ([offsite guide](deploy/offsite/README.md)). Don't
  push to repositories that deploy through CI while the migration runs.
- Users are renamed with `usermod`: uid/gid and the ownership of every file stay the
  same. The database, backups, volumes, deploy keys, TLS certificates, both audit
  chains and the optional offsite, volume-backup and alarm units move to the new
  names. A timer that was off stays off.
- Your own systemd drop-ins follow their units. The migration stops before the new
  executor starts if its effective `--allow-repo` list differs from the old one.
- The old proxy and containers keep serving until the new control plane has started
  its own containers. The site is down only while the proxy switches: about 1.3 s on
  the Debian 13 test server, read from the journal; 0 of 80 probe requests failed.
- While they overlap, the old and new replicas of an app mount the same volume, as in a
  blue-green deploy but for longer. Stop apps that keep a single-writer database in a
  volume before migrating.
- If the new containers don't come up within 5 minutes, the migration stops and the
  site stays on the old stack. Running `bootstrap` again continues where it stopped.
- The daemon briefly cannot reach the proxy during the switch and raises "proxy not
  reconciled". With Telegram delivery you get that alarm and its recovery.
- **Afterwards:** move CI to the v0.4.0 CLI and the `kadran-client@` target at the same
  time. The desktop app moves its saved profiles on first start.
- **Rollback:** `/usr/local/lib/kadran/kadran-geri-donus.sh` on the server (installed
  before anything moves), then `bootstrap` from v0.3.0. The site is down until the old
  stack is back: ~75 s on the test server.

### Breaking changes

- **Protocol 3.** The wire protocol is now `kadran.v1`; a v0.3.0 CLI cannot talk to a
  v0.4.0 server or the other way round. The new CLI recognizes an old server and says
  to run `bootstrap`, and answers a `panely-client@` target with the `kadran-client@`
  form.
- Server names: `kadrand`, `kadran-exec`, `kadran-connect`, `kadran-caddy`; users
  `kadran`, `kadran-client`, `kadran-caddy`; `/var/lib/kadran`, `/etc/kadran`,
  `/usr/local/lib/kadran`; `kadran.db`; Docker labels `kadran.*`, images `kadran/<app>`;
  backups `kadran-<time>.db`; volume archives `kadran-hacim-…`; the rclone remote
  `kadran-offsite`; environment variables `KADRAN_*`.

### Found by rehearsing the migration

The migration was run on the Debian 13 test server against a copy of the live setup:
migrate, reboot, roll back, migrate again, and once more after the security fixes
(K-136). That found four problems before they reached the live server:

- **The executor's repository allowlist would have been dropped.** On the live server it
  lives in an operator drop-in (`panely-exec.service.d`). Without carrying it over, the
  new executor would have started without the restriction. Drop-ins now follow their
  units, and an allowlist gate stops the migration if the effective list changes.
- **An app without a domain would have blocked the migration from ever finishing.**
  Cleanup waited for every container to appear in the proxy config, and an app with no
  domain has no route. Finishing and cleanup are now separate. Old containers are
  removed only once none of them receives traffic.
- **The first version was down for 16 s,** mostly waiting for the proxy watcher's 10 s
  tick. The daemon now restarts right after the new proxy, and startup reconciliation
  writes the routes immediately: about 1.3 s.
- **An installed but disabled optional unit was dropped** (found in a second rehearsal
  after the security fixes). Only units with an enabled timer were reinstalled under the
  new names. Every installed one is now carried over, and only the enabled ones are
  turned on.

### Found by a security review of the migration

The migration scripts were reviewed separately after the rehearsal. No path started the
executor without its allowlist. Fixed, each with a test scenario and a mutant (K-136):

- **Cleanup could wedge every later install.** It ran before the post-install checks, and
  a network or image still in use aborted the install, again on every rerun. It now runs
  after the checks pass and only warns.
- **An interrupted run could not resume** if it stopped between renaming the rclone remote
  and updating `offsite.conf`. Each file is now handled on its own.
- **Root rewrote files through symlinks.** A fixed temporary name inside the client user's
  `.ssh` let that user make root overwrite another file. Temporary copies now come from
  `mktemp`, which also keeps the rclone key from being briefly world-readable, and
  symlinked files or directories stop the migration.
- **An unreadable `systemctl show` counted as "no allowlist".** It now stops the
  migration.
- **Rollback deleted drop-ins edited after the migration.** It now keeps them in the
  migration record.

### Documentation

- The README is reorganized around a quick start, the migration guide, and long
  reference material folded into collapsible sections.
- The README's GitHub Actions example is now a complete workflow: the one that
  deploys the author's site on every push to `main`. It downloads the CLI and
  checks it against `SHA256SUMS`, reads only repository contents, and queues
  deploys instead of running them side by side (K-135).
- The README no longer says the workflow deploys "the commit that was just
  tested". The example runs no tests: it deploys whatever lands on `main`. To
  deploy only tested commits, add a test job and `needs:` (K-135).
- `[skip ci]` in a CI-deployed repository: GitHub looks only at the last commit of a
  push, so a code commit followed by a `[skip ci]` commit is not deployed (measured,
  K-129).

## v0.3.0 — 2026-10-02

Deploy-only keys for CI, an audit log that finally records which key acted, the
rename to Kadran, and the move to Apache-2.0.

### Upgrading from v0.2.0

Run `kadran bootstrap` with the v0.3.0 files, as `root@server` or `-sudo user@server`.
The workstation tool is now called `kadran`; the server side keeps its names.
- The reverse proxy does **not** restart: its binary and units are unchanged.
  Measured twice on the Debian 13 test server (K-131, K-134).
- The post-install check now requires every line in
  `~panely-client/.ssh/authorized_keys` to be forced to `panely-connect`. If you
  added lines by hand, it reports them.
- Deploy-only keys need the server and the CLI both at v0.3.0. An older
  `panely-connect` does not know `-deploy` and refuses such a key (it never falls
  back to admin rights).
- To roll back, remove any deploy-key lines (`kadran key remove`), then run
  `bootstrap` from the v0.2.0 tree. Never swap a binary on its own.

### License: Apache-2.0

Kadran is now licensed under the Apache License 2.0 (was MIT). v0.1.0 and v0.2.0
were released under MIT and remain available under it. The change adds an explicit
patent grant and spells out that contributions come under the same terms; see
`NOTICE` and K-132.

- **Release files now carry license texts.** Earlier releases shipped bare
  binaries; the server archive held four binaries and no license file. The
  binaries embed Caddy and gRPC (Apache-2.0) and SQLite and protobuf (BSD), whose
  licenses must travel with them. Each release now includes `LICENSE`, `NOTICE`
  and `THIRD_PARTY_LICENSES.txt`, both inside the server archives and as separate
  files. The third-party file is generated from `go list -deps` for every shipped
  binary and platform, and the build fails if any module has no license file.
- Release files are named `kadran-<version>-<os>-<arch>` and
  `kadran-server-<version>-linux-<arch>.tar.gz` (were `panely-…`).

### Renamed to Kadran

The project is now **Kadran** (from the Turkish word for a dial or gauge face). The
repository moved to `github.com/erkanrzgc/kadran`; GitHub redirects the old address.
- The command-line tool is `kadran` (was `panely`), built from `cmd/kadran`. The Go
  module path is `github.com/erkanrzgc/kadran`.
- Server-side names are unchanged: the services (`panelyd`, `panely-exec`,
  `panely-caddy`, `panely-connect`), the `panely` and `panely-client` users, the
  directories, container labels, image names, and backup file prefixes. Existing
  installs need nothing, and you keep connecting as `panely-client@server`.
- The systemd unit files and the reverse proxy's module are deliberately untouched.
  Changing them would have restarted the reverse proxy on the next upgrade (K-130).
- Environment variables keep their `PANELY_` names.

### Security

- **Deploy-only keys.** An `authorized_keys` line with
  `panely-connect -deploy=site,api` gives a key that can only deploy those apps:
  `Ping` and `Deploy` for the listed apps, `PermissionDenied` for every other call.
  Lines without the flag stay admin keys, so existing installs are unchanged.
  - The key cannot read app definitions (they carry environment values). Pass the
    commit with `kadran deploy -commit`; the CLI says so when it is missing.
  - An empty, unknown, or malformed role is refused when the connection opens.
    It never falls back to admin rights.
  - Streaming calls (`Deploy`, `StreamLogs`) now go through an interceptor too.
    Before this they skipped the server's interceptor chain entirely.
  - Scope limits, including that a deploy key effectively owns the secrets of
    the apps it may deploy, are in `SECURITY.md` (K-131).
  - `kadran key add -deploy site,api ci.pub root@server` adds such a key;
    `key list` shows every key with its role and fingerprint; `key remove`
    deletes one. They use the same root path as `bootstrap` (`-sudo` works).
    `key add` refuses a key that already has a line, `key remove` refuses the
    last admin key, and `key list` exits non-zero on any line that is not forced
    to `panely-connect`. The server re-checks every line it is asked to write.
- **Fixed: the audit log never recorded which SSH key acted.** The actor's key
  fingerprint was read from `SSH_AUTH_INFO_0`, a PAM-internal variable OpenSSH
  keeps out of the session, so it was always empty: on the live server, 0 of 56
  SSH audit records carried one. It is now read from the file sshd names in
  `SSH_USER_AUTH`, the documented `ExposeAuthInfo` interface. Records written
  before the upgrade stay without a fingerprint (K-134).
- **Fixed: a multi-line public key file opened a shell.** `bootstrap` wrote the
  admin line as `command=...,restrict <file contents>`. With two keys in the file
  (for example `https://github.com/<user>.keys`), the second one landed in
  `authorized_keys` as a separate line with no forced command and no `restrict`.
  - `bootstrap` now refuses key files with more than one line and sends the
    single validated line; `install.sh` refuses it again before writing.
  - The post-install check used to pass if *any* line had a forced command. It
    now requires every line to be forced to `panely-connect` with `restrict`.
    If you added lines to `~panely-client/.ssh/authorized_keys` by hand, the next
    upgrade will report them.

### CLI

- **DNS check before setting a domain.** `app create -domain` and
  `app update -domain` compare the domain's A and AAAA records with the server's
  address before they contact the server.
  - They stop when the domain has no records or points somewhere else. The
    command used to succeed, the certificate never came, and Caddy could wait up
    to a day before retrying once the DNS was fixed.
  - They warn and continue when the answer is unclear: a server on a private or
    Tailscale address, an AAAA record when the server's IPv6 address is unknown,
    or a failing DNS query.
  - The server's address comes from the SSH target, including `HostName` from
    your SSH config. `.localhost` names are skipped. `-skip-dns-check` bypasses
    the check, for example behind Cloudflare's proxy.
  - Measured with the real resolver: a wildcard record pointing elsewhere was
    refused, as were a nonexistent name and a name that points at the other
    server. The check refused all three before connecting (K-128).
- **`kadran domain check <domain> [target]`** diagnoses a missing certificate from
  your machine: DNS against the server's address, TCP to ports 80 and 443, the
  HTTP redirect on port 80, and the certificate on 443 (trusted, which issuer,
  days left). It reports every step instead of stopping at the first problem, and
  exits 1 if any step fails. An untrusted certificate is described (subject,
  issuer, names). When the DNS points elsewhere, it says that the remaining lines
  measured that other host (K-128).

### Known gaps

- Audit records written before the upgrade carry no key fingerprint; only new
  records do (K-134).
- A deploy key can deploy any commit the configured repository serves, including
  commits from open pull requests: `panelyd` has no outbound network to check the
  branch. Use the key only in workflows triggered by pushes (K-131).
- The two audit chains are still verified separately, not against each other, and
  there is still no secret store (K-123, K-126 drafts).

## v0.2.0 — 2026-10-01

Hang detection, a bounded start-up, installs without root SSH, and a second tested
platform (Debian 13).

### Upgrading from v0.1.0

Run `panely bootstrap` again with the v0.2.0 files, either as `root@server` or as
`-sudo user@server`.
- The unit file and the binary must change together. The new unit turns on
  systemd's watchdog, and an old panelyd under it would be killed every minute.
- To roll back, run `bootstrap` from the v0.1.0 tree. Never swap a binary on its
  own.
- The reverse proxy restarts once during this upgrade, because its unit changed.
  The restart cost about 1.5 s of downtime (K-118).

### Server

- **Hang detection.** panelyd pings systemd's watchdog (`WatchdogSec=60s`) only while
  its background loops (health supervisor, proxy watcher, disk check, backups) make
  progress and its database pool can hand out a connection. A plain ping goroutine
  would keep pinging through a deadlock. Measured under real systemd in CI:
  - a normal run caused no restarts;
  - a frozen daemon was killed and restarted;
  - SIGABRT left a goroutine dump showing the loops in the journal;
  - a clean stop was not counted as a watchdog failure.

  The thresholds (15 minutes, 3 hours for backups) are derived from the code's
  timeouts. Production was idle for its first 22 hourly reports: the longest gaps
  were 2–3 s for the supervisor and 10 s for the proxy watcher (K-115, K-118).
  Under normal load on a test server (deploys and killed containers), the longest
  gaps were 7 s for the supervisor and 10 s for the proxy watcher. The hung-executor
  cases that the 15-minute floor exists for were not measured (K-115).
- The watchdog's hourly report counts the interval that is still open. A loop that
  is stuck but has not yet reached its threshold used to be invisible, and an hourly
  loop always showed "0s" (K-118).
- panelyd's unit allows 180 s to start (was systemd's default 90 s). With a hung
  executor or Docker daemon, startup can take up to 103 s before the daemon reports
  ready. systemd used to kill it just before that point and restart it into the same
  wall, so `panely status` never answered. The bound is derived from the code's
  timeouts; the hang itself was not reproduced (K-117).
- The reverse-proxy unit no longer has a reload command. It never worked, and a
  working one would have loaded the base configuration, which has no routes, and
  taken every site down (K-112).
- `panely app show` marks which release is live. The old status column showed the
  build status only; after a rollback the top "built" release does not get the
  traffic. A separate line names the live release even when it is older than the
  listed ones. Against an older server that does not report the live release, it
  says "unknown" instead of claiming nothing is live (K-112, measured against the
  old server in K-118).

Measured in production on 30 September (K-118):
- the upgrade itself cost ~1.5 s of downtime, measured from inside the server;
- a killed reverse proxy was serving again in ~6.4 s;
- a reboot onto a new kernel had sites back after ~43 s, and the startup alarm
  closed on its own;
- the watchdog was armed after the upgrade and after the reboot;
- Telegram delivery worked end to end for the proxy crash and the reboot.

### Install

- `panely bootstrap -sudo user@server` installs and upgrades through the user's
  passwordless sudo, so root SSH stays closed and sshd's policy is left alone.
  - The privilege check and the install itself run under `sudo -n`, which never
    prompts; the upload and the log follow run as the user. Before anything is
    uploaded, the installer checks in the exact form it will use that it becomes
    uid 0, and it stops with sudo's own message if it cannot.
  - Root mode also checks the uid before uploading. `panely-client` is refused as an
    install account in both modes.
  - Measured on Debian 13 on GCP: 17/17 checks passed, root login was refused before
    and after, sudo's log recorded the command, and sites answered throughout the
    upgrade (K-122).
- **A dropped connection no longer restarts the install.** On 1 October three long
  uploads in a row were cut with "Connection reset by peer", on two providers.
  - The package goes to the SSH user's own directory, named by its SHA-256. After a
    drop only the missing bytes are sent. They are written at an explicit offset, so
    a dead session's late write cannot shorten or shift the file.
  - The server checks the SHA-256 before installing. A corrupt package is deleted
    and uploaded once more.
  - The install runs detached from the SSH session and logs to a file. The CLI
    follows the log from the last byte it printed and reconnects if the link drops.
  - If the install process dies before it finishes (for example, out of memory),
    the CLI says so and does not wait forever. Only one install runs at a time.
  - SSH keepalives now detect a connection that dies without a reset.

  Measured with the real tools on Debian 13, including a real `sudo -n` as an
  unprivileged user (K-127).
- `panely bootstrap` sends a gzip-compressed package: 28.3 MiB instead of 74.7 MiB
  with the same binaries. On a slow link the uncompressed upload took about
  13 minutes and dropped once (K-119).
- **Debian 13** is a tested platform, alongside Ubuntu 24.04. Tested on a GCP
  e2-micro with 1 GB RAM and 2 GB swap. Measured:
  - all 17 post-install checks pass;
  - HTTPS with a Let's Encrypt certificate works end to end;
  - a Node build peaked with 237 MiB free and no OOM;
  - after a reboot everything came back and the startup alarm closed on its own.

  GCP's guest agent did not touch `panely-client`'s forced-command key through
  metadata changes, an agent restart or a reboot. Never add `panely-client` to SSH
  metadata (K-121).

### CLI

- When SSH cannot connect (unknown host, rejected key, changed host key), the CLI
  shows SSH's own message. It used to show only "error reading server preface:
  EOF", which hid even a host key verification failure. A child process that
  keeps SSH's stderr open can no longer turn a fast failure into a long hang
  (K-120).

### Known gaps

Listed in full in the README. In short:
- audit chains are not cross-checked;
- hang detection covers only what its loops and probe watch, with derived
  thresholds;
- volume backups are not snapshots;
- no secret store;
- Dockerfile builds from public repositories only;
- single node;
- CLI messages are in Turkish.

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
