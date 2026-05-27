import * as React from "react";
import { Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

export type CodeBlockProps = React.ComponentProps<"div"> & {
  children: React.ReactNode;
  language?: string;
};

function extractTextFromReactNode(node: React.ReactNode): string {
  if (node === null || node === undefined || typeof node === "boolean") {
    return "";
  }
  if (typeof node === "string" || typeof node === "number") {
    return String(node);
  }
  if (Array.isArray(node)) {
    return node.map(extractTextFromReactNode).join("");
  }
  if (React.isValidElement<{ children?: React.ReactNode }>(node)) {
    return extractTextFromReactNode(node.props.children);
  }
  return "";
}

export function CodeBlock({
  children,
  className,
  language = "preview",
  ...props
}: CodeBlockProps) {
  const [copyState, setCopyState] = React.useState<
    "copied" | "failed" | "idle"
  >("idle");
  const resetTimerRef = React.useRef<number | null>(null);
  const codeText = React.useMemo(
    () => extractTextFromReactNode(children),
    [children],
  );

  React.useEffect(() => {
    return () => {
      if (resetTimerRef.current !== null) {
        window.clearTimeout(resetTimerRef.current);
      }
    };
  }, []);

  async function handleCopy() {
    if (resetTimerRef.current !== null) {
      window.clearTimeout(resetTimerRef.current);
      resetTimerRef.current = null;
    }
    try {
      const copied = await copyTextWithToast(codeText, {
        errorTitle: "复制代码失败",
        successTitle: "已复制代码",
      });
      setCopyState(copied ? "copied" : "failed");
    } catch {
      setCopyState("failed");
    }
    resetTimerRef.current = window.setTimeout(() => {
      setCopyState("idle");
      resetTimerRef.current = null;
    }, 1400);
  }

  return (
    <div
      className={cn(
        "w-full max-w-[580px] overflow-hidden rounded-md border border-border bg-secondary",
        className,
      )}
      {...props}
    >
      <div className="flex items-center gap-2 border-b border-border px-2.5 py-1.5">
        <span className="font-mono text-[10px] font-semibold text-muted-foreground">
          {language}
        </span>
        <span className="min-w-0 flex-1" />
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
          onClick={() => void handleCopy()}
        >
          <Copy data-icon="inline-start" aria-hidden="true" />
          {copyState === "copied"
            ? "已复制"
            : copyState === "failed"
              ? "复制失败"
              : "复制"}
        </Button>
      </div>
      <pre
        data-selectable-text="true"
        className="overflow-auto px-3 py-2.5 font-mono text-xs leading-relaxed text-foreground"
      >
        {children}
      </pre>
    </div>
  );
}
