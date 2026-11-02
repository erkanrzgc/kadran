/**
 * Logo: kalın, geniş aralıklı metin. Segoe UI'nin dış hatları yeniden
 * dağıtılamadığı için SVG'ye çevrilmez; yazı tipi işletim sisteminden gelir.
 */
export function BrandWordmark({ label }: { label: string }) {
  return <span className="marka" role="img" aria-label={label} data-testid="brand-logo">KΛDRΛN</span>;
}
