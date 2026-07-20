/**
 * 后端错误经 Wails 过界时只剩一个字符串,chat_svc 的 operationFailedWithCause 因此把
 * 「本地化 headline + 换行 + 原始 cause」编码进 Error.message。这里按首个换行还原成两段。
 *
 * detail 是动态错误文本,不进 i18n(见 AGENTS.md:不翻译动态输出)。
 */
export function splitErrorDetail(e: unknown): {
  msg: string;
  detail?: string;
} {
  const raw = e instanceof Error ? e.message : String(e);
  const at = raw.indexOf("\n");
  if (at < 0) {
    return { msg: raw, detail: undefined };
  }
  const detail = raw.slice(at + 1).trim();
  return {
    msg: raw.slice(0, at),
    detail: detail.length > 0 ? detail : undefined,
  };
}
