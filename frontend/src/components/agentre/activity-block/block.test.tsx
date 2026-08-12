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

// subagent sidecar 是 wails 生成的 class 类型(带 convertValues),测试只用其中
// 两个字段,按纯数据对象构造后 cast —— 与 store 里同一手法。
function subagentState(status: string, taskId: string) {
  return { status, taskId } as unknown as ChatBlockData["subagent"];
}

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

  // 单条不成组:一段活动只有一步时不套「1 步」的壳,直接渲染那一行活动行。
  it("Given 一段活动只有一步, Then 不出组头,直接就是那一行活动行", () => {
    renderBlock({ steps: [readStep], summary: summaryOf({ steps: 1 }) });

    expect(screen.queryByTestId("activity-header")).toBeNull();
    const rows = screen.getAllByTestId("activity-row");
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getByTestId("activity-name")).toHaveTextContent(
      "Read",
    );
    // 那一行仍是就地可展开的:折叠不等于信息丢失。
    expect(rows[0]).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(rows[0]);
    const body = screen.getByTestId("activity-row-body");
    expect(within(body).getByText("path")).toBeInTheDocument();
    expect(within(body).getByText(/func A/)).toBeInTheDocument();
  });

  it("Given 单步且这一组正在跑, Then 仍是那一行活动行(没有壳可展开)", () => {
    renderBlock({
      running: true,
      steps: [readStep],
      summary: summaryOf({ steps: 1 }),
    });

    expect(screen.queryByTestId("activity-header")).toBeNull();
    expect(screen.getAllByTestId("activity-row")).toHaveLength(1);
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

    const writeBody = within(
      screen.getByTestId("activity-row-body"),
    ).getByTestId("activity-file-write");
    // 「既有的文件内容渲染」= FileWriteCard 今天的带行号内容区,不是一段裸代码块。
    const content = within(writeBody).getByTestId("file-write-content-scroll");
    expect(content).toHaveTextContent("line two");
    expect(within(content).getByText("2")).toBeInTheDocument();
  });

  it("Given 一步 file.write 内容被截断, When 点开, Then 截断条与「复制完整内容」都在", () => {
    const write = toolStep(
      "message:1:tool:tool:write-2",
      {
        canonical: {
          fileWrite: {
            bytes: 999_999,
            content: "kept one\nkept two",
            lines: 4200,
            path: "/repo/big.ts",
            truncated: true,
          },
          kind: "file.write",
        } as unknown as ChatBlockData["canonical"],
        toolInput: { content: "kept one\nkept two", file_path: "/repo/big.ts" },
        toolName: "Write",
      },
      { text: "ok" },
    );
    renderBlock({ steps: [write, readStep] });
    fireEvent.click(screen.getByTestId("activity-header"));
    fireEvent.click(screen.getAllByTestId("activity-row")[0]);

    // 折叠前看得到的东西展开后必须一样看得到:少了截断条,用户不知道自己
    // 读到的是被砍过的内容,也拿不到完整原文。
    const writeBody = within(
      screen.getByTestId("activity-row-body"),
    ).getByTestId("activity-file-write");
    expect(within(writeBody).getByText(/4200/)).toBeInTheDocument();
    expect(
      within(writeBody).getByRole("button", { name: /Copy full content/i }),
    ).toBeInTheDocument();
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

// 「没有标记 = 成功」只有在「还没成功的那些步」都带标记时才成立(spec 决策 10:
// 只有运行中 / 失败 / 待审批有标记)。没有结果的一步、以及结果只是启动 ACK 的
// 后台命令,都还没有成功 —— 它们不带标记就是把发生过的事藏了。
describe("ActivityBlock 未落定的一步", () => {
  const pendingStep = toolStep("message:1:tool:tool:bash-3", {
    toolInput: { command: "go build ./..." },
    toolName: "Bash",
  });

  const backgroundStep = toolStep(
    "message:1:tool:tool:bash-4",
    {
      subagent: subagentState("running", "bg_1"),
      toolInput: { command: "pnpm dev", run_in_background: true },
      toolName: "Bash",
    },
    { text: "Command running in background with ID: bg_1" },
  );

  it("Given 一步还没有结果, Then 该行带运行中标记而不是与成功步同形", () => {
    renderBlock({ steps: [readStep, pendingStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const rows = screen.getAllByTestId("activity-row");
    expect(within(rows[1]).getByTestId("activity-pending")).toHaveTextContent(
      "running",
    );
    // 落定的那一步仍然什么标记都没有。
    expect(within(rows[0]).queryByTestId("activity-pending")).toBeNull();
  });

  it("Given 一步是仍在后台跑的命令, Then 该行报后台运行与任务 id", () => {
    renderBlock({ steps: [readStep, backgroundStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const marker = within(screen.getAllByTestId("activity-row")[1]).getByTestId(
      "activity-pending",
    );
    expect(marker).toHaveTextContent("Background");
    expect(marker).toHaveTextContent("bg_1");
  });

  it("Given 后台命令连启动 ACK 都还没回, Then 只报一个标记(不叠运行中 + 后台)", () => {
    renderBlock({
      steps: [
        readStep,
        toolStep("message:1:tool:tool:bash-6", {
          subagent: subagentState("running", "bg_3"),
          toolInput: { command: "pnpm dev", run_in_background: true },
          toolName: "Bash",
        }),
      ],
    });
    fireEvent.click(screen.getByTestId("activity-header"));

    // getByTestId 本身就会在出现第二个标记时报错。
    const row = screen.getAllByTestId("activity-row")[1];
    expect(within(row).getByTestId("activity-pending")).toHaveTextContent(
      "running",
    );
  });

  it("Given 后台任务已经结束, Then 该行不再报后台运行", () => {
    renderBlock({
      steps: [
        readStep,
        toolStep(
          "message:1:tool:tool:bash-5",
          {
            subagent: subagentState("completed", "bg_2"),
            toolInput: { command: "pnpm build", run_in_background: true },
            toolName: "Bash",
          },
          { text: "Command running in background with ID: bg_2" },
        ),
      ],
    });
    fireEvent.click(screen.getByTestId("activity-header"));

    expect(
      within(screen.getAllByTestId("activity-row")[1]).queryByTestId(
        "activity-pending",
      ),
    ).toBeNull();
  });

  it("Given 调用方声明这一轮已终结, Then 没配到结果的一步报结果未知且不再转圈", () => {
    const { container } = renderBlock({
      pendingOutcome: "unknown",
      steps: [readStep, pendingStep],
    });
    fireEvent.click(screen.getByTestId("activity-header"));

    expect(
      within(screen.getAllByTestId("activity-row")[1]).getByTestId(
        "activity-pending",
      ),
    ).toHaveTextContent("unknown result");
    expect(container.querySelector(".animate-spin")).toBeNull();
  });

  it("Given 这一轮以失败终结, Then 没配到结果的一步按失败行呈现", () => {
    renderBlock({ pendingOutcome: "failed", steps: [readStep, pendingStep] });
    fireEvent.click(screen.getByTestId("activity-header"));

    const row = screen.getAllByTestId("activity-row")[1];
    expect(row).toHaveAttribute("data-failed", "true");
    expect(within(row).getByTestId("activity-name").className).toContain(
      "text-status-error",
    );
  });

  it("Given 单条不成组的一步还在跑, Then 那一行同样带运行中标记", () => {
    renderBlock({ steps: [pendingStep], summary: summaryOf({ steps: 1 }) });

    expect(screen.getByTestId("activity-pending")).toHaveTextContent("running");
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
