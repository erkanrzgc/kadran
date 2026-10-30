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

import { app, BrowserWindow, clipboard, ipcMain, nativeTheme, session } from "electron";
import { spawn } from "node:child_process";
import { release, userInfo } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { defaultAppearance, materialFor, type Material, type PlatformInfo } from "../shared/appearance.ts";
import { CHANNELS } from "../shared/channels.ts";
import { SidecarClient } from "../shared/sidecar-client.ts";
import type { SidecarProcess } from "../shared/sidecar-client.ts";
import { loadAppearance, saveAppearance } from "./appearance.ts";
import { requireCopyText, requireWindowAction } from "./ipc-validate.ts";
import { lockDownPermissions } from "./permissions.ts";
import { registerManagementHandlers } from "./management-handlers.ts";
import { loadProfiles, migrateLegacyProfiles, saveProfiles } from "./profiles.ts";
import { prepareSmokeUserData, runSmokeTest, SMOKE_TEST } from "./smoke.ts";
import { registerSidecarReads } from "./sidecar-reads.ts";
import { resolveSidecarCommand } from "./spawn-sidecar.ts";
import { createWindowHost, type WindowHost } from "./window.ts";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(here, "..", "..", "..");
const preloadPath = join(here, "..", "preload", "index.cjs");
const rendererIndex = join(here, "..", "renderer", "index.html");
const platformInfo: PlatformInfo = { platform: process.platform, release: release() };
const warn = (message: string): void => console.warn("[kadran]", message);
// Duman testi yerel malzeme istemez: gizli pencerede yakalanan görüntü ve CI aynı kalsın.
const material: Material = SMOKE_TEST ? "none" : materialFor(platformInfo);

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
  registerManagementHandlers(sidecar, syntheticFixture, async () => (await loadAppearance(userData(), warn)).language);
  registerSidecarReads({ handle: (channel, fn) => ipcMain.handle(channel, (event, ...args) => fn(event, ...args)), call: (method, params, options) => sidecar().call(method, params, options) });
  ipcMain.handle(CHANNELS.loadProfiles, () => loadProfiles(userData(), (msg) => console.warn("[profiller]", msg)));
  ipcMain.handle(CHANNELS.saveProfiles, (_event, profiles: unknown) => saveProfiles(userData(), profiles));
  ipcMain.handle(CHANNELS.loadAppearance, async () => ({
    appearance: await loadAppearance(userData(), warn),
    defaults: defaultAppearance(),
    material,
  }));
  ipcMain.handle(CHANNELS.saveAppearance, (_event, raw: unknown) => saveAppearance(userData(), raw));
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
  ipcMain.handle(CHANNELS.userName, () => userInfo().username);
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
    // Görünüm tek ve koyu: Acrylic sistem açık temada olsa da koyu kalsın.
    nativeTheme.themeSource = "dark";
    lockDownPermissions(session.defaultSession);

    // Eski adlı uygulamanın (panely-desktop) profilleri, pencere onları
    // okumadan ÖNCE taşınır (K-136). Hata uygulamayı durdurmaz.
    await migrateLegacyProfiles(userData(), join(app.getPath("appData"), "panely-desktop"), (msg) =>
      console.warn("[profiller]", msg),
    ).catch((err: unknown) => console.warn("[profiller]", (err as Error).message));

    registerHandlers();
    host = createWindowHost({ preloadPath, rendererIndex, platform: process.platform, material, showOnReady: !SMOKE_TEST, iconPath: join(here, "..", "renderer", "kadran-icon.png") });
    const win = host.open();
    if (SMOKE_TEST) void runSmokeTest(win);

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) host?.open();
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
