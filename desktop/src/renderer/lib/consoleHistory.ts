/**
 * Girdi satırının geçmiş gezintisi (saf, değişmez). Geçmişin kendisi Go'dan
 * gelir ve orada arıtılır (-env değerleri, istem girdisi); arayüz kendi
 * geçmişini yazmaz, yalnız gezer. İmleç `entries.length` ise yazılmakta olan
 * taslaktayız.
 */

export interface HistoryState {
  /** Eskiden yeniye. */
  readonly entries: readonly string[];
  readonly cursor: number;
  readonly draft: string;
}

export function createHistory(entries: readonly string[]): HistoryState {
  return { entries, cursor: entries.length, draft: "" };
}

/** Kullanıcı yazdı: metin taslak olur, imleç taslağa döner. */
export function edit(state: HistoryState, text: string): HistoryState {
  return { entries: state.entries, cursor: state.entries.length, draft: text };
}

export function move(state: HistoryState, direction: "up" | "down"): HistoryState {
  const cursor = direction === "up" ? Math.max(0, state.cursor - 1) : Math.min(state.entries.length, state.cursor + 1);
  return cursor === state.cursor ? state : { ...state, cursor };
}

/** Satırda görünecek metin. */
export function current(state: HistoryState): string {
  return state.cursor < state.entries.length ? (state.entries[state.cursor] ?? "") : state.draft;
}
