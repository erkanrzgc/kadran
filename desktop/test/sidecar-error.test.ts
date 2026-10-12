/**
 * Sidecar hatasının kullanıcıya giden metni: gerçek sebep (data) görünür,
 * sırlar silinir, biçim/kontrol karakterleri atılır, uzunluk sınırlıdır.
 */

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { CHANNELS } from "../src/shared/channels.ts";
import { SidecarError } from "../src/shared/sidecar-client.ts";
import { registerSidecarReads } from "../src/main/sidecar-reads.ts";
import { callForUi, MAX_ERROR_CHARS, sidecarErrorText, userFacingError } from "../src/main/sidecar-error.ts";

const here = dirname(fileURLToPath(import.meta.url));
// Gerçek sidecar'ın hata biçimi: cmd/kadran/sidecar_error_test.go bu dosyayı denetler.
const golden = JSON.parse(readFileSync(join(here, "fixtures", "sidecar-error.json"), "utf8")) as Record<"connect" | "call", { code: number; message: string; data: string }>;

const fail = (data: string, message = "call failed") => new SidecarError(message, -32603, data);

test("gerçek hata biçimi: sınıf adı + gerçek sebep birlikte gösterilir", () => {
  for (const kind of ["connect", "call"] as const) {
    const g = golden[kind];
    const text = sidecarErrorText(new SidecarError(g.message, g.code, g.data));
    assert.equal(text, `${g.message}: ${g.data}`);
    assert.ok(text.includes(g.data), "gerçek sebep kayboldu");
  }
});

test("data yoksa yalnız message; SidecarError olmayan hatalar da arıtılır", () => {
  assert.equal(sidecarErrorText(new SidecarError("could not parse JSON", -32700)), "could not parse JSON");
  assert.equal(sidecarErrorText(new Error("the sidecar exited (code 1)")), "the sidecar exited (code 1)");
  assert.equal(sidecarErrorText("düz metin"), "düz metin");
});

const REDACTION: [string, string, string][] = [
  ["URL kimlik bilgisi", "clone https://user:hunter2@git.example.com/org/repo.git failed", "clone https://***@git.example.com/org/repo.git failed"],
  ["yalnız kullanıcı adı", "ssh://deploy@host.example/x", "ssh://***@host.example/x"],
  ["x-access-token", "fatal: x-access-token:abc123@github.com denied", "fatal: x-access-token:***@github.com denied"],
  ["x-access-token URL içinde", "https://x-access-token:ghs_AAA111@github.com/o/r", "https://***@github.com/o/r"],
  ["ghp_", "token ghp_AbC123xyz rejected", "token *** rejected"],
  ["gho_", "gho_AbC123 bad", "*** bad"],
  ["ghu_", "ghu_AbC123 bad", "*** bad"],
  ["ghs_", "ghs_AbC123 bad", "*** bad"],
  ["ghr_", "ghr_AbC123 bad", "*** bad"],
  ["github_pat_", "pat github_pat_11ABCDEF_0123456789abcdef used", "pat *** used"],
  ["sıfır genişlikli karakterle bölünmüş jeton", "tok gh​p_secretvalue99 x", "tok *** x"],
  ["sır içermeyen metin değişmez", "connection refused at host:22", "connection refused at host:22"],
];

for (const [name, input, want] of REDACTION) {
  test(`redaksiyon: ${name}`, () => {
    const text = sidecarErrorText(fail(input));
    assert.equal(text, `call failed: ${want}`);
    assert.doesNotMatch(text, /hunter2|abc123|AbC123|secretvalue99|11ABCDEF/);
  });
}

test("kontrol ve biçim (bidi) karakterleri atılır, satır sonu boşluk olur", () => {
  const bidi = ["‪", "‫", "‬", "‭", "‮", "⁦", "⁧", "⁨", "⁩", "‎", "‏", "؜", "​", "﻿"];
  for (const ch of bidi) assert.equal(sidecarErrorText(fail(`a${ch}b`)), "call failed: ab", `U+${ch.charCodeAt(0).toString(16)}`);
  assert.equal(sidecarErrorText(fail("a\u0000b\u001bc\u007fd")), "call failed: abcd");
  assert.equal(sidecarErrorText(fail("line1\nline2\r\n\tline3")), "call failed: line1 line2 line3");
});

test("uzunluk sınırı: 500 kod noktası + …", () => {
  const exact = "x".repeat(MAX_ERROR_CHARS - "call failed: ".length);
  assert.equal(sidecarErrorText(fail(exact)).length, MAX_ERROR_CHARS);
  assert.equal(sidecarErrorText(fail(exact)).endsWith("…"), false);
  const long = sidecarErrorText(fail("y".repeat(5000)));
  assert.equal([...long].length, MAX_ERROR_CHARS + 1);
  assert.ok(long.endsWith("…"));
  // Vekil çiftler (emoji) ortadan bölünmez.
  const emoji = sidecarErrorText(fail("😀".repeat(1000)));
  assert.equal([...emoji].length, MAX_ERROR_CHARS + 1);
  assert.doesNotMatch(emoji, /[\uD800-\uDBFF](?![\uDC00-\uDFFF])/);
});

test("userFacingError / callForUi: ham SidecarError (data alanı) renderer'a gitmez", async () => {
  const err = userFacingError(fail("reason"));
  assert.equal(err instanceof SidecarError, false);
  assert.equal(err.message, "call failed: reason");
  await assert.rejects(callForUi(async () => { throw fail("boom https://u:p@h/x"); }, "status"), (e: unknown) => {
    assert.equal((e as Error).message, "call failed: boom https://***@h/x");
    assert.equal(Object.hasOwn(e as object, "data"), false);
    return true;
  });
  assert.equal(await callForUi(async () => 7, "version"), 7);
});

test("durum, sürüm ve denetim kanalları da arıtılmış hata döndürür", async () => {
  const handlers = new Map<string, (event: unknown, ...args: unknown[]) => unknown>();
  registerSidecarReads({
    handle: (channel, fn) => { handlers.set(channel, fn); },
    call: async () => { throw fail("dial https://u:pw@host failed‮ ghp_zzz999"); },
  });
  const target = "kadran-client@203.0.113.7";
  const cases: [keyof typeof CHANNELS, unknown[]][] = [
    ["version", []], ["status", [target]], ["auditList", [target, 0, 50]], ["auditVerify", [target]],
  ];
  for (const [name, args] of cases) {
    const handler = handlers.get(CHANNELS[name]);
    assert.ok(handler, `kayıtlı değil: ${name}`);
    await assert.rejects(Promise.resolve().then(() => handler({}, ...args)), (e: unknown) => {
      assert.equal((e as Error).message, "call failed: dial https://***@host failed ***");
      return true;
    }, name);
  }
});

test("yönetim kancaları hatayı errorText ile gösterir (Electron öneki temizlenir)", () => {
  for (const file of ["useManagementOperation.ts", "useManagementQuery.ts"]) {
    const source = readFileSync(join(here, "..", "src", "renderer", "hooks", file), "utf8");
    assert.match(source, /errorText\(/, file);
    assert.doesNotMatch(source, /error\.message/, file);
  }
});
