import type { DomainRule, RulesResponse, Subscription } from "./api";

export function filterRules(rules: DomainRule[], query: string): DomainRule[] {
  if (!query) return rules;
  const q = query.toLowerCase();
  return rules.filter((r) => r.pattern.toLowerCase().includes(q));
}

export function partitionDomainRules(rules: DomainRule[]): {
  auto: DomainRule[];
  manual: DomainRule[];
} {
  const auto: DomainRule[] = [];
  const manual: DomainRule[] = [];
  for (const r of rules) {
    if ((r.source || "manual") === "auto") auto.push(r);
    else manual.push(r);
  }
  return { auto, manual };
}

export function rulesDataSignature(data: RulesResponse): string {
  return JSON.stringify({
    domains: data.domains ?? [],
    apps: data.apps ?? [],
  });
}

export function formatRefreshLabel(sub: Subscription): string {
  if (!sub.autoRefresh) return "обновление вручную";
  const m = sub.refreshIntervalMinutes || 60;
  if (m < 60) return `авто каждые ${m} мин.`;
  if (m % 60 === 0) return `авто каждые ${m / 60} ч.`;
  return `авто каждые ${m} мин.`;
}

/** Строка квоты/срока из Subscription-Userinfo (байты, expire ISO/RFC3339). */
export function formatSubscriptionQuota(sub: Subscription): string {
  const parts: string[] = [];
  const total = Number(sub.trafficTotal) || 0;
  const used = (Number(sub.trafficUpload) || 0) + (Number(sub.trafficDownload) || 0);
  if (total > 0) {
    const left = Math.max(0, total - used);
    parts.push(`осталось ${formatBytesGiB(left)}`);
  } else if (used > 0) {
    parts.push(`использовано ${formatBytesGiB(used)}`);
  }
  const exp = formatExpireDate(sub.expireAt);
  if (exp) parts.push(`до ${exp}`);
  return parts.join(" · ");
}

function formatBytesGiB(bytes: number): string {
  const gib = bytes / (1024 * 1024 * 1024);
  if (gib >= 10) return `${Math.round(gib)} GiB`;
  if (gib >= 1) return `${gib.toFixed(1)} GiB`;
  const mib = bytes / (1024 * 1024);
  if (mib >= 1) return `${Math.round(mib)} MiB`;
  return `${bytes} B`;
}

function formatExpireDate(raw?: string): string {
  if (!raw) return "";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime()) || d.getTime() <= 0) return "";
  const y = d.getUTCFullYear();
  const m = String(d.getUTCMonth() + 1).padStart(2, "0");
  const day = String(d.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

/** Profile-Title / Announce могут приходить как `base64:…` (Clash). */
export function decodeHeaderText(raw?: string): string {
  const trimmed = (raw || "").trim();
  if (!trimmed) return "";
  const m = /^base64:(.*)$/i.exec(trimmed);
  if (!m) return trimmed;
  try {
    let b64 = m[1].trim().replace(/-/g, "+").replace(/_/g, "/");
    while (b64.length % 4) b64 += "=";
    const bin = atob(b64);
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
    return new TextDecoder("utf-8", { fatal: false }).decode(bytes).trim();
  } catch {
    return trimmed;
  }
}

export function formatAnnounce(raw?: string): string {
  return decodeHeaderText(raw);
}

/** Дата/время для карточки подписки (локальная TZ). */
export function formatSubscriptionDateTime(raw?: string): string {
  if (!raw) return "";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return "";
  return d.toLocaleString("ru-RU", { dateStyle: "short", timeStyle: "short" });
}

/**
 * Строка «добавлена … · обновлена …».
 * lastRefresh всегда как «обновлена», если есть; createdAt — только если задан.
 */
export function formatSubscriptionDates(sub: Subscription): string {
  const created = formatSubscriptionDateTime(sub.createdAt);
  const updated = formatSubscriptionDateTime(sub.lastRefresh);
  const parts: string[] = [];
  if (created) parts.push(`добавлена ${created}`);
  if (updated && updated !== created) {
    parts.push(`обновлена ${updated}`);
  } else if (updated && !created) {
    parts.push(`обновлена ${updated}`);
  }
  return parts.join(" · ");
}

/** 1 сервер / 2 сервера / 5 серверов */
export function formatServerCount(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "";
  const abs = Math.abs(Math.trunc(n)) % 100;
  const last = abs % 10;
  if (abs > 10 && abs < 20) return `${n} серверов`;
  if (last === 1) return `${n} сервер`;
  if (last >= 2 && last <= 4) return `${n} сервера`;
  return `${n} серверов`;
}

/** Человекочитаемый тип подписки (не путать с именем и announce). */
export function formatSubscriptionType(source?: string): string {
  switch ((source || "").toLowerCase()) {
    case "url":
      return "URL-подписка";
    case "uri":
      return "Share-ссылка";
    case "ovpn":
      return "OpenVPN (.ovpn)";
    case "text":
      return "Текст / список узлов";
    case "file":
      return "Файл";
    default:
      return "Неизвестно";
  }
}

/** Верхняя строка карточки: авто · сервер выбран · N серверов */
export function formatSubscriptionStatusLine(sub: Subscription, remote: boolean): string {
  const parts: string[] = [];
  parts.push(remote ? formatRefreshLabel(sub) : "локальный импорт");
  if (sub.selectedNodeId) parts.push("сервер выбран");
  const count = formatServerCount(Number(sub.nodeCount) || 0);
  if (count) parts.push(count);
  return parts.join(" · ");
}
