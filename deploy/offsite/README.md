# Offsite backup setup

The local backups (`/var/lib/kadran/backups`) are lost if the disk itself goes. This unit
**encrypts** them and copies them to a remote target.

## Threat model — what it protects, what it does not

| situation | result |
|---|---|
| The disk dies / the server is deleted | ✅ restored from the remote copy |
| The server is compromised and the attacker wants to **read past backups** | ✅ cannot decrypt them — the private key is not here |
| The server is compromised and the attacker wants to **delete the remote backups** | ⚠ CAN delete them unless the provider restricts it |
| The private key is lost | ❌ the backups CANNOT BE RECOVERED |

The last two rows are real and can be mitigated; how is described below.

## Why encryption is mandatory

Backups carry secrets. This was measured, not assumed:

```
sqlite3 backup.db "SELECT env_json FROM apps"
→ {"DATABASE_URL":"postgres://kadran:<password>@db:5432/..."}
```

(Since v0.5.0 the values are sealed with the executor's key (K-123); backups taken before
that hold them in plain text.) Uploading unencrypted would hand the apps' secrets to a third
party.

## Setup

### 1. Generate the key pair on YOUR OWN machine

The private key **never goes to the server.**

```bash
age-keygen -o kadran-backup-key.txt
```

The output contains a `# public key: age1...` line. The **public key** goes to the server;
you keep the whole file.

> ⚠ Keep `kadran-backup-key.txt` in at least two separate places (password manager +
> an offline copy). If it is lost, the backups cannot be decrypted. Do not keep it on the
> server — keeping it there defeats the whole purpose.

### 2. Define the remote target (on the server, as root)

```bash
sudo install -d -m 0755 -o root -g root /etc/kadran
sudo rclone config --config /etc/kadran/rclone.conf
sudo chown root:kadran /etc/kadran/rclone.conf
sudo chmod 0640 /etc/kadran/rclone.conf
```

The path is FIXED: the unit starts with `RCLONE_CONFIG=/etc/kadran/rclone.conf`.

> ⚠ **Do NOT put the configuration under `/var/lib/kadran`** (that is rclone's default
> location for the `kadran` user). That directory belongs to the daemon; a compromised
> kadrand could delete the file there and put its own in its place — even if the file is
> owned by root. Since an rclone configuration can run commands (e.g. webdav's
> `bearer_token_command`), that would give the network-less daemon a way to run commands
> in a process that has network access. `/etc/kadran` is root's directory; `kadran`
> cannot delete files there. See K-100.

Name the target `kadran-offsite`. Backblaze B2 and every S3-compatible provider work.

**Do NOT grant delete permission at the provider.** It is the only thing that closes the
"a compromised server can delete the remote backups" row:

- **Backblaze B2:** create the application key with `listBuckets, listFiles, readFiles,
  writeFiles` — do NOT grant `deleteFiles`. Turn on Object Lock / versioning on the bucket.
- **S3:** deny `s3:DeleteObject` in the IAM policy, and enable versioning + MFA delete on the
  bucket.
- **Cloudflare R2:** see the separate section below — R2 has NO "write but not delete"
  permission, so the protection is built another way.

Without delete permission the script's remote pruning does not work; that is a choice, not
a defect. Put `OFFSITE_PRUNE=hayir` in `offsite.conf` (the values are Turkish: `evet` = yes,
`hayir` = no) and leave old copies to the provider's lifecycle rule.

#### Cloudflare R2 (free tier: 10 GB, no egress fees)

Measured size (21 Sep): one encrypted backup is 143,592 bytes. 24 backups a day × 90 days
≈ 310 MB — far below the free tier. The database grows as apps are added; measure again if
that ratio changes.

> **If you installed before v0.4.0 (panely names, K-136):** the migration does NOT TOUCH
> your bucket or path; `offsite.conf` keeps pointing at the same bucket (only the rclone
> target's name changes from `panely-offsite` to `kadran-offsite`). But new objects are
> uploaded as `kadran-…`, and lock rules match by prefix: **BEFORE the migration** add the
> same two rules for the `kadran-` prefix too (steps 2 and 3 below: lock 30 days,
> lifecycle 90 days). Do not delete the `panely-` rules until the old objects have expired.
> Then measure the lock on the `kadran-` prefix with the "Do not trust the lock until it is
> measured" method below. The migration uploads the local backups once more under the new
> names (~3.4 MB on the live server).

⚠ **R2 tokens have NO write-without-delete permission.** The options are Admin Read & Write,
Admin Read, Object Read & Write and Object Read. Every token that can write CAN DELETE. What
stops deletion is the bucket's **bucket lock**:

1. Create a bucket: `kadran-backup` (Standard class — the free tier does NOT APPLY to
   Infrequent Access).
2. Add a **bucket lock** rule: prefix `kadran-`, retention **30 days**. A locked object
   cannot be deleted or overwritten before that time is up.
3. Add a **lifecycle** rule: prefix `kadran-`, delete after **90 days**. It must be longer
   than the lock; the lock always wins.
4. API token: **Object Read & Write**, for the `kadran-backup` bucket ONLY. Do NOT use an
   Admin token: it carries bucket management rights, and the lock is a bucket setting — a
   key on the server able to change the lock would defeat the lock's purpose.
5. On the server, set up an `s3` target named `kadran-offsite` with
   `rclone config --config /etc/kadran/rclone.conf`. The result should look like this:

   ```ini
   [kadran-offsite]
   type = s3
   provider = Cloudflare
   access_key_id = …
   secret_access_key = …
   endpoint = https://<account-id>.r2.cloudflarestorage.com
   acl = private
   no_check_bucket = true
   no_head = true
   ```

   `no_check_bucket = true` is REQUIRED: an object-level token cannot create buckets, and
   rclone otherwise fails with "Access Denied" (Cloudflare's own documentation).

   `no_head = true` is REQUIRED too (measured on 24 Sep, K-106): on a bucket with bucket lock
   R2 returns a version ID for every upload; rclone 1.60 sends `HEAD ?versionId=…` after the
   upload and R2 answers `501 Not Implemented`. The file IS WRITTEN, but rclone exits 1 and
   the script would count every upload as failed. The script already checks the size after
   each upload itself.
6. Put `OFFSITE_PRUNE=hayir` in `offsite.conf`. Pruning would try to delete locked files and
   print an error on every run.

**Do not trust the lock until it is measured.** Cloudflare's documentation does NOT say
explicitly that the lock overrides token permissions. After setup, try two deletions with
the same token:

- a file under the locked prefix (`kadran-…`) → it must be **refused**
- a test file under an unlocked prefix → it must be **deleted** (the control group: it proves
  the token has delete permission and that the refusal comes from the lock)

⚠ **The price of the lock — cost.** If the server is compromised, the key that can write
can upload LARGE files with the `kadran-` prefix to the bucket. The free tier is 10 GB;
beyond that you pay, and the lock prevents deleting those files for 30 days too. Do not
make the lock longer than needed; 30 days is a sufficient window to notice and react. Watch
the usage/billing notifications of your Cloudflare account.

### 3. Write the configuration (on the server)

```bash
sudo tee /etc/kadran/offsite.conf >/dev/null <<'CONF'
OFFSITE_REMOTE=kadran-offsite:kadran-backup
OFFSITE_RECIPIENT=age1...            # the PUBLIC key from step 1
OFFSITE_KEEP=30
# OFFSITE_PRUNE=hayir                # R2 / a token without delete: turn pruning off
CONF
sudo chmod 0640 /etc/kadran/offsite.conf
sudo chgrp kadran /etc/kadran/offsite.conf
```

⚠ `rclone.conf` holds the provider key, and since the uploader and the daemon run as the
same user (`kadran`), the daemon CAN READ it — but CANNOT CHANGE it. That is why the
delete-permission restriction in step 2 is mandatory: a key that is read must not be able to
delete the backups. OAuth-based providers (Google Drive, OneDrive) want to refresh the token
and WRITE it to the file; that fails on a read-only file. Use key-based B2/S3.

### 4. Enable the timer

```bash
sudo systemctl enable --now kadran-offsite.timer
sudo systemctl start kadran-offsite.service   # do the first run right away
journalctl -u kadran-offsite -n 30 --no-pager
```

### Upgrading

Since v0.5.0, `kadran bootstrap` replaces the installed script and units with the ones in
the package on every upgrade; the timer stays on if it was on and off if it was off
(K-138). Put your own settings in a drop-in with `systemctl edit`, not in the unit file;
drop-ins are kept.

When upgrading to v0.4.1 the script stays old and anchors are not uploaded. Copy the
script by hand (from the v0.4.1 tag):

```bash
scp deploy/offsite/kadran-offsite.sh root@server:/tmp/
ssh root@server install -m 0755 /tmp/kadran-offsite.sh /usr/local/lib/kadran/offsite/
```

## Restore

```bash
# 1. Download from the remote
rclone copy kadran-offsite:kadran-backup/kadran-20260918T083505Z.db.age .

# 2. Decrypt on YOUR OWN machine (the private key is here)
age -d -i kadran-backup-key.txt \
    -o kadran.db kadran-20260918T083505Z.db.age

# 3. Move it to the server and restore
scp kadran.db root@server:/tmp/
ssh root@server 'systemctl stop kadrand && \
  /usr/local/lib/kadran/kadrand --restore /tmp/kadran.db'
```

Before running, `--restore` takes a safety copy of the current database and verifies the
integrity of the restored file (K-091).

> ⚠ **These backups do not carry volume data.** Only the control plane database: app
> definitions, releases, the audit chain. For the containers' persistent volumes
> (`/var/lib/kadran/volumes`) set up the **volume backup** below as well.

> After a restore the audit chain forks from the moment of that backup: newer anchors now
> contradict it (below). Note the time of the restore and pass it to the anchor check with
> `-anchors-since`.

## Chain anchors (K-126)

kadrand writes the tip of the audit chain next to every backup
(`kadran-<stamp>.capa`: sequence number + hash), and this unit uploads it **without
encryption**. It contains no secrets; the `kadran-` prefix is covered by the bucket lock, so
an uploaded anchor cannot be changed or deleted for 30 days. Pruning does not touch anchors;
the lifecycle rule deletes the old ones.

A compromised kadrand can rewrite its own chain from scratch, and `audit verify` still says
"valid". Anchors catch that; on YOUR OWN machine:

```bash
rclone copy kadran-offsite:kadran-backup ./anchors --include 'kadran-*.capa'
kadran audit verify -anchors ./anchors kadran-client@server
```

The CLI recomputes the chain itself and does not trust the hashes the server sends. If a
single anchor contradicts it, or the chain is shorter than an anchor, the exit code is `3`.
Days without an anchor are listed as a warning (no upload happened that day).

Limits: only the daemon chain (the executor's is not anchored), back as far as the lock
(30 days) and up to the newest anchor. Since the daemon user can read `rclone.conf`, it can
add FAKE anchors; it cannot change the locked real ones, and a single contradicting anchor
turns the result red.

## Volume backup — app data (K-111)

kadrand cannot read the apps' persistent volumes, and that is a safeguard (K-091). A
separate unit archives the volume data:

| | |
|---|---|
| reads | every volume — its only privilege: `CAP_DAC_READ_SEARCH` |
| reaches | nothing — no network, no sockets (the Docker socket included) |
| writes | only `/var/lib/kadran-volume-backup` |
| produces | an archive encrypted with `age`, to the same public key as the offsite backup |

kadrand can read the archives but cannot decrypt them; it cannot delete or overwrite them.
The uploader moves the archives offsite as they are (without re-encrypting them). All of
this was measured on the server with control groups.

### ⚠ NOT a snapshot

The app is not stopped; files are read one by one while it runs. In an app that holds a
database the files may come from different moments, and the restored copy may be
**corrupt**. Also take the database into the volume as a **dump** — a dump file is
consistent:

```bash
pg_dump -U app app > /data/dump.sql              # PostgreSQL
sqlite3 /data/app.db ".backup /data/backup.db"   # SQLite
```

### Setup

The offsite backup (above) must be set up: the recipient key is read from `offsite.conf`.

```bash
sudo install -m 0755 deploy/offsite/kadran-volume-backup.sh /usr/local/lib/kadran/offsite/
sudo install -m 0644 deploy/systemd/kadran-volume-backup.service \
                     deploy/systemd/kadran-volume-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now kadran-volume-backup.timer
sudo systemctl start kadran-volume-backup.service   # do the first run right away
journalctl -u kadran-volume-backup -n 20 --no-pager
```

`offsite.conf` and its parent directories must be owned by root and not writable by group
or others; otherwise the unit refuses to run. Someone able to change the recipient would get
all app data encrypted to their own key.

The unit runs every night at 23:30 and the uploader picks it up at midnight. Locally,
`OFFSITE_VOLUME_KEEP` (default 3) archives are kept per app. Remote pruning
(`OFFSITE_PRUNE=evet`) keeps `OFFSITE_KEEP` volume archives **per app**.

### Restore

```bash
# 1. Download and decrypt on YOUR OWN machine (the private key is here)
rclone copy kadran-offsite:kadran-backup/kadran-hacim-web-20260926T233000Z.tar.zst.age .
age -d -i kadran-backup-key.txt -o web.tar.zst kadran-hacim-web-20260926T233000Z.tar.zst.age

# 2. Move it to the server, set the current volume aside, unpack the archive
scp web.tar.zst root@server:/root/
ssh root@server
cd /var/lib/kadran/volumes
mv web .web-old         # dotted name: the archiver skips it
zstd -dq < /root/web.tar.zst | tar -x --numeric-owner -f - -C /var/lib/kadran/volumes
# After checking: rm -rf .web-old /root/web.tar.zst
```

`--numeric-owner` is REQUIRED: the container's user (e.g. uid 101) may map to someone
else's name on this machine; the number must be kept.

This path was measured end to end on the live server (K-111): the archive came down from R2,
was decrypted on a workstation and unpacked on the server; type, mode, owner, size and
sha256 matched the original. ⚠ The volume in that drill was NOT attached to a running
container. If a container uses the volume, stop it first; that step was not measured.

### Cost

Every night a FULL archive of every app is taken, not an incremental one. With the R2 lock
(30 days) and lifecycle (90 days), each archive stays offsite for ~90 days: X MB a day →
~90 × X MB offsite. R2's free tier is 10 GB; if all apps' archives together exceed ~110 MB a
day, you move into the paid tier.

## Verification

The script **measures** the upload instead of assuming it: after every file it reads the
remote size and compares it with the local one. If they differ, the unit fails.

Partial success does not count as success — if even one file cannot be uploaded,
`kadran-offsite.service` goes into the `failed` state:

```bash
systemctl status kadran-offsite.service
systemctl list-timers kadran-offsite.timer
```

The failure is reported to Telegram: the unit calls the alarm sender through `OnFailure=`
(K-108, [`deploy/notify`](../notify/README.md)). If the sender is not installed the unit
still stays `failed`, but nobody is told.
