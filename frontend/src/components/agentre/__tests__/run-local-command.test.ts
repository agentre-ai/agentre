import { afterEach, describe, expect, it, vi } from "vitest";

import {
  EnsureChatSession,
  ResolveLocalCommandScope,
  TerminalRunCommand,
} from "../../../../wailsjs/go/app/App";
import { makeStreamDecoder } from "../local-command/decode";

afterEach(() => {
  delete window.go;
});

describe("makeStreamDecoder", () => {
  it("decodes base64 chunks incrementally", () => {
    const dec = makeStreamDecoder();
    // "hi" = aGk=
    expect(dec("aGk=")).toBe("hi");
  });
});

describe("shared Wails command bindings mock", () => {
  it("Given window-backed command bindings, When the aliases are called, Then every production-compatible argument and response is delegated unchanged", async () => {
    const request = { agentId: 7, projectId: 2, sessionId: 0 };
    const scope = { deviceId: "7", cwd: "/srv/project" };
    const response = { scope, startError: "executable not found" };
    const bridge = {
      EnsureChatSession: vi.fn().mockResolvedValue(91),
      ResolveLocalCommandScope: vi.fn().mockResolvedValue(scope),
      TerminalRunCommand: vi.fn().mockResolvedValue(response),
    };
    window.go = { app: { App: bridge } };

    await expect(ResolveLocalCommandScope(request)).resolves.toEqual(scope);
    await expect(EnsureChatSession(7, 2)).resolves.toBe(91);
    await expect(
      TerminalRunCommand("terminal-1", 91, "pwd", 80, 24),
    ).resolves.toEqual(response);
    expect(bridge.ResolveLocalCommandScope).toHaveBeenCalledWith(request);
    expect(bridge.EnsureChatSession).toHaveBeenCalledWith(7, 2);
    expect(bridge.TerminalRunCommand).toHaveBeenCalledWith(
      "terminal-1",
      91,
      "pwd",
      80,
      24,
    );
  });

  it("Given no Wails bridge, When command bindings use their defaults, Then no session or execution scope is fabricated", async () => {
    await expect(EnsureChatSession(7, 2)).resolves.toBe(0);
    await expect(
      ResolveLocalCommandScope({ agentId: 7, projectId: 2, sessionId: 0 }),
    ).rejects.toThrow("Wails binding ResolveLocalCommandScope not available");
    await expect(
      TerminalRunCommand("terminal-1", 91, "pwd", 80, 24),
    ).rejects.toThrow("Wails binding TerminalRunCommand not available");
  });
});
