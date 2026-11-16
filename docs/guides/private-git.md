# Private GitHub repositories

After this guide you can deploy a private GitHub repository by branch name, without putting a
token on your workstation or in an app definition.

You connect your server to GitHub once. Kadran uses a GitHub App called **Kadran Deploy** that can
only read the repositories you pick. The sign-in token is created on the server and never leaves
it. Your workstation sees a short code, nothing else.

Private builds are off until you connect. Upgrading changes nothing for public repositories.

## How it works

- A small service, `kadran-git`, holds the token and talks to GitHub. It runs as its own
  unprivileged user.
- `kadrand` (the daemon) and your workstation never see the token. The root executor never
  reaches the internet.
- For a private repository, `kadran-git` downloads the archive of the exact commit and the
  executor checks it before Docker builds from it.
- Public repositories keep using the plain Git path, unchanged.

## Before you start

You need an admin SSH key for the server, a server and client that support GitHub connect, and a
GitHub account that can install apps on the repositories you want. If the server is too old,
`kadran github connect` says `this server does not support GitHub connect: upgrade the server`.
See [Upgrading](#upgrading) first if the server already ran an older private-repo setup.

With [TOTP](totp.md) on, run `kadran login` first.

## Step 1: install the app on selected repositories only

The connect command (next step) prints the install link. It looks like
`https://github.com/apps/kadran-deploy/installations/new`.

On the GitHub page, choose **Only select repositories** and pick the repositories Kadran may
build. Do not choose **All repositories**: that lets the app read every repository of the
account, present and future. Kadran accepts it but warns you every time (see
[The "All repositories" warning](#the-all-repositories-warning)).

## Step 2: connect

```bash
kadran github connect your-server
```

```
1. Install the Kadran Deploy app on the repositories Kadran may build:
   https://github.com/apps/kadran-deploy/installations/new
2. Open https://github.com/login/device and enter the code:
   ABCD-1234
Waiting for approval (expires 14:25)…
```

1. Install the app (step 1), if you have not already.
2. Open `https://github.com/login/device` and type the code. It is valid for 15 minutes.
3. Leave the command running. It checks every 3 seconds and ends when GitHub approves.

```
Connected as your-login.
  your-login: 2 repositories (selected)
```

If you press Ctrl-C, the sign-in keeps running on the server until the code expires. If you
entered the code in time, the connection is made anyway. Check with `kadran github status`.

`your-server` is the same target you pass to other commands (`kadran-client@your-server`). You can
leave it out when you have only one saved server.

Connecting and disconnecting are written to the audit log: the account name and the selected
repositories, never a token.

## Step 3: deploy by branch

Deploy as usual. Without `-branch`, Kadran uses the app's default branch:

```bash
kadran deploy -branch main APP
```

If the anonymous lookup of the branch fails, because the repository is private, the client asks
the server. The server asks GitHub for the branch's commit and deploys that exact commit.

A deploy-only key (CI) cannot do this lookup, because it cannot read the app definition. In CI,
pass the full commit as before:

```bash
kadran deploy -commit FULL_COMMIT_SHA APP
```

## Check the connection

```bash
kadran github status your-server
```

```
connected as your-login (since 2026-11-12 14:20)
  your-login: 2 repositories (selected)
Manage the app installation: https://github.com/settings/installations
```

Other states you can see:

| Output | Meaning |
|---|---|
| `not connected: run kadran github connect` | No connection. Public builds still work |
| `waiting for approval: code ABCD-1234, expires 14:25` | A sign-in is open. Enter the code on GitHub |
| `connection failed: ...` | The last sign-in ended in an error. Run `connect` again |
| `reconnect required: run kadran github connect` | The saved sign-in no longer works (see below) |

## Desktop

The desktop app has a **GitHub** section in the server's security area. It does the same three
things: connect, show the status, and disconnect. While it waits, it shows the code, a **Copy**
button, **Open github.com/login/device** and a link to install the app. Disconnect asks you to
type a word to confirm.

## Disconnect and revoke

```bash
kadran github disconnect your-server
```

```
Disconnected. The token was deleted on the server.
To revoke the grant on GitHub as well:
  authorized apps:    https://github.com/settings/apps/authorizations
  app installations:  https://github.com/settings/installations
```

Disconnect deletes the token on the server and stops private builds. It does **not** revoke the
grant on GitHub, because that needs the app's client secret and Kadran never holds one. A token
that was copied before you disconnected stays valid until it expires (8 hours). To remove it for
good:

- Open [Authorized GitHub Apps](https://github.com/settings/apps/authorizations) and revoke
  Kadran Deploy.
- Open [Installed GitHub Apps](https://github.com/settings/installations) and uninstall or
  narrow the repository list.

## Limits

Private builds do not run `git clone`. Kadran builds from the archive of the exact commit, so:

- There is **no `.git` directory** in the build context. A Dockerfile that reads Git history
  (for example `git describe`) fails.
- **Submodules are not checked out.**
- **Git LFS** content depends on your repository's
  [GitHub archive setting](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/managing-git-lfs-objects-in-archives-of-your-repository).
  Kadran does not run a separate LFS fetch.
- Source trees that need symlinks are not supported.
- The archive is at most 128 MiB compressed, 512 MiB unpacked and 100,000 entries. The download
  is limited by `kadran-git`'s own two-minute timeout per request to GitHub. On the executor side, `kadran-git` has 5 seconds to decide whether a repository is private, and the archive stream may pause for at most 60 seconds between chunks.
- The token is not passed to your Dockerfile. Downloading other private packages during a build
  does not use it.

Only repositories that the installation lists as **private** take this path. Public repositories
(even ones inside an "All repositories" installation) build as before, with none of these limits.

If `kadran-git` is down, public builds continue. A private repository then fails in Docker
because it cannot reach the repository anonymously. See
[Troubleshooting](#troubleshooting).

`--allow-repo` on the executor still works. When set, it limits every build, private or public,
to that list.

## The "All repositories" warning

If you installed the app with **All repositories**, `connect` and `status` print:

```
  your-login: ALL repositories — consider Only select repositories
```

The desktop shows: "The app can read ALL repositories of an account. Consider Only select
repositories." Kadran does not refuse this. It is a risk you choose to take: anyone who can
deploy on your server can then build any repository of that account. To fix it, open
`https://github.com/settings/installations`, open the app's **Configure** page and switch to
**Only select repositories**.

## Register your own GitHub App

By default Kadran uses the maintainer's GitHub App:

| | |
|---|---|
| Name | Kadran Deploy |
| Client ID | `Iv23lizBm3zlCdYH3NQ5` |
| Short name (slug) | `kadran-deploy` |

The client ID and slug are public. They are not secrets.

### What you trust by default

Whoever holds the private key of the default App can read the selected repositories of **every**
account that installed it. That is true of every public GitHub App. Kadran stores no private key
and no client secret on your server, and the maintainer does not publish one. You still trust the
App's owner to protect their key.

If you do not want that trust, register your own App. Then only you hold its key, and nobody else
can read your repositories through it.

### Register

1. On GitHub, open **Settings, Developer settings, GitHub Apps, New GitHub App**.
2. Set these values:
   - Webhook: off.
   - Callback URL: none.
   - Permissions: **Contents: Read-only**. GitHub adds **Metadata: Read-only**.
   - **Enable Device Flow**: on.
   - **Expire user authorization tokens**: on.
   - Where can this app be installed: **Only on this account**.
3. Create the App. Note its **Client ID** and its short name (the last part of its
   `https://github.com/apps/...` address).
4. You do not need to generate a private key or a client secret.

### Point `kadran-git` at your App

Add a drop-in on the server. Clear `ExecStart=` first, then set it. The path is the one the
install uses.

```bash
sudo systemctl edit kadran-git.service
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/lib/kadran/kadran-git --client-id YOUR_CLIENT_ID --app-slug YOUR_APP_SLUG
```

```bash
sudo systemctl restart kadran-git
```

The installer does not write a drop-in for `kadran-git`, so yours stays across upgrades.
After you change the client ID, the old connection no longer counts (it belongs to another
App). Run `kadran github connect` again.

## Upgrading

Private repositories used to need a token file and a drop-in on `kadran-exec` that set
`--git-credentials`. That path is gone. The server upgrade (`kadran bootstrap`) checks two things
before it replaces any binary, and stops with a message if one fails:

- **`systemd-resolved` must be running, with its stub at `127.0.0.53`.** `kadran-git` resolves
  GitHub through that stub. If it is missing, the upgrade stops:

  ```
  kadran-git needs systemd-resolved (stub resolver at 127.0.0.53)
  ```

  Installing it changes how the host resolves DNS. A host that already runs its own resolver
  can lose DNS. Keep a copy first: `sudo cp /etc/resolv.conf /root/resolv.conf.bak`. Then
  install and enable it (on Debian: `sudo apt install systemd-resolved`, then
  `sudo systemctl enable --now systemd-resolved`) and check:

  ```bash
  resolvectl status
  getent hosts api.github.com
  grep nameserver /run/systemd/resolve/stub-resolv.conf   # must show: nameserver 127.0.0.53
  ```

  The last file is the one the installer's pre-check reads. Run the upgrade again when all
  three work.

- **The `kadran-exec` drop-in must not set `--git-credentials`.** If it does, the upgrade stops:

  ```
  remove --git-credentials from the kadran-exec drop-in; private repositories now use `kadran github connect`
  ```

  Run `sudo systemctl edit kadran-exec.service`, delete `--git-credentials ...` from the
  `ExecStart=` line (keep `--allow-repo` if you use it), save, and upgrade again. You can also
  delete `/etc/kadran/private-git/credentials.json`; nothing reads it now. After the upgrade,
  run `kadran github connect`.

## Troubleshooting

| Message | What it means and what to do |
|---|---|
| `the kadran-git service is not running` | The server has no running `kadran-git`. Check `systemctl status kadran-git`. Public builds still work. Private ones fail until it runs |
| `this server does not support GitHub connect: upgrade the server` | The server is older than the client. Run `kadran bootstrap` to upgrade |
| `kadran-git is older than kadrand: upgrade the server` | The two services are from different versions. Upgrade the server |
| `reconnect required: run kadran github connect` | The saved sign-in no longer works: it was not used for 6 months, GitHub revoked it, or the server could not save a renewed token. Run `kadran github connect` |
| `the code expired: run kadran github connect again` | You did not enter the code in 15 minutes. Run `connect` again |
| `the sign-in flow ended on the server; run connect again` | A newer `connect` replaced this one, or the server restarted. Run `connect` again |
| `access was denied on github.com` | You pressed Cancel on GitHub. Run `connect` again and approve |
| `device flow is disabled for the GitHub app` | Your own App needs **Enable Device Flow** turned on |
| `the GitHub app client id was rejected` | `--client-id` does not match a GitHub App |
| `kadran-git could not reach GitHub` | The server cannot reach `github.com`. Check its DNS and network |
| `GitHub rate limit reached, retry later` | Wait, then try again |
| ``No installation yet: install the app, then run `kadran github status`.`` | Printed by `connect`: you connected but installed the app on no repository. Open the install link under it |
| `No installation yet: install the app with the link below.` | Printed by `status`: same cause. The install link is on the last line of the output |
| `ALL repositories — consider Only select repositories` | See [the warning](#the-all-repositories-warning) |

### A private build fails

A private build that fails with `(if it is private, install the Kadran Deploy app on it)` after
Docker's own error means the server is connected, but the app is not installed on that
repository. Open `https://github.com/settings/installations`, configure Kadran Deploy and add the
repository. No reconnect is needed: Kadran looks at the list again within a minute.

If the error has no such hint, check `kadran github status`. A server that is not connected, or
whose `kadran-git` is down, treats the repository as public, so Docker fails with a "not found"
or "authentication" error.

If the repository is listed as private and the download fails, the build fails with the real
cause. Kadran never falls back to an anonymous build for a private repository.

Design and measurements: [K-167 in `docs/decisions.md`](../decisions.md).
