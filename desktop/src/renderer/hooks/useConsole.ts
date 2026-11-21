/**
 * Konsol oturumunun React bağı. App'te yaşar: panel kapanınca ya da ekran
 * değişince bile çalışma, tampon ve olay aboneliği durur. Mantık
 * lib/consoleSession.ts'te; burada yalnız köprü çağrıları ve abonelik.
 * Arayüz hiçbir güvenlik kararı vermez: satırı Go'ya iletir, Go'nun
 * olaylarını gösterir.
 */

import { useCallback, useEffect, useReducer, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { applyCompletion } from "../lib/consoleKeys.ts";
import { createHistory, current, edit, move, type HistoryState } from "../lib/consoleHistory.ts";
import {
  addNote,
  answer,
  applyEvent,
  begin,
  canRun,
  clearOutput,
  closeCard,
  setPanelOpen,
  initialSession,
  isBusy,
  promptServer,
  started,
  startFailed,
  type ConsoleSession,
} from "../lib/consoleSession.ts";

type Update = (s: ConsoleSession) => ConsoleSession;
const apply = (s: ConsoleSession, update: Update): ConsoleSession => update(s);

export interface ConsoleApi {
  session: ConsoleSession;
  /** İstemde görünen sunucu: çalışırken başladığı, değilse seçili olan. */
  promptName: string;
  busy: boolean;
  /** Kaydedilmemiş gizli kart açık, çalışma yok: yeni satır çalışmaz (R17). */
  held: boolean;
  /** Girdi satırı komutun istemi için açık. */
  awaitingInput: boolean;
  line: string;
  edit: (text: string) => void;
  historyMove: (direction: "up" | "down") => void;
  run: () => void;
  submitInput: (text: string) => void;
  cancel: () => void;
  clear: () => void;
  complete: (cursor: number) => Promise<{ line: string; cursor: number } | null>;
  closeCard: () => void;
  /** Panel kapanırken: gizli kart kapanır, komut sürer. */
  setPanelOpen: (open: boolean) => void;
}

interface Options {
  /** Kenar çubuğunda seçili sunucunun adı; yoksa boş. */
  server: string;
  /** console:use: kenar çubuğu bu sunucuya geçsin. */
  onUse: (name: string) => void;
  /** "Gizli çıktı ayrı kartta gösterildi" satırı (çevrilmiş). */
  secretNote: string;
  /** Çalışma bitti: satır, başladığı sunucu ve çıkış kodu. */
  onFinish?: (line: string, server: string, exit: number) => void;
}

export function useConsole({ server, onUse, secretNote, onFinish }: Options): ConsoleApi {
  const [session, update] = useReducer(apply, undefined, initialSession);
  const [history, setHistory] = useState<HistoryState>(() => createHistory([]));
  const runInfo = useRef({ line: "", server: "" });
  const live = useRef({ session, server, onUse, secretNote, onFinish });
  live.current = { session, server, onUse, secretNote, onFinish };
  const busy = isBusy(session);

  // Olaylar consoleRun dönmeden önce de gelir: abonelik panelden bağımsız, hep açık.
  useEffect(
    () =>
      bridge.onConsoleEvent((event) => {
        if (event.name === "console:use") live.current.onUse(event.data.server);
        else {
          if (event.name === "console:done" && isBusy(live.current.session)) {
            live.current.onFinish?.(runInfo.current.line, runInfo.current.server, event.data.exit);
          }
          update((s) => applyEvent(s, event, live.current.secretNote));
        }
      }),
    [],
  );

  // Geçmiş Go'dan gelir (arıtılmış): açılışta, sunucu değişince ve her komuttan sonra.
  useEffect(() => {
    if (busy) return;
    let cancelled = false;
    bridge.consoleState(server).then(
      (state) => {
        if (!cancelled) setHistory((h) => ({ ...createHistory(state.history), draft: h.draft }));
      },
      () => {},
    );
    return () => {
      cancelled = true;
    };
  }, [server, busy]);

  const run = useCallback(() => {
    const { session: s, server: name } = live.current;
    const text = current(history);
    // R17: kart açıkken satır köprüye gitmez; yazılan satır taslakta kalır.
    if (!canRun(s) || text.trim() === "") return;
    runInfo.current = { line: text, server: name };
    update((x) => begin(x, promptServer(x, name), text));
    setHistory((h) => edit(h, ""));
    bridge.consoleRun(name, text).then(
      (id) => update((x) => started(x, id)),
      (err: unknown) => update((x) => startFailed(x, errorText(err))),
    );
  }, [history]);

  const submitInput = useCallback((text: string) => {
    const { run: id } = live.current.session;
    if (id === null) return;
    // Yanıt yankılanmaz ve saklanmaz: istem satırı yerinde biter, metin köprüye gider.
    update(answer);
    bridge.consoleInput(id, text).catch((err: unknown) => update((x) => startFailed(x, errorText(err))));
  }, []);

  const cancel = useCallback(() => {
    const { run: id } = live.current.session;
    if (id !== null) void bridge.consoleCancel(id).catch(() => {});
  }, []);

  const complete = useCallback(
    async (cursor: number) => {
      const text = current(history);
      let candidates: string[];
      try {
        candidates = await bridge.consoleComplete(live.current.server, text, cursor);
      } catch {
        return null;
      }
      const done = applyCompletion(text, cursor, candidates);
      if (done.candidates.length > 0) update((s) => addNote(s, done.candidates.join("  ")));
      setHistory((h) => edit(h, done.line));
      return { line: done.line, cursor: done.cursor };
    },
    [history],
  );

  const clear = useCallback(() => update(clearOutput), []);
  const dismissCard = useCallback(() => update(closeCard), []);
  const panelOpen = useCallback((open: boolean) => update((s) => setPanelOpen(s, open)), []);
  const editLine = useCallback((text: string) => setHistory((h) => edit(h, text)), []);
  const historyMove = useCallback((direction: "up" | "down") => setHistory((h) => move(h, direction)), []);

  return {
    session,
    promptName: promptServer(session, server),
    busy,
    held: !busy && !canRun(session),
    awaitingInput: session.input,
    line: current(history),
    edit: editLine,
    historyMove,
    run,
    submitInput,
    cancel,
    clear,
    complete,
    closeCard: dismissCard,
    setPanelOpen: panelOpen,
  };
}
