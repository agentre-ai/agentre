// mention 是 inline atom 节点:承载 @ 引用的结构化数据(kind/refId/label/path),
// 以纯 DOM (renderHTML/parseHTML) 渲染成一个 pill,不使用 React node-view ——
// 保持可测 + 与仓库现有「只用 decoration 插件」的编辑器风格一致。
// color 仅用于显示着色 (从选中项带入),不参与 XML 序列化。
import { Node, mergeAttributes } from "@tiptap/core";

import { tokenToCssColor } from "../../session-avatar";
import type { MentionKind } from "./xml";

export const MENTION_NODE_NAME = "mention";

export const Mention = Node.create({
  name: MENTION_NODE_NAME,
  group: "inline",
  inline: true,
  atom: true,
  selectable: true,
  draggable: false,

  addAttributes() {
    return {
      kind: {
        default: "agent" as MentionKind,
        parseHTML: (el) =>
          (el.getAttribute("data-mention-kind") as MentionKind) || "agent",
        renderHTML: (attrs) => ({ "data-mention-kind": attrs.kind }),
      },
      refId: {
        default: 0,
        parseHTML: (el) => Number(el.getAttribute("data-ref-id") ?? "0"),
        renderHTML: (attrs) => ({ "data-ref-id": String(attrs.refId) }),
      },
      label: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-label") ?? "",
        renderHTML: (attrs) => ({ "data-label": attrs.label }),
      },
      path: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-path") ?? "",
        renderHTML: (attrs) => ({ "data-path": attrs.path }),
      },
      color: {
        default: "",
        parseHTML: (el) => el.getAttribute("data-color") ?? "",
        renderHTML: (attrs) => ({ "data-color": attrs.color }),
      },
    };
  },

  parseHTML() {
    return [{ tag: "span[data-mention-kind]" }];
  },

  renderHTML({ node, HTMLAttributes }) {
    const css = tokenToCssColor(node.attrs.color as string);
    return [
      "span",
      mergeAttributes(HTMLAttributes, {
        class: "agentre-mention",
        ...(css ? { style: `--mention-color:${css}` } : {}),
      }),
      `@${node.attrs.label as string}`,
    ];
  },
});
