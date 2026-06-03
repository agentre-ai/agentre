// Package chat_entity 维护聊天会话 / 消息的充血实体。
package chat_entity

import (
	"context"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

// allowedAgentStatuses 枚举：
//   - idle     已经收尾的会话（含 abort）
//   - running  turn 进行中，模型正在产生输出
//   - waiting  turn 进行中，等待用户操作（AskUserQuestion / ToolPermission 审批）
//   - error    turn 异常终止
//
// waiting 由 chat_svc.markSessionWaiting 在等用户操作时设置，应答后由 markSessionRunning
// 翻回 running；turn 真结束时回到 idle/error。
var allowedAgentStatuses = map[string]struct{}{
	"idle":    {},
	"running": {},
	"waiting": {},
	"error":   {},
}

// Session is one open or historical chat thread scoped to a single Agent.
type Session struct {
	ID            int64  `gorm:"column:id;primaryKey;autoIncrement"`
	AgentID       int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	Title         string `gorm:"column:title;type:text;not null;default:''"`
	AgentStatus   string `gorm:"column:agent_status;type:text;not null;default:'idle'"`
	LastMessageAt int64  `gorm:"column:last_message_at;type:bigint;not null;default:0"`
	// LastReadAt 是会话上次被用户「看到」的时间戳（unix ms）。sidebar 折叠态 attention
	// bubble 用 LastMessageAt > LastReadAt 判定「未读」；前端打开会话 + stream done 后
	// 经由 chat_svc.MarkSessionRead 向后端推进；早期 localStorage 字段现在落到 DB。
	LastReadAt        int64  `gorm:"column:last_read_at;type:bigint;not null;default:0"`
	ProviderSessionID string `gorm:"column:provider_session_id;type:text;not null;default:''"`
	// NeedsAttention 是 Wails / frontend 兼容字段，不落库。DB source of truth 是
	// AgentStatus=="waiting"；repo / service 出口会由 ApplyDerivedFields 回填。
	NeedsAttention bool `gorm:"-"`
	// ProjectID = 0 表示自由会话（保留老行为，spec Q5/B 兜底）；> 0 时受 project_svc 管控。
	ProjectID int64 `gorm:"column:project_id;type:bigint;not null;default:0"`
	// GroupID = 0 表示普通单 agent 会话；> 0 时为群聊 backing session，归属该群聊。
	// 群聊成员 session 在默认会话列表中按 group_id=0 过滤隐藏。
	GroupID int64 `gorm:"column:group_id;type:bigint;not null;default:0"`
	// ContextWindow 是 runner 在最近一轮上报的模型上下文窗口大小（tokens）：
	//   - codex：从 thread/tokenUsage/updated 的 modelContextWindow 字段落库；
	//   - claudecode / builtin：runner 不报，恒为 0，LoadSession 走 provider/catalog 兜底。
	// 0 表示尚未探到；> 0 时是 chat_svc 解析 contextWindow 时最高优先的来源。
	ContextWindow int `gorm:"column:context_window;type:int;not null;default:0"`
	// PermissionMode 是 CLI 会话模式：
	//   - claudecode: default / acceptEdits / plan / bypassPermissions
	//   - codex: default / plan
	// 空串是历史兼容值；claudecode 视为 acceptEdits，codex 视为 default。
	// chat_svc.SetPermissionMode 落库；runTurn 启动时按 backend 归一化后传给
	// claudecode --permission-mode 或 codex turn/start collaborationMode。
	// builtin 后端不读这个字段。
	PermissionMode string `gorm:"column:permission_mode;type:text;not null;default:''"`
	// PermissionModeAtLaunch 是 CLI 子进程 spawn 时下发的 --permission-mode 值的
	// 持久化快照（claudecode 专用）。runtime 通过 set_permission_mode 切换的
	// 当前模式落在 PermissionMode；本字段仅由 runner 在 spawn / respawn 成功后
	// 写入，运行时切换不会动它。前端用它决定 pill 上的 bypass 选项是否还可点：
	// 只有以 bypass 启动的 session 才能在运行时来回切回 bypass（CLI 约束）。
	PermissionModeAtLaunch string `gorm:"column:permission_mode_at_launch;type:text;not null;default:''"`
	Status                 int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime             int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime             int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Session) TableName() string { return "chat_sessions" }

func (s *Session) IsActive() bool { return s != nil && s.Status == consts.ACTIVE }

// IsWaitingForUser returns whether this session is blocked on user input such
// as AskUserQuestion or a tool permission request.
func (s *Session) IsWaitingForUser() bool {
	return s != nil && s.AgentStatus == "waiting"
}

// ApplyDerivedFields fills non-persisted compatibility fields from persisted
// state. Call this after loading sessions from storage and before projecting to
// Wails DTOs.
func (s *Session) ApplyDerivedFields() {
	if s == nil {
		return
	}
	s.NeedsAttention = s.IsWaitingForUser()
}

// HasProviderSession 是否已绑定 cago cliagent / builtin Session id。
// 空串视为未绑定 — 首条消息时由 runner 生成并回写。
func (s *Session) HasProviderSession() bool { return s != nil && s.ProviderSessionID != "" }

// SetProviderSession 写入 cago Session id；nil receiver 无操作。
func (s *Session) SetProviderSession(id string) {
	if s == nil {
		return
	}
	s.ProviderSessionID = id
}

func (s *Session) Check(ctx context.Context) error {
	if s == nil {
		return i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	if s.AgentID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, ok := allowedAgentStatuses[s.AgentStatus]; !ok {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
