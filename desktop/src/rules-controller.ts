import { ApiError, type RoutePath, type RulesResponse } from "./api";

export interface NewRule {
  pattern: string;
  path: RoutePath;
}

export interface RulesView {
  data: RulesResponse | null;
  adding: boolean;
  deleting: number;
}

interface RulesTransport {
  load(): Promise<RulesResponse>;
  add(rule: NewRule): Promise<unknown>;
  remove(id: string): Promise<unknown>;
}

/** Keeps pending deletions hidden even when a background poll finishes. */
export class RulesController {
  private data: RulesResponse | null = null;
  private adding = false;
  private deleting = new Set<string>();
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

  async add(rule: NewRule): Promise<boolean> {
    if (this.adding) return false;
    this.adding = true;
    this.revision++;
    this.emit();
    try {
      await this.transport.add(rule);
      this.revision++;
      await this.refresh(true);
      return true;
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
    if (this.deleting.has(id)) return false;
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

  private forget(id: string): void {
    if (this.data) {
      this.data = { ...this.data, domains: this.data.domains.filter((rule) => rule.id !== id) };
    }
  }
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
