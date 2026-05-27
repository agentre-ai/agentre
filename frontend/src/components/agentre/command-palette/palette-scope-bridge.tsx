import * as React from "react";

import { useCommandPaletteStore } from "@/stores/command-palette-store";

import { useOptionalShortcutsContext } from "../shortcuts/shortcuts-provider";

// 把 paletteStore.toggle 通过 ShortcutsContext.setPaletteScope 注入到
// ShortcutsProvider 的 paletteRef 里。这样 ShortcutsProvider 不直接 import store，
// keydown handler 仍能在命中 palette.open 时调 toggle。
export function PaletteScopeBridge(): null {
  const ctx = useOptionalShortcutsContext();
  React.useEffect(() => {
    if (!ctx) return;
    const toggle = () => useCommandPaletteStore.getState().toggle();
    const openWith = (query: string) =>
      useCommandPaletteStore.getState().openWith(query);
    ctx.setPaletteScope({ toggle, openWith });
    return () => ctx.setPaletteScope(null);
  }, [ctx]);
  return null;
}
