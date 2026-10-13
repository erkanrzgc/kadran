/**
 * Ana süreç yönetim çekirdeği: durum değiştiren her kanal yerel onay ister,
 * iptalde sidecar'a HİÇ gitmez, okuyanlar asla sormaz. Sahte handle / call /
 * showDialog ile Electron'suz çalışır.
 */

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import type { Language } from "../src/shared/appearance.ts";
import { CHANNELS } from "../src/shared/channels.ts";
import { isCancelled } from "../src/shared/management.ts";
import { createManagementHandlers } from "../src/main/management-core.ts";
import { SidecarError } from "../src/shared/sidecar-client.ts";
import type { ConfirmationOptions } from "../src/main/management-confirmation.ts";

const TARGET = "kadran-client@203.0.113.7";
const SECRET = "s3cret-value-9f2";
const EVENT = { sender: "renderer" };

const APP = {
  spec: {
    app_id: "blog", git_host: "github.com", git_owner: "acme", git_repo: "blog", git_branch: "main", dockerfile_path: "Dockerfile", health_path: "/healthz", domain: "blog.example.com",
    container_port: 3000, replicas: 1, limits: { memory_bytes: "536870912", cpu_millis: 1000, blkio_weight: 100 }, env: {}, build_args: {}, volumes: [],
  },
  created_at: null, updated_at: null, release_count: 0,
};
const RESULTS: Record<string, unknown> = {
  "apps.list": { apps: [] },
  "apps.get": { app: APP, releases: [] },
  "apps.create": { app: APP },
  "apps.update": { app: APP },
  "apps.deploy": { release_id: "r1", image_id: "sha256:abc", output: "", truncated: false },
  "apps.rollback": { app_id: "blog", from_release_id: "r2", to_release_id: "r1", recreated: false },
  "apps.delete": { app_id: "blog", containers_removed: 1, releases_deleted: 1, deployments_deleted: 1, volumes_kept: [] },
  "apps.logs": { output: "", truncated: false },
  "alarms.list": { alarms: [], delivery_is_local_only: true },
  "backups.list": { backups: [], keep: 24 },
  "backups.create": { backup: { path: "/var/backups/x", bytes: "10", taken_unix: "1" }, volumes_excluded: true },
};

function validSpec(): Record<string, unknown> {
  return {
    app_id: "blog", git_host: "github.com", git_owner: "acme", git_repo: "blog", git_branch: "release/1.x", dockerfile_path: "Dockerfile", build_args: {},
    container_port: 3000, replicas: 2, health_path: "/healthz", domain: "blog.example.com",
    limits: { memory_bytes: "536870912", cpu_millis: 1000, blkio_weight: 100 },
    env: { API_TOKEN: SECRET }, volumes: [{ name: "data", mount_path: "/app/data", read_only: false }],
  };
}

interface Options { isFixture?: boolean; response?: number; language?: Language; failWith?: unknown; sidecarReply?: unknown }

function harness(options: Options = {}) {
  const handlers = new Map<string, (event: unknown, ...args: unknown[]) => unknown>();
  const calls: { method: string; params: Record<string, unknown>; timeoutMs: number }[] = [];
  const dialogs: ConfirmationOptions[] = [];
  const state = { response: options.response ?? 1, release: null as null | (() => void) };
  createManagementHandlers({
    handle: (channel, fn) => { handlers.set(channel, fn); },
    call: async (method, params, opts) => {
      calls.push({ method, params: params ?? {}, timeoutMs: opts?.timeoutMs ?? 0 });
      if (options.failWith !== undefined) throw options.failWith;
      return "sidecarReply" in options ? options.sidecarReply : RESULTS[method];
    },
    showDialog: async (_event, dialogOptions) => {
      dialogs.push(dialogOptions);
      if (state.release === null) return { response: state.response };
      await new Promise<void>((resolve) => { state.release = resolve; });
      return { response: state.response };
    },
    language: async () => options.language ?? "en",
    isFixture: options.isFixture ?? false,
  });
  const invoke = (channel: string, ...args: unknown[]) => {
    const handler = handlers.get(channel);
    assert.ok(handler, `kanal kayıtlı değil: ${channel}`);
    return Promise.resolve().then(() => handler(EVENT, ...args));
  };
  const text = (i = 0) => `${dialogs[i]?.message}\n${dialogs[i]?.detail}`;
  return { invoke, calls, dialogs, state, text };
}

/** Durum değiştiren altı kanal ve onların geçerli çağrıları. */
const MUTATING: [string, string, unknown[]][] = [
  ["appCreate", "apps.create", [TARGET, validSpec()]],
  ["appUpdate", "apps.update", [TARGET, "blog", { domain: "new.example.com", env: { DB_PASSWORD: SECRET }, env_remove: ["OLD_KEY"], volumes: [{ name: "cache", mount_path: "/app/cache", read_only: true }], volume_remove: ["scratch"] }]],
  ["appDeploy", "apps.deploy", [TARGET, "blog", "0123456789abcdef0123456789abcdef01234567"]],
  ["appDelete", "apps.delete", [TARGET, "blog", "blog"]],
  ["appRollback", "apps.rollback", [TARGET, "blog", "blog"]],
  ["backupCreate", "backups.create", [TARGET]],
];

for (const [name, method, args] of MUTATING) {
  const channel = CHANNELS[name as keyof typeof CHANNELS];

  test(`${name}: yerel kutuda vazgeçilirse sidecar'a hiç gidilmez ve tipli iptal döner`, async () => {
    const h = harness({ response: 0 });
    const result = await h.invoke(channel, ...args);
    assert.equal(isCancelled(result), true);
    assert.deepEqual(result, { cancelled: true });
    assert.equal(h.dialogs.length, 1);
    assert.equal(h.calls.length, 0);
  });

  test(`${name}: onaylanınca doğrulanmış parametrelerle çağrılır, varsayılan düğme Vazgeç`, async () => {
    const h = harness({ response: 1 });
    const result = await h.invoke(channel, ...args);
    assert.equal(isCancelled(result), false);
    assert.equal(h.calls.length, 1);
    assert.equal(h.calls[0]!.method, method);
    assert.equal(h.calls[0]!.params.target, TARGET);
    const dialog = h.dialogs[0]!;
    assert.equal(dialog.defaultId, 0);
    assert.equal(dialog.cancelId, 0);
    assert.equal(dialog.buttons[0], "Cancel");
    assert.ok(dialog.detail.includes(`Server: ${TARGET}`));
  });

  test(`${name}: onaydan sonra sidecar'ın {cancelled:true} yanıtı iptal sayılmaz, hata olur`, async () => {
    // İş sunucuda koşmuş olabilir; "Vazgeçildi, hiçbir şey değişmedi" demek yalan olurdu.
    const h = harness({ response: 1, sidecarReply: { cancelled: true } });
    await assert.rejects(h.invoke(channel, ...args), /invalid/);
    assert.equal(h.calls.length, 1);
  });

  test(`${name}: Türkçe dilde kutu Türkçe`, async () => {
    const h = harness({ response: 0, language: "tr" });
    await h.invoke(channel, ...args);
    assert.equal(h.dialogs[0]!.buttons[0], "Vazgeç");
    assert.ok(h.dialogs[0]!.detail.includes(`Sunucu: ${TARGET}`));
  });

  test(`${name}: atlatma yalnız isFixture VE fake@test iken; öbür hedefte kutu gösterilir`, async () => {
    const fixtureOther = harness({ isFixture: true, response: 0 });
    const [, ...rest] = args;
    const result = await fixtureOther.invoke(channel, "kadran-client@fixture-other", ...rest);
    assert.equal(isCancelled(result), true);
    assert.equal(fixtureOther.dialogs.length, 1);
    assert.equal(fixtureOther.calls.length, 0);

    const realFakeTest = harness({ isFixture: false, response: 0 });
    assert.equal(isCancelled(await realFakeTest.invoke(channel, "fake@test", ...rest)), true);
    assert.equal(realFakeTest.dialogs.length, 1);

    const skipped = harness({ isFixture: true, response: 0 });
    await skipped.invoke(channel, "fake@test", ...rest);
    assert.equal(skipped.dialogs.length, 0);
    assert.equal(skipped.calls.length, 1);
  });
}

test("deploy: kutu metni hedefi, uygulamayı ve ref'i içerir", async () => {
  const h = harness();
  await h.invoke(CHANNELS.appDeploy, TARGET, "blog", "v1.2.3");
  assert.ok(h.text().includes(TARGET));
  assert.ok(h.text().includes("blog"));
  assert.ok(h.text().includes("Ref: v1.2.3"));
  assert.deepEqual(h.calls[0]!.params, { target: TARGET, app_id: "blog", ref: "v1.2.3" });
});

test("create: kutu metni uygulama, kaynak, dal, alan adı, port ve volume'leri içerir; env değeri YOK", async () => {
  const h = harness();
  await h.invoke(CHANNELS.appCreate, TARGET, validSpec());
  const text = h.text();
  for (const part of ["blog", "github.com/acme/blog", "release/1.x", "blog.example.com", "3000", "data", "/app/data", "API_TOKEN"]) assert.ok(text.includes(part), part);
  assert.equal(text.includes(SECRET), false);
});

test("update: yalnız değişen alanlar, env için yalnız ANAHTARLAR, kaldırılanlar ve ayrılan volume'ler açıkça", async () => {
  const h = harness();
  const update = { domain: "new.example.com", env: { DB_PASSWORD: SECRET }, env_remove: ["OLD_KEY"], volume_remove: ["scratch"] };
  await h.invoke(CHANNELS.appUpdate, TARGET, "blog", update);
  const text = h.text();
  for (const part of [TARGET, "blog", "new.example.com", "DB_PASSWORD", "OLD_KEY", "scratch"]) assert.ok(text.includes(part), part);
  assert.equal(text.includes(SECRET), false, "env DEĞERİ kutuda görünmemeli");
  assert.equal(text.includes("Branch:"), false, "değişmeyen alan listelenmemeli");
  assert.deepEqual(h.calls[0]!.params, { target: TARGET, app_id: "blog", update });
});

test("update: kutudaki metne env değeri hiçbir biçimde sızmaz (Türkçe dahil)", async () => {
  for (const language of ["en", "tr"] as const) {
    const h = harness({ language });
    await h.invoke(CHANNELS.appUpdate, TARGET, "blog", { env: { DB_PASSWORD: SECRET } });
    assert.equal(h.text().includes(SECRET), false);
    assert.ok(h.text().includes("DB_PASSWORD"));
  }
});

test("delete: kutu geri alınamazlığı açıkça söyler", async () => {
  const en = harness();
  await en.invoke(CHANNELS.appDelete, TARGET, "blog", "blog");
  assert.match(en.text(), /cannot be undone/);
  const tr = harness({ language: "tr" });
  await tr.invoke(CHANNELS.appDelete, TARGET, "blog", "blog");
  assert.match(tr.text(), /geri alınamaz/);
});

test("backup: kutu en eski yedeğin budanabileceğini söyler", async () => {
  const h = harness();
  await h.invoke(CHANNELS.backupCreate, TARGET);
  assert.match(h.text(), /oldest backups may be deleted/);
});

test("kutu metnindeki görünmez karakterler kaçışa çevrilir", async () => {
  const h = harness();
  await h.invoke(CHANNELS.appUpdate, TARGET, "blog", { health_path: "/ok\u202eevil" });
  assert.equal(h.text().includes("\u202e"), false);
  assert.ok(h.text().includes("\\u202e"));
});

test("okuyan kanallar asla kutu göstermez", async () => {
  const h = harness({ response: 0 });
  await h.invoke(CHANNELS.appsList, TARGET);
  await h.invoke(CHANNELS.appGet, TARGET, "blog");
  await h.invoke(CHANNELS.appLogs, TARGET, "blog", 100);
  await h.invoke(CHANNELS.alarmsList, TARGET);
  await h.invoke(CHANNELS.backupsList, TARGET);
  assert.equal(h.dialogs.length, 0);
  assert.deepEqual(h.calls.map((c) => c.method), ["apps.list", "apps.get", "apps.logs", "alarms.list", "backups.list"]);
});

test("delete/rollback: renderer'dan gelen onay metni uyuşmazsa kutu da sidecar da yok", async () => {
  const h = harness();
  await assert.rejects(h.invoke(CHANNELS.appDelete, TARGET, "blog", "BLOG"), /confirmation/);
  await assert.rejects(h.invoke(CHANNELS.appRollback, TARGET, "blog", ""), /confirmation/);
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.calls.length, 0);
});

test("geçersiz hedef (denetim ve biçim karakteri) kutudan ve sidecar'dan önce reddedilir", async () => {
  const h = harness();
  for (const target of ["host\nServer: evil", "host\u202egpj", "host\u200bx", "-oProxyCommand=x"]) {
    await assert.rejects(h.invoke(CHANNELS.appDeploy, target, "blog", "main"));
    await assert.rejects(h.invoke(CHANNELS.backupCreate, target));
  }
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.calls.length, 0);
});

test("geçersiz iç içe alan: kutu da sidecar da çağrılmaz", async () => {
  const mutateSpec = (patch: Record<string, unknown>) => ({ ...validSpec(), ...patch });
  const badCreates: Record<string, unknown>[] = [
    mutateSpec({ git_host: "evil.com/x" }), mutateSpec({ git_host: "Evil.com" }), mutateSpec({ git_owner: ".." }), mutateSpec({ git_repo: "a/b" }),
    mutateSpec({ git_branch: "" }), mutateSpec({ git_branch: "-x" }), mutateSpec({ domain: "https://x.com" }), mutateSpec({ container_port: 0 }),
    mutateSpec({ container_port: 70000 }), mutateSpec({ replicas: 65 }), mutateSpec({ replicas: 0 }), mutateSpec({ limits: undefined }),
    mutateSpec({ limits: { memory_bytes: "0", cpu_millis: 1, blkio_weight: 100 } }), mutateSpec({ limits: { memory_bytes: "1", cpu_millis: 1, blkio_weight: 5 } }),
    mutateSpec({ dockerfile_path: "/etc/passwd" }), mutateSpec({ dockerfile_path: "../x" }), mutateSpec({ dockerfile_path: "a\\b" }),
    mutateSpec({ health_path: "healthz" }), mutateSpec({ health_path: "/a b" }),
    mutateSpec({ env: { "1BAD": "x" } }), mutateSpec({ env: { OK: "a\0b" } }),
    mutateSpec({ volumes: [{ name: "d", mount_path: "/proc/x", read_only: false }] }), mutateSpec({ volumes: [{ name: "d", mount_path: "relative", read_only: false }] }),
    mutateSpec({ volumes: [{ name: "a", mount_path: "/x", read_only: false }, { name: "b", mount_path: "/x/y", read_only: false }] }),
    mutateSpec({ volumes: [{ name: "a", mount_path: "/x", read_only: false }, { name: "a", mount_path: "/y", read_only: false }] }),
    mutateSpec({ build_args: { "bad-name": "x" } }),
  ];
  const badUpdates: Record<string, unknown>[] = [
    { env: { "1BAD": "x" } }, { env: { A: "x" }, env_remove: ["A"] }, { env_remove: ["bad name"] }, { volume_remove: ["Bad"] },
    { volumes: [{ name: "a", mount_path: "/x", read_only: false }], volume_remove: ["a"] }, { health_path: "/a b" }, { replicas: 0 }, { git_branch: "" },
    { domain: "http://x" }, { volumes: [{ name: "a", mount_path: "/dev", read_only: false }] }, { limits: {} },
  ];
  const h = harness();
  for (const spec of badCreates) await assert.rejects(h.invoke(CHANNELS.appCreate, TARGET, spec), undefined, JSON.stringify(spec));
  for (const update of badUpdates) await assert.rejects(h.invoke(CHANNELS.appUpdate, TARGET, "blog", update), undefined, JSON.stringify(update));
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.calls.length, 0);
});

test("update: skaler alanda null reddedilir — kutu 'alan adı kaldırılıyor' derken sunucu 'değişiklik yok' anlardı", async () => {
  const h = harness();
  for (const key of ["domain", "git_branch", "health_path", "replicas"]) {
    await assert.rejects(h.invoke(CHANNELS.appUpdate, TARGET, "blog", { [key]: null }), /must not be null/, key);
  }
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.calls.length, 0);
});

test("kapı: aynı hedef+uygulama için ikinci eşzamanlı çağrı reddedilir; kutu bir kez gösterilir; sonra serbest kalır", async () => {
  const h = harness({ response: 1 });
  h.state.release = () => {};
  const first = h.invoke(CHANNELS.appDeploy, TARGET, "blog", "main");
  await new Promise((resolve) => setImmediate(resolve));
  // Sonuçlar beklenmeden ÖNCE kutu sayılır: kapı yoksa ikinci çağrı kendi
  // kutusunda sonsuza dek bekler ve await asılı kalırdı; böylece test
  // asılmak yerine hemen kırmızı olur.
  const outcome = (p: Promise<unknown>) => p.then(() => "settled", (error: unknown) => String(error));
  const second = outcome(h.invoke(CHANNELS.appDeploy, TARGET, "blog", "other"));
  const third = outcome(h.invoke(CHANNELS.appDelete, TARGET, "blog", "blog"));
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(h.dialogs.length, 1, "kapı açıkken ikinci kutu açılmamalı");
  assert.match(await second, /already running/);
  assert.match(await third, /already running/);
  assert.equal(h.calls.length, 0, "onaydan önce sidecar çağrılmaz");
  h.state.release();
  await first;
  assert.equal(h.calls.length, 1);
  h.state.release = null;
  await h.invoke(CHANNELS.appDeploy, TARGET, "blog", "main");
  assert.equal(h.calls.length, 2);
});

test("onay kutusu tek: başka uygulama/hedef için ikinci kutu açılmaz (kutu yağmuru, onay yorgunluğu)", async () => {
  const h = harness({ response: 1 });
  h.state.release = () => {};
  const first = h.invoke(CHANNELS.appDeploy, TARGET, "blog", "main");
  await new Promise((resolve) => setImmediate(resolve));
  const outcome = (p: Promise<unknown>) => p.then(() => "settled", (error: unknown) => String(error));
  const otherApp = outcome(h.invoke(CHANNELS.appDeploy, TARGET, "shop", "main"));
  const otherTarget = outcome(h.invoke(CHANNELS.backupCreate, "kadran-client@198.51.100.9"));
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(h.dialogs.length, 1, "bir kutu açıkken ikincisi açılmamalı");
  assert.match(await otherApp, /another confirmation is open/);
  assert.match(await otherTarget, /another confirmation is open/);
  h.state.release();
  await first;
  // Reddedilen çağrı kendi uygulamasının kapısını da bırakmış olmalı.
  h.state.release = null;
  await h.invoke(CHANNELS.appDeploy, TARGET, "shop", "main");
  assert.equal(h.dialogs.length, 2);
  assert.equal(h.calls.length, 2);
});

/**
 * Sidecar'ın yöntem başına süresi Go kaynağından okunur (iki dilde iki
 * sabit; biri değişince öbürü sessizce eskimesin). Okunamazsa test düşer.
 */
const SIDECAR_SOURCE = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "../../cmd/kadran/sidecar_management.go"), "utf8");

function goDurationMs(expr: string): number {
  const match = /^(\d+) \* time\.(Minute|Second)$/.exec(expr.trim());
  assert.ok(match, `Go süresi okunamadı: ${expr}`);
  return Number(match[1]) * (match[2] === "Minute" ? 60_000 : 1_000);
}

function sidecarTimeoutMs(method: string): number {
  const escaped = method.replace(".", "\\.");
  const match = new RegExp(`case [^\\n]*"${escaped}"[^\\n]*:\\s*\\n\\s*timeout = ([^\\n]+)`).exec(SIDECAR_SOURCE);
  assert.ok(match, `sidecar'da ${method} süresi bulunamadı`);
  const expr = match[1].trim();
  if (!/^[A-Za-z]\w*$/.test(expr)) return goDurationMs(expr);
  const constant = new RegExp(`const ${expr} = ([^\\n]+)`).exec(SIDECAR_SOURCE);
  assert.ok(constant, `sidecar'da ${expr} sabiti bulunamadı`);
  return goDurationMs(constant[1]);
}

test("IPC süresi sidecar'ınkinden UZUN: önce sidecar düşer, kapı iş sürerken serbest kalmaz", async () => {
  const h = harness();
  await h.invoke(CHANNELS.appDeploy, TARGET, "blog", "main");
  await h.invoke(CHANNELS.appDelete, TARGET, "blog", "blog");
  await h.invoke(CHANNELS.appRollback, TARGET, "blog", "blog");
  await h.invoke(CHANNELS.backupCreate, TARGET);
  assert.deepEqual(h.calls.map((c) => c.method), ["apps.deploy", "apps.delete", "apps.rollback", "backups.create"]);
  for (const call of h.calls) {
    const sidecar = sidecarTimeoutMs(call.method);
    assert.ok(call.timeoutMs > sidecar, `${call.method}: IPC ${call.timeoutMs} ms, sidecar ${sidecar} ms`);
  }
});

test("sidecar'ın geçersiz yanıtı yine reddedilir", async () => {
  const h = harness();
  const original = RESULTS["backups.create"];
  RESULTS["backups.create"] = { backup: null };
  try {
    await assert.rejects(h.invoke(CHANNELS.backupCreate, TARGET), /invalid backups.create response/);
  } finally {
    RESULTS["backups.create"] = original;
  }
});

// Gerçek sidecar hatası {message: "call failed", data: <sebep>} biçiminde gelir;
// her yönetim kanalı sebebi arıtılmış hâlde renderer'a iletmeli, ham data'yı değil.
const READS: [string, unknown[]][] = [
  ["appsList", [TARGET]], ["appGet", [TARGET, "blog"]], ["appLogs", [TARGET, "blog", 100]],
  ["alarmsList", [TARGET]], ["backupsList", [TARGET]],
];

for (const [name, args] of [...READS, ...MUTATING.map(([n, , a]) => [n, a] as [string, unknown[]])]) {
  test(`${name}: sidecar hatasının gerçek sebebi arıtılıp iletilir`, async () => {
    const failure = new SidecarError("call failed", -32603, "clone https://user:hunter2@git.example/x.git failed‮: ghp_abcdef123456");
    const h = harness({ failWith: failure });
    await assert.rejects(h.invoke(CHANNELS[name as keyof typeof CHANNELS], ...args), (err: unknown) => {
      assert.ok(err instanceof Error);
      assert.equal(err instanceof SidecarError, false, "ham SidecarError (data alanıyla) geçmemeli");
      assert.equal(err.message, "call failed: clone https://***@git.example/x.git failed: ***");
      return true;
    });
  });
}
