import type { DomainRule } from "./api";
import type { RuleSource } from "./rules-groups";

export function ruleClearSnapshot(rules: DomainRule[], source: RuleSource): string[] {
  return [
    ...new Set(
      rules.filter((r) => (r.source === "auto" ? "auto" : "manual") === source).map((r) => r.id),
    ),
  ];
}

export function ruleClearError(error: unknown): string {
  if (error && typeof error === "object" && "code" in error && error.code === "reapply_failed") {
    return "Правила удалены из настроек, но применить изменения не удалось. Проверьте диагностику маршрутизатора.";
  }
  return error instanceof Error ? error.message : "Не удалось очистить правила";
}

interface ClearRulesActions {
  open(title: string, html: string, onClose?: () => void): void;
  close(): void;
  clear(source: RuleSource, ids: string[]): Promise<number | false>;
  notify(message: string, error?: boolean): void;
}

export function showRulesClearConfirmation(
  source: RuleSource,
  ids: string[],
  actions: ClearRulesActions,
) {
  if (!ids.length) return;
  const label = source === "auto" ? "автоматические" : "ручные";
  const previousFocus = document.activeElement;
  actions.open(
    `Очистить ${label} правила?`,
    `
    <p>Будут удалены ${label} доменные правила: <strong>${ids.length}</strong>.</p>
    <p class="muted">Очистка всей секции включает правила, скрытые поиском и фильтрами.
    Новые правила, появившиеся после открытия этого окна, сохранятся.</p>
    ${source === "auto" ? '<p class="muted">При включённом автоматическом запоминании новые правила могут появиться снова.</p>' : ""}
    <p id="rules-clear-error" class="err hidden" role="alert"></p>
    <div class="rules-clear-actions">
      <button type="button" id="rules-clear-cancel" class="secondary">Отмена</button>
      <button type="button" id="rules-clear-confirm" class="secondary btn-danger">Удалить правила (${ids.length})</button>
    </div>`,
    () => {
      if (previousFocus instanceof HTMLElement) {
        const target = previousFocus.matches(":disabled")
          ? previousFocus.closest<HTMLElement>("summary")
          : previousFocus;
        target?.focus();
      }
    },
  );
  const cancel = document.querySelector<HTMLButtonElement>("#rules-clear-cancel")!;
  const confirm = document.querySelector<HTMLButtonElement>("#rules-clear-confirm")!;
  const errorBox = document.querySelector<HTMLElement>("#rules-clear-error")!;
  cancel.addEventListener("click", actions.close);
  cancel.focus();
  confirm.addEventListener("click", async () => {
    if (confirm.disabled) return;
    confirm.disabled = true;
    let needsNewConfirmation = false;
    cancel.disabled = true;
    confirm.textContent = "Удаляем…";
    errorBox.classList.add("hidden");
    try {
      const count = await actions.clear(source, ids);
      if (count === false) throw new Error("Дождитесь завершения другой операции с правилами");
      if (document.contains(confirm)) actions.close();
      actions.notify(`Удалено правил: ${count}`);
    } catch (error) {
      needsNewConfirmation =
        !!error &&
        typeof error === "object" &&
        "code" in error &&
        (error.code === "reapply_failed" || error.code === "rules_changed");
      const message = ruleClearError(error);
      if (document.contains(errorBox)) {
        errorBox.textContent = message;
        errorBox.classList.remove("hidden");
      } else actions.notify(message, true);
    } finally {
      confirm.disabled = false;
      confirm.classList.toggle("hidden", needsNewConfirmation);
      cancel.disabled = false;
      if (needsNewConfirmation) {
        cancel.textContent = "Закрыть";
        cancel.focus();
      }
      confirm.textContent = `Удалить правила (${ids.length})`;
    }
  });
}
