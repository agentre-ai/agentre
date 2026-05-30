// frontend/src/lib/client-log.ts
//
// clientLog 是前端诊断日志的统一出口:既打浏览器 DevTools(开发时即时可见),
// 又通过 Wails LogClient binding 落进后端 agentre.log(复现/线上行为可追溯)。
//
// 为什么需要它:有些运行时事件「只有前端能测到」—— 典型如活跃 LiveStream 时
// session 状态被过期 DB 快照覆盖的竞态。这类事件进不了后端业务日志,以前只能靠
// DevTools 抓现行,排查时 agentre.log 一片干净。clientLog 把这些埋点桥到后端日志,
// 跟 chat_svc 的 turn 生命周期日志(turn starting / agent_status finalized /
// LoadSession served)对得上时间线。
//
// 设计:
//   - fire-and-forget:binding 调用不 await、吞掉 reject —— headless/测试环境或
//     binding 尚未注入(window.go 不存在)时,绝不让调用方崩。
//   - 不做采样/限流:调用点通常已在「命中异常分支」的 if 里,频率天然很低。
import { LogClient } from "../../wailsjs/go/app/App";

export type ClientLogLevel = "error" | "warn" | "info" | "debug";
export type ClientLogFields = Record<string, unknown>;

function consoleFor(level: ClientLogLevel): (...args: unknown[]) => void {
  if (level === "error") return console.error;
  if (level === "info" || level === "debug") return console.info;
  return console.warn;
}

function emit(
  level: ClientLogLevel,
  scope: string,
  message: string,
  fields?: ClientLogFields,
): void {
  // 1) DevTools:保留开发期即时可见性。
  consoleFor(level)(`[${scope}] ${message}`, fields ?? {});

  // 2) 后端桥:落 agentre.log。binding 不可用时静默降级(同步抛 / 异步 reject 都吞)。
  try {
    void Promise.resolve(LogClient({ level, scope, message, fields })).catch(
      () => {
        /* binding 不可用(headless / 尚未 bind)时忽略 */
      },
    );
  } catch {
    /* binding 未注入时 LogClient 本身可能同步抛,忽略 */
  }
}

export const clientLog = {
  error: (scope: string, message: string, fields?: ClientLogFields) =>
    emit("error", scope, message, fields),
  warn: (scope: string, message: string, fields?: ClientLogFields) =>
    emit("warn", scope, message, fields),
  info: (scope: string, message: string, fields?: ClientLogFields) =>
    emit("info", scope, message, fields),
  debug: (scope: string, message: string, fields?: ClientLogFields) =>
    emit("debug", scope, message, fields),
};
