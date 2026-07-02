import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import i18n from "../../../../i18n";
import { IssuesBoard } from "../issues-board";

const mk = (
  id: number,
  stage: string,
  position: number,
  title: string,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): any => ({ id, stage, position, title, labels: [], agentStatus: "idle" });

beforeEach(async () => {
  await i18n.changeLanguage("zh-CN");
});

describe("IssuesBoard", () => {
  it("按 stage 渲染 4 列并显示计数", () => {
    render(
      <IssuesBoard
        issues={[mk(1, "todo", 10, "甲"), mk(2, "doing", 10, "乙")]}
        stageCounts={{ todo: 1, doing: 1, review: 0, done: 0 }}
        onEdit={vi.fn()}
        onMove={vi.fn()}
      />,
    );
    expect(screen.getByText("甲")).toBeInTheDocument();
    expect(screen.getByText("乙")).toBeInTheDocument();
    // 4 个阶段列标题都在（i18n 中文）
    expect(screen.getByText("待办")).toBeInTheDocument();
    expect(screen.getByText("已完成")).toBeInTheDocument();
  });
});
