import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import type { Appearance, WindowMode } from "../../shared/appearance.ts";
import { bridge, type AppearanceBundle } from "../bridge.ts";
import { applyAppearance, CODE_FALLBACK, fontStack, UI_FALLBACK, type FontStacks } from "../lib/apply-appearance.ts";
import { errorText } from "../lib/errors.ts";
import { listLocalFonts, registerFamily } from "../lib/font-loader.ts";
import { groupFaces, strongerWeight, type Face } from "../lib/fonts.ts";
import { useToast } from "../toast/ToastProvider.tsx";

/** Kaydırıcı sürüklenirken her adımda diske yazılmasın. */
const SAVE_DELAY_MS = 250;

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
  const [stacks, setStacks] = useState<FontStacks>({ ui: UI_FALLBACK, code: CODE_FALLBACK, strong: 600 });
  const latest = useRef(initial.appearance);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    void listLocalFonts().then((list) => {
      setFamilies(groupFaces(list));
      setFontsListed(list.length > 0);
    });
  }, []);

  const uiFamily = appearance.uiFont.family;
  const uiWeight = appearance.uiFont.weight;
  const codeFamily = appearance.codeFont.family;
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const uiFaces = families.get(uiFamily) ?? [];
      const codeFaces = families.get(codeFamily) ?? [];
      // Ad yalnız yüz seçer; CSS'e giden tek şey sabit takma ad.
      const uiOk = uiFaces.length > 0 && (await registerFamily("kadran-ui", uiFaces)) > 0;
      const codeOk = codeFaces.length > 0 && (await registerFamily("kadran-code", codeFaces)) > 0;
      if (cancelled) return;
      setStacks({
        ui: fontStack(uiOk ? "kadran-ui" : null, UI_FALLBACK),
        code: fontStack(codeOk ? "kadran-code" : null, CODE_FALLBACK),
        strong: uiOk ? strongerWeight(uiFaces, uiWeight) : 600,
      });
    })();
    return () => {
      cancelled = true;
    };
  }, [families, uiFamily, uiWeight, codeFamily]);

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
