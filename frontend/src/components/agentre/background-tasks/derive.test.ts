import { describe, expect, it } from "vitest";

import { deriveBackgroundTasks } from "./derive";

const tu = (over: Record<string, unknown> = {}) =>
  ({
    type: "tool_use",
    toolUseId: "tu1",
    subagent: {
      kind: "local_bash",
      taskDescription: "sleep 20",
      status: "running",
    },
    ...over,
  }) as unknown as Parameters<typeof deriveBackgroundTasks>[1][number];

describe("deriveBackgroundTasks", () => {
  it("derives running task from a live tool_use block with .subagent", () => {
    const tasks = deriveBackgroundTasks([], [tu()]);
    expect(tasks).toEqual([
      {
        toolUseId: "tu1",
        kind: "local_bash",
        description: "sleep 20",
        status: "running",
      },
    ]);
  });

  it("includes persisted-message tool_use tasks and maps local_agent + completed", () => {
    const msg = {
      blocks: [
        tu({
          toolUseId: "tu2",
          subagent: {
            kind: "local_agent",
            taskDescription: "Explore repo",
            status: "completed",
          },
        }),
      ],
    };
    const tasks = deriveBackgroundTasks([msg as never], []);
    expect(tasks[0]).toMatchObject({
      toolUseId: "tu2",
      kind: "local_agent",
      status: "completed",
    });
  });

  it("live overrides history for the same toolUseId (dedupe, live wins)", () => {
    const msg = {
      blocks: [
        tu({
          subagent: {
            kind: "local_bash",
            taskDescription: "x",
            status: "running",
          },
        }),
      ],
    };
    const live = [
      tu({
        subagent: {
          kind: "local_bash",
          taskDescription: "x",
          status: "completed",
        },
      }),
    ];
    const tasks = deriveBackgroundTasks([msg as never], live);
    expect(tasks).toHaveLength(1);
    expect(tasks[0].status).toBe("completed");
  });

  it("ignores tool_use blocks without .subagent and non-tool_use blocks", () => {
    const tasks = deriveBackgroundTasks(
      [
        {
          blocks: [
            { type: "tool_use", toolUseId: "x" },
            { type: "text", text: "hi" },
          ],
        } as never,
      ],
      [],
    );
    expect(tasks).toEqual([]);
  });

  it("empty/unknown kind falls back to local_agent", () => {
    const tasks = deriveBackgroundTasks(
      [],
      [tu({ subagent: { taskDescription: "y", status: "running" } })],
    );
    expect(tasks[0].kind).toBe("local_agent");
  });
});
