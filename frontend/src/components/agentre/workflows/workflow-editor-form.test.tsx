import { fireEvent, render, screen } from "@testing-library/react";
import * as React from "react";
import { describe, expect, it, vi } from "vitest";

import { WorkflowEditorForm } from "./workflow-editor-form";

const base = {
  name: "n",
  content: "c",
  error: null,
  onNameChange: vi.fn(),
  onContentChange: vi.fn(),
};

// ── existing tests (name / content / template / error) ────────────────────
describe("WorkflowEditorForm", () => {
  it("编辑名称回写", () => {
    const onNameChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        name=""
        tags={[]}
        outline={[]}
        onNameChange={onNameChange}
        onTagsChange={vi.fn()}
        onOutlineChange={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "评审流程" },
    });
    expect(onNameChange).toHaveBeenCalledWith("评审流程");
  });

  it("空正文点插入模板:写入模板(以 # 开头)", () => {
    const onContentChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        content=""
        tags={[]}
        outline={[]}
        onContentChange={onContentChange}
        onTagsChange={vi.fn()}
        onOutlineChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    expect(onContentChange).toHaveBeenCalledWith(
      expect.stringMatching(/^#/),
    );
  });

  it("非空正文插入模板:追加不覆盖", () => {
    const onContentChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        content="已有内容"
        tags={[]}
        outline={[]}
        onContentChange={onContentChange}
        onTagsChange={vi.fn()}
        onOutlineChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    const called = onContentChange.mock.calls[0][0] as string;
    expect(called.startsWith("已有内容")).toBe(true);
    expect(called.length).toBeGreaterThan("已有内容".length);
  });

  it("error 非空时渲染错误条", () => {
    render(
      <WorkflowEditorForm
        name="x"
        content=""
        error="boom"
        tags={[]}
        outline={[]}
        onNameChange={() => {}}
        onContentChange={() => {}}
        onTagsChange={() => {}}
        onOutlineChange={() => {}}
      />,
    );
    expect(screen.getByText("boom")).toBeTruthy();
  });
});

// ── tags / outline (Task 7) ────────────────────────────────────────────────
describe("WorkflowEditorForm tags/outline", () => {
  it("回车添加标签 → onTagsChange 追加", () => {
    const onTagsChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={["通用"]}
        outline={[]}
        onTagsChange={onTagsChange}
        onOutlineChange={vi.fn()}
      />,
    );
    const input = screen.getByTestId("workflow-tags-input");
    fireEvent.change(input, { target: { value: "新功能" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onTagsChange).toHaveBeenCalledWith(["通用", "新功能"]);
  });

  it("点标签 × → onTagsChange 移除", () => {
    const onTagsChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={["通用", "新功能"]}
        outline={[]}
        onTagsChange={onTagsChange}
        onOutlineChange={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId("workflow-tag-remove-0"));
    expect(onTagsChange).toHaveBeenCalledWith(["新功能"]);
  });

  it("回车添加步骤 → onOutlineChange 追加", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["需求拆解"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    const input = screen.getByTestId("workflow-outline-input");
    fireEvent.change(input, { target: { value: "方案设计" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onOutlineChange).toHaveBeenCalledWith(["需求拆解", "方案设计"]);
  });

  it("删除步骤 → onOutlineChange 去掉该项", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["需求拆解", "方案设计"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    fireEvent.click(screen.getByTestId("workflow-outline-remove-0"));
    expect(onOutlineChange).toHaveBeenCalledWith(["方案设计"]);
  });

  it("上移步骤 → 交换位置", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["步骤一", "步骤二", "步骤三"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    // move index 1 up → should become [步骤二, 步骤一, 步骤三]
    fireEvent.click(screen.getByTestId("workflow-outline-move-up-1"));
    expect(onOutlineChange).toHaveBeenCalledWith(["步骤二", "步骤一", "步骤三"]);
  });

  it("下移步骤 → 交换位置", () => {
    const onOutlineChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={["步骤一", "步骤二", "步骤三"]}
        onTagsChange={vi.fn()}
        onOutlineChange={onOutlineChange}
      />,
    );
    // move index 1 down → should become [步骤一, 步骤三, 步骤二]
    fireEvent.click(screen.getByTestId("workflow-outline-move-down-1"));
    expect(onOutlineChange).toHaveBeenCalledWith(["步骤一", "步骤三", "步骤二"]);
  });

  it("提示文案渲染:tags hint 和 outline hint", () => {
    render(
      <WorkflowEditorForm
        {...base}
        tags={[]}
        outline={[]}
        onTagsChange={vi.fn()}
        onOutlineChange={vi.fn()}
      />,
    );
    expect(
      screen.getByText("For humans only — not sent to the AI"),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "A human-readable skeleton; add/remove/reorder. Display only — does not constrain the AI",
      ),
    ).toBeTruthy();
  });
});
