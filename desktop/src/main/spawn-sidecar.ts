/**
 * `panely sidecar` alt sürecinin nasıl başlatılacağını çözer.
 *
 * # Hangi binary?
 *
 * Masaüstü uygulaması KULLANICININ iş istasyonunda çalışır ve sunucuya SSH
 * ile bağlanır. Yani gereken binary iş istasyonunun kendi platformuna ait:
 * Windows'ta `panely.exe`, macOS/Linux'ta `panely`.
 *
 * Bu, ilk taslaktaki bir yanlış varsayımı düzeltiyor. Go, Windows'ta unix
 * soketine BAĞLANAMIYOR (ölçüldü) — ama bu yalnızca YEREL soket yolunu
 * etkiler ve iş istasyonunda o yol zaten kullanılmaz; hedef her zaman
 * uzak bir sunucudur. Windows derlemesi SSH yolu için sorunsuz çalışır.
 *
 * WSL köprüsü yalnızca GELİŞTİRME kolaylığıdır: bu makinede yerel soket
 * yolunu denemek için. Uygulamaya gömülmüyor, ortam değişkeniyle
 * devreye giriyor.
 */

import { join } from "node:path";

export interface SidecarCommand {
  command: string;
  args: string[];
}

export interface ResolveOptions {
  /** Uygulama paketlenmiş mi? Electron'da `app.isPackaged`. */
  isPackaged: boolean;
  /** Paketlenmiş uygulamada kaynak dizini. Electron'da `process.resourcesPath`. */
  resourcesPath?: string;
  /** Depo kökü — geliştirmede binary buradaki bin/ altında aranır. */
  repoRoot?: string;
  /** Hedef platform. Varsayılan `process.platform`. */
  platform?: NodeJS.Platform;
  /** Ortam değişkenleri. Varsayılan `process.env`. */
  env?: Record<string, string | undefined>;
}

/**
 * PANELY_SIDECAR_COMMAND, binary çözümlemesini tamamen geçersiz kılar.
 *
 * Boşlukla ayrılmış komut ve argümanlar. Geliştirmede WSL köprüsü için:
 *
 *   PANELY_SIDECAR_COMMAND="wsl.exe -d Ubuntu -- /mnt/c/.../panely"
 *
 * Yalnızca geliştirici makinesinde anlamlı; paketlenmiş uygulamada da
 * çalışır ama orada kullanılması için bir neden yok.
 */
const COMMAND_OVERRIDE = "PANELY_SIDECAR_COMMAND";

/** binaryName, platforma göre çalıştırılabilir adını verir. */
export function binaryName(platform: NodeJS.Platform): string {
  return platform === "win32" ? "panely.exe" : "panely";
}

/**
 * resolveSidecarCommand, çalıştırılacak komutu ve argümanları döndürür.
 *
 * `sidecar` alt komutu HER ZAMAN sona eklenir — geçersiz kılma yoluyla
 * bile başka bir alt komut çalıştırılamaz. Bu kasıtlı: ortam değişkeni
 * bir kolaylıktır, rastgele komut çalıştırma kapısı değil.
 */
export function resolveSidecarCommand(options: ResolveOptions): SidecarCommand {
  const platform = options.platform ?? process.platform;
  const env = options.env ?? process.env;

  const override = env[COMMAND_OVERRIDE]?.trim();
  if (override) {
    const parts = override.split(/\s+/).filter(Boolean);
    const [command, ...args] = parts;
    if (!command) {
      throw new Error(`${COMMAND_OVERRIDE} boş — komut belirtilmedi`);
    }
    return { command, args: [...args, "sidecar"] };
  }

  const name = binaryName(platform);

  if (options.isPackaged) {
    if (!options.resourcesPath) {
      throw new Error(
        "paketlenmiş uygulamada resourcesPath gerekli — " +
          "binary electron-builder'da extraResources olarak paketlenmeli",
      );
    }
    return { command: join(options.resourcesPath, name), args: ["sidecar"] };
  }

  if (!options.repoRoot) {
    throw new Error("geliştirme kipinde repoRoot gerekli");
  }
  // Geliştirmede binary `go build -o bin/<ad> ./cmd/panely` ile üretilir.
  return { command: join(options.repoRoot, "bin", name), args: ["sidecar"] };
}
