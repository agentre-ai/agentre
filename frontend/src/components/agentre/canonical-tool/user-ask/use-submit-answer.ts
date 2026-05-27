import { AnswerUserQuestion as wailsAnswerUserQuestion } from "../../../../../wailsjs/go/app/App";
import type { AskAnswerDTO } from "../types";

// useSubmitAnswer 把 AnswerUserQuestion IPC 包成一个朴素的 async 调用。
// 后端 UserAskResolved 事件直接更新 store,前端不做 optimistic 分支;
// 失败由后端报错 + 调用方自己处理。
export async function submitAnswer(opts: {
  sessionId: number;
  requestId: string;
  answers?: AskAnswerDTO[];
  skipped?: boolean;
}): Promise<void> {
  await wailsAnswerUserQuestion({
    sessionId: opts.sessionId,
    requestId: opts.requestId,
    answers: opts.answers,
    skipped: !!opts.skipped,
  } as Parameters<typeof wailsAnswerUserQuestion>[0]);
}
