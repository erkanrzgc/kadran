/**
 * Konsol kısayolları ve Tab tamamlamanın uygulanışı (saf). Karar Go'da
 * verilir; burada yalnız tuşun neye karşılık geldiği.
 */

export type ConsoleAction = "run" | "history-up" | "history-down" | "complete" | "cancel" | "clear" | "toggle";

/** KeyboardEvent'ten ihtiyaç duyulan alanlar, `running` komut sürüyor mu. */
export interface KeyLike {
  key: string;
  code?: string;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
  metaKey: boolean;
  running: boolean;
}

/**
 * Esc paneli kapatmaz (null). Ctrl+C çalışma varken durdurur; yokken seçili
 * metnin kopyalanmasına bırakılır (null).
 */
export function action(e: KeyLike): ConsoleAction | null {
  if (e.metaKey) return null;
  const letter = e.key.toLowerCase();
  if (e.ctrlKey) {
    if (e.altKey) return null;
    if (e.key === "`" || e.code === "Backquote") return e.shiftKey ? null : "toggle";
    if (e.shiftKey) return null;
    if (letter === "c") return e.running ? "cancel" : null;
    if (letter === "l") return "clear";
    return null;
  }
  if (e.altKey) return null;
  switch (e.key) {
    case "Enter":
      return e.shiftKey ? null : "run";
    case "ArrowUp":
      return "history-up";
    case "ArrowDown":
      return "history-down";
    case "Tab":
      return e.shiftKey ? null : "complete";
    default:
      return null;
  }
}

export interface Completion {
  line: string;
  cursor: number;
  /** Birden çok aday varsa kullanıcıya gösterilecek liste; tek adayda boş. */
  candidates: string[];
}

function commonPrefix(words: readonly string[]): string {
  let prefix = words[0] ?? "";
  for (const w of words) while (!w.startsWith(prefix)) prefix = prefix.slice(0, -1);
  return prefix;
}

/** İmlecin solundaki sözcüğü adaylarla değiştirir; Go tam sözcük adayları döner. */
export function applyCompletion(line: string, cursor: number, candidates: readonly string[]): Completion {
  if (candidates.length === 0) return { line, cursor, candidates: [] };
  const start = line.slice(0, cursor).search(/\S*$/);
  const single = candidates.length === 1;
  const word = single ? `${candidates[0]} ` : commonPrefix(candidates);
  return { line: line.slice(0, start) + word + line.slice(cursor), cursor: start + word.length, candidates: single ? [] : [...candidates] };
}
