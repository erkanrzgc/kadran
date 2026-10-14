# External heartbeat check (Cloudflare Worker)

The alarm sender (`deploy/notify`) runs on the server. If it stops, or the
whole server goes down, nothing is left on the server to report it. This
Worker writes to Telegram when it has heard nothing from the server for 15
minutes. Details: K-109.

```
server  kadran-notify (every minute)  ──POST /ping, about every 5 min──►  Worker ─► KV
Worker  check every 5 min: last heartbeat > 15 min?  ──►  Telegram
```

Free tier: ~320 KV writes a day (measured; the limit is 1000), 1 scheduled
trigger (the limit is 5).

## Setup

You need Node.js and a Cloudflare account. Run the commands in this
directory.

### 1. Log in and deploy (on your own computer)

```bash
cd deploy/nabiz
npx wrangler login
npx wrangler deploy
```

`deploy` creates the KV namespace itself and writes its id into
`wrangler.jsonc`. **Do not commit that change** — the id belongs to your
account. Note the address in the output:
`https://kadran-nabiz.<subdomain>.workers.dev`.

### 2. Secrets

Generate the heartbeat token and copy it somewhere:

```bash
openssl rand -hex 32
```

Then give the Worker three values (each command asks you to paste the value):

```bash
npx wrangler secret put PING_TOKEN          # the string you just generated
npx wrangler secret put TELEGRAM_TOKEN      # bot token
npx wrangler secret put TELEGRAM_CHAT_ID    # chat id
```

### 3. Heartbeat settings on the server (root)

Add two lines to `/etc/kadran/notify.conf`:

```ini
HEARTBEAT_URL=https://kadran-nabiz.<subdomain>.workers.dev/ping
HEARTBEAT_TOKEN=<the same PING_TOKEN>
```

The first heartbeat goes out within a minute.

> **If you deployed it as `panely-nabiz` before v0.4.0 (K-136):** the server
> migration does not touch the Worker; the address in `notify.conf` keeps
> pointing at the old Worker and everything works. You do not have to rename
> it. Running `wrangler deploy` from this directory creates a NEW Worker named
> `kadran-nabiz` with a new address; the old one keeps running on its
> schedule. If you move, the order matters, or the old Worker gets no
> heartbeat and sends a false "NO HEARTBEAT":
>
> 1. `npx wrangler deploy`, then enter the three secrets above on the NEW
>    Worker (same KV, the id in `wrangler.jsonc`; the state is kept).
> 2. On the server, point `HEARTBEAT_URL` at the new address and wait a minute.
> 3. Delete the old one: `npx wrangler delete --name panely-nabiz`.
>
> Messages are in English since K-150. A Worker deployed before that keeps
> sending the old Turkish texts until you run `npx wrangler deploy` again.

## Verification

A control group is required: only seeing "the alarm arrived" does not prove
the Worker can tell the difference.

1. **Silence:** while the sender is running, NO message should arrive for at
   least one check round (5 min).
2. **Alarm:** `systemctl stop kadran-notify.timer` → "🔴 NO HEARTBEAT" should
   arrive within ~20 min at most.
3. **Recovery:** `systemctl start kadran-notify.timer` → "✅ HEARTBEAT BACK".

Measured (25 Sep, K-109): 🔴 17 min after the last heartbeat, ✅ on the first
round after the timer was started. Each state change produced one message,
and the rounds below the threshold stayed silent.

## Known limits

- The heartbeat is sent when the alarm sender runs SUCCESSFULLY. If the
  sender fails because Telegram is unreachable, the heartbeat stops too and the
  Worker raises an alarm — and if the Worker cannot reach Telegram either, that
  message does not go out.
- If the Worker itself stops (a Cloudflare outage, an account problem), nobody
  hears about it. It is the last link in the chain.
