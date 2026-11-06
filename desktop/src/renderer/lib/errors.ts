import type { Translate } from "../i18n/I18nProvider.tsx";

/**
 * errorText, IPC hatasının Electron önekini temizler. Kalan metin
 * sunucudan ya da sidecar'dan gelir ve olduğu gibi gösterilir; hep düz
 * metin olarak (React kaçırarak yazar).
 */
export function errorText(err: unknown): string {
  if (err === undefined || err === null) return "unknown error";
  const raw = err instanceof Error ? err.message : String(err);
  return raw
    .replace(/^Error invoking remote method '[^']*':\s*/, "")
    .replace(/^(?:Sidecar)?Error:\s*/, "");
}

/**
 * serverListError, sunucu listesinin bilinen sebeplerini ("server-list:…",
 * main/server-list.ts) arayüzün dilinde söyler; geri kalanı errorText gibi.
 */
export function serverListError(t: Translate, err: unknown): string {
  const text = errorText(err);
  switch (text) {
    case "server-list:duplicate_name":
      return t("servers.error_duplicate_name");
    case "server-list:duplicate_target":
      return t("servers.error_duplicate_target");
    case "server-list:not_found":
      return t("servers.error_not_found");
    case "server-list:newer_file":
      return t("servers.error_newer_file");
  }
  return text;
}
