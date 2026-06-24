import { act, render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// mock sonner toast
vi.mock("sonner", () => ({
  toast: Object.assign(vi.fn(), { success: vi.fn() }),
}));

// 拦截 wailsjs go 绑定(orch-run-store 内部 import)
vi.mock("../../../../wailsjs/go/app/App", () => ({
  RunLoad: vi.fn().mockResolvedValue({}),
  RunList: vi.fn().mockResolvedValue([]),
}));

import { toast } from "sonner";
import type { app } from "../../../../wailsjs/go/models";
import { useOrchRunStore } from "../../../stores/orch-run-store";
import { useChatTabsStore } from "../../../stores/chat-tabs-store";
import { OrchNotifier } from "../orch-notifier";

// 构造测试用 RunDetailDTO，避免 TypeScript 类型不兼容。
function makeDetail(
  run: { id: number; goal: string; status: string },
  tasks: { id: number; status: string }[],
): app.RunDetailDTO {
  return { run, tasks } as unknown as app.RunDetailDTO;
}

beforeEach(() => {
  vi.clearAllMocks();
  useOrchRunStore.getState().__reset();
  useChatTabsStore.setState({ tabs: [], activeTabId: null });
});

describe("OrchNotifier", () => {
  it("Run 从非 done 转为 done 时触发 toast.success", async () => {
    render(<OrchNotifier />);
    await act(async () => {});

    act(() => {
      useOrchRunStore.setState({
        details: new Map([
          [1, makeDetail({ id: 1, goal: "G", status: "done" }, [])],
        ]),
      });
    });

    expect(toast.success).toHaveBeenCalledTimes(1);
    const call = (toast.success as ReturnType<typeof vi.fn>).mock.calls[0] as [
      string,
    ];
    expect(call[0]).toContain("G");
  });

  it("挂载前已是 done 的 Run 不误报", async () => {
    // 挂载前注入 done 状态
    useOrchRunStore.setState({
      details: new Map([
        [1, makeDetail({ id: 1, goal: "G", status: "done" }, [])],
      ]),
    });

    render(<OrchNotifier />);
    await act(async () => {});

    expect(toast.success).not.toHaveBeenCalled();
  });

  it("任务首次进入 awaiting-user 时触发 toast（等待你）", async () => {
    render(<OrchNotifier />);
    await act(async () => {});

    act(() => {
      useOrchRunStore.setState({
        details: new Map([
          [
            2,
            makeDetail({ id: 2, goal: "Goal2", status: "running" }, [
              { id: 10, status: "awaiting-user" },
            ]),
          ],
        ]),
      });
    });

    expect(toast).toHaveBeenCalledTimes(1);
    const call = (toast as unknown as ReturnType<typeof vi.fn>).mock
      .calls[0] as [string];
    expect(call[0]).toContain("Goal2");
  });

  it("同一 awaiting-user 任务重复 setState 不重复通知（去重）", async () => {
    render(<OrchNotifier />);
    await act(async () => {});

    const detail = makeDetail({ id: 2, goal: "Goal2", status: "running" }, [
      { id: 10, status: "awaiting-user" },
    ]);

    act(() => {
      useOrchRunStore.setState({ details: new Map([[2, detail]]) });
    });
    act(() => {
      // 再次 setState 同样内容
      useOrchRunStore.setState({
        details: new Map([
          [
            2,
            makeDetail({ id: 2, goal: "Goal2", status: "running" }, [
              { id: 10, status: "awaiting-user" },
            ]),
          ],
        ]),
      });
    });

    // 只调用一次
    expect(toast).toHaveBeenCalledTimes(1);
  });

  it("挂载前已有 awaiting-user 任务不误报", async () => {
    // 挂载前注入 awaiting-user 任务
    useOrchRunStore.setState({
      details: new Map([
        [
          3,
          makeDetail({ id: 3, goal: "G3", status: "running" }, [
            { id: 20, status: "awaiting-user" },
          ]),
        ],
      ]),
    });

    render(<OrchNotifier />);
    await act(async () => {});

    expect(toast).not.toHaveBeenCalled();
  });

  it("点击 toast action 调用 openRun", async () => {
    const openRun = vi.spyOn(useChatTabsStore.getState(), "openRun");

    render(<OrchNotifier />);
    await act(async () => {});

    act(() => {
      useOrchRunStore.setState({
        details: new Map([
          [5, makeDetail({ id: 5, goal: "TestGoal", status: "done" }, [])],
        ]),
      });
    });

    expect(toast.success).toHaveBeenCalledTimes(1);
    const opts = (toast.success as ReturnType<typeof vi.fn>).mock
      .calls[0][1] as {
      action: { onClick: () => void };
    };
    opts.action.onClick();
    expect(openRun).toHaveBeenCalledWith(5, "TestGoal");
  });
});
