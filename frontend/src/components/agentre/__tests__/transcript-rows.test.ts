import { describe, expect, it } from "vitest";

import {
  buildRenderItems,
  buildTranscriptRows,
  estimateRowSize,
  estimateRowSizeWithSpacing,
  isLastRowOfMessage,
  ROW_END_PADDING_DELTA,
  ROW_MID_PADDING_DELTA,
  type TranscriptRow,
  type TranscriptRowItem,
} from "@/components/agentre/transcript-rows";
import type { ChatBlockData } from "@/stores/chat-streams-store";
import type { LocalCommandEntry } from "@/stores/local-commands-store";
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

// estimateRowSize:虚拟化 estimateSize 的估值表。这组测试是**分析性**校准的执行
// 断言,不是布局测试 —— jsdom 没有真实排版引擎,测不出这些数字是否等于组件实际
// 渲染高度(那部分只能靠 `make dev` 里的人工滚动观察验证)。这里只钉死两件事:
// ①估值确实按 Task 10 校准比例(25.5/22.75,对话流正文 14×1.625→15×1.7)从旧值
// 缩放而来,不是随手改的数;②每一档都严格大于重构前的旧值,防止"只改了兜底、
// 漏了某个 case"的回归 —— 那正是本任务存在的理由(虚拟化行高系统性偏小)。
describe("estimateRowSize", () => {
  const ROW_SIZE_SCALE = 25.5 / 22.75;

  function row(item: TranscriptRowItem): TranscriptRow {
    return {
      autonomous: false,
      isFirstOfMessage: true,
      isLastOfMessage: true,
      item,
      key: "k",
      messageId: 1,
    };
  }

  // [item.type, 重构前(旧字号 14/1.625)的估值] —— 旧值来自 git 历史
  // (transcript-rows.ts 改动前),用来断言"新值 = 旧值 × 比例"而非凭空数字。
  const PRE_CALIBRATION_BASELINE: [TranscriptRowItem["type"], number][] = [
    ["text", 132],
    ["placeholder", 132],
    ["image", 160],
    ["thinking", 40],
    ["compact_boundary", 48],
    ["local_command", 120],
    ["tool", 48],
  ];

  function makeItem(type: TranscriptRowItem["type"]): TranscriptRowItem {
    switch (type) {
      case "text":
        return { text: "hello", type: "text", uiStateKey: "k" };
      case "placeholder":
        return { type: "placeholder" };
      case "image":
        return { block: {} as ChatBlockData, type: "image", uiStateKey: "k" };
      case "thinking":
        return {
          block: {} as ChatBlockData,
          streaming: false,
          type: "thinking",
          uiStateKey: "k",
        };
      case "compact_boundary":
        return {
          block: {} as ChatBlockData,
          type: "compact_boundary",
          uiStateKey: "k",
        };
      case "local_command": {
        const entry: LocalCommandEntry = {
          command: "!ls",
          createdAt: 0,
          id: "cmd-1",
          output: "",
          sessionId: 1,
          status: "done",
        };
        return { entry, type: "local_command" };
      }
      default:
        // tool / plan / tool_permission_request / tool_approval / unknown 全部
        // 落进 estimateRowSize 的 default 分支,取 "tool" 代表整档。
        return { type: "tool", uiStateKey: "k" };
    }
  }

  it("每一档都等于旧值(重构前)按 25.5/22.75 的比例缩放并四舍五入", () => {
    for (const [type, before] of PRE_CALIBRATION_BASELINE) {
      const expected = Math.round(before * ROW_SIZE_SCALE);
      expect(estimateRowSize(row(makeItem(type)))).toBe(expected);
    }
  });

  it("新估值严格大于重构前的旧估值(防止漏改某个 case 导致的系统性偏小)", () => {
    for (const [type, before] of PRE_CALIBRATION_BASELINE) {
      expect(estimateRowSize(row(makeItem(type)))).toBeGreaterThan(before);
    }
  });

  it("text 与 placeholder 共享同一档估值(148),与兜底 chat.tsx:estimateSize 的 148 一致", () => {
    expect(estimateRowSize(row(makeItem("text")))).toBe(148);
    expect(estimateRowSize(row(makeItem("placeholder")))).toBe(148);
  });
});

// estimateRowSizeWithSpacing / isLastRowOfMessage:复审对 Task 10 提出的 Important
// 缺口 —— 上面的 estimateRowSize 只做了字号/间距的乘法缩放(ROW_SIZE_SCALE),没处理
// chat.tsx rowWrapperPad 的加法部分:消息末行 padding pb-5→pb-7(20→28px),消息内
// 分片行 padding pb-2→pb-2.5(8→10px)。这两档 padding 打在与 measureElement 同一个
// div 上(chat.tsx 注释「padding 打在行 wrapper 上,跟随 measureElement 一起计入行
// 高」),所以上面 estimateRowSize 表里的旧值本就隐含了旧 padding;纯乘法只把它放大到
// 20×SCALE≈22.4px / 8×SCALE≈8.97px,分别比新值少 ≈5.6px / ≈1px。
// estimateRowSizeWithSpacing 在 estimateRowSize 之上,按 isLastRowOfMessage(与
// chat.tsx:rowWrapperPad 共用同一份边界判断)补回这段差值。
describe("estimateRowSizeWithSpacing / isLastRowOfMessage", () => {
  const SCALE = 25.5 / 22.75;

  function makeRow(
    messageId: number,
    key: string,
    item: TranscriptRowItem = { text: "x", type: "text", uiStateKey: key },
  ): TranscriptRow {
    return {
      autonomous: false,
      isFirstOfMessage: false,
      isLastOfMessage: false,
      item,
      key,
      messageId,
    };
  }

  it("下一行不存在 → 视为消息末行", () => {
    const rows = [makeRow(1, "a")];
    expect(isLastRowOfMessage(rows, 0)).toBe(true);
  });

  it("下一行属于另一条消息 → 视为消息末行", () => {
    const rows = [makeRow(1, "a"), makeRow(2, "b")];
    expect(isLastRowOfMessage(rows, 0)).toBe(true);
  });

  it("下一行属于同一条消息 → 视为块内行,不是消息末行", () => {
    const rows = [makeRow(1, "a"), makeRow(1, "b")];
    expect(isLastRowOfMessage(rows, 0)).toBe(false);
  });

  it("间距增量常量 = 新 padding - 旧 padding×ROW_SIZE_SCALE:消息末行 ≈5.6px,块内行 ≈1px,末行显著大于块内行", () => {
    expect(ROW_END_PADDING_DELTA).toBeCloseTo(28 - 20 * SCALE, 5);
    expect(ROW_MID_PADDING_DELTA).toBeCloseTo(10 - 8 * SCALE, 5);
    expect(ROW_END_PADDING_DELTA).toBeGreaterThan(ROW_MID_PADDING_DELTA);
  });

  it("同类型行:消息末行估值 = estimateRowSize + ROW_END_PADDING_DELTA(四舍五入),块内行 = + ROW_MID_PADDING_DELTA", () => {
    const rows = [
      makeRow(1, "a", { text: "x", type: "text", uiStateKey: "a" }),
      makeRow(1, "b", { text: "y", type: "text", uiStateKey: "b" }),
    ];
    const base = estimateRowSize(rows[0]);

    expect(estimateRowSizeWithSpacing(rows, 0)).toBe(
      Math.round(base + ROW_MID_PADDING_DELTA),
    );
    expect(estimateRowSizeWithSpacing(rows, 1)).toBe(
      Math.round(base + ROW_END_PADDING_DELTA),
    );
  });

  it("间距增量只取决于是否消息末行,与 item 类型无关(类型差异已经在 estimateRowSize 里算过)", () => {
    const rows = [
      makeRow(1, "a", { type: "tool", uiStateKey: "a" }),
      makeRow(1, "b", {
        block: {} as ChatBlockData,
        streaming: false,
        type: "thinking",
        uiStateKey: "b",
      }),
    ];
    expect(estimateRowSizeWithSpacing(rows, 0)).toBe(
      Math.round(estimateRowSize(rows[0]) + ROW_MID_PADDING_DELTA),
    );
  });

  it("越界下标返回安全兜底值(不抛异常)", () => {
    expect(estimateRowSizeWithSpacing([], 0)).toBeGreaterThan(0);
  });
});
