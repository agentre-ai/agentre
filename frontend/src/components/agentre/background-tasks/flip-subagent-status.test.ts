import { describe, expect, it } from "vitest";

import type { chat_svc } from "../../../../wailsjs/go/models";

import {
  flipSubagentStatusInMessages,
  mergeSubagentMetaInMessages,
} from "./flip-subagent-status";

// A background task's tool_use block lives in `messages` (not liveBlocks) by the
// time its cross-turn completion (autonomous `completedTask`) arrives — the main
// turn that launched it has already ended. flipSubagentStatusInMessages patches
// that persisted block's subagent.status/summary so the panel + inline pill stop
// spinning without waiting for a full session reload.
const msg = (id: number, blocks: unknown[]) =>
  ({ id, blocks }) as unknown as chat_svc.ChatMessage;
const toolUse = (
  toolUseId: string,
  subagent: Record<string, unknown> | undefined,
) =>
  ({
    type: "tool_use",
    toolUseId,
    toolInput: { run_in_background: true },
    subagent,
  }) as unknown as chat_svc.ChatBlock;

describe("flipSubagentStatusInMessages", () => {
  it("flips the matching running block to completed and sets summary", () => {
    const messages = [
      msg(1, [toolUse("tu-bg", { kind: "local_bash", status: "running" })]),
    ];
    const out = flipSubagentStatusInMessages(
      messages,
      "tu-bg",
      "completed",
      "Background command finished (exit 0)",
    );
    expect(out[0].blocks[0].subagent?.status).toBe("completed");
    expect(out[0].blocks[0].subagent?.summary).toBe(
      "Background command finished (exit 0)",
    );
  });

  it("does not mutate the input messages (immutable update)", () => {
    const messages = [
      msg(1, [toolUse("tu-bg", { kind: "local_bash", status: "running" })]),
    ];
    flipSubagentStatusInMessages(messages, "tu-bg", "completed");
    expect(messages[0].blocks[0].subagent?.status).toBe("running");
  });

  it("returns the same array reference when the toolUseId is not found", () => {
    const messages = [
      msg(1, [toolUse("tu-other", { kind: "local_bash", status: "running" })]),
    ];
    const out = flipSubagentStatusInMessages(
      messages,
      "tu-missing",
      "completed",
    );
    expect(out).toBe(messages);
  });

  it("leaves sibling blocks and other messages untouched", () => {
    const sibling = toolUse("tu-keep", {
      kind: "local_bash",
      status: "running",
    });
    const messages = [
      msg(1, [sibling]),
      msg(2, [toolUse("tu-bg", { kind: "local_agent", status: "running" })]),
    ];
    const out = flipSubagentStatusInMessages(messages, "tu-bg", "completed");
    expect(out[0]).toBe(messages[0]); // untouched message keeps its reference
    expect(out[1].blocks[0].subagent?.status).toBe("completed");
    expect(out[0].blocks[0].subagent?.status).toBe("running");
  });

  it("no-ops (empty status) leave messages unchanged", () => {
    const messages = [
      msg(1, [toolUse("tu-bg", { kind: "local_bash", status: "running" })]),
    ];
    expect(flipSubagentStatusInMessages(messages, "tu-bg", "")).toBe(messages);
    expect(flipSubagentStatusInMessages(messages, "", "completed")).toBe(
      messages,
    );
  });
});

// A background subagent keeps working while the session is idle: the CLI streams
// task_progress the whole time, but its spawn card (the Agent tool_use block) has
// long since landed in `messages`. mergeSubagentMetaInMessages patches that
// persisted block so the card's tool count / token pill keeps ticking live.
describe("mergeSubagentMetaInMessages", () => {
  it("merges live progress into the persisted spawn card", () => {
    const messages = [
      msg(1, [
        toolUse("tu-agent", {
          kind: "local_agent",
          status: "running",
          toolUses: 9,
          totalTokens: 84739,
          lastToolName: "Read",
        }),
      ]),
    ];
    const out = mergeSubagentMetaInMessages(messages, "tu-agent", {
      toolUses: 21,
      totalTokens: 132480,
      lastToolName: "Edit",
    } as unknown as chat_svc.ChatBlockSubagent);
    expect(out[0].blocks[0].subagent?.toolUses).toBe(21);
    expect(out[0].blocks[0].subagent?.totalTokens).toBe(132480);
    expect(out[0].blocks[0].subagent?.lastToolName).toBe("Edit");
    // fields the progress frame didn't carry stay put
    expect(out[0].blocks[0].subagent?.status).toBe("running");
    expect(out[0].blocks[0].subagent?.kind).toBe("local_agent");
  });

  it("does not mutate the input messages (immutable update)", () => {
    const messages = [
      msg(1, [toolUse("tu-agent", { status: "running", toolUses: 9 })]),
    ];
    mergeSubagentMetaInMessages(messages, "tu-agent", {
      toolUses: 21,
    } as unknown as chat_svc.ChatBlockSubagent);
    expect(messages[0].blocks[0].subagent?.toolUses).toBe(9);
  });

  it("returns the same array reference on no-ops", () => {
    const messages = [
      msg(1, [toolUse("tu-agent", { status: "running", toolUses: 9 })]),
    ];
    expect(
      mergeSubagentMetaInMessages(messages, "tu-missing", {
        toolUses: 21,
      } as unknown as chat_svc.ChatBlockSubagent),
    ).toBe(messages);
    expect(
      mergeSubagentMetaInMessages(
        messages,
        "tu-agent",
        undefined as unknown as chat_svc.ChatBlockSubagent,
      ),
    ).toBe(messages);
  });
});
