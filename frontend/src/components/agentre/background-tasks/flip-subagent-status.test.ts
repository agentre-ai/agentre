import { describe, expect, it } from "vitest";

import type { chat_svc } from "../../../../wailsjs/go/models";

import { flipSubagentStatusInMessages } from "./flip-subagent-status";

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
