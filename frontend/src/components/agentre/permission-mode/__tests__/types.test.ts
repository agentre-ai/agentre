import { describe, expect, it } from "vitest";

import {
  nextPermissionMode,
  normalizePermissionMode,
  permissionModeDisabledReason,
} from "../types";

// 模拟 caps.permissionModeMeta 的几种典型 runtime 配置。
const CLAUDECODE_ORDER = [
  "default",
  "acceptEdits",
  "plan",
  "bypassPermissions",
] as const;
const CODEX_ORDER = ["default", "plan"] as const;

describe("nextPermissionMode (claudecode order)", () => {
  it("cycles default → acceptEdits → plan → bypassPermissions → default", () => {
    expect(nextPermissionMode("default", CLAUDECODE_ORDER)).toBe("acceptEdits");
    expect(nextPermissionMode("acceptEdits", CLAUDECODE_ORDER)).toBe("plan");
    expect(nextPermissionMode("plan", CLAUDECODE_ORDER)).toBe(
      "bypassPermissions",
    );
    expect(nextPermissionMode("bypassPermissions", CLAUDECODE_ORDER)).toBe(
      "default",
    );
  });

  it("returns first when current not in order", () => {
    expect(nextPermissionMode("unknown", CLAUDECODE_ORDER)).toBe("default");
  });

  it("returns current when order is empty", () => {
    expect(nextPermissionMode("default", [])).toBe("default");
  });
});

describe("nextPermissionMode (codex order)", () => {
  it("cycles default ↔ plan", () => {
    expect(nextPermissionMode("default", CODEX_ORDER)).toBe("plan");
    expect(nextPermissionMode("plan", CODEX_ORDER)).toBe("default");
  });
});

describe("normalizePermissionMode", () => {
  it("returns raw when in allowedModes", () => {
    expect(
      normalizePermissionMode("acceptEdits", CLAUDECODE_ORDER, "default"),
    ).toBe("acceptEdits");
    expect(
      normalizePermissionMode("bypassPermissions", CLAUDECODE_ORDER, "default"),
    ).toBe("bypassPermissions");
  });

  it("falls back to backendDefault when raw is empty/null/undefined", () => {
    expect(
      normalizePermissionMode(undefined, CLAUDECODE_ORDER, "default", "plan"),
    ).toBe("plan");
    expect(
      normalizePermissionMode(null, CLAUDECODE_ORDER, "default", "acceptEdits"),
    ).toBe("acceptEdits");
    expect(
      normalizePermissionMode(
        "",
        CLAUDECODE_ORDER,
        "default",
        "bypassPermissions",
      ),
    ).toBe("bypassPermissions");
  });

  it("falls back to defaultMode when raw is empty and backendDefault is missing/illegal", () => {
    expect(
      normalizePermissionMode(undefined, CLAUDECODE_ORDER, "default"),
    ).toBe("default");
    expect(
      normalizePermissionMode(
        undefined,
        CLAUDECODE_ORDER,
        "default",
        "garbage",
      ),
    ).toBe("default");
  });

  it("falls back to defaultMode when raw is unknown", () => {
    expect(
      normalizePermissionMode("nonsense", CLAUDECODE_ORDER, "default"),
    ).toBe("default");
  });

  it("backendDefault does not override valid raw", () => {
    expect(
      normalizePermissionMode(
        "acceptEdits",
        CLAUDECODE_ORDER,
        "default",
        "plan",
      ),
    ).toBe("acceptEdits");
  });
});

describe("permissionModeDisabledReason", () => {
  it("claudecode + bypassPermissions + active session + non-bypass launch → disabled with reason", () => {
    const reason = permissionModeDisabledReason(
      "bypassPermissions",
      "claudecode",
      { hasActiveSession: true, permissionModeAtLaunch: "" },
    );
    expect(reason).not.toBeNull();
    expect(reason!.length).toBeGreaterThan(0);
    expect(reason).toContain("启动时");
  });

  it("claudecode + non-bypass modes → never disabled", () => {
    for (const m of ["default", "acceptEdits", "plan"] as const) {
      expect(
        permissionModeDisabledReason(m, "claudecode", {
          hasActiveSession: true,
          permissionModeAtLaunch: "plan",
        }),
      ).toBeNull();
    }
  });

  it("pre-spawn (no active session) → bypass is always selectable", () => {
    expect(
      permissionModeDisabledReason("bypassPermissions", "claudecode", {
        hasActiveSession: false,
        permissionModeAtLaunch: "",
      }),
    ).toBeNull();
  });

  it("post-spawn launched in bypass → bypass stays selectable", () => {
    expect(
      permissionModeDisabledReason("bypassPermissions", "claudecode", {
        hasActiveSession: true,
        permissionModeAtLaunch: "bypassPermissions",
      }),
    ).toBeNull();
  });

  it("codex / builtin / empty runtimeKey → never disabled", () => {
    expect(
      permissionModeDisabledReason("bypassPermissions", "codex", {
        hasActiveSession: true,
        permissionModeAtLaunch: "",
      }),
    ).toBeNull();
    expect(
      permissionModeDisabledReason("plan", "builtin", {
        hasActiveSession: true,
        permissionModeAtLaunch: "",
      }),
    ).toBeNull();
    expect(
      permissionModeDisabledReason("bypassPermissions", null, {
        hasActiveSession: true,
        permissionModeAtLaunch: "",
      }),
    ).toBeNull();
  });
});
