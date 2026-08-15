import type { PersonalReadiness, StatusResponse } from "./api";

export type SystemVpnUiState = "disconnected" | "connecting" | "connected";

export function systemVpnState(s: StatusResponse["systemVpn"]): SystemVpnUiState {
  if (s.state) return s.state;
  return s.connected ? "connected" : "disconnected";
}

/** Подпись badge при connecting (MFA vs обычное подключение). */
export function systemVpnConnectingHint(nmState?: string): string {
  return nmState && /need|auth/i.test(nmState) ? "Ожидание MFA" : "Подключение…";
}

export type PersonalVpnBusy = "connect" | "disconnect" | null;

/** Текст прогресса на карточке личного VPN во время connect/disconnect. */
export function personalVpnBusyMessage(busy: PersonalVpnBusy): string | null {
  if (busy === "connect") {
    return "Идёт включение: проверка сервера и запуск маршрутизатора…";
  }
  if (busy === "disconnect") {
    return "Идёт выключение личного VPN…";
  }
  return null;
}

/** Подписи кнопок на время операции (null = вернуть обычные). */
export function personalVpnBusyButtonLabels(busy: PersonalVpnBusy): {
  on: string;
  off: string;
} {
  if (busy === "connect") {
    return { on: "Включение…", off: "Выключить" };
  }
  if (busy === "disconnect") {
    return { on: "Включить", off: "Выключение…" };
  }
  return { on: "Включить", off: "Выключить" };
}

export type PersonalConnectGate = "ready" | "host_not_ready" | "setup";

/** Решение по readiness перед POST /personal-vpn/connect. */
export function personalConnectGate(ready: PersonalReadiness): PersonalConnectGate {
  if (ready.configured) return "ready";
  if (ready.reason === "host_not_ready") return "host_not_ready";
  return "setup";
}

export type PersonalStatusFlags = {
  configured: boolean;
  showHostSyncHint: boolean;
  showConfigStale: boolean;
  showPolkitAuthHint: boolean;
  showCoexistHint: boolean;
};

/** Флаги предупреждений для карточки личного VPN из /api/status. */
export function personalStatusFlags(pv: StatusResponse["personalVpn"]): PersonalStatusFlags {
  return {
    configured: pv.configured ?? false,
    showHostSyncHint: pv.hostConfigured === false,
    showConfigStale: Boolean(pv.configStale),
    showPolkitAuthHint: Boolean(pv.running && pv.configFileMode === "full" && pv.systemVpnActive),
    showCoexistHint: pv.configMode === "coexist",
  };
}

/** Минимальная поддерживаемая версия API демона (UI). */
export const MIN_API_VERSION = 5;

export function isDaemonApiOutdated(apiVersion?: number): boolean {
  return (apiVersion ?? 0) < MIN_API_VERSION;
}
