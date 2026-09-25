// node --test deploy/nabiz/
import { test } from "node:test";
import assert from "node:assert/strict";
import { karar, mesaj, DURUM_VAR, DURUM_YOK, DURUM_HIC } from "./karar.js";

const DK = 60_000;
const ESIK = 15 * DK;
const SIMDI = 1_800_000_000_000;

const durumlar = [
  // ad, son nabız (şimdiden önce kaç dk; null = hiç), önceki durum, beklenen eylem, beklenen yeni durum
  ["taze nabız, sağlıklıydı → sessiz", 1, DURUM_VAR, "yok", null],
  ["ilk sağlıklı gözlem → kaydet, mesaj YOK", 1, null, "yok", DURUM_VAR],
  ["eşik aşıldı, sağlıklıydı → ALARM", 16, DURUM_VAR, "alarm_yok", DURUM_YOK],
  ["eşik aşıldı, zaten alarmda → sessiz (kenar)", 30, DURUM_YOK, "yok", null],
  ["eşik aşıldı, hiç durum yok → ALARM", 16, null, "alarm_yok", DURUM_YOK],
  ["nabız geri geldi → DÜZELDİ", 2, DURUM_YOK, "duzeldi", DURUM_VAR],
  ["hiç nabız yok → bir kez söyle", null, null, "alarm_hic", DURUM_HIC],
  ["hiç nabız yok, zaten söylendi → sessiz", null, DURUM_HIC, "yok", null],
  ["hiç gelmemişti, ilk nabız geldi → DÜZELDİ", 1, DURUM_HIC, "duzeldi", DURUM_VAR],
];

for (const [ad, dkOnce, onceki, eylem, yeni] of durumlar) {
  test(ad, () => {
    const son = dkOnce === null ? null : SIMDI - dkOnce * DK;
    assert.deepEqual(karar(SIMDI, son, onceki, ESIK), { eylem, yeniDurum: yeni });
  });
}

// Sınır: eşik TAM sınırda alarm vermemeli; bir milisaniye sonrası vermeli.
// Kontrol grubu: iki taraf birlikte, `>` ile `>=` karışırsa biri düşer.
test("tam eşikte alarm YOK, bir ms sonra VAR", () => {
  assert.equal(karar(SIMDI, SIMDI - ESIK, DURUM_VAR, ESIK).eylem, "yok");
  assert.equal(karar(SIMDI, SIMDI - ESIK - 1, DURUM_VAR, ESIK).eylem, "alarm_yok");
});

// KV'den bozuk bir değer gelirse (ör. "abc" → NaN) sessizce "taze"
// sayılmamalı: NaN ile her karşılaştırma false döner ve alarm hiç
// çalmazdı.
test("bozuk zaman damgası taze SAYILMAZ", () => {
  assert.equal(karar(SIMDI, NaN, DURUM_VAR, ESIK).eylem, "alarm_hic");
});

test("mesajlar sunucu adını ve süreyi taşıyor", () => {
  assert.match(mesaj("alarm_yok", "panely-test", 17), /NABIZ YOK — panely-test[\s\S]*17 dakikadır/);
  assert.match(mesaj("duzeldi", "panely-test", null), /GERİ GELDİ — panely-test/);
  assert.throws(() => mesaj("yok", "x", 0));
});
