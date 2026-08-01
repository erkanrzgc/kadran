/**
 * SidecarClient testleri.
 *
 * Bağımlılık yok: Node 24 TypeScript tiplerini kendisi sıyırıyor ve
 * test koşucusu yerleşik. `node --test desktop/test/` ile çalışır.
 *
 * Sahte alt süreç kullanılıyor — asıl neden, gerçek bir sunucunun bu
 * makinede bulunmaması değil, mantığın deterministik sınanabilmesi:
 * sıra dışı yanıt, bölünmüş satır ve süreç ölümü gibi durumları gerçek
 * bir süreçle güvenilir biçimde üretemezsiniz.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { EventEmitter } from "node:events";

import { SidecarClient, SidecarError } from "../src/shared/sidecar-client.ts";
import type { SidecarProcess } from "../src/shared/sidecar-client.ts";

/** FakeProcess, bir alt süreci taklit eder ve yazılanları kaydeder. */
class FakeProcess extends EventEmitter {
  written: string[] = [];
  killed = false;
  ended = false;

  #stdout = new EventEmitter();
  #stderr = new EventEmitter();

  readonly stdin = {
    write: (chunk: string) => {
      this.written.push(chunk);
      return true;
    },
    end: () => {
      this.ended = true;
    },
  };

  get stdout() {
    return this.#stdout;
  }
  get stderr() {
    return this.#stderr;
  }

  kill() {
    this.killed = true;
  }

  /** emit, sidecar'ın stdout'una ham metin yazar. */
  emitStdout(text: string) {
    this.#stdout.emit("data", Buffer.from(text, "utf8"));
  }

  emitStderr(text: string) {
    this.#stderr.emit("data", Buffer.from(text, "utf8"));
  }

  /** requests, yazılan istekleri çözümlenmiş olarak döndürür. */
  requests(): Array<{ id: number; method: string; params?: unknown }> {
    return this.written
      .join("")
      .split("\n")
      .filter((line) => line.trim())
      .map((line) => JSON.parse(line));
  }
}

function newClient(options = {}) {
  const proc = new FakeProcess();
  const client = new SidecarClient(
    () => proc as unknown as SidecarProcess,
    options,
  );
  return { proc, client };
}

test("istek JSON-RPC 2.0 biçiminde ve tek satır gider", async () => {
  const { proc, client } = newClient();

  const promise = client.call("version");
  const [req] = proc.requests();

  assert.equal((req as Record<string, unknown>).jsonrpc, "2.0");
  assert.equal(req.method, "version");
  assert.equal(typeof req.id, "number");
  // Çerçeveleme satır sonuna dayanıyor: istek tam olarak bir satır olmalı.
  assert.equal(proc.written.join("").split("\n").filter(Boolean).length, 1);

  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: req.id, result: { ok: 1 } }) + "\n");
  assert.deepEqual(await promise, { ok: 1 });

  client.close();
});

test("alt süreç ilk çağrıya kadar başlatılmaz", () => {
  let spawned = 0;
  const client = new SidecarClient(() => {
    spawned++;
    return new FakeProcess() as unknown as SidecarProcess;
  });

  assert.equal(spawned, 0, "istemci kurulur kurulmaz süreç doğdu");

  void client.call("version").catch(() => {});
  assert.equal(spawned, 1);

  client.close();
});

test("sıra dışı yanıtlar doğru çağrıyla eşleşir", async () => {
  // Bu varsayım değil ÖLÇÜM: sidecar istekleri eşzamanlı işliyor ve arka
  // arkaya gönderilen iki `version` çağrısında id 2, id 1'den önce döndü.
  const { proc, client } = newClient();

  const first = client.call<{ n: number }>("version");
  const second = client.call<{ n: number }>("version");
  const [r1, r2] = proc.requests();

  // Yanıtlar TERS sırada geliyor.
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: r2.id, result: { n: 2 } }) + "\n");
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: r1.id, result: { n: 1 } }) + "\n");

  assert.deepEqual(await first, { n: 1 });
  assert.deepEqual(await second, { n: 2 });

  client.close();
});

test("iki okuma arasında bölünmüş satır birleştirilir", async () => {
  const { proc, client } = newClient();

  const promise = client.call("version");
  const [req] = proc.requests();
  const payload = JSON.stringify({ jsonrpc: "2.0", id: req.id, result: { parca: true } });

  // Boru sınırları JSON sınırlarına uymaz; satır ortadan bölünebilir.
  const mid = Math.floor(payload.length / 2);
  proc.emitStdout(payload.slice(0, mid));
  proc.emitStdout(payload.slice(mid) + "\n");

  assert.deepEqual(await promise, { parca: true });
  client.close();
});

test("tek okumada gelen birden çok satır ayrı ayrı işlenir", async () => {
  const { proc, client } = newClient();

  const first = client.call("version");
  const second = client.call("version");
  const [r1, r2] = proc.requests();

  proc.emitStdout(
    JSON.stringify({ jsonrpc: "2.0", id: r1.id, result: 1 }) +
      "\n" +
      JSON.stringify({ jsonrpc: "2.0", id: r2.id, result: 2 }) +
      "\n",
  );

  assert.equal(await first, 1);
  assert.equal(await second, 2);
  client.close();
});

test("hata yanıtı SidecarError'a çevrilir", async () => {
  const { proc, client } = newClient();

  const promise = client.call("yok");
  const [req] = proc.requests();

  proc.emitStdout(
    JSON.stringify({
      jsonrpc: "2.0",
      id: req.id,
      error: { code: -32601, message: 'bilinmeyen metot "yok"', data: "ayrıntı" },
    }) + "\n",
  );

  const err = await promise.then(
    () => null,
    (e: unknown) => e as SidecarError,
  );
  assert.ok(err instanceof SidecarError, "SidecarError bekleniyordu");
  assert.equal(err.code, -32601);
  assert.equal(err.data, "ayrıntı");
  assert.match(err.message, /bilinmeyen metot/);

  client.close();
});

test("çözümlenemeyen satır diğer çağrıları öldürmez", async () => {
  const { proc, client } = newClient();
  const diagnostics: string[] = [];
  const c2 = new SidecarClient(() => proc as unknown as SidecarProcess, {
    onDiagnostic: (line) => diagnostics.push(line),
  });
  client.close();

  const promise = c2.call("version");
  const [req] = proc.requests();

  // Bozuk satır: hangi id'ye ait olduğu bilinmiyor. Hepsini reddetmek
  // sağlıklı çağrıları da düşürürdü.
  proc.emitStdout("bu JSON değil\n");
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: req.id, result: "iyi" }) + "\n");

  assert.equal(await promise, "iyi");
  assert.ok(
    diagnostics.some((d) => d.includes("çözümlenemeyen")),
    "bozuk satır tanılamaya yazılmadı",
  );

  c2.close();
});

test("bilinmeyen id yok sayılır, çökme olmaz", async () => {
  const { proc, client } = newClient();

  const promise = client.call("version");
  const [req] = proc.requests();

  // Süre aşımına uğramış bir çağrının geç gelen yanıtı böyle görünür.
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: 9999, result: "hayalet" }) + "\n");
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: req.id, result: "gerçek" }) + "\n");

  assert.equal(await promise, "gerçek");
  client.close();
});

test("süre sınırı aşılınca çağrı reddedilir", async () => {
  const { proc, client } = newClient({ requestTimeoutMs: 40 });

  const err = await client.call("version").then(
    () => null,
    (e: unknown) => e as SidecarError,
  );

  assert.ok(err instanceof SidecarError);
  assert.match(err.message, /yanıtlanmadı/);
  // Yanıt geç gelirse çökmemeli: bekleyen kayıt zaten silinmiş olmalı.
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: 1, result: "geç" }) + "\n");

  client.close();
});

test("süreç ölünce bekleyen çağrılar reddedilir", async () => {
  const { proc, client } = newClient();

  const promise = client.call("version");
  proc.emit("exit", 1);

  const err = await promise.then(
    () => null,
    (e: unknown) => e as SidecarError,
  );
  assert.ok(err instanceof SidecarError);
  assert.match(err.message, /sonland/);

  client.close();
});

test("süreç ölümünden sonraki çağrı yeni süreç başlatır", async () => {
  let spawned = 0;
  let current = new FakeProcess();
  const client = new SidecarClient(() => {
    spawned++;
    current = new FakeProcess();
    return current as unknown as SidecarProcess;
  });

  const first = client.call("version");
  current.emit("exit", 1);
  await first.catch(() => {});
  assert.equal(spawned, 1);

  // Süreç öldüyse istemci ölü kalmamalı: GUI yeniden denemeli.
  const second = client.call("version");
  assert.equal(spawned, 2, "ölü süreçten sonra yeniden başlatılmadı");

  const [req] = current.requests();
  current.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: req.id, result: "diri" }) + "\n");
  assert.equal(await second, "diri");

  client.close();
});

test("stderr protokol olarak ayrıştırılmaz, tanılamaya gider", async () => {
  const diagnostics: string[] = [];
  const proc = new FakeProcess();
  const client = new SidecarClient(() => proc as unknown as SidecarProcess, {
    onDiagnostic: (line) => diagnostics.push(line),
  });

  const promise = client.call("version");
  const [req] = proc.requests();

  proc.emitStderr("panely: ssh: Permission denied\n");
  proc.emitStdout(JSON.stringify({ jsonrpc: "2.0", id: req.id, result: "ok" }) + "\n");

  assert.equal(await promise, "ok");
  assert.ok(
    diagnostics.some((d) => d.includes("Permission denied")),
    "stderr tanılamaya iletilmedi",
  );

  client.close();
});

test("close bekleyen çağrıları reddeder ve süreci kapatır", async () => {
  const { proc, client } = newClient();

  const promise = client.call("version");
  client.close();

  const err = await promise.then(
    () => null,
    (e: unknown) => e as SidecarError,
  );
  assert.ok(err instanceof SidecarError);
  assert.ok(proc.killed, "alt süreç öldürülmedi");
  assert.ok(proc.ended, "stdin kapatılmadı");

  // Kapandıktan sonraki çağrı sessizce asılı kalmamalı.
  const after = await client.call("version").then(
    () => null,
    (e: unknown) => e as SidecarError,
  );
  assert.ok(after instanceof SidecarError);
});

test("params verilmezse istekte params alanı bulunmaz", () => {
  const { proc, client } = newClient();

  void client.call("version").catch(() => {});
  void client.call("status", { target: "erkan@sunucu" }).catch(() => {});

  const [noParams, withParams] = proc.requests();
  assert.equal("params" in noParams, false, "params gereksiz yere gönderildi");
  assert.deepEqual(withParams.params, { target: "erkan@sunucu" });

  client.close();
});
