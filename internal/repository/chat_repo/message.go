package chat_repo

import "github.com/agentre-hub/agentre/internal/repository/transcript_repo"

// 消息 / 块的仓储实现已抽成独立域 transcript_repo，两个宿主（桌面端 / agentred）共用
// 同一份写入、块拆分、CheckpointBlocks 差分、按定位键与类型的点查、按宿主收窄的删除
// （决策 8）。chat_repo 只留会话（见 session.go）。
//
// 这里保留的类型别名与转发函数，只是为了不强迫尚未纳入本轮范围的调用方
// （internal/bootstrap、chat_import_svc 等）跟着改 import path —— chat_repo.Message()
// 与 transcript_repo.Message() 返回同一个单例，不是两份实现。
type MessageRepo = transcript_repo.MessageRepo

// MessageUsage 转发自 transcript_repo，供 chat_svc 现有调用点（dispatcher_adapters.go
// 等）按旧名继续引用。
type MessageUsage = transcript_repo.MessageUsage

// SubagentProgress 转发自 transcript_repo，供 chat_svc 现有调用点（subagent_activity.go
// 等）按旧名继续引用。
type SubagentProgress = transcript_repo.SubagentProgress

func Message() MessageRepo             { return transcript_repo.Message() }
func RegisterMessage(impl MessageRepo) { transcript_repo.RegisterMessage(impl) }
func NewMessage() MessageRepo          { return transcript_repo.NewMessage() }
