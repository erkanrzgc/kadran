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
  // Her kaçış tam olmalı: "…" iki yanında yalnız eksiksiz ‮ dizileri.
  for (const side of line.slice("Ref: ".length).split("…")) {
    assert.equal(side.split(`${BACKSLASH}u202e`).join(""), "", `an escape sequence was cut: ${side.slice(-8)}`);
  }
});

test("line and paragraph separators and unusual spaces are shown as escapes", () => {
  const ref = `a${cp(0x2028)}b${cp(0x2029)}c${cp(0xa0)}d${cp(0x3000)}e f`;
  const out = managementConfirmation("en", { kind: "deploy", target: "user@host", app: "blog", ref });
  const want = ["a", "u2028b", "u2029c", "u00a0d", "u3000e f"].join(BACKSLASH);
  assert.ok(out.detail.includes(`Ref: ${want}`), out.detail);
});
