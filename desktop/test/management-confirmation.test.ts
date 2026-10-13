import assert from "node:assert/strict";
import { test } from "node:test";
import { managementConfirmation, type ConfirmRequest } from "../src/main/management-confirmation.ts";

const REQUESTS: ConfirmRequest[] = [
  { kind: "deploy", target: "user@host", app: "blog", ref: "main" },
  { kind: "rollback", target: "user@host", app: "blog" },
  { kind: "delete", target: "user@host", app: "blog" },
  { kind: "backup", target: "user@host" },
  { kind: "update", target: "user@host", app: "blog", update: { domain: "x.example.com" } },
  { kind: "create", target: "user@host", spec: { app_id: "blog", git_host: "github.com", git_owner: "a", git_repo: "b", git_branch: "main", container_port: 3000 } },
];

test("native confirmation follows the saved language and defaults to cancellation", () => {
  for (const request of REQUESTS) {
    const tr = managementConfirmation("tr", request);
    const en = managementConfirmation("en", request);
    assert.equal(tr.buttons[0], "Vazgeç");
    assert.equal(en.buttons[0], "Cancel");
    assert.ok(tr.detail.startsWith("Sunucu: user@host"));
    assert.ok(en.detail.startsWith("Server: user@host"));
    assert.notEqual(tr.buttons[1], en.buttons[1]);
    assert.equal(tr.defaultId, 0);
    assert.equal(tr.cancelId, 0);
    assert.equal(en.defaultId, 0);
    assert.equal(en.cancelId, 0);
  }
});

test("dialog values cannot re-expand placeholders or hide invisible characters", () => {
  const out = managementConfirmation("en", { kind: "deploy", target: "user@host", app: "blog", ref: "{app}‮" });
  assert.ok(out.detail.includes("Ref: {app}\\u202e"));
  assert.equal(out.detail.includes("‮"), false);
});

test("long lists are truncated with a count instead of growing the dialog", () => {
  const env = Object.fromEntries(Array.from({ length: 30 }, (_, i) => [`KEY_${i}`, "v"]));
  const out = managementConfirmation("en", { kind: "update", target: "t", app: "blog", update: { env } });
  assert.ok(out.detail.includes("and 20 more"));
  assert.equal(out.detail.includes("KEY_29"), false);
});

test("create dialog shows replicas, limits, health path and build-argument NAMES (never values)", () => {
  const spec = {
    app_id: "blog", git_host: "github.com", git_owner: "a", git_repo: "b", git_branch: "main", container_port: 3000,
    replicas: 64, health_path: "/healthz", limits: { memory_bytes: "8589934592", cpu_millis: 4000, blkio_weight: 500 },
    build_args: { NPM_TOKEN: "build-secret-7c1" },
  };
  const en = managementConfirmation("en", { kind: "create", target: "user@host", spec }).detail;
  assert.ok(en.includes("Replicas: 64"), en);
  assert.ok(en.includes("Health path: /healthz"), en);
  assert.ok(en.includes("Limits: memory 8192 MiB, CPU 4000 millicores, I/O weight 500"), en);
  assert.ok(en.includes("Build arguments set (values are not shown): NPM_TOKEN"), en);
  assert.equal(en.includes("build-secret-7c1"), false);
  const tr = managementConfirmation("tr", { kind: "create", target: "user@host", spec }).detail;
  assert.ok(tr.includes("Kopya sayısı: 64"), tr);
  assert.equal(tr.includes("build-secret-7c1"), false);
  const bare = managementConfirmation("en", { kind: "create", target: "user@host", spec: { ...spec, health_path: "", build_args: {} } }).detail;
  assert.ok(bare.includes("Health path: none"), bare);
  assert.equal(bare.includes("Build arguments"), false);
});

test("create dialog shows the limit the validator accepted, in every accepted form (JSON number or decimal text)", () => {
  // Doğrulayıcı (uint64Positive) bellek sınırını JSON sayısı olarak da kabul eder;
  // kutu onu boş gösterirse onaylanan ile gönderilen ayrışır.
  const base = { app_id: "blog", git_host: "github.com", git_owner: "a", git_repo: "b", git_branch: "main", container_port: 3000, replicas: 1 };
  const asNumber = managementConfirmation("en", { kind: "create", target: "user@host", spec: { ...base, limits: { memory_bytes: 9007199254740991, cpu_millis: "0100", blkio_weight: 10 } } }).detail;
  assert.ok(asNumber.includes("Limits: memory 9007199254740991 B, CPU 100 millicores, I/O weight 10"), asNumber);
  const asText = managementConfirmation("en", { kind: "create", target: "user@host", spec: { ...base, replicas: "064", limits: { memory_bytes: "1048576", cpu_millis: 1, blkio_weight: 10 } } }).detail;
  assert.ok(asText.includes("Limits: memory 1 MiB, CPU 1 millicores"), asText);
  assert.ok(asText.includes("Replicas: 64"), asText);
});

const cp = (...points: number[]) => String.fromCodePoint(...points);
const BACKSLASH = "\\";

test("a long target keeps its host visible (the host is at the tail of user@host)", () => {
  const target = `${"u".repeat(400)}@prod.example.com`;
  const out = managementConfirmation("en", { kind: "backup", target });
  const line = out.detail.split("\n")[0]!;
  assert.ok(line.endsWith("@prod.example.com"), line);
  assert.ok(line.includes("…"));
  assert.ok(line.length <= "Server: ".length + 201, `line is ${line.length} characters`);
});

test("escaping happens before truncation: a value of invisible characters stays bounded", () => {
  const out = managementConfirmation("en", { kind: "deploy", target: "user@host", app: "blog", ref: cp(0x202e).repeat(500) });
  const line = out.detail.split("\n").find((l) => l.startsWith("Ref: "))!;
  assert.ok(line.length <= "Ref: ".length + 201, `line is ${line.length} characters`);
  // Her kaçış tam olmalı: "…" iki yanında yalnız eksiksiz U+202E kaçış dizileri.
  for (const side of line.slice("Ref: ".length).split("…")) {
    assert.equal(side.split(`${BACKSLASH}u202e`).join(""), "", `an escape sequence was cut: ${side.slice(-8)}`);
  }
});

test("blank-looking letters, combining marks and unassigned code points are shown as escapes", () => {
  // Hedef yalnız Cc/Cf'yi reddeder: U+3164 ile host'u boş gösteren bir hedef kutuda görünmeli.
  const target = `user@${cp(0x3164)}${cp(0x2800)}${cp(0x115f)}e${cp(0x301)}${cp(0xe000)}${cp(0x378)}`;
  const out = managementConfirmation("en", { kind: "backup", target });
  const esc = (hex: string) => `${BACKSLASH}u${hex}`;
  const want = `user@${esc("3164")}${esc("2800")}${esc("115f")}e${esc("0301")}${esc("e000")}${esc("0378")}`;
  assert.ok(out.detail.startsWith(`Server: ${want}`), out.detail);
});

test("a literal ellipsis inside a value is escaped, so only real truncation shows …", () => {
  const out = managementConfirmation("en", { kind: "backup", target: `user@a${cp(0x2026)}b` });
  assert.ok(out.detail.startsWith(`Server: user@a${BACKSLASH}u2026b`), out.detail);
});

test("line and paragraph separators and unusual spaces are shown as escapes", () => {
  const ref = `a${cp(0x2028)}b${cp(0x2029)}c${cp(0xa0)}d${cp(0x3000)}e f`;
  const out = managementConfirmation("en", { kind: "deploy", target: "user@host", app: "blog", ref });
  const want = ["a", "u2028b", "u2029c", "u00a0d", "u3000e f"].join(BACKSLASH);
  assert.ok(out.detail.includes(`Ref: ${want}`), out.detail);
});
