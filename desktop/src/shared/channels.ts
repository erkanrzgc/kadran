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
  version: "panely:version",
  status: "panely:status",
  auditList: "panely:audit-list",
  auditVerify: "panely:audit-verify",
  loadProfiles: "panely:profiles-load",
  saveProfiles: "panely:profiles-save",
} as const;

export type ChannelName = (typeof CHANNELS)[keyof typeof CHANNELS];
