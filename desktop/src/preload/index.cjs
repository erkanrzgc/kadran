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
 * Burada yalnızca altı işlem var ve hiçbiri sunucunun durumunu
 * DEĞİŞTİRMİYOR. Faz 1'de durum değiştiren işlemler geldiğinde (dağıtım,
 * geri alma, silme) ayrı bir onay mekanizması gerekecek; o zaman bu
 * yorumun da güncellenmesi gerekiyor.
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
  version: "panely:version",
  status: "panely:status",
  auditList: "panely:audit-list",
  auditVerify: "panely:audit-verify",
  loadProfiles: "panely:profiles-load",
  saveProfiles: "panely:profiles-save",
};

contextBridge.exposeInMainWorld("panely", {
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
   * Profil bir SIR DEĞİLDİR: yalnızca `panely-client@1.2.3.4` gibi bir
   * hedef dizesi. Anahtar malzemesi ssh-agent'ta kalır ve bu uygulamaya
   * hiç girmez — bu yüzden işletim sistemi anahtarlığı kullanılmıyor.
   */
  saveProfiles: (profiles) => ipcRenderer.invoke(CHANNELS.saveProfiles, profiles),
});
