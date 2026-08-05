import { describe, expect, it } from "vitest";
import type { PersonalReadiness, StatusResponse } from "./api";
import {
  isDaemonApiOutdated,
  personalConnectGate,
  personalStatusFlags,
  systemVpnConnectingHint,
  systemVpnState,
} from "./status-ui";

const baseSystem = {
  connected: false,
  connectionId: "PTsecurity",
} satisfies StatusResponse["systemVpn"];

describe("systemVpnState", () => {
  it("prefers explicit state", () => {
    expect(systemVpnState({ ...baseSystem, state: "connecting", connected: false })).toBe(
      "connecting",
    );
    expect(systemVpnState({ ...baseSystem, state: "connected", connected: false })).toBe(
      "connected",
    );
  });

  it("falls back to connected flag", () => {
    expect(systemVpnState({ ...baseSystem, connected: true })).toBe("connected");
    expect(systemVpnState({ ...baseSystem, connected: false })).toBe("disconnected");
  });
});

describe("systemVpnConnectingHint", () => {
  it("detects MFA", () => {
    expect(systemVpnConnectingHint("need-auth")).toBe("Ожидание MFA");
    expect(systemVpnConnectingHint("AUTH_DIALOG")).toBe("Ожидание MFA");
    expect(systemVpnConnectingHint("activating")).toBe("Подключение…");
    expect(systemVpnConnectingHint(undefined)).toBe("Подключение…");
  });
});

describe("personalConnectGate", () => {
  it("gates connect", () => {
    expect(personalConnectGate({ configured: true })).toBe("ready");
    expect(
      personalConnectGate({ configured: false, reason: "host_not_ready" } as PersonalReadiness),
    ).toBe("host_not_ready");
    expect(personalConnectGate({ configured: false, reason: "no_subscription" })).toBe("setup");
  });
});

describe("personalStatusFlags", () => {
  const basePv: StatusResponse["personalVpn"] = { running: false };

  it("derives UI flags", () => {
    expect(personalStatusFlags(basePv)).toEqual({
      configured: false,
      showHostSyncHint: false,
      showConfigStale: false,
      showPolkitAuthHint: false,
      showCoexistHint: false,
    });

    expect(
      personalStatusFlags({
        ...basePv,
        configured: true,
        hostConfigured: false,
        configStale: true,
        configMode: "coexist",
        running: true,
        configFileMode: "full",
        systemVpnActive: true,
      }),
    ).toEqual({
      configured: true,
      showHostSyncHint: true,
      showConfigStale: true,
      showPolkitAuthHint: true,
      showCoexistHint: true,
    });
  });

  it("hides polkit hint when already coexist", () => {
    expect(
      personalStatusFlags({
        ...basePv,
        running: true,
        configFileMode: "coexist",
        systemVpnActive: true,
      }).showPolkitAuthHint,
    ).toBe(false);
  });
});

describe("isDaemonApiOutdated", () => {
  it("requires apiVersion >= 4", () => {
    expect(isDaemonApiOutdated(undefined)).toBe(true);
    expect(isDaemonApiOutdated(3)).toBe(true);
    expect(isDaemonApiOutdated(4)).toBe(false);
  });
});
