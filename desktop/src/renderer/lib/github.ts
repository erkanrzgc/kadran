import { errorText } from "./errors.ts";

/**
 * GitHub bağlantısı (K-167). Sunucu protojson döner (alan adları .proto'daki
 * gibi); burada arayüzün tükettiği biçime çevrilir. Ayrıştırma ASLA atmaz:
 * bozuk ya da bilinmeyen yanıt "failed" olur. Ekranda gösterilecek her dize
 * (kullanıcı kodu, adresler) burada süzülür; süzgeçi geçmeyen değer null'dır
 * ve ekran onu hiç göstermez.
 */

export type GitHubState = "not_connected" | "pending" | "connected" | "failed";

export interface GitHubPrompt {
  /** Yalnız XXXX-XXXX biçimindeyse; yoksa null (ekran CLI'a yönlendirir). */
  userCode: string | null;
  verificationUri: string | null;
  installUrl: string | null;
  /** Unix ms; bilinmiyorsa null. */
  expiresAt: number | null;
}

export interface GitHubRepo {
  fullName: string;
  isPrivate: boolean;
}

export interface GitHubInstallation {
  account: string;
  allRepositories: boolean;
  truncated: boolean;
  repositories: GitHubRepo[];
}

export interface GitHubConnection {
  state: GitHubState;
  login: string;
  prompt: GitHubPrompt | null;
  installations: GitHubInstallation[];
  /** i18n anahtarı; yalnız state === "failed" iken dolu. */
  failureKey: string | null;
  reconnectRequired: boolean;
  installUrl: string | null;
  hasAllRepositories: boolean;
}

/** Durum yoklama aralığı; yalnız bekleyen akışta ve pencere görünürken. */
export const GITHUB_POLL_MS = 3000;
/** Bağlantıyı kesmek için yazılan sözcük (Go'daki guard ve sidecar ile aynı). */
export const DISCONNECT_WORD = "disconnect";

const USER_CODE = /^[A-Z0-9]{4}-[A-Z0-9]{4}$/;
const INSTALL_URL = /^https:\/\/github\.com\/apps\/[a-z0-9-]{1,100}\/installations\/new$/;
// Go tarafındaki guard.GitHubURL ile AYNI dört biçim.
const FIXED_URLS = new Set(["https://github.com/login/device", "https://github.com/settings/apps/authorizations", "https://github.com/settings/installations"]);

const FAILURE_KEYS: Readonly<Record<string, string>> = {
  GIT_HUB_FAILURE_ACCESS_DENIED: "github.failure_access_denied",
  GIT_HUB_FAILURE_CODE_EXPIRED: "github.failure_code_expired",
  GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED: "github.failure_device_flow_disabled",
  GIT_HUB_FAILURE_BAD_CLIENT: "github.failure_bad_client",
  GIT_HUB_FAILURE_NETWORK: "github.failure_network",
  GIT_HUB_FAILURE_RATE_LIMITED: "github.failure_rate_limited",
};
const FAILURE_FALLBACK = "github.failure_internal";

const STATES: Readonly<Record<string, GitHubState>> = {
  STATE_NOT_CONNECTED: "not_connected",
  STATE_PENDING: "pending",
  STATE_CONNECTED: "connected",
  STATE_FAILED: "failed",
};

type Raw = Record<string, unknown>;
const isRecord = (v: unknown): v is Raw => typeof v === "object" && v !== null && !Array.isArray(v);

export function isGitHubUrl(value: unknown): value is string {
  return typeof value === "string" && (FIXED_URLS.has(value) || INSTALL_URL.test(value));
}

const safeUrl = (value: unknown): string | null => (isGitHubUrl(value) ? value : null);

function parsePromptBody(raw: unknown): GitHubPrompt | null {
  if (!isRecord(raw)) return null;
  const code = raw.user_code;
  const expires = typeof raw.expires_at === "string" ? Date.parse(raw.expires_at) : Number.NaN;
  return {
    userCode: typeof code === "string" && USER_CODE.test(code) ? code : null,
    verificationUri: safeUrl(raw.verification_uri),
    installUrl: safeUrl(raw.install_url),
    expiresAt: Number.isFinite(expires) ? expires : null,
  };
}

/** `github.connect` yanıtı: `{prompt}`. */
export function parseGitHubPrompt(raw: unknown): GitHubPrompt | null {
  return isRecord(raw) ? parsePromptBody(raw.prompt) : null;
}

function parseRepo(raw: unknown): GitHubRepo | null {
  if (!isRecord(raw) || typeof raw.full_name !== "string") return null;
  return { fullName: raw.full_name, isPrivate: raw.private === true };
}

function parseInstallation(raw: unknown): GitHubInstallation | null {
  if (!isRecord(raw) || typeof raw.account !== "string") return null;
  const repos = Array.isArray(raw.repositories) ? raw.repositories.map(parseRepo).filter((r): r is GitHubRepo => r !== null) : [];
  return { account: raw.account, allRepositories: raw.all_repositories === true, truncated: raw.truncated === true, repositories: repos };
}

function empty(state: GitHubState, failureKey: string | null): GitHubConnection {
  return { state, login: "", prompt: null, installations: [], failureKey, reconnectRequired: false, installUrl: null, hasAllRepositories: false };
}

/** `github.status` yanıtı: `{connection}`. Bilinmeyen ya da bozuk her şey "failed". */
export function parseGitHubConnection(raw: unknown): GitHubConnection {
  const c = isRecord(raw) ? raw.connection : undefined;
  if (!isRecord(c)) return empty("failed", FAILURE_FALLBACK);
  const state = typeof c.state === "string" ? STATES[c.state] : undefined;
  if (state === undefined) return empty("failed", FAILURE_FALLBACK);
  const installations = Array.isArray(c.installations) ? c.installations.map(parseInstallation).filter((i): i is GitHubInstallation => i !== null) : [];
  const failure = typeof c.failure === "string" ? c.failure : "";
  return {
    state,
    login: typeof c.login === "string" ? c.login : "",
    prompt: state === "pending" ? parsePromptBody(c.pending) : null,
    installations,
    failureKey: state === "failed" ? (FAILURE_KEYS[failure] ?? FAILURE_FALLBACK) : null,
    reconnectRequired: c.reconnect_required === true,
    installUrl: safeUrl(c.install_url),
    hasAllRepositories: installations.some((i) => i.allRepositories),
  };
}

/**
 * Eski sunucuda RPC yok (gRPC Unimplemented): hata değil, "sunucuyu
 * yükseltin" (resourceFailure ile aynı kural).
 */
export function githubFailure(error: unknown): "unsupported" | "failed" {
  if (error === undefined || error === null) return "failed";
  return /code = Unimplemented/.test(errorText(error)) ? "unsupported" : "failed";
}

/** Yoklama yalnız akış bekliyorken ve pencere görünürken (K-166 deseni). */
export function shouldPoll(state: GitHubState, hidden: boolean): boolean {
  return state === "pending" && !hidden;
}

/** Bağlantıyı kesme düğmesi yalnız TAM sözcük yazılınca açılır. */
export function canDisconnect(typed: string): boolean {
  return typed === DISCONNECT_WORD;
}

/** Kalan tam saniye (yukarı yuvarlanır); süre bilinmiyorsa null, dolduysa 0. */
export function remainingSeconds(expiresAt: number | null, now: number): number | null {
  if (expiresAt === null) return null;
  return Math.max(0, Math.ceil((expiresAt - now) / 1000));
}

export function formatRemaining(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

/** `github.connect` sonrası, ilk yoklamadan önce ekranın göstereceği bekleyen durum. */
export function pendingConnection(prompt: GitHubPrompt): GitHubConnection {
  return { ...empty("pending", null), prompt, installUrl: prompt.installUrl };
}
