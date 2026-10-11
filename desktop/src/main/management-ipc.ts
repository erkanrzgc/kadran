/** Narrow, bounded management inputs. Server/executor remain policy authorities. */
const CREATE_FIELDS = ["app_id", "git_host", "git_owner", "git_repo", "git_branch", "dockerfile_path", "build_args", "container_port", "replicas", "health_path", "domain", "limits", "env", "volumes"];
const UPDATE_FIELDS = ["domain", "git_branch", "health_path", "replicas", "env", "env_remove", "volumes", "volume_remove"];

export function requireAppId(value: unknown): string {
  if (typeof value !== "string" || !/^[a-z][a-z0-9-]{0,31}$/.test(value)) throw new Error("invalid app ID");
  return value;
}
export function requireConfirmation(appId: string, value: unknown): void {
  if (value !== appId) throw new Error("confirmation must match the app ID exactly");
}
export function requireRef(value: unknown): string {
  if (typeof value !== "string" || (value !== "" && !/^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$/.test(value))) throw new Error("invalid branch, tag or full commit");
  return value;
}
export function requireTail(value: unknown): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 1 || value > 10000) throw new Error("log tail must be 1-10000 lines");
  return value;
}
export function requireAppPayload(value: unknown, mode: "create" | "update"): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("app payload must be an object");
  const fields = mode === "create" ? CREATE_FIELDS : UPDATE_FIELDS;
  if (Object.keys(value).some((key) => !fields.includes(key))) throw new Error("unknown app field");
  const encoded = JSON.stringify(value);
  if (Buffer.byteLength(encoded, "utf8") > 512 * 1024) throw new Error("app payload is too large");
  const copy = JSON.parse(encoded) as Record<string, unknown>;
  if (mode === "create") requireAppId(copy.app_id);
  return copy;
}
/** Keep buttons, repeated IPC and native dialogs from starting the same action twice. */
export function createManagementGate() {
  const active = new Set<string>();
  return async <T>(target: string, appId: string, work: () => Promise<T>): Promise<T> => {
    const key = JSON.stringify([target, appId]);
    if (active.has(key)) throw new Error("an operation for this app is already running");
    active.add(key);
    try { return await work(); } finally { active.delete(key); }
  };
}
