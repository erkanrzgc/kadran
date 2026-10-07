/**
 * Kaynak bütünlüğü doğrulama: SHA-256 özetler sabitlenmiş commit'lere karşı.
 * Sabitlenmiş commit'ler GitHub'da değişmezdir; bu özetler aktarım hatasını
 * veya kaynak değişikliğini algılayarak başarısız kapatmayı sağlar.
 *
 * Pure functions — ağ erişimi yok, dış yan etki yok. Testlenebilir.
 */

import { createHash } from "node:crypto";

/**
 * SHA-256 özetini doğrula.
 * @param {Buffer|string} data — ham baytlar veya UTF-8 dizesi
 * @param {string} key — EXPECTED_SHA256 anahtarı (örnek: "activity" veya "LICENSE:lucide")
 * @param {object} expectedMap — { key: "hex hash" } eşlemesi
 * @throws Özet uyuşmadığında
 */
export function verifySha256(data, key, expectedMap) {
  const expected = expectedMap[key];
  if (!expected) {
    throw new Error(`Eksik özet anahtarı: ${key}`);
  }

  const buf = typeof data === "string" ? Buffer.from(data, "utf8") : data;
  const actual = createHash("sha256").update(buf).digest("hex");

  if (actual !== expected) {
    throw new Error(
      `Bütünlük hatası: ${key}\n` +
      `  Beklenen: ${expected}\n` +
      `  Gerçek:   ${actual}`
    );
  }
}

/**
 * Beklenenen özet anahtar setini doğrula.
 * @param {string[]} names — ikon adları
 * @param {object} expectedMap — { key: "hex hash" } eşlemesi
 * @throws Eksik veya fazla anahtar varsa
 */
export function validateKeySet(names, expectedMap) {
  const expected = new Set([...names, "LICENSE:pqoqubbw", "LICENSE:lucide"]);
  const actual = new Set(Object.keys(expectedMap));

  if (expected.size !== actual.size || ![...expected].every((k) => actual.has(k))) {
    const missing = [...expected].filter((k) => !actual.has(k));
    const extra = [...actual].filter((k) => !expected.has(k));
    const msg = [];
    if (missing.length > 0) msg.push(`Eksik: ${missing.join(", ")}`);
    if (extra.length > 0) msg.push(`Fazla: ${extra.join(", ")}`);
    throw new Error(`Özet anahtar seti uyuşmuyor:\n  ${msg.join("\n  ")}`);
  }
}
