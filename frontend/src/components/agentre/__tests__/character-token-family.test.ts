// §1.8 Token family addition — characterization PIN (post-Plan C)
// 参见 docs/superpowers/specs/2026-05-22-agentruntime-canonical-refactor-design.md §"TDD/BDD §1.8"。
//
// Plan C 前:前端按 backendType 做 family-specific 加法(Anthropic 系 = prompt
// +cached+cacheCreation, OpenAI 系 = prompt-only)。
// Plan C 后:runtime translator 在每条 ChatMessage / StreamUsage 上自报
// TotalInputTokens(已按 family 聚合好的「本次 API call 实际输入大小」),
// 前端 computeComposerContextUsage 直接读这个值。
//
// 本测试不再 pin 前端的家族公式 —— 而是 pin"前端按 TotalInputTokens 读上下文用量"
// 的新契约。原家族公式的 pin 迁到后端 translator 测试(runtimes/*/translator_test.go)。
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

describe("§1.8 Token family addition (Plan C contract PIN)", () => {
  const CTX = 100_000;

  it("liveUsage.totalInputTokens 优先 → 用 live 值", () => {
    const msg = asstMsg({ id: 1, totalInputTokens: 4000 });
    expect(
      computeComposerContextUsage([msg], CTX, { totalInputTokens: 6200 }),
    ).toEqual({ used: 6200, max: CTX });
  });

  it("无 liveUsage → fallback 到最近一条 assistant 的 totalInputTokens", () => {
    const msg = asstMsg({ id: 1, totalInputTokens: 6200 });
    expect(computeComposerContextUsage([msg], CTX)).toEqual({
      used: 6200,
      max: CTX,
    });
  });

  it("totalInputTokens=0 的 assistant 跳过,向更早的找", () => {
    const m1 = asstMsg({ id: 1, totalInputTokens: 6200 });
    const m2 = asstMsg({ id: 2, totalInputTokens: 0 });
    expect(computeComposerContextUsage([m1, m2], CTX)).toEqual({
      used: 6200,
      max: CTX,
    });
  });

  it("contextWindow=0 → undefined(整块隐藏)", () => {
    const msg = asstMsg({ id: 1, totalInputTokens: 100 });
    expect(computeComposerContextUsage([msg], 0)).toBeUndefined();
  });

  it("没有 assistant + 没有 liveUsage → { used: 0, max }(渲染 0/max 进度条)", () => {
    expect(computeComposerContextUsage([], CTX)).toEqual({ used: 0, max: CTX });
  });
});
