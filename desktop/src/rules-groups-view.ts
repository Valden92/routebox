import type { DomainRule } from "./api";
import {
  RulesDisclosure,
  rulePaths,
  ruleSources,
  type RuleGroupKey,
  type RuleSource,
  type RulesGroups,
} from "./rules-groups";

export class RulesGroupsView {
  private readonly disclosure: RulesDisclosure;
  private busy = false;
  private totals: Partial<Record<RuleSource, number>> = {};
  private readonly details: HTMLDetailsElement[];

  constructor(
    private readonly root: HTMLElement,
    storage?: Pick<Storage, "getItem" | "setItem">,
    private readonly onClear?: (source: RuleSource) => void,
  ) {
    this.disclosure = new RulesDisclosure(storage);
    root.innerHTML = ruleSources
      .map(
        (source) => `<details class="rules-section" data-rule-group="${source.key}">
          <summary><span>${source.title}</span><span class="rules-group-count" data-count="${source.key}"></span><button type="button" class="secondary small rules-clear" data-clear-source="${source.key}" aria-label="Очистить ${source.title.toLowerCase()}" disabled>Очистить</button></summary>
          <div class="rules-section-body">
            <p class="muted hidden" data-empty="${source.key}"></p>
            ${rulePaths
              .map(
                (
                  path,
                ) => `<details class="rules-path-group" data-rule-group="${source.key}-${path.key}">
                  <summary><span>${path.title}</span><span class="rules-group-count" data-count="${source.key}-${path.key}"></span></summary>
                  <ul class="rules-list" data-rules="${source.key}-${path.key}"></ul>
                </details>`,
              )
              .join("")}
          </div>
        </details>`,
      )
      .join("");
    root.querySelectorAll<HTMLButtonElement>("[data-clear-source]").forEach((button) => {
      button.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        this.onClear?.(button.dataset.clearSource as RuleSource);
      });
    });
    this.details = [...root.querySelectorAll<HTMLDetailsElement>("[data-rule-group]")];
    for (const details of this.details) {
      const key = details.dataset.ruleGroup as RuleGroupKey;
      details.open = this.disclosure.open(key);
      details.addEventListener("toggle", () => {
        if (details.open !== this.disclosure.open(key)) this.disclosure.set(key, details.open);
      });
    }
  }

  render(
    groups: RulesGroups,
    filterKey: string,
    renderList: (list: HTMLElement, rules: DomainRule[], empty: string) => void,
  ) {
    // Capture a click whose native toggle event is still queued before refreshing.
    for (const details of this.details) {
      const key = details.dataset.ruleGroup as RuleGroupKey;
      if (details.open !== this.disclosure.open(key)) this.disclosure.set(key, details.open);
    }
    this.disclosure.setFilter(filterKey);
    for (const details of this.details) {
      details.open = this.disclosure.open(details.dataset.ruleGroup as RuleGroupKey);
    }
    for (const source of groups) {
      this.totals[source.key] = source.total;
      this.count(source.key, source.matched, source.total, Boolean(filterKey));
      const empty = this.root.querySelector<HTMLElement>(`[data-empty="${source.key}"]`)!;
      empty.classList.toggle("hidden", source.matched > 0);
      empty.textContent = source.total ? "Нет правил по заданным фильтрам." : "Пока нет правил.";
      for (const group of source.groups) {
        const key: RuleGroupKey = `${source.key}-${group.key}`;
        const details = this.root.querySelector<HTMLElement>(`[data-rule-group="${key}"]`)!;
        details.classList.toggle("hidden", group.rules.length === 0);
        this.count(key, group.rules.length, group.total, Boolean(filterKey));
        const list = this.root.querySelector<HTMLElement>(`[data-rules="${key}"]`)!;
        renderList(list, group.rules, "Правил нет.");
      }
    }
    this.setBusy(this.busy);
  }

  setBusy(busy: boolean) {
    this.busy = busy;
    this.root.querySelectorAll<HTMLButtonElement>("[data-clear-source]").forEach((button) => {
      const source = button.dataset.clearSource as RuleSource;
      button.disabled = busy || !this.onClear || !this.totals[source];
    });
  }

  private count(key: RuleGroupKey, shown: number, total: number, filtered: boolean) {
    this.root.querySelector<HTMLElement>(`[data-count="${key}"]`)!.textContent = filtered
      ? `${shown} / ${total}`
      : String(total);
  }
}
