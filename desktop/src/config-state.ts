import type { RouterConfigurationState } from "./api";
import { escapeHtml } from "./ui-labels";

const phases: Record<RouterConfigurationState["phase"], string> = {
  unknown: "Версия ещё не подтверждена",
  preparing: "Подготовка и проверка новой конфигурации",
  applying: "Применение и проверка готовности",
  applied: "Последнее применение успешно",
  rejected: "Новая конфигурация отклонена",
  rolling_back: "Восстановление предыдущей версии",
  rolled_back: "Предыдущая рабочая версия восстановлена",
  rollback_failed: "Восстановление не завершилось успешно",
  failed: "Запуск конфигурации не удался",
};

const reasons: Record<string, string> = {
  startup: "Запуск демона",
  rules_changed: "Изменение правил",
  subscription_changed: "Изменение подписки или сервера",
  personal_enabled: "Включение личного VPN",
  personal_disabled: "Выключение личного VPN",
  manual_reapply: "Повторное применение",
  system_vpn_changed: "Изменение режима системного VPN",
  background_learning: "Фоновое обучение правил",
  auto_select: "Автовыбор сервера",
};

function version(value?: string): string {
  return value ? escapeHtml(value.slice(0, 12)) : "Не подтверждена";
}

export function renderConfigurationState(
  state?: RouterConfigurationState,
  expanded = false,
): string {
  if (!state) return "";
  const status = phases[state.phase] ?? phases.unknown;
  const pending = state.matchesSavedSettings === false;
  const failed = ["rejected", "rolled_back", "rollback_failed", "failed"].includes(state.phase);
  const tone = failed || pending ? "err" : "muted";
  const saved = pending
    ? "Есть сохранённые изменения, которые не применены к работающей версии."
    : state.matchesSavedSettings === true
      ? "Сохранённые настройки соответствуют подтверждённой версии."
      : "Соответствие сохранённым настройкам ещё не подтверждено.";
  return `<details class="router-configuration"${expanded ? " open" : ""}>
    <summary>Диагностика конфигурации</summary>
    <p class="muted">Здесь видно, какая конфигурация сейчас работает и применены ли сохранённые настройки. При ошибке показан результат восстановления предыдущей версии.</p>
    <p class="${tone}">${escapeHtml(status)}</p>
    <dl>
      <dt>Работающая версия</dt><dd>${state.running ? version(state.appliedVersion) : "Маршрутизатор выключен"}</dd>
      <dt>Последний кандидат</dt><dd>${version(state.desiredVersion)}</dd>
      <dt>Последняя рабочая</dt><dd>${version(state.lastSuccessfulVersion)}</dd>
      ${state.reason ? `<dt>Причина применения</dt><dd>${escapeHtml(reasons[state.reason] ?? "Изменение конфигурации")}</dd>` : ""}
    </dl>
    <p class="${pending ? "err" : "muted"}">${saved}</p>
    ${state.error ? `<p class="err">${escapeHtml(state.error)}</p>` : ""}
  </details>`;
}
