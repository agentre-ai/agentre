import { toast } from "sonner";

export const COPY_TOAST_DURATION_MS = 5000;
export const COPY_TOAST_ERROR_DURATION_MS = 7000;

type CopyTextWithToastOptions = {
  errorTitle?: string;
  successDescription?: string;
  successTitle: string;
};

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

export async function copyTextWithToast(
  text: string,
  {
    errorTitle = "复制失败",
    successDescription,
    successTitle,
  }: CopyTextWithToastOptions,
): Promise<boolean> {
  try {
    if (!navigator.clipboard?.writeText) {
      throw new Error("当前环境不支持剪贴板");
    }
    await navigator.clipboard.writeText(text);
    toast.success(successTitle, {
      description: successDescription,
      duration: COPY_TOAST_DURATION_MS,
      position: "bottom-right",
    });
    return true;
  } catch (err) {
    toast.error(errorTitle, {
      description: errorMessage(err),
      duration: COPY_TOAST_ERROR_DURATION_MS,
      position: "bottom-right",
    });
    return false;
  }
}
