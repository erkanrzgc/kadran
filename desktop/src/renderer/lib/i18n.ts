/**
 * Küçük çeviri katmanı. Kütüphane yok: yer tutucu ({ad}) ve bir/çok
 * biçimi (`_one` / `_other`) yeterli. Anahtar seçilen dilde yoksa
 * İngilizceye, orada da yoksa anahtarın kendisine düşer; böylece eksik
 * çeviri ekranı boşaltmaz, testte de yakalanır.
 */

export type Dict = Readonly<Record<string, string>>;
export type Params = Readonly<Record<string, string | number>>;

export function translate(dict: Dict, fallback: Dict, key: string, params: Params = {}): string {
  const count = params.count;
  const pluralKey = typeof count === "number" ? `${key}_${count === 1 ? "one" : "other"}` : null;
  const template =
    (pluralKey !== null ? dict[pluralKey] ?? fallback[pluralKey] : undefined) ?? dict[key] ?? fallback[key] ?? key;
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : match,
  );
}

/** placeholders, bir şablondaki yer tutucu adlarını sıralı verir. */
export function placeholders(template: string): string[] {
  return [...template.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? "").sort();
}
