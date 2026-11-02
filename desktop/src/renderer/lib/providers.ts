/**
 * Sağlayıcı etiketleri. Kullanıcının elle seçtiği bir addır: Kadran bulut
 * hesabına bağlanmaz. Bilinmeyen etiket olduğu gibi gösterilir, renk uydurulmaz.
 * Etiket elle düzenlenmiş dosyadan gelebilir: "__proto__" gibi bir ad nesnenin
 * kendi özelliğine denk gelmesin diye Map.
 */

export interface ProviderView {
  name: string;
  letter: string;
  /** Rozet rengi; null ise nötr rozet. */
  color: string | null;
}

export const PROVIDERS: ReadonlyMap<string, { name: string; color: string }> = new Map([
  ["hetzner", { name: "Hetzner", color: "#d50c2d" }],
  ["google-cloud", { name: "Google Cloud", color: "#4285f4" }],
  ["hostinger", { name: "Hostinger", color: "#673de6" }],
  ["digitalocean", { name: "DigitalOcean", color: "#0080ff" }],
  ["aws", { name: "AWS", color: "#ff9900" }],
]);

export function providerView(provider: string): ProviderView {
  const known = PROVIDERS.get(provider);
  if (known) return { name: known.name, letter: known.name[0]!, color: known.color };
  return { name: provider, letter: provider ? provider[0]!.toUpperCase() : "?", color: null };
}
