/** Подписи режима маршрутизатора для UI. */
export function routerModeLabel(mode?: string): string {
  if (mode === "coexist") {
    return "Совместно с системным VPN";
  }
  return "Обычный";
}

export function routerModeHint(mode?: string): string {
  if (mode === "coexist") {
    return "Системный VPN активен: рабочие сайты идут через него, остальное — по правилам или напрямую.";
  }
  return "Системный VPN выключен: трафик идёт напрямую или через личный VPN по правилам.";
}

export function routerPriorityLabel(mode?: string): string {
  if (mode === "coexist") {
    return "Обычная сеть > Системный VPN > Личный VPN";
  }
  return "Обычная сеть > Личный VPN";
}

export function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}
