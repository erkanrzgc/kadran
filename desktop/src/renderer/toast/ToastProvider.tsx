import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

import type { Params } from "../lib/i18n.ts";

/**
 * Bildirimler çeviri ANAHTARIYLA tutulur, metinle değil: bildirimi
 * gönderen kod (ör. görünüm kaydı) dil sağlayıcısının dışında olabilir.
 * Metne Toast bileşeni çevirir.
 */
export interface ToastMessage {
  id: number;
  key: string;
  params: Params;
}

type Show = (key: string, params?: Params) => void;

interface ToastContext {
  message: ToastMessage | null;
  show: Show;
  dismiss: () => void;
}

const Ctx = createContext<ToastContext | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState<ToastMessage | null>(null);
  const show = useCallback<Show>((key, params = {}) => setMessage({ id: Date.now(), key, params }), []);
  const dismiss = useCallback(() => setMessage(null), []);
  const value = useMemo(() => ({ message, show, dismiss }), [message, show, dismiss]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

function useCtx(): ToastContext {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("ToastProvider missing");
  return ctx;
}

export function useToast(): Show {
  return useCtx().show;
}

export function useToastState(): Pick<ToastContext, "message" | "dismiss"> {
  const { message, dismiss } = useCtx();
  return { message, dismiss };
}
