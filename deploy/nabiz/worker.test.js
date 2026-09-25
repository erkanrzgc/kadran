// node --test deploy/nabiz/
//
// Worker'ı Cloudflare'e yüklemeden, sahte KV ve sahte fetch ile sınar.
import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { timingSafeEqual } from "node:crypto";
import worker from "./worker.js";

// Workers çalışma zamanındaki standart dışı fonksiyon; Node'da yok.
if (!crypto.subtle.timingSafeEqual) {
  crypto.subtle.timingSafeEqual = (a, b) =>
    timingSafeEqual(Buffer.from(a), Buffer.from(b));
}

function sahteKV(baslangic = {}) {
  const veri = new Map(Object.entries(baslangic));
  return {
    veri,
    yazma: 0,
    async get(k) { return veri.has(k) ? veri.get(k) : null; },
    async put(k, v) { this.yazma++; veri.set(k, v); },
  };
}

let gonderilen;
let telegramCalisiyor;
beforeEach(() => {
  gonderilen = [];
  telegramCalisiyor = true;
  globalThis.fetch = async (url, secenek) => {
    gonderilen.push({ url: String(url), govde: JSON.parse(secenek.body) });
    return { ok: telegramCalisiyor, status: telegramCalisiyor ? 200 : 502 };
  };
});

const env = (kv) => ({
  NABIZ: kv,
  PING_TOKEN: "dogru-anahtar",
  TELEGRAM_TOKEN: "123:abc",
  TELEGRAM_CHAT_ID: "42",
  ESIK_DAKIKA: "15",
  SUNUCU: "panely-test",
});

const ping = (yetki, yol = "/ping", yontem = "POST") =>
  new Request(`https://nabiz.example${yol}`, {
    method: yontem,
    headers: yetki === null ? {} : { Authorization: yetki },
  });

test("doğru anahtarlı nabız zamanı yazar", async () => {
  const kv = sahteKV();
  const y = await worker.fetch(ping("Bearer dogru-anahtar"), env(kv));
  assert.equal(y.status, 204);
  assert.ok(Number(kv.veri.get("son_nabiz")) > 0);
});

test("yanlış, eksik ya da önek farklı anahtar REDDEDİLİR, yazma YOK", async () => {
  for (const yetki of ["Bearer yanlis", null, "dogru-anahtar", "Bearer dogru-anahtarX"]) {
    const kv = sahteKV();
    const y = await worker.fetch(ping(yetki), env(kv));
    assert.equal(y.status, 401, `yetki=${yetki}`);
    assert.equal(kv.yazma, 0, `yetki=${yetki} yine de yazdı`);
  }
});

test("PING_TOKEN tanımsızsa hiçbir nabız kabul edilmez", async () => {
  const kv = sahteKV();
  const e = { ...env(kv), PING_TOKEN: undefined };
  const y = await worker.fetch(ping("Bearer undefined"), e);
  assert.equal(y.status, 503);
  assert.equal(kv.yazma, 0);
});

test("başka yol ya da yöntem 404", async () => {
  const kv = sahteKV();
  assert.equal((await worker.fetch(ping("Bearer dogru-anahtar", "/"), env(kv))).status, 404);
  assert.equal((await worker.fetch(ping("Bearer dogru-anahtar", "/ping", "GET"), env(kv))).status, 404);
  assert.equal(kv.yazma, 0);
});

test("nabız eskiyse zamanlanmış kontrol ALARM gönderir ve durumu yazar", async () => {
  const kv = sahteKV({ son_nabiz: String(Date.now() - 20 * 60_000), durum: "var" });
  await worker.scheduled({}, env(kv));
  assert.equal(gonderilen.length, 1);
  assert.match(gonderilen[0].govde.text, /NABIZ YOK — panely-test/);
  assert.equal(gonderilen[0].govde.chat_id, "42");
  assert.equal(kv.veri.get("durum"), "yok");
});

// En önemli davranış: Telegram'a ulaşılamazsa durum YAZILMAMALI.
// Yazılsaydı bir sonraki kontrol "zaten alarmda" deyip susardı ve
// alarm hiç ulaşmazdı.
test("Telegram başarısızsa durum yazılmaz, sonraki kontrol yeniden dener", async () => {
  const kv = sahteKV({ son_nabiz: String(Date.now() - 20 * 60_000), durum: "var" });
  telegramCalisiyor = false;
  await assert.rejects(worker.scheduled({}, env(kv)), /Telegram gönderimi başarısız/);
  assert.equal(kv.veri.get("durum"), "var", "durum yazıldı — alarm yutulurdu");

  telegramCalisiyor = true;
  await worker.scheduled({}, env(kv));
  assert.equal(gonderilen.length, 2, "ikinci kontrol yeniden denemedi");
  assert.equal(kv.veri.get("durum"), "yok");
});

test("hata iletisi bot anahtarını SIZDIRMAZ", async () => {
  const kv = sahteKV({ son_nabiz: String(Date.now() - 20 * 60_000), durum: "var" });
  telegramCalisiyor = false;
  await assert.rejects(worker.scheduled({}, env(kv)), (h) => !String(h).includes("123:abc"));
});

test("taze nabızda sessiz", async () => {
  const kv = sahteKV({ son_nabiz: String(Date.now() - 60_000), durum: "var" });
  await worker.scheduled({}, env(kv));
  assert.equal(gonderilen.length, 0);
  assert.equal(kv.yazma, 0);
});
