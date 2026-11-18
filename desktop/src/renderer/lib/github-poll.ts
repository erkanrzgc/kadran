/**
 * GitHub bekleyen akışının yoklaması (K-167). useGitHub kancasının içindeki
 * zamanlayıcı bağı burada, React'siz: ortam (zamanlayıcı, görünürlük)
 * dışarıdan verilir, böylece testler sahte ortamla sürer. Yoklama yalnız akış
 * BEKLERKEN ve pencere görünürken olur (shouldPoll); çağrılar üst üste binmez;
 * dönen işlev zamanlayıcıyı ve görünürlük dinleyicisini bırakır.
 */

import { GITHUB_POLL_MS, shouldPoll, type GitHubState } from "./github.ts";

export interface PollEnv {
  hidden(): boolean;
  setInterval(fn: () => void, ms: number): unknown;
  clearInterval(id: unknown): void;
  onVisibility(fn: () => void): void;
  offVisibility(fn: () => void): void;
}

export function startGitHubPolling(state: GitHubState, read: () => Promise<void>, env: PollEnv): () => void {
  if (state !== "pending") return () => undefined;
  let running = false;
  const tick = async () => {
    if (running || !shouldPoll(state, env.hidden())) return;
    running = true;
    try {
      await read();
    } finally {
      running = false;
    }
  };
  const onVisible = () => void tick();
  const timer = env.setInterval(() => void tick(), GITHUB_POLL_MS);
  env.onVisibility(onVisible);
  return () => {
    env.clearInterval(timer);
    env.offVisibility(onVisible);
  };
}

/** Gerçek ortam: tarayıcı penceresi. Yalnız çalışma anında çağrılır. */
export function browserPollEnv(): PollEnv {
  return {
    hidden: () => document.hidden,
    setInterval: (fn, ms) => window.setInterval(fn, ms),
    clearInterval: (id) => window.clearInterval(id as number),
    onVisibility: (fn) => document.addEventListener("visibilitychange", fn),
    offVisibility: (fn) => document.removeEventListener("visibilitychange", fn),
  };
}
