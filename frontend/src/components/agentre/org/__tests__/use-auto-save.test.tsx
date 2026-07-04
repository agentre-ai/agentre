import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAutoSave } from "../use-auto-save";

type Form = { name: string; color: string };
const initial: Form = { name: "A", color: "red" };

describe("useAutoSave", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("immediate patch saves once with merged values", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    act(() => result.current.patch({ color: "blue" }, { immediate: true }));

    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "A", color: "blue" });
    expect(result.current.values).toEqual({ name: "A", color: "blue" });
    await act(async () => {});
    expect(result.current.status).toBe("saved");
  });

  it("debounced patches coalesce into one save with the latest value", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useAutoSave({ initial, save, debounceMs: 600 }),
    );

    act(() => result.current.patch({ name: "AB" }));
    act(() => result.current.patch({ name: "ABC" }));
    expect(save).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(600));
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "ABC", color: "red" });
  });

  it("flush runs a pending debounced save immediately", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    act(() => result.current.patch({ name: "AB" }));
    act(() => result.current.flush());
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ name: "AB", color: "red" });
  });

  it("holds save when invalid, then saves once valid again", () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() =>
      useAutoSave({
        initial,
        save,
        isValid: (v) => v.name.trim() !== "",
      }),
    );

    act(() => result.current.patch({ name: "" }, { immediate: true }));
    expect(save).not.toHaveBeenCalled();
    expect(result.current.pendingInvalid).toBe(true);

    act(() => result.current.patch({ name: "B" }, { immediate: true }));
    expect(save).toHaveBeenCalledTimes(1);
    expect(result.current.pendingInvalid).toBe(false);
  });

  it("sets error on rejection and retry re-runs the save", async () => {
    const save = vi
      .fn()
      .mockRejectedValueOnce(new Error("boom"))
      .mockResolvedValueOnce(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    await act(async () => {
      result.current.patch({ color: "blue" }, { immediate: true });
    });
    expect(result.current.status).toBe("error");

    await act(async () => {
      result.current.retry();
    });
    expect(save).toHaveBeenCalledTimes(2);
    expect(result.current.status).toBe("saved");
  });

  it("wrap drives status and returns the fn result", async () => {
    const save = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => useAutoSave({ initial, save }));

    let ret: string | null = "x";
    await act(async () => {
      ret = await result.current.wrap(async () => "moved");
    });
    expect(ret).toBe("moved");
    expect(result.current.status).toBe("saved");
  });
});
