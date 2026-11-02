/**
 * Ana süreç ile preload arasındaki IPC kanal adları.
 *
 * # Neden ayrı bir dosya?
 *
 * Preload betiği CommonJS olmak ZORUNDA: sandbox açıkken Electron ESM
 * preload çalıştırmıyor. Ana süreç ise ESM. İkisi aynı modülü paylaşamıyor,
 * bu yüzden kanal adları preload tarafında elle yazılıyor
 * (src/preload/index.cjs).
 *
 * Kopya bir liste sessizce ayrışabilir — bir kanal yeniden adlandırılır,
 * diğer taraf güncellenmez ve çağrı çalışma anında "no handler" ile ölür.
 * Bu yüzden test/channels.test.ts iki listenin aynı olduğunu doğruluyor:
 * ayrışma derleme değil ama TEST hatası veriyor.
 */

export const CHANNELS = {
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
} as const;

export type ChannelName = (typeof CHANNELS)[keyof typeof CHANNELS];
