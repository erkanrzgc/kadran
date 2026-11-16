/**
 * Sunucu sayfasının GitHub bağlantısı (K-167). Durum bir kez okunur; akış
 * BEKLERKEN GITHUB_POLL_MS'de bir yoklanır, ama yalnız pencere görünürken
 * (shouldPoll; K-166 deseni). Bağlandı, başarısız oldu ya da akış bitti:
 * yoklama durur. Eski sunucu (Unimplemented) "unsupported" olur ve bir daha
 * sorulmaz.
 */

import { useCallback, useEffect, useRef, useState } from "react";

import { isCancelled } from "../../shared/management.ts";
import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { errorText } from "../lib/errors.ts";
import { DISCONNECT_WORD, GITHUB_POLL_MS, githubFailure, pendingConnection, shouldPoll, type GitHubConnection } from "../lib/github.ts";

export interface GitHubView {
  loaded: boolean;
  unsupported: boolean;
  connection: GitHubConnection | null;
  /** Son okuma ya da işlem hatası; bağlantı bilgisi (varsa) yerinde kalır. */
  error: string | null;
  /** Bekleyen akış sunucuda sona erdi (bağlanmadan not_connected'a döndü). */
  ended: boolean;
}

const INITIAL: GitHubView = { loaded: false, unsupported: false, connection: null, error: null, ended: false };

export function useGitHub(target: string) {
  const t = useT();
  const [view, setView] = useState<GitHubView>(INITIAL);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  // Hedef değişince ya da ekran kapanınca eski yanıtlar atılır.
  const session = useRef<object>({});

  const read = useCallback(
    async (mine: object) => {
      try {
        const next = await bridge.githubStatus(target);
        if (session.current !== mine) return;
        setView((v) => ({ loaded: true, unsupported: false, connection: next, error: null, ended: v.connection?.state === "pending" && next.state === "not_connected" }));
      } catch (err) {
        if (session.current !== mine) return;
        const unsupported = githubFailure(err) === "unsupported";
        setView((v) => ({ ...v, loaded: true, unsupported, error: unsupported ? null : errorText(err) }));
      }
    },
    [target],
  );

  useEffect(() => {
    const mine = {};
    session.current = mine;
    setView(INITIAL);
    setBusy(false);
    setNotice(null);
    void read(mine);
    return () => {
      session.current = {};
    };
  }, [target, read]);

  const state = view.connection?.state ?? null;
  useEffect(() => {
    if (state !== "pending") return;
    const mine = session.current;
    let running = false;
    const tick = async () => {
      if (running || !shouldPoll(state, document.hidden)) return;
      running = true;
      try {
        await read(mine);
      } finally {
        running = false;
      }
    };
    const onVisible = () => void tick();
    const timer = window.setInterval(() => void tick(), GITHUB_POLL_MS);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [state, read]);

  const connect = useCallback(async () => {
    if (busy) return;
    const mine = session.current;
    setBusy(true);
    setNotice(null);
    setView((v) => ({ ...v, error: null }));
    try {
      const prompt = await bridge.githubConnect(target);
      if (session.current !== mine) return;
      setView({ loaded: true, unsupported: false, connection: pendingConnection(prompt), error: null, ended: false });
    } catch (err) {
      if (session.current !== mine) return;
      const unsupported = githubFailure(err) === "unsupported";
      setView((v) => ({ ...v, unsupported, error: unsupported ? null : errorText(err) }));
    } finally {
      if (session.current === mine) setBusy(false);
    }
  }, [busy, target]);

  /** Yerel onay kutusundan SONRA kesilir; "done" yalnız gerçekten kesildiyse. */
  const disconnect = useCallback(async (): Promise<"done" | "cancelled" | "failed"> => {
    if (busy) return "failed";
    const mine = session.current;
    setBusy(true);
    setNotice(null);
    setView((v) => ({ ...v, error: null }));
    try {
      const result = await bridge.githubDisconnect(target, DISCONNECT_WORD);
      if (session.current !== mine) return "failed";
      if (isCancelled(result)) {
        setNotice(t("operation.cancelled"));
        return "cancelled";
      }
      await read(mine);
      return "done";
    } catch (err) {
      if (session.current !== mine) return "failed";
      setView((v) => ({ ...v, error: errorText(err) }));
      return "failed";
    } finally {
      if (session.current === mine) setBusy(false);
    }
  }, [busy, target, read, t]);

  const reload = useCallback(() => {
    setView(INITIAL);
    void read(session.current);
  }, [read]);

  return { view, busy, notice, connect, disconnect, reload };
}
