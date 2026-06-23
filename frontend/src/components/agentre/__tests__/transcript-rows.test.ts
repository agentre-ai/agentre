import { describe, expect, it } from "vitest";

import {
  buildRenderItems,
  buildTranscriptRows,
  type TranscriptRow,
} from "@/components/agentre/transcript-rows";
import type { ChatBlockData } from "@/stores/chat-streams-store";
import type { chat_svc } from "../../../../wailsjs/go/models";

// buildRenderItems 是 renderMessageBlocks 状态机的纯函数抽取。这些单测把配对 /
// 合并 / skip / FIFO / 归集 / 合成顺序 / uiStateKey 字面量逐一钉死 —— 行级虚拟化
// 重构(把每个 RenderItem 拆成独立虚拟行)的逻辑根基全在这里。

function toolUse(
  toolUseId: string | undefined,
  toolName = "Bash",
  extra: Partial<ChatBlockData> = {},
): ChatBlockData {
  return {
    toolInput: { command: "echo hi" },
    toolName,
    toolUseId,
    type: "tool_use",
    ...extra,
  } as ChatBlockData;
}

function toolResult(
  toolUseId: string | undefined,
  text = "ok",
  extra: Partial<ChatBlockData> = {},
): ChatBlockData {
  return { text, toolUseId, type: "tool_result", ...extra } as ChatBlockData;
}

function text(t: string): ChatBlockData {
  return { type: "text", text: t } as ChatBlockData;
}

describe("buildRenderItems", () => {
  it("合并连续 text block,并把 tool_use/tool_result 按 toolUseId 配对成单个 tool item", () => {
    const items = buildRenderItems({
      messageId: 5,
      blocks: [
        text("Hello "),
        text("world"),
        toolUse("toolu-1"),
        toolResult("toolu-1", "paired"),
      ],
    });

    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({ type: "text", text: "Hello world" });
    expect(items[1]).toMatchObject({
      type: "tool",
      toolBlock: { toolUseId: "toolu-1" },
      resultBlock: { text: "paired" },
    });
    // uiStateKey 字面量:格式 message:${id}:${type}:${identity},identity 优先 toolUseId。
    // TranscriptUIStateContext 里所有已展开卡片的状态都挂在这个键上,字节级不能漂。
    expect(items[1].uiStateKey).toBe("message:5:tool:tool:toolu-1");
  });

  it("tool_use 在 persisted blocks、tool_result 在 liveBlocks 时仍配对到同一 item", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [toolUse("toolu-x")],
      liveBlocks: [toolResult("toolu-x", "late result")],
    });

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      type: "tool",
      toolBlock: { toolUseId: "toolu-x" },
      resultBlock: { text: "late result" },
    });
  });

  it("匿名 tool(无 toolUseId)按 LIFO 配对,孤儿 tool_result 直接丢弃", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [
        toolUse(undefined, "Bash"),
        toolUse(undefined, "Read"),
        toolResult(undefined, "lifo-paired"),
        toolResult("toolu-orphan", "orphan"),
      ],
    });

    expect(items).toHaveLength(2);
    // LIFO:匿名 result 配给最后一个匿名 tool_use,第一个保持未配对。
    expect(items[0].type === "tool" && items[0].resultBlock).toBeUndefined();
    expect(items[1]).toMatchObject({
      type: "tool",
      toolBlock: { toolName: "Read" },
      resultBlock: { text: "lifo-paired" },
    });
    // 孤儿 result 不产生幽灵 tool 卡。
    expect(
      items.some(
        (item) => item.type === "tool" && item.resultBlock?.text === "orphan",
      ),
    ).toBe(false);
  });

  it("AskUserQuestion / ExitPlanMode 的 tool_use 与对应 tool_result 双双跳过", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [
        toolUse("toolu-ask", "AskUserQuestion"),
        toolResult("toolu-ask", "answer"),
        toolUse("toolu-plan", "ExitPlanMode"),
        toolResult("toolu-plan", "approved"),
      ],
    });

    expect(items).toHaveLength(0);
  });

  it("resolved+allowed 审批被后续同名 tool_use FIFO 消费;denied 保留为独立卡", () => {
    const allowedPerm = {
      type: "tool_permission_request",
      toolPermission: {
        allowed: true,
        requestId: "req-allowed",
        resolved: true,
        toolName: "Bash",
      },
    } as unknown as ChatBlockData;
    const deniedPerm = {
      type: "tool_permission_request",
      toolPermission: {
        allowed: false,
        requestId: "req-denied",
        resolved: true,
        toolName: "Bash",
      },
    } as unknown as ChatBlockData;

    const items = buildRenderItems({
      messageId: 5,
      blocks: [allowedPerm, deniedPerm, toolUse("toolu-9", "Bash")],
    });

    expect(items).toHaveLength(2);
    expect(items[0]).toMatchObject({
      type: "tool_permission_request",
      block: { toolPermission: { requestId: "req-denied" } },
    });
    expect(items[0].uiStateKey).toBe(
      "message:5:permission:permission:req-denied",
    );
    expect(items[1]).toMatchObject({
      type: "tool",
      permissionBlock: { toolPermission: { requestId: "req-allowed" } },
    });
  });

  it("tool_approval block 产出一个 tool_approval 渲染项", () => {
    const approvalBlock = {
      type: "tool_approval",
      toolApproval: {
        toolKey: "org",
        requestId: "org-1",
        toolName: "org_create_department",
        toolInput: { name: "研发部" },
        status: "pending",
      },
    } as unknown as ChatBlockData;

    const items = buildRenderItems({
      messageId: 8,
      blocks: [approvalBlock],
    });

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      type: "tool_approval",
      block: { toolApproval: { requestId: "org-1", status: "pending" } },
    });
  });

  it("agent.spawn 归集 parentToolUseId 子块到 childBlocks,子块不再上顶层", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [
        toolUse("toolu-parent", "Agent", {
          canonical: { kind: "agent.spawn" },
        } as unknown as Partial<ChatBlockData>),
        toolUse("toolu-child", "Bash", { parentToolUseId: "toolu-parent" }),
        toolResult("toolu-child", "hello", { parentToolUseId: "toolu-parent" }),
        toolResult("toolu-parent", "Raw output"),
      ],
    });

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      type: "tool",
      toolBlock: { toolUseId: "toolu-parent" },
      resultBlock: { text: "Raw output" },
    });
    expect(items[0].type === "tool" ? items[0].childBlocks : []).toHaveLength(
      2,
    );
  });

  it("合成顺序:persisted → liveThinking → liveBlocks → liveTail;thinking 的 streaming 只在无后续输出时为 true", () => {
    // case A:liveBlocks 已有 tool → 思考已结束 (streaming=false),且 thinking 排在 tool 前。
    const withTool = buildRenderItems({
      messageId: 1,
      blocks: [text("done part")],
      liveThinking: "reasoning…",
      liveBlocks: [toolUse("toolu-live")],
    });
    expect(withTool.map((item) => item.type)).toEqual([
      "text",
      "thinking",
      "tool",
    ]);
    expect(withTool[1]).toMatchObject({ type: "thinking", streaming: false });

    // case B:纯思考阶段 → streaming=true。
    const thinkingOnly = buildRenderItems({
      messageId: 1,
      liveThinking: "reasoning…",
      liveThinkingStartedAt: 1234,
    });
    expect(thinkingOnly).toHaveLength(1);
    expect(thinkingOnly[0]).toMatchObject({
      type: "thinking",
      streaming: true,
      startedAt: 1234,
    });
  });

  it("liveTail 与前面已冻结的 text 段合并为同一 item 并整体标记 streaming", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [text("abc")],
      liveTail: "def",
    });

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      type: "text",
      text: "abcdef",
      streaming: true,
    });
  });

  it("无身份的 item(如 thinking)uiStateKey 回退到 visible 下标", () => {
    const items = buildRenderItems({
      messageId: 5,
      blocks: [{ type: "thinking", text: "chain" } as ChatBlockData],
    });

    expect(items).toHaveLength(1);
    expect(items[0].uiStateKey).toBe("message:5:thinking:0");
  });

  it("plan.update 只有 actionable(带 actions)才渲染,纯进度块丢弃", () => {
    const actionable = {
      type: "plan",
      canonical: {
        kind: "plan.update",
        planUpdate: { actions: [{ kind: "approve" }], steps: [], text: "" },
      },
    } as unknown as ChatBlockData;
    const progressOnly = {
      type: "plan",
      canonical: {
        kind: "plan.update",
        planUpdate: { actions: [], steps: [{ title: "step" }], text: "" },
      },
    } as unknown as ChatBlockData;

    const items = buildRenderItems({
      messageId: 1,
      blocks: [actionable, progressOnly],
    });

    expect(items).toHaveLength(1);
    expect(items[0].type).toBe("plan");
  });

  it("ask_user_question / compact_boundary / unknown / 空 text 的入列行为", () => {
    const items = buildRenderItems({
      messageId: 1,
      blocks: [
        text(""),
        { type: "ask_user_question" } as ChatBlockData,
        { type: "compact_boundary" } as ChatBlockData,
        { type: "mystery" } as ChatBlockData,
      ],
    });

    expect(items.map((item) => item.type)).toEqual([
      "tool",
      "compact_boundary",
      "unknown",
    ]);
  });
});

function message(
  id: number,
  role: "user" | "assistant",
  blocks: ChatBlockData[],
): chat_svc.ChatMessage {
  return {
    blocks,
    completionTokens: 0,
    createtime: 0,
    durationMs: 0,
    errorText: "",
    id,
    model: "",
    promptTokens: 0,
    role,
    seq: id,
    sessionId: 1,
  } as chat_svc.ChatMessage;
}

describe("buildTranscriptRows", () => {
  it("把 displayMessages 平铺成行:每个 RenderItem 一行,首/末行标志与 messageId 正确", () => {
    const { rows, firstRowIndexByMessageId, rowIndexByKey } =
      buildTranscriptRows({
        displayMessages: [
          message(1, "user", [text("hi")]),
          message(2, "assistant", [
            text("reply"),
            toolUse("toolu-1"),
            toolResult("toolu-1"),
            toolUse("toolu-2"),
          ]),
        ],
        autonomousIds: new Set(),
      });

    expect(
      rows.map((r) => [
        r.messageId,
        r.item.type,
        r.isFirstOfMessage,
        r.isLastOfMessage,
      ]),
    ).toEqual([
      [1, "text", true, true],
      [2, "text", true, false],
      [2, "tool", false, false],
      [2, "tool", false, true],
    ]);
    expect(firstRowIndexByMessageId.get(1)).toBe(0);
    expect(firstRowIndexByMessageId.get(2)).toBe(1);
    for (const [idx, row] of rows.entries()) {
      expect(rowIndexByKey.get(row.key)).toBe(idx);
    }
    // 行 key 全局唯一。
    expect(new Set(rows.map((r) => r.key)).size).toBe(rows.length);
  });

  it("autonomous 标志只落在自主续轮消息的行上", () => {
    const { rows } = buildTranscriptRows({
      displayMessages: [
        message(1, "user", [text("bg task")]),
        message(2, "assistant", [text("started")]),
        message(3, "assistant", [text("done")]),
      ],
      autonomousIds: new Set([3]),
    });

    expect(rows.map((r) => [r.messageId, r.autonomous])).toEqual([
      [1, false],
      [2, false],
      [3, true],
    ]);
  });

  it("空 blocks(或全部被 skip)的消息发射一个 placeholder 行", () => {
    const { rows } = buildTranscriptRows({
      displayMessages: [
        message(2, "assistant", []),
        // 全部被 skip:AskUserQuestion 的 tool_use + result。
        message(3, "assistant", [
          toolUse("toolu-ask", "AskUserQuestion"),
          toolResult("toolu-ask"),
        ]),
      ],
      autonomousIds: new Set(),
    });

    expect(
      rows.map((r) => [
        r.messageId,
        r.item.type,
        r.isFirstOfMessage,
        r.isLastOfMessage,
      ]),
    ).toEqual([
      [2, "placeholder", true, true],
      [3, "placeholder", true, true],
    ]);
  });

  it("live 数据只注入 liveTargetId 那条消息", () => {
    const { rows } = buildTranscriptRows({
      displayMessages: [
        message(1, "assistant", [text("old")]),
        message(2, "assistant", []),
      ],
      autonomousIds: new Set(),
      liveTargetId: 2,
      liveTail: "growing",
    });

    expect(rows.map((r) => [r.messageId, r.item.type])).toEqual([
      [1, "text"],
      [2, "text"],
    ]);
    expect(rows[0].item).toMatchObject({ text: "old" });
    expect(rows[1].item).toMatchObject({ text: "growing", streaming: true });
  });

  it("key 稳定性快照:同一内容的流式形态与落库形态产出逐项相等的行 key 序列", () => {
    // 流式形态:persisted 空,冻结块在 liveBlocks,尾巴在 liveTail。
    const liveForm = buildTranscriptRows({
      displayMessages: [message(2, "assistant", [])],
      autonomousIds: new Set(),
      liveTargetId: 2,
      liveBlocks: [
        text("frozen intro"),
        toolUse("toolu-1"),
        toolResult("toolu-1"),
      ],
      liveTail: "tail text",
    });
    // 落库形态:同样内容全部进 persisted blocks。
    const persistedForm = buildTranscriptRows({
      displayMessages: [
        message(2, "assistant", [
          text("frozen intro"),
          toolUse("toolu-1"),
          toolResult("toolu-1"),
          text("tail text"),
        ]),
      ],
      autonomousIds: new Set(),
    });

    expect(liveForm.rows.map((r) => r.key)).toEqual(
      persistedForm.rows.map((r) => r.key),
    );
  });

  it("流式推进 append-only:liveBlocks 追加 tool 后旧行 key 不变,只在尾部增行", () => {
    const base = {
      displayMessages: [message(2, "assistant", [])],
      autonomousIds: new Set<number>(),
      liveTargetId: 2,
    };
    const before = buildTranscriptRows({
      ...base,
      liveBlocks: [text("intro"), toolUse("toolu-1")],
    });
    const after = buildTranscriptRows({
      ...base,
      liveBlocks: [
        text("intro"),
        toolUse("toolu-1"),
        toolResult("toolu-1"),
        toolUse("toolu-2"),
      ],
    });

    const beforeKeys = before.rows.map((r) => r.key);
    const afterKeys = after.rows.map((r) => r.key);
    expect(afterKeys.slice(0, beforeKeys.length)).toEqual(beforeKeys);
    expect(afterKeys.length).toBe(beforeKeys.length + 1);
  });

  it("WeakMap 缓存:非 live 消息同对象重建返回同一行数组引用,live 消息绕过缓存", () => {
    const cache = new WeakMap<chat_svc.ChatMessage, TranscriptRow[]>();
    const persisted = message(1, "assistant", [text("stable")]);
    const live = message(2, "assistant", []);
    const args = {
      displayMessages: [persisted, live],
      autonomousIds: new Set<number>(),
      liveTargetId: 2,
      liveTail: "grow",
      cache,
    };

    const first = buildTranscriptRows(args);
    const second = buildTranscriptRows(args);

    // persisted 消息:两次构建产出同一 row 对象(=== 引用)→ 行组件 React.memo 恒命中。
    expect(second.rows[0]).toBe(first.rows[0]);
    // live 消息:每次现场重建,不进缓存。
    expect(second.rows[1]).not.toBe(first.rows[1]);
  });
});
