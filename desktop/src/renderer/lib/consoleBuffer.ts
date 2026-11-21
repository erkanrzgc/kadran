/**
 * Konsol çıktı tamponu (saf, değişmez). Panelde en çok 5.000 satır tutulur,
 * eskiler düşer; bir satır en çok 8 KiB'tır, fazlası kesilir ve `…` eklenir
 * (K-165 bellek bütçesi). Çıktı olayları satırın ortasında bölünebilir: aynı
 * akışın yarım satırı bir sonraki parçayla birleşir.
 *
 * Güvenlik: tampona yalnız komutun çıktısı girer. Gizli çıktı kartının
 * içeriği ve istem girdisi buraya hiç uğramaz (consoleSession.ts).
 */

/** out/err: komut çıktısı; echo: yazılan satır; note: konsolun kendi notu. */
export type ConsoleStream = "out" | "err" | "echo" | "note";

export interface ConsoleLine {
  readonly id: number;
  readonly text: string;
  readonly stream: ConsoleStream;
  /** Satır sonu henüz gelmedi (CLI istemi gibi). */
  readonly open: boolean;
  /** Komutun yankı satırında bitiş kodu; işareti belirler. Çıktı satırlarında hep null. */
  readonly exit: number | null;
  readonly truncated: boolean;
}

export interface BufferState {
  readonly lines: readonly ConsoleLine[];
  readonly next: number;
}

export const MAX_LINES = 5000;
/** Satır başına en çok UTF-16 birimi (8 KiB). */
export const MAX_LINE_CHARS = 8192;
export const TRUNCATION_MARK = "…";

export function emptyBuffer(): BufferState {
  return { lines: [], next: 0 };
}

// ESC dizileri (renk, imleç) görünmez; kalan denetim (Cc) ve biçim (Cf)
// karakterleri, bidi geçersiz kılmaları dahil, atılır. Satır sonu ve sekme kalır.
const ANSI = /\x1B\[[0-?]*[ -/]*[@-~]/g;
const UNSAFE = /(?![\t\n])[\p{Cc}\p{Cf}]/gu;

function clean(text: string): string {
  return text.replace(ANSI, "").replace(UNSAFE, "");
}

function grow(line: ConsoleLine, piece: string, open: boolean): ConsoleLine {
  if (line.truncated) return { ...line, open };
  const room = MAX_LINE_CHARS - line.text.length;
  if (piece.length <= room) return { ...line, text: line.text + piece, open };
  let cut = piece.slice(0, room);
  // Yarım vekil çifti bırakma.
  if (cut.length > 0 && /[\uD800-\uDBFF]/.test(cut[cut.length - 1]!)) cut = cut.slice(0, -1);
  return { ...line, text: line.text + cut + TRUNCATION_MARK, open, truncated: true };
}

/** Akışa metin ekler; parça satır ortasında bitebilir. */
export function append(state: BufferState, stream: ConsoleStream, text: string): BufferState {
  const pieces = clean(text).split("\n");
  if (pieces.length === 1 && pieces[0] === "") return state;
  const lines = state.lines.slice();
  let next = state.next;
  pieces.forEach((piece, i) => {
    const closed = i < pieces.length - 1;
    if (!closed && piece === "") return; // son satır sonundan sonra kalan boşluk
    const last = lines[lines.length - 1];
    const continues = i === 0 && last !== undefined && last.open && last.stream === stream;
    if (i === 0 && last?.open && !continues) lines[lines.length - 1] = { ...last, open: false };
    if (continues) {
      lines[lines.length - 1] = grow(last, piece, !closed);
    } else {
      lines.push(grow({ id: next++, text: "", stream, open: true, exit: null, truncated: false }, piece, !closed));
    }
  });
  return { lines: lines.length > MAX_LINES ? lines.slice(lines.length - MAX_LINES) : lines, next };
}

/** Açık satırı yerinde bitirir; metin eklemez (istem girdisi yazdırılmaz). */
export function endLine(state: BufferState): BufferState {
  const last = state.lines[state.lines.length - 1];
  if (!last?.open) return state;
  return { ...state, lines: [...state.lines.slice(0, -1), { ...last, open: false }] };
}

/**
 * Komut bitti: açık son satır kapanır (renklenmez); çıkış kodu yalnız komutun
 * yankı satırına (lineId) yazılır. Satır 5.000 sınırıyla düştüyse kimse boyanmaz.
 */
export function finish(state: BufferState, lineId: number, exit: number): BufferState {
  const closed = endLine(state);
  const at = closed.lines.findIndex((l) => l.id === lineId);
  if (at < 0) return closed;
  const lines = closed.lines.slice();
  lines[at] = { ...lines[at]!, exit };
  return { ...closed, lines };
}
