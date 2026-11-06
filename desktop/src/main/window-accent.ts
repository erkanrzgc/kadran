/**
 * Windows camı: pencere odakta olmasa da masaüstü arkada bulanık kalır.
 * Electron'un `backgroundMaterial` ayarı (Acrylic) odak gidince griye düşer ve
 * bunu değiştirmenin yolu yok; `SetWindowCompositionAttribute` düşmez (K-161).
 *
 * Çağrı belgelenmemiş: bir Windows güncellemesi kaldırırsa yükleme null döner ve
 * uygulama Electron'un Acrylic'ine geçer. koffi yalnız burada, ana süreçte ve
 * yalnız Windows 11'de yüklenir; arayüz ona ulaşamaz, pencere kimliğinden başka
 * dışarıdan değer almaz.
 */

import type { BrowserWindow } from "electron";

import type { Theme } from "../shared/appearance.ts";

const WCA_ACCENT_POLICY = 19;
const ACCENT_DISABLED = 0;
const ACCENT_ENABLE_ACRYLICBLURBEHIND = 4;
const ACCENT_FLAG_GRADIENT_COLOR = 2;
const DWMWA_WINDOW_CORNER_PREFERENCE = 33;
const DWMWCP_ROUND = 2;

/**
 * Tema başına ton (AABBGGRR). Kullanıcı Electron deneme penceresinde seçti
 * (6 Kasım, K-163): açıkta buzlu beyaz, koyuda hafif koyu. Saydamlığı sıfır
 * olamaz: pencere simsiyah çizilir. Düz bulanıklık (3) Electron'un penceresinde
 * hiç çalışmadı (arka siyah); yalnız Akrilik masaüstünü gösteriyor.
 */
const GLASS_TINT: Record<Theme, number> = { light: 0xa0f7f4f4, dark: 0x30181816 };

export interface AccentPolicy {
  State: number;
  Flags: number;
  Color: number;
  AnimationId: number;
}

export function accentPolicy(on: boolean, scheme: Theme): AccentPolicy {
  return { State: on ? ACCENT_ENABLE_ACRYLICBLURBEHIND : ACCENT_DISABLED, Flags: ACCENT_FLAG_GRADIENT_COLOR, Color: GLASS_TINT[scheme], AnimationId: 0 };
}

export function handleValue(handle: Buffer): number {
  return handle.length >= 8 ? Number(handle.readBigUInt64LE(0)) : handle.readUInt32LE(0);
}

export interface WindowAccent {
  /** false: Windows çağrıyı reddetti. `scheme` tonu seçer. */
  set(win: BrowserWindow, on: boolean, scheme: Theme): boolean;
}

export async function loadWindowAccent(warn: (message: string) => void): Promise<WindowAccent | null> {
  try {
    const { default: koffi } = await import("koffi");
    const policy = koffi.struct("AccentPolicy", { State: "int", Flags: "int", Color: "uint32", AnimationId: "int" });
    const data = koffi.struct("WindowCompositionData", { Attribute: "int", Data: koffi.pointer(policy), Size: "size_t" });
    const setComposition = koffi.load("user32.dll").func("SetWindowCompositionAttribute", "int", ["uintptr_t", koffi.pointer(data)]);
    const setDwm = koffi.load("dwmapi.dll").func("DwmSetWindowAttribute", "int", ["uintptr_t", "uint32", koffi.pointer("int"), "uint32"]);
    const size = koffi.sizeof(policy);
    return {
      set(win, on, scheme) {
        const hwnd = handleValue(win.getNativeWindowHandle());
        // Köşe isteği başarısız olsa da cam çalışır; sonucu önemsiz.
        setDwm(hwnd, DWMWA_WINDOW_CORNER_PREFERENCE, [DWMWCP_ROUND], 4);
        return setComposition(hwnd, { Attribute: WCA_ACCENT_POLICY, Data: accentPolicy(on, scheme), Size: size }) !== 0;
      },
    };
  } catch (err) {
    warn(`Windows camı yüklenemedi, Acrylic kullanılıyor: ${(err as Error).message}`);
    return null;
  }
}
