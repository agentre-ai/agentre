import { beforeEach, describe, expect, it } from "vitest";

import { useCommandPaletteStore } from "../command-palette-store";

describe("useCommandPaletteStore", () => {
  beforeEach(() => {
    useCommandPaletteStore.setState({ open: false, initialQuery: "" });
  });

  it("defaults open=false", () => {
    expect(useCommandPaletteStore.getState().open).toBe(false);
  });

  it("setOpen toggles state", () => {
    useCommandPaletteStore.getState().setOpen(true);
    expect(useCommandPaletteStore.getState().open).toBe(true);
    useCommandPaletteStore.getState().setOpen(false);
    expect(useCommandPaletteStore.getState().open).toBe(false);
  });

  it("toggle flips state on each call", () => {
    useCommandPaletteStore.getState().toggle();
    expect(useCommandPaletteStore.getState().open).toBe(true);
    useCommandPaletteStore.getState().toggle();
    expect(useCommandPaletteStore.getState().open).toBe(false);
    useCommandPaletteStore.getState().toggle();
    expect(useCommandPaletteStore.getState().open).toBe(true);
  });

  it("close forces open=false regardless of current state", () => {
    useCommandPaletteStore.setState({ open: true });
    useCommandPaletteStore.getState().close();
    expect(useCommandPaletteStore.getState().open).toBe(false);
    // idempotent
    useCommandPaletteStore.getState().close();
    expect(useCommandPaletteStore.getState().open).toBe(false);
  });

  it("initialQuery defaults to empty string", () => {
    expect(useCommandPaletteStore.getState().initialQuery).toBe("");
  });

  it("openWith opens palette and seeds initialQuery atomically", () => {
    useCommandPaletteStore.getState().openWith("> New chat with ");
    const s = useCommandPaletteStore.getState();
    expect(s.open).toBe(true);
    expect(s.initialQuery).toBe("> New chat with ");
  });

  it("close clears initialQuery so reopen doesn't replay last seed", () => {
    useCommandPaletteStore.getState().openWith("> hello");
    useCommandPaletteStore.getState().close();
    const s = useCommandPaletteStore.getState();
    expect(s.open).toBe(false);
    expect(s.initialQuery).toBe("");
  });

  it("setOpen(false) also clears initialQuery — Dialog's onOpenChange uses setOpen, must not leave stale seed", () => {
    // 还原 bug：用户 ⌘N → 关弹窗（Dialog onOpenChange(false) 走 setOpen(false)）
    // → ⌘P 用 toggle，但 store 里 initialQuery 仍是 "> " → palette 进命令模式。
    useCommandPaletteStore.getState().openWith("> ");
    expect(useCommandPaletteStore.getState().initialQuery).toBe("> ");
    useCommandPaletteStore.getState().setOpen(false);
    expect(useCommandPaletteStore.getState().open).toBe(false);
    expect(useCommandPaletteStore.getState().initialQuery).toBe("");
  });

  it("setOpen(true) doesn't touch initialQuery (caller controls seeding)", () => {
    useCommandPaletteStore.setState({ initialQuery: "> foo" });
    useCommandPaletteStore.getState().setOpen(true);
    expect(useCommandPaletteStore.getState().initialQuery).toBe("> foo");
  });

  it("toggle() to closed also clears initialQuery (parity with setOpen(false))", () => {
    useCommandPaletteStore.getState().openWith("> hi");
    useCommandPaletteStore.getState().toggle();
    expect(useCommandPaletteStore.getState().open).toBe(false);
    expect(useCommandPaletteStore.getState().initialQuery).toBe("");
  });

  it("toggle() to open does NOT seed (only openWith does)", () => {
    // BDD: ⌘P 永远进默认模式，不会因为 store 残留进入命令模式
    expect(useCommandPaletteStore.getState().initialQuery).toBe("");
    useCommandPaletteStore.getState().toggle();
    expect(useCommandPaletteStore.getState().open).toBe(true);
    expect(useCommandPaletteStore.getState().initialQuery).toBe("");
  });
});
