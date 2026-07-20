import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  TranscriptCard,
  TranscriptCardBody,
  TranscriptCardHeader,
  TranscriptPill,
} from "../transcript-card";

describe("TranscriptCard", () => {
  it("默认 tone 用中性边框 + measure 栏宽 + 无阴影", () => {
    render(<TranscriptCard data-testid="c">x</TranscriptCard>);
    const el = screen.getByTestId("c");
    expect(el.className).toContain("max-w-measure");
    expect(el.className).toContain("rounded-lg");
    expect(el.className).toContain("bg-card");
    expect(el.className).toContain("border-border");
    expect(el.className).not.toContain("shadow");
  });

  it("error tone 换成告警边框", () => {
    render(
      <TranscriptCard tone="error" data-testid="c">
        x
      </TranscriptCard>,
    );
    const el = screen.getByTestId("c");
    expect(el.className).toContain("border-status-error/40");
    expect(el.className).not.toContain("border-border");
  });

  it("调用方 className 能覆盖默认值", () => {
    render(
      <TranscriptCard className="rounded-none" data-testid="c">
        x
      </TranscriptCard>,
    );
    expect(screen.getByTestId("c").className).toContain("rounded-none");
    expect(screen.getByTestId("c").className).not.toContain("rounded-lg");
  });

  it("header 与 body 用同一套水平内边距", () => {
    render(
      <TranscriptCard>
        <TranscriptCardHeader data-testid="h">h</TranscriptCardHeader>
        <TranscriptCardBody data-testid="b">b</TranscriptCardBody>
      </TranscriptCard>,
    );
    expect(screen.getByTestId("h").className).toContain("px-3.5");
    expect(screen.getByTestId("b").className).toContain("px-3.5");
    expect(screen.getByTestId("b").className).toContain("border-t");
  });

  it("pill 用 text-meta,不再是 9px", () => {
    render(<TranscriptPill data-testid="p">完成</TranscriptPill>);
    const el = screen.getByTestId("p");
    expect(el.className).toContain("text-meta");
    expect(el.className).not.toMatch(/text-\[\d+px\]/);
  });

  it("done tone 的 pill 用 running 配色", () => {
    render(
      <TranscriptPill tone="done" data-testid="p">
        完成
      </TranscriptPill>,
    );
    expect(screen.getByTestId("p").className).toContain("bg-status-running-bg");
    expect(screen.getByTestId("p").className).toContain("text-status-running");
  });
});
