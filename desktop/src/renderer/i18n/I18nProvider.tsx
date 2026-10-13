import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";

import type { Language } from "../../shared/appearance.ts";
import { translate, type Dict, type Params } from "../lib/i18n.ts";
import en from "./en.json";
import tr from "./tr.json";

const DICTS: Readonly<Record<Language, Dict>> = { en, tr };

export type Translate = (key: string, params?: Params) => string;

interface I18nValue {
  t: Translate;
  language: Language;
}

const Ctx = createContext<I18nValue>({ t: (key) => key, language: "en" });

export function I18nProvider({ language, children }: { language: Language; children: ReactNode }) {
  const t = useCallback<Translate>((key, params) => translate(DICTS[language], en, key, params), [language]);
  const value = useMemo(() => ({ t, language }), [t, language]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useT(): Translate {
  return useContext(Ctx).t;
}

export function useLanguage(): Language {
  return useContext(Ctx).language;
}
