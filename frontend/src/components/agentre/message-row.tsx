import * as React from "react";
import { Copy } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { copyTextWithToast } from "@/lib/clipboard-toast";
import { cn } from "@/lib/utils";

import { AgentAvatar } from "./primitives";
import type { AgentColor } from "./types";

// MESSAGE_AVATAR_CLASS：单聊/群聊统一的彩色头像尺寸(以单聊为准)。抽成常量，
// 杜绝两处各写 size-6 / size-7 的漂移。size="md" 的 size-8 被 twMerge 去重成 size-7。
export const MESSAGE_AVATAR_CLASS = "size-7 rounded-lg text-[11px]";

type MessageRowProps = Omit<React.ComponentProps<"article">, "children"> & {
  /** 逃生口：自定义头像节点(如单聊 user 的「我」灰胶囊)。传了就不渲染内置头像。 */
  avatar?: React.ReactNode;
  avatarName?: string;
  avatarColor?: AgentColor;
  avatarInitials?: string;
  /** 名字行；传 null 时不显名(单聊 user 行)。 */
  name?: React.ReactNode;
  /** 名字行右侧附加内容：时间 / 群聊「仅 X 收到」灰字。 */
  headerExtra?: React.ReactNode;
  /** 动作行 / token 行的挂载点(复制按钮)。 */
  footer?: React.ReactNode;
  children: React.ReactNode;
};

// MessageRow：单条消息的布局骨架(头像列 + 内容列)。纯展示，不取数据、不决定业务。
// 单聊 ChatMessage 与群聊 transcript 共用，保证头像尺寸/布局一致，并给群聊提供 footer 槽。
function MessageRow({
  avatar,
  avatarName = "",
  avatarColor = "agent-1",
  avatarInitials,
  name,
  headerExtra,
  footer,
  children,
  className,
  ...props
}: MessageRowProps) {
  const showHeader = name != null || headerExtra != null;
  return (
    <article className={cn("flex gap-3 text-sm", className)} {...props}>
      {avatar ?? (
        <AgentAvatar
          name={avatarName}
          initials={avatarInitials}
          color={avatarColor}
          size="md"
          className={MESSAGE_AVATAR_CLASS}
        />
      )}
      <div className="flex min-w-0 max-w-[720px] flex-1 flex-col gap-1">
        {showHeader ? (
          <div className="flex items-center gap-2">
            {name != null ? (
              <span className="font-semibold">{name}</span>
            ) : null}
            {headerExtra}
          </div>
        ) : null}
        <div
          data-selectable-text="true"
          className="flex flex-col gap-2 leading-[1.55]"
        >
          {children}
        </div>
        {footer ? (
          <div className="mt-1 flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-subtle-foreground">
            {footer}
          </div>
        ) : null}
      </div>
    </article>
  );
}

type MessageCopyButtonProps = {
  text: string;
  /** 可见文案，默认 common.copy。 */
  label?: string;
  /** aria-label，默认同可见文案。 */
  ariaLabel?: string;
  /** 复制成功 toast 标题，默认 common.copied。 */
  successTitle?: string;
  /** 复制失败 toast 标题，默认走 copyTextWithToast 内置的 common.copyFailed。 */
  errorTitle?: string;
};

// MessageCopyButton：通用「复制消息正文」按钮。text 为空时不渲染。
function MessageCopyButton({
  text,
  label,
  ariaLabel,
  successTitle,
  errorTitle,
}: MessageCopyButtonProps) {
  const { t } = useTranslation();
  if (text.length === 0) return null;
  const visible = label ?? t("common.copy");
  async function handleCopy() {
    await copyTextWithToast(text, {
      successTitle: successTitle ?? t("common.copied"),
      errorTitle,
    });
  }
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground"
      aria-label={ariaLabel ?? visible}
      onClick={() => void handleCopy()}
    >
      <Copy data-icon="inline-start" aria-hidden="true" />
      {visible}
    </Button>
  );
}

export { MessageRow, MessageCopyButton };
