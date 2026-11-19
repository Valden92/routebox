import type { AutoSelectStartResponse } from "./api";

/** Одна попытка подбора; закрытие ждёт отмены даже при ещё не полученном jobId. */
export class AutoSelectSession {
  private request: Promise<AutoSelectStartResponse> | null = null;
  private cleanup: Promise<void> | null = null;
  private controller = new AbortController();
  private isClosed = false;

  constructor(
    private previous: AutoSelectSession | null,
    private cancelJob: (job: AutoSelectStartResponse) => Promise<void>,
  ) {}

  get closed(): boolean {
    return this.isClosed;
  }

  get signal(): AbortSignal {
    return this.controller.signal;
  }

  async start(
    request: () => Promise<AutoSelectStartResponse>,
  ): Promise<AutoSelectStartResponse | null> {
    await this.waitForPrevious();
    if (this.closed) return null;
    // POST не прерываем локальным AbortSignal: нужен его jobId для отмены
    // на демоне, даже если пользователь уже закрыл окно.
    this.request ??= request();
    const job = await this.request;
    if (this.closed) {
      await this.close();
      return null;
    }
    return job;
  }

  close(): Promise<void> {
    this.isClosed = true;
    this.controller.abort();
    if (this.cleanup) return this.cleanup;
    const cleanup = this.stop();
    this.cleanup = cleanup;
    // При сетевой ошибке следующий запуск может повторить отмену.
    void cleanup.catch(() => {
      if (this.cleanup === cleanup) this.cleanup = null;
    });
    return cleanup;
  }

  private async waitForPrevious(): Promise<void> {
    const previous = this.previous;
    if (previous) {
      await previous.close();
      this.previous = null;
    }
  }

  private async stop(): Promise<void> {
    await this.waitForPrevious();
    if (!this.request) return;
    let job: AutoSelectStartResponse;
    try {
      job = await this.request;
    } catch {
      return; // запрос запуска не дал jobId
    }
    await this.cancelJob(job);
  }
}
