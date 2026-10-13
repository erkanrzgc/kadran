/**
 * Bağlı sunucunun durumu. Hedef değişince eski istekler yok sayılır:
 * yavaş bir sunucunun geç gelen cevabı yeni seçilen sunucunun üstüne
 * yazmamalı. Aynı sunucuya da aynı anda tek status ve tek verify isteği
 * gider (lib/single-flight.ts): yoksa geç gelen eski cevap yenisini ezer.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { memoryUsedGiB } from "../lib/format.ts";
import { pushSample } from "../lib/samples.ts";
import { createSingleFlight } from "../lib/single-flight.ts";
import type { StatusPayload, VerifyPayload } from "../lib/types.ts";
import { verificationExpired, verificationFailed } from "../lib/verification-state.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export interface ServerState {
  target: string | null;
  status: StatusPayload | null;
  verify: VerifyPayload | null;
  verifiedAt: number | null;
  verifyStale: boolean;
  /** Son status çağrısının gidiş-dönüş süresi (ms): IPC + sidecar başlatma + ssh + daemon, köprü çağrısının tamamı. */
  rtt: number | null;
  memorySamples: readonly number[];
  rttSamples: readonly number[];
  connecting: boolean;
  verifying: boolean;
  /** Son durum yenilemesi başarısız: ekrandaki değerler eski, "Healthy" gösterilmemeli. */
  stale: boolean;
}

const EMPTY: ServerState = {
  target: null,
  status: null,
  verify: null,
  verifiedAt: null,
  verifyStale: false,
  rtt: null,
  memorySamples: [],
  rttSamples: [],
  connecting: false,
  verifying: false,
  stale: false,
};

export function useServer() {
  const toast = useToast();
  const [state, setState] = useState<ServerState>(EMPTY);
  const current = useRef<string | null>(null);
  const [statusFlight] = useState(() => createSingleFlight<boolean>());
  const [verifyFlight] = useState(() => createSingleFlight<void>());

  useEffect(() => {
    const timer = window.setInterval(() => setState((s) =>
      s.verifiedAt !== null && !s.verifyStale && verificationExpired(s.verifiedAt, Date.now())
        ? { ...s, verify: verificationFailed(s.verify), verifyStale: true } : s), 30_000);
    return () => window.clearInterval(timer);
  }, []);

  const fetchStatus = useCallback(
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
          stale: false,
          memorySamples: pushSample(s.memorySamples, memoryUsedGiB(status.host)),
          rttSamples: pushSample(s.rttSamples, rtt),
        }));
        return true;
      } catch (err) {
        if (current.current === target) {
          setState((s) => ({ ...s, stale: true }));
          toast("toast.connect_failed", { error: errorText(err) });
        }
        return false;
      }
    },
    [toast],
  );

  const loadStatus = useCallback(
    (target: string): Promise<boolean> => statusFlight(target, () => fetchStatus(target)),
    [statusFlight, fetchStatus],
  );

  const runVerify = useCallback(
    async (target: string): Promise<void> => {
      try {
        const verify = await bridge.auditVerify(target);
        if (current.current === target) setState((s) => ({ ...s, verify, verifiedAt: Date.now(), verifyStale: false }));
      } catch (err) {
        if (current.current === target) {
          setState((s) => ({ ...s, verify: verificationFailed(s.verify), verifyStale: true }));
          toast("toast.verify_failed", { error: errorText(err) });
        }
      } finally {
        if (current.current === target) setState((s) => ({ ...s, verifying: false }));
      }
    },
    [toast],
  );

  // connect durumu sıfırlayınca Verify düğmesi açılır; o sırada süren eski
  // doğrulama varsa yenisi onu bekler, ikisi üst üste binmez.
  const verifyChains = useCallback(async () => {
    const target = current.current;
    if (!target) return;
    setState((s) => ({ ...s, verifying: true }));
    await verifyFlight(target, () => runVerify(target));
  }, [verifyFlight, runVerify]);

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

  const refresh = useCallback(async (): Promise<boolean> => {
    const target = current.current;
    return target ? loadStatus(target) : false;
  }, [loadStatus]);

  return useMemo(() => ({ state, connect, refresh, verifyChains }), [state, connect, refresh, verifyChains]);
}

export type ServerApi = ReturnType<typeof useServer>;
