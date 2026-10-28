# Two-step sign-in (TOTP)

With TOTP on, an admin SSH key alone can no longer manage the server. Admin commands also need a
session token, and you get one by typing a code from an authenticator app. A token lasts 8 hours
and only works with the SSH key that signed in.

TOTP is off until you turn it on. Upgrading changes nothing for you or for CI.

## What it covers

| | With TOTP on |
|---|---|
| Admin key (`kadran-client`), workstation or desktop | Needs a session: `kadran login` |
| Deploy-only key (CI, `kadran key add -deploy`) | Unchanged, no code needed |
| Admin commands on the server itself (local socket) | Refused. Use `kadran` over SSH from your workstation |
| Root on the server | Can always turn TOTP off (`kadran totp reset`) |

TOTP protects against a stolen admin key. It does not protect against someone who already has
root on the server.

## Turn it on

You need a server and a client that support TOTP. If either is too old, `kadran totp enable`
says `this server does not support TOTP; upgrade it with kadran bootstrap`.

```bash
kadran totp enable kadran-client@your-server
```

```
add this server to your authenticator app (confirm before 2026-10-28 14:10 +03):
  1. scan this QR code with the app:

  <QR code>

     if it does not scan (it is drawn for a dark terminal), add an account
     by hand, time based, with this key:
       JBSWY3DPEHPK3PXP...
     link: otpauth://totp/Kadran:<server hostname>?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=...
  2. type the 6-digit code the app shows (not the key)
6-digit code from the app:
```

1. Scan the QR code with an authenticator app (Aegis, Google Authenticator, 1Password, ...). The
   QR is drawn for a dark terminal. On a light terminal, or if it does not scan, add the account
   by hand: time based, with the key in one piece (some apps reject a key with spaces).
2. Type the 6-digit code the app shows, not the key. You have 10 minutes. After that, run
   `enable` again. If what you type is not 6 digits, kadran says what it looks like (the setup
   key, a recovery code) and asks again, up to 3 times. Such input is never sent to the server,
   so it does not count toward the lock.
3. Kadran prints **10 recovery codes once**. Store them offline (on paper, or in a password
   manager that is not on this machine). Each code works once.

You are now signed in for 8 hours.

## Daily use

```bash
kadran login kadran-client@your-server
6-digit code from the app: 123456
signed in until 2026-10-28 22:10 +03
```

The code is read from the terminal and never passed as an argument, so it does not end up in
your shell history. Commands run without asking until the token expires. After that you get:

```
kadran: ... session expired: sign in again with "kadran login"
  run: kadran login kadran-client@your-server
```

The desktop app uses the same saved session. Sign in with the CLI and the desktop picks it up on
its next call, without a restart.

The token is saved per server under your user config directory
(`%AppData%\kadran\sessions` on Windows, `~/.config/kadran/sessions` on Linux), readable only by
you. `kadran logout <server>` deletes it from this machine. The server has no list of tokens, so
a token copied before the logout stays valid until it expires. To invalidate every token at
once, re-enroll or turn TOTP off.

## Recovery codes

If your phone is gone:

```bash
kadran login -recovery kadran-client@your-server
recovery code: ABCDE-FGHIJ
```

Case, dashes and spaces do not matter. Every use raises its own warning alarm
(`login_recovery`), because the phone may be lost or the codes may have leaked. These alarms close
when you re-enroll or turn TOTP off. `kadran totp status` shows how many codes are left and warns
when 3 or fewer remain.

To set up a new phone without the old one, re-enroll with a recovery code:

```bash
kadran totp enable -recovery kadran-client@your-server
recovery code: KLMNO-PQRST
```

This uses up a second recovery code, and enrolling gives you 10 new ones.

## Re-enroll (new phone, new codes)

```bash
kadran totp enable kadran-client@your-server
TOTP is already on: replacing it needs a code from your current authenticator
current 6-digit code from the app: 123456
```

Re-enrolling needs a valid session **and** a current code (or a recovery code, with `-recovery`),
so a stolen token cannot swap in a new secret. The old secret keeps working until you confirm the
new one. Confirming also replaces the key that signs session tokens. Every token issued before it
is rejected with "session expired", and an old copy of the database can no longer be used to
create new ones.

## Wrong codes and lockout

After 5 wrong codes in a row, that SSH key is locked for 1 minute. Each further wrong code doubles
the lock, up to 1 hour. While a key is locked, even the right code is refused, and the message
says how long to wait. A lock raises a warning alarm (`login_lockout`). A successful sign-in closes
it, and so does turning TOTP off or resetting it. `kadran totp status` lists locked keys. Recovery codes, enrollment confirmation codes
and the codes asked by `disable` and re-enroll count toward the same lock.

Every wrong TOTP code gets the same message:

```
code rejected: check that the code is current and your phone's clock is correct
```

Spaces in a code do not matter (`123 456` works). The phone and the server must agree on the time
within about 30 seconds. The audit log records
whether a rejected code was close in time (`clock`) or simply wrong (`wrong_code`). The caller is
not told, since that would reveal that a guessed code is about to become valid.

## Turn it off

```bash
kadran totp disable kadran-client@your-server
6-digit code from the app: 123456
TOTP is off
```

Turning TOTP off needs a valid session and a fresh code, so a stolen token cannot do it. Without
the phone, use `kadran totp disable -recovery` and a recovery code. Turning it off also closes the
lock and recovery alarms.

## Lost phone and lost recovery codes

Only root on the server can help now. `totp reset` uses the same root path as `bootstrap` and
`kadran key`: SSH as root, or as a user with **passwordless** sudo (`-sudo` never asks for a
password).

```bash
kadran totp reset root@your-server
kadran totp reset -sudo you@your-server
```

```
TOTP disabled; every session is invalid
turn it back on with kadran totp enable (new secret, new recovery codes)
```

On the server, this runs `kadrand -totp-reset` as the `kadran` user. It deletes the secret, the
recovery codes, the signing keys and the lock counters, closes the lock and recovery alarms, and
writes a `totp.reset` audit record. The daemon keeps running, and the change takes effect on the
next call.

## Where the secret lives

The TOTP secret is stored in plain form in the daemon's database (`/var/lib/kadran/kadran.db`).
The daemon has to read it to check codes, and it can seal values but not open them (K-123). So
whoever can read that database, or a local backup of it (`0600`, owned by `kadran`), can generate
codes. Offsite backups are encrypted with `age` (K-098). The secret, codes, recovery codes and
tokens never go into audit records, logs or alarm texts.

## Messages

| Message | Meaning |
|---|---|
| `TOTP is on for this server: sign in with "kadran login" ...` | No token for this server. Run `kadran login` |
| `session expired: sign in again ...` | The token expired, or TOTP was re-enrolled. Sign in again |
| `invalid session token: ...` | The token is damaged or belongs to another SSH key. Sign in again |
| `TOTP is on for this server: admin commands need an SSH key identity ...` | You are on the server's local socket. Use `kadran` over SSH |
| `too many failed codes for this key: try again in 2m0s` | Locked. Wait, or use another admin key |
| `code already used: wait for the next one` | Each code works once. Wait for the next 30-second code |
| `this server does not support TOTP; upgrade it with kadran bootstrap` | The server is older than the client |
| `warning: saved session is damaged ...` | The token file is unreadable. `kadran login` rewrites it |
| `that is the setup key, which goes into the app ...` | You typed the key at the code prompt. Type the app's 6-digit code |
| `that looks like a recovery code: ... -recovery` | Run the same command with `-recovery` to use it |
| `a code is 6 digits ...` | Type the number the app shows. Nothing was sent to the server |

Design and measurements: [K-155 and K-156 in `docs/decisions.md`](../decisions.md).
