import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { WorkflowEditorForm } from "./workflow-editor-form";

const base = {
  name: "n",
  content: "c",
  tags: [] as string[],
  error: null,
  onNameChange: vi.fn(),
  onContentChange: vi.fn(),
  onTagsChange: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
});

// ── name / content / template / error ──────────────────────────────────────
describe("WorkflowEditorForm", () => {
  it("编辑名称回写", () => {
    const onNameChange = vi.fn();
    render(
      <WorkflowEditorForm {...base} name="" onNameChange={onNameChange} />,
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
        onContentChange={onContentChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    expect(onContentChange).toHaveBeenCalledWith(expect.stringMatching(/^#/));
  });

  it("非空正文插入模板:追加不覆盖", () => {
    const onContentChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        content="已有内容"
        onContentChange={onContentChange}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Insert template" }));
    const called = onContentChange.mock.calls[0][0] as string;
    expect(called.startsWith("已有内容")).toBe(true);
    expect(called.length).toBeGreaterThan("已有内容".length);
  });

  it("error 非空时渲染错误条", () => {
    render(<WorkflowEditorForm {...base} content="" error="boom" />);
    expect(screen.getByText("boom")).toBeTruthy();
  });
});

// ── tags ─────────────────────────────────────────────────────────────────
describe("WorkflowEditorForm tags", () => {
  it("回车添加标签 → onTagsChange 追加", () => {
    const onTagsChange = vi.fn();
    render(
      <WorkflowEditorForm
        {...base}
        tags={["通用"]}
        onTagsChange={onTagsChange}
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
        onTagsChange={onTagsChange}
      />,
    );
    fireEvent.click(screen.getByTestId("workflow-tag-remove-0"));
    expect(onTagsChange).toHaveBeenCalledWith(["新功能"]);
  });

  it("提示文案渲染:tags hint", () => {
    render(<WorkflowEditorForm {...base} />);
    const tagsHint = screen.getByTestId("workflow-tags-hint");
    expect(tagsHint.textContent).toBeTruthy();
  });
});

// ── DAG designer surface removed ────────────────────────────────────────────
describe("WorkflowEditorForm 不再渲染步骤(outline)/DAG 相关控件", () => {
  it("不渲染 outline 输入/上移/下移控件", () => {
    render(<WorkflowEditorForm {...base} />);
    expect(screen.queryByTestId("workflow-outline-input")).toBeNull();
    expect(
      screen.queryByTestId(/workflow-outline-move-(up|down)-0/),
    ).toBeNull();
    expect(screen.queryByTestId("workflow-outline-hint")).toBeNull();
  });
});
