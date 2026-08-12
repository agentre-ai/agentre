import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ChatBlockData } from "@/stores/chat-streams-store";

import type { ActivityStep, ActivitySummary } from "../transcript-rows";
import { TranscriptUIStateProvider } from "../transcript-ui-state";

import { ActivityBlock } from "./block";

// 活动块组件测试。组头汇总 / 三档字重 / 行内就地展开体 / 失败行 / 运行态自动展开
// 都在这里钉住 —— 判据本身(tier / toolCategory / summary)由上游纯函数负责,
// 这里只验「渲染成了什么、点开看得到什么、折叠时没 mount 什么」。

function toolStep(
  key: string,
  toolBlock: Partial<ChatBlockData>,
  resultBlock?: Partial<ChatBlockData>,
): ActivityStep {
  return {
    resultBlock: resultBlock
      ? ({ type: "tool_result", ...resultBlock } as ChatBlockData)
      : undefined,
    toolBlock: { type: "tool_use", ...toolBlock } as ChatBlockData,
    type: "tool",
    uiStateKey: key,
  };
}

function thinkingStep(key: string, text: string): ActivityStep {
  return {
    block: { text, type: "thinking" } as ChatBlockData,
    streaming: false,
    type: "thinking",
    uiStateKey: key,
  };
}

// 读层的判据是 input shape(tier() 的 path / pattern / query / url),不是工具名。
const readStep = toolStep(
  "message:1:tool:tool:read-1",
  { toolInput: { path: "/repo/chat.go" }, toolName: "Read" },
  { text: "package chat\nfunc A() {}\nfunc B() {}" },
);

const commandStep = toolStep(
  "message:1:tool:tool:bash-1",
  { toolInput: { command: "pnpm test" }, toolName: "Bash" },
  { text: '{"exitCode":0,"output":"13 passed"}' },
);

const mcpStep = toolStep(
  "message:1:tool:tool:mcp-1",
  {
    toolInput: { commands: [{ op: "set_node" }] },
    toolName: "mcp__pencil__execute",
  },
  { text: '{"ok":true}' },
);

const failedStep = toolStep(
  "message:1:tool:tool:bash-2",
  { toolInput: { command: "pnpm lint" }, toolName: "Bash" },
  { isError: true, text: '{"exitCode":1,"output":"boom"}' },
);

function summaryOf(partial: Partial<ActivitySummary> = {}): ActivitySummary {
  return {
    failures: 0,
    parts: [],
    steps: 0,
    truncated: false,
    ...partial,
  };
}

function renderBlock(props: Partial<Parameters<typeof ActivityBlock>[0]> = {}) {
  const steps = props.steps ?? [readStep, commandStep];
  return render(
    <TranscriptUIStateProvider>
      <ActivityBlock
        steps={steps}
        summary={props.summary ?? summaryOf({ steps: steps.length })}
        uiStateKey={props.uiStateKey ?? "message:1:activity:tool:read-1"}
        {...props}
      />
    </TranscriptUIStateProvider>,
  );
}

describe("ActivityBlock 组头(折叠态)", () => {
  it("Given 一组已落定的活动, When 折叠渲染, Then 组头报出步数、固定顺序的汇总与红色失败计数", () => {
    renderBlock({
      steps: [
        thinkingStep("k-think", "hmm"),
        readStep,
        commandStep,
        failedStep,
      ],
      summary: summaryOf({
        failures: 1,
        parts: [
          { category: "thinking", count: 2 },
          { category: "read", count: 8 },
          { category: "edit", count: 1, files: 1, minus: 4, plus: 18 },
          { category: "command", count: 1 },
        ],
        steps: 10,
        truncated: true,
      }),
    });

    const header = screen.getByTestId("activity-header");
    expect(header.tagName).toBe("BUTTON");
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(within(header).getByText("10 steps")).toBeInTheDocument();
    expect(within(header).getByText(/Thinking 2/)).toBeInTheDocument();
    expect(within(header).getByText(/Read 8/)).toBeInTheDocument();
    // 写操作报对象规模:改了几个文件 + 增删行,而不是只报一个步数。
    expect(within(header).getByText(/1 file/)).toBeInTheDocument();
    expect(within(header).getByText("+18")).toBeInTheDocument();
    expect(within(header).getByText("−4")).toBeInTheDocument();
    // 截断省略号在,失败计数在省略号之后、且用 status-error 着色。
    expect(within(header).getByText("…")).toBeInTheDocument();
    const failures = within(header).getByTestId("activity-failures");
    expect(failures).toHaveTextContent("1 failed");
    expect(failures.className).toContain("text-status-error");
    // 失败计数不在会被裁掉的汇总段里 —— 组头挤不下时先裁类目,红标永远在。
    expect(screen.getByTestId("activity-summary").contains(failures)).toBe(
      false,
    );
  });

  it("Given 折叠态, Then 组内步骤的结果文本不进 DOM(行级虚拟化/懒挂载不回归)", () => {
    renderBlock({ steps: [readStep, commandStep] });

    expect(screen.queryByTestId("activity-row")).toBeNull();
    expect(screen.queryByText(/func A/)).toBeNull();
    expect(screen.queryByText(/13 passed/)).toBeNull();
  });

  it("Given 组头, When 点开再点收, Then aria-expanded 跟随并展开/收起时间轴", () => {
    renderBlock({ steps: [readStep, commandStep] });
    const header = screen.getByTestId("activity-header");

    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByTestId("activity-row")).toHaveLength(2);

    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("activity-row")).toBeNull();
  });
});

describe("ActivityBlock 活动行(展开态)", () => {
  it("Given 展开的时间轴, Then 读 / 中性 / 写三档以字重与前景色区分,且 MCP 名字拆成 server · tool", () => {
    renderBlock({ steps: [readStep, mcpStep, commandStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const rows = screen.getAllByTestId("activity-row");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveAttribute("data-weight", "read");
    expect(rows[1]).toHaveAttribute("data-weight", "neutral");
    expect(rows[2]).toHaveAttribute("data-weight", "write");

    const name = (row: HTMLElement) => within(row).getByTestId("activity-name");
    expect(name(rows[0]).className).toContain("text-muted-foreground");
    expect(name(rows[1]).className).toContain("font-medium");
    expect(name(rows[2]).className).toContain("font-semibold");
    expect(name(rows[1])).toHaveTextContent("pencil · execute");
  });

  it("Given 一行活动, When 点开, Then 就地出现参数与结果两段", () => {
    renderBlock({ steps: [readStep, commandStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const row = screen.getAllByTestId("activity-row")[0];
    expect(row).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(row);

    expect(row).toHaveAttribute("aria-expanded", "true");
    const body = screen.getByTestId("activity-row-body");
    expect(within(body).getByText("path")).toBeInTheDocument();
    expect(within(body).getByText("/repo/chat.go")).toBeInTheDocument();
    expect(within(body).getByText(/func A/)).toBeInTheDocument();
  });

  it("Given 一步 file.edit, When 点开, Then 展开体是既有 diff 渲染而不是参数 JSON", () => {
    const edit = toolStep(
      "message:1:tool:tool:edit-1",
      {
        canonical: {
          fileEdit: {
            files: [
              {
                hunks: [
                  {
                    lines: [
                      { new: 1, op: "+", text: "const next = 1;" },
                      { old: 1, op: "-", text: "const prev = 0;" },
                    ],
                    newLines: 1,
                    newStart: 1,
                    oldLines: 1,
                    oldStart: 1,
                  },
                ],
                kind: "modified",
                minus: 1,
                path: "/repo/a.ts",
                plus: 1,
              },
            ],
          },
          kind: "file.edit",
        } as unknown as ChatBlockData["canonical"],
        toolInput: { new_string: "const next = 1;", old_string: "x" },
        toolName: "Edit",
      },
      { text: "ok" },
    );
    renderBlock({ steps: [edit, readStep] });
    fireEvent.click(screen.getByTestId("activity-header"));
    fireEvent.click(screen.getAllByTestId("activity-row")[0]);

    const body = screen.getByTestId("activity-row-body");
    expect(within(body).getByTestId("file-edit-diff-scroll")).toBeDefined();
    expect(within(body).getByText("const next = 1;")).toBeInTheDocument();
  });

  it("Given 一步 file.write, When 点开, Then 展开体是文件内容", () => {
    const write = toolStep(
      "message:1:tool:tool:write-1",
      {
        canonical: {
          fileWrite: {
            bytes: 24,
            content: "line one\nline two",
            lines: 2,
            path: "/repo/new.ts",
          },
          kind: "file.write",
        } as unknown as ChatBlockData["canonical"],
        toolInput: { content: "line one\nline two", file_path: "/repo/new.ts" },
        toolName: "Write",
      },
      { text: "ok" },
    );
    renderBlock({ steps: [write, readStep] });
    fireEvent.click(screen.getByTestId("activity-header"));
    fireEvent.click(screen.getAllByTestId("activity-row")[0]);

    const body = screen.getByTestId("activity-row-body");
    expect(within(body).getByTestId("activity-file-write")).toHaveTextContent(
      "line two",
    );
  });

  it("Given 一步失败, Then 该行红色并带 exit N, 但默认不展开", () => {
    renderBlock({ steps: [readStep, failedStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const failed = screen.getAllByTestId("activity-row")[1];
    expect(failed).toHaveAttribute("aria-expanded", "false");
    expect(failed).toHaveAttribute("data-failed", "true");
    expect(within(failed).getByTestId("activity-name").className).toContain(
      "text-status-error",
    );
    expect(within(failed).getByText("exit 1")).toBeInTheDocument();
    expect(screen.queryByText("boom")).toBeNull();
  });

  it("Given 一步思考, When 点开, Then 展开体是思考正文", () => {
    renderBlock({
      steps: [thinkingStep("k-think", "check the store"), readStep],
    });
    fireEvent.click(screen.getByTestId("activity-header"));

    const row = screen.getAllByTestId("activity-row")[0];
    expect(screen.queryByText("check the store")).toBeNull();
    fireEvent.click(row);
    expect(screen.getByText("check the store")).toBeInTheDocument();
  });
});

describe("ActivityBlock 运行态", () => {
  it("Given 这一组正在跑, Then 自动展开并在组头播报当前这一步", () => {
    renderBlock({
      running: true,
      steps: [
        readStep,
        toolStep("k-live", {
          toolInput: { path: "/repo/live.ts" },
          toolName: "Read",
        }),
      ],
    });

    const header = screen.getByTestId("activity-header");
    expect(header).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByTestId("activity-row")).toHaveLength(2);
    const tail = screen.getByTestId("activity-live-tail");
    expect(tail).toHaveAttribute("aria-live", "polite");
    expect(tail).toHaveTextContent("/repo/live.ts");
    // 运行中不报耗时(轮次没结束,数字还没有意义)。
    expect(screen.queryByTestId("activity-duration")).toBeNull();
  });

  it("Given 调用方给了默认展开(子代理内部按步数阈值), Then 落定态也展开,用户仍可收起", () => {
    renderBlock({ defaultExpanded: true });

    const header = screen.getByTestId("activity-header");
    expect(header).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(header);
    expect(header).toHaveAttribute("aria-expanded", "false");
  });

  it("Given 轮次结束, Then 自动收起", () => {
    const { rerender } = renderBlock({ running: true });
    expect(screen.getByTestId("activity-header")).toHaveAttribute(
      "aria-expanded",
      "true",
    );

    rerender(
      <TranscriptUIStateProvider>
        <ActivityBlock
          steps={[readStep, commandStep]}
          summary={summaryOf({ steps: 2 })}
          uiStateKey="message:1:activity:tool:read-1"
          durationMs={6600}
        />
      </TranscriptUIStateProvider>,
    );

    expect(screen.getByTestId("activity-header")).toHaveAttribute(
      "aria-expanded",
      "false",
    );
    expect(screen.getByTestId("activity-duration")).toHaveTextContent("6.6s");
  });

  it("Given 用户在运行中手动收起, Then 轮次结束后仍按用户的选择", () => {
    const { rerender } = renderBlock({ running: true });
    fireEvent.click(screen.getByTestId("activity-header"));
    expect(screen.getByTestId("activity-header")).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    rerender(
      <TranscriptUIStateProvider>
        <ActivityBlock
          steps={[readStep, commandStep]}
          summary={summaryOf({ steps: 2 })}
          uiStateKey="message:1:activity:tool:read-1"
        />
      </TranscriptUIStateProvider>,
    );
    expect(screen.getByTestId("activity-header")).toHaveAttribute(
      "aria-expanded",
      "false",
    );

    // 反向:落定后用户点开,保持展开(自动收起只作用于没被碰过的块)。
    fireEvent.click(screen.getByTestId("activity-header"));
    expect(screen.getByTestId("activity-header")).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });
});
