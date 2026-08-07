import { render, screen } from "@testing-library/react";
import i18next from "i18next";
import { describe, expect, it, vi } from "vitest";

import enCommon from "@/i18n/locales/en/common.json";

// 用录制 spy 替掉 useTranslation,断言 notice 分支确实以 t("chat.notice.modelDeviation",
// { selected, actual }) 双参数调用 —— 证明文案走 i18n、模型 id 以插值参数流入而非烘死。
//
// mockT 本身转发给一个独立的、加载了真实 en/common.json 的 i18next 实例
// (不走 "@/i18n" 单例,因为那个单例依赖同样被本文件 mock 掉的 "react-i18next" 的
// initReactI18next 插件) —— 这样断言调用参数的旧用例不受影响,同时新增的可见文本
// 断言能拿到跟生产环境一致的真实翻译结果,而不是裸 key。
const realI18n = i18next.createInstance();
void realI18n.init({
  lng: "en",
  fallbackLng: "en",
  resources: { en: { common: enCommon } },
  defaultNS: "common",
  interpolation: { escapeValue: false },
});

const mockT = vi.fn((key: string, options?: Record<string, unknown>) =>
  realI18n.t(key, options),
);

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

  it("renders the visible sentence with a space between the model id and the following copy, matching the aria-label sentence", () => {
    const selected = "e2e-unbound-model";
    const actual = "e2e-fake-model";

    renderRow(noticeRow({ selectedModel: selected, actualModel: actual }));

    const expectedSentence = realI18n
      .t("chat.notice.modelDeviation.sentence", { selected, actual })
      .replace(/\s+/g, " ")
      .trim();

    const notice = screen.getByTestId("model-deviation-notice");
    const visibleText = (notice.textContent ?? "").replace(/\s+/g, " ").trim();

    // 回归:selected span 与后面的 "was not applied..." 文案之间少一个空格,
    // 导致渲染成 "...modelwas not applied..."。可见文本(空白归一化后)必须和
    // aria-label 已经拼好的 sentence 一致。
    expect(visibleText).toBe(expectedSentence);
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
