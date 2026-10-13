import assert from "node:assert/strict";
import { test } from "node:test";

import { canDeleteApp, confirmationMatches, createRequestScope, draftToSpec, editableUpdate, newAppDraft, specToDraft, validateAppDraft } from "../src/renderer/lib/management.ts";

function validDraft() {
  return { ...newAppDraft(), appId: "hello", gitOwner: "team", gitRepo: "site" };
}

test("create form requires a valid identity, source and bounded resources", () => {
  assert.equal(validateAppDraft(validDraft()), null);
  assert.equal(validateAppDraft({ ...validDraft(), appId: "Hello;rm" }), "form.invalid_id");
  assert.equal(validateAppDraft({ ...validDraft(), gitHost: "https://token@github.com" }), "form.invalid_source");
  assert.equal(validateAppDraft({ ...validDraft(), memoryMiB: "0" }), "form.invalid_limits");
  assert.equal(validateAppDraft({ ...validDraft(), memoryMiB: "5" }), "form.invalid_limits");
  assert.equal(validateAppDraft({ ...validDraft(), memoryMiB: "6", cpuMillis: "10" }), null);
  assert.equal(validateAppDraft({ ...validDraft(), cpuMillis: "9" }), "form.invalid_limits");
  assert.equal(validateAppDraft({ ...validDraft(), replicas: "1.5" }), "form.invalid_replicas");
  assert.equal(validateAppDraft({ ...validDraft(), replicas: "64" }), null);
  assert.equal(validateAppDraft({ ...validDraft(), replicas: "65" }), "form.invalid_replicas");
  assert.equal(validateAppDraft({ ...validDraft(), containerPort: "65536" }), "form.invalid_port");
  assert.equal(validateAppDraft({ ...validDraft(), dockerfilePath: "../Dockerfile" }), "form.invalid_dockerfile");
});

test("editing leaves valid immutable source and exact non-MiB resource limits untouched", () => {
  const original = draftToSpec(validDraft());
  original.limits.memory_bytes = "100000001";
  original.replicas = 64;
  const draft = { ...specToDraft(original), domain: "new.example.com" };
  assert.equal(validateAppDraft(draft, true), null);
  const update = editableUpdate(draft, original)!;
  assert.equal(update.domain, "new.example.com");
  assert.equal(Object.hasOwn(update, "replicas"), false); // değişmeyen alan gönderilmez
  assert.equal(Object.hasOwn(update, "limits"), false);
  assert.equal(original.limits.memory_bytes, "100000001");
});

test("form preserves explicit empty environment values and rejects duplicate or unsafe volume paths", () => {
  const draft = { ...validDraft(), envText: "MODE=production\nEMPTY=\nVALUE=a=b", volumesText: "data=/app/data\ncache=/app/cache:ro" };
  assert.equal(validateAppDraft(draft), null);
  const spec = draftToSpec(draft);
  assert.deepEqual(spec.env, { MODE: "production", EMPTY: "", VALUE: "a=b" });
  assert.deepEqual(spec.volumes, [{ name: "data", mount_path: "/app/data", read_only: false }, { name: "cache", mount_path: "/app/cache", read_only: true }]);
  assert.equal(spec.limits.memory_bytes, "536870912");
  assert.equal(validateAppDraft({ ...draft, envText: "MODE=x\nMODE=y" }), "form.invalid_env");
  assert.equal(validateAppDraft({ ...draft, volumesText: "data=/proc/private" }), "form.invalid_volumes");
  assert.equal(validateAppDraft({ ...draft, volumesText: "data=/app\ncache=/app/cache" }), "form.invalid_volumes");
});

test("updates express removals while preserving immutable source, port and limits", () => {
  const original = draftToSpec({ ...validDraft(), envText: "OLD=x\nMODE=old", volumesText: "data=/data" });
  const next = { ...validDraft(), domain: "", envText: "MODE=new", envRemoveText: "OLD", volumesText: "" };
  assert.deepEqual(editableUpdate(next, original), { env: { MODE: "new" }, env_remove: ["OLD"], volume_remove: ["data"] });
  // Alan adı da kaldırılıyorsa boş değer açıkça gönderilir.
  assert.deepEqual(editableUpdate({ ...next, domain: "" }, { ...original, domain: "a.example.com" })?.domain, "");
});

test("redacted environment names remain unchanged until explicit replacement or removal", () => {
  const original = draftToSpec(validDraft());
  original.env = { SECRET: "", MODE: "" }; // Actual API returns names only.
  const draft = specToDraft(original);
  const domainOnly = editableUpdate({ ...draft, domain: "new.example.com" }, original)!;
  assert.deepEqual(domainOnly, { domain: "new.example.com" });
  const replacement = editableUpdate({ ...draft, envText: "MODE=", envRemoveText: "SECRET" }, original)!;
  assert.deepEqual(replacement.env, { MODE: "" });
  assert.deepEqual(replacement.env_remove, ["SECRET"]);
  assert.equal(validateAppDraft({ ...draft, envText: "MODE=", envRemoveText: "MODE" }), "form.invalid_env_remove");
});

test("destructive confirmation is exact and unknown/live release prevents deletion", () => {
  assert.equal(confirmationMatches("hello", "hello"), true);
  assert.equal(confirmationMatches("hello", " Hello "), false);
  assert.equal(canDeleteApp({ active_release_id: "" }), true);
  assert.equal(canDeleteApp({ active_release_id: "r3" }), false);
  assert.equal(canDeleteApp({}), false);
});

test("request scope discards old-server responses and errors without reporting success", async () => {
  const scope = createRequestScope();
  let finish!: (value: string) => void;
  const old = scope.run(() => new Promise<string>((resolve) => { finish = resolve; }));
  scope.invalidate();
  const fresh = await scope.run(async () => "new server");
  finish("old server");
  assert.deepEqual(await old, { status: "discarded" });
  assert.deepEqual(fresh, { status: "success", value: "new server" });
  const failure = await scope.run(async () => { throw new Error("health gate failed"); });
  assert.equal(failure.status, "error");
  if (failure.status === "error") assert.equal(failure.error.message, "health gate failed");
});

test("newer reads win when an earlier request settles last", async () => {
  const scope = createRequestScope();
  let finish!: (value: string) => void;
  const old = scope.run(() => new Promise<string>((resolve) => { finish = resolve; }));
  await scope.run(async () => "latest");
  finish("obsolete");
  assert.deepEqual(await old, { status: "discarded" });
});

function specWith(volumes: Array<{ name: string; mount_path: string; read_only: boolean }>) {
  return { ...draftToSpec({ ...validDraft(), domain: "a.example.com" }), volumes };
}

test("hiçbir şey değişmediyse güncelleme gönderilmez", () => {
  const original = specWith([{ name: "data", mount_path: "/data", read_only: true }]);
  assert.equal(editableUpdate(specToDraft(original), original), null);
});

test("hacim metni düzenlenmediyse volumes ve volume_remove hiç gönderilmez", () => {
  const original = specWith([{ name: "data", mount_path: "/data", read_only: true }]);
  const update = editableUpdate({ ...specToDraft(original), replicas: "2" }, original)!;
  assert.deepEqual(update, { replicas: 2 });
});

test("hacim düzenlenince yalnız eklenen/değişenler ve kaldırılanlar gider", () => {
  const original = specWith([{ name: "keep", mount_path: "/keep", read_only: false }, { name: "ro", mount_path: "/ro", read_only: false }, { name: "gone", mount_path: "/gone", read_only: false }]);
  const draft = { ...specToDraft(original), volumesText: "keep=/keep\nro=/ro:ro\nnew=/new" };
  assert.deepEqual(editableUpdate(draft, original), {
    volumes: [{ name: "ro", mount_path: "/ro", read_only: true }, { name: "new", mount_path: "/new", read_only: false }],
    volume_remove: ["gone"],
  });
});

test("yalnız boşluk satırı eklemek hacimlerde değişiklik sayılmaz", () => {
  const original = specWith([{ name: "data", mount_path: "/data", read_only: false }]);
  assert.equal(editableUpdate({ ...specToDraft(original), volumesText: "data=/data\n\n" }, original), null);
});

test("yalnız değişen skalerler gider", () => {
  const original = specWith([]);
  const draft = { ...specToDraft(original), gitBranch: "dev", healthPath: "/ready" };
  assert.deepEqual(editableUpdate(draft, original), { git_branch: "dev", health_path: "/ready" });
});
