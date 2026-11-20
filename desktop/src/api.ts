import { invoke, isTauri } from "@tauri-apps/api/core";

const API = "http://127.0.0.1:47891";

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function readResponse<T>(res: Response): Promise<T> {
  const text = await res.text();
  if (!res.ok) {
    let code = "error";
    let message = text || res.statusText;
    try {
      const body = JSON.parse(text) as { code?: string; message?: string };
      code = body.code || code;
      message = body.message || message;
    } catch {
      /* plain text */
    }
    throw new ApiError(res.status, code, message);
  }
  return (text ? JSON.parse(text) : {}) as T;
}

export function createApiClient(
  fetcher: typeof fetch = (...args) => fetch(...args),
  tokenProvider?: () => Promise<string>,
) {
  let token: string | null = null;
  let pending: Promise<string> | null = null;
  const provide =
    tokenProvider ??
    (async () => {
      if (isTauri()) {
        try {
          return await invoke<string>("local_api_token");
        } catch {
          throw new ApiError(
            503,
            "api_credential_unavailable",
            "Нет доступа к API. Перезапустите приложение через make dev",
          );
        }
      }
      throw new ApiError(
        503,
        "api_desktop_required",
        "Откройте Router BOX через make dev для доступа к API",
      );
    });
  async function credential(): Promise<string> {
    if (token) return token;
    if (!pending) {
      pending = (async () => {
        const next = await provide();
        if (!/^[a-fA-F0-9]{64}$/.test(next)) {
          throw new ApiError(
            503,
            "invalid_api_credential",
            "Недоступен токен API. Перезапустите приложение через make dev",
          );
        }
        token = next;
        return next;
      })().finally(() => {
        pending = null;
      });
    }
    return pending;
  }
  return async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const method = init?.method ?? "GET";
    const publicProbe = method === "GET" && (path === "/api/health" || path === "/api/version");
    const used = publicProbe ? null : await credential();
    const send = async (access: string | null) => {
      const headers = new Headers(init?.headers);
      if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
      headers.delete("Authorization");
      if (access) headers.set("Authorization", `Bearer ${access}`);
      return readResponse<T>(
        await fetcher(`${API}${path}`, { ...init, headers, credentials: "omit" }),
      );
    };
    try {
      return await send(used);
    } catch (error) {
      // Only this outer-guard response proves a mutation has not started.
      if (
        !publicProbe &&
        error instanceof ApiError &&
        error.status === 401 &&
        error.code === "api_auth_required"
      ) {
        if (token === used) token = null;
        return send(await credential());
      }
      throw error;
    }
  };
}

const defaultClient = createApiClient();
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  return defaultClient<T>(path, init);
}

export type RoutePath = "direct" | "work" | "personal";

export interface AutoRoutingSettings {
  autoDetectRouting: boolean;
}

export interface RouterConfigurationState {
  attempt: number;
  phase:
    | "unknown"
    | "preparing"
    | "applying"
    | "applied"
    | "rejected"
    | "rolling_back"
    | "rolled_back"
    | "rollback_failed"
    | "failed";
  reason?: string;
  desiredVersion?: string;
  appliedVersion?: string;
  lastSuccessfulVersion?: string;
  startedAt?: string;
  completedAt?: string;
  lastSuccessfulAt?: string;
  rollback?: "restored" | "preserved" | "file_restored" | "failed";
  error?: string;
  running: boolean;
  matchesSavedSettings?: boolean;
  unchanged?: boolean;
}

export interface StatusResponse {
  internet: {
    up: boolean;
    publicIp?: string;
    latencyMs?: number;
    link: { name: string; state: string; ipv4?: string };
    error?: string;
  };
  systemVpn: {
    connected: boolean;
    state?: "disconnected" | "connecting" | "connected";
    connectionId: string;
    vpnType?: string;
    nmState?: string;
    interface?: string;
    ipv4?: string;
    gateway?: string;
    routeCount?: number;
    error?: string;
    message?: string;
  };
  personalVpn: {
    running: boolean;
    enabled?: boolean;
    routingRunning?: boolean;
    configured?: boolean;
    subscriptionId?: string;
    subscriptionName?: string;
    selectedNodeId?: string;
    message?: string;
    error?: string;
    tunCapable?: boolean;
    hostConfigured?: boolean;
    singBoxPath?: string;
    systemVpnActive?: boolean;
    configMode?: "coexist" | "full";
    configFileMode?: "coexist" | "full" | "";
    configStale?: boolean;
    configuration?: RouterConfigurationState;
  };
}

export interface PersonalReadiness {
  configured: boolean;
  reason?: string;
  message?: string;
}

export type NodeSortMode = "name-asc" | "name-desc" | "ping" | "frequent";

export interface Subscription {
  id: string;
  name: string;
  url: string;
  source?: string;
  refreshIntervalMinutes: number;
  autoRefresh: boolean;
  createdAt?: string;
  lastRefresh?: string;
  selectedNodeId?: string;
  nodeSelectCounts?: Record<string, number>;
  enabled: boolean;
  trafficUpload?: number;
  trafficDownload?: number;
  trafficTotal?: number;
  expireAt?: string;
  profileTitle?: string;
  announce?: string;
  supportUrl?: string;
  profileUpdateIntervalHours?: number;
  importSummary?: string;
  nodeCount?: number;
  skipped?: number;
}

export interface Node {
  id: string;
  name: string;
  protocol: string;
  host: string;
  port: number;
  network?: string;
}

export interface PingResult {
  nodeId: string;
  host: string;
  port: number;
  latencyMs: number;
  ok: boolean;
  error?: string;
  /** tcp | icmp — при включённом личном VPN демон предпочитает ICMP */
  mode?: string;
}

export interface SiteProbe {
  url: string;
  resolvedIp?: string;
  unavailable: boolean;
  bestPath?: string;
  checkedAt?: string;
  results: {
    path: RoutePath;
    available: boolean;
    statusCode?: number;
    latencyMs?: number;
    error?: string;
    skipped?: boolean;
    skipReason?: string;
  }[];
}

export interface DomainRule {
  id: string;
  pattern: string;
  path: RoutePath;
  source?: "manual" | "auto" | "import" | "";
  enabled: boolean;
  createdAt?: string;
  lastCheckedAt?: string;
  lastSuccessAt?: string;
  lastError?: string;
  checkCount?: number;
}

export interface AppRule {
  id: string;
  processName: string;
  execPath?: string;
  path: RoutePath;
  enabled: boolean;
}

export interface RulesResponse {
  domains: DomainRule[];
  apps: AppRule[];
}

export type RuleAdditionStatus = "created" | "updated" | "unchanged";

export interface AutoCheckResponse {
  host: string;
  rule?: DomainRule;
  report: SiteProbe;
  changed: boolean;
  reapplied: boolean;
  suggestedPath?: RoutePath;
  message?: string;
}

export interface AutoSelectAttempt {
  nodeId: string;
  name: string;
  latencyMs: number;
  weight?: number;
  ok: boolean;
  error?: string;
}

export interface AutoSelectStartResponse {
  jobId: string;
  subscriptionId: string;
  total: number;
  phase: string;
}

/** Уже удалённое завершённое задание не должно блокировать следующий запуск. */
export async function cancelAutoSelectJob(job: AutoSelectStartResponse): Promise<void> {
  try {
    await api<AutoSelectJobStatus>(
      `/api/subscriptions/${encodeURIComponent(job.subscriptionId)}/auto-select/${encodeURIComponent(job.jobId)}/cancel`,
      { method: "POST" },
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404 && err.code === "auto_select_job_not_found") {
      return;
    }
    throw err;
  }
}

export interface AutoSelectJobStatus {
  jobId: string;
  subscriptionId: string;
  phase: string;
  checked: number;
  total: number;
  currentName?: string;
  attempts: AutoSelectAttempt[];
  finished: boolean;
  selectedNodeId?: string;
  selectedNodeName?: string;
  reconnected?: boolean;
  message?: string;
  error?: string;
}

/** @deprecated use AutoSelectJobStatus; оставлен для совместимости тестов UI */
export type AutoSelectResponse = AutoSelectJobStatus;
