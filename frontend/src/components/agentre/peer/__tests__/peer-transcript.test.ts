import { describe, it, expect } from "vitest";
import {
  createPeerTranscript,
  reducePeerEvent,
  reducePeerPullPage,
  type PeerEventFrame,
} from "../peer-transcript";

const frame = (
  seq: number,
  kind: string,
  extra: Record<string, unknown> = {},
) =>
  ({
    fingerprint: "sha256:peer-desktop",
    sessionId: 7,
    seq,
    event: { kind, ...extra },
  }) satisfies PeerEventFrame;

describe("peer-transcript", () => {
  it("user_message opens a user row with source identity (R21)", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(
      s,
      frame(1, "user_message", {
        text: "帮我看看",
        sourceDevice: "sha256:browser",
        sourceDeviceName: "Chrome",
      }),
    );
    expect(s.messages).toHaveLength(1);
    const m = s.messages[0];
    expect(m.role).toBe("user");
    expect(m.blocks[0]).toMatchObject({ type: "text", text: "帮我看看" });
    expect(m.sourceDeviceName).toBe("Chrome");
  });

  it("text_delta accumulates into the current assistant message", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(s, frame(1, "user_message", { text: "hi" }));
    s = reducePeerEvent(s, frame(2, "text_delta", { text: "Hello" }));
    s = reducePeerEvent(s, frame(3, "text_delta", { text: " world" }));
    expect(s.messages).toHaveLength(2);
    expect(s.messages[1].role).toBe("assistant");
    expect(s.messages[1].blocks[0]).toMatchObject({
      type: "text",
      text: "Hello world",
    });
    expect(s.cursor).toBe(3);
  });

  it("done closes the assistant turn; the next assistant event opens a new row", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(s, frame(1, "user_message", { text: "hi" }));
    s = reducePeerEvent(s, frame(2, "text_delta", { text: "one" }));
    s = reducePeerEvent(s, frame(3, "done"));
    s = reducePeerEvent(s, frame(4, "user_message", { text: "again" }));
    s = reducePeerEvent(s, frame(5, "text_delta", { text: "two" }));
    expect(s.messages).toHaveLength(4);
    expect(s.messages[3].role).toBe("assistant");
    expect(s.messages[3].blocks[0]).toMatchObject({
      type: "text",
      text: "two",
    });
  });

  it("tool_use_start + tool_result pair into one assistant row", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(
      s,
      frame(1, "tool_use_start", { id: "t1", name: "Read" }),
    );
    s = reducePeerEvent(
      s,
      frame(2, "tool_result", { toolCallId: "t1", content: "file content" }),
    );
    expect(s.messages).toHaveLength(1);
    const m = s.messages[0];
    expect(m.blocks[0]).toMatchObject({
      type: "tool_use",
      toolUseId: "t1",
      toolName: "Read",
    });
    expect(m.blocks[1]).toMatchObject({
      type: "tool_result",
      toolUseId: "t1",
      text: "file content",
    });
  });

  it("ask_user_question becomes a pending decision; answered marks it", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(
      s,
      frame(1, "ask_user_question", {
        requestId: "req-1",
        questions: [
          { id: "q1", question: "继续?", header: "确认", options: [] },
        ],
      }),
    );
    expect(s.decisions).toHaveLength(1);
    expect(s.decisions[0]).toMatchObject({ kind: "ask", requestId: "req-1" });
    expect(s.waitingForInput).toBe(true);
    s = reducePeerEvent(
      s,
      frame(2, "ask_user_question_answered", {
        requestId: "req-1",
        skipped: false,
      }),
    );
    expect(s.decisions[0]).toMatchObject({ kind: "ask", answered: true });
    expect(s.waitingForInput).toBe(false);
  });

  it("tool_permission_request becomes a pending decision; resolved marks it", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(
      s,
      frame(1, "tool_permission_request", {
        requestId: "p-1",
        toolName: "Bash",
        toolCallId: "t1",
      }),
    );
    expect(s.decisions[0]).toMatchObject({
      kind: "permission",
      requestId: "p-1",
      toolName: "Bash",
    });
    s = reducePeerEvent(
      s,
      frame(2, "tool_permission_resolved", { requestId: "p-1", allowed: true }),
    );
    expect(s.decisions[0]).toMatchObject({
      kind: "permission",
      resolved: true,
      allowed: true,
    });
  });

  it("unknown kinds fall back to a raw block instead of being dropped (R8)", () => {
    let s = createPeerTranscript();
    s = reducePeerEvent(s, frame(1, "subagent_started", { toolCallId: "x" }));
    const last = s.messages.at(-1)!;
    expect(last.blocks[0].type).toBe("raw");
  });

  it("reducePeerPullPage overlays the journal seq onto each frame", () => {
    let s = createPeerTranscript();
    s = reducePeerPullPage(s, [
      {
        seq: 1,
        params: {
          sessionId: 7,
          event: { kind: "user_message", text: "历史第一句" },
        },
      },
      {
        seq: 2,
        params: { sessionId: 7, event: { kind: "text_delta", text: "回复" } },
      },
    ]);
    expect(s.messages).toHaveLength(2);
    expect(s.messages[0]).toMatchObject({ role: "user", seq: 1 });
    expect(s.messages[1]).toMatchObject({ role: "assistant", seq: 2 });
    expect(s.cursor).toBe(2);
  });
});
