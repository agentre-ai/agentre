import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { app } from "../../../wailsjs/go/models";

import { toneClass } from "./issue-tones";

export function IssueLabels({ labels }: { labels: app.LabelItem[] }) {
  if (!labels || labels.length === 0) {
    return null;
  }
  return (
    <span className="flex shrink-0 flex-wrap items-center gap-1.5">
      {labels.map((label) => (
        <Badge
          variant="secondary"
          className={cn(
            "rounded-full border-0 px-2 py-px font-mono text-2xs font-semibold",
            toneClass(label.tone),
          )}
          key={label.id}
        >
          {label.name}
        </Badge>
      ))}
    </span>
  );
}
