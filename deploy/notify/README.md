# Alarm delivery (Telegram)

kadrand detects alarms but cannot send them anywhere: it runs with
`IPAddressDeny=any`, on purpose. This unit reads the alarms from kadrand's
journal and forwards them to Telegram. It also reports when a core service
(kadrand, kadran-exec, kadran-caddy) crashes or stops. Details and
measurements: `docs/decisions.md`, K-108 and K-110.

## What gets sent

| event | source |
|---|---|
| 🔴 CRITICAL / 🟡 WARNING opened | kadrand `msg=ALARM state=opened` |
| 🔴 WORSE | `state=escalated` |
| ✅ RESOLVED | `state=closed` |
| 🔴 UNIT FAILED | `OnFailure=` — currently `kadran-offsite.service` |
| 🟠 CRASHED | a core service crashed (systemd's "Failed with result" event) |
| 🔴 CRASH LOOP | still crashing at the next check; no new message while the loop lasts |
| 🔴 NOT RUNNING | no process for two checks in a row (`exit 0`, `stop`, gave up) |
| ✅ RECOVERED / RUNNING AGAIN | the crash or the stop is over |

Delay is at most ~1 minute. If sending fails, the event is NOT lost; the
next run tries again.

Alarm lines written before K-150 used Turkish keys (`durum=acildi` and so
on). The sender still reads them, so events written just before an upgrade
are delivered too.

Service messages carry only systemd's fields (result: `signal`,
`exit-code`, `oom-kill`…). The service's own output, such as a panic
message, is not sent to Telegram.

## Security model

- The sender is NOT `kadran`; it is a temporary user for each run
  (`DynamicUser`).
- The token file is `0600 root:root`. systemd reads it as root and hands it
  only to this unit (`LoadCredential`). **kadrand cannot read the token**;
  even a compromised kadrand cannot send messages in your name.
- Network: outbound only. The local network is closed, with the DNS resolver
  as the only exception (K-107).
- ⚠ The sender can read the whole system journal (`systemd-journal` group).
  A deliberate trade-off; the reasoning is in the unit file.

## Setup

### 1. Create a bot (in Telegram)

1. Message **@BotFather** in Telegram → `/newbot` → give it a name.
2. Copy the token it gives you (`123456789:ABC…`). **Do not share it.**
3. Send your new bot any message from Telegram (for example `hello`).
   A bot can only message you after you have written to it first.

### 2. Put the token on the server (root)

```bash
sudo install -m 0600 -o root -g root /dev/null /etc/kadran/notify.conf
sudo nano /etc/kadran/notify.conf
```

```ini
TELEGRAM_TOKEN=123456789:ABC…
TELEGRAM_CHAT_ID=
```

### 3. Find the chat id

```bash
sudo /usr/local/lib/kadran/notify/kadran-notify.sh sohbet-bul
```

Put the number from `chat id: 123456789` on the `TELEGRAM_CHAT_ID=` line.

### 4. Test it — with the real unit

```bash
sudo systemctl start kadran-notify-failure@test.service
```

A "UNIT FAILED — test" message should arrive in Telegram. This is an
end-to-end test under the real unit's restrictions (temporary user,
credential, network).

### 5. Turn it on

```bash
sudo systemctl enable --now kadran-notify.timer
```

The first run sets the cursor to "now"; alarms from before the setup are not
sent.

## Known limits

- **If the sender itself goes down**, nothing on the server catches it. The
  external heartbeat check ([`deploy/nabiz`](../nabiz/README.md), K-109)
  reports when it has heard nothing for 15 minutes.
- **A hang is caught only in kadrand.** kadrand has `WatchdogSec` (K-115) and
  systemd restarts it when it stops reporting liveness; that restart is sent
  as 🟠 CRASHED (`watchdog`). kadran-exec and kadran-caddy have no watchdog:
  a hung process keeps running and looks "running".
- Stopping a core service for maintenance for more than two minutes also
  produces a "🔴 NOT RUNNING" message. That is correct: the service really is
  not running.
- `kadran bootstrap` does not set this up (like the off-site backup). Once it
  is installed, `bootstrap` (since v0.5.0) refreshes the script and the units
  on every upgrade and leaves the timer's enabled/disabled state alone (K-138).
