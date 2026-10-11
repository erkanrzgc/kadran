import type { AppSpec, AppUpdate } from "./types.ts";

export interface AppDraft {
  appId: string; gitHost: string; gitOwner: string; gitRepo: string; gitBranch: string;
  dockerfilePath: string; containerPort: string; replicas: string; healthPath: string; domain: string;
  memoryMiB: string; cpuMillis: string; blkioWeight: string; envText: string; envRemoveText: string; volumesText: string;
}

export function newAppDraft(): AppDraft {
  return { appId: "", gitHost: "github.com", gitOwner: "", gitRepo: "", gitBranch: "main", dockerfilePath: "Dockerfile", containerPort: "3000", replicas: "1", healthPath: "/healthz", domain: "", memoryMiB: "512", cpuMillis: "1000", blkioWeight: "100", envText: "", envRemoveText: "", volumesText: "" };
}

export function specToDraft(spec: AppSpec): AppDraft {
  return { appId: spec.app_id, gitHost: spec.git_host, gitOwner: spec.git_owner, gitRepo: spec.git_repo, gitBranch: spec.git_branch, dockerfilePath: spec.dockerfile_path, containerPort: String(spec.container_port), replicas: String(spec.replicas), healthPath: spec.health_path, domain: spec.domain, memoryMiB: String(Number(spec.limits.memory_bytes) / 1024 ** 2), cpuMillis: String(spec.limits.cpu_millis), blkioWeight: String(spec.limits.blkio_weight), envText: "", envRemoveText: "", volumesText: spec.volumes.map((v) => `${v.name}=${v.mount_path}${v.read_only ? ":ro" : ""}`).join("\n") };
}

const integer = (value: string, min: number, max: number) => /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) >= min && Number(value) <= max;
const lines = (text: string) => text.split(/\r?\n/).filter((line) => line.trim() !== "");

function parseEnv(text: string): Record<string, string> | null {
  const env: Record<string, string> = {};
  for (const line of lines(text)) {
    const i = line.indexOf("=");
    const key = line.slice(0, i);
    if (i < 1 || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || Object.hasOwn(env, key) || line.includes("\0")) return null;
    Object.defineProperty(env, key, { value: line.slice(i + 1), enumerable: true, writable: true, configurable: true });
  }
  return env;
}

function parseVolumes(text: string): AppSpec["volumes"] | null {
  const volumes: AppSpec["volumes"] = [];
  for (const line of lines(text)) {
    const match = line.trim().match(/^([a-z0-9][a-z0-9-]{0,63})=(\/[^\s:]*)(:ro)?$/);
    if (!match) return null;
    const name = match[1]!;
    const path = match[2]!;
    if (path === "/" || path.endsWith("/") || path.includes("//") || /(?:^|\/)\.{1,2}(?:\/|$)/.test(path) || /^\/(proc|sys|dev)(\/|$)/.test(path)) return null;
    if (volumes.some((v) => v.name === name || v.mount_path === path || v.mount_path.startsWith(path + "/") || path.startsWith(v.mount_path + "/"))) return null;
    volumes.push({ name, mount_path: path, read_only: !!match[3] });
  }
  return volumes;
}

export function validateAppDraft(draft: AppDraft, update = false): string | null {
  if (!/^[a-z][a-z0-9-]{0,31}$/.test(draft.appId)) return "form.invalid_id";
  if ((!update && (!/^[a-zA-Z0-9][a-zA-Z0-9.-]*$/.test(draft.gitHost) || !/^[a-zA-Z0-9_.-]+$/.test(draft.gitOwner) || !/^[a-zA-Z0-9_.-]+$/.test(draft.gitRepo))) || !draft.gitBranch || /[\s\0]/.test(draft.gitBranch)) return "form.invalid_source";
  if (!update && !integer(draft.containerPort, 1, 65535)) return "form.invalid_port";
  if (!integer(draft.replicas, 1, 64)) return "form.invalid_replicas";
  if (!update && (!integer(draft.memoryMiB, 6, 1048576) || !integer(draft.cpuMillis, 10, 64000) || !integer(draft.blkioWeight, 10, 1000))) return "form.invalid_limits";
  if (!update && draft.dockerfilePath && (draft.dockerfilePath.startsWith("/") || /(?:^|\/)\.\.(?:\/|$)/.test(draft.dockerfilePath) || /[\0\\]/.test(draft.dockerfilePath))) return "form.invalid_dockerfile";
  if (draft.domain && (!/^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$/.test(draft.domain) || draft.domain.includes(".."))) return "form.invalid_domain";
  if (draft.healthPath && (!draft.healthPath.startsWith("/") || /[\s\0]/.test(draft.healthPath))) return "form.invalid_health";
  const env = parseEnv(draft.envText);
  if (!env) return "form.invalid_env";
  const removed = lines(draft.envRemoveText).map((line) => line.trim());
  if (removed.some((key) => !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key) || Object.hasOwn(env, key)) || new Set(removed).size !== removed.length) return "form.invalid_env_remove";
  if (!parseVolumes(draft.volumesText)) return "form.invalid_volumes";
  return null;
}

export function draftToSpec(draft: AppDraft): AppSpec {
  const error = validateAppDraft(draft);
  if (error) throw new Error(error);
  return { app_id: draft.appId, git_host: draft.gitHost, git_owner: draft.gitOwner, git_repo: draft.gitRepo, git_branch: draft.gitBranch, dockerfile_path: draft.dockerfilePath, container_port: Number(draft.containerPort), replicas: Number(draft.replicas), health_path: draft.healthPath, domain: draft.domain, limits: { memory_bytes: String(Number(draft.memoryMiB) * 1024 ** 2), cpu_millis: Number(draft.cpuMillis), blkio_weight: Number(draft.blkioWeight) }, build_args: {}, env: parseEnv(draft.envText)!, volumes: parseVolumes(draft.volumesText)! };
}

export function editableUpdate(draft: AppDraft, original: AppSpec): AppUpdate {
  const error = validateAppDraft(draft, true);
  if (error) throw new Error(error);
  const volumes = parseVolumes(draft.volumesText)!;
  return { domain: draft.domain, git_branch: draft.gitBranch, health_path: draft.healthPath, replicas: Number(draft.replicas), env: parseEnv(draft.envText)!, env_remove: lines(draft.envRemoveText).map((line) => line.trim()), volumes, volume_remove: original.volumes.filter((v) => !volumes.some((n) => n.name === v.name)).map((v) => v.name) };
}

export function confirmationMatches(appId: string, value: string): boolean { return appId !== "" && value === appId; }
export function canDeleteApp(detail: { active_release_id?: string }): boolean { return detail.active_release_id === ""; }

export type RequestResult<T> = { status: "success"; value: T } | { status: "error"; error: Error } | { status: "discarded" };

/** Target changes, unmounts and newer reads invalidate previous responses. */
export function createRequestScope() {
  let generation = 0;
  return {
    invalidate() { generation++; },
    async run<T>(work: () => Promise<T>): Promise<RequestResult<T>> {
      const request = ++generation;
      try {
        const value = await work();
        return request === generation ? { status: "success", value } : { status: "discarded" };
      } catch (error) {
        return request === generation ? { status: "error", error: error instanceof Error ? error : new Error(String(error)) } : { status: "discarded" };
      }
    },
  };
}
