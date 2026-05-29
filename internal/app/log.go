package app

import (
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
)

// ClientLogEntry 是前端通过 LogClient binding 上报的一条诊断日志。
//
// 前端的 console.warn 只进浏览器 DevTools,排查线上/复现行为时拿不到 —— 尤其是
// 「活跃 LiveStream 时 session 状态被过期 DB 快照覆盖」这类只有前端能测到的竞态,
// 后端业务日志完全看不到。这条桥把前端埋点落进 agentre.log,跟后端 turn 生命周期
// 日志(turn starting / agent_status finalized / LoadSession served)能对上时间线。
type ClientLogEntry struct {
	// Level: "error" | "warn" | "info" | "debug";其它值按 warn 处理。
	Level string `json:"level"`
	// Scope: 调用点标识,如 "use-chat-session" / "chat-streams-host"。
	Scope string `json:"scope"`
	// Message: 人类可读的事件描述。
	Message string `json:"message"`
	// Fields: 结构化上下文(sessionId / prevAgentStatus / loadedAgentStatus 等)。
	Fields map[string]any `json:"fields,omitempty"`
}

// LogClient 把前端上报的诊断日志落进后端 zap 日志(agentre.log)。
// 故意做成「薄桥」:只按 level 分流 + 附带 scope/fields,不掺任何业务判断
// (符合 CLAUDE.md「不要把测得了的逻辑塞进 App」)。
func (a *App) LogClient(entry ClientLogEntry) {
	fields := []zap.Field{
		zap.String("scope", entry.Scope),
		zap.String("source", "client"),
	}
	if len(entry.Fields) > 0 {
		fields = append(fields, zap.Any("fields", entry.Fields))
	}
	msg := "client: " + entry.Message
	switch entry.Level {
	case "error":
		logger.Default().Error(msg, fields...)
	case "info":
		logger.Default().Info(msg, fields...)
	case "debug":
		logger.Default().Debug(msg, fields...)
	default:
		logger.Default().Warn(msg, fields...)
	}
}
