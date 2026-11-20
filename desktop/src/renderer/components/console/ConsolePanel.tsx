import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";

import { useT } from "../../i18n/I18nProvider.tsx";
import type { ConsoleApi } from "../../hooks/useConsole.ts";
import { clampHeight } from "../../lib/consoleLayout.ts";
import { ConsoleOutput } from "./ConsoleOutput.tsx";
import { ConsolePrompt } from "./ConsolePrompt.tsx";
import { SecretCard } from "./SecretCard.tsx";

/** Tutamaç okla bu kadar piksel oynar. */
const KEY_STEP_PX = 24;
const SHORTCUT = "Ctrl+`";

interface Props {
  api: ConsoleApi;
  open: boolean;
  /** Kayıtlı yükseklik; sürüklerken yerel tutulur, bırakınca onHeight ile kaydedilir. */
  height: number;
  onToggle: () => void;
  onHeight: (height: number) => void;
}

function useWindowHeight(): number {
  const [value, setValue] = useState(window.innerHeight);
  useEffect(() => {
    const onResize = () => setValue(window.innerHeight);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return value;
}

/** Sunucu ekranının altındaki konsol paneli: kapalıyken ince bir çubuk. */
export function ConsolePanel({ api, open, height, onToggle, onHeight }: Props) {
  const t = useT();
  const windowHeight = useWindowHeight();
  const [drag, setDrag] = useState<{ startY: number; startHeight: number; height: number } | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const shown = clampHeight(drag?.height ?? height, windowHeight);

  // Açılınca ve komut bitince girdi satırına odaklan.
  useEffect(() => {
    if (open && (!api.busy || api.awaitingInput)) input.current?.focus();
  }, [open, api.busy, api.awaitingInput]);

  if (!open) {
    return (
      <button type="button" className="console-bar" onClick={onToggle} data-testid="console-open">
        <span>{t("console.title")}</span>
        <kbd>{SHORTCUT}</kbd>
      </button>
    );
  }

  const onDown = (e: PointerEvent<HTMLDivElement>) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    setDrag({ startY: e.clientY, startHeight: shown, height: shown });
  };
  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    if (drag) setDrag({ ...drag, height: clampHeight(drag.startHeight + drag.startY - e.clientY, windowHeight) });
  };
  const onUp = () => {
    if (drag) onHeight(clampHeight(drag.height, windowHeight));
    setDrag(null);
  };
  const onGripKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.key === "ArrowUp" ? KEY_STEP_PX : e.key === "ArrowDown" ? -KEY_STEP_PX : 0;
    if (step === 0) return;
    e.preventDefault();
    onHeight(clampHeight(shown + step, windowHeight));
  };

  return (
    <section className="console" style={{ height: shown }} aria-label={t("console.title")} data-testid="console">
      <SecretCard content={api.session.card} onClose={api.closeCard} />
      <div
        className="console-grip"
        role="separator"
        aria-orientation="horizontal"
        aria-label={t("console.resize")}
        aria-valuenow={shown}
        tabIndex={0}
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        onKeyDown={onGripKey}
        data-testid="console-grip"
      />
      <header className="console-head">
        <strong>{t("console.title")}</strong>
        <button type="button" className="btn btn-ghost console-close" onClick={onToggle} aria-label={t("console.close")} data-testid="console-close">
          <kbd>{SHORTCUT}</kbd>
        </button>
      </header>
      <ConsoleOutput lines={api.session.buffer.lines} onBlankClick={() => input.current?.focus()} />
      <ConsolePrompt
        ref={input}
        promptName={api.promptName}
        root={api.session.root}
        busy={api.busy}
        awaitingInput={api.awaitingInput}
        line={api.line}
        onEdit={api.edit}
        onRun={api.run}
        onAnswer={api.submitInput}
        onHistory={api.historyMove}
        onComplete={api.complete}
        onCancel={api.cancel}
        onClear={api.clear}
      />
    </section>
  );
}
