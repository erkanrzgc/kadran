# Security Policy

Kadran's entire reason for existing is a security property, so security reports
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

> **[Report a vulnerability](https://github.com/erkanrzgc/kadran/security/advisories/new)**

If that is unavailable to you, email **benerkanrzgc@gmail.com** with `KADRAN SECURITY`
in the subject line.

### What to include

The more of this you can provide, the faster it gets fixed:

- The version or commit SHA you tested
- Which component is affected — `kadrand`, `kadran-exec`, `kadran-connect`, the CLI,
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

Kadran's security model rests on specific, testable boundaries. Anything that
crosses one of these is in scope and will be treated as high severity:

1. **`kadrand` reaching Docker or root.** The daemon runs as an unprivileged user,
   is not in the `docker` group, and has an empty capability bounding set. Any path
   by which it obtains privileged capability is the most severe class in this
   project.
2. **Escaping the executor's schema.** `proto/kadran/v1/exec.proto` is the whitelist.
   If a request can reach a privileged operation that the schema was supposed to make
   unrepresentable — a container Kadran does not manage, an arbitrary image, a host
   path, a free-form argv — that is in scope.
3. **Bypassing `kadran-connect`.** The forced command plus `restrict` should make the
   client key incapable of anything but running that one binary. A shell, a tunnel,
   or a second command is in scope.
4. **Forging or breaking the audit chain.** Writing a record attributed to another
   actor, deleting a record without `kadran audit verify` detecting it (other than
   the known cross-chain gap listed below), or forging the SSH fingerprint carried
   in the connection preamble.
5. **Reading environment values or build arguments** from the audit log (both are
   written as `[REDACTED]`) or from process memory.
6. **Cross-application escape** — one deployed app reaching another app's volumes,
   network, or environment.
7. **A deploy key exceeding its scope.** A key whose `authorized_keys` line carries
   `kadran-connect -deploy=<apps>` should reach only `Ping` and `Deploy`, and
   `Deploy` only for those apps. Calling any other RPC, deploying an app outside
   the list, or a malformed line falling back to admin rights is in scope (K-131).

## Explicitly out of scope

These are known and documented limitations, not undisclosed weaknesses:

- **There is no secret store yet.** Environment variables are stored in the
  daemon's database (`0600`, owned by `kadran`) and are visible to `docker inspect`
  on the host. The mitigation is that only the executor can reach Docker. This
  boundary is documented, not hidden.
- **The two audit chains are not compared with each other.** Each is verified on
  its own, so a compromised `kadrand` that drops records from *its own* chain is
  not detected. Closing this needs the executor to return its record hash
  (`docs/decisions.md`, K-079). Tampering with the *executor's* chain remains in
  scope.
- **Root on the server can do anything.** Kadran defends against a compromised
  *panel*, not against an attacker who already holds root.
- **Anyone in the `kadran-client` group can talk to `api.sock`.** That is the
  design; group membership is the authorization boundary and is set up by
  `bootstrap`. Note the two groups are distinct and the distinction is
  load-bearing: `kadran-client` reaches `api.sock`, while `exec.sock` sits in a
  `0750 root:kadran` directory the client group cannot traverse at all.
  Membership must be the user's *primary* group — `SO_PEERCRED` reports
  only that, so adding a second admin with `usermod -aG` yields a silent
  denial rather than access.
- **A deploy key controls the apps in its scope.** The code it deploys reads that
  app's environment and volumes, and a Dockerfile can print build arguments into
  the streamed build log. The scope protects other apps and administrative
  actions, not the secrets of the apps the key may deploy. `kadrand` has no
  outbound network, so it cannot check which branch a commit is on: a deploy key
  can deploy any commit the configured repository serves, including commits from
  open pull requests. Use it only in workflows triggered by pushes to your default
  branch, never in `pull_request_target`.
- **A malicious operator.** Kadran produces a tamper-evident audit trail; it does
  not prevent an authorized human from taking authorized destructive actions.
- Denial of service by resource exhaustion from a legitimately deployed app.
- Findings from automated scanners with no demonstrated impact.

## Disclosure

Coordinated disclosure. Once a fix is available, the advisory is published with
credit to the reporter unless you prefer to remain anonymous. There is no bug
bounty — this is an unfunded open-source project — but every valid report is
credited in the advisory and in `docs/decisions.md` alongside the measurement that
proved the fix.
