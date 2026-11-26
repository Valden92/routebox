import { isTauri } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";

export const shutdownFailureMessage =
  "Не удалось завершить Router BOX. Выполните make stop и повторите закрытие окна.";

export async function setupShutdownNotice(notify: (message: string) => void): Promise<void> {
  if (!isTauri()) return;
  await listen("router-shutdown-failed", () => notify(shutdownFailureMessage));
}
