import { forwardRef, useEffect, useState, type KeyboardEvent } from "react";

import { useT } from "../../i18n/I18nProvider.tsx";
import { action } from "../../lib/consoleKeys.ts";

/** Go sınırları: satır 4096 bayt, istem girdisi 256 bayt; asıl denetim Go'da. */
const LINE_MAX = 4096;
const ANSWER_MAX = 256;

interface Props {
  promptName: string;
  root: boolean;
  /** Komut sürüyor ya da başlıyor. */
  busy: boolean;
  /** Kaydedilmemiş gizli kart açık: yeni satır çalışmaz, istem nedenini söyler (R17). */
  held: boolean;
  /** Komut bir istem bekliyor: satır yalnız o zaman açılır. */
  awaitingInput: boolean;
  line: string;
  onEdit: (text: string) => void;
  onRun: () => void;
  onAnswer: (text: string) => void;
  onHistory: (direction: "up" | "down") => void;
  onComplete: (cursor: number) => Promise<{ line: string; cursor: number } | null>;
  onCancel: () => void;
  onClear: () => void;
}

/**
 * Girdi satırı. Çalışırken kilitli (salt okunur, odak kalır ki Ctrl+C
 * çalışsın); yalnız console:input gelince açılır. Gizli kart açıkken de
 * kilitli (R17): yazılan satır taslakta kalır, istem "önce kartı kapatın" der.
 * İstem yanıtı yerel durumda durur, geçmişe ve tampona girmez, gönderilince
 * silinir.
 */
export const ConsolePrompt = forwardRef<HTMLInputElement, Props>(function ConsolePrompt(p, ref) {
  const t = useT();
  const [typed, setTyped] = useState("");
  const answering = p.busy && p.awaitingInput;
  const locked = (p.busy && !p.awaitingInput) || p.held;
  const rootRun = p.busy && p.root;

  useEffect(() => {
    if (!answering) setTyped("");
  }, [answering]);

  const complete = (input: HTMLInputElement) => {
    void p.onComplete(input.selectionStart ?? p.line.length).then((next) => {
      if (next) requestAnimationFrame(() => input.setSelectionRange(next.cursor, next.cursor));
    });
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const act = action({ key: e.key, code: e.code, ctrlKey: e.ctrlKey, shiftKey: e.shiftKey, altKey: e.altKey, metaKey: e.metaKey, running: p.busy });
    if (act === null || act === "toggle") return;
    e.preventDefault();
    if (act === "cancel") p.onCancel();
    else if (act === "clear") p.onClear();
    else if (locked) return;
    else if (answering) {
      if (act === "run") {
        p.onAnswer(typed);
        setTyped("");
      }
    } else if (act === "run") p.onRun();
    else if (act === "history-up") p.onHistory("up");
    else if (act === "history-down") p.onHistory("down");
    else complete(e.currentTarget);
  };

  return (
    <div className={`console-prompt${rootRun ? " is-root" : ""}`} data-testid="console-prompt">
      <span className="console-ps" aria-hidden="true">
        {p.promptName} {rootRun ? "#" : "›"}
      </span>
      <input
        ref={ref}
        key={answering ? "answer" : "line"}
        className="console-input"
        data-testid="console-input"
        type="text"
        spellCheck={false}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        aria-label={t("console.input_label")}
        placeholder={p.held ? t("console.card_first") : locked ? t("console.running") : answering ? "" : t("console.placeholder")}
        readOnly={locked}
        aria-disabled={locked}
        maxLength={answering ? ANSWER_MAX : LINE_MAX}
        value={answering ? typed : locked ? "" : p.line}
        onChange={(e) => (answering ? setTyped(e.target.value) : p.onEdit(e.target.value))}
        onKeyDown={onKeyDown}
      />
    </div>
  );
});
