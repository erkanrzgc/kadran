/**
 * Electron ana süreci.
 *
 * `kadran sidecar`'ı çocuk süreç olarak başlatır ve renderer'a yalnızca
 * dar bir yüzey açar (bkz. src/preload/index.cjs).
 *
 * # Güvenlik duruşu
 *
 * Sunucu tarafındaki ayrıcalık ayrımını istemci tarafından delmemek için:
 * contextIsolation açık, nodeIntegration kapalı, sandbox açık
 * (window-options.ts), uzak içerik yüklenmiyor, CSP ile satır içi script
 * yasak, gezinme ve yeni pencere engelli (window.ts) ve izinler kapalı;
 * yalnız yazı tipi listesine izin var (permissions.ts).
 */

import { app, BrowserWindow, clipboard, ipcMain, session } from "electron";
import { spawn } from "node:child_process";
import { release } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { defaultAppearance, modesFor, type PlatformInfo } from "../shared/appearance.ts";
import { CHANNELS } from "../shared/channels.ts";
import { SidecarClient } from "../shared/sidecar-client.ts";
import type { SidecarProcess } from "../shared/sidecar-client.ts";
import { loadAppearance, saveAppearance } from "./appearance.ts";
import { requireCopyText, requireTarget, requireWindowAction, toUint } from "./ipc-validate.ts";
import { lockDownPermissions } from "./permissions.ts";
import { registerManagementHandlers } from "./management-handlers.ts";
import { loadProfiles, migrateLegacyProfiles, saveProfiles } from "./profiles.ts";
import { prepareSmokeUserData, runSmokeTest, SMOKE_TEST } from "./smoke.ts";
import { resolveSidecarCommand } from "./spawn-sidecar.ts";
import { createWindowHost, type WindowHost } from "./window.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const preloadPath = join(here, "..", "preload", "index.cjs");
const rendererIndex = join(here, "..", "renderer", "index.html");
const platformInfo: PlatformInfo = { platform: process.platform, release: release() };
const warn = (message: string): void => console.warn("[kadran]", message);

// Kullanıcı verisi dizini whenReady'den ÖNCE belirlenmeli.
if (SMOKE_TEST) prepareSmokeUserData();

let client: SidecarClient | null = null;
let host: WindowHost | null = null;

function sidecar(): SidecarClient {
  if (client) return client;
  const command = resolveSidecarCommand({ isPackaged: app.isPackaged, resourcesPath: process.resourcesPath, repoRoot });
  client = new SidecarClient(
    () =>
      spawn(command.command, command.args, {
        stdio: ["pipe", "pipe", "pipe"],
        // Kabuk YOK: argümanlar doğrudan geçiyor. shell:true olsaydı
        // yoldaki bir boşluk ya da özel karakter komut enjeksiyonuna
        // dönüşebilirdi.
        shell: false,
      }) as unknown as SidecarProcess,
    { onDiagnostic: (line) => console.warn("[sidecar]", line) },
  );
  return client;
}

function userData(): string {
  return app.getPath("userData");
}

// ── IPC işleyicileri ─────────────────────────────────────────────────
//
// Her biri renderer'dan gelen girdiyi doğrular (ipc-validate.ts).

function registerHandlers(): void {
  const command = resolveSidecarCommand({ isPackaged: app.isPackaged, resourcesPath: process.resourcesPath, repoRoot });
  const syntheticFixture = SMOKE_TEST && !app.isPackaged && command.args.some((arg) => resolve(arg) === join(repoRoot, "desktop", "test", "fixtures", "fake-sidecar.mjs"));
  registerManagementHandlers(sidecar, syntheticFixture, async () => (await loadAppearance(userData(), platformInfo, warn)).language);
  ipcMain.handle(CHANNELS.version, () => sidecar().call("version"));
  ipcMain.handle(CHANNELS.status, (_event, target: unknown) =>
    sidecar().call("status", { target: requireTarget(target) }),
  );
  ipcMain.handle(CHANNELS.auditList, (_event, target: unknown, afterSeq: unknown, limit: unknown) =>
    sidecar().call("audit.list", { target: requireTarget(target), after_seq: toUint(afterSeq, 0), limit: toUint(limit, 50) }),
  );
  ipcMain.handle(CHANNELS.auditVerify, (_event, target: unknown) =>
    sidecar().call("audit.verify", { target: requireTarget(target) }),
  );
  ipcMain.handle(CHANNELS.loadProfiles, () => loadProfiles(userData(), (msg) => console.warn("[profiller]", msg)));
  ipcMain.handle(CHANNELS.saveProfiles, (_event, profiles: unknown) => saveProfiles(userData(), profiles));
  ipcMain.handle(CHANNELS.loadAppearance, async () => ({
    appearance: await loadAppearance(userData(), platformInfo, warn),
    defaults: defaultAppearance(platformInfo),
    modes: [...modesFor(platformInfo)],
  }));
  ipcMain.handle(CHANNELS.saveAppearance, async (_event, raw: unknown) => {
    const saved = await saveAppearance(userData(), raw, platformInfo);
    host?.applyMode(saved.mode);
    return saved;
  });
  ipcMain.handle(CHANNELS.windowControl, (event, action: unknown) => {
    const what = requireWindowAction(action);
    const win = BrowserWindow.fromWebContents(event.sender);
    if (!win) return;
    if (what === "minimize") win.minimize();
    else if (what === "maximize") {
      if (win.isMaximized()) win.unmaximize();
      else win.maximize();
    } else win.close();
  });
  ipcMain.handle(CHANNELS.copyText, (_event, text: unknown) => {
    clipboard.writeText(requireCopyText(text));
  });
}

// ── Yaşam döngüsü ────────────────────────────────────────────────────

// Tek örnek: ikinci bir kopya aynı profil dosyasına yazmasın.
if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => {
    const win = host?.current();
    if (win) {
      if (win.isMinimized()) win.restore();
      win.focus();
    }
  });

  void app.whenReady().then(async () => {
    lockDownPermissions(session.defaultSession);

    // Eski adlı uygulamanın (panely-desktop) profilleri, pencere onları
    // okumadan ÖNCE taşınır (K-136). Hata uygulamayı durdurmaz.
    await migrateLegacyProfiles(userData(), join(app.getPath("appData"), "panely-desktop"), (msg) =>
      console.warn("[profiller]", msg),
    ).catch((err: unknown) => console.warn("[profiller]", (err as Error).message));

    registerHandlers();
    const appearance = await loadAppearance(userData(), platformInfo, warn);
    host = createWindowHost({ preloadPath, rendererIndex, platform: process.platform, showOnReady: !SMOKE_TEST, iconPath: join(here, "..", "renderer", "kadran-logo.png") });
    const win = host.open(appearance.mode);
    if (SMOKE_TEST) void runSmokeTest(win);

    // Pencere kapalıyken Dock'tan dönüldüğünde kayıtlı mod diskten okunur;
    // başlangıçta okunan mod Ayarlar'da değişmiş olabilir.
    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length > 0) return;
      void loadAppearance(userData(), platformInfo, warn).then((a) => host?.open(a.mode));
    });
  }).catch((err: unknown) => {
    console.error("[kadran] başlatılamadı:", err);
    app.exit(1);
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
