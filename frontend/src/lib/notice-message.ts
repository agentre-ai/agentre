import type { chat_svc } from "../../wailsjs/go/models";

/**
 * isNoticeOnlyMessage 判断一条消息是不是「只承载 notice 的旁白行」。
 *
 * 供应商切换 notice 是独立落库的一条消息(session_provider.go 的
 * appendProviderSwitchNotice):role 是 assistant、块只有一个 notice,但它不是一轮
 * 对话 —— 它可以在任意时刻插进 transcript(pill 允许轮中切换,切完 chat-panel 立刻
 * reloadSession 把它拉进来),包括插在在跑的 assistant 之后。
 *
 * 所以凡是「找紧邻的前一条 / 末条**真实** assistant」的推导都必须跳过它,否则它会
 * 顶替真正那一条:自主续轮横幅错判、生成指示器跳到旁白行、LoadSession 的 pending
 * 审批 overlay 搬不到在跑的那条消息上(卡片永远 pending)。
 *
 * 没有块 ≠ 旁白行:轮刚起时 assistant 行的 blocks 恒为 "[]",那是真实的一轮,
 * 判定必须认到它(横幅/指示器就该在那一刻出现)。
 */
export function isNoticeOnlyMessage(m: chat_svc.ChatMessage): boolean {
  const blocks = m.blocks ?? [];
  return blocks.length > 0 && blocks.every((b) => b.type === "notice");
}
