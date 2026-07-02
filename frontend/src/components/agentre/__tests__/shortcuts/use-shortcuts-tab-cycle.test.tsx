import React from "react";
import { act, render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { DesktopPlatform } from "../../chrome";

import {
  ShortcutsProvider,
  useShortcutsContext,
} from "../../shortcuts/shortcuts-provider";

function TabsScopeBinder(props: {
  cycle: (delta: number) => void;
  mounted: boolean;
}) {
  const { setTabsScope } = useShortcutsContext();
  React.useEffect(() => {
    if (!props.mounted) return;
    setTabsScope({
      switchTo: () => {},
      close: () => {},
      cycle: props.cycle,
    });
    return () => setTabsScope(null);
  }, [props.mounted, props.cycle, setTabsScope]);
  return null;
}

function renderHarness(opts: {
  platform?: DesktopPlatform;
  cycle: (delta: number) => void;
  mounted?: boolean;
}) {
  return render(
    <MemoryRouter initialEntries={["/chat"]}>
      <Routes>
        <Route
          path="*"
          element={
            <ShortcutsProvider platform={opts.platform ?? "darwin"}>
              <TabsScopeBinder
                cycle={opts.cycle}
                mounted={opts.mounted ?? true}
              />
            </ShortcutsProvider>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

function press(key: string, init: KeyboardEventInit = {}) {
  act(() => {
    window.dispatchEvent(new KeyboardEvent("keydown", { key, ...init }));
  });
}

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  localStorage.clear();
});

describe("ShortcutsProvider Ctrl+Tab tab cycling", () => {
  it("Ctrl+Tab cycles forward (delta +1)", () => {
    const cycle = vi.fn();
    renderHarness({ cycle });
    press("Tab", { ctrlKey: true });
    expect(cycle).toHaveBeenCalledTimes(1);
    expect(cycle).toHaveBeenCalledWith(1);
  });

  it("Ctrl+Shift+Tab cycles backward (delta -1)", () => {
    const cycle = vi.fn();
    renderHarness({ cycle });
    press("Tab", { ctrlKey: true, shiftKey: true });
    expect(cycle).toHaveBeenCalledTimes(1);
    expect(cycle).toHaveBeenCalledWith(-1);
  });

  it("plain Tab without Ctrl does not cycle (focus traversal is untouched)", () => {
    const cycle = vi.fn();
    renderHarness({ cycle });
    press("Tab");
    expect(cycle).not.toHaveBeenCalled();
  });

  it("⌘Tab does not cycle — the macOS app switcher stays untouched", () => {
    const cycle = vi.fn();
    renderHarness({ cycle, platform: "darwin" });
    press("Tab", { metaKey: true });
    expect(cycle).not.toHaveBeenCalled();
  });

  it("Alt+Ctrl+Tab does not cycle", () => {
    const cycle = vi.fn();
    renderHarness({ cycle });
    press("Tab", { ctrlKey: true, altKey: true });
    expect(cycle).not.toHaveBeenCalled();
  });

  it("is a no-op when no tabs scope is mounted", () => {
    const cycle = vi.fn();
    renderHarness({ cycle, mounted: false });
    press("Tab", { ctrlKey: true });
    expect(cycle).not.toHaveBeenCalled();
  });

  it("ignores Ctrl+Tab while IME composition is active", () => {
    const cycle = vi.fn();
    renderHarness({ cycle });
    act(() => {
      window.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "Tab",
          ctrlKey: true,
          isComposing: true,
        }),
      );
    });
    expect(cycle).not.toHaveBeenCalled();
  });

  it("cycles the same way on linux with Ctrl+Tab", () => {
    const cycle = vi.fn();
    renderHarness({ cycle, platform: "linux" });
    press("Tab", { ctrlKey: true });
    expect(cycle).toHaveBeenCalledWith(1);
  });
});
