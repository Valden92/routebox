import { api, type AutoRoutingSettings } from "./api";
import { isDaemonApiOutdated } from "./status-ui";

export interface AutoRoutingView {
  enabled: boolean | null;
  pending: boolean;
  error: string;
}

interface AutoRoutingTransport {
  load(): Promise<AutoRoutingSettings>;
  save(enabled: boolean): Promise<AutoRoutingSettings>;
}

export function autoRoutingTransport(request: typeof api = api): AutoRoutingTransport {
  return {
    load: async () => {
      const version = await request<{ apiVersion?: number }>("/api/version");
      if (isDaemonApiOutdated(version.apiVersion)) {
        throw new Error("Перезапустите приложение через make dev");
      }
      return request<AutoRoutingSettings>("/api/settings");
    },
    save: (enabled) =>
      request<AutoRoutingSettings>("/api/settings", {
        method: "PUT",
        body: JSON.stringify({ autoDetectRouting: enabled }),
      }),
  };
}

export function autoRoutingStatus(view: AutoRoutingView): string {
  if (view.error) return view.error;
  if (view.pending) return view.enabled === null ? "Загрузка настройки…" : "Сохранение настройки…";
  return view.enabled
    ? "Фоновое обучение включено. Существующие правила продолжают работать."
    : "Фоновое обучение выключено. Существующие правила и ручная проверка продолжают работать.";
}

/** Preserve the acknowledged value on failed saves and prevent overlapping changes. */
export class AutoRoutingController {
  private enabled: boolean | null = null;
  private pending = false;
  private error = "";

  constructor(
    private readonly transport: AutoRoutingTransport,
    private readonly changed: (view: AutoRoutingView) => void,
  ) {}

  private emit(): void {
    this.changed({ enabled: this.enabled, pending: this.pending, error: this.error });
  }

  async refresh(): Promise<void> {
    if (this.pending) return;
    this.pending = true;
    this.error = "";
    this.emit();
    try {
      this.enabled = (await this.transport.load()).autoDetectRouting;
    } catch {
      this.enabled = null;
      this.error =
        "Не удалось загрузить настройку. Проверьте запуск через make dev и откройте вкладку ещё раз.";
    } finally {
      this.pending = false;
      this.emit();
    }
  }

  async setEnabled(enabled: boolean): Promise<boolean> {
    if (this.pending || this.enabled === null) return false;
    const previous = this.enabled;
    this.enabled = enabled;
    this.pending = true;
    this.error = "";
    this.emit();
    try {
      this.enabled = (await this.transport.save(enabled)).autoDetectRouting;
      return true;
    } catch (error) {
      this.enabled = previous;
      throw error;
    } finally {
      this.pending = false;
      this.emit();
    }
  }
}
