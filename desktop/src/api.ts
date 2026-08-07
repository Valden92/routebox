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

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  const text = await res.text();
  if (!res.ok) {
    let code = "error";
    let message = text || res.statusText;
    try {
      const j = JSON.parse(text) as { code?: string; message?: string };
      if (j.code) code = j.code;
      if (j.message) message = j.message;
    } catch {
      /* plain text */
    }
    throw new ApiError(res.status, code, message);
  }
  if (!text) return {} as T;
  return JSON.parse(text) as T;
}

export type RoutePath = "direct" | "work" | "personal";

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
  refreshIntervalMinutes: number;
  autoRefresh: boolean;
  selectedNodeId?: string;
  nodeSelectCounts?: Record<string, number>;
  enabled: boolean;
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

export interface AutoCheckResponse {
  host: string;
  rule?: DomainRule;
  report: SiteProbe;
  changed: boolean;
  reapplied: boolean;
  suggestedPath?: RoutePath;
  message?: string;
}
