/**
 * SidecarClient'ı GERÇEK `panely sidecar` alt sürecine karşı çalıştırır.
 *
 * # Neden ayrı bir test?
 *
 * Birim testleri sahte bir süreç kullanıyor ve sahte süreç, gerçeğinin
 * yaptığı şeyleri yapmıyor: satırları kendi tamponlama sınırlarında
 * bölmüyor, eşzamanlı yanıt vermiyor, gerçek boru semantiği taşımıyor.
 * Sahte sürece karşı geçen bir istemci, gerçeğine karşı hâlâ bozuk
 * olabilir — Go tarafında tam olarak bu sınıf bir hata çıkmıştı (K-012).
 *
 * Binary yoksa test ATLANIYOR, uydurma bir başarı üretmiyor. Üretmek için:
 *
 *   go build -o bin/panely.exe ./cmd/panely     (Windows)
 *   go build -o bin/panely ./cmd/panely         (Linux/macOS)
 */

import { test, describe } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { SidecarClient } from "../src/shared/sidecar-client.ts";
import type { SidecarProcess } from "../src/shared/sidecar-client.ts";
import { resolveSidecarCommand } from "../src/main/spawn-sidecar.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..");

const command = resolveSidecarCommand({ isPackaged: false, repoRoot });
const binaryExists = existsSync(command.command);

describe("gerçek sidecar süreci", { skip: binaryExists ? false : "panely binary'si yok — `go build -o bin/panely.exe ./cmd/panely`" }, () => {
  function newClient() {
    return new SidecarClient(
      () =>
        spawn(command.command, command.args, {
          stdio: ["pipe", "pipe", "pipe"],
        }) as unknown as SidecarProcess,
      { requestTimeoutMs: 15_000 },
    );
  }

  test("version metodu gerçek protokol sürümünü döndürür", async () => {
    const client = newClient();
    try {
      const result = await client.call<{ protocol: number; version: string }>("version");

      assert.equal(typeof result.protocol, "number");
      assert.ok(result.protocol >= 1, `protokol sürümü beklenmedik: ${result.protocol}`);
      assert.equal(typeof result.version, "string");
    } finally {
      client.close();
    }
  });

  test("bilinmeyen metot yapılandırılmış hata döndürür", async () => {
    const client = newClient();
    try {
      const err = await client.call("kendini.imha.et").then(
        () => null,
        (e) => e as { code?: number; message: string },
      );

      assert.ok(err, "hata bekleniyordu");
      // JSON-RPC 2.0: metot bulunamadı.
      assert.equal(err.code, -32601);
    } finally {
      client.close();
    }
  });

  test("eşzamanlı çağrılar karışmadan yanıtlanır", async () => {
    // Sidecar istekleri eşzamanlı işliyor; yanıtlar sıra dışı dönebilir.
    // İstemcinin id eşleştirmesi bunu doğru yapmalı — birim testinde
    // sahte süreçle kurgulanan durum burada GERÇEKTEN oluşuyor.
    const client = newClient();
    try {
      const calls = Array.from({ length: 25 }, () =>
        client.call<{ protocol: number }>("version"),
      );
      const results = await Promise.all(calls);

      assert.equal(results.length, 25);
      for (const r of results) {
        assert.equal(typeof r.protocol, "number");
      }
    } finally {
      client.close();
    }
  });

  test("ulaşılamayan hedef hata döndürür, süreci öldürmez", async () => {
    const client = newClient();
    try {
      const err = await client
        .call("status", { target: "unix:///olmayan/panely-test.sock" })
        .then(
          () => null,
          (e) => e as { message: string },
        );
      assert.ok(err, "ulaşılamayan hedef için hata bekleniyordu");

      // Süreç hâlâ ayakta olmalı: bir hata oturumu bitirmemeli.
      const after = await client.call<{ protocol: number }>("version");
      assert.equal(typeof after.protocol, "number");
    } finally {
      client.close();
    }
  });
});
