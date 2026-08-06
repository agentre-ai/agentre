import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// 用录制 spy 替掉 useTranslation,断言 notice 分支确实以 t("chat.notice.modelDeviation",
// { selected, actual }) 双参数调用 —— 证明文案走 i18n、模型 id 以插值参数流入而非烘死。
const mockT = vi.fn((key: string) => key);

vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: mockT }),
}));

import { TooltipProvider } from "@/components/ui/tooltip";
import {
  TranscriptRenderContext,
  TranscriptRowView,
} from "../transcript-row-view";

import type { TranscriptRow } from "../transcript-rows";

type NoticeBlock = {
  selectedModel?: string;
  actualModel?: string;
  text?: string;
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
  mockT.mockClear();
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

describe("transcript model-deviation notice", () => {
  it("renders a structured notice via t() with both model ids in monospace", () => {
    renderRow(
      noticeRow({
        selectedModel: "selected-model",
        actualModel: "actual-model",
      }),
    );

    // t() 以「双参数」调用 —— 文案 key + 两个模型 id 插值。
    expect(mockT).toHaveBeenCalledWith("chat.notice.modelDeviation.sentence", {
      selected: "selected-model",
      actual: "actual-model",
    });

    // 两个模型名各自以等宽字体渲染。
    const selectedEl = screen.getByText("selected-model");
    expect(selectedEl.className).toContain("font-mono");
    const actualEl = screen.getByText("actual-model");
    expect(actualEl.className).toContain("font-mono");
  });

  it("renders a legacy unstructured notice text verbatim", () => {
    renderRow(noticeRow({ text: "legacy notice text" }));

    expect(screen.getByText("legacy notice text")).toBeDefined();
    // 无结构化字段时不走模型偏离文案。
    expect(mockT).not.toHaveBeenCalledWith(
      "chat.notice.modelDeviation.sentence",
      expect.anything(),
    );
  });
});
