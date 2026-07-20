import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// 对话流排版依赖这几个 token。它们是 globals.css 与所有 transcript 组件之间的契约,
// 改名/删除会让 max-w-measure、text-prose 等工具类静默失效(Tailwind 不会报错,
// 只是不生成这个类),所以在这里锁死。
const GLOBALS_CSS = path.resolve(__dirname, "../styles/globals.css");

const REQUIRED_TOKENS: [string, string][] = [
  ["--text-prose", "0.9375rem"],
  ["--text-prose--line-height", "1.7"],
  ["--text-aux", "0.8125rem"],
  ["--text-aux--line-height", "1.65"],
  ["--text-meta", "0.75rem"],
  ["--text-meta--line-height", "1.25rem"],
  ["--container-measure", "45rem"],
];

describe("design tokens", () => {
  it("globals.css 暴露对话流排版 token", () => {
    const css = fs.readFileSync(GLOBALS_CSS, "utf8");
    const missing = REQUIRED_TOKENS.filter(
      ([name, value]) => !new RegExp(`${name}:\\s*${value}\\s*;`).test(css),
    ).map(([name]) => name);
    expect(missing).toEqual([]);
  });
});
