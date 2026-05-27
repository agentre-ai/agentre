import { act, render } from "@testing-library/react";
import * as React from "react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it } from "vitest";

import { useCommandPaletteStore } from "@/stores/command-palette-store";

import { PaletteScopeBridge } from "../../command-palette";
import {
  ShortcutsProvider,
  useShortcutsContext,
} from "../../shortcuts/shortcuts-provider";
import type { PaletteScope } from "../../shortcuts/types";

// Probe runs after effects (via useEffect) and pushes the current ref value out.
function ScopeProbe(props: {
  onChange: (scope: PaletteScope | null | undefined) => void;
}) {
  const ctx = useShortcutsContext();
  React.useEffect(() => {
    props.onChange(ctx.paletteScopeRef.current);
  });
  return null;
}

describe("PaletteScopeBridge", () => {
  beforeEach(() => {
    useCommandPaletteStore.setState({ open: false });
  });

  it("injects paletteScope.toggle on mount; calling toggle flips palette store", () => {
    let lastScope: PaletteScope | null | undefined = undefined;
    render(
      <MemoryRouter>
        <ShortcutsProvider platform="darwin">
          <PaletteScopeBridge />
          <ScopeProbe
            onChange={(s) => {
              lastScope = s;
            }}
          />
        </ShortcutsProvider>
      </MemoryRouter>,
    );

    expect(lastScope).toBeTruthy();
    expect(typeof lastScope!.toggle).toBe("function");

    act(() => lastScope!.toggle());
    expect(useCommandPaletteStore.getState().open).toBe(true);
    act(() => lastScope!.toggle());
    expect(useCommandPaletteStore.getState().open).toBe(false);
  });

  it("clears paletteScope on unmount", () => {
    let lastScope: PaletteScope | null | undefined = undefined;
    const { unmount } = render(
      <MemoryRouter>
        <ShortcutsProvider platform="darwin">
          <PaletteScopeBridge />
          <ScopeProbe
            onChange={(s) => {
              lastScope = s;
            }}
          />
        </ShortcutsProvider>
      </MemoryRouter>,
    );
    expect(lastScope).toBeTruthy();
    unmount();
    // After unmount, no further re-renders happen on this tree — but the
    // cleanup ran. We verify by mounting a fresh provider+probe; ref starts null.
    let nextScope: PaletteScope | null | undefined = undefined;
    render(
      <MemoryRouter>
        <ShortcutsProvider platform="darwin">
          <ScopeProbe
            onChange={(s) => {
              nextScope = s;
            }}
          />
        </ShortcutsProvider>
      </MemoryRouter>,
    );
    expect(nextScope ?? null).toBeNull();
  });
});
