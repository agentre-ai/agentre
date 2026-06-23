import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";

import { RawToolCard } from "./card";
import type { ChatBlockData } from "@/stores/chat-streams-store";

const bashUse = (overrides: Partial<ChatBlockData> = {}): ChatBlockData =>
  ({
    type: "tool_use",
    toolName: "Bash",
    toolInput: { command: "ls -la" },
    ...overrides,
  }) as unknown as ChatBlockData;

const result = (overrides: Partial<ChatBlockData> = {}): ChatBlockData =>
  ({
    type: "tool_result",
    text: "hi\n",
    ...overrides,
  }) as unknown as ChatBlockData;

describe("RawToolCard header", () => {
  it("shows the tool name and a one-line summary", () => {
    render(<RawToolCard toolBlock={bashUse()} />);
    expect(screen.getByText("Bash")).toBeDefined();
    expect(screen.getByText(/ls -la/)).toBeDefined();
  });

  it("relativizes file paths against cwd", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolName: "Read",
          toolInput: { file_path: "/root/app/foo.ts" },
        })}
        cwd="/root/app"
      />,
    );
    expect(screen.getByText("./foo.ts")).toBeDefined();
  });

  it("uses 'Bash' label when input has a command field, regardless of toolName", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolName: "shell",
          toolInput: { command: "uname" },
        })}
      />,
    );
    expect(screen.getByText("Bash")).toBeDefined();
  });
});

describe("RawToolCard status pill", () => {
  it("shows RUNNING while waiting for a result", () => {
    render(<RawToolCard toolBlock={bashUse()} />);
    expect(screen.getByText("RUNNING")).toBeDefined();
  });

  it("shows DONE once a result arrives", () => {
    render(<RawToolCard toolBlock={bashUse()} resultBlock={result()} />);
    expect(screen.getByText("DONE")).toBeDefined();
  });

  it("shows ERROR when resultBlock.isError", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolName: "Read",
          toolInput: { file_path: "/missing" },
        })}
        resultBlock={result({ text: "ENOENT", isError: true })}
      />,
    );
    expect(screen.getByText("ERROR")).toBeDefined();
  });
});

describe("RawToolCard command_execution result parsing", () => {
  const cmdUse = bashUse({
    toolName: "command_execution",
    toolInput: { command: "echo ok" },
  });

  it("shows EXIT N pill and parsed output (not raw JSON)", () => {
    render(
      <RawToolCard
        toolBlock={cmdUse}
        resultBlock={result({
          text: JSON.stringify({
            exitCode: 0,
            output: "ok\n",
            status: "success",
          }),
        })}
      />,
    );
    expect(screen.getByText("EXIT 0")).toBeDefined();
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByText(/^ok$/m)).toBeDefined();
    expect(screen.queryByText(/"exitCode"/)).toBeNull();
  });

  it("flags non-zero exit as error", () => {
    render(
      <RawToolCard
        toolBlock={cmdUse}
        resultBlock={result({
          text: JSON.stringify({ exitCode: 1, output: "" }),
        })}
      />,
    );
    expect(screen.getByText("EXIT 1")).toBeDefined();
    expect(screen.getByTestId("raw-tool-card").className).toMatch(
      /border-status-error/,
    );
  });

  it("flags failed/interrupted status as error", () => {
    render(
      <RawToolCard
        toolBlock={cmdUse}
        resultBlock={result({
          text: JSON.stringify({ output: "", status: "failed" }),
        })}
      />,
    );
    expect(screen.getByText("ERROR")).toBeDefined();
  });
});

describe("RawToolCard expansion", () => {
  it("starts collapsed; params and result are hidden", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolInput: { command: "echo hi", timeout: 5000 },
        })}
        resultBlock={result({ text: "hi\n" })}
      />,
    );
    expect(screen.queryByText("timeout")).toBeNull();
    expect(screen.queryByText(/^hi$/m)).toBeNull();
  });

  it("expanding reveals params (key=value entries) and result body", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolInput: { command: "echo hi", timeout: 5000 },
        })}
        resultBlock={result({ text: "hi\n" })}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByText("timeout")).toBeDefined();
    expect(screen.getByText("5000")).toBeDefined();
    expect(screen.getByText(/^hi$/m)).toBeDefined();
  });

  it("renders an empty-params placeholder when input is empty", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({ toolInput: {} as Record<string, unknown> })}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByText("No parameters")).toBeDefined();
  });
});

describe("RawToolCard background running pill", () => {
  const bashBlock = (extra: Record<string, unknown>) =>
    ({
      type: "tool_use",
      toolName: "Bash",
      toolUseId: "tu1",
      toolInput: { command: "sleep 5", run_in_background: true },
      ...extra,
    }) as never;

  it("shows 后台运行 + task_id pill when run_in_background", () => {
    render(
      <RawToolCard
        toolBlock={bashBlock({
          subagent: {
            kind: "local_bash",
            taskId: "b3875slp0",
            status: "running",
          },
        })}
        resultBlock={undefined}
        cwd="/tmp"
        sessionId={1}
      />,
    );
    expect(screen.getByText(/后台运行|Background/)).toBeDefined();
    expect(screen.getByText("b3875slp0")).toBeDefined();
  });

  it("does NOT show the pill for a normal foreground bash", () => {
    render(
      <RawToolCard
        toolBlock={
          {
            type: "tool_use",
            toolName: "Bash",
            toolUseId: "tu2",
            toolInput: { command: "ls" },
          } as never
        }
        resultBlock={undefined}
        cwd="/tmp"
        sessionId={1}
      />,
    );
    expect(screen.queryByText(/后台运行|Background/)).toBeNull();
  });

  it("hides the 后台运行 pill once the background task has completed", () => {
    render(
      <RawToolCard
        toolBlock={bashBlock({
          subagent: {
            kind: "local_bash",
            taskId: "b3875slp0",
            status: "completed",
          },
        })}
        resultBlock={undefined}
        cwd="/tmp"
        sessionId={1}
      />,
    );
    expect(screen.queryByText(/后台运行|Background/)).toBeNull();
  });
});

describe("RawToolCard permission integration", () => {
  it("shows the unresolved permission overlay", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolInput: { command: "rm -rf /" },
          toolPermission: {
            requestId: "req-1",
            toolName: "Bash",
            toolInput: {},
            resolved: false,
          },
        })}
        sessionId={42}
      />,
    );
    expect(screen.getByTestId("tool-permission-overlay")).toBeDefined();
  });

  it("shows an Allowed badge when toolPermission resolved+allowed", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolPermission: {
            requestId: "r1",
            toolName: "Bash",
            toolInput: {},
            resolved: true,
            allowed: true,
          },
        })}
      />,
    );
    expect(screen.getByText("Allowed")).toBeDefined();
  });

  it("shows 'Allowed · session' when alwaysAllow is set", () => {
    render(
      <RawToolCard
        toolBlock={bashUse({
          toolPermission: {
            requestId: "r1",
            toolName: "Bash",
            toolInput: {},
            resolved: true,
            allowed: true,
            alwaysAllow: true,
          },
        })}
      />,
    );
    expect(screen.getByText("Allowed · session")).toBeDefined();
  });
});
