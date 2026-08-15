/** Качественная оценка пинга до хоста (мс). При активном VPN — ICMP, иначе TCP connect. */
export type PingLevel = "low" | "medium" | "high" | "very_high";

const LABELS: Record<PingLevel, string> = {
  low: "низкий",
  medium: "средний",
  high: "высокий",
  very_high: "очень высокий",
};

/** Пороги можно подстроить под типичный пинг до VPN-серверов */
export function pingLevel(ms: number): PingLevel {
  if (ms < 80) return "low";
  if (ms < 150) return "medium";
  if (ms < 300) return "high";
  return "very_high";
}

export function pingLevelLabel(ms: number): string {
  return LABELS[pingLevel(ms)];
}

/** «42 ms (низкий)» */
export function formatPing(ms: number | null | undefined): string {
  if (ms == null || Number.isNaN(ms)) return "—";
  return `${Math.round(ms)} ms (${pingLevelLabel(ms)})`;
}
