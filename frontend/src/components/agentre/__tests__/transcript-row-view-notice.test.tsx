import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { TooltipProvider } from "@/components/ui/tooltip";
import i18n from "@/i18n";
import {
  TranscriptRenderContext,
  TranscriptRowView,
} from "../transcript-row-view";

import type { TranscriptRow } from "../transcript-rows";

// 供应商回退等持久 notice 走既有 NoticeBlock 渲染：Text 原样显示。
// （#26 的结构化模型偏离提示已随 model_override 整体移除，ChatBlock 不再有
// selectedModel / actualModel 字段，见 specs/2026-08-09-new-session-provider-select.md。）

type NoticeBlock = {
  text?: string;
  providerKey?: string;
};

function noticeRow(block: NoticeBlock): TranscriptRow {
  return {
    key: "message:9:notice:0",
    messageId: 9,
    message: {
      id: 9,
      role: "assistant",
      blocks: [{ type: "notice", ...block }],
      createtime: 0,
      durationMs: 0,
      model: "",
      promptTokens: 0,
      completionTokens: 0,
      cachedTokens: 0,
      cacheCreationTokens: 0,
      reasoningTokens: 0,
    },
    item: {
      type: "notice",
      uiStateKey: "message:9:notice:0",
      block: { type: "notice", ...block },
    },
    isFirstOfMessage: true,
    isLastOfMessage: true,
    autonomous: false,
  } as unknown as TranscriptRow;
}

function renderRow(row: TranscriptRow) {
  render(
    <TooltipProvider>
      <TranscriptRenderContext.Provider
        value={{ agentName: "Agentre", agentColor: "agent-1", sessionId: 42 }}
      >
        <TranscriptRowView
          row={row}
          liveTail=""
          liveBlocks={undefined}
          liveRetry={null}
          showIndicator={false}
          compacting={false}
          reconnecting={false}
        />
      </TranscriptRenderContext.Provider>
    </TooltipProvider>,
  );
}

describe("transcript notice block", () => {
  it("renders a notice text verbatim", () => {
    renderRow(
      noticeRow({
        text: "Selected provider X is unavailable; fell back to the agent binding",
      }),
    );

    expect(
      screen.getByText(
        "Selected provider X is unavailable; fell back to the agent binding",
      ),
    ).toBeDefined();
  });

  it("renders an empty-text notice as an empty status box (no crash)", () => {
    renderRow(noticeRow({ text: "" }));
    expect(screen.getByTestId("transcript-notice")).toBeInTheDocument();
  });

  it("renders a provider-fallback notice（结构化 providerKey）走 i18n 文案", () => {
    // 决策 8：会话所选供应商缺失/停用 → 回退 agent 绑定并追加持久 notice，
    // 文案「所选供应商 X 不可用，已回退 agent 绑定」必须显示，而不是空框。
    renderRow(noticeRow({ providerKey: "gone-provider" }));

    expect(
      screen.getByText(
        i18n.t("chat.notice.providerFallback.sentence", {
          provider: "gone-provider",
        }),
      ),
    ).toBeInTheDocument();
  });
});
