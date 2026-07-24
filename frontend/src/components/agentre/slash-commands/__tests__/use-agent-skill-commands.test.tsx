import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useAgentSkillCommands } from "../use-agent-skill-commands";

function stubCatalog(commands: unknown[]) {
  const fn = vi.fn().mockResolvedValue({ commands });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = { app: { App: { ListAgentSkillCommands: fn } } };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

describe("useAgentSkillCommands", () => {
  it("Given a Codex agent with an effective skill, When the composer mounts, Then it loads $ suggestions from Wails", async () => {
    const list = stubCatalog([
      {
        description: "Browse the web",
        name: "browser:browser",
      },
      {
        description: "Local system skill",
        name: "shadcn",
      },
    ]);

    const { result } = renderHook(() =>
      useAgentSkillCommands(7, "codex", "/tmp/project"),
    );

    await waitFor(() =>
      expect(result.current.map((command) => command.label)).toEqual([
        "$browser:browser",
        "$shadcn",
      ]),
    );
    expect(list).toHaveBeenCalledWith(7, "/tmp/project");
  });

  it("Given no agent or a backend without skill commands, When the composer mounts, Then it avoids unnecessary discovery", async () => {
    const list = stubCatalog([]);
    const { result, rerender } = renderHook(
      ({ agentId, backendType }) => useAgentSkillCommands(agentId, backendType),
      { initialProps: { agentId: 0, backendType: "codex" } },
    );

    expect(result.current).toEqual([]);
    rerender({ agentId: 7, backendType: "builtin" });
    await Promise.resolve();
    expect(result.current).toEqual([]);
    expect(list).not.toHaveBeenCalled();
  });

  it("Given a Pi agent with a project skill, When the composer mounts, Then it loads /skill:name suggestions", async () => {
    const list = stubCatalog([
      { name: "skill:review", description: "Review changes" },
    ]);

    const { result } = renderHook(() =>
      useAgentSkillCommands(7, "piagent", "/work/project"),
    );

    await waitFor(() =>
      expect(result.current.map((command) => command.label)).toEqual([
        "/skill:review",
      ]),
    );
    expect(list).toHaveBeenCalledWith(7, "/work/project");
  });
});
