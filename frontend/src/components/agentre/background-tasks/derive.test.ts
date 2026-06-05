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

  it("threads startedAt from the containing message createtime + durationMs + summary", () => {
    const msg = {
      createtime: 1700000000000,
      blocks: [
        tu({
          toolUseId: "tu9",
          subagent: {
            kind: "local_bash",
            taskDescription: "sleep",
            status: "completed",
            summary: 'Background command "sleep 20" completed (exit code 0)',
          },
        }),
      ],
    };
    const tasks = deriveBackgroundTasks([msg as never], []);
    expect(tasks[0]).toMatchObject({
      toolUseId: "tu9",
      startedAt: 1700000000000,
      summary: 'Background command "sleep 20" completed (exit code 0)',
    });
  });

  it("reads durationMs from subagent for completed subagents", () => {
    const msg = {
      createtime: 1,
      blocks: [
        tu({
          toolUseId: "tA",
          subagent: {
            kind: "local_agent",
            taskDescription: "Explore",
            status: "completed",
            durationMs: 4200,
          },
        }),
      ],
    };
    expect(deriveBackgroundTasks([msg as never], [])[0].durationMs).toBe(4200);
  });

  it("live blocks (no containing message) have undefined startedAt", () => {
    const tasks = deriveBackgroundTasks([], [tu()]);
    expect(tasks[0].startedAt).toBeUndefined();
  });

  it("keeps history startedAt when a running task is also in liveBlocks (live wins but preserves elapsed base)", () => {
    const msg = {
      createtime: 1700000000000,
      blocks: [
        tu({
          toolUseId: "tuBoth",
          subagent: {
            kind: "local_bash",
            taskDescription: "sleep 20",
            status: "running",
          },
        }),
      ],
    };
    const live = [
      tu({
        toolUseId: "tuBoth",
        subagent: {
          kind: "local_bash",
          taskDescription: "sleep 20",
          status: "running",
        },
      }),
    ];
    const tasks = deriveBackgroundTasks([msg as never], live);
    expect(tasks).toHaveLength(1);
    expect(tasks[0]).toMatchObject({
      toolUseId: "tuBoth",
      startedAt: 1700000000000,
      status: "running",
    });
  });
});
