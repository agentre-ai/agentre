import { describe, expect, it } from "vitest";

import type { chat_svc } from "../../../../wailsjs/go/models";

import { computeComposerContextUsage } from "../chat-panel-context-usage";

type Msg = chat_svc.ChatMessage;

function asstMsg(partial: Partial<Msg> & { id: number }): Msg {
  return {
    blocks: [],
    cacheCreationTokens: 0,
    cachedTokens: 0,
    completionTokens: 0,
    createtime: 0,
    durationMs: 0,
    errorText: "",
    model: "",
    promptTokens: 0,
    reasoningTokens: 0,
    totalInputTokens: 0,
    role: "assistant",
    seq: partial.id,
    sessionId: 1,
    ...partial,
  } as Msg;
}

// Plan C 后:computeComposerContextUsage 不再做 family-specific 加法 —— runtime
// translator 在每条 ChatMessage/ChatStreamUsage 上自报 TotalInputTokens(已按
// family 聚合好)。前端直接读这个值。
describe("computeComposerContextUsage", () => {
  it("contextWindow 为 0 → 返回 undefined(前端不渲染上下文条)", () => {
    expect(
      computeComposerContextUsage(
        [asstMsg({ id: 1, totalInputTokens: 100 })],
        0,
      ),
    ).toBeUndefined();
  });

  it("liveUsage.totalInputTokens > 0 → 用 live 值,覆盖 messages 扫描", () => {
    const out = computeComposerContextUsage(
      [asstMsg({ id: 1, totalInputTokens: 31000 })],
      200000,
      { totalInputTokens: 51200 },
    );
    expect(out).toEqual({ used: 51200, max: 200000 });
  });

  it("liveUsage.totalInputTokens=0 → fallback 到 messages 扫描", () => {
    const out = computeComposerContextUsage(
      [asstMsg({ id: 1, totalInputTokens: 31000 })],
      200000,
      { totalInputTokens: 0 },
    );
    expect(out).toEqual({ used: 31000, max: 200000 });
  });

  it("从尾部往前找首条 totalInputTokens>0 的 assistant", () => {
    const out = computeComposerContextUsage(
      [
        asstMsg({ id: 1, totalInputTokens: 1000 }),
        { ...asstMsg({ id: 2 }), role: "user" } as Msg,
        asstMsg({ id: 3, totalInputTokens: 50000 }),
        asstMsg({ id: 4, totalInputTokens: 0 }), // 跳过
      ],
      200000,
    );
    expect(out?.used).toBe(50000);
  });

  it("没有任何带 token 的 assistant + 无 liveUsage → { used: 0, max } 占位", () => {
    const out = computeComposerContextUsage(
      [{ ...asstMsg({ id: 1 }), role: "user" } as Msg],
      200000,
    );
    expect(out).toEqual({ used: 0, max: 200000 });
  });

  it("liveUsage 缺省 → 等价于只读 messages", () => {
    const out = computeComposerContextUsage(
      [asstMsg({ id: 1, totalInputTokens: 50000 })],
      200000,
    );
    expect(out).toEqual({ used: 50000, max: 200000 });
  });
});
