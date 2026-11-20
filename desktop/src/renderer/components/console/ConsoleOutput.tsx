import { memo, useLayoutEffect, useRef } from "react";

import type { ConsoleLine } from "../../lib/consoleBuffer.ts";

/** Alta bu kadar piksel yakınsa çıktı akarken en alta yapışık kalınır. */
const STICK_PX = 24;

function lineClass(line: ConsoleLine): string {
  const parts = ["console-line", `is-${line.stream}`];
  if (line.exit === 0) parts.push("is-exit-ok");
  else if (line.exit !== null) parts.push("is-exit-bad");
  return parts.join(" ");
}

/** Satır nesnesi değişmedikçe yeniden çizilmez: 5.000 satırda yalnız yeni satırlar çizilir. */
const Line = memo(function Line({ line }: { line: ConsoleLine }) {
  return <div className={lineClass(line)}>{line.text}</div>;
});

interface Props {
  lines: readonly ConsoleLine[];
  /** Boş alana tıklayınca girdi satırına odaklanılır. */
  onBlankClick: () => void;
}

/** Çıktı: düz metin olarak (React kaçırarak yazar), seçilip kopyalanabilir. */
export function ConsoleOutput({ lines, onBlankClick }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const stuck = useRef(true);

  useLayoutEffect(() => {
    const el = box.current;
    if (el && stuck.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  return (
    <div
      ref={box}
      className="console-output"
      data-testid="console-output"
      onScroll={(e) => {
        const el = e.currentTarget;
        stuck.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_PX;
      }}
      onClick={() => {
        if (window.getSelection()?.isCollapsed !== false) onBlankClick();
      }}
    >
      {lines.map((line) => (
        <Line key={line.id} line={line} />
      ))}
    </div>
  );
}
