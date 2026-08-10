import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useExecTargetAvailability } from "../use-exec-target-availability";

function stubBinding(items: unknown[]) {
  const fn = vi.fn().mockResolvedValue(items);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = {
    app: { App: { ListAgentExecTargetAvailability: fn } },
  };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

describe("useExecTargetAvailability", () => {
  it("fetches on mount with projectID fixed at 0 (org page is not session-scoped)", async () => {
    const fn = stubBinding([
      { agentBackendId: 51, available: true, reason: "", hint: "" },
      {
        agentBackendId: 52,
        available: false,
        reason: "exec-target-offline",
        hint: "这台 agentred 当前离线",
      },
    ]);
    const { result } = renderHook(() => useExecTargetAvailability(7, "51,52"));
    await waitFor(() => expect(fn).toHaveBeenCalledWith(7, 0));
    await waitFor(() => expect(result.current.byBackendId.size).toBe(2));
    expect(result.current.byBackendId.get(51)?.available).toBe(true);
    expect(result.current.byBackendId.get(52)?.reason).toBe(
      "exec-target-offline",
    );
  });

  it("re-fetches when the set of backend ids changes", async () => {
    const fn = stubBinding([]);
    const { rerender } = renderHook(
      ({ key }: { key: string }) => useExecTargetAvailability(7, key),
      { initialProps: { key: "51" } },
    );
    await waitFor(() => expect(fn).toHaveBeenCalledTimes(1));
    rerender({ key: "51,52" });
    await waitFor(() => expect(fn).toHaveBeenCalledTimes(2));
  });

  it("does not call the binding when agentId is falsy", () => {
    const fn = stubBinding([]);
    renderHook(() => useExecTargetAvailability(0, ""));
    expect(fn).not.toHaveBeenCalled();
  });

  // 增删执行目标会连着触发多次拉取（targetsKey 变一次拉一次），先发的那次晚返回时
  // 不能把新一批判定盖回旧的——列表上的徽标会一直停在删掉那一档还在的快照上。
  it("ignores a stale response that lands after a newer request", async () => {
    let resolveStale: (v: unknown) => void = () => {};
    const fn = stubBinding([]);
    fn.mockReturnValueOnce(
      new Promise((resolve) => {
        resolveStale = resolve;
      }),
    );
    fn.mockResolvedValue([{ agentBackendId: 52, available: true, reason: "" }]);

    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useExecTargetAvailability(7, key),
      { initialProps: { key: "51" } },
    );
    rerender({ key: "52" });
    await waitFor(() => expect(result.current.byBackendId.has(52)).toBe(true));

    await act(async () => {
      resolveStale([{ agentBackendId: 51, available: true, reason: "" }]);
    });
    expect(result.current.byBackendId.has(52)).toBe(true);
    expect(result.current.byBackendId.has(51)).toBe(false);
  });
});
