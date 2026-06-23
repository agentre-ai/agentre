// mentions.ts —— 群聊 @mention 的唯一解析真相。composer（产出结构化收件人
// recipientMemberIDs）与 transcript（渲染 chip）共用同一套解析，避免两边对「什么算一个
// mention」各执一词导致路由 / 高亮不一致。后端不做任何文本解析，前端是 recipientMemberIDs
// 的唯一来源 —— 一旦解析错就是消息路由错，因此这里必须精确、最长优先、带边界。

export type MentionRosterEntry = { memberId: number; name: string };

export type MentionSegment =
  | { type: "text"; value: string }
  | { type: "mention"; memberId: number; name: string };

// <mention>NAME</mention>：编排器 / 后端可能下发的结构化标记。NAME 原样取出（可含空格），
// 先归一成 @NAME 再统一扫描；归一后仍需命中 roster 才成为 chip，未命中回退纯文本。
// 导出给 markdown 渲染路径：react-markdown 默认丢弃 raw HTML 节点，<mention> 标记
// 必须在喂给 markdown 之前归一成 @NAME，否则整段标记会从渲染结果里消失。
const MENTION_MARKUP = /<mention>([^<]*)<\/mention>/g;

export function normalizeMentionMarkup(text: string): string {
  return text.replace(MENTION_MARKUP, (_m, name: string) => `@${name}`);
}

// 名字最后一个字符之后必须是「边界」才算一个完整 mention：字符串末尾、空白或标点。
// 这样 "@Bob" 不会命中 "@Bobby" 的 Bob 前缀（后面紧跟 'b' 是名字字符，不是边界）。
// 注意：名字本身可以含空格 / 标点（"Code Reviewer" / "C++ (dev)"），所以边界只看
// 「名字之后那一个字符」，而不是用 \S+ 之类去切 token。
const BOUNDARY = /[\s,.!?;:，。！？、；：'"`)\]}（）【】「」]/;

function isBoundaryAfter(text: string, index: number): boolean {
  if (index >= text.length) return true; // 字符串末尾
  return BOUNDARY.test(text[index]);
}

// 名字按长度降序排（最长优先），使更长的名字胜过更短的前缀：
//   "@Bobby" 命中 "Bobby" 而非 "Bob"；"@Code Reviewer" 命中整名而非 "Code"。
// 用纯字面 slice 比较命中名字，绝不把原始名字塞进 RegExp（名字可能含 +、(、[ 等正则元字符
// 会让 RegExp 抛错或误匹配），从根上避免注入。
function rosterByLengthDesc(
  roster: MentionRosterEntry[],
): MentionRosterEntry[] {
  return [...roster].sort((a, b) => b.name.length - a.name.length);
}

// matchAt：在 normalized 文本的 '@'（位于 atIndex）处，尝试逐个候选名字（最长优先）做
// 字面匹配。命中条件：'@' 之后紧跟该名字字面，且名字之后是边界。返回命中的 roster 项与
// 名字结束位置（用于继续扫描）；都不命中返回 null。
function matchAt(
  text: string,
  atIndex: number,
  candidates: MentionRosterEntry[],
): { entry: MentionRosterEntry; end: number } | null {
  const start = atIndex + 1; // 跳过 '@'
  for (const entry of candidates) {
    const end = start + entry.name.length;
    if (text.slice(start, end) === entry.name && isBoundaryAfter(text, end)) {
      return { entry, end };
    }
  }
  return null;
}

// tokenizeMentions：把 text 切成渲染段。每段要么是 { type:"text" } 要么 { type:"mention" }。
// 同时吃裸 @NAME 与 <mention>NAME</mention>（先归一）。未命中的 @whatever 留作纯文本。
export function tokenizeMentions(
  text: string,
  roster: MentionRosterEntry[],
): MentionSegment[] {
  const normalized = normalizeMentionMarkup(text);
  const candidates = rosterByLengthDesc(roster);
  const segments: MentionSegment[] = [];
  let pending = ""; // 累积尚未冲刷的纯文本

  const flush = () => {
    if (pending) {
      segments.push({ type: "text", value: pending });
      pending = "";
    }
  };

  let i = 0;
  while (i < normalized.length) {
    if (normalized[i] === "@") {
      const hit = matchAt(normalized, i, candidates);
      if (hit) {
        flush();
        segments.push({
          type: "mention",
          memberId: hit.entry.memberId,
          name: hit.entry.name,
        });
        i = hit.end;
        continue;
      }
    }
    pending += normalized[i];
    i += 1;
  }
  flush();
  return segments;
}

// parseMentionedMemberIds：text 里以 @name 形式出现、且能命中 roster 的成员 id 集合。
// 精确、最长优先、带边界。去重、保持出现顺序。直接复用 tokenizeMentions 的命中结果，
// 保证 composer 与 transcript 对「什么算一个 mention」永远一致。
export function parseMentionedMemberIds(
  text: string,
  roster: MentionRosterEntry[],
): number[] {
  const ids: number[] = [];
  const seen = new Set<number>();
  for (const seg of tokenizeMentions(text, roster)) {
    if (seg.type === "mention" && !seen.has(seg.memberId)) {
      seen.add(seg.memberId);
      ids.push(seg.memberId);
    }
  }
  return ids;
}
