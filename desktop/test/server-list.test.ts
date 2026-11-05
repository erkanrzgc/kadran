/**
 * Ortak sunucu listesi: ana süreç kanalları ve eski profil listesinin bir
 * kez taşınması (K-162). Dosyayı yan süreç yazar; burada sahte bir çağrı
 * listeyi bellekte tutar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { CHANNELS } from "../src/shared/channels.ts";
import { SidecarError } from "../src/shared/sidecar-client.ts";
import { IMPORT_MARKER, importProfiles, registerServerList } from "../src/main/server-list.ts";

interface Call { method: string; params?: Record<string, unknown> }

function fakeSidecar(fail?: Error) {
  const calls: Call[] = [];
  let list: Record<string, unknown>[] = [];
  const call = async (method: string, params?: Record<string, unknown>) => {
    calls.push({ method, params });
    if (fail) throw fail;
    if (method === "servers.import") {
      // Gerçek yan süreç gibi: eski kullanıcı adıyla hedef alınamaz, sebebiyle döner.
      const profiles = (params?.profiles ?? []) as Record<string, unknown>[];
      const refused = (p: Record<string, unknown>) => String(p.target).startsWith("panely-client@");
      list = [...list, ...profiles.filter((p) => !refused(p))];
      const skipped = profiles.filter(refused).map((p) => ({ target: p.target, reason: "old user name" }));
      return { servers: list, imported: profiles.length - skipped.length, ...(skipped.length ? { skipped } : {}) };
    }
    if (method === "servers.add") list = [...list, { ...params, admin: "root@x", sudo: true }];
    return { servers: list };
  };
  return { calls, call };
}

async function userData(profiles?: string): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "kadran-sunucu-"));
  if (profiles !== undefined) await writeFile(join(dir, "profiles.json"), profiles);
  return dir;
}

const exists = (p: string) => stat(p).then(() => true, () => false);

test("migration normalizes names and runs once", async () => {
  const profiles = [
    { name: "a", target: "t1" },
    { name: "a", target: "t2" },
    { name: "-x", target: "t3" },
    { name: "x".repeat(200), target: "t4", provider: "hetzner" },
  ];
  const dir = await userData(JSON.stringify(profiles));
  const sidecar = fakeSidecar();
  assert.equal((await importProfiles(dir, sidecar.call, () => {})).result, "imported");
  assert.equal(sidecar.calls.length, 1);
  assert.equal(sidecar.calls[0]!.method, "servers.import");
  assert.deepEqual(sidecar.calls[0]!.params, { profiles });
  assert.ok(await exists(join(dir, IMPORT_MARKER)));
  // profiles.json olduğu gibi kalır: eski sürüme dönen listesini görür.
  assert.equal(await readFile(join(dir, "profiles.json"), "utf8"), JSON.stringify(profiles));

  assert.equal((await importProfiles(dir, sidecar.call, () => {})).result, "already");
  assert.equal(sidecar.calls.length, 1);
});

test("no profiles file: marker written, nothing imported", async () => {
  const dir = await userData();
  const sidecar = fakeSidecar();
  assert.equal((await importProfiles(dir, sidecar.call, () => {})).result, "imported");
  assert.equal(sidecar.calls.length, 0);
  assert.ok(await exists(join(dir, IMPORT_MARKER)));
});

test("damaged profiles file is not 'nothing to import'", async () => {
  for (const body of ["[{", "{}", ""]) {
    const dir = await userData(body);
    const sidecar = fakeSidecar();
    const warnings: string[] = [];
    assert.equal((await importProfiles(dir, sidecar.call, (m) => warnings.push(m))).result, "failed", body);
    assert.equal(sidecar.calls.length, 0);
    assert.equal(await exists(join(dir, IMPORT_MARKER)), false);
    assert.equal(warnings.length, 1);
  }
});

test("sidecar error: no marker, retried on next start", async () => {
  const dir = await userData(JSON.stringify([{ name: "a", target: "t1" }]));
  const failing = fakeSidecar(new SidecarError("call failed", -32603, "the server list is damaged"));
  assert.equal((await importProfiles(dir, failing.call, () => {})).result, "failed");
  assert.equal(await exists(join(dir, IMPORT_MARKER)), false);
  const ok = fakeSidecar();
  assert.equal((await importProfiles(dir, ok.call, () => {})).result, "imported");
});

function handlers(call: ReturnType<typeof fakeSidecar>["call"], status: () => string | null = () => null) {
  const fns = new Map<string, (event: unknown, ...args: unknown[]) => unknown>();
  registerServerList({ handle: (channel, fn) => fns.set(channel, fn), call, importStatus: status as () => never });
  // ipcMain.handle gibi: eşzamanlı fırlatılan hata da reddedilmiş söz olur.
  return async (channel: string, ...args: unknown[]) => fns.get(channel)!(undefined, ...args);
}

test("handlers reject bad input before calling the sidecar", async () => {
  const sidecar = fakeSidecar();
  const invoke = handlers(sidecar.call);
  await assert.rejects(invoke(CHANNELS.serversAdd, { name: 5, target: "kadran-client@a" }));
  await assert.rejects(invoke(CHANNELS.serversAdd, { name: "a", target: "kadran-client@a", provider: "p".repeat(65) }));
  await assert.rejects(invoke(CHANNELS.serversAdd, { name: "a", target: "-oProxyCommand=x" }));
  await assert.rejects(invoke(CHANNELS.serversAdd, { name: "", target: "kadran-client@a" }));
  await assert.rejects(invoke(CHANNELS.serversUpdate, "kadran-client@a", { name: "a".repeat(201), provider: "" }));
  await assert.rejects(invoke(CHANNELS.serversRemove, 42));
  assert.equal(sidecar.calls.length, 0);
});

test("handlers return profiles only: admin and sudo stay in the file", async () => {
  const sidecar = fakeSidecar();
  const invoke = handlers(sidecar.call);
  const list = await invoke(CHANNELS.serversAdd, { name: "a", target: "kadran-client@a", provider: "Hetzner" });
  assert.deepEqual(list, [{ name: "a", target: "kadran-client@a", provider: "Hetzner" }]);
  assert.deepEqual(sidecar.calls[0], { method: "servers.add", params: { name: "a", target: "kadran-client@a", provider: "Hetzner" } });
  await invoke(CHANNELS.serversUpdate, "kadran-client@a", { name: "b", provider: "" });
  assert.deepEqual(sidecar.calls[1], { method: "servers.update", params: { target: "kadran-client@a", name: "b", provider: "" } });
  await invoke(CHANNELS.serversRemove, "kadran-client@a");
  assert.deepEqual(sidecar.calls[2], { method: "servers.remove", params: { target: "kadran-client@a" } });
});

test("a known sidecar reason reaches the renderer as a stable code", async () => {
  const invoke = handlers(fakeSidecar(new SidecarError("call failed", -32603, "duplicate_name")).call);
  await assert.rejects(invoke(CHANNELS.serversAdd, { name: "a", target: "kadran-client@a" }), { message: "server-list:duplicate_name" });
  const other = handlers(fakeSidecar(new SidecarError("call failed", -32603, "disk full")).call);
  await assert.rejects(other(CHANNELS.serversList), /disk full/);
});

test("import status channel reports the startup result", async () => {
  const invoke = handlers(fakeSidecar().call, () => ({ result: "failed", skipped: [] }));
  assert.deepEqual(await invoke(CHANNELS.serversImportStatus), { result: "failed", skipped: [] });
});

// Göç bir kez koşar: alınamayan sunucu kartlardan sessizce kaybolmamalı. Eski
// masaüstü yalnız ilk 100 profili gösteriyordu; fazlası da söylenir ve göç biter.
test("over 100 old profiles and refused ones are reported, and the import finishes", async () => {
  const profiles = Array.from({ length: 105 }, (_, i) => ({ name: `s${i}`, target: `t${i}` }));
  profiles[3] = { name: "old", target: "panely-client@x" };
  const dir = await userData(JSON.stringify(profiles));
  const sidecar = fakeSidecar();
  const status = await importProfiles(dir, sidecar.call, () => {});
  assert.equal(status.result, "imported");
  assert.equal((sidecar.calls[0]!.params!.profiles as unknown[]).length, 100);
  assert.deepEqual(status.skipped, ["panely-client@x", "t100", "t101", "t102", "t103", "t104"]);
  assert.ok(await exists(join(dir, IMPORT_MARKER)));
});
