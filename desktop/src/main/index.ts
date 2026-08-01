/**
 * Electron ana süreci.
 *
 * `panely sidecar`'ı çocuk süreç olarak başlatır ve renderer'a yalnızca
 * dar bir salt okunur yüzey açar (bkz. src/preload/index.cjs).
 *
 * # Güvenlik duruşu
 *
 * Sunucu tarafındaki ayrıcalık ayrımını istemci tarafından delmemek için:
 * contextIsolation açık, nodeIntegration kapalı, sandbox açık, uzak
 * içerik yüklenmiyor ve CSP ile satır içi script yasak. Yeni pencere
 * açma ve gezinme engelli — bir bağımlılık ya da kazayla eklenen bağlantı
 * uygulamayı rastgele bir siteye taşıyamaz.
 */

import { app, BrowserWindow, ipcMain, shell } from "electron";
import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { SidecarClient } from "../shared/sidecar-client.ts";
import type { SidecarProcess } from "../shared/sidecar-client.ts";
import { resolveSidecarCommand } from "./spawn-sidecar.ts";
import { loadProfiles, saveProfiles } from "./profiles.ts";
import { CHANNELS } from "../shared/channels.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");

/**
 * Duman testi kipi.
 *
 * Pencereyi gösterme, yüklendiğini ve köprünün yerinde olduğunu doğrula,
 * çık. Bir GUI'nin "açılıyor mu" sorusu ancak çalıştırılarak yanıtlanır;
 * elle bakmak CI'da tekrarlanamaz ve bu projede ölçülmeyen iddia
 * sayılmıyor.
 */
const SMOKE_TEST = process.env.PANELY_SMOKE_TEST === "1";

let client: SidecarClient | null = null;

function sidecar(): SidecarClient {
  if (client) return client;

  const command = resolveSidecarCommand({
    isPackaged: app.isPackaged,
    resourcesPath: process.resourcesPath,
    repoRoot,
  });

  client = new SidecarClient(
    () =>
      spawn(command.command, command.args, {
        stdio: ["pipe", "pipe", "pipe"],
        // Kabuk YOK: argümanlar doğrudan geçiyor. shell:true olsaydı
        // yoldaki bir boşluk ya da özel karakter komut enjeksiyonuna
        // dönüşebilirdi.
        shell: false,
      }) as unknown as SidecarProcess,
    {
      onDiagnostic: (line) => console.warn("[sidecar]", line),
    },
  );
  return client;
}

function createWindow(): void {
  const window = new BrowserWindow({
    width: 1100,
    height: 760,
    minWidth: 720,
    minHeight: 520,
    backgroundColor: "#0d1117",
    title: "Panely",
    show: false,
    webPreferences: {
      preload: join(here, "..", "preload", "index.cjs"),
      // Üçü birlikte renderer'ı Node'dan tamamen ayırır. Bunlar
      // Electron'un varsayılanları ama AÇIKÇA yazılıyorlar: varsayılana
      // güvenmek, bir sürüm yükseltmesinde sessizce değişebilir.
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webviewTag: false,
    },
  });

  // Yalnızca hazır olunca göster: boş beyaz bir pencere yanıp sönmesin.
  window.once("ready-to-show", () => {
    if (!SMOKE_TEST) window.show();
  });

  if (SMOKE_TEST) void runSmokeTest(window);

  // Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa kullanıcının
  // tarayıcısında açılır; uygulama penceresi hiçbir zaman uzak bir
  // içeriğe dönüşemez.
  window.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("https://")) void shell.openExternal(url);
    return { action: "deny" };
  });
  window.webContents.on("will-navigate", (event) => event.preventDefault());

  void window.loadFile(join(here, "..", "renderer", "index.html"));
}

/**
 * runSmokeTest, pencerenin gerçekten yüklendiğini doğrular ve çıkar.
 *
 * Üç şey sınanıyor:
 *
 *  1. Renderer yükleniyor mu (CSP ihlali ya da eksik dosya yükü keser).
 *  2. Preload köprüsü yerinde mi — `window.panely` ve beklenen metotlar.
 *     Sandbox açıkken preload'un ESM olması bu adımı sessizce düşürür.
 *  3. Konsola hata düşüyor mu.
 *
 * Çıkış kodu 0 = geçti, 1 = kaldı.
 */
async function runSmokeTest(window: BrowserWindow): Promise<void> {
  const problems: string[] = [];

  window.webContents.on("console-message", (_event, level, message) => {
    // level 3 = error
    if (level >= 3) problems.push(`konsol hatası: ${message}`);
  });
  window.webContents.on("did-fail-load", (_event, code, description) => {
    problems.push(`sayfa yüklenemedi (${code}): ${description}`);
  });
  window.webContents.on("preload-error", (_event, path, error) => {
    problems.push(`preload hatası (${path}): ${error.message}`);
  });

  await new Promise<void>((done) => {
    window.webContents.once("did-finish-load", () => done());
    setTimeout(() => {
      problems.push("sayfa 20 saniyede yüklenmedi");
      done();
    }, 20_000);
  });

  // Renderer'ın gördüğü yüzeyi renderer'a sorarak doğruluyoruz: ana
  // süreçten bakmak preload'un gerçekten çalıştığını kanıtlamazdı.
  try {
    const surface = (await window.webContents.executeJavaScript(
      "Object.keys(window.panely ?? {}).sort()",
    )) as string[];

    const expected = [
      "auditList",
      "auditVerify",
      "loadProfiles",
      "saveProfiles",
      "status",
      "version",
    ];
    if (JSON.stringify(surface) !== JSON.stringify(expected)) {
      problems.push(
        `köprü yüzeyi beklenenden farklı: ${JSON.stringify(surface)} != ${JSON.stringify(expected)}`,
      );
    }

    // Renderer Node'a ERİŞEMEMELİ. Erişebiliyorsa contextIsolation ya da
    // sandbox bir yerde kapanmış demektir ve ürünün ayrıcalık ayrımı
    // istemci tarafından delinmiş olur.
    const leaked = (await window.webContents.executeJavaScript(
      "[typeof require, typeof process, typeof module].join(',')",
    )) as string;
    if (leaked !== "undefined,undefined,undefined") {
      problems.push(`renderer'a Node sızdı: ${leaked}`);
    }
  } catch (err) {
    problems.push(`köprü sorgulanamadı: ${(err as Error).message}`);
  }

  if (problems.length === 0) {
    console.log("duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor");
    app.exit(0);
    return;
  }
  for (const problem of problems) console.error("duman testi:", problem);
  app.exit(1);
}

// ── IPC işleyicileri ─────────────────────────────────────────────────
//
// Her biri renderer'dan gelen girdiyi doğrular. Renderer güvenilmez bir
// kaynak olarak ele alınıyor — contextIsolation onu Node'dan ayırıyor ama
// gönderdiği veriyi doğrulamıyor.

function requireTarget(value: unknown): string {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error("hedef boş olamaz");
  }
  if (value.length > 500) {
    throw new Error("hedef çok uzun");
  }
  return value.trim();
}

function registerHandlers(): void {
  ipcMain.handle(CHANNELS.version, () => sidecar().call("version"));

  ipcMain.handle(CHANNELS.status, (_event, target: unknown) =>
    sidecar().call("status", { target: requireTarget(target) }),
  );

  ipcMain.handle(
    CHANNELS.auditList,
    (_event, target: unknown, afterSeq: unknown, limit: unknown) =>
      sidecar().call("audit.list", {
        target: requireTarget(target),
        after_seq: toUint(afterSeq, 0),
        limit: toUint(limit, 50),
      }),
  );

  ipcMain.handle(CHANNELS.auditVerify, (_event, target: unknown) =>
    sidecar().call("audit.verify", { target: requireTarget(target) }),
  );

  ipcMain.handle(CHANNELS.loadProfiles, () =>
    loadProfiles(app.getPath("userData"), (msg) => console.warn("[profiller]", msg)),
  );

  ipcMain.handle(CHANNELS.saveProfiles, (_event, profiles: unknown) =>
    saveProfiles(app.getPath("userData"), profiles),
  );
}

function toUint(value: unknown, fallback: number): number {
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n) || n < 0) return fallback;
  return Math.floor(n);
}

// ── Yaşam döngüsü ────────────────────────────────────────────────────

// Tek örnek: ikinci bir kopya aynı profil dosyasına yazmasın.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => {
    const [window] = BrowserWindow.getAllWindows();
    if (window) {
      if (window.isMinimized()) window.restore();
      window.focus();
    }
  });

  void app.whenReady().then(() => {
    registerHandlers();
    createWindow();

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  });

  app.on("window-all-closed", () => {
    if (process.platform !== "darwin") app.quit();
  });

  // Sidecar alt süreci uygulamayla birlikte ölmeli; yoksa arkada açık
  // SSH bağlantıları taşıyan yetim süreçler birikir.
  app.on("before-quit", () => {
    client?.close();
    client = null;
  });
}
