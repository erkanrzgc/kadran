/**
 * Konsolun hedef sunucusu (saf, değişmez). GUI bağlantısından bağımsızdır:
 * oturumu düşmüş sunucuda GUI bağlanamasa da konsol `use` ile o sunucuya geçer
 * ve `login` çalıştırabilir. Arayüz güvenilmezdir (K-165): burada yalnız hangi
 * adın istemde görüneceği seçilir; kabul kararı Go'dadır.
 *
 * Çalışma sürerken gelen değişiklik `pending`'e yazılır, çalışma bitince
 * uygulanır: komut başladığı sunucuda biter.
 */

export type TargetEvent = { kind: "gui"; server: string | null } | { kind: "use"; server: string } | { kind: "done" };

export interface ConsoleTarget {
  /** Konsolun sunucusu; yoksa boş. */
  server: string;
  /** Çalışma bitince geçilecek sunucu. */
  pending: string | null;
  running: boolean;
}

function change(t: ConsoleTarget, server: string): ConsoleTarget {
  return t.running ? { ...t, pending: server } : { ...t, server, pending: null };
}

export function nextTarget(t: ConsoleTarget, e: TargetEvent): ConsoleTarget {
  switch (e.kind) {
    case "gui":
      // Liste ekranına dönmek konsolun sunucusunu silmez.
      return e.server === null ? t : change(t, e.server);
    case "use":
      return change(t, e.server);
    case "done":
      return { server: t.pending ?? t.server, pending: null, running: false };
  }
}

/** Çalışma başladı ya da bitti (bitiş `done` gibi bekleyeni uygular). */
export function setRunning(t: ConsoleTarget, running: boolean): ConsoleTarget {
  return running ? (t.running ? t : { ...t, running: true }) : nextTarget(t, { kind: "done" });
}

/** Satır `login` mi (yalnız görünüm: başarıdan sonra GUI yeniden bağlanmayı dener). */
export function isLoginLine(line: string): boolean {
  const words = line.trim().split(/\s+/);
  if (words[0] === "kadran") words.shift();
  return words[0] === "login";
}
