import { describe, expect, it } from "vitest";
import type { RouterConfigurationState } from "./api";
import { renderConfigurationState } from "./config-state";

function state(overrides: Partial<RouterConfigurationState> = {}): RouterConfigurationState {
  return {
    attempt: 1,
    phase: "applied",
    running: true,
    desiredVersion: "111111111111222222222222",
    appliedVersion: "111111111111222222222222",
    lastSuccessfulVersion: "111111111111222222222222",
    reason: "rules_changed",
    matchesSavedSettings: true,
    ...overrides,
  };
}

describe("router configuration diagnostics", () => {
  it("distinguishes published rules from an acknowledged core configuration", () => {
    const html = renderConfigurationState(
      state({
        phase: "rules_published",
        publishedRulesVersion: "aaaaaaaaaaaa123456",
        matchesSavedSettings: undefined,
      }),
    );
    expect(html).toContain("Правила обновлены без перезапуска VPN");
    expect(html).toContain("Опубликованные правила</dt><dd>aaaaaaaaaaaa");
    expect(html).toContain("Подтверждение загрузки от ядра недоступно");
    expect(html).toContain("Уже открытые соединения сохраняют свой маршрут");
    expect(html).not.toContain("Сохранённые настройки соответствуют подтверждённой версии");
  });
  it("distinguishes a checked unchanged config from a restarted one", () => {
    const html = renderConfigurationState(state({ unchanged: true }));
    expect(html).toContain("Конфигурация не изменилась — перезапуск не потребовался");
    expect(html).not.toContain("Последнее применение успешно");
  });
  it("supports an older daemon without the added optional state", () => {
    expect(renderConfigurationState()).toBe("");
  });

  it("starts collapsed and includes a short explanation", () => {
    const html = renderConfigurationState(state());
    expect(html).toContain('<details class="router-configuration">');
    expect(html).toContain("<summary>Диагностика конфигурации</summary>");
    expect(html).toContain("какая конфигурация сейчас работает");
    expect(html).toContain("применены ли сохранённые настройки");
  });

  it("keeps the chosen expansion state when status content changes", () => {
    const updated = state({ phase: "preparing", desiredVersion: "aaaaaaaaaaaa" });
    expect(renderConfigurationState(updated, true)).toContain(
      '<details class="router-configuration" open>',
    );
    expect(renderConfigurationState(updated, true)).toContain("Подготовка и проверка");
    expect(renderConfigurationState(updated, false)).toContain(
      '<details class="router-configuration">',
    );
  });

  it("shows confirmed versions, safe reason and saved settings status", () => {
    const html = renderConfigurationState(state());
    expect(html).toContain("Последнее применение успешно");
    expect(html).toContain("Изменение правил");
    expect(html).toContain("111111111111");
    expect(html).not.toContain("111111111111222222222222");
    expect(html).toContain("соответствуют подтверждённой версии");
  });

  it("keeps a rejected desired version distinct from the restored active version", () => {
    const html = renderConfigurationState(
      state({
        phase: "rolled_back",
        desiredVersion: "aaaaaaaaaaaa",
        appliedVersion: "bbbbbbbbbbbb",
        lastSuccessfulVersion: "bbbbbbbbbbbb",
        matchesSavedSettings: false,
        rollback: "restored",
        error: "Предыдущая рабочая версия восстановлена.",
      }),
    );
    expect(html).toContain("Предыдущая рабочая версия восстановлена");
    expect(html).toContain("Работающая версия</dt><dd>bbbbbbbbbbbb");
    expect(html).toContain("Последний кандидат</dt><dd>aaaaaaaaaaaa");
    expect(html).toContain("не применены к работающей версии");
  });

  it("does not claim that a stopped router runs its last successful version", () => {
    const html = renderConfigurationState(state({ running: false, appliedVersion: undefined }));
    expect(html).toContain("Работающая версия</dt><dd>Маршрутизатор выключен");
    expect(html).toContain("Последняя рабочая</dt><dd>111111111111");
  });

  it("keeps unknown versions and settings correspondence explicit", () => {
    const html = renderConfigurationState(
      state({
        phase: "applying",
        appliedVersion: undefined,
        matchesSavedSettings: undefined,
      }),
    );
    expect(html).toContain("Применение и проверка готовности");
    expect(html).toContain("Работающая версия</dt><dd>Не подтверждена");
    expect(html).toContain("Соответствие сохранённым настройкам ещё не подтверждено");
  });

  it("escapes diagnostic text and does not display arbitrary reason categories", () => {
    const html = renderConfigurationState(
      state({ reason: "<script>secret</script>", error: "<script>error</script>" }),
    );
    expect(html).not.toContain("<script>");
    expect(html).not.toContain("secret");
    expect(html).toContain("&lt;script&gt;error&lt;/script&gt;");
    expect(html).toContain("Изменение конфигурации");
  });
});
