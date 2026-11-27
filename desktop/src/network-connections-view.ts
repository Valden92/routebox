import { api, type NetworkApp } from "./api";
import { escapeHtml } from "./ui-labels";
import {
  ConnectionsController,
  filterNetworkApps,
  networkAppName,
  socketState,
  type ConnectionsState,
  type ConnectionFilters,
} from "./network-connections";

export class NetworkConnectionsView {
  private readonly controller: ConnectionsController;
  private state: ConnectionsState = { apps: null, pending: false, receivedAt: null, error: "" };
  private readonly expanded = new Set<number>();
  private readonly pendingRules = new Set<string>();
  private filtered = false;

  constructor(
    private readonly root: HTMLElement,
    private readonly notify: (message: string, error?: boolean) => void,
    private readonly onRuleChanged: () => Promise<unknown>,
    scan = () => api<NetworkApp[]>("/api/apps"),
    private readonly routePersonal = (name: string) =>
      api("/api/rules", {
        method: "POST",
        body: JSON.stringify({ processName: name, path: "personal" }),
      }),
  ) {
    this.controller = new ConnectionsController(scan, (state) => {
      this.state = state;
      this.render();
    });
    this.root
      .querySelector<HTMLButtonElement>("#btn-scan-apps")!
      .addEventListener("click", () => void this.controller.refresh());
    this.root
      .querySelector<HTMLInputElement>("#connections-search")!
      .addEventListener("input", () => this.render());
    this.root
      .querySelector<HTMLSelectElement>("#connections-protocol")!
      .addEventListener("change", () => this.render());
  }

  private render() {
    const list = this.root.querySelector<HTMLElement>("#apps-list")!;
    if (!this.filtered) {
      for (const details of list.querySelectorAll<HTMLDetailsElement>(
        "details[data-process-pid]",
      )) {
        const pid = Number(details.dataset.processPid);
        if (details.open) this.expanded.add(pid);
        else this.expanded.delete(pid);
      }
    }
    const filters: ConnectionFilters = {
      query: this.root.querySelector<HTMLInputElement>("#connections-search")!.value,
      protocol: this.root.querySelector<HTMLSelectElement>("#connections-protocol")!
        .value as ConnectionFilters["protocol"],
    };
    this.filtered = Boolean(filters.query.trim() || filters.protocol !== "all");
    const state = this.state;
    const button = this.root.querySelector<HTMLButtonElement>("#btn-scan-apps")!;
    button.disabled = state.pending;
    button.textContent = state.pending ? "Сканирование…" : "Обновить снимок";
    button.setAttribute("aria-busy", String(state.pending));
    const error = this.root.querySelector<HTMLElement>("#connections-error")!;
    error.textContent = state.error
      ? `${state.error}${state.apps ? ". Показан предыдущий снимок." : ""}`
      : "";
    error.classList.toggle("hidden", !state.error);
    const status = this.root.querySelector<HTMLElement>("#connections-status")!;
    status.textContent = state.receivedAt
      ? `Снимок получен: ${state.receivedAt.toLocaleTimeString("ru-RU")}`
      : "Нажмите «Обновить снимок»";
    if (state.apps === null) return;
    const apps = filterNetworkApps(state.apps, filters);
    status.textContent += ` · Показано групп: ${apps.length} из ${state.apps.length}`;
    list.innerHTML = apps.length
      ? apps.map((app) => this.appMarkup(app)).join("")
      : `<p class="muted">${state.apps.length ? "По заданным фильтрам сокетов нет." : "TCP/UDP-сокеты не найдены."}</p>`;
    list.querySelectorAll<HTMLButtonElement>("[data-route-process]").forEach((button) => {
      button.addEventListener("click", async () => {
        const name = button.dataset.routeProcess!;
        if (this.pendingRules.has(name)) return;
        this.pendingRules.add(name);
        this.render();
        try {
          await this.routePersonal(name);
          await this.onRuleChanged();
          this.notify("Правило приложения для личного VPN сохранено");
        } catch (error) {
          this.notify(
            error instanceof Error ? error.message : "Не удалось сохранить правило приложения",
            true,
          );
        } finally {
          this.pendingRules.delete(name);
          this.render();
        }
      });
    });
  }

  private appMarkup(app: NetworkApp): string {
    const sockets = app.sockets;
    const count = sockets
      ? `Сокеты: ${sockets.length}${sockets.length !== app.connections ? ` / ${app.connections}` : ""}`
      : `TCP-соединения: ${app.connections}`;
    const rows =
      sockets
        ?.map(
          (socket) => `<li class="connection-row">
      <div class="connection-protocol"><strong>${escapeHtml(socket.protocol.toUpperCase())}</strong><span class="muted">${escapeHtml(socketState(socket.state))}</span></div>
      <div class="connection-address"><span class="muted">Локальный адрес</span><code>${escapeHtml(socket.localAddress)}</code></div>
      <div class="connection-address"><span class="muted">Удалённый адрес</span><code>${escapeHtml(socket.remoteAddress)}</code></div>
    </li>`,
        )
        .join("") ?? "";
    return `<details class="connection-app" data-process-pid="${app.pid}" ${this.filtered || this.expanded.has(app.pid) ? "open" : ""}>
      <summary><span class="connection-name">${escapeHtml(networkAppName(app))}</span><span class="muted">${app.pid > 0 ? `PID ${app.pid}` : "Владелец недоступен"}</span><span class="connection-count">${count}</span></summary>
      <div class="connection-body">
        <div class="connection-process"><code>${escapeHtml(app.execPath || "Путь процесса недоступен")}</code>${app.pid > 0 && app.processName ? `<button class="secondary small" data-route-process="${escapeHtml(app.processName)}" title="Правило действует на все процессы с таким именем, а не только на этот PID" ${this.pendingRules.has(app.processName) ? "disabled" : ""}>${this.pendingRules.has(app.processName) ? "Применение…" : "Через личный VPN"}</button>` : ""}</div>
        ${sockets ? `<ul class="connection-sockets">${rows}</ul>` : '<p class="muted">Подробности сокетов недоступны. Перезапустите приложение через make dev.</p>'}
      </div>
    </details>`;
  }
}
