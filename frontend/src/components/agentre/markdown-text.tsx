import * as React from "react";
import ReactMarkdown, {
  type Components,
  type Options as ReactMarkdownOptions,
} from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";
import { splitStreamingMarkdown } from "@/lib/streaming-markdown";

import { CodeBlock } from "./code-block";
import { RichLink } from "./rich-link";

type MarkdownElementNode = {
  tagName?: string;
  properties?: {
    className?: unknown;
  };
};

type MarkdownCodeElement = React.ReactElement<{
  children?: React.ReactNode;
  className?: string;
  node?: MarkdownElementNode;
}>;

function markdownClassName(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.filter(Boolean).join(" ");
  return "";
}

function isCodeElement(child: React.ReactNode): child is MarkdownCodeElement {
  return (
    React.isValidElement<{
      node?: MarkdownElementNode;
    }>(child) && child.props.node?.tagName === "code"
  );
}

function codeBlockLanguage(className: string): string {
  const match = /\blanguage-([^\s]+)/.exec(className);
  return match?.[1] ?? "code";
}

const SAFE_HREF_PATTERNS: RegExp[] = [
  /^https?:/i,
  /^mailto:/i,
  /^tel:/i,
  /^file:\/\//i,
  /^www\./i,
  /^\//, // POSIX 绝对
  /^[A-Za-z]:[\\/]/, // Windows 绝对
];

function whitelistUrl(url: string): string {
  for (const p of SAFE_HREF_PATTERNS) {
    if (p.test(url)) return url;
  }
  return "";
}

// markdownComponentsStatic 给 react-markdown 提供按 tailwind token 调过的元素映射。
// 没有装 @tailwindcss/typography，所以手工把常用块/行内元素的样式补上，
// 保持和 chat 气泡内的字号 / 间距协调。
//
// 每个 component 都显式 destruct 出 `node` 扔掉:react-markdown 给 components
// 传 props 时会带 hast 节点对象,直接 `{...props}` 会把它 spread 到 DOM,
// 浏览器渲染成 <p node="[object Object]"> 这种垃圾属性。
//
// `a` 不在这里定义——它需要 cwd 上下文，所以在 MarkdownText 的 useMemo 里动态构建。
const markdownComponentsStatic: Components = {
  blockquote: ({ node: _node, className, ...props }) => (
    <blockquote
      {...props}
      className={cn(
        "my-2 border-l-2 border-border-strong pl-3 text-muted-foreground",
        className,
      )}
    />
  ),
  code: ({ node: _node, className, children, ...props }) => {
    const isBlock = /\bhljs\b|\blanguage-/.test(className ?? "");
    if (isBlock) {
      return (
        <code {...props} className={cn("font-mono", className)}>
          {children}
        </code>
      );
    }
    return (
      <code
        {...props}
        className={cn(
          "rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]",
          className,
        )}
      >
        {children}
      </code>
    );
  },
  h1: ({ node: _node, className, ...props }) => (
    <h1
      {...props}
      className={cn("mt-3 mb-1 text-base font-semibold", className)}
    />
  ),
  h2: ({ node: _node, className, ...props }) => (
    <h2
      {...props}
      className={cn("mt-3 mb-1 text-[15px] font-semibold", className)}
    />
  ),
  h3: ({ node: _node, className, ...props }) => (
    <h3
      {...props}
      className={cn("mt-2 mb-1 text-sm font-semibold", className)}
    />
  ),
  hr: ({ node: _node, className, ...props }) => (
    <hr {...props} className={cn("my-3 border-border", className)} />
  ),
  li: ({ node: _node, className, ...props }) => (
    <li {...props} className={cn("my-0.5", className)} />
  ),
  ol: ({ node: _node, className, ...props }) => (
    <ol
      {...props}
      className={cn("my-1 list-decimal space-y-0.5 pl-5", className)}
    />
  ),
  p: ({ node: _node, className, ...props }) => (
    <p {...props} className={cn("my-1 first:mt-0 last:mb-0", className)} />
  ),
  pre: ({ node: _node, children, className, ...props }) => {
    const codeChild = React.Children.toArray(children).find(isCodeElement);
    if (codeChild) {
      const codeClassName =
        codeChild.props.className ??
        markdownClassName(codeChild.props.node?.properties?.className);
      // 不要把 `pre` 元素的属性 spread 到 CodeBlock —— pre 的 HTMLAttributes
      // (ref / onCopy 等) 都是 HTMLPreElement 类型,与 CodeBlock 包的 <div>
      // (HTMLDivElement) 类型不兼容,tsc -b 严格模式会卡。react-markdown 实际
      // 也几乎不会在 pre 上注入事件处理器,直接丢弃 props 不影响渲染。
      void props;
      return (
        <CodeBlock
          className={cn("my-2", className)}
          language={codeBlockLanguage(codeClassName)}
        >
          <code className={cn("font-mono", codeClassName)}>
            {codeChild.props.children}
          </code>
        </CodeBlock>
      );
    }
    return (
      <pre
        {...props}
        className={cn(
          "my-2 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed",
          className,
        )}
      >
        {children}
      </pre>
    );
  },
  table: ({ node: _node, className, ...props }) => (
    <div className="my-2 overflow-auto">
      <table
        {...props}
        className={cn("w-full border-collapse text-xs", className)}
      />
    </div>
  ),
  td: ({ node: _node, className, ...props }) => (
    <td
      {...props}
      className={cn("border border-border px-2 py-1 align-top", className)}
    />
  ),
  th: ({ node: _node, className, ...props }) => (
    <th
      {...props}
      className={cn(
        "border border-border bg-muted px-2 py-1 text-left font-semibold",
        className,
      )}
    />
  ),
  ul: ({ node: _node, className, ...props }) => (
    <ul
      {...props}
      className={cn("my-1 list-disc space-y-0.5 pl-5", className)}
    />
  ),
};

// 提到模块顶层：plugins/components 当作 ReactMarkdown 的稳定引用，避免每次
// render 都让 react-markdown 重新构建 unified processor。配合 React.memo 下方
// MarkdownText，让历史消息的 markdown 解析+语法高亮可以跳过。
const markdownRemarkPlugins: ReactMarkdownOptions["remarkPlugins"] = [
  remarkGfm,
];
const markdownRehypePlugins: ReactMarkdownOptions["rehypePlugins"] = [
  [rehypeHighlight, { detect: true, ignoreMissing: true }],
];

// MarkdownInner 是「不带 .markdown-body 外壳」的 ReactMarkdown。单独 React.memo:
// 文本稳定时跳过 markdown 解析 + highlight.js 高亮。抽出外壳是为了让 StreamingMarkdown
// 能把多个已定稿段塞进同一个 .markdown-body 里渲染 —— 段落的 first/last 外边距
// 重置必须跨整条消息生效,分到多个外壳会让已定稿段落间距塌成 0。
const MarkdownInner = React.memo(function MarkdownInner({
  text,
  cwd,
}: {
  text: string;
  cwd?: string;
}) {
  const components = React.useMemo<Components>(
    () => ({
      ...markdownComponentsStatic,
      a: ({ node: _node, href, children, className }) => (
        <RichLink
          href={href}
          className={typeof className === "string" ? className : undefined}
          cwd={cwd}
        >
          {children}
        </RichLink>
      ),
    }),
    [cwd],
  );

  return (
    <ReactMarkdown
      components={components}
      remarkPlugins={markdownRemarkPlugins}
      rehypePlugins={markdownRehypePlugins}
      urlTransform={whitelistUrl}
    >
      {text}
    </ReactMarkdown>
  );
});

export const MarkdownText = React.memo(function MarkdownText({
  text,
  cwd,
}: {
  text: string;
  cwd?: string;
}) {
  return (
    <div className="markdown-body break-words text-sm leading-relaxed">
      <MarkdownInner text={text} cwd={cwd} />
    </div>
  );
});

// StreamingMarkdown 增量渲染「流式累积中」的 markdown:把文本按 block 边界切成
// [已定稿 block...] + [活跃尾巴],每段交给一个 React.memo 的 MarkdownInner,
// 全部塞进同一个 .markdown-body 外壳。已定稿段的文本逐字节稳定 → memo 命中 →
// 只解析+高亮一次,后续 chunk 跳过;只有活跃尾巴每个 chunk 重解析,把单 chunk
// 渲染开销从 O(n) 降到 O(Δ),彻底消除「整段 markdown 每 chunk 全量重解析 +
// highlight.js 全量重探测」的 O(n²) 卡顿。各段同处一个外壳,段落 first/last
// 外边距跨整条消息计算,间距与一次性解析完全一致。
//
// 定稿段的 key 用顺序下标:切分是 append-only,已出现的下标对应的文本永不改变,
// memo 稳定命中。仅用于流式途中的活跃消息;turn done 后消息从持久化数据走普通
// MarkdownText 整段渲染一次,所以切分近似(松散列表 / 后置引用定义等极少数情形)会自愈。
export const StreamingMarkdown = React.memo(function StreamingMarkdown({
  text,
  cwd,
}: {
  text: string;
  cwd?: string;
}) {
  const { committed, tail } = React.useMemo(
    () => splitStreamingMarkdown(text),
    [text],
  );
  return (
    <div className="markdown-body break-words text-sm leading-relaxed">
      {committed.map((segment, i) => (
        <MarkdownInner key={i} text={segment} cwd={cwd} />
      ))}
      {tail ? <MarkdownInner key="tail" text={tail} cwd={cwd} /> : null}
    </div>
  );
});
