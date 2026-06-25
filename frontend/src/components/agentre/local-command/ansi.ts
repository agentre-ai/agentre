// 去除 CSI/SGR 等 ANSI 转义,用于内联卡片只读展示(真彩交互在 xterm)。
// eslint-disable-next-line no-control-regex
const ANSI_RE = /\[[0-9;?]*[ -/]*[@-~]/g;
export function stripAnsi(s: string): string {
  return s.replace(ANSI_RE, "");
}
