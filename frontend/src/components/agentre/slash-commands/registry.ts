// Slash command 注册表。新增命令时只需在 slashCommands 数组里追加;按 backend 分发
// 执行策略由 SlashCommand.resolve(backendType) 决定 —— 返回 null 表示该命令在此
// backend 不可用,UI 直接从候选列表里过滤掉。
//
// 已知后端:
//   - claudecode: CLI 自己识别 / 前缀,绝大多数命令都走 literal_text(把命令字符串
//     当普通 user 文本送过去 SendChatMessage)。
//   - codex: CLI 协议没有内置 slash command,但 chat-panel 的 onSubmit 会拦截
//     `/compact` 文本转走 CompactChatSession RPC —— 所以这里也只需要 literal_text,
//     用户从菜单选中只补全文字,真正分发由 Enter 走 chat-panel 完成。
//   - builtin / 其它: 暂不参与 slash 命令。

export type SlashExec =
  | {
      // 直接以普通用户消息形式发送一段文本(典型例子:claudecode 的 /compact)。
      kind: "literal_text";
      text: string;
    }
  | {
      // 调用 Wails 绑定走专门 RPC 路径(典型例子:codex 没有原生 /compact,
      // 需要前端自行触发一次 Compact RPC)。handler 拿到 sessionId 自行 dispatch。
      kind: "rpc";
      handler: (ctx: { sessionId: number }) => Promise<void> | void;
    };

export type SlashCommand = {
  // canonical name (kebab-case),用于稳定 key/匹配,例:"compact"。
  name: string;
  // 下拉里显示的命令字面值,通常等于 `/${name}`。
  label: string;
  // 一句话说明,会在下拉项右侧 muted 显示。
  description?: string;
  // 返回当前 backend 下的执行策略;null 表示该 backend 不支持此命令。
  resolve: (backendType: string) => SlashExec | null;
};

export const slashCommands: SlashCommand[] = [
  {
    name: "compact",
    label: "/compact",
    description: "压缩对话上下文,只保留摘要",
    resolve(backend) {
      if (backend === "claudecode" || backend === "codex") {
        return { kind: "literal_text", text: "/compact" };
      }
      return null;
    },
  },
];

// listAvailable 返回当前 backend 下可用的命令清单。UI 用它做下拉候选。
export function listAvailable(backendType: string): SlashCommand[] {
  if (!backendType) return [];
  return slashCommands.filter((c) => c.resolve(backendType) !== null);
}

// filterByQuery 在 listAvailable 基础上按用户输入的 query 做前缀匹配
// (大小写不敏感)。空 query 显示全部。
export function filterByQuery(
  commands: SlashCommand[],
  query: string,
): SlashCommand[] {
  const q = query.trim().toLowerCase();
  if (!q) return commands;
  return commands.filter((c) => c.name.toLowerCase().startsWith(q));
}
