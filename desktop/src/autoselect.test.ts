import { describe, expect, it } from "vitest";
import type { AutoSelectJobStatus, DomainRule } from "./api";
import {
  autoSelectResultToast,
  initialAutoSelectSitesText,
  parseSitesInput,
  renderAutoSelectProgress,
  renderAutoSelectResult,
  sitesFromPersonalRules,
  updateAutoSelectProgress,
} from "./autoselect";

describe("parseSitesInput", () => {
  it("trims, prefixes https, dedupes, limits", () => {
    expect(parseSitesInput("cursor.com\nhttps://a.com\ncursor.com\n")).toEqual([
      "https://cursor.com",
      "https://a.com",
    ]);
  });
});

describe("sitesFromPersonalRules", () => {
  it("takes enabled personal host patterns", () => {
    const rules: DomainRule[] = [
      { id: "1", pattern: "cursor.com", path: "personal", enabled: true },
      { id: "2", pattern: "*.google.com", path: "personal", enabled: true },
      { id: "3", pattern: "corp.local", path: "work", enabled: true },
      { id: "4", pattern: "api2.cursor.sh", path: "personal", enabled: false },
    ];
    expect(sitesFromPersonalRules(rules)).toEqual(["https://cursor.com", "https://google.com"]);
  });
});

describe("initialAutoSelectSitesText", () => {
  it("falls back to defaults without storage/rules", () => {
    expect(initialAutoSelectSitesText([])).toContain("generate_204");
  });

  it("uses personal domain rules when there is no saved list", () => {
    expect(
      initialAutoSelectSitesText([
        { id: "1", pattern: "example.com", path: "personal", enabled: true },
        { id: "2", pattern: "corp.local", path: "work", enabled: true },
      ]),
    ).toBe("https://example.com");
  });
});

describe("renderAutoSelectResult", () => {
  const result: AutoSelectJobStatus = {
    jobId: "j",
    subscriptionId: "s",
    phase: "done",
    checked: 1,
    total: 1,
    attempts: [],
    finished: true,
    selectedNodeId: "node-1",
    selectedNodeName: "NL",
  };

  it("shows an apply error even when a server was selected", () => {
    const html = renderAutoSelectResult({ ...result, error: "reapply: <failed>" });
    expect(html).toContain('class="probe-summary err"');
    expect(html).toContain("reapply: &lt;failed&gt;");
    expect(html).not.toContain("VPN переподключён");
  });

  it("shows a successful selection and reconnection", () => {
    const html = renderAutoSelectResult({ ...result, reconnected: true });
    expect(html).toContain("NL");
    expect(html).toContain("Личный VPN переподключён");
    expect(html).not.toContain('class="probe-summary err"');
  });
});

describe("autoSelectResultToast", () => {
  const result: AutoSelectJobStatus = {
    jobId: "j",
    subscriptionId: "s",
    phase: "done",
    checked: 1,
    total: 1,
    attempts: [],
    finished: true,
  };

  it("prioritizes an apply error over the selected server and success message", () => {
    expect(
      autoSelectResultToast({
        ...result,
        selectedNodeId: "node-1",
        selectedNodeName: "NL",
        message: "Сервер выбран",
        error: "Не удалось применить конфигурацию",
      }),
    ).toEqual({ message: "Не удалось применить конфигурацию", isError: true });
  });

  it("reports a successful selection by name, falling back to ID", () => {
    expect(
      autoSelectResultToast({ ...result, selectedNodeId: "node-1", selectedNodeName: "NL" }),
    ).toEqual({ message: "Выбран сервер: NL", isError: false });
    expect(autoSelectResultToast({ ...result, selectedNodeId: "node-1" })).toEqual({
      message: "Выбран сервер: node-1",
      isError: false,
    });
  });

  it("reports no match as an error, with the daemon message or a fallback", () => {
    expect(autoSelectResultToast({ ...result, message: "Все проверки неуспешны" })).toEqual({
      message: "Все проверки неуспешны",
      isError: true,
    });
    expect(autoSelectResultToast(result)).toEqual({
      message: "Подходящий сервер не найден",
      isError: true,
    });
  });
});

describe("renderAutoSelectProgress", () => {
  it("shows checked/total", () => {
    const html = renderAutoSelectProgress({
      jobId: "j",
      subscriptionId: "s",
      phase: "probing",
      checked: 3,
      total: 40,
      currentName: "NL",
      attempts: [],
      finished: false,
    });
    expect(html).toContain("3");
    expect(html).toContain("40");
    expect(html).toContain("NL");
  });
});

describe("updateAutoSelectProgress", () => {
  const status: AutoSelectJobStatus = {
    jobId: "j",
    subscriptionId: "s",
    phase: "pinging",
    checked: 0,
    total: 312,
    attempts: [],
    finished: false,
  };

  it("keeps the spinner container intact across repeated poll updates", () => {
    const label = { innerHTML: "Запуск подбора…" };
    const container = { innerHTML: "original spinner markup", querySelector: () => label };
    for (let checked = 0; checked < 50; checked++) {
      updateAutoSelectProgress(container as unknown as HTMLElement, {
        ...status,
        phase: "probing",
        checked,
        currentName: `Server ${checked}`,
      });
    }
    expect(container.innerHTML).toBe("original spinner markup");
    expect(label.innerHTML).toContain("<strong>49</strong>");
    expect(label.innerHTML).toContain("<strong>312</strong>");
    expect(label.innerHTML).toContain("Server 49");
  });

  it("mounts a progress indicator when it is not present", () => {
    const container = { innerHTML: "", querySelector: () => null };
    updateAutoSelectProgress(container as unknown as HTMLElement, status);
    expect(container.innerHTML).toContain('class="spinner"');
    expect(container.innerHTML).toContain("Пинг серверов…");
    expect(container.innerHTML).toContain("<strong>312</strong>");
  });

  it.each([
    ["pinging", "Пинг серверов…"],
    ["probing", "Проверка сайтов…"],
    ["done", "Готово"],
    ["error", "Ошибка"],
    ["unknown", "Подбор…"],
  ])("updates the phase %s without replacing the spinner", (phase, expected) => {
    const label = { innerHTML: "" };
    const container = { innerHTML: "original", querySelector: () => label };
    updateAutoSelectProgress(container as unknown as HTMLElement, { ...status, phase });
    expect(label.innerHTML).toContain(expected);
    expect(container.innerHTML).toBe("original");
  });

  it("escapes server names in the updated label", () => {
    const label = { innerHTML: "" };
    const container = { innerHTML: "original", querySelector: () => label };
    updateAutoSelectProgress(container as unknown as HTMLElement, {
      ...status,
      currentName: '<img src=x onerror="alert(1)">',
    });
    expect(label.innerHTML).not.toContain("<img");
    expect(label.innerHTML).toContain("&lt;img");
    expect(container.innerHTML).toBe("original");
  });
});
