import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useSkillCatalog } from "../use-skill-catalog";

function stubBinding(packs: unknown[]) {
  const fn = vi.fn().mockResolvedValue({ packs });
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).go = { app: { App: { ListAgentSkillPacks: fn } } };
  return fn;
}

afterEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (window as any).go;
});

describe("useSkillCatalog", () => {
  it("does not fetch until load() is called", () => {
    const fn = stubBinding([]);
    const { result } = renderHook(() => useSkillCatalog(7));
    expect(fn).not.toHaveBeenCalled();
    expect(result.current.items).toEqual([]);
    expect(result.current.fetched).toBe(false);
  });

  it("loads and maps packs on load(false)", async () => {
    const fn = stubBinding([
      {
        id: "sp@m",
        name: "superpowers",
        description: "d",
        skills: ["a"],
        source: "installed",
        recommended: false,
        installed: true,
        enabled: true,
      },
    ]);
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(false);
    });
    expect(fn).toHaveBeenCalledWith(7, false);
    await waitFor(() => expect(result.current.items).toHaveLength(1));
    expect(result.current.items[0].name).toBe("superpowers");
    expect(result.current.fetched).toBe(true);
  });

  it("rescan calls the binding with refresh=true", async () => {
    const fn = stubBinding([]);
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(true);
    });
    expect(fn).toHaveBeenCalledWith(7, true);
  });

  it("captures errors", async () => {
    const fn = vi.fn().mockRejectedValue(new Error("boom"));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).go = { app: { App: { ListAgentSkillPacks: fn } } };
    const { result } = renderHook(() => useSkillCatalog(7));
    await act(async () => {
      await result.current.load(false);
    });
    await waitFor(() => expect(result.current.error).toBeTruthy());
  });

  it("auto-loads once on mount when autoLoad is true", async () => {
    const fn = stubBinding([]);
    renderHook(() => useSkillCatalog(7, true));
    await waitFor(() => expect(fn).toHaveBeenCalledTimes(1));
    expect(fn).toHaveBeenCalledWith(7, false);
  });
});
