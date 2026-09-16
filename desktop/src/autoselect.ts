import type { AutoSelectJobStatus, DomainRule } from "./api";
import { escapeHtml } from "./ui-labels";

export const AUTOSELECT_SITES_STORAGE_KEY = "vpn-router-autoselect-sites";
export const DEFAULT_AUTOSELECT_SITES =
  "https://www.google.com/generate_204\nhttps://cursor.com\nhttps://api2.cursor.sh";
const MAX_SITES = 5;

/** Строки textarea → список сайтов: trim, https-префикс, дедуп, лимит. */
export function parseSitesInput(text: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const raw of text.split("\n")) {
    let s = raw.trim();
    if (!s) continue;
    if (!s.startsWith("http://") && !s.startsWith("https://")) {
      s = `https://${s}`;
    }
    if (seen.has(s)) continue;
    seen.add(s);
    out.push(s);
    if (out.length >= MAX_SITES) break;
  }
  return out;
}

/**
 * Домены из правил с путём «личный VPN» → https://host для автовыбора.
 * Только простые hostname без wildcard.
 */
export function sitesFromPersonalRules(rules: DomainRule[]): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const r of rules) {
    if (r.enabled === false || r.path !== "personal") continue;
    let host = (r.pattern || "").trim().toLowerCase();
    host = host.replace(/^\*\./, "");
    if (!host || host.includes("*") || host.includes("/") || host.includes(" ")) continue;
    if (!/^[a-z0-9.-]+$/.test(host)) continue;
    const url = `https://${host}`;
    if (seen.has(url)) continue;
    seen.add(url);
    out.push(url);
    if (out.length >= MAX_SITES) break;
  }
  return out;
}

/** Текст для textarea: сохранённый список, иначе правила personal, иначе дефолт. */
export function initialAutoSelectSitesText(rules: DomainRule[] = []): string {
  try {
    const saved = localStorage.getItem(AUTOSELECT_SITES_STORAGE_KEY);
    if (saved && saved.trim()) return saved;
  } catch {
    /* ignore */
  }
  const fromRules = sitesFromPersonalRules(rules);
  if (fromRules.length) return fromRules.join("\n");
  return DEFAULT_AUTOSELECT_SITES;
}

export function loadAutoSelectSites(): string {
  return initialAutoSelectSitesText([]);
}

export function saveAutoSelectSites(text: string): void {
  try {
    localStorage.setItem(AUTOSELECT_SITES_STORAGE_KEY, text);
  } catch {
    /* ignore */
  }
}

/** Строка прогресса: «Проверено 3 / 40 · сейчас: NL-1». */
export function renderAutoSelectProgress(st: AutoSelectJobStatus): string {
  const total = st.total || 0;
  const checked = st.checked || 0;
  let phase = "Подбор…";
  if (st.phase === "pinging") phase = "Пинг серверов…";
  else if (st.phase === "probing") phase = "Проверка сайтов…";
  else if (st.phase === "done") phase = "Готово";
  else if (st.phase === "error") phase = "Ошибка";
  const current = st.currentName ? ` · сейчас: ${escapeHtml(st.currentName)}` : "";
  return `<div class="probe-loading">
    <div class="spinner" aria-hidden="true"></div>
    <span>${phase} Проверено <strong>${checked}</strong> / <strong>${total}</strong>${current}</span>
  </div>`;
}

/** HTML-сводка результата автовыбора для модалки. */
export function renderAutoSelectResult(res: AutoSelectJobStatus): string {
  const rows = (res.attempts ?? [])
    .map((a) => {
      const status = a.ok ? "✓" : "✗";
      const ping = a.latencyMs ? ` · ${Math.round(a.latencyMs)} мс` : "";
      const weight =
        a.weight != null && a.weight !== 0 ? ` · вес ${a.weight > 0 ? "+" : ""}${a.weight}` : "";
      const err = a.error ? ` · ${escapeHtml(a.error)}` : "";
      return `<li class="probe-row ${a.ok ? "ok" : "fail"}">
        <span>${status} ${escapeHtml(a.name || a.nodeId)}</span>
        <span class="path-meta">${ping}${weight}${err}</span>
      </li>`;
    })
    .join("");

  const checkedLine = `<p class="muted">Проверено: ${res.checked ?? res.attempts?.length ?? 0} / ${res.total || "—"}</p>`;

  if (res.error && !res.selectedNodeId) {
    return `<p class="probe-summary err">${escapeHtml(res.error)}</p>${checkedLine}<ul class="probe-list">${rows}</ul>`;
  }
  if (res.selectedNodeId) {
    const reconnected = res.reconnected ? " Личный VPN переподключён." : "";
    return `<p class="probe-summary">Выбран сервер: <strong>${escapeHtml(
      res.selectedNodeName || res.selectedNodeId,
    )}</strong>.${reconnected}</p>
      ${checkedLine}
      <ul class="probe-list">${rows}</ul>`;
  }
  return `<p class="probe-summary err">${escapeHtml(
    res.message || res.error || "Подходящий сервер не найден",
  )}</p>${checkedLine}<ul class="probe-list">${rows}</ul>`;
}
