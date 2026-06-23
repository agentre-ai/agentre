import * as React from "react";
import { Plus, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "../primitives";
import type { AgentColor } from "../types";

export type PickableAgent = {
  id: number;
  name: string;
  avatarColor: string;
  avatarIcon: string;
  avatarDataUrl: string;
};

export type AgentMultiPickerProps = {
  agents: PickableAgent[];
  value: number[];
  onChange: (next: number[]) => void;
  exclude?: number[];
};

function AgentMultiPicker({
  agents,
  value,
  onChange,
  exclude = [],
}: AgentMultiPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = React.useState(false);
  const selected = agents.filter((a) => value.includes(a.id));
  const candidates = agents.filter(
    (a) => !value.includes(a.id) && !exclude.includes(a.id),
  );

  const add = (id: number) => {
    onChange([...value, id]);
    setOpen(false);
  };
  const remove = (id: number) => onChange(value.filter((v) => v !== id));

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {selected.map((a) => (
        <span
          key={a.id}
          className="inline-flex items-center gap-1.5 rounded-md bg-secondary px-2 py-1 text-xs"
        >
          <AgentAvatar
            name={a.name}
            initials={a.name.charAt(0)}
            color={(a.avatarColor as AgentColor) || "agent-1"}
            avatarIcon={a.avatarIcon || undefined}
            avatarDataUrl={a.avatarDataUrl || undefined}
            size="md"
            className="size-4 rounded-sm text-[8px]"
          />
          <span className="font-medium">{a.name}</span>
          <button
            type="button"
            aria-label={t("group.new.removeMember", { name: a.name })}
            onClick={() => remove(a.id)}
            className="text-muted-foreground hover:text-foreground"
          >
            <X className="size-3" aria-hidden="true" />
          </button>
        </span>
      ))}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <Plus className="size-3" aria-hidden="true" />
            {t("group.new.addMember")}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-56 p-1">
          {candidates.length === 0 ? (
            <div className="px-2 py-3 text-center text-2xs text-muted-foreground">
              {t("group.new.noCandidates")}
            </div>
          ) : (
            candidates.map((a) => (
              <button
                key={a.id}
                type="button"
                onClick={() => add(a.id)}
                className={cn(
                  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs",
                  "hover:bg-accent",
                )}
              >
                <AgentAvatar
                  name={a.name}
                  initials={a.name.charAt(0)}
                  color={(a.avatarColor as AgentColor) || "agent-1"}
                  avatarIcon={a.avatarIcon || undefined}
                  avatarDataUrl={a.avatarDataUrl || undefined}
                  size="md"
                  className="size-5 rounded-sm text-[10px]"
                />
                <span className="font-medium">{a.name}</span>
              </button>
            ))
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}

export { AgentMultiPicker };
