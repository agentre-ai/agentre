import * as React from "react";
import ReactMarkdown, {
  type Components,
  type Options as ReactMarkdownOptions,
} from "react-markdown";
import rehypeHighlight from "rehype-highlight";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";

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

export const MarkdownText = React.memo(function MarkdownText({
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
    <div className="markdown-body break-words text-sm leading-relaxed">
      <ReactMarkdown
        components={components}
        remarkPlugins={markdownRemarkPlugins}
        rehypePlugins={markdownRehypePlugins}
        urlTransform={whitelistUrl}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
});
