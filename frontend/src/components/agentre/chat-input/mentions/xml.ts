// Mention 的 XML 序列化/解析核心 —— 纯函数,单测覆盖。
// 序列化产出进入 SendRequest.text;解析用于草稿回填与 transcript chip 渲染。

export type MentionKind = "agent" | "project";

export type MentionRef = {
  kind: MentionKind;
  refId: number;
  label: string;
  // 仅 project 有;agent 省略。
  path?: string;
};

export type MentionSegment =
  | { type: "text"; value: string }
  | { type: "mention"; ref: MentionRef };

function xmlEscape(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function xmlUnescape(s: string): string {
  return s
    .replace(/&quot;/g, '"')
    .replace(/&gt;/g, ">")
    .replace(/&lt;/g, "<")
    .replace(/&amp;/g, "&");
}

export function serializeMentionXml(ref: MentionRef): string {
  const label = xmlEscape(ref.label);
  if (ref.kind === "project") {
    const path = xmlEscape(ref.path ?? "");
    return `<project id="${ref.refId}" path="${path}">${label}</project>`;
  }
  return `<agent id="${ref.refId}">${label}</agent>`;
}

// 同时匹配 <agent …>…</agent> 与 <project …>…</project>。属性顺序固定
// (id,先; project 再 path),但解析用独立属性正则,不依赖顺序。
const TAG_RE = /<(agent|project)\b([^>]*)>([\s\S]*?)<\/\1>/g;
const ID_RE = /\bid="(\d+)"/;
const PATH_RE = /\bpath="([^"]*)"/;

export function parseMentionXml(text: string): MentionSegment[] {
  const out: MentionSegment[] = [];
  let last = 0;
  TAG_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = TAG_RE.exec(text)) !== null) {
    if (m.index > last) {
      out.push({ type: "text", value: text.slice(last, m.index) });
    }
    const kind = m[1] as MentionKind;
    const attrs = m[2];
    const id = Number(ID_RE.exec(attrs)?.[1] ?? "0");
    const label = xmlUnescape(m[3]);
    const ref: MentionRef = { kind, refId: id, label };
    if (kind === "project") {
      ref.path = xmlUnescape(PATH_RE.exec(attrs)?.[1] ?? "");
    }
    out.push({ type: "mention", ref });
    last = m.index + m[0].length;
  }
  if (last < text.length) {
    out.push({ type: "text", value: text.slice(last) });
  }
  if (out.length === 0) out.push({ type: "text", value: text });
  return out;
}
