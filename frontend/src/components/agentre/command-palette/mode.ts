// 命令面板的模式从 query 字符串 derive，单一真相。
// query 以 ">" 开头 → command 模式；payload 是 prefix 后去掉前导空格的剩余串。
// 不引入独立 state，避免 query / mode 双源不一致。

export const COMMAND_PREFIX = ">";

export type PaletteMode = "default" | "command";

export type ParsedQuery = {
  mode: PaletteMode;
  payload: string;
};

export function parseMode(query: string): ParsedQuery {
  if (query.startsWith(COMMAND_PREFIX)) {
    return { mode: "command", payload: query.slice(1).trimStart() };
  }
  return { mode: "default", payload: query };
}
