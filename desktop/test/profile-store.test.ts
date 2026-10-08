/**
 * Kayıtlı sunucu listesi. Diskten okunmadan yazılmaz: boş başlangıç
 * listesiyle yapılan ilk kayıt kullanıcının bütün sunucularını silerdi.
 * Yazmalar sıradadır ve hep en son listeden hesaplanır.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import type { Profile } from "../src/renderer/bridge.ts";
import { createProfileStore } from "../src/renderer/lib/profile-store.ts";

const p = (target: string): Profile => ({ name: target, target });

function deferred<V>() {
  let resolve!: (v: V) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<V>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

/** Sahte disk: save gelen listeyi kaydeder ve geri döner. */
function fakeDisk(load: () => Promise<Profile[]>) {
  const saves: string[][] = [];
  const seen: string[][] = [];
  const store = createProfileStore({
    load,
    save: async (list) => {
      saves.push(list.map((x) => x.target));
      return list;
    },
    onChange: (list) => seen.push(list.map((x) => x.target)),
  });
  return { store, saves, seen };
}

test("okuma bitmeden gelen ekleme diskteki listenin üstüne eklenir", async () => {
  const disk = deferred<Profile[]>();
  const { store, saves } = fakeDisk(() => disk.promise);
  const loading = store.load();
  const adding = store.add(p("new"));
  disk.resolve([p("a"), p("b")]);
  await Promise.all([loading, adding]);
  assert.deepEqual(saves, [["a", "b", "new"]]);
});

test("okuma başarısızsa yazılmaz; sonraki iş yeniden okur", async () => {
  let fail = true;
  const { store, saves } = fakeDisk(async () => {
    if (fail) throw new Error("disk");
    return [p("a")];
  });
  await assert.rejects(store.load(), /disk/);
  await assert.rejects(store.add(p("new")), /disk/);
  assert.deepEqual(saves, []);
  fail = false;
  await store.add(p("new"));
  assert.deepEqual(saves, [["a", "new"]]);
});

test("üst üste iki silme ikisini de uygular", async () => {
  const { store, saves, seen } = fakeDisk(async () => [p("a"), p("b"), p("c")]);
  await Promise.all([store.remove("a"), store.remove("b")]);
  assert.deepEqual(saves, [["b", "c"], ["c"]]);
  assert.deepEqual(seen.at(-1), ["c"]);
});

test("varsa eklemez, yoksa silmez", async () => {
  const { store, saves } = fakeDisk(async () => [p("a")]);
  await store.add(p("a"));
  await store.remove("zzz");
  assert.deepEqual(saves, []);
});
