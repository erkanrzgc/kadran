/**
 * Eski adlı kurulumun profillerinin taşınması (K-136).
 *
 * # Neden gerekli?
 *
 * Electron kullanıcı verisi dizinini paket adından türetiyor. Ad
 * `panely-desktop` → `kadran-desktop` olunca kayıtlı profiller ESKİ
 * dizinde kalır ve uygulama boş açılır: kullanıcı profillerini kaybettiğini
 * sanar. Ayrıca profillerin hedefleri `panely-client@sunucu`; sunucu
 * göçünden sonra o kullanıcı yok, hedefler de çevrilmeli.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { loadProfiles, migrateLegacyProfiles } from "../src/main/profiles.ts";

async function dirs(): Promise<{ root: string; legacy: string; current: string }> {
  const root = await mkdtemp(join(tmpdir(), "kadran-profil-"));
  return { root, legacy: join(root, "panely-desktop"), current: join(root, "kadran-desktop") };
}

const ESKI = [
  { name: "canlı", target: "panely-client@sunucu.example" },
  { name: "port", target: "panely-client@10.0.0.5:2222" },
  { name: "yerel", target: "unix:///run/kadran/api.sock" },
  { name: "başka", target: "biri@baska.example" },
];

test("eski dizindeki profiller yeni dizine taşınıyor, hedefler çevriliyor", async () => {
  const { root, legacy, current } = await dirs();
  try {
    await mkdir(legacy, { recursive: true });
    await writeFile(join(legacy, "profiles.json"), JSON.stringify(ESKI), "utf8");

    const moved = await migrateLegacyProfiles(current, legacy);
    assert.equal(moved, 4);

    const got = await loadProfiles(current);
    assert.deepEqual(got, [
      { name: "canlı", target: "kadran-client@sunucu.example" },
      { name: "port", target: "kadran-client@10.0.0.5:2222" },
      { name: "yerel", target: "unix:///run/kadran/api.sock" },
      // Kullanıcının elle yazdığı başka bir kullanıcı adına dokunulmuyor.
      { name: "başka", target: "biri@baska.example" },
    ]);
    // Eski dosya silinmiyor: geri dönüş için yerinde.
    assert.equal(JSON.parse(await readFile(join(legacy, "profiles.json"), "utf8")).length, 4);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("yeni dizinde profil dosyası varsa ÜSTÜNE YAZILMIYOR", async () => {
  const { root, legacy, current } = await dirs();
  try {
    await mkdir(legacy, { recursive: true });
    await mkdir(current, { recursive: true });
    await writeFile(join(legacy, "profiles.json"), JSON.stringify(ESKI), "utf8");
    const yeni = [{ name: "yeni", target: "kadran-client@yeni.example" }];
    await writeFile(join(current, "profiles.json"), JSON.stringify(yeni), "utf8");

    assert.equal(await migrateLegacyProfiles(current, legacy), 0);
    assert.deepEqual(await loadProfiles(current), yeni);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("eski dizin yoksa hiçbir şey yapılmıyor", async () => {
  const { root, legacy, current } = await dirs();
  try {
    assert.equal(await migrateLegacyProfiles(current, legacy), 0);
    assert.deepEqual(await loadProfiles(current), []);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("bozuk eski dosya taşınmıyor, yeni dizin boş kalıyor", async () => {
  const { root, legacy, current } = await dirs();
  try {
    await mkdir(legacy, { recursive: true });
    await writeFile(join(legacy, "profiles.json"), "{bozuk", "utf8");
    const uyarilar: string[] = [];
    assert.equal(await migrateLegacyProfiles(current, legacy, (m) => uyarilar.push(m)), 0);
    assert.deepEqual(await loadProfiles(current), []);
    assert.equal(uyarilar.length, 1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
