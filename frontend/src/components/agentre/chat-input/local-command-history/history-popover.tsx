import * as React from "react";
import { Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { SuggestionPopover } from "../suggestion-popover";
import type {
  LocalCommandHistoryEntry,
  LocalCommandHistoryMenuState,
} from "./types";

export function LocalCommandHistoryPopover({
  state,
  onPick,
  onHover,
  onClear,
}: {
  state: LocalCommandHistoryMenuState;
  onPick: (entry: LocalCommandHistoryEntry) => void;
  onHover: (index: number) => void;
  onClear: () => void;
}): React.ReactElement | null {
  const { t } = useTranslation();

  return (
    <SuggestionPopover
      open={state.open}
      anchorRect={state.anchorRect}
      selectedIndex={state.selectedIndex}
      itemCount={state.items.length}
      ariaLabel={t("localCommandHistory.aria")}
      footer={(activeRef) => {
        const active = state.selectedIndex === state.items.length;
        return (
          <div className="mt-1 border-t border-border pt-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              ref={active ? activeRef : undefined}
              tabIndex={-1}
              aria-label={t("localCommandHistory.clearCurrentScope")}
              aria-current={active ? "true" : undefined}
              className={cn(
                "h-auto w-full justify-start rounded-sm px-2 py-1.5 text-xs",
                active
                  ? "bg-accent text-accent-foreground"
                  : "text-muted-foreground hover:text-foreground",
              )}
              onMouseMove={() => onHover(state.items.length)}
              onMouseDown={(event) => event.preventDefault()}
              onClick={onClear}
            >
              <Trash2 className="size-3" aria-hidden="true" />
              {t("localCommandHistory.clearCurrentScope")}
            </Button>
          </div>
        );
      }}
    >
      {(activeRef) =>
        state.items.map((entry, index) => {
          const active = index === state.selectedIndex;
          return (
            <button
              key={entry.command}
              type="button"
              role="option"
              ref={active ? activeRef : undefined}
              aria-label={entry.command}
              aria-selected={active}
              onMouseMove={() => onHover(index)}
              onMouseDown={(event) => {
                event.preventDefault();
                onPick(entry);
              }}
              className={cn(
                "flex w-full min-w-0 cursor-pointer items-center rounded-sm px-2 py-1.5 text-left text-xs",
                active ? "bg-accent text-accent-foreground" : "text-foreground",
              )}
            >
              <span className="min-w-0 flex-1 truncate font-mono">
                {entry.command}
              </span>
            </button>
          );
        })
      }
    </SuggestionPopover>
  );
}
