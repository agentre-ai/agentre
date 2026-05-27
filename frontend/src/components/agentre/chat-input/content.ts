import type {
  AIChatInputDraft,
  ProseMirrorLikeNode,
  TipTapDocNode,
  TipTapParagraphNode,
  TipTapTextNode,
} from "./types";

// 提取纯文本：text 原样保留；hardBreak / 段落边界统一用 \n。
// （TipTap doc 末尾必带空段落，最后再去掉行尾 \n。）
export function extractPlainText(doc: ProseMirrorLikeNode): string {
  let out = "";
  doc.descendants((node) => {
    if (node.type.name === "text") {
      out += node.text ?? "";
    } else if (node.type.name === "hardBreak") {
      out += "\n";
    } else if (node.type.name === "paragraph" && out.length > 0) {
      out += "\n";
    }
    return true;
  });
  return out.replace(/\n+$/g, "");
}

function normalizeDraftMessage(
  draft: string | AIChatInputDraft,
): AIChatInputDraft {
  if (typeof draft === "string") {
    return { content: draft };
  }
  return { content: draft.content ?? "" };
}

export function buildEditorDocFromMessage(
  message: string | AIChatInputDraft,
): TipTapDocNode {
  const { content } = normalizeDraftMessage(message);
  const paragraphs: TipTapParagraphNode[] = [];
  const segments = content.split("\n");
  for (const seg of segments) {
    const textNodes: TipTapTextNode[] =
      seg.length > 0 ? [{ type: "text", text: seg }] : [];
    paragraphs.push(
      textNodes.length > 0
        ? { type: "paragraph", content: textNodes }
        : { type: "paragraph" },
    );
  }
  if (paragraphs.length === 0) {
    paragraphs.push({ type: "paragraph" });
  }
  return { type: "doc", content: paragraphs };
}
