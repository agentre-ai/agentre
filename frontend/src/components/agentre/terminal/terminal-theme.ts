import type { ITheme } from "@xterm/xterm";

// 跟随应用主题：background/foreground 对齐 globals.css 的 --background/--foreground
// (light #fafafa/#18181b, dark #17191c/#e6e8eb)，其余 16 色 ANSI + selection 显式
// 配齐。xterm 默认调色板是给黑底调的，只设 bg/fg 会让浅色模式下亮色文字发白看不清。
// selectionForeground/selectionInactiveBackground 显式设置让选区走"统一 selectionFg"
// 渲染路径，避免选中文字被重新栅格化成另一种字重。

const LIGHT_TERMINAL_THEME: ITheme = {
  background: "#fafafa",
  foreground: "#18181b",
  cursor: "#18181b",
  cursorAccent: "#fafafa",
  selectionBackground: "#bdd0ea",
  selectionForeground: "#18181b",
  selectionInactiveBackground: "#d6e0ef",
  black: "#2b3040",
  red: "#e45649",
  green: "#50a14f",
  yellow: "#c18401",
  blue: "#4078f2",
  magenta: "#a626a4",
  cyan: "#0184bc",
  white: "#cdd0d8",
  brightBlack: "#4e5569",
  brightRed: "#e06c75",
  brightGreen: "#98c379",
  brightYellow: "#e5c07b",
  brightBlue: "#61afef",
  brightMagenta: "#c678dd",
  brightCyan: "#56b6c2",
  brightWhite: "#18181b",
};

const DARK_TERMINAL_THEME: ITheme = {
  background: "#17191c",
  foreground: "#e6e8eb",
  cursor: "#e6e8eb",
  cursorAccent: "#17191c",
  selectionBackground: "#1e3050",
  selectionForeground: "#e6e8eb",
  selectionInactiveBackground: "#27344a",
  black: "#17191c",
  red: "#f07178",
  green: "#a6d189",
  yellow: "#e5c07b",
  blue: "#7b93f5",
  magenta: "#c78ddd",
  cyan: "#89dceb",
  white: "#e6e8eb",
  brightBlack: "#4e5569",
  brightRed: "#f38ba8",
  brightGreen: "#a6e3a1",
  brightYellow: "#f9e2af",
  brightBlue: "#89b4fa",
  brightMagenta: "#cba6f7",
  brightCyan: "#94e2d5",
  brightWhite: "#f5f8fc",
};

// 返回一份完整的 xterm 主题。isDark 选 light/dark 调色板；bg/fg 传入应用实时解析出
// 的 --background/--foreground (空串视为未提供，回退调色板默认值)，使终端底色与应用
// 表面一致。
export function resolveTerminalTheme(
  isDark: boolean,
  bg?: string,
  fg?: string,
): ITheme {
  const base = isDark ? DARK_TERMINAL_THEME : LIGHT_TERMINAL_THEME;
  const background = bg?.trim() || base.background;
  const foreground = fg?.trim() || base.foreground;
  return { ...base, background, foreground };
}
