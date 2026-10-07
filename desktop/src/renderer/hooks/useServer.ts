/**
 * Bağlı sunucunun durumu. Hedef değişince eski istekler yok sayılır:
 * yavaş bir sunucunun geç gelen cevabı yeni seçilen sunucunun üstüne
 * yazmamalı.
 */

import { useCallback, useMemo, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { memoryUsedGiB } from "../lib/format.ts";
import { pushSample } from "../lib/samples.ts";
import type { StatusPayload, VerifyPayload } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export interface ServerState {
  target: string | null;
  status: StatusPayload | null;
  verify: VerifyPayload | null;
  /** Son status çağrısının gidiş-dönüş süresi (ms): IPC + sidecar başlatma + ssh + daemon, köprü çağrısının tamamı. */
  rtt: number | null;
  memorySamples: readonly number[];
  rttSamples: readonly number[];
  connecting: boolean;
  verifying: boolean;
}

const EMPTY: ServerState = {
  target: null,
  status: null,
  verify: null,
  rtt: null,
  memorySamples: [],
  rttSamples: [],
  connecting: false,
  verifying: false,
};

export function useServer() {
  const toast = useToast();
  const [state, setState] = useState<ServerState>(EMPTY);
  const current = useRef<string | null>(null);

  const loadStatus = useCallback(
    async (target: string): Promise<boolean> => {
      const started = performance.now();
      try {
        const status = await bridge.status(target);
        if (current.current !== target) return false;
        const rtt = Math.round(performance.now() - started);
        setState((s) => ({
          ...s,
          status,
          rtt,
          memorySamples: pushSample(s.memorySamples, memoryUsedGiB(status.host)),
          rttSamples: pushSample(s.rttSamples, rtt),
        }));
        return true;
      } catch (err) {
        if (current.current === target) toast("toast.connect_failed", { error: errorText(err) });
        return false;
      }
    },
    [toast],
  );

  const verifyChains = useCallback(async () => {
    const target = current.current;
    if (!target) return;
    setState((s) => ({ ...s, verifying: true }));
    try {
      const verify = await bridge.auditVerify(target);
      if (current.current === target) setState((s) => ({ ...s, verify }));
    } catch (err) {
      if (current.current === target) toast("toast.verify_failed", { error: errorText(err) });
    } finally {
      if (current.current === target) setState((s) => ({ ...s, verifying: false }));
    }
  }, [toast]);

  const connect = useCallback(
    async (target: string): Promise<boolean> => {
      current.current = target;
      setState({ ...EMPTY, target, connecting: true });
      const ok = await loadStatus(target);
      if (current.current !== target) return false;
      if (!ok) {
        current.current = null;
        setState(EMPTY);
        return false;
      }
      setState((s) => ({ ...s, connecting: false }));
      await verifyChains();
      return true;
    },
    [loadStatus, verifyChains],
  );

  const refresh = useCallback(async () => {
    const target = current.current;
    if (target) await loadStatus(target);
  }, [loadStatus]);

  return useMemo(() => ({ state, connect, refresh, verifyChains }), [state, connect, refresh, verifyChains]);
}

export type ServerApi = ReturnType<typeof useServer>;
