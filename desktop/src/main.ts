import {
  api,
  ApiError,
  type AutoCheckResponse,
  type DomainRule,
  type Node,
  type NodeSortMode,
  type PersonalReadiness,
  type PingResult,
  type RoutePath,
  type RulesResponse,
  type SiteProbe,
  type StatusResponse,
  type Subscription,
} from "./api";
import { formatPing } from "./ping-label";

const pathLabels: Record<RoutePath, string> = {
  direct: "Без VPN",
  work: "Системный VPN",
  personal: "Личный VPN",
};

function routerModeLabel(mode?: string): string {
  if (mode === "coexist") {
    return "Совместно с системным VPN";
  }
  return "Обычный";
}

function routerModeHint(mode?: string): string {
  if (mode === "coexist") {
    return "Системный VPN активен: рабочие сайты идут через него, остальное — по правилам или напрямую.";
  }
  return "Системный VPN выключен: трафик идёт напрямую или через личный VPN по правилам.";
}

function routerPriorityLabel(mode?: string): string {
  if (mode === "coexist") {
    return "Обычная сеть > Системный VPN > Личный VPN";
  }
  return "Обычная сеть > Личный VPN";
}

function el<T extends HTMLElement>(sel: string): T {
  return document.querySelector(sel) as T;
}

function badge(ok: boolean, on: string, off: string) {
  return `<span class="badge ${ok ? "badge-ok" : "badge-err"}">${ok ? on : off}</span>`;
}

function systemVpnState(
  s: StatusResponse["systemVpn"],
): "disconnected" | "connecting" | "connected" {
  if (s.state) return s.state;
  return s.connected ? "connected" : "disconnected";
}

function systemVpnBadge(s: StatusResponse["systemVpn"]) {
  const st = systemVpnState(s);
  if (st === "connected") return badge(true, "Подключён", "Отключён");
  if (st === "connecting") {
    const hint = s.nmState && /need|auth/i.test(s.nmState) ? "Ожидание MFA" : "Подключение…";
    return `<span class="badge badge-warn">${hint}</span>`;
  }
  return badge(false, "Подключён", "Отключён");
}

function switchTab(name: string) {
  document.querySelectorAll(".tab").forEach((t) => {
    t.classList.toggle("active", (t as HTMLButtonElement).dataset.tab === name);
  });
  document.querySelectorAll(".panel").forEach((p) => {
    p.classList.toggle("active", p.id === `tab-${name}`);
  });
}

function showToast(message: string, isError = false) {
  const t = el("#toast");
  t.textContent = message;
  t.className = isError ? "toast err" : "toast";
  t.classList.remove("hidden");
  window.setTimeout(() => t.classList.add("hidden"), 4500);
}

function showPersonalSetupBanner(message: string) {
  const b = el("#personal-setup-banner");
  b.innerHTML = `<strong>Настройка личного VPN</strong>${escapeHtml(message)}`;
  b.classList.remove("hidden");
}

function hidePersonalSetupBanner() {
  el("#personal-setup-banner").classList.add("hidden");
}

function escapeHtml(s: string) {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function openModal(title: string, bodyHtml: string) {
  el("#modal-title").textContent = title;
  el("#modal-body").innerHTML = bodyHtml;
  const overlay = el("#modal-overlay");
  overlay.classList.add("is-open");
  overlay.setAttribute("aria-hidden", "false");
}

function closeModal() {
  const overlay = el("#modal-overlay");
  overlay.classList.remove("is-open");
  overlay.setAttribute("aria-hidden", "true");
}

function renderProbeModal(report: SiteProbe) {
  const best = report.bestPath ? pathLabels[report.bestPath as RoutePath] : null;
  const summary = report.unavailable
    ? `<p class="probe-summary err">Сайт недоступен ни по одному из проверенных путей.</p>`
    : `<p class="probe-summary">Рекомендуемый путь: <strong>${escapeHtml(best ?? "—")}</strong></p>`;

  const rows = report.results
    .map((r) => {
      const label = pathLabels[r.path];
      let rowClass = "fail";
      let statusText = "Недоступен";
      let meta = "";

      if (r.skipped) {
        rowClass = "skip";
        statusText = "Пропущено";
        meta = r.skipReason ?? "";
      } else if (r.available) {
        rowClass = "ok";
        statusText = "Доступен";
        const parts: string[] = [];
        if (r.latencyMs != null) parts.push(formatPing(r.latencyMs));
        if (r.statusCode) parts.push(`HTTP ${r.statusCode}`);
        meta = parts.join(" · ");
      } else {
        if (r.latencyMs != null) meta = formatPing(r.latencyMs);
        if (r.statusCode) meta += (meta ? " · " : "") + `HTTP ${r.statusCode}`;
        if (r.error) meta += (meta ? " · " : "") + r.error;
      }

      return `<li class="probe-row ${rowClass}">
        <span class="path-name">${escapeHtml(label)}</span>
        <span class="path-status">${escapeHtml(statusText)}</span>
        ${meta ? `<span class="path-meta">${escapeHtml(meta)}</span>` : ""}
      </li>`;
    })
    .join("");

  const ipLine = report.resolvedIp
    ? `<p class="muted" style="margin:0 0 0.75rem">DNS → ${escapeHtml(report.resolvedIp)} · ${escapeHtml(report.url)}</p>`
    : `<p class="muted" style="margin:0 0 0.75rem">${escapeHtml(report.url)}</p>`;

  openModal("Проверка сайта", `${ipLine}${summary}<ul class="probe-list">${rows}</ul>`);
}

function showProbeLoading(url: string) {
  openModal(
    "Проверка сайта",
    `<p class="muted" style="margin:0 0 0.5rem">${escapeHtml(url)}</p>
     <div class="probe-loading">
       <div class="spinner" aria-hidden="true"></div>
       <span>Проверяем direct, системный и личный VPN…</span>
     </div>`,
  );
}

async function getPersonalReadiness(): Promise<PersonalReadiness> {
  return api<PersonalReadiness>("/api/personal-vpn/readiness");
}

function goToPersonalSetup(message: string) {
  showPersonalSetupBanner(message);
  switchTab("subscriptions");
}

/** Включить — primary когда можно включить; Выключить — primary когда VPN уже включён */
function updateVpnToggleButtons(
  onBtn: HTMLButtonElement,
  offBtn: HTMLButtonElement,
  opts: { active: boolean; onClickable?: boolean; onHint?: string },
) {
  const { active, onClickable = true, onHint = "" } = opts;
  onBtn.title = onHint;
  offBtn.title = "";

  if (active) {
    onBtn.classList.add("secondary");
    onBtn.disabled = true;
    offBtn.classList.remove("secondary");
    offBtn.disabled = false;
    return;
  }

  offBtn.classList.add("secondary");
  offBtn.disabled = true;
  if (onClickable) {
    onBtn.classList.remove("secondary");
    onBtn.disabled = false;
  } else {
    onBtn.classList.add("secondary");
    onBtn.disabled = true;
  }
}

let systemVpnFastPoll: ReturnType<typeof setInterval> | null = null;

function setSystemVpnFastPoll(on: boolean) {
  if (systemVpnFastPoll) {
    clearInterval(systemVpnFastPoll);
    systemVpnFastPoll = null;
  }
  if (on) {
    systemVpnFastPoll = setInterval(() => void refreshStatus(), 2000);
  }
}

function updatePersonalVpnButtons(configured: boolean, running: boolean) {
  if (!configured) {
    updateVpnToggleButtons(el("#btn-personal-on"), el("#btn-personal-off"), {
      active: false,
      onClickable: true,
      onHint: "Сначала настройте подписку и выберите сервер",
    });
    return;
  }
  updateVpnToggleButtons(el("#btn-personal-on"), el("#btn-personal-off"), {
    active: running,
  });
}

async function refreshStatus() {
  const s = await api<StatusResponse>("/api/status");
  cachedStatus = s;
  const ready = s.personalVpn.configured ?? false;
  el("#card-internet .card-body").innerHTML = `
    ${badge(s.internet.up, "OK", "Нет")}
    <dl>
      <dt>Интерфейс</dt><dd>${s.internet.link.name} (${s.internet.link.state})</dd>
      <dt>Локальный IP</dt><dd>${s.internet.link.ipv4 ?? "—"}</dd>
      <dt>Публичный IP</dt><dd>${s.internet.publicIp ?? "—"}</dd>
      <dt>Задержка</dt><dd>${s.internet.latencyMs != null ? formatPing(s.internet.latencyMs) : "—"}</dd>
    </dl>
    ${s.internet.error ? `<p class="${s.internet.up ? "muted" : "err"}">${escapeHtml(s.internet.error)}</p>` : ""}
  `;
  const sysSt = systemVpnState(s.systemVpn);
  setSystemVpnFastPoll(sysSt === "connecting");
  const sys = s.systemVpn;
  el("#card-system-vpn .card-body").innerHTML = `
    ${systemVpnBadge(sys)}
    <dl>
      <dt>Профиль NM</dt><dd>${escapeHtml(sys.connectionId)}</dd>
      <dt>Тип</dt><dd>${sys.vpnType ? escapeHtml(sys.vpnType) : "—"}</dd>
      <dt>Состояние NM</dt><dd>${sys.nmState ? escapeHtml(sys.nmState) : "—"}</dd>
      <dt>Интерфейс</dt><dd>${sys.interface ?? "—"}</dd>
      <dt>VPN IP</dt><dd>${sysSt === "connected" ? (sys.ipv4 ?? "—") : "—"}</dd>
      <dt>Шлюз</dt><dd>${sysSt === "connected" ? (sys.gateway ?? "—") : "—"}</dd>
      <dt>Маршрутов</dt><dd>${sysSt === "connected" ? (sys.routeCount ?? 0) : "—"}</dd>
    </dl>
    ${sysSt === "connecting" ? `<p class="muted">Идёт подключение — подтвердите MFA в <strong>системном</strong> окне NetworkManager.</p>` : ""}
    ${sys.error ? `<p class="err">${escapeHtml(sys.error)}</p>` : ""}
  `;
  const pvErr = s.personalVpn.error;
  el("#card-router .card-body").innerHTML = `
    ${badge(Boolean(s.personalVpn.routingRunning), "Активен", "Выключен")}
    <dl>
      <dt>Интерфейс</dt><dd>tun100</dd>
      <dt>Режим</dt><dd>${escapeHtml(routerModeLabel(s.personalVpn.configMode))}</dd>
      <dt>Приоритет</dt><dd>${escapeHtml(routerPriorityLabel(s.personalVpn.configMode))}</dd>
      <dt>Наблюдение</dt><dd>${s.personalVpn.routingRunning ? "Chrome, Cursor, терминал" : "—"}</dd>
    </dl>
    <p class="muted">${escapeHtml(routerModeHint(s.personalVpn.configMode))}</p>
    ${!s.personalVpn.routingRunning && pvErr ? `<p class="err">${escapeHtml(pvErr)}</p>` : ""}
  `;
  const cfgBadge = badge(ready, "Настроен", "Не настроен");
  el("#card-personal .card-body").innerHTML = `
    ${badge(s.personalVpn.running, "Личный VPN разрешён", "Личный VPN выключен")}
    ${cfgBadge}
    <p>${escapeHtml(s.personalVpn.subscriptionName || "—")}</p>
    ${!ready && s.personalVpn.message ? `<p class="muted">${escapeHtml(s.personalVpn.message)}</p>` : ""}
    ${s.personalVpn.hostConfigured === false ? `<p class="err">Выполните в терминале: <code>make sync</code></p>` : ""}
    ${s.personalVpn.routingRunning && !s.personalVpn.running && pvErr ? `<p class="err">${escapeHtml(pvErr)}</p>` : ""}
    ${
      s.personalVpn.configMode === "coexist"
        ? `<p class="muted">${escapeHtml(routerModeHint("coexist"))}</p>`
        : ""
    }
    ${
      s.personalVpn.configStale
        ? `<p class="err">Конфиг sing-box устарел — <strong>выключите и снова включите</strong> личный VPN (сначала системный VPN в GNOME).</p>`
        : ""
    }
    ${
      s.personalVpn.running &&
      s.personalVpn.configFileMode === "full" &&
      s.personalVpn.systemVpnActive
        ? `<p class="muted">Без <code>make sync</code> (polkit v6) при включении могут всплывать окна «Authentication» — их можно закрыть; после sync не должны.</p>`
        : ""
    }
  `;
  updatePersonalVpnButtons(ready, s.personalVpn.running);
  if (ready) hidePersonalSetupBanner();
}

function formatRefreshLabel(sub: Subscription): string {
  if (!sub.autoRefresh) return "обновление вручную";
  const m = sub.refreshIntervalMinutes || 60;
  if (m < 60) return `авто каждые ${m} мин.`;
  if (m % 60 === 0) return `авто каждые ${m / 60} ч.`;
  return `авто каждые ${m} мин.`;
}

function syncAddRefreshInputs() {
  const auto = el<HTMLInputElement>("#add-auto-refresh");
  const mins = el<HTMLInputElement>("#add-refresh-minutes");
  const wrap = mins.closest(".interval-label");
  if (auto.checked) {
    wrap?.classList.remove("is-disabled");
    mins.disabled = false;
  } else {
    wrap?.classList.add("is-disabled");
    mins.disabled = true;
  }
}

let activeSubId = "";
let expandedSubId = "";
let cachedStatus: StatusResponse | null = null;

const NODE_SORT_STORAGE_KEY = "vpn-router-node-sort";
const nodesCache = new Map<string, Node[]>();
const pingsCache = new Map<string, PingResult[] | null>();
const subsByIdCache = new Map<string, Subscription>();

function getNodeSort(subId: string): NodeSortMode {
  try {
    const all = JSON.parse(localStorage.getItem(NODE_SORT_STORAGE_KEY) || "{}") as Record<
      string,
      NodeSortMode
    >;
    const mode = all[subId];
    if (mode === "name-asc" || mode === "name-desc" || mode === "ping" || mode === "frequent") {
      return mode;
    }
  } catch {
    /* ignore */
  }
  return "name-asc";
}

function setNodeSort(subId: string, mode: NodeSortMode) {
  try {
    const all = JSON.parse(localStorage.getItem(NODE_SORT_STORAGE_KEY) || "{}") as Record<
      string,
      NodeSortMode
    >;
    all[subId] = mode;
    localStorage.setItem(NODE_SORT_STORAGE_KEY, JSON.stringify(all));
  } catch {
    /* ignore */
  }
}

function nodeSortSelectHtml(subId: string): string {
  const sort = getNodeSort(subId);
  const options: { value: NodeSortMode; label: string }[] = [
    { value: "name-asc", label: "А → Я" },
    { value: "name-desc", label: "Я → А" },
    { value: "ping", label: "По пингу" },
    { value: "frequent", label: "Часто выбираемые" },
  ];
  const opts = options
    .map(
      (o) => `<option value="${o.value}"${sort === o.value ? " selected" : ""}>${o.label}</option>`,
    )
    .join("");
  return `<label class="node-sort-label">Сортировка<select data-node-sort="${subId}">${opts}</select></label>`;
}

function cacheSubNodes(
  subId: string,
  nodes: Node[],
  pings: PingResult[] | null,
  sub?: Subscription,
) {
  nodesCache.set(subId, nodes);
  pingsCache.set(subId, pings);
  if (sub) subsByIdCache.set(subId, sub);
}

function compareNodesByPing(a: Node, b: Node, pingMap: Map<string, PingResult>): number {
  const pa = pingMap.get(a.id);
  const pb = pingMap.get(b.id);
  if (!pa?.ok && !pb?.ok) return a.name.localeCompare(b.name, "ru");
  if (pa?.ok && !pb?.ok) return -1;
  if (!pa?.ok && pb?.ok) return 1;
  return (pa?.latencyMs ?? 9999) - (pb?.latencyMs ?? 9999);
}

function compareNodesByFrequent(a: Node, b: Node, selectCounts: Record<string, number>): number {
  const ca = selectCounts[a.id] ?? 0;
  const cb = selectCounts[b.id] ?? 0;
  if (cb !== ca) return cb - ca;
  return a.name.localeCompare(b.name, "ru");
}

function sortNodes(
  nodes: Node[],
  mode: NodeSortMode,
  pingMap: Map<string, PingResult>,
  selectCounts: Record<string, number>,
): Node[] {
  const sorted = [...nodes];
  switch (mode) {
    case "name-desc":
      sorted.sort((a, b) => b.name.localeCompare(a.name, "ru"));
      break;
    case "ping":
      sorted.sort((a, b) => compareNodesByPing(a, b, pingMap));
      break;
    case "frequent":
      sorted.sort((a, b) => compareNodesByFrequent(a, b, selectCounts));
      break;
    default:
      sorted.sort((a, b) => a.name.localeCompare(b.name, "ru"));
  }
  return sorted;
}

function orderNodes(
  nodes: Node[],
  selectedNodeId: string | undefined,
  sortMode: NodeSortMode,
  pingMap: Map<string, PingResult>,
  selectCounts: Record<string, number>,
): Node[] {
  if (!selectedNodeId) {
    return sortNodes(nodes, sortMode, pingMap, selectCounts);
  }
  const selected = nodes.find((n) => n.id === selectedNodeId);
  const rest = nodes.filter((n) => n.id !== selectedNodeId);
  const sortedRest = sortNodes(rest, sortMode, pingMap, selectCounts);
  return selected ? [selected, ...sortedRest] : sortedRest;
}

function resolveSelectedNodeId(subId: string, sub?: Subscription): string | undefined {
  if (sub?.selectedNodeId) return sub.selectedNodeId;
  if (cachedStatus?.personalVpn.subscriptionId === subId) {
    return cachedStatus.personalVpn.selectedNodeId;
  }
  return undefined;
}

function renderNodeRow(
  n: Node,
  selectedNodeId: string | undefined,
  pingMap: Map<string, PingResult>,
  selectCounts: Record<string, number>,
  sortMode: NodeSortMode,
): string {
  const p = pingMap.get(n.id);
  const ping = p?.ok ? formatPing(p.latencyMs) : p?.error ? "недоступен" : "";
  const isSelected = n.id === selectedNodeId;
  const sel = isSelected ? " is-selected" : "";
  const picks = selectCounts[n.id] ?? 0;
  const picksLabel = sortMode === "frequent" && picks > 0 ? ` · ${picks}×` : "";
  return `<li class="${sel}">
    <span>${escapeHtml(n.name)}</span>
    <small>${n.protocol} · ${n.host}:${n.port}${ping ? ` · ${ping}` : ""}${picksLabel}</small>
    <button type="button" data-select="${n.id}" class="${isSelected ? "secondary" : ""}">${isSelected ? "✓ Выбран" : "Выбрать"}</button>
  </li>`;
}

function renderNodesList(
  container: HTMLElement,
  subId: string,
  nodes: Node[],
  pings: PingResult[] | null,
  selectedNodeId?: string,
  sortMode: NodeSortMode = getNodeSort(subId),
  selectCounts: Record<string, number> = {},
) {
  const pingMap = new Map(pings?.map((p) => [p.nodeId, p]) ?? []);
  const ordered = orderNodes(nodes, selectedNodeId, sortMode, pingMap, selectCounts);
  const hasSelected = Boolean(selectedNodeId && ordered[0]?.id === selectedNodeId);
  const rows: string[] = [];
  if (hasSelected) {
    rows.push('<li class="nodes-section-label muted">Выбранный сервер</li>');
    rows.push(renderNodeRow(ordered[0], selectedNodeId, pingMap, selectCounts, sortMode));
    if (ordered.length > 1) {
      rows.push('<li class="nodes-section-label muted">Остальные серверы</li>');
      rows.push(
        ...ordered
          .slice(1)
          .map((n) => renderNodeRow(n, selectedNodeId, pingMap, selectCounts, sortMode)),
      );
    }
  } else {
    rows.push(
      ...ordered.map((n) => renderNodeRow(n, selectedNodeId, pingMap, selectCounts, sortMode)),
    );
  }
  container.innerHTML = rows.join("");
  container.scrollTop = 0;
  container.querySelectorAll("[data-select]").forEach((btn) => {
    btn.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      await api(`/api/subscriptions/${subId}/select`, {
        method: "POST",
        body: JSON.stringify({ nodeId: (btn as HTMLButtonElement).dataset.select }),
      });
      showToast("Сервер выбран");
      hidePersonalSetupBanner();
      await refreshStatus();
      await loadSubscriptions();
      if (expandedSubId === subId) await expandSubscription(subId, true);
    });
  });
}

function rerenderNodesList(subId: string) {
  const list = document.querySelector(`[data-list="${subId}"]`) as HTMLElement | null;
  const nodes = nodesCache.get(subId);
  if (!list || !nodes?.length) return;
  const sub = subsByIdCache.get(subId);
  renderNodesList(
    list,
    subId,
    nodes,
    pingsCache.get(subId) ?? null,
    resolveSelectedNodeId(subId, sub),
    getNodeSort(subId),
    sub?.nodeSelectCounts ?? {},
  );
}

async function expandSubscription(id: string, forceOpen = false) {
  const card = document.querySelector(`.sub-card[data-id="${id}"]`);
  if (!card) return;

  const servers = card.querySelector(".sub-servers") as HTMLElement;
  const list = card.querySelector(".nodes-list") as HTMLElement;
  const wasExpanded = card.classList.contains("is-expanded");

  if (wasExpanded && !forceOpen) {
    card.classList.remove("is-expanded");
    servers.classList.add("hidden");
    expandedSubId = "";
    return;
  }

  activeSubId = id;
  expandedSubId = id;
  card.classList.add("is-expanded");
  servers.classList.remove("hidden");
  list.innerHTML = `<li class="muted">Загрузка серверов…</li>`;

  try {
    const subs = await api<Subscription[]>("/api/subscriptions");
    const sub = subs.find((s) => s.id === id);
    const nodes = await api<Node[]>(`/api/subscriptions/${id}/nodes`);
    list.innerHTML = "";
    if (!nodes.length) {
      list.innerHTML = `<li class="muted">Серверов нет — нажмите «Обновить подписку»</li>`;
      return;
    }
    renderNodesList(
      list,
      id,
      nodes,
      null,
      resolveSelectedNodeId(id, sub),
      getNodeSort(id),
      sub?.nodeSelectCounts ?? {},
    );
    cacheSubNodes(id, nodes, null, sub);
    const meta = card.querySelector(`[data-node-count="${id}"]`);
    if (meta) meta.textContent = ` · ${nodes.length} серверов`;
    showToast(`Загружено серверов: ${nodes.length}`);
  } catch (e) {
    list.innerHTML = `<li class="err">${escapeHtml(e instanceof Error ? e.message : "Ошибка загрузки")}</li>`;
    showToast(e instanceof Error ? e.message : "Ошибка", true);
  }
}

async function deleteSubscription(id: string, name: string) {
  if (!confirm(`Удалить подписку «${name}»?`)) return;
  await api(`/api/subscriptions/delete`, {
    method: "POST",
    body: JSON.stringify({ id }),
  });
  if (expandedSubId === id) expandedSubId = "";
  if (activeSubId === id) activeSubId = "";
  showToast("Подписка удалена");
  await loadSubscriptions();
  await refreshStatus();
}

async function saveSubSettings(id: string, auto: boolean, minutes: number, silent = false) {
  const mins = Math.min(10080, Math.max(5, minutes || 60));
  await api<Subscription>(`/api/subscriptions/${id}`, {
    method: "PUT",
    body: JSON.stringify({
      autoRefresh: auto,
      refreshIntervalMinutes: auto ? mins : 0,
    }),
  });
  if (!silent) {
    showToast("Настройки подписки сохранены");
  }
  const meta = document.querySelector(`[data-meta="${id}"]`);
  if (meta) {
    const selectedHint = meta.textContent?.includes("сервер выбран") ? " · сервер выбран" : "";
    meta.textContent = `${formatRefreshLabel({
      autoRefresh: auto,
      refreshIntervalMinutes: auto ? mins : 0,
    } as Subscription)}${selectedHint}`;
  }
  const minsInput = document.querySelector(`[data-mins="${id}"]`) as HTMLInputElement | null;
  if (minsInput && auto) {
    minsInput.value = String(mins);
  }
}

const subSettingsSaveTimers: Record<string, number> = {};

function scheduleSaveSubSettings(id: string) {
  window.clearTimeout(subSettingsSaveTimers[id]);
  subSettingsSaveTimers[id] = window.setTimeout(() => {
    void persistSubSettings(id);
  }, 400);
}

async function persistSubSettings(id: string) {
  const autoEl = document.querySelector(`[data-auto="${id}"]`) as HTMLInputElement | null;
  const minsEl = document.querySelector(`[data-mins="${id}"]`) as HTMLInputElement | null;
  if (!autoEl || !minsEl) return;
  const auto = autoEl.checked;
  const mins = parseInt(minsEl.value, 10) || 60;
  try {
    await saveSubSettings(id, auto, mins, true);
  } catch (e) {
    showToast(e instanceof Error ? e.message : "Не удалось сохранить настройки", true);
  }
}

async function loadSubscriptions() {
  if (!cachedStatus) {
    try {
      cachedStatus = await api<StatusResponse>("/api/status");
    } catch {
      /* ignore */
    }
  }
  const subs = await api<Subscription[]>("/api/subscriptions");
  const box = el<HTMLDivElement>("#subs-list");
  if (!subs.length) {
    box.innerHTML = '<p class="muted">Подписок нет. Добавьте URL.</p>';
    return;
  }

  const prevExpanded = expandedSubId;
  const activeId = cachedStatus?.personalVpn.subscriptionId || "";
  activeSubId = activeId;
  box.innerHTML = subs
    .map((s) => {
      const expanded = s.id === prevExpanded;
      const isActive = Boolean(activeId) && s.id === activeId;
      return `
      <article class="sub-card${expanded ? " is-expanded" : ""}${isActive ? " is-active" : ""}" data-id="${s.id}">
        <div class="sub-header">
          <div class="sub-header-main">
            <div class="sub-title-row">
              <strong>${escapeHtml(s.name)}</strong>
              ${isActive ? `<span class="badge badge-ok">Выбрана</span>` : ""}
            </div>
            <span class="muted" data-meta="${s.id}">${formatRefreshLabel(s)}${s.selectedNodeId ? " · сервер выбран" : ""}</span>
            <span class="muted" data-node-count="${s.id}"></span>
          </div>
          <div class="sub-header-actions">
            <button type="button" class="secondary small" data-toggle="${s.id}">${expanded ? "Свернуть" : "Показать серверы"}</button>
            <button type="button" class="secondary small btn-danger" data-delete="${s.id}">Удалить</button>
          </div>
        </div>
        <div class="sub-settings" data-id="${s.id}">
          <label class="check-label">
            <input type="checkbox" data-auto="${s.id}" ${s.autoRefresh ? "checked" : ""} />
            Авто
          </label>
          <label class="interval-label">
            каждые
            <input type="number" data-mins="${s.id}" min="5" max="10080" value="${s.refreshIntervalMinutes || 60}" ${s.autoRefresh ? "" : "disabled"} />
            мин.
          </label>
        </div>
        <div class="sub-servers${expanded ? "" : " hidden"}">
          <div class="sub-servers-toolbar">
            <button type="button" class="secondary" data-refresh="${s.id}">Обновить подписку</button>
            <button type="button" data-ping="${s.id}">Проверить пинг</button>
            ${nodeSortSelectHtml(s.id)}
          </div>
          <ul class="nodes-list" data-list="${s.id}"></ul>
        </div>
      </article>`;
    })
    .join("");

  box.querySelectorAll("[data-toggle]").forEach((node) => {
    node.addEventListener("click", (ev) => {
      ev.stopPropagation();
      expandSubscription((node as HTMLElement).dataset.toggle!);
    });
  });

  box.querySelectorAll("[data-delete]").forEach((btn) => {
    btn.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      const id = (btn as HTMLButtonElement).dataset.delete!;
      const name =
        btn.closest(".sub-card")?.querySelector("strong")?.textContent?.trim() || "подписка";
      try {
        await deleteSubscription(id, name);
      } catch (e) {
        showToast(e instanceof Error ? e.message : "Не удалось удалить", true);
      }
    });
  });

  box.querySelectorAll("[data-auto]").forEach((inp) => {
    inp.addEventListener("change", () => {
      const id = (inp as HTMLInputElement).dataset.auto!;
      const mins = box.querySelector(`[data-mins="${id}"]`) as HTMLInputElement;
      const label = mins.closest(".interval-label");
      if ((inp as HTMLInputElement).checked) {
        mins.disabled = false;
        label?.classList.remove("is-disabled");
      } else {
        mins.disabled = true;
        label?.classList.add("is-disabled");
      }
      void persistSubSettings(id);
    });
  });

  box.querySelectorAll("[data-mins]").forEach((inp) => {
    inp.addEventListener("change", () => {
      const id = (inp as HTMLInputElement).dataset.mins!;
      void persistSubSettings(id);
    });
    inp.addEventListener("input", () => {
      const id = (inp as HTMLInputElement).dataset.mins!;
      scheduleSaveSubSettings(id);
    });
  });

  box.querySelectorAll("[data-node-sort]").forEach((sel) => {
    sel.addEventListener("change", (ev) => {
      ev.stopPropagation();
      const id = (sel as HTMLSelectElement).dataset.nodeSort!;
      const mode = (sel as HTMLSelectElement).value as NodeSortMode;
      setNodeSort(id, mode);
      rerenderNodesList(id);
    });
  });

  box.querySelectorAll("[data-refresh]").forEach((btn) => {
    btn.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      const id = (btn as HTMLButtonElement).dataset.refresh!;
      (btn as HTMLButtonElement).disabled = true;
      try {
        await api(`/api/subscriptions/${id}/refresh`, { method: "POST" });
        showToast("Подписка обновлена");
        await expandSubscription(id, true);
      } catch (e) {
        showToast(e instanceof Error ? e.message : "Ошибка", true);
      } finally {
        (btn as HTMLButtonElement).disabled = false;
      }
    });
  });

  box.querySelectorAll("[data-ping]").forEach((btn) => {
    btn.addEventListener("click", async (ev) => {
      ev.stopPropagation();
      const id = (btn as HTMLButtonElement).dataset.ping!;
      const list = box.querySelector(`[data-list="${id}"]`) as HTMLElement;
      (btn as HTMLButtonElement).disabled = true;
      try {
        const [pings, nodes, subs] = await Promise.all([
          api<PingResult[]>(`/api/subscriptions/${id}/ping`, { method: "POST" }),
          api<Node[]>(`/api/subscriptions/${id}/nodes`),
          api<Subscription[]>("/api/subscriptions"),
        ]);
        const sub = subs.find((s) => s.id === id);
        setNodeSort(id, "ping");
        const sortSelect = box.querySelector(
          `[data-node-sort="${id}"]`,
        ) as HTMLSelectElement | null;
        if (sortSelect) sortSelect.value = "ping";
        cacheSubNodes(id, nodes, pings, sub);
        renderNodesList(
          list,
          id,
          nodes,
          pings,
          resolveSelectedNodeId(id, sub),
          getNodeSort(id),
          sub?.nodeSelectCounts ?? {},
        );
        if (getNodeSort(id) === "ping") {
          showToast("Список отсортирован по пингу");
        }
      } catch (e) {
        showToast(e instanceof Error ? e.message : "Ошибка пинга", true);
      } finally {
        (btn as HTMLButtonElement).disabled = false;
      }
    });
  });

  if (prevExpanded) await expandSubscription(prevExpanded, true);
}

function rulePathSelect(rule: DomainRule): string {
  const options = (Object.keys(pathLabels) as RoutePath[])
    .map(
      (p) => `<option value="${p}"${rule.path === p ? " selected" : ""}>${pathLabels[p]}</option>`,
    )
    .join("");
  return `<select data-rule-path="${rule.id}">${options}</select>`;
}

function formatRuleTime(value?: string): string {
  if (!value) return "—";
  return new Date(value).toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
}

function ruleMeta(rule: DomainRule): string {
  const checks = rule.checkCount ? `проверок: ${rule.checkCount}` : "ещё не проверялось";
  const last = rule.lastCheckedAt ? ` · последняя: ${formatRuleTime(rule.lastCheckedAt)}` : "";
  const ok = rule.lastSuccessAt ? ` · успех: ${formatRuleTime(rule.lastSuccessAt)}` : "";
  const err = rule.lastError ? ` · ${escapeHtml(rule.lastError)}` : "";
  return `${checks}${last}${ok}${err}`;
}

function renderRulesList(container: HTMLElement, rules: DomainRule[], emptyText: string) {
  if (!rules.length) {
    container.innerHTML = `<li class="muted">${emptyText}</li>`;
    return;
  }
  container.innerHTML = rules
    .map((r) => {
      const source = (r.source || "manual") === "auto" ? "Авто" : "Вручную";
      const disabled = r.enabled ? "" : " is-disabled";
      return `<li class="rule-row${disabled}">
        <div class="rule-main">
          <strong>${escapeHtml(r.pattern)}</strong>
          <small>${source} · ${ruleMeta(r)}</small>
        </div>
        ${rulePathSelect(r)}
        <label class="check-label small"><input type="checkbox" data-rule-enabled="${r.id}" ${r.enabled ? "checked" : ""} /> Вкл</label>
        <button data-del="${r.id}" class="secondary small">×</button>
      </li>`;
    })
    .join("");
}

function bindRuleControls(root: HTMLElement) {
  root.querySelectorAll("[data-del]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      await api(`/api/rules/${(btn as HTMLButtonElement).dataset.del}`, { method: "DELETE" });
      await loadRules();
      showToast("Правило удалено");
    });
  });
  root.querySelectorAll("[data-rule-path]").forEach((sel) => {
    sel.addEventListener("change", async () => {
      await api(`/api/rules/${(sel as HTMLSelectElement).dataset.rulePath}`, {
        method: "PUT",
        body: JSON.stringify({ path: (sel as HTMLSelectElement).value }),
      });
      await loadRules();
      showToast("Правило обновлено");
    });
  });
  root.querySelectorAll("[data-rule-enabled]").forEach((inp) => {
    inp.addEventListener("change", async () => {
      await api(`/api/rules/${(inp as HTMLInputElement).dataset.ruleEnabled}`, {
        method: "PUT",
        body: JSON.stringify({ enabled: (inp as HTMLInputElement).checked }),
      });
      await loadRules();
      showToast("Правило обновлено");
    });
  });
}

let rulesLoadInFlight = false;
let rulesSignature = "";
let cachedRulesData: RulesResponse | null = null;

function rulesDataSignature(data: RulesResponse): string {
  return JSON.stringify({
    domains: data.domains ?? [],
    apps: data.apps ?? [],
  });
}

function rulesTabActive(): boolean {
  return document.querySelector("#tab-rules")?.classList.contains("active") ?? false;
}

function currentRulesSearch(): string {
  return el<HTMLInputElement>("#rules-search").value.trim().toLowerCase();
}

function filterRules(rules: DomainRule[], query: string): DomainRule[] {
  if (!query) return rules;
  return rules.filter((r) => r.pattern.toLowerCase().includes(query));
}

function renderRules(data: RulesResponse) {
  const domains = data.domains ?? [];
  const query = currentRulesSearch();
  const filteredDomains = filterRules(domains, query);
  const autoRules = filteredDomains.filter((r) => (r.source || "manual") === "auto");
  const manualRules = filteredDomains.filter((r) => (r.source || "manual") !== "auto");
  const autoList = el<HTMLElement>("#auto-rules-list");
  const manualList = el<HTMLElement>("#manual-rules-list");
  const suffix = query ? ` по запросу «${escapeHtml(query)}»` : "";

  renderRulesList(autoList, autoRules, `Автоматических правил${suffix} нет.`);
  renderRulesList(manualList, manualRules, `Ручных правил${suffix} нет.`);
  bindRuleControls(autoList);
  bindRuleControls(manualList);

  const status = el<HTMLElement>("#rules-search-status");
  status.textContent = query
    ? `Найдено: ${filteredDomains.length} из ${domains.length}`
    : `Всего правил: ${domains.length}`;
}

async function loadRules(force = false) {
  if (rulesLoadInFlight) return;
  rulesLoadInFlight = true;
  try {
    const data = await api<RulesResponse>("/api/rules");
    const sig = rulesDataSignature(data);
    if (!force && sig === rulesSignature) return;
    rulesSignature = sig;
    cachedRulesData = data;
    renderRules(data);
  } finally {
    rulesLoadInFlight = false;
  }
}

function setupTabs() {
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      const name = (tab as HTMLButtonElement).dataset.tab!;
      switchTab(name);
      if (name === "rules") {
        void loadRules();
      }
    });
  });
}

async function pingApi(): Promise<boolean> {
  try {
    await api("/api/health");
    el("#api-status").className = "badge badge-ok";
    el("#api-status").textContent = "API online";
    return true;
  } catch {
    el("#api-status").className = "badge badge-err";
    el("#api-status").textContent = "API offline";
    return false;
  }
}

async function checkApiVersion() {
  try {
    const ver = await api<{ apiVersion?: number }>("/api/version");
    if ((ver.apiVersion ?? 0) < 4) {
      showToast("Демон устарел — закройте приложение и запустите: make stop && make dev", true);
    }
  } catch {
    showToast("API демона недоступен — make dev", true);
  }
}

window.addEventListener("DOMContentLoaded", () => {
  setupTabs();

  el("#modal-close").addEventListener("click", closeModal);
  el("#modal-overlay").addEventListener("click", (e) => {
    if (e.target === el("#modal-overlay")) closeModal();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeModal();
  });

  el("#btn-personal-on").addEventListener("click", async () => {
    const ready = await getPersonalReadiness();
    if (!ready.configured) {
      if (ready.reason === "host_not_ready") {
        showToast(ready.message ?? "Выполните: make sync", true);
        return;
      }
      goToPersonalSetup(ready.message ?? "Добавьте подписку и выберите сервер");
      return;
    }
    try {
      const res = await api<{ running: boolean }>("/api/personal-vpn/connect", {
        method: "POST",
      });
      await refreshStatus();
      if (res.running) {
        showToast("Личный VPN включён");
      } else {
        showToast("Не удалось подтвердить запуск VPN", true);
      }
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        goToPersonalSetup(e.message);
        return;
      }
      if (e instanceof ApiError && e.status === 412) {
        showToast(e.message || "Выполните: make sync", true);
        await refreshStatus();
        return;
      }
      showToast(e instanceof Error ? e.message : "Ошибка", true);
    }
  });

  el("#btn-personal-off").addEventListener("click", async () => {
    try {
      const res = await api<{ status: string }>("/api/personal-vpn/disconnect", {
        method: "POST",
      });
      await refreshStatus();
      showToast(
        res.status === "personal_disabled_deferred"
          ? "Личный VPN выключен в приложении. Чтобы не сбрасывать системный VPN, сетевой конфиг применится позже."
          : "Личный VPN выключен",
      );
    } catch (e) {
      showToast(e instanceof Error ? e.message : "Ошибка", true);
    }
  });

  el("#add-auto-refresh").addEventListener("change", syncAddRefreshInputs);
  syncAddRefreshInputs();

  el("#form-add-sub").addEventListener("submit", async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    const btn = (e.target as HTMLFormElement).querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;
    const auto = fd.get("autoRefresh") === "on";
    const mins = parseInt(String(fd.get("refreshMinutes") || "60"), 10) || 60;
    (btn as HTMLButtonElement).disabled = true;
    try {
      const sub = await api<Subscription>("/api/subscriptions", {
        method: "POST",
        body: JSON.stringify({
          name: fd.get("name"),
          url: fd.get("url"),
          autoRefresh: auto,
          refreshIntervalMinutes: auto ? mins : 0,
        }),
      });
      (e.target as HTMLFormElement).reset();
      el<HTMLInputElement>("#add-auto-refresh").checked = true;
      el<HTMLInputElement>("#add-refresh-minutes").value = "60";
      syncAddRefreshInputs();
      expandedSubId = sub.id;
      await loadSubscriptions();
      await expandSubscription(sub.id, true);
      showToast(`Загружено серверов — выберите узел в списке`);
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Ошибка", true);
    } finally {
      (btn as HTMLButtonElement).disabled = false;
    }
  });

  el("#form-add-rule").addEventListener("submit", async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target as HTMLFormElement);
    try {
      await api("/api/rules", {
        method: "POST",
        body: JSON.stringify({ pattern: fd.get("pattern"), path: fd.get("path") }),
      });
      loadRules();
      showToast("Правило добавлено");
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Ошибка", true);
    }
  });

  el("#form-auto-rule").addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const fd = new FormData(form);
    const btn = el<HTMLButtonElement>("#btn-auto-rule");
    const url = String(fd.get("url") || "").trim();
    if (!url) return;
    (btn as HTMLButtonElement).disabled = true;
    try {
      const res = await api<AutoCheckResponse>("/api/rules/auto-check", {
        method: "POST",
        body: JSON.stringify({ url }),
      });
      await loadRules();
      const path = res.rule?.path || res.suggestedPath;
      const applied = res.reapplied ? " Конфиг VPN переприменён." : "";
      showToast(
        res.message || `Для ${res.host} выбран путь: ${path ? pathLabels[path] : "—"}.${applied}`,
        Boolean(res.message && !path),
      );
    } catch (err) {
      showToast(err instanceof Error ? err.message : "Ошибка авто-проверки", true);
    } finally {
      (btn as HTMLButtonElement).disabled = false;
    }
  });

  el<HTMLInputElement>("#rules-search").addEventListener("input", () => {
    if (cachedRulesData) {
      renderRules(cachedRulesData);
    }
  });

  el("#btn-scan-apps").addEventListener("click", async () => {
    const btn = el<HTMLButtonElement>("#btn-scan-apps");
    (btn as HTMLButtonElement).disabled = true;
    try {
      const apps =
        await api<{ processName: string; connections: number; execPath?: string }[]>("/api/apps");
      el("#apps-list").innerHTML = apps
        .slice(0, 40)
        .map(
          (a) =>
            `<li>${escapeHtml(a.processName)} (${a.connections}) <button data-app="${escapeHtml(a.processName)}" class="small">→ личный</button></li>`,
        )
        .join("");
      el("#apps-list")
        .querySelectorAll("[data-app]")
        .forEach((b) => {
          b.addEventListener("click", async () => {
            await api("/api/rules", {
              method: "POST",
              body: JSON.stringify({
                processName: (b as HTMLButtonElement).dataset.app,
                path: "personal",
              }),
            });
            loadRules();
          });
        });
    } catch (e) {
      showToast(e instanceof Error ? e.message : "Ошибка", true);
    } finally {
      (btn as HTMLButtonElement).disabled = false;
    }
  });

  el("#form-probe").addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = e.target as HTMLFormElement;
    const url = (new FormData(form).get("url") as string).trim();
    if (!url) return;

    const submitBtn = el<HTMLButtonElement>("#btn-probe-submit");
    submitBtn.disabled = true;
    showProbeLoading(url);

    try {
      const report = await api<SiteProbe>("/api/probe/site", {
        method: "POST",
        body: JSON.stringify({ url }),
      });
      renderProbeModal(report);
    } catch (err) {
      closeModal();
      showToast(err instanceof Error ? err.message : "Проверка не удалась", true);
    } finally {
      submitBtn.disabled = false;
    }
  });

  void checkApiVersion();

  const tick = async () => {
    if (await pingApi()) {
      await refreshStatus();
    }
  };
  tick();
  setInterval(tick, 5000);
  setInterval(() => {
    if (rulesTabActive()) {
      void loadRules();
    }
  }, 2000);
  loadSubscriptions();
  loadRules();
});
