import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const globalsPath = resolve(process.cwd(), "src/styles/globals.css");

function readThemeBlock(selector: string) {
  const css = readFileSync(globalsPath, "utf8");
  const match = new RegExp(`${selector.replace(".", "\\.")}\\s*{([^}]*)}`).exec(
    css,
  );

  if (!match) {
    throw new Error(`Missing ${selector} theme block`);
  }

  return match[1];
}

function readColorVar(block: string, name: string) {
  const match = new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6});`).exec(block);

  if (!match) {
    throw new Error(`Missing ${name} color variable`);
  }

  return match[1].toLowerCase();
}

function rgb(hex: string) {
  return {
    r: Number.parseInt(hex.slice(1, 3), 16),
    g: Number.parseInt(hex.slice(3, 5), 16),
    b: Number.parseInt(hex.slice(5, 7), 16),
  };
}

function colorDistance(a: string, b: string) {
  const left = rgb(a);
  const right = rgb(b);

  return Math.hypot(left.r - right.r, left.g - right.g, left.b - right.b);
}

describe("theme tokens", () => {
  it("keeps dark accent visibly distinct from popover for hover states", () => {
    const darkTheme = readThemeBlock(".dark");
    const popover = readColorVar(darkTheme, "--popover");
    const accent = readColorVar(darkTheme, "--accent");

    expect(accent).not.toBe(popover);
    expect(colorDistance(accent, popover)).toBeGreaterThanOrEqual(32);
  });

  it("keeps copyable control text enabled after button selection reset", () => {
    const css = readFileSync(globalsPath, "utf8");
    const buttonReset = css.indexOf('[data-selectable-text="true"] button');
    const copyableText = css.indexOf('[data-copyable-control-text="true"]');

    expect(buttonReset).toBeGreaterThanOrEqual(0);
    expect(copyableText).toBeGreaterThan(buttonReset);
    expect(css).toContain('[data-copyable-control-text="true"] *');
    expect(css).toContain("user-select: text;");
  });
});
