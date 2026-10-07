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
