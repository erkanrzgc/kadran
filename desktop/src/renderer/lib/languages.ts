import type { Language } from "../../shared/appearance.ts";

/** Dil adları kendi dillerinde yazılır ve çevrilmez. */
export const LANGUAGES: ReadonlyArray<{ code: Language; name: string }> = [
  { code: "en", name: "English" },
  { code: "tr", name: "Türkçe" },
];
