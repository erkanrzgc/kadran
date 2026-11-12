/**
 * Kaynak değerlerinin metni (K-166): yüzde ve bayt/sn arayüz dilinde
 * ("%35" / "35%", "/sn" / "/s"). Ölçülemeyen değer "—"; 0 yazılmaz.
 */

import { useMemo } from "react";

import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { humanBytes, percentNumber } from "../lib/format.ts";

export interface ResourceText {
  percent: (value: number | null) => string;
  rate: (value: number | null) => string;
}

export function useResourceText(): ResourceText {
  const t = useT();
  const language = useLanguage();
  return useMemo(
    () => ({
      percent: (value) => (value === null ? "—" : t("unit.percent", { value: percentNumber(value, language) })),
      rate: (value) => (value === null ? "—" : t("unit.per_second", { value: humanBytes(value) })),
    }),
    [t, language],
  );
}
