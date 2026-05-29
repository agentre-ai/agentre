// frontend/src/components/agentre/remote-devices/device-action-menu.tsx
import {
  MoreHorizontal,
  RotateCw,
  Edit3,
  Settings2,
  Trash2,
  Activity,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

type Props = {
  onRefresh: () => void;
  onRename: () => void;
  onEditTLS: () => void;
  onRemove: () => void;
  onToggleProviders?: () => void;
};

export function DeviceActionMenu(props: Props) {
  const { t } = useTranslation();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("common.moreActions")}
        >
          <MoreHorizontal className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={props.onRefresh}>
          <RotateCw className="mr-2 h-4 w-4" />
          {t("remoteDevices.actions.refreshStatus")}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={props.onRename}>
          <Edit3 className="mr-2 h-4 w-4" />
          {t("remoteDevices.actions.rename")}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={props.onEditTLS}>
          <Settings2 className="mr-2 h-4 w-4" />
          {t("remoteDevices.actions.editTls")}
        </DropdownMenuItem>
        {props.onToggleProviders ? (
          <DropdownMenuItem onSelect={props.onToggleProviders}>
            <Activity className="mr-2 h-4 w-4" />
            {t("remoteDevices.providers.title")}
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={props.onRemove}
          className="text-destructive"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          {t("remoteDevices.actions.removePairing")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
