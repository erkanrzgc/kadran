/**
 * Kayıtlı sunucu listesi (K-162). Liste ortak dosyada (servers.json) ve onu
 * yan süreç yazar; arayüz bütün listeyi değil tek değişikliği gönderir, böylece
 * CLI'ın o arada eklediği sunucu silinmez. Her çağrı güncel listeyi döner.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import type { Profile } from "../src/renderer/bridge.ts";
import { createProfileStore, type ProfileIO } from "../src/renderer/lib/profile-store.ts";

const p = (target: string): Profile => ({ name: target, target });

/** Sahte yan süreç: listeyi bellekte tutar, çağrıları kaydeder. */
function fakeIO(initial: Profile[]) {
  let disk = [...initial];
  const calls: unknown[][] = [];
  const seen: string[][] = [];
  const io: ProfileIO = {
    load: async () => {
      calls.push(["load"]);
      return disk;
    },
    add: async (profile) => {
      calls.push(["add", profile]);
      disk = [...disk, profile];
      return disk;
    },
    update: async (target, change) => {
      calls.push(["update", target, change]);
      disk = disk.map((x) => (x.target === target ? { name: change.name, target, ...(change.provider ? { provider: change.provider } : {}) } : x));
      return disk;
    },
    remove: async (target) => {
      calls.push(["remove", target]);
      disk = disk.filter((x) => x.target !== target);
      return disk;
    },
    onChange: (list) => seen.push(list.map((x) => x.target)),
  };
  return { io, calls, seen, write: (next: Profile[]) => (disk = next) };
}

test("add sends only the new profile and applies the returned list", async () => {
  const f = fakeIO([p("a"), p("b")]);
  const store = createProfileStore(f.io);
  await store.load();
  await store.add(p("new"));
  assert.deepEqual(f.calls, [["load"], ["add", p("new")]]);
  assert.deepEqual(f.seen.at(-1), ["a", "b", "new"]);
});

test("reload re-reads even when already loaded: another writer's change appears", async () => {
  const f = fakeIO([p("a")]);
  const store = createProfileStore(f.io);
  await store.load();
  await store.load();
  assert.equal(f.calls.filter((c) => c[0] === "load").length, 1);
  f.write([p("a"), p("from-cli")]);
  await store.reload();
  assert.equal(f.calls.filter((c) => c[0] === "load").length, 2);
  assert.deepEqual(f.seen.at(-1), ["a", "from-cli"]);
});

test("an add error leaves the current list as it was and rejects", async () => {
  const f = fakeIO([p("a")]);
  f.io.add = async () => {
    throw new Error("server-list:duplicate_target");
  };
  const store = createProfileStore(f.io);
  await store.load();
  await assert.rejects(store.add(p("a")), /duplicate_target/);
  assert.deepEqual(f.seen, [["a"]]);
});

test("two removes in a row are both sent, in order", async () => {
  const f = fakeIO([p("a"), p("b"), p("c")]);
  const store = createProfileStore(f.io);
  await Promise.all([store.remove("a"), store.remove("b")]);
  assert.deepEqual(f.calls, [["remove", "a"], ["remove", "b"]]);
  assert.deepEqual(f.seen.at(-1), ["c"]);
});

/** Yan sürecin "kayıt yok" hatası, IPC'nin eklediği önekle (main/server-list.ts). */
const notFound = () => new Error("Error invoking remote method 'kadran:servers-remove': Error: server-list:not_found");

test("removing a server already deleted from the CLI re-reads the list and resolves", async () => {
  const f = fakeIO([p("a"), p("b")]);
  const store = createProfileStore(f.io);
  await store.load();
  f.write([p("a")]);
  f.io.remove = async (target) => {
    f.calls.push(["remove", target]);
    throw notFound();
  };
  await store.remove("b");
  assert.deepEqual(f.calls.slice(-2), [["remove", "b"], ["load"]]);
  assert.deepEqual(f.seen.at(-1), ["a"]);
});

test("updating a server already deleted from the CLI re-reads the list and still rejects", async () => {
  const f = fakeIO([p("a"), p("b")]);
  const store = createProfileStore(f.io);
  await store.load();
  f.write([p("a")]);
  f.io.update = async () => {
    throw notFound();
  };
  await assert.rejects(store.update("b", { name: "x", provider: "" }), /server-list:not_found/);
  assert.deepEqual(f.seen.at(-1), ["a"]);
});

test("another remove error rejects and does not re-read", async () => {
  const f = fakeIO([p("a")]);
  const store = createProfileStore(f.io);
  await store.load();
  f.io.remove = async () => {
    throw new Error("server-list:newer_file");
  };
  await assert.rejects(store.remove("a"), /newer_file/);
  assert.equal(f.calls.filter((c) => c[0] === "load").length, 1);
});

test("update sends the target and the change, provider empty clears it", async () => {
  const f = fakeIO([{ name: "a", target: "a", provider: "aws" }]);
  const store = createProfileStore(f.io);
  await store.update("a", { name: "ana", provider: "" });
  assert.deepEqual(f.calls, [["update", "a", { name: "ana", provider: "" }]]);
  assert.deepEqual(f.seen.at(-1), ["a"]);
});
