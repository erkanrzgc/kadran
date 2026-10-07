import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { Appearance, WindowMode } from "../../shared/appearance.ts";
import { bridge, type AppearanceBundle } from "../bridge.ts";
import { applyAppearance, CODE_FALLBACK, UI_FALLBACK, type FontStacks } from "../lib/apply-appearance.ts";
import { errorText } from "../lib/errors.ts";
import { listLocalFonts, registerFamily } from "../lib/font-loader.ts";
import { resolveStack } from "../lib/font-stack.ts";
import { groupFaces, strongerWeight, type Face } from "../lib/fonts.ts";
import { useToast } from "../toast/ToastProvider.tsx";

/** Kaydırıcı sürüklenirken her adımda diske yazılmasın. */
const SAVE_DELAY_MS = 250;

/** Kararlı boş liste: efekt bağımlılığı her çizimde değişmesin. */
const NO_FACES: readonly Face[] = [];

export interface AppearanceApi {
  appearance: Appearance;
  defaults: Appearance;
  modes: readonly WindowMode[];
  families: ReadonlyMap<string, Face[]>;
  fontsListed: boolean;
  update: (patch: Partial<Appearance>) => void;
  reset: () => void;
}

const Ctx = createContext<AppearanceApi | null>(null);

export function AppearanceProvider({ initial, children }: { initial: AppearanceBundle; children: ReactNode }) {
  const toast = useToast();
  const [appearance, setAppearance] = useState(initial.appearance);
  const [families, setFamilies] = useState<ReadonlyMap<string, Face[]>>(new Map());
  const [fontsListed, setFontsListed] = useState(false);
  const [uiStack, setUiStack] = useState(UI_FALLBACK);
  const [codeStack, setCodeStack] = useState(CODE_FALLBACK);
  const latest = useRef(initial.appearance);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    void listLocalFonts().then((list) => {
      setFamilies(groupFaces(list));
      setFontsListed(list.length > 0);
    });
  }, []);

  const uiFaces = families.get(appearance.uiFont.family) ?? NO_FACES;
  const codeFaces = families.get(appearance.codeFont.family) ?? NO_FACES;
  const uiWeight = appearance.uiFont.weight;

  // Her takma ad kendi etkisinde kaydedilir: bir takma adı yalnız tek etki
  // çağırır, böylece canlı çağrının 0 dönmesi gerçek başarısızlıktır.
  // Kalınlık burada YOK: kaydırıcı her adımda yüzleri yeniden yüklemesin.
  useEffect(() => {
    let cancelled = false;
    void resolveStack("kadran-ui", uiFaces, UI_FALLBACK, registerFamily, () => cancelled).then((stack) => {
      if (stack !== null) setUiStack(stack);
    });
    return () => {
      cancelled = true;
    };
  }, [uiFaces]);

  useEffect(() => {
    let cancelled = false;
    void resolveStack("kadran-code", codeFaces, CODE_FALLBACK, registerFamily, () => cancelled).then((stack) => {
      if (stack !== null) setCodeStack(stack);
    });
    return () => {
      cancelled = true;
    };
  }, [codeFaces]);

  const stacks = useMemo<FontStacks>(
    () => ({ ui: uiStack, code: codeStack, strong: uiStack === UI_FALLBACK ? 600 : strongerWeight(uiFaces, uiWeight) }),
    [uiStack, codeStack, uiFaces, uiWeight],
  );

  useEffect(() => {
    applyAppearance(document.documentElement, appearance, stacks);
    document.documentElement.lang = appearance.language;
  }, [appearance, stacks]);

  const save = useCallback(
    (next: Appearance) => {
      void bridge.saveAppearance(next).catch((err: unknown) => toast("toast.save_failed", { error: errorText(err) }));
    },
    [toast],
  );

  const update = useCallback(
    (patch: Partial<Appearance>) => {
      const prev = latest.current;
      const next: Appearance = { ...prev, ...patch };
      latest.current = next;
      setAppearance(next);
      if (timer.current) clearTimeout(timer.current);
      // Mod değişimi pencereyi yeniden kurabilir: bekletmeden kaydet.
      if (patch.mode !== undefined && patch.mode !== prev.mode) save(next);
      else timer.current = setTimeout(() => save(next), SAVE_DELAY_MS);
    },
    [save],
  );

  const reset = useCallback(() => update({ ...initial.defaults }), [update, initial.defaults]);

  const value = useMemo<AppearanceApi>(
    () => ({ appearance, defaults: initial.defaults, modes: initial.modes, families, fontsListed, update, reset }),
    [appearance, initial.defaults, initial.modes, families, fontsListed, update, reset],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAppearance(): AppearanceApi {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("AppearanceProvider missing");
  return ctx;
}
