/**
 * Renderer'a açılan API yüzeyi.
 *
 * # Bu dosya bir beyaz listedir
 *
 * `exec.proto` ile aynı fikir: yüzeyin KENDİSİ beyaz liste. Renderer'a
 * genel bir "şu metodu çağır" köprüsü vermek, sunucudaki tipli RPC
 * sınırını istemci tarafından delmek olurdu — kötü bir bağımlılık ya da
 * bir XSS, sidecar'a istediği çağrıyı yaptırabilirdi.
 *
 * Yönetim işlemleri de adlandırılmış tipli kanallardır; genel RPC,
 * kabuk veya dosya erişimi yoktur. Geri alma ve silme ana süreçte
 * uygulama adı ile doğrulanır; girdi boyutları ayrıca sınırlanır.
 *
 * # Neden CommonJS?
 *
 * `sandbox: true` iken Electron ESM preload çalıştırmıyor. Sandbox'ı
 * kapatmak yerine bu dosyayı CJS tutuyoruz: kum havuzu, renderer'ın
 * Node'a erişmesini engelleyen asıl duvar ve bir uzantı yüzünden
 * bırakılmaz.
 *
 * Kanal adları src/shared/channels.ts ile aynı olmak zorunda; iki liste
 * test/channels.test.ts tarafından karşılaştırılıyor.
 */

"use strict";

const { contextBridge, ipcRenderer } = require("electron");

const CHANNELS = {
  version: "kadran:version",
  status: "kadran:status",
  auditList: "kadran:audit-list",
  auditVerify: "kadran:audit-verify",
  appsList: "kadran:apps-list",
  appGet: "kadran:app-get",
  appCreate: "kadran:app-create",
  appUpdate: "kadran:app-update",
  appDelete: "kadran:app-delete",
  appDeploy: "kadran:app-deploy",
  appRollback: "kadran:app-rollback",
  appLogs: "kadran:app-logs",
  alarmsList: "kadran:alarms-list",
  backupsList: "kadran:backups-list",
  backupCreate: "kadran:backup-create",
  loadProfiles: "kadran:profiles-load",
  saveProfiles: "kadran:profiles-save",
  loadAppearance: "kadran:appearance-load",
  saveAppearance: "kadran:appearance-save",
  windowControl: "kadran:window",
  copyText: "kadran:copy",
};

contextBridge.exposeInMainWorld("kadran", {
  appsList: (target) => ipcRenderer.invoke(CHANNELS.appsList, target),
  appGet: (target, appId) => ipcRenderer.invoke(CHANNELS.appGet, target, appId),
  appCreate: (target, spec) => ipcRenderer.invoke(CHANNELS.appCreate, target, spec),
  appUpdate: (target, appId, update) => ipcRenderer.invoke(CHANNELS.appUpdate, target, appId, update),
  appDelete: (target, appId, confirmation) => ipcRenderer.invoke(CHANNELS.appDelete, target, appId, confirmation),
  appDeploy: (target, appId, ref) => ipcRenderer.invoke(CHANNELS.appDeploy, target, appId, ref),
  appRollback: (target, appId, confirmation) => ipcRenderer.invoke(CHANNELS.appRollback, target, appId, confirmation),
  appLogs: (target, appId, tail) => ipcRenderer.invoke(CHANNELS.appLogs, target, appId, tail),
  alarmsList: (target) => ipcRenderer.invoke(CHANNELS.alarmsList, target),
  backupsList: (target) => ipcRenderer.invoke(CHANNELS.backupsList, target),
  backupCreate: (target) => ipcRenderer.invoke(CHANNELS.backupCreate, target),
  /** Sidecar'ın sürüm ve protokol bilgisi. Sunucuya dokunmaz. */
  version: () => ipcRenderer.invoke(CHANNELS.version),

  /** Hedef sunucunun durumu. */
  status: (target) => ipcRenderer.invoke(CHANNELS.status, target),

  /** Denetim kayıtlarını sayfalı okur. */
  auditList: (target, afterSeq = 0, limit = 50) =>
    ipcRenderer.invoke(CHANNELS.auditList, target, afterSeq, limit),

  /** İki denetim zincirini de doğrular. */
  auditVerify: (target) => ipcRenderer.invoke(CHANNELS.auditVerify, target),

  /** Kayıtlı sunucu profillerini okur. */
  loadProfiles: () => ipcRenderer.invoke(CHANNELS.loadProfiles),

  /**
   * Sunucu profillerini kaydeder.
   *
   * Profil bir SIR DEĞİLDİR: yalnızca `kadran-client@1.2.3.4` gibi bir
   * hedef dizesi. Anahtar malzemesi ssh-agent'ta kalır ve bu uygulamaya
   * hiç girmez — bu yüzden işletim sistemi anahtarlığı kullanılmıyor.
   */
  saveProfiles: (profiles) => ipcRenderer.invoke(CHANNELS.saveProfiles, profiles),

  /** Çalışılan işletim sistemi; başlık çubuğu macOS'ta trafik ışıklarına yer bırakır. */
  platform: process.platform,

  /** Görünüm ayarları, varsayılanlar ve bu sistemde sunulan modlar. */
  loadAppearance: () => ipcRenderer.invoke(CHANNELS.loadAppearance),

  /**
   * Görünümü kaydeder. Ana süreç doğrular; mod değiştiyse pencereyi
   * yeniden kurabilir (Clear ↔ diğerleri) ya da yerinde değiştirir.
   */
  saveAppearance: (appearance) => ipcRenderer.invoke(CHANNELS.saveAppearance, appearance),

  /** Özel başlık çubuğunun düğmeleri: minimize | maximize | close. */
  windowControl: (action) => ipcRenderer.invoke(CHANNELS.windowControl, action),

  /** Parmak izi gibi kısa bir metni panoya yazar (en fazla 1024 karakter). */
  copyText: (text) => ipcRenderer.invoke(CHANNELS.copyText, text),
});
