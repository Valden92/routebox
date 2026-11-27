import type { NetworkApp } from "./api";

const states: Record<string, string> = {
  ESTAB: "Установлено",
  LISTEN: "Ожидание подключений",
  UNCONN: "Без соединения",
  "SYN-SENT": "Подключение",
  "SYN-RECV": "Получен запрос",
  "FIN-WAIT-1": "Завершение",
  "FIN-WAIT-2": "Ожидание завершения",
  "TIME-WAIT": "Ожидание освобождения",
  "CLOSE-WAIT": "Ожидание закрытия",
  "LAST-ACK": "Ожидание подтверждения",
  CLOSING: "Закрытие",
  CLOSED: "Закрыто",
};

export function socketState(state: string): string {
  return states[state] ? `${states[state]} · ${state}` : state;
}

export function networkAppName(app: NetworkApp): string {
  return app.processName || (app.pid > 0 ? `Процесс #${app.pid}` : "Процесс не определён");
}

export interface ConnectionFilters {
  query: string;
  protocol: "all" | "tcp" | "udp";
}

export function filterNetworkApps(apps: NetworkApp[], filters: ConnectionFilters): NetworkApp[] {
  const query = filters.query.trim().toLowerCase();
  return apps.flatMap((app) => {
    const processMatches = [networkAppName(app), app.execPath ?? "", String(app.pid)].some(
      (value) => value.toLowerCase().includes(query),
    );
    if (!app.sockets) return filters.protocol === "all" && processMatches ? [app] : [];
    const sockets = app.sockets.filter(
      (socket) =>
        (filters.protocol === "all" || socket.protocol === filters.protocol) &&
        (processMatches ||
          [
            socket.localAddress,
            socket.remoteAddress,
            socket.protocol,
            socketState(socket.state),
          ].some((value) => value.toLowerCase().includes(query))),
    );
    return sockets.length ? [{ ...app, sockets }] : [];
  });
}

export interface ConnectionsState {
  apps: NetworkApp[] | null;
  pending: boolean;
  receivedAt: Date | null;
  error: string;
}

/** A failed scan preserves the last snapshot and its original timestamp. */
export class ConnectionsController {
  private state: ConnectionsState = { apps: null, pending: false, receivedAt: null, error: "" };

  constructor(
    private readonly scan: () => Promise<NetworkApp[] | null>,
    private readonly changed: (state: ConnectionsState) => void,
  ) {}

  async refresh(): Promise<void> {
    if (this.state.pending) return;
    this.state = { ...this.state, pending: true, error: "" };
    this.changed(this.state);
    try {
      const apps = await this.scan();
      this.state = { ...this.state, apps: apps ?? [], receivedAt: new Date() };
    } catch (error) {
      this.state = {
        ...this.state,
        error: error instanceof Error ? error.message : "Не удалось получить сетевой снимок",
      };
    } finally {
      this.state = { ...this.state, pending: false };
      this.changed(this.state);
    }
  }
}
