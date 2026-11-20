/**
 * "Konsolda yönetici komutları" ayarı. Değeri Go tutar ve değiştirir: açmak
 * yerel onay kutusundan geçer, kutu reddedilirse ayar eski kalır. Arayüz hiçbir
 * şey varsaymaz; yalnız Go'nun döndürdüğü değeri gösterir.
 */

import { useCallback, useEffect, useState } from "react";

import { bridge } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export interface ConsoleAdmin {
  /** Go'dan okunana kadar null. */
  admin: boolean | null;
  pending: boolean;
  set: (on: boolean) => void;
}

export function useConsoleAdmin(): ConsoleAdmin {
  const toast = useToast();
  const [admin, setAdmin] = useState<boolean | null>(null);
  const [pending, setPending] = useState(false);

  useEffect(() => {
    let cancelled = false;
    bridge.consoleState("").then(
      (state) => {
        if (!cancelled) setAdmin(state.admin);
      },
      (err: unknown) => toast("toast.save_failed", { error: errorText(err) }),
    );
    return () => {
      cancelled = true;
    };
  }, [toast]);

  const set = useCallback(
    (on: boolean) => {
      if (pending || admin === null) return;
      setPending(true);
      bridge.consoleSetAdmin(on).then(
        (result) => setAdmin(result),
        (err: unknown) => toast("toast.save_failed", { error: errorText(err) }),
      ).finally(() => setPending(false));
    },
    [pending, admin, toast],
  );

  return { admin, pending, set };
}
