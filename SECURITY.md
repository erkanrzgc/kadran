# Security Policy

Panely's entire reason for existing is a security property, so security reports
are treated as first-class work, not as an interruption.

## Supported versions

| Version | Supported |
|---|---|
| `main` | ✅ |
| tagged releases | ❌ — none yet; the project is pre-release |

Until a `v1.0.0` tag exists, only `main` receives fixes.

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Use GitHub's private reporting:

> **[Report a vulnerability](https://github.com/erkanrzgc/panely/security/advisories/new)**

If that is unavailable to you, email **benerkanrzgc@gmail.com** with `PANELY SECURITY`
in the subject line.

### What to include

The more of this you can provide, the faster it gets fixed:

- The version or commit SHA you tested
- Which component is affected — `panelyd`, `panely-exec`, `panely-connect`, the CLI,
  the Electron app, the systemd units, or the bootstrap path
- A concrete reproduction: exact request, command, or `authorized_keys` line
- What boundary you believe it crosses (see below)
- Whether you needed prior access, and at what privilege level

### Response

- **Acknowledgement:** within 72 hours
- **Initial assessment:** within 7 days
- **Fix or documented mitigation:** target 30 days for anything crossing a
  privilege boundary

This is a single-maintainer project, so these are honest targets rather than a
contractual SLA. You will get a real answer, including "this is not a
vulnerability, and here is why."

## What counts as a vulnerability

Panely's security model rests on specific, testable boundaries. Anything that
crosses one of these is in scope and will be treated as high severity:

1. **`panelyd` reaching Docker or root.** The daemon runs as an unprivileged user,
   is not in the `docker` group, and has an empty capability bounding set. Any path
   by which it obtains privileged capability is the most severe class in this
   project.
2. **Escaping the executor's schema.** `proto/panely/v1/exec.proto` is the whitelist.
   If a request can reach a privileged operation that the schema was supposed to make
   unrepresentable — a container Panely does not manage, an arbitrary image, a host
   path, a free-form argv — that is in scope.
3. **Bypassing `panely-connect`.** The forced command plus `restrict` should make the
   client key incapable of anything but running that one binary. A shell, a tunnel,
   or a second command is in scope.
4. **Forging or breaking the audit chain.** Writing a record attributed to another
   actor, deleting a record without `panely audit verify` detecting it, or forging the
   SSH fingerprint carried in the connection preamble.
5. **Reading secrets** from the vault, from process memory, or from the audit log
   (which redacts vault fields before writing).
6. **Cross-application escape** — one deployed app reaching another app's volumes,
   network, or environment.

## Explicitly out of scope

These are known and documented limitations, not undisclosed weaknesses:

- **Container environment variables are visible to `docker inspect`.** The vault
  protects secrets at rest, not at container runtime. The mitigation is that only
  the executor can reach Docker. This boundary is documented, not hidden.
- **Root on the server can do anything.** Panely defends against a compromised
  *panel*, not against an attacker who already holds root.
- **Anyone in the `panely` group can talk to `api.sock`.** That is the design;
  group membership is the authorization boundary and is set up by `bootstrap`.
- **A malicious operator.** Panely produces a tamper-evident audit trail; it does
  not prevent an authorized human from taking authorized destructive actions.
- Denial of service by resource exhaustion from a legitimately deployed app.
- Findings from automated scanners with no demonstrated impact.

## Disclosure

Coordinated disclosure. Once a fix is available, the advisory is published with
credit to the reporter unless you prefer to remain anonymous. There is no bug
bounty — this is an unfunded open-source project — but every valid report is
credited in the advisory and in `docs/decisions.md` alongside the measurement that
proved the fix.
