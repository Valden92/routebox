import { ApiError, type RoutePath, type RulesResponse, type RuleAdditionStatus } from "./api";

import type { RuleSource } from "./rules-groups";

export interface NewRule {
  pattern: string;
  path: RoutePath;
}

export interface RulesView {
  data: RulesResponse | null;
  adding: boolean;
  deleting: number;
  clearing: RuleSource | null;
}

interface RulesTransport {
  load(): Promise<RulesResponse>;
  add(rule: NewRule): Promise<unknown>;
  remove(id: string): Promise<unknown>;
  clear?(source: RuleSource, ids: string[]): Promise<{ deleted: number }>;
}

/** Keeps pending deletions hidden even when a background poll finishes. */
export class RulesController {
  private data: RulesResponse | null = null;
  private adding = false;
  private deleting = new Set<string>();
  private clearing: RuleSource | null = null;
  private revision = 0;
  private loading: Promise<void> | null = null;
  private reloadRequested = false;

  constructor(
    private readonly transport: RulesTransport,
    private readonly changed: (view: RulesView) => void,
  ) {}

  private emit(): void {
    this.changed({
      data: this.data && {
        ...this.data,
        domains: this.data.domains.filter((rule) => !this.deleting.has(rule.id)),
      },
      adding: this.adding,
      deleting: this.deleting.size,
      clearing: this.clearing,
    });
  }

  /** A forced refresh queues behind an older poll instead of being dropped. */
  refresh(force = false): Promise<void> {
    if (this.loading) {
      this.reloadRequested ||= force;
      return this.loading;
    }
    this.loading = this.load().finally(() => {
      this.loading = null;
    });
    return this.loading;
  }

  private async load(): Promise<void> {
    do {
      this.reloadRequested = false;
      const revision = this.revision;
      try {
        const data = await this.transport.load();
        if (revision === this.revision) {
          this.data = { domains: data.domains ?? [], apps: data.apps ?? [] };
          this.emit();
        }
      } catch (error) {
        if (!this.reloadRequested) throw error;
      }
    } while (this.reloadRequested);
  }

  async add(rule: NewRule): Promise<RuleAdditionStatus | false> {
    if (this.adding || this.clearing) return false;
    this.adding = true;
    this.revision++;
    this.emit();
    try {
      const status = ruleAdditionStatus(await this.transport.add(rule));
      this.revision++;
      await this.refresh(true);
      return status;
    } catch (error) {
      // Saving can succeed before reapply fails; reconcile instead of guessing.
      await this.refresh(true).catch(() => {});
      throw error;
    } finally {
      this.adding = false;
      this.revision++;
      this.emit();
    }
  }

  async remove(id: string): Promise<boolean> {
    if (this.clearing || this.deleting.has(id)) return false;
    this.deleting.add(id);
    this.revision++;
    this.emit();
    try {
      await this.transport.remove(id);
      this.revision++;
      this.forget(id);
      // A list refresh failure must not undo an acknowledged deletion.
      await this.refresh(true).catch(() => {});
      return true;
    } catch (error) {
      this.revision++;
      if (error instanceof ApiError && error.code === "reapply_failed") this.forget(id);
      await this.refresh(true).catch(() => {});
      throw error;
    } finally {
      this.deleting.delete(id);
      this.revision++;
      this.emit();
    }
  }

  async clear(source: RuleSource, ids: string[]): Promise<number | false> {
    if (this.clearing || this.adding || this.deleting.size || !ids.length) return false;
    if (!this.transport.clear) throw new Error("Очистка правил недоступна");
    const selected = new Set(ids);
    this.clearing = source;
    for (const id of selected) this.deleting.add(id);
    this.revision++;
    this.emit();
    const forget = () => {
      if (this.data)
        this.data = { ...this.data, domains: this.data.domains.filter((r) => !selected.has(r.id)) };
    };
    try {
      const result = await this.transport.clear(source, [...selected]);
      this.revision++;
      forget();
      await this.refresh(true).catch(() => {});
      return result.deleted;
    } catch (error) {
      this.revision++;
      if (error instanceof ApiError && error.code === "reapply_failed") forget();
      await this.refresh(true).catch(() => {});
      throw error;
    } finally {
      for (const id of selected) this.deleting.delete(id);
      this.clearing = null;
      this.revision++;
      this.emit();
    }
  }

  private forget(id: string): void {
    if (this.data) {
      this.data = { ...this.data, domains: this.data.domains.filter((rule) => rule.id !== id) };
    }
  }
}

function ruleAdditionStatus(result: unknown): RuleAdditionStatus {
  if (result && typeof result === "object" && "status" in result) {
    if (result.status === "updated" || result.status === "unchanged") return result.status;
  }
  return "created";
}

export function ruleAdditionMessage(status: RuleAdditionStatus): string {
  return {
    created: "Правило добавлено",
    updated: "Правило обновлено",
    unchanged: "Правило уже существует",
  }[status];
}

export function setRuleButtonPending(button: HTMLButtonElement, pending: boolean): void {
  button.disabled = pending;
  button.classList.toggle("is-pending", pending);
  button.setAttribute("aria-busy", String(pending));
}

export function ruleMutationError(error: unknown, action: "add" | "remove"): string {
  if (error instanceof ApiError && error.code === "reapply_failed") {
    return `${action === "add" ? "Правило сохранено" : "Правило удалено"}, но применить изменения не удалось: ${error.message}`;
  }
  return error instanceof Error ? error.message : "Не удалось изменить правило";
}
