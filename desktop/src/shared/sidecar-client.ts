/**
 * `panely sidecar` alt sürecine JSON-RPC 2.0 istemcisi.
 *
 * # Neden Electron'a bağımlı değil?
 *
 * Bu modül `spawn` işlevini DIŞARIDAN alır. Böylece Electron olmadan,
 * sahte bir alt süreçle test edilebiliyor — ve asıl mesele şu: bu
 * makinede GUI'nin konuşabileceği gerçek bir sunucu yok (Go, Windows'ta
 * unix soketine bağlanamıyor; SSH hedefi de henüz kurulmadı). Test
 * edilemeyen bir katman yazmak yerine, bağımlılığı tersine çevirip
 * mantığı sınanabilir tuttuk.
 *
 * # Çerçeveleme
 *
 * Her istek tek satır JSON, her yanıt tek satır JSON. Yanıtlar SIRA DIŞI
 * dönebilir — bu varsayım değil, ölçüm: sidecar istekleri eşzamanlı
 * işliyor ve arka arkaya gönderilen iki `version` çağrısında id 2, id
 * 1'den önce döndü. Eşleştirme bu yüzden `id` üzerinden yapılıyor.
 */

/** JSON-RPC 2.0 protokol sürümü. */
const JSONRPC_VERSION = "2.0";

/**
 * Tampon üst sınırı.
 *
 * Bozuk ya da sonsuz çıktı üreten bir alt süreç GUI'nin belleğini
 * tüketmemeli. Gerçek yanıtlar birkaç kilobayt.
 */
const MAX_BUFFER_BYTES = 8 * 1024 * 1024;

/** Varsayılan istek süre sınırı. */
const DEFAULT_TIMEOUT_MS = 35_000;

export interface SidecarProcess {
  readonly stdin: {
    write(chunk: string): unknown;
    end?(): unknown;
  };
  readonly stdout: {
    on(event: "data", listener: (chunk: Buffer | string) => void): unknown;
  };
  readonly stderr?: {
    on(event: "data", listener: (chunk: Buffer | string) => void): unknown;
  };
  on(event: "exit", listener: (code: number | null) => void): unknown;
  on(event: "error", listener: (err: Error) => void): unknown;
  kill(): unknown;
}

export type SpawnSidecar = () => SidecarProcess;

export interface SidecarOptions {
  /** Tek bir çağrı için süre sınırı (ms). */
  requestTimeoutMs?: number;
  /** Tanılama satırları için geri çağırım. stderr protokol kanalı değildir. */
  onDiagnostic?: (line: string) => void;
}

interface RpcError {
  code: number;
  message: string;
  data?: string;
}

interface RpcResponse {
  jsonrpc?: string;
  id?: number | string | null;
  result?: unknown;
  error?: RpcError;
}

/**
 * Sidecar'dan dönen yapılandırılmış hata.
 *
 * Alanlar tek tek atanıyor; TypeScript'in kısayolu olan parametre
 * özellikleri (`constructor(readonly code: number)`) KOD üretir, yalnızca
 * tip değil. Node'un tip sıyırma kipi onu reddediyor ve bu modülün
 * derlemesiz çalışabilmesi, testlerin bağımlılıksız koşmasını sağlıyor.
 */
export class SidecarError extends Error {
  readonly code?: number;
  readonly data?: string;

  constructor(message: string, code?: number, data?: string) {
    super(message);
    this.name = "SidecarError";
    this.code = code;
    this.data = data;
  }
}

interface Pending {
  resolve: (value: unknown) => void;
  reject: (err: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

export class SidecarClient {
  #spawn: SpawnSidecar;
  #timeoutMs: number;
  #onDiagnostic?: (line: string) => void;

  #proc: SidecarProcess | null = null;
  #buffer = "";
  #nextId = 1;
  #pending = new Map<number, Pending>();
  #closed = false;

  constructor(spawn: SpawnSidecar, options: SidecarOptions = {}) {
    this.#spawn = spawn;
    this.#timeoutMs = options.requestTimeoutMs ?? DEFAULT_TIMEOUT_MS;
    this.#onDiagnostic = options.onDiagnostic;
  }

  /**
   * Bir JSON-RPC metodunu çağırır.
   *
   * Alt süreç ilk çağrıda başlatılır: uygulama açılışta bir süreç
   * doğurup hiç kullanmamalı.
   */
  call<T = unknown>(method: string, params?: unknown): Promise<T> {
    if (this.#closed) {
      return Promise.reject(new SidecarError("sidecar kapatıldı"));
    }

    let proc: SidecarProcess;
    try {
      proc = this.#ensureProcess();
    } catch (err) {
      return Promise.reject(
        new SidecarError(`sidecar başlatılamadı: ${(err as Error).message}`),
      );
    }

    const id = this.#nextId++;
    const request = JSON.stringify({
      jsonrpc: JSONRPC_VERSION,
      id,
      method,
      ...(params === undefined ? {} : { params }),
    });

    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(
          new SidecarError(
            `"${method}" çağrısı ${this.#timeoutMs} ms içinde yanıtlanmadı`,
          ),
        );
      }, this.#timeoutMs);

      // unref: bekleyen bir zamanlayıcı Node sürecini ayakta tutmasın.
      timer.unref?.();

      this.#pending.set(id, {
        resolve: resolve as (value: unknown) => void,
        reject,
        timer,
      });

      try {
        proc.stdin.write(request + "\n");
      } catch (err) {
        this.#settleError(id, new SidecarError(`istek yazılamadı: ${(err as Error).message}`));
      }
    });
  }

  /** Alt süreci kapatır ve bekleyen çağrıları reddeder. */
  close(): void {
    this.#closed = true;
    const proc = this.#proc;
    this.#proc = null;

    this.#rejectAll(new SidecarError("sidecar kapatıldı"));

    if (proc) {
      try {
        proc.stdin.end?.();
      } catch {
        // Boru zaten kapanmış olabilir; kapatma yolunda hata yutmak
        // burada doğru: amacımız kaynağı bırakmak.
      }
      try {
        proc.kill();
      } catch {
        // Aynı gerekçe.
      }
    }
  }

  #ensureProcess(): SidecarProcess {
    if (this.#proc) return this.#proc;

    const proc = this.#spawn();
    this.#proc = proc;
    this.#buffer = "";

    proc.stdout.on("data", (chunk) => this.#onStdout(chunk));

    proc.stderr?.on("data", (chunk) => {
      // stderr PROTOKOL KANALI DEĞİL: sidecar tanılamayı oraya yazar.
      // Ayrıştırmaya çalışmak, tanılama satırlarını bozuk yanıt sanmak
      // olurdu.
      const text = typeof chunk === "string" ? chunk : chunk.toString("utf8");
      for (const line of text.split("\n")) {
        if (line.trim()) this.#onDiagnostic?.(line);
      }
    });

    proc.on("exit", (code) => {
      this.#proc = null;
      this.#rejectAll(
        new SidecarError(`sidecar süreci sonlandı (çıkış kodu ${code ?? "bilinmiyor"})`),
      );
    });

    proc.on("error", (err) => {
      this.#proc = null;
      this.#rejectAll(new SidecarError(`sidecar süreci hata verdi: ${err.message}`));
    });

    return proc;
  }

  #onStdout(chunk: Buffer | string): void {
    this.#buffer += typeof chunk === "string" ? chunk : chunk.toString("utf8");

    if (this.#buffer.length > MAX_BUFFER_BYTES) {
      this.#buffer = "";
      this.#rejectAll(
        new SidecarError("sidecar çıktısı tampon sınırını aştı — süreç kapatılıyor"),
      );
      this.close();
      return;
    }

    // Son parça eksik olabilir: bir satır iki okuma arasında bölünebilir.
    // Sondaki tamamlanmamış parça tamponda bekletiliyor.
    const lines = this.#buffer.split("\n");
    this.#buffer = lines.pop() ?? "";

    for (const line of lines) {
      if (line.trim()) this.#handleLine(line);
    }
  }

  #handleLine(line: string): void {
    let resp: RpcResponse;
    try {
      resp = JSON.parse(line) as RpcResponse;
    } catch {
      // Çözümlenemeyen bir satır, bekleyen çağrıları ÖLDÜRMEMELİ:
      // hangi id'ye ait olduğunu bilmiyoruz ve hepsini reddetmek
      // sağlıklı çağrıları da düşürürdü.
      this.#onDiagnostic?.(`çözümlenemeyen sidecar satırı: ${line}`);
      return;
    }

    if (typeof resp.id !== "number") {
      this.#onDiagnostic?.(`id'siz sidecar yanıtı yok sayıldı: ${line}`);
      return;
    }

    const pending = this.#pending.get(resp.id);
    if (!pending) {
      // Süre aşımına uğramış bir çağrının geç gelen yanıtı olabilir.
      return;
    }
    this.#pending.delete(resp.id);
    clearTimeout(pending.timer);

    if (resp.error) {
      pending.reject(
        new SidecarError(resp.error.message, resp.error.code, resp.error.data),
      );
      return;
    }
    pending.resolve(resp.result);
  }

  #settleError(id: number, err: Error): void {
    const pending = this.#pending.get(id);
    if (!pending) return;
    this.#pending.delete(id);
    clearTimeout(pending.timer);
    pending.reject(err);
  }

  #rejectAll(err: Error): void {
    for (const [id, pending] of this.#pending) {
      this.#pending.delete(id);
      clearTimeout(pending.timer);
      pending.reject(err);
    }
  }
}
