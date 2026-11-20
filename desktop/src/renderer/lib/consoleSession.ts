/**
 * Konsol oturumu (saf, değişmez): çalışma kimliği, istem kilidi, sunucu
 * kilidi ve gizli çıktı kartı. Arayüz güvenilmezdir (K-165): sınıflandırma,
 * onay, geçmiş ve kayıt Go'dadır; burada yalnız Go'nun olayları gösterilir.
 *
 * Bilinen zamanlama:
 * - Bir çalıştırmanın olayları consoleRun kimliği döndürmeden ÖNCE gelebilir:
 *   başlarken bilinmeyen kimlik geçerli çalışma sayılır, olay düşmez.
 * - Nadiren output olayı done'dan sonra gelir: tampona eklenir, istemi yeniden
 *   açmaz ve kilitlemez.
 *
 * Gizli değerler: kart içeriği yalnız `card` alanında yaşar; tampona, geçmişe
 * ya da başka bir alana yazılmaz. İstem girdisi (TOTP kodu olabilir) bu
 * katmana hiç girmez: answer() metin almaz.
 */

import type { ConsoleEvent } from "../bridge.ts";
import { append, emptyBuffer, endLine, finish, type BufferState } from "./consoleBuffer.ts";

/** Bitmiş çalışma kimlikleri: gecikmiş olay yeni çalışma sanılmasın. */
const FINISHED_KEPT = 8;

export interface ConsoleSession {
  readonly buffer: BufferState;
  /** consoleRun gönderildi, kimlik henüz dönmedi. */
  readonly starting: boolean;
  readonly run: string | null;
  /** Çalışmanın başladığı sunucu; istem bunu gösterir. */
  readonly runServer: string | null;
  /** Çalışan satır root sınıfında mı (yalnız görünüm: istem `#` ve kırmızı). */
  readonly root: boolean;
  readonly finished: readonly string[];
  /** Komut bir istem bekliyor (console:input). */
  readonly input: boolean;
  /** Gizli çıktı kartı; kapalıyken null (boş dize değil). */
  readonly card: string | null;
  /** Panel acik mi. Kapaliyken gizli cikti karta girmez (yalniz not satiri). */
  readonly panelOpen: boolean;
}

export function initialSession(): ConsoleSession {
  return { buffer: emptyBuffer(), starting: false, run: null, runServer: null, root: false, finished: [], input: false, card: null, panelOpen: false };
}

/** Çalışma var ya da başlıyor: istem kilitli, sunucu sabit. */
export function isBusy(s: ConsoleSession): boolean {
  return s.starting || s.run !== null;
}

/** İstemin gösterdiği sunucu: çalışırken başladığı sunucu, değilse seçili olan. */
export function promptServer(s: ConsoleSession, selected: string): string {
  return isBusy(s) && s.runServer !== null ? s.runServer : selected;
}

/**
 * Yalnız görünüm için: root sınıfındaki satır çalışırken istem `#` olur. Asıl
 * sınıflandırma Go'dadır (gate.go); burası yanılsa yalnız istemin rengi şaşar.
 */
export function isRootLine(line: string): boolean {
  const words = line.trim().split(/\s+/);
  if (words[0] === "kadran") words.shift();
  if (words[0] === "key") return ["list", "add", "remove"].includes(words[1] ?? "");
  return words[0] === "totp" && words[1] === "reset";
}

/** Satır gönderilirken: yazılan satır yankılanır, çalışma başlıyor sayılır. */
export function begin(s: ConsoleSession, server: string, line: string): ConsoleSession {
  const name = server === "" ? "–" : server;
  return {
    ...s,
    buffer: append(s.buffer, "echo", `${name} › ${line}\n`),
    starting: true,
    run: null,
    runServer: server,
    root: isRootLine(line),
    input: false,
  };
}

/** consoleRun kimliği döndü. Olaylar önce geldiyse zaten benimsenmiştir. */
export function started(s: ConsoleSession, runId: string): ConsoleSession {
  if (s.run === runId) return { ...s, starting: false };
  if (s.finished.includes(runId)) return { ...s, starting: false };
  return { ...s, starting: false, run: runId };
}

/** consoleRun reddedildi (köprü hatası): kilit açılır, sebep stderr olarak görünür. */
export function startFailed(s: ConsoleSession, message: string): ConsoleSession {
  return { ...s, buffer: append(s.buffer, "err", `${message}\n`), starting: false, run: null, input: false };
}

/** Bu kimlik geçerli çalışma mı; başlarken bilinmeyen yeni kimlik benimsenir. */
function claim(s: ConsoleSession, id: string): ConsoleSession | null {
  if (s.run === id) return s;
  if (s.run === null && s.starting && !s.finished.includes(id)) return { ...s, run: id };
  return null;
}

function finishRun(s: ConsoleSession, id: string, exit: number): ConsoleSession {
  return {
    ...s,
    buffer: finish(s.buffer, exit),
    run: null,
    input: false,
    finished: [...s.finished, id].slice(-FINISHED_KEPT),
    // R14: sıfırla biten komutta kart açık kalır (totp enable kurtarma kodlarını
    // en son basar); sıfır dışında hemen kapanır.
    card: exit === 0 ? s.card : null,
  };
}

/** Go'dan gelen konsol olayı. `console:use` arayüzün işidir (kenar çubuğu), burada yok sayılır. */
export function applyEvent(s: ConsoleSession, event: ConsoleEvent, secretNote: string): ConsoleSession {
  switch (event.name) {
    case "console:clear":
      return clearOutput(s);
    case "console:output": {
      const owner = claim(s, event.data.run) ?? s;
      return { ...owner, buffer: append(owner.buffer, event.data.stream, event.data.data) };
    }
    case "console:input": {
      const owner = claim(s, event.data.run);
      return owner ? { ...owner, input: true } : s;
    }
    case "console:secret": {
      const owner = claim(s, event.data.run);
      if (!owner || event.data.data === "") return s;
      // Tamponda yalnız bir not satırı; kartın metni asla.
      const last = owner.buffer.lines[owner.buffer.lines.length - 1];
      const noted = owner.card !== null || (last?.stream === "note" && last.text === secretNote);
      const buffer = noted ? owner.buffer : append(owner.buffer, "note", `${secretNote}\n`);
      // Panel kapalıyken sır hiçbir yerde tutulmaz: kart null kalır, parça düşer.
      if (!owner.panelOpen) return { ...owner, buffer };
      return { ...owner, buffer, card: (owner.card ?? "") + event.data.data };
    }
    case "console:done": {
      const owner = claim(s, event.data.run);
      return owner ? finishRun(owner, event.data.run, event.data.exit) : s;
    }
    default:
      return s;
  }
}

/** Kullanıcı istemi yanıtladı: istem satırı yerinde biter, yazılan metin hiçbir yere girmez. */
export function answer(s: ConsoleSession): ConsoleSession {
  return { ...s, input: false, buffer: endLine(s.buffer) };
}

/** "Kaydettim": kart kapanır, içerik durumdan gider. */
export function closeCard(s: ConsoleSession): ConsoleSession {
  return s.card === null ? s : { ...s, card: null };
}

/** Panel açıldı ya da kapandı. Kapanınca kart silinir; komut ve kimliği durmaz. */
export function setPanelOpen(s: ConsoleSession, open: boolean): ConsoleSession {
  if (open) return s.panelOpen ? s : { ...s, panelOpen: true };
  return { ...closeCard(s), panelOpen: false };
}

export function closePanel(s: ConsoleSession): ConsoleSession {
  return setPanelOpen(s, false);
}

/** Çıktıyı temizler (Ctrl+L, `clear`); çalışma durumuna dokunmaz. */
export function clearOutput(s: ConsoleSession): ConsoleSession {
  return { ...s, buffer: emptyBuffer() };
}

/** Konsolun kendi notu (tamamlama adayları). */
export function addNote(s: ConsoleSession, text: string): ConsoleSession {
  return { ...s, buffer: append(s.buffer, "note", `${text}\n`) };
}
