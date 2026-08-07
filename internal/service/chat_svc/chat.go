// Package chat_svc 提供聊天会话 / 消息的业务逻辑层。
package chat_svc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/gogo"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	"go.uber.org/zap"
	"gorm.io/gorm"

	daemonrpc "github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"

	// 显式 blank import 触发本地 runtime 子包 init() 把 *Runtime 注册到 RuntimeFor。
	// remote 是显式构造,不参与全局注册;以下三种为本地后端,必须自注册才能被
	// selectRunner 解析到。claudecodert 别名避免与 pkg/claudecode CLI 库名字撞车。
	_ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/builtin"
	claudecodert "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/claudecode"
	codexrt "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/codex"
	_ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/openclaw"
	piagentrt "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/pkg/llmcatalog"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/handlers"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/view"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	"github.com/agentre-ai/agentre/pkg/claudecode"
)

const (
	maxSendImages         = 4
	maxSendImageBytes     = 5 * 1024 * 1024
	dataURLBase64Token    = ";base64,"
	piStopAbortWriteBound = 500 * time.Millisecond
)

var sendImageMediaTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
}

type ChatSvc interface {
	ListAgents(ctx context.Context, req *ListAgentsRequest) (*ListAgentsResponse, error)
	ListAgentSessions(ctx context.Context, req *ListAgentSessionsRequest) (*ListAgentSessionsResponse, error)
	LoadSession(ctx context.Context, req *LoadSessionRequest) (*LoadSessionResponse, error)
	GetLaunchCommand(ctx context.Context, req *LaunchCommandRequest) (*LaunchCommandResponse, error)
	GetSessionGitState(ctx context.Context, req *GetSessionGitStateRequest) (*GetSessionGitStateResponse, error)
	// ResolveSessionWorkspace 把 sessionID 解析成 {deviceID, cwd}(deviceID 为 0
	// 即本机会话)。实现 workspace_fs_svc 的 SessionWorkspaceResolver 窄接口,
	// 由 bootstrap 注入 —— 让那个服务不必跨域读 chat / agent / agent_backend 表。
	ResolveSessionWorkspace(ctx context.Context, sessionID int64) (deviceID int64, cwd string, err error)
	// ResolveLocalCommandScope 为已有 session 或未持久化的 agent/project 目标解析历史作用域。
	ResolveLocalCommandScope(ctx context.Context, req *ResolveLocalCommandScopeRequest) (*LocalCommandScope, error)
	Send(ctx context.Context, req *SendRequest) (*SendResponse, error)
	Compact(ctx context.Context, req *CompactRequest) (*CompactResponse, error)
	GetGoal(ctx context.Context, req *GoalRequest) (*GoalResponse, error)
	SetGoal(ctx context.Context, req *SetGoalRequest) (*GoalResponse, error)
	StartGoal(ctx context.Context, req *StartGoalRequest) (*StartGoalResponse, error)
	ClearGoal(ctx context.Context, req *ClearGoalRequest) (*ClearGoalResponse, error)
	Enqueue(ctx context.Context, req *EnqueueRequest) (*EnqueueResponse, error)
	CancelQueued(ctx context.Context, req *CancelQueuedRequest) (*CancelQueuedResponse, error)
	Stop(ctx context.Context, req *StopRequest) (*StopResponse, error)
	StopBackgroundTask(ctx context.Context, req *StopBackgroundTaskRequest) (*StopBackgroundTaskResponse, error)
	SetPermissionMode(ctx context.Context, req *SetPermissionModeRequest) (*SetPermissionModeResponse, error)
	SetSessionModel(ctx context.Context, sessionID int64, model string) error
	Regenerate(ctx context.Context, req *RegenerateRequest) (*SendResponse, error)
	Edit(ctx context.Context, req *EditRequest) (*SendResponse, error)
	Rename(ctx context.Context, req *RenameRequest) (*RenameResponse, error)
	Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
	MarkSessionRead(ctx context.Context, req *MarkSessionReadRequest) (*MarkSessionReadResponse, error)
	AnswerUserQuestion(ctx context.Context, req *AnswerUserQuestionRequest) (*AnswerUserQuestionResponse, error)
	AnswerToolPermission(ctx context.Context, req *AnswerToolPermissionRequest) (*AnswerToolPermissionResponse, error)
	ResolveExecApproval(ctx context.Context, req *ResolveExecApprovalRequest) (*ResolveExecApprovalResponse, error)
	ResolvePlanAction(ctx context.Context, req *ResolvePlanActionRequest) (*ResolvePlanActionResponse, error)
	// EnsureSession 是 chat_sessions 的统一创建/复用边界。其它 domain 不直接写 chat_repo.Session().Create。
	EnsureSession(ctx context.Context, req *EnsureSessionRequest) (*EnsureSessionResponse, error)
	// ObserveTurn 订阅指定 session 下一次 turn 完成(服务端, 不经 Wails)。
	ObserveTurn(sessionID int64) (<-chan TurnResult, func())
	// AgentBackendHasCapability 报告某 agent 的后端 runtime 是否声明指定能力(领域无关探针)。
	// 后端缺失/类型无法解析 → (false, nil)。MVP 仅解析本地 runtime; 远程后端目前返回 (false, nil)。
	AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error)
	CountActiveSessions(ctx context.Context) (int, error)
	// BeginToolApproval 在 sessionID 当前活跃 turn 上登记一条 pending 工具审批、推流,
	// 并返回等待 channel;无活跃 turn → error(工具 MCP handler 据此拒绝工具调用)。
	// org / hook 等内置写工具共用此入口。
	BeginToolApproval(ctx context.Context, sessionID int64, blk *chatblocks.ToolApprovalBlock) (<-chan bool, error)
	// AnswerToolApproval 按 requestID 唤醒挂起的写工具调用(前端审批入口的唯一后端方法);
	// 未知/重复/已超时 → error。
	AnswerToolApproval(ctx context.Context, sessionID int64, requestID string, allow bool) error
	// FinishToolApproval 把审批置为终态(approved/denied/expired)并推 resolved 事件;
	// requestID 不存在(已被 finalize 取走)→ error。
	FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
	// FinalAssistantText 读取某 assistant message 的纯文本(拼接所有 TextBlock)。
	FinalAssistantText(ctx context.Context, messageID int64) (string, error)
	// LatestAssistantText 按 sessionID 取末条 assistant 文本(running peek;无 → 空串)。
	LatestAssistantText(ctx context.Context, sessionID int64) (string, error)
	// SessionProjectID 返回某会话所属的 project id(0=未挂项目);子 agent 工具用它继承调用方项目/cwd。
	SessionProjectID(ctx context.Context, sessionID int64) (int64, error)
	// CatchUpRemoteSessions 在 App 启动后按 chat_sessions.exec_device_id 连回各台配对
	// daemon,把桌面端离线期间远端产生的转录与待决策补回来。见 remote_catchup.go。
	CatchUpRemoteSessions(ctx context.Context) error
	// CatchUpRemoteDevice 在某台 daemon 重新上线时补上启动那次没做成的补齐(启动补齐
	// 只跑一次,开机自启早于网络就绪的那一次拨号失败否则就是终局)。见 remote_catchup.go。
	CatchUpRemoteDevice(ctx context.Context, deviceID int64) error
}

var defaultChat ChatSvc

var defaultGateway httpgateway.TokenIssuer

func Chat() ChatSvc { return defaultChat }

func RegisterChat(impl ChatSvc) {
	if s, ok := impl.(*chatSvc); ok && s.gateway == nil {
		s.gateway = defaultGateway
	}
	defaultChat = impl
}

func NewChat(emitter Emitter) ChatSvc {
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	s := &chatSvc{
		emitter:       emitter,
		locks:         &sync.Map{},
		activeCancels: &sync.Map{},
		aborted:       &sync.Map{},
		turnObservers: &sync.Map{},
		toolApprovals: map[int64][]*chatblocks.ToolApprovalBlock{},
		gateway:       defaultGateway,
	}
	s.dispatcher = newPackageDispatcher(s)
	return s
}

// RegisterGateway 由 bootstrap 注入 httpgateway 单例；
// 没有注入时（早期单测、headless 启动）走 CLI 自身 login 路径。
func RegisterGateway(g httpgateway.TokenIssuer) {
	defaultGateway = g
	if s, ok := defaultChat.(*chatSvc); ok {
		s.gateway = g
	}
}

const renameTitleMaxRunes = 200

type activeTurnControl struct {
	cancel context.CancelFunc

	mu            sync.RWMutex
	gracefulAbort agentruntime.Aborter
}

func (c *activeTurnControl) setGracefulAbort(aborter agentruntime.Aborter) {
	if c == nil || aborter == nil {
		return
	}
	c.mu.Lock()
	c.gracefulAbort = aborter
	c.mu.Unlock()
}

func (c *activeTurnControl) gracefulAborter() (agentruntime.Aborter, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gracefulAbort, c.gracefulAbort != nil
}

type chatSvc struct {
	emitter Emitter
	// dispatcher 是 svc-bound turn.Dispatcher,注册了带 chat_svc 适配器的 18 个 handler。
	// 在 NewChat 时构造一次(svc-bound,handlers 的 Writer/Persister 持 *chatSvc 引用)。
	// AGENTRE_NEW_DISPATCHER=1 时 runTurn drain loop 通过它处理 Event;默认关。
	dispatcher *turn.Dispatcher
	locks      *sync.Map
	// activeCancels：sessionID(int64) → *activeTurnControl。控制对象也是 generation
	// token；旧 turn 收尾只 CompareAndDelete 自己，不能删掉即时重试的新 cancel。
	activeCancels *sync.Map
	// aborted：sessionID(int64) → struct{}。Stop 触发时 store；runTurn 收尾时
	// LoadAndDelete 判定是否走 StreamAborted 路径 + 跳过 DrainPending 自动接续。
	aborted *sync.Map
	// activeTurnStreams: sessionID(int64) → 当前活跃 turn 的 per-turn 流名(string)。
	// runTurn 起止维护;工具审批(BeginToolApproval)据此路由审批卡到正确的流。
	activeTurnStreams sync.Map
	// toolApprovals: 本会话进行中 turn 上挂起/已决的工具审批 block(org / hook 等内置
	// 写工具共用),finalize 时 merge 进 assistant 消息;LoadSession 时 overlay 到投影。
	toolApprovalsMu sync.Mutex
	toolApprovals   map[int64][]*chatblocks.ToolApprovalBlock
	// toolApprovalWaiters: requestID(string) → chan bool(buffered=1)。BeginToolApproval
	// 登记,AnswerToolApproval LoadAndDelete 后回灌决策,FinishToolApproval 终态兜底清。
	toolApprovalWaiters sync.Map
	// turnObservers：sessionID(int64) → *sync.Map(chan TurnResult → struct{})。
	// 服务端 turn 完成观察口(不经 Wails);调度方在 Send 前 ObserveTurn 订阅,
	// finalize / failTurn 各回灌恰好一条终态用于释放调度位 + 判定 quiesce。
	turnObservers *sync.Map
	// autoWatchers：sessionID(int64) → struct{}。startAutonomousWatcher 用它防同一
	// session 重复起 watcher goroutine(每会话一个,惰性启动);watcher 在底层
	// AutonomousTurns channel close(子进程 evict / CloseSession)时退出并清这条。
	autoWatchers sync.Map
	// catchUpPending：deviceID(int64) → struct{}。启动补齐时拨不通(或补齐半途出错)的
	// 设备记在这里 —— 那一刻拿不到任何判据,会话一行都不能碰。设备监视报它重新上线时
	// 由 CatchUpRemoteDevice 重来一次并清掉这条。
	catchUpPending sync.Map
	// subagentActivityWatchers：sessionID(int64) → struct{}。startSubagentActivityWatcher
	// 用它防同一 session 重复起后台 subagent 活动 watcher(每会话一个,惰性启动);watcher
	// 在底层 SubagentActivity channel close(子进程 evict / CloseSession)时退出并清这条。
	subagentActivityWatchers sync.Map
	// bgRunning: sessionID(int64) → *bgRunningSet。per-session「运行中后台 subagent 的
	// tool_use_id 集合」。集合非空 = 该会话有后台 subagent 在跑。后台 subagent 易失
	// (随 CLI 子进程/重启消失)，故不落库；重启后 map 空 = 0 天然正确。见 bg_running.go。
	bgRunning sync.Map
	gateway   httpgateway.TokenIssuer
	// chatTokens 缓存每个 chat session 的常驻 gateway token(sessionID int64 → token string)。
	// 该 token 在 spawn 时烤进 claude 子进程 env 给 PostToolUse hook 用,子进程跨轮复用
	// 时 env 不重建 —— 所以 token 必须签成永久(ttl=0)并跨轮稳定复用,否则长会话(>15min)
	// 会让 hook 拿过期 token 撞 401、steer 整轮 drain 不到。session 删除时 revokeChatToken
	// 撤销 + 清缓存。
	chatTokens sync.Map

	// remoteCache 是 device → (runtime, lease) 的 session 引用计数缓存。
	// runtime 复用底层 lease.Client(),lease 由 remote_device_svc.Pool 管理 conn
	// 复用 + idle 回收 + daemon drop evict。lease.Closed() 关闭时 watchLeaseClosed
	// 把 entry 从 map 摘掉,下次 borrow 走冷路径重建。
	remoteMu    sync.Mutex
	remoteCache map[int64]*remoteRuntimeEntry
	// connStates: sessionID(int64) → 该会话此刻偏离缺省的连接态。onRemoteConnState
	// (chat:conn:<sid> 的发布方)维护,LoadSession 同步读。见 remote_reconnect.go。
	connMu     sync.Mutex
	connStates map[int64]remote.ConnState
	// testHookPool 如果非 nil,代替 remote_device_svc.Default().Pool() 用于测试注入。
	testHookPool remote_device_svc.ConnPool
}

// ── ListAgents ───────────────────────────────────────────────────────────────

// CountActiveSessions 返回正在进行(running|waiting)的会话总数,供退出二次确认判断。
func (s *chatSvc) CountActiveSessions(ctx context.Context) (int, error) {
	n, err := chat_repo.Session().CountActive(ctx, []string{"running", "waiting"})
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *chatSvc) ListAgents(ctx context.Context, _ *ListAgentsRequest) (*ListAgentsResponse, error) {
	agents, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	resp := &ListAgentsResponse{Agents: make([]ChatAgentItem, 0, len(agents))}
	if len(agents) == 0 {
		return resp, nil
	}

	backendIDs := uniqueNonZeroBackendIDs(agents)
	backends, err := agent_backend_repo.AgentBackend().BatchFind(ctx, backendIDs)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	providerKeys := uniqueProviderKeys(backends)
	providers, err := llm_provider_repo.LLMProvider().BatchFindByKey(ctx, providerKeys)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}

	// 批量查远端 device 视图，避免 per-agent 单次查询的 N+1 问题。
	deviceIDSet := map[int64]struct{}{}
	for _, be := range backends {
		if be == nil || !be.IsRemote() {
			continue
		}
		if id, ok := be.DeviceIDInt(); ok {
			deviceIDSet[id] = struct{}{}
		}
	}
	deviceViews := map[int64]*remote_device_svc.DeviceView{}
	if rds := remote_device_svc.Default(); rds != nil {
		for id := range deviceIDSet {
			if dv, derr := rds.Get(ctx, id); derr == nil && dv != nil {
				deviceViews[id] = dv
			}
			// missing device → leave DeviceID populated but DeviceName empty + Online false.
		}
	}

	agentIDs := make([]int64, 0, len(agents))
	for _, a := range agents {
		agentIDs = append(agentIDs, a.ID)
	}
	counts, err := chat_repo.Session().CountRunningByAgents(ctx, agentIDs)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	totals, err := chat_repo.Session().CountByAgentsIncludingGroups(ctx, agentIDs)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	sessionIDs, err := chat_repo.Session().ListIDsByAgentsIncludingGroups(ctx, agentIDs)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}

	for _, a := range agents {
		ids := sessionIDs[a.ID]
		if ids == nil {
			ids = []int64{}
		}
		item := ChatAgentItem{
			ID:            a.ID,
			Name:          a.Name,
			AvatarColor:   a.AvatarColor,
			AvatarIcon:    a.AvatarIcon,
			AvatarDataURL: a.AvatarDataURL,
			Pinned:        a.IsSystem() || a.Pinned,
			ActiveCount:   counts[a.ID],
			TotalSessions: totals[a.ID],
			SessionIDs:    ids,
		}
		if be := backends[a.AgentBackendID]; be != nil {
			item.BackendType = be.Type
			item.LLMProviderKey = be.LLMProviderKey
			if agent_backend_entity.BackendType(be.Type) == agent_backend_entity.TypeClaudeCode {
				// 仅 claudecode 透出；entity.Check 限定其它后端为空串。
				item.DefaultPermissionMode = be.DefaultPermissionMode
			}
			// 远端 device 归属字段。
			item.DeviceID = be.DeviceID
			if id, ok := be.DeviceIDInt(); ok {
				if dv := deviceViews[id]; dv != nil {
					item.DeviceName = dv.Name
					item.Online = dv.Online
				}
			}
			switch agent_backend_entity.BackendType(be.Type) {
			case agent_backend_entity.TypeBuiltin:
				if prov := providers[be.LLMProviderKey]; prov != nil && prov.IsActive() {
					item.Chattable = true
				} else {
					item.ChattableHint = "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
				}
			case agent_backend_entity.TypeClaudeCode, agent_backend_entity.TypeCodex, agent_backend_entity.TypePiAgent:
				if be.LLMProviderKey == "" {
					// 走 CLI 自身 login；这里不做可达性探测，启动失败由 chat turn 兜底报错。
					item.Chattable = true
				} else if prov := providers[be.LLMProviderKey]; prov == nil || !prov.IsActive() {
					item.ChattableHint = "请先在设置 → LLM 供应商激活该 Agent 后端关联的供应商"
				} else if remoteProviderKnownMissing(be) {
					item.ChattableHint = fmt.Sprintf("远端 agentred 未配置该供应商，请在远端执行 agentred llm add --key=%s 并填写 API Key", be.LLMProviderKey)
				} else if be.IsRemote() {
					item.Chattable = true
				} else if s.gateway == nil || s.gateway.Status().State != "running" {
					item.ChattableHint = "本地网关未启动，CLI 后端暂不可用"
				} else {
					item.Chattable = true
				}
			case agent_backend_entity.TypeOpenClaw:
				if be.IsRemote() {
					item.ChattableHint = "远端 OpenClaw 暂不可用：agentred 尚无安全的 secret enrollment/reference"
				} else {
					item.Chattable = true
				}
			default:
				item.ChattableHint = "未知 Agent 后端类型"
			}
		} else if a.IsSystem() {
			item.ChattableHint = "CEO 助手还没配置后端，请在组织架构页选择一个 Agent 后端"
		} else {
			item.ChattableHint = "该 Agent 还没配置后端，请在组织架构页选择一个 Agent 后端"
		}

		sessions, err := chat_repo.Session().ListByAgentIncludingGroups(ctx, a.ID, 5)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		item.RecentCount = len(sessions)
		item.Sessions = make([]ChatSessionLite, 0, len(sessions))
		for _, sess := range sessions {
			item.Sessions = append(item.Sessions, s.sessionLiteFromEntity(sess))
		}

		// sidebar 折叠态 attention bubble：拉所有 running/waiting/error 会话。
		// 不受 5 行常规列表的约束；limit=20 防异常数据撑爆 UI，前端去重与本组 sessions 的重叠。
		attention, err := chat_repo.Session().ListAttentionByAgentIncludingGroups(ctx, a.ID, 20)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		item.AttentionSessions = make([]ChatSessionLite, 0, len(attention))
		for _, sess := range attention {
			item.AttentionSessions = append(item.AttentionSessions, s.sessionLiteFromEntity(sess))
		}
		resp.Agents = append(resp.Agents, item)
	}
	return resp, nil
}

// ── ListAgentSessions ────────────────────────────────────────────────────────

// ListAgentSessions 给「查看全部 N 个会话」popover 翻页拉数据用。
// 服务侧在这里做参数 clamp（offset≥0、limit∈[1,100]），repo 只忠实按参数查；
// hasMore 按 offset+len < total 判定，让前端不用自己算页数。
const (
	listAgentSessionsDefaultLimit = 20
	listAgentSessionsMaxLimit     = 100
)

func (s *chatSvc) ListAgentSessions(ctx context.Context, req *ListAgentSessionsRequest) (*ListAgentSessionsResponse, error) {
	if req == nil || req.AgentID <= 0 || req.Offset < 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = listAgentSessionsDefaultLimit
	}
	if limit > listAgentSessionsMaxLimit {
		limit = listAgentSessionsMaxLimit
	}

	sessions, err := chat_repo.Session().ListByAgentPagedIncludingGroups(ctx, req.AgentID, req.Offset, limit)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	total, err := chat_repo.Session().CountByAgentIncludingGroups(ctx, req.AgentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}

	resp := &ListAgentSessionsResponse{
		Sessions: make([]ChatSessionLite, 0, len(sessions)),
		Total:    total,
		HasMore:  int64(req.Offset+len(sessions)) < total,
	}
	for _, sess := range sessions {
		resp.Sessions = append(resp.Sessions, s.sessionLiteFromEntity(sess))
	}
	return resp, nil
}

func (s *chatSvc) sessionLiteFromEntity(sess *chat_entity.Session) ChatSessionLite {
	if sess == nil {
		return ChatSessionLite{}
	}
	return ChatSessionLite{
		ID:             sess.ID,
		Title:          sess.Title,
		Status:         sess.AgentStatus,
		NeedsAttention: sess.IsWaitingForUser(),
		BgRunning:      s.bgRunningActive(sess.ID),
		LastMessageAt:  sess.LastMessageAt,
		LastReadAt:     sess.LastReadAt,
	}
}

// activeStreamName 给 LoadSession 用:turn 进行中时,让中途打开该会话的前端能重挂到
// per-turn 实时流。per-turn 流名只在用户主动 Send 时由响应给出;子 agent 调用轮 / 自主轮等"非
// 前端发起"的 turn 前端拿不到这个名字 —— 这里按在跑 turn 的(末条)assistant 消息把它重建出来,
// 前端据此 openStream 续看。无活跃 turn / 还没建出 assistant 消息时返回空串。
func activeStreamName(activeTurn bool, sessionID int64, msgs []*chat_entity.Message) string {
	if !activeTurn {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == "assistant" {
			return StreamName(sessionID, msgs[i].ID)
		}
	}
	return ""
}

func (s *chatSvc) LoadSession(ctx context.Context, req *LoadSessionRequest) (*LoadSessionResponse, error) {
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	msgs, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	resp := &LoadSessionResponse{
		Session: ChatSessionDetail{
			ID:                     sess.ID,
			AgentID:                sess.AgentID,
			ModelOverride:          sess.ModelOverride,
			Title:                  sess.Title,
			AgentStatus:            sess.AgentStatus,
			NeedsAttention:         sess.IsWaitingForUser(),
			BgRunning:              s.bgRunningActive(sess.ID),
			LastMessageAt:          sess.LastMessageAt,
			LastReadAt:             sess.LastReadAt,
			Createtime:             sess.Createtime,
			PermissionMode:         sess.PermissionMode,
			PermissionModeAtLaunch: sess.PermissionModeAtLaunch,
			ProjectID:              sess.ProjectID,
		},
		Messages: make([]ChatMessage, 0, len(msgs)),
	}
	// 诊断: 记录这次 serve 出去的 agentStatus + 后端此刻是否有活跃 turn。
	// 前端「过期快照覆盖 running」竞态在 serve 端通常看着无辜(turn 还没起、DB 还是
	// idle),但若 serve 时已有活跃 turn 却吐 非 running/waiting,就是后端侧能直接抓到
	// 的不一致。配合前端 LogClient 上报的 apply 时刻能把竞态时间线对上。
	_, activeTurn := s.activeCancels.Load(sess.ID)
	// ActiveStream 让中途打开本会话的前端重挂到 per-turn 实时流(子 agent 调用轮 / 自主轮等
	// 非前端发起的 turn 没有 Send 响应入口)。无活跃 turn 时为空,前端不重挂。
	resp.Session.ActiveStream = activeStreamName(activeTurn, sess.ID, msgs)
	// 连接态随响应同步返回:重挂上来的前端要在订阅 chat:conn:<sid> 之前就知道这条会话
	// 此刻是不是断着的,否则整个退避窗口里只剩打字指示器。
	resp.Session.ConnectionState = string(s.sessionConnState(sess.ID))
	if activeTurn &&
		sess.AgentStatus != "running" && sess.AgentStatus != "waiting" {
		logger.Ctx(ctx).Warn("chat_svc: LoadSession served non-running status while turn active",
			zap.Int64("sessionId", sess.ID),
			zap.String("agentStatus", sess.AgentStatus),
			zap.Bool("activeTurn", true))
	} else {
		logger.Ctx(ctx).Debug("chat_svc: LoadSession served",
			zap.Int64("sessionId", sess.ID),
			zap.String("agentStatus", sess.AgentStatus))
	}
	if a != nil {
		resp.Session.AgentName = a.Name
		resp.Session.AgentColor = a.AvatarColor
		resp.Session.AgentIcon = a.AvatarIcon
		resp.Session.AgentAvatarDataURL = a.AvatarDataURL
		// BackendType 给前端判断「复制启动命令」这类仅 CLI 后端有效的菜单是否显示。
		// 上下文窗口走统一优先级 resolveContextWindowWithRuntime：
		//   runtime 上报（session.ContextWindow）> 用户配置（provider.ContextWindow）
		//   > latestAssistantModel catalog > provider.Model catalog。
		// backend 不存在或无 provider 时仍尝试用 latestAssistantModel 兜底；都没有 → 0。
		// 查询失败一律不阻塞加载会话本身。
		var prov *llm_provider_entity.LLMProvider
		var be *agent_backend_entity.AgentBackend
		if a.AgentBackendID > 0 {
			if be, _ = agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID); be != nil {
				resp.Session.BackendType = be.Type
				if be.LLMProviderKey != "" {
					prov, _ = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
				}
			}
		}
		if prov != nil {
			resp.Session.LLMProviderType = prov.Type
			resp.Session.LLMProviderKey = be.LLMProviderKey
			resp.Session.ProviderDefaultModel = strings.TrimSpace(prov.Model)
		}
		resp.Session.ContextWindow = resolveContextWindowWithRuntime(sess, prov, msgs)

		// Device + cwd 信息: 给前端 chat header 渲染"远端运行 · /home/me/proj"小字使用。
		// be 解析失败 / device 离线 / cwd 查询失败时容忍降级 (字段留空,不让 LoadSession 整体失败);
		// 降级路径都补 debug log,给排查 blank DeviceName / missing Cwd 留信号。
		if be != nil {
			resp.Session.DeviceID = be.DeviceID
			if id, ok := be.DeviceIDInt(); ok {
				if rds := remote_device_svc.Default(); rds != nil {
					if dv, derr := rds.Get(ctx, id); derr == nil && dv != nil {
						resp.Session.DeviceName = dv.Name
						resp.Session.Online = dv.Online
					} else if derr != nil {
						logger.Ctx(ctx).Debug("LoadSession: device lookup degraded",
							zap.Int64("deviceID", id),
							zap.Int64("sessionID", sess.ID),
							zap.Error(derr))
					}
				}
			}
			if cwd, cerr := resolveSessionCwd(ctx, sess, be); cerr == nil {
				resp.Session.Cwd = cwd
			} else {
				logger.Ctx(ctx).Debug("LoadSession: cwd resolve degraded",
					zap.Int64("sessionID", sess.ID),
					zap.Error(cerr))
			}
		}
	}
	for _, m := range msgs {
		cm, err := toChatMessage(m)
		if err != nil {
			return nil, i18n.NewError(ctx, code.ChatBlocksMalformed)
		}
		resp.Messages = append(resp.Messages, cm)
	}
	// 进行中 turn 上挂起/已决的审批 block 还没 finalize 进消息行,overlay 到末条
	// assistant 消息的投影,中途打开会话也能看到审批卡(finalize 时会真正落库)。
	if pend := s.snapshotToolApprovals(sess.ID); len(pend) > 0 {
		for i := len(resp.Messages) - 1; i >= 0; i-- {
			if resp.Messages[i].Role == "assistant" {
				for _, b := range pend {
					resp.Messages[i].Blocks = append(resp.Messages[i].Blocks, toolApprovalBlockToChatBlock(b))
				}
				break
			}
		}
	}
	return resp, nil
}

// GetLaunchCommand 把当前 session 关联的 CLI 后端配置拼成一条人类可读、可在终端
// 粘贴运行的命令。Token 故意写成占位符 <TOKEN>，不发放实际 token —— 用户自行替换。
//
// builtin 后端没有外部 CLI，直接返回 ChatLaunchCommandNotAvailable。
func (s *chatSvc) GetLaunchCommand(ctx context.Context, req *LaunchCommandRequest) (*LaunchCommandResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if a == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	if a.AgentBackendID <= 0 {
		return nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if be.IsBuiltin() {
		return nil, i18n.NewError(ctx, code.ChatLaunchCommandNotAvailable)
	}

	var prov *llm_provider_entity.LLMProvider
	if be.LLMProviderKey != "" {
		prov, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
	}

	// 关联 provider 时拼 gateway URL 并签一个"进程内永久"的 token 内联进命令；
	// CLI 自身 login 模式则什么也不传，BuildLaunchCommand 不会写 BASE_URL/API_KEY。
	//
	// TTL=0 = 永久（gateway 进程生命周期内）。这意味着 gateway 重启所有这种 token
	// 都会失效——这是 token 仅存在内存里的天然安全护栏，前端 toast 已告知用户。
	gatewayURL, gatewayToken := "", ""
	if prov != nil && s.gateway != nil {
		gatewayURL = s.gateway.URL()
		if tok, terr := s.gateway.IssueToken(ctx, be, 0); terr == nil {
			gatewayToken = tok
		}
	}

	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return nil, err
	}
	cmd, err := agentruntime.BuildLaunchCommand(agentruntime.LaunchCommandSpec{
		Backend:           be,
		Provider:          prov,
		AgentID:           a.ID,
		SessionID:         sess.ID,
		Cwd:               cwd,
		ProviderSessionID: sess.ProviderSessionID,
		GatewayURL:        gatewayURL,
		Token:             gatewayToken,
	})
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatLaunchCommandNotAvailable)
	}
	return &LaunchCommandResponse{Command: cmd, BackendType: be.Type}, nil
}

// modelDeviationPayload 是偏离提示持久化时写进 blocks.NoticeBlock.Text 的小 JSON。
// NoticeBlock 是 cago 库的 UI-only 块,只有 Level/Text 两个字段(库类型,不能加字段),
// 所以把 selected/actual 两个模型 id 编码进 Text;前端投影(noticeBlockToChatBlock)
// 解回 ChatBlock.SelectedModel/ActualModel 再用 t() 渲染。该块从不发给 LLM。
// 旧数据 / 非结构化文本的 NoticeBlock 走 Text 原样渲染兜底。
type modelDeviationPayload struct {
	Selected string `json:"selected"`
	Actual   string `json:"actual"`
}

func encodeModelDeviation(override, actual string) string {
	b, _ := json.Marshal(modelDeviationPayload{Selected: override, Actual: actual})
	return string(b)
}

// decodeModelDeviation 把 NoticeBlock.Text 还原成 selected/actual。
// ok=false 表示文本不是本功能产出的结构化负载(旧数据/其它来源的 notice),调用方应
// 原样渲染 Text。
func decodeModelDeviation(text string) (selected, actual string, ok bool) {
	var p modelDeviationPayload
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		return "", "", false
	}
	if p.Selected == "" || p.Actual == "" {
		return "", "", false
	}
	return p.Selected, p.Actual, true
}

func modelDeviationNotice(override, actual string) *blocks.NoticeBlock {
	override = strings.TrimSpace(override)
	actual = strings.TrimSpace(actual)
	if override == "" || actual == "" || override == actual {
		return nil
	}
	return &blocks.NoticeBlock{
		Level: "info",
		Text:  encodeModelDeviation(override, actual),
	}
}

// noticeBlockToChatBlock 把持久化的 blocks.NoticeBlock 投影成前端 ChatBlock。
// 结构化偏离提示(本功能产出的 {"selected":..,"actual":..} 小 JSON)解回
// SelectedModel/ActualModel,Text 置空——前端走 t() 渲染;非结构化旧数据原样透传 Text。
func noticeBlockToChatBlock(tb blocks.NoticeBlock) ChatBlock {
	if selected, actual, ok := decodeModelDeviation(tb.Text); ok {
		return ChatBlock{
			Type:          "notice",
			Level:         tb.Level,
			SelectedModel: selected,
			ActualModel:   actual,
		}
	}
	return ChatBlock{Type: "notice", Level: tb.Level, Text: tb.Text}
}

func toChatMessage(m *chat_entity.Message) (ChatMessage, error) {
	bs, err := m.GetBlocks()
	if err != nil {
		return ChatMessage{}, err
	}
	out := ChatMessage{
		ID:                  m.ID,
		SessionID:           m.SessionID,
		Role:                m.Role,
		Model:               m.Model,
		PromptTokens:        m.PromptTokens,
		CompletionTokens:    m.CompletionTokens,
		CachedTokens:        m.CachedTokens,
		CacheCreationTokens: m.CacheCreationTokens,
		ReasoningTokens:     m.ReasoningTokens,
		TotalInputTokens:    m.TotalInputTokens,
		DurationMs:          m.DurationMs,
		ErrorText:           m.ErrorText,
		Seq:                 m.Seq,
		Createtime:          m.Createtime,
		Blocks:              make([]ChatBlock, 0, len(bs)),
	}
	// 预扫一遍,把 SubagentStateBlock 按 ParentToolCallID 索引起来,
	// 后续 tool_use 命中时把元数据合入 .Subagent,实现持久化/重载路径与
	// live 路径(dispatcher_emitter mergeSubagentMeta)形态一致。
	subByParent := make(map[string]*chatblocks.SubagentStateBlock)
	for _, b := range bs {
		switch sb := b.(type) {
		case chatblocks.SubagentStateBlock:
			cp := sb
			subByParent[sb.ParentToolCallID] = &cp
		case *chatblocks.SubagentStateBlock:
			if sb != nil {
				subByParent[sb.ParentToolCallID] = sb
			}
		}
	}

	for _, b := range bs {
		switch tb := b.(type) {
		case blocks.TextBlock:
			out.Blocks = append(out.Blocks, ChatBlock{Type: "text", Text: tb.Text})
		case *blocks.TextBlock:
			out.Blocks = append(out.Blocks, ChatBlock{Type: "text", Text: tb.Text})
		case blocks.ImageBlock:
			out.Blocks = append(out.Blocks, imageBlockToChatBlock(tb))
		case *blocks.ImageBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, imageBlockToChatBlock(*tb))
			}
		case blocks.ThinkingBlock:
			out.Blocks = append(out.Blocks, ChatBlock{Type: "thinking", Text: tb.Text})
		case *blocks.ThinkingBlock:
			out.Blocks = append(out.Blocks, ChatBlock{Type: "thinking", Text: tb.Text})
		case blocks.NoticeBlock:
			out.Blocks = append(out.Blocks, noticeBlockToChatBlock(tb))
		case *blocks.NoticeBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, noticeBlockToChatBlock(*tb))
			}
		case blocks.ToolUseBlock:
			cb := toolUseToChatBlock(tb.ID, tb.Name, tb.Input)
			if sb := subByParent[tb.ID]; sb != nil {
				attachSubagentStateToChatBlock(&cb, tb.Name, sb)
			}
			out.Blocks = append(out.Blocks, cb)
		case *blocks.ToolUseBlock:
			cb := toolUseToChatBlock(tb.ID, tb.Name, tb.Input)
			if sb := subByParent[tb.ID]; sb != nil {
				attachSubagentStateToChatBlock(&cb, tb.Name, sb)
			}
			out.Blocks = append(out.Blocks, cb)
		case blocks.ToolResultBlock:
			out.Blocks = append(out.Blocks, toolResultToChatBlock(tb.ToolUseID, tb.Content, tb.IsError))
		case *blocks.ToolResultBlock:
			out.Blocks = append(out.Blocks, toolResultToChatBlock(tb.ToolUseID, tb.Content, tb.IsError))
		case *chatblocks.NestedToolUseBlock:
			out.Blocks = append(out.Blocks, nestedToolUseToChatBlock(tb))
		case chatblocks.NestedToolUseBlock:
			out.Blocks = append(out.Blocks, nestedToolUseToChatBlock(&tb))
		case *chatblocks.NestedToolResultBlock:
			out.Blocks = append(out.Blocks, nestedToolResultToChatBlock(tb))
		case chatblocks.NestedToolResultBlock:
			out.Blocks = append(out.Blocks, nestedToolResultToChatBlock(&tb))
		case *chatblocks.SubagentStateBlock, chatblocks.SubagentStateBlock,
			*chatblocks.PermissionModeChangeBlock, chatblocks.PermissionModeChangeBlock:
			// SubagentStateBlock: 元数据已在预扫阶段合入对应 tool_use 块的 .Subagent 字段,
			// 不再作为独立 block 下行前端(否则会被打成 type=unknown 让用户看到 debug 卡)。
			// PermissionModeChangeBlock: 审计 block,无 UI 元素,一并 skip。
		case *chatblocks.CompactBoundaryBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, ChatBlock{
					Type: "compact_boundary",
					Compact: &ChatBlockCompactBoundary{
						PreTokens: tb.PreTokens, Trigger: tb.Trigger, At: tb.At,
					},
				})
			}
		case chatblocks.CompactBoundaryBlock:
			out.Blocks = append(out.Blocks, ChatBlock{
				Type: "compact_boundary",
				Compact: &ChatBlockCompactBoundary{
					PreTokens: tb.PreTokens, Trigger: tb.Trigger, At: tb.At,
				},
			})
		case chatblocks.UserAskBlock:
			out.Blocks = append(out.Blocks, askUserQuestionBlockToChatBlock(tb))
		case *chatblocks.UserAskBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, askUserQuestionBlockToChatBlock(*tb))
			}
		case chatblocks.ToolPermissionBlock:
			out.Blocks = append(out.Blocks, toolPermissionBlockToChatBlock(tb))
		case *chatblocks.ToolPermissionBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, toolPermissionBlockToChatBlock(*tb))
			}
		case chatblocks.ExecApprovalBlock:
			out.Blocks = append(out.Blocks, execApprovalBlockToChatBlock(tb))
		case *chatblocks.ExecApprovalBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, execApprovalBlockToChatBlock(*tb))
			}
		case chatblocks.ToolApprovalBlock:
			out.Blocks = append(out.Blocks, toolApprovalBlockToChatBlock(tb))
		case *chatblocks.ToolApprovalBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, toolApprovalBlockToChatBlock(*tb))
			}
		case PlanBlock:
			out.Blocks = append(out.Blocks, planBlockToChatBlock(tb))
		case *PlanBlock:
			if tb != nil {
				out.Blocks = append(out.Blocks, planBlockToChatBlock(*tb))
			}
		default:
			out.Blocks = append(out.Blocks, ChatBlock{Type: "unknown", Raw: map[string]any{"kind": b.Type()}})
		}
	}
	return out, nil
}

func toolUseToChatBlock(id, name string, input map[string]any) ChatBlock {
	cb := ChatBlock{Type: "tool_use", ToolUseID: id, ToolName: name}
	if len(input) > 0 {
		cb.ToolInput = input
	}
	if c, ok := canonical.FromToolUse(name, input); ok {
		cb.Canonical = view.FromCanonical(c)
	}
	return cb
}

func subagentStateToChatBlockSubagent(sb *chatblocks.SubagentStateBlock) *ChatBlockSubagent {
	if sb == nil {
		return nil
	}
	return &ChatBlockSubagent{
		TaskID:          sb.TaskID,
		Kind:            sb.Kind,
		TaskDescription: sb.Description,
		LastToolName:    sb.LastToolName,
		ToolUses:        sb.ToolUses,
		TotalTokens:     sb.TotalTokens,
		DurationMs:      sb.DurationMs,
		Status:          sb.Status,
		Summary:         sb.Summary,
		Mode:            sb.Mode,
		Runs:            cloneSubagentRunSnapshot(sb.Runs),
		Model:           sb.Model,
	}
}

func attachSubagentStateToChatBlock(cb *ChatBlock, toolName string, sb *chatblocks.SubagentStateBlock) {
	cb.Subagent = subagentStateToChatBlockSubagent(sb)
	if cb.Canonical != nil || !isNormalizedPiSubagentReplay(toolName, sb) {
		return
	}
	cb.Canonical = view.FromCanonical(canonical.AgentSpawn{
		TaskID:          sb.TaskID,
		TaskDescription: sb.Description,
		Mode:            sb.Mode,
		Runs:            agentSpawnRunsFromRuntime(sb.Runs),
		LastToolName:    sb.LastToolName,
		ToolUses:        sb.ToolUses,
		TotalTokens:     sb.TotalTokens,
		DurationMs:      sb.DurationMs,
		Status:          sb.Status,
	})
}

func isNormalizedPiSubagentReplay(toolName string, sb *chatblocks.SubagentStateBlock) bool {
	return sb != nil && sb.Mode != "" && len(sb.Runs) > 0 &&
		strings.Contains(strings.ToLower(toolName), "subagent")
}

func cloneSubagentRunSnapshot(runs []agentruntime.SubagentRun) []agentruntime.SubagentRun {
	if runs == nil {
		return nil
	}
	out := make([]agentruntime.SubagentRun, len(runs))
	copy(out, runs)
	return out
}

func imageBlockToChatBlock(img blocks.ImageBlock) ChatBlock {
	cb := ChatBlock{Type: "image", Image: &ChatBlockImage{MediaType: img.MediaType}}
	if len(img.Source.Inline) > 0 {
		cb.Image.DataURL = "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Source.Inline)
	} else if img.Source.URL != "" {
		cb.Image.DataURL = img.Source.URL
	}
	return cb
}

// nestedToolUseToChatBlock 把 subagent 内层 ToolUse 投影到 wire ChatBlock。
// 与外层 toolUseToChatBlock 的差别在于带 ParentToolCallID(json: parentToolUseId) +
// 可选 SubagentRunID；前端据此先挂到外层 AgentSpawnCard，再按 normalized run 分组。
// canonical 故意不算 —— 内层是被父 agent.spawn 包住的 step,不需要独立 canonical 路由。
func nestedToolUseToChatBlock(b *chatblocks.NestedToolUseBlock) ChatBlock {
	cb := ChatBlock{
		Type:             "tool_use",
		ToolUseID:        b.ID,
		ToolName:         b.Name,
		ParentToolCallID: b.ParentToolCallID,
		SubagentRunID:    b.SubagentRunID,
	}
	if len(b.Input) > 0 {
		cb.ToolInput = b.Input
	}
	return cb
}

// nestedToolResultToChatBlock 镜像 nestedToolUseToChatBlock —— 内层 tool_result
// 保留 ParentToolCallID/SubagentRunID，Content 已经是拍平字符串。
func nestedToolResultToChatBlock(b *chatblocks.NestedToolResultBlock) ChatBlock {
	return ChatBlock{
		Type:             "tool_result",
		ToolUseID:        b.ToolCallID,
		Text:             b.Content,
		IsError:          b.IsError,
		ParentToolCallID: b.ParentToolCallID,
		SubagentRunID:    b.SubagentRunID,
	}
}

// toolResultToChatBlock 把 ToolResultBlock 拍平：拼接所有 TextBlock 内容；
// 其它子块暂时丢弃（设计稿 Sec 02/04 的特殊卡片下个迭代再做）。
func toolResultToChatBlock(toolUseID string, content []blocks.ContentBlock, isError bool) ChatBlock {
	var sb strings.Builder
	for _, c := range content {
		switch t := c.(type) {
		case blocks.TextBlock:
			sb.WriteString(t.Text)
		case *blocks.TextBlock:
			sb.WriteString(t.Text)
		}
	}
	return ChatBlock{Type: "tool_result", ToolUseID: toolUseID, Text: sb.String(), IsError: isError}
}

type sendOptions struct {
	allowPlanWaiting bool
}

func (s *chatSvc) Send(ctx context.Context, req *SendRequest) (*SendResponse, error) {
	return s.send(ctx, req, sendOptions{})
}

func (s *chatSvc) Compact(ctx context.Context, req *CompactRequest) (*CompactResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}
	gate, err := s.acquireTurnGate(ctx, sess, be)
	if err != nil {
		return nil, err
	}
	gateOwned := true
	defer func() {
		if gateOwned {
			gate.lock.Unlock()
		}
	}()
	// Codex and Pi Agent compact an existing provider-native session; neither may
	// create an empty replacement session merely to satisfy an explicit compact.
	if compactRequiresProviderSession(be) && strings.TrimSpace(sess.ProviderSessionID) == "" {
		return nil, i18n.NewError(ctx, code.ChatCompactNoSession)
	}
	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.Compact: selectRunner failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatCompactUnsupported)
	}
	releasePreflight := func() {}
	if be.IsRemote() {
		if deviceID, ok := be.DeviceIDInt(); ok {
			releasePreflight = func() { s.releaseRemoteRuntime(deviceID, sess.ID) }
		}
	}
	if !runner.Capabilities().Has(capability.CapCompact) {
		releasePreflight()
		return nil, i18n.NewError(ctx, code.ChatCompactUnsupported)
	}
	gateOwned = false
	resp, err := s.startCompactTurn(ctx, sess, a, be, prov, gate.lock)
	if err != nil {
		releasePreflight()
		return nil, err
	}
	return resp, nil
}

func compactRequiresProviderSession(be *agent_backend_entity.AgentBackend) bool {
	return be != nil && (be.IsCodex() || be.IsPiAgent())
}

func (s *chatSvc) GetGoal(ctx context.Context, req *GoalRequest) (*GoalResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	controller, goalReq, release, err := s.goalController(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	goal, err := controller.GetGoal(ctx, goalReq)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.GetGoal: runner.GetGoal failed",
			zap.Int64("sessionId", req.SessionID),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatGoalInternal)
	}
	return &GoalResponse{Goal: chatGoalFromRuntime(goal)}, nil
}

func (s *chatSvc) SetGoal(ctx context.Context, req *SetGoalRequest) (*GoalResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if req.Objective == nil && req.Status == nil && req.TokenBudget == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, a, be, prov, err := s.goalSessionContext(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	resp, release, err := s.setGoalOnSession(ctx, sess, a, be, prov, req)
	defer release()
	return resp, err
}

func (s *chatSvc) StartGoal(ctx context.Context, req *StartGoalRequest) (*StartGoalResponse, error) {
	if req == nil || req.AgentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if req.Objective == nil || strings.TrimSpace(*req.Objective) == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	a, be, prov, err := s.resolveAgentBackend(ctx, req.AgentID)
	if err != nil {
		return nil, err
	}
	if !be.IsCodex() {
		return nil, i18n.NewError(ctx, code.ChatGoalUnsupported)
	}
	projectID, err := s.resolveProjectContext(ctx, req.ProjectID, req.AgentID)
	if err != nil {
		return nil, err
	}
	permissionMode, err := createPermissionMode(ctx, be, req.PermissionMode, true)
	if err != nil {
		return nil, err
	}
	objective := strings.TrimSpace(*req.Objective)
	sess := &chat_entity.Session{
		AgentID:                req.AgentID,
		ProjectID:              projectID,
		PermissionMode:         permissionMode,
		PermissionModeAtLaunch: permissionMode,
		Title:                  sessionTitleFromFirstMessage(objective),
		AgentStatus:            "idle",
		Status:                 consts.ACTIVE,
	}
	if err := chat_repo.Session().Create(ctx, sess); err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	setReq := &SetGoalRequest{
		SessionID:   sess.ID,
		Objective:   &objective,
		Status:      req.Status,
		TokenBudget: req.TokenBudget,
	}
	resp, release, err := s.setGoalOnSession(ctx, sess, a, be, prov, setReq)
	defer release()
	if err != nil {
		return nil, err
	}
	if resp != nil && resp.Goal != nil {
		providerSessionID := strings.TrimSpace(resp.Goal.ThreadID)
		if providerSessionID == "" {
			return nil, i18n.NewError(ctx, code.ChatGoalInternal)
		}
		sess.SetProviderSession(providerSessionID)
		if err := chat_repo.Session().Update(ctx, sess); err != nil {
			return nil, operationFailedWithCause(ctx, err,
				zap.Int64("sessionId", sess.ID),
				zap.String("providerSessionID", providerSessionID))
		}
	}
	return &StartGoalResponse{SessionID: sess.ID, Goal: resp.Goal}, nil
}

func (s *chatSvc) ClearGoal(ctx context.Context, req *ClearGoalRequest) (*ClearGoalResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	controller, goalReq, release, err := s.goalController(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	cleared, err := controller.ClearGoal(ctx, goalReq)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.ClearGoal: runner.ClearGoal failed",
			zap.Int64("sessionId", req.SessionID),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatGoalInternal)
	}
	return &ClearGoalResponse{Cleared: cleared}, nil
}

func (s *chatSvc) goalSessionContext(ctx context.Context, sessionID int64) (*chat_entity.Session, *agent_entity.Agent, *agent_backend_entity.AgentBackend, *llm_provider_entity.LLMProvider, error) {
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		return nil, nil, nil, nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, nil, nil, nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	if strings.TrimSpace(sess.ProviderSessionID) == "" {
		return nil, nil, nil, nil, i18n.NewError(ctx, code.ChatGoalNoSession)
	}
	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if !be.IsCodex() {
		return nil, nil, nil, nil, i18n.NewError(ctx, code.ChatGoalUnsupported)
	}
	return sess, a, be, prov, nil
}

func (s *chatSvc) goalController(ctx context.Context, sessionID int64) (agentruntime.GoalController, agentruntime.GoalRequest, func(), error) {
	sess, a, be, prov, err := s.goalSessionContext(ctx, sessionID)
	if err != nil {
		return nil, agentruntime.GoalRequest{}, func() {}, err
	}
	return s.goalControllerForSession(ctx, sess, a, be, prov)
}

func (s *chatSvc) goalControllerForSession(ctx context.Context, sess *chat_entity.Session, a *agent_entity.Agent, be *agent_backend_entity.AgentBackend, prov *llm_provider_entity.LLMProvider) (agentruntime.GoalController, agentruntime.GoalRequest, func(), error) {
	release := func() {}
	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.goalController: selectRunner failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, agentruntime.GoalRequest{}, release, i18n.NewError(ctx, code.ChatGoalUnsupported)
	}
	if be.IsRemote() {
		if deviceID, ok := be.DeviceIDInt(); ok {
			released := false
			release = func() {
				if released {
					return
				}
				released = true
				s.releaseRemoteRuntime(deviceID, sess.ID)
			}
		}
	}
	if !runner.Capabilities().Has(capability.CapGoal) {
		release()
		return nil, agentruntime.GoalRequest{}, func() {}, i18n.NewError(ctx, code.ChatGoalUnsupported)
	}
	controller, ok := runner.(agentruntime.GoalController)
	if !ok {
		release()
		return nil, agentruntime.GoalRequest{}, func() {}, i18n.NewError(ctx, code.ChatGoalUnsupported)
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return nil, agentruntime.GoalRequest{}, release, err
	}
	return controller, agentruntime.GoalRequest{
		SessionID:         sess.ID,
		ProviderSessionID: sess.ProviderSessionID,
		Backend:           be,
		Provider:          prov,
		Cwd:               cwd,
		AgentID:           a.ID,
	}, release, nil
}

func (s *chatSvc) setGoalOnSession(ctx context.Context, sess *chat_entity.Session, a *agent_entity.Agent, be *agent_backend_entity.AgentBackend, prov *llm_provider_entity.LLMProvider, req *SetGoalRequest) (*GoalResponse, func(), error) {
	controller, goalReq, release, err := s.goalControllerForSession(ctx, sess, a, be, prov)
	if err != nil {
		return nil, release, err
	}
	goalReq.Objective = req.Objective
	goalReq.Status = req.Status
	goalReq.TokenBudget = req.TokenBudget
	goal, err := controller.SetGoal(ctx, goalReq)
	if err != nil {
		release()
		logger.Ctx(ctx).Warn("chat_svc.SetGoal: runner.SetGoal failed",
			zap.Int64("sessionId", req.SessionID),
			zap.Error(err))
		return nil, func() {}, i18n.NewError(ctx, code.ChatGoalInternal)
	}
	return &GoalResponse{Goal: chatGoalFromRuntime(goal)}, release, nil
}

func chatGoalFromRuntime(goal *agentruntime.Goal) *ChatGoal {
	if goal == nil {
		return nil
	}
	return &ChatGoal{
		ThreadID:        goal.ThreadID,
		Objective:       goal.Objective,
		Status:          goal.Status,
		TokenBudget:     goal.TokenBudget,
		TokensUsed:      goal.TokensUsed,
		TimeUsedSeconds: goal.TimeUsedSeconds,
		CreatedAt:       goal.CreatedAt,
		UpdatedAt:       goal.UpdatedAt,
	}
}

func (s *chatSvc) send(ctx context.Context, req *SendRequest, opts sendOptions) (*SendResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	text := strings.TrimSpace(req.Text)
	imageBlocks, err := blocksFromSendImages(ctx, req.Images)
	if err != nil {
		return nil, err
	}
	if text == "" && len(imageBlocks) == 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if len(text) > chat_entity.MessageTextMaxBytes {
		return nil, i18n.NewError(ctx, code.ChatTextTooLong)
	}
	if req.SessionID < 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	var (
		sess          *chat_entity.Session
		targetAgentID = req.AgentID
	)
	if req.SessionID > 0 {
		var err error
		sess, err = chat_repo.Session().Find(ctx, req.SessionID)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		if sess == nil {
			return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
		}
		targetAgentID = sess.AgentID
	}
	if targetAgentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	a, be, prov, err := s.resolveAgentBackend(ctx, targetAgentID)
	if err != nil {
		return nil, err
	}
	var gate *sessionTurnGate
	gateOwned := false
	if req.SessionID > 0 {
		gate, err = s.acquireTurnGate(ctx, sess, be)
		if err != nil {
			return nil, err
		}
		gateOwned = true
		defer func() {
			if gateOwned {
				gate.lock.Unlock()
			}
		}()
	}
	if len(imageBlocks) > 0 && be.IsLocal() {
		runner, err := s.selectRunner(ctx, be, req.SessionID)
		if err != nil {
			return nil, err
		}
		if !runner.Capabilities().Has(capability.CapImageInput) {
			return nil, i18n.NewError(ctx, code.AgentBackendTypeUnsupported)
		}
	}

	if req.SessionID == 0 {
		modelOverride, merr := normalizeModelOverride(ctx, req.ModelOverride)
		if merr != nil {
			return nil, merr
		}
		// 项目上下文（可选）：仅在新建会话时生效；已存在的会话不再换项目。
		projectID, perr := s.resolveProjectContext(ctx, req.ProjectID, targetAgentID)
		if perr != nil {
			return nil, perr
		}
		permissionMode, perr := createPermissionMode(ctx, be, req.PermissionMode, true)
		if perr != nil {
			return nil, perr
		}
		sess = &chat_entity.Session{
			AgentID:        targetAgentID,
			ProjectID:      projectID,
			PermissionMode: permissionMode,
			// at_launch 同步落库,避免 runtime 在 spawn goroutine 里写入跟前端 LoadSession
			// 抢跑——前端拿到空串后 messages.length>0 时会把 bypass pill 错灰。
			// runtime 后续仍按 resolveLaunchMode 结果幂等覆盖,处理后端默认值回落。
			PermissionModeAtLaunch: permissionMode,
			ModelOverride:          modelOverride,
			Title:                  sessionTitleFromFirstMessage(text),
			// idle 落库;running 由 startTurn 事务内的 Update 原子翻转 —— 事务失败
			// 时不残留 running(否则空会话永久卡 running,还会 block 退出)。
			AgentStatus: "idle",
			Status:      consts.ACTIVE,
		}
		if err := chat_repo.Session().Create(ctx, sess); err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
	} else {
		planWaiting, err := s.canContinuePlanWaiting(ctx, sess, be, opts.allowPlanWaiting)
		if err != nil {
			return nil, err
		}
		if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, planWaiting); err != nil {
			return nil, err
		}
	}

	var prelocked *trylockMutex
	if gate != nil {
		prelocked = gate.lock
		gateOwned = false
	}
	return s.startTurn(ctx, sess, a, be, prov, userBlocksForSend(text, imageBlocks), nil /*preTxHook*/, nil /*replacement*/, "" /*forkAnchor*/, turnExtras{
		emitTurnStartedBypass: req.EmitTurnStartedBypass,
	}, prelocked)
}

func userBlocksForSend(text string, imageBlocks []blocks.ContentBlock) []blocks.ContentBlock {
	out := make([]blocks.ContentBlock, 0, 1+len(imageBlocks))
	if strings.TrimSpace(text) != "" {
		out = append(out, &blocks.TextBlock{Text: text})
	}
	out = append(out, imageBlocks...)
	return out
}

func blocksFromSendImages(ctx context.Context, images []SendImage) ([]blocks.ContentBlock, error) {
	if len(images) == 0 {
		return nil, nil
	}
	if len(images) > maxSendImages {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	out := make([]blocks.ContentBlock, 0, len(images))
	for _, img := range images {
		mediaType, payload, ok := strings.Cut(strings.TrimSpace(img.DataURL), dataURLBase64Token)
		if !ok || !strings.HasPrefix(mediaType, "data:") {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		mediaType = strings.TrimPrefix(mediaType, "data:")
		if _, ok := sendImageMediaTypes[mediaType]; !ok {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil || len(decoded) == 0 || len(decoded) > maxSendImageBytes {
			return nil, i18n.NewError(ctx, code.InvalidParameter)
		}
		out = append(out, blocks.ImageBlock{
			MediaType: mediaType,
			Source:    blocks.BlobSource{Inline: decoded},
		})
	}
	return out, nil
}

// resolveProjectContext 校验新建会话的项目参数。返回 (projectID, err)。
// projectID=0 表示自由会话（不属于任何项目）。
//
// 规则：
//   - projectID=0 → 直接返回（自由会话）。
//   - 项目必须存在且 active；agent 必须是项目直接成员或继承成员。
//
// cwd 解析交给 project_svc.ResolveSessionCwd，永远 = project.Path。
func (s *chatSvc) resolveProjectContext(ctx context.Context, projectID int64, agentID int64) (int64, error) {
	if projectID == 0 {
		return 0, nil
	}
	p, err := project_repo.Project().Find(ctx, projectID)
	if err != nil {
		return 0, operationFailedWithCause(ctx, err)
	}
	if p == nil || !p.IsActive() {
		return 0, i18n.NewError(ctx, code.ProjectNotFound)
	}
	if ok, mErr := s.isAgentInProjectChain(ctx, agentID, p); mErr != nil {
		return 0, mErr
	} else if !ok {
		return 0, i18n.NewError(ctx, code.ProjectAgentNotMember)
	}
	return p.ID, nil
}

// isAgentInProjectChain 自下而上扫一遍 parent 链，看 agent 是不是直接 / 继承成员。
// 走批量 ListByProjects 一次拉完，避免 N+1。
func (s *chatSvc) isAgentInProjectChain(ctx context.Context, agentID int64, p *project_entity.Project) (bool, error) {
	ids := []int64{p.ID}
	cur := p
	for cur.ParentID > 0 {
		parent, err := project_repo.Project().Find(ctx, cur.ParentID)
		if err != nil {
			return false, operationFailedWithCause(ctx, err)
		}
		if parent == nil {
			break
		}
		ids = append(ids, parent.ID)
		cur = parent
	}
	mapByProj, err := project_repo.ProjectAgent().ListByProjects(ctx, ids)
	if err != nil {
		return false, operationFailedWithCause(ctx, err)
	}
	for _, list := range mapByProj {
		for _, pa := range list {
			if pa.AgentID == agentID {
				return true, nil
			}
		}
	}
	return false, nil
}

// resolveAgentBackend 查 agent → backend → provider 并做完整的"可对话"校验。
// Send 和 Regenerate 都走这条；规则集中在一处避免两边漂移。
func (s *chatSvc) resolveAgentBackend(ctx context.Context, agentID int64) (
	*agent_entity.Agent,
	*agent_backend_entity.AgentBackend,
	*llm_provider_entity.LLMProvider,
	error,
) {
	a, err := agent_repo.Agent().Find(ctx, agentID)
	if err != nil {
		return nil, nil, nil, operationFailedWithCause(ctx, err)
	}
	if a == nil {
		return nil, nil, nil, i18n.NewError(ctx, code.NotFound)
	}
	if a.AgentBackendID <= 0 {
		return nil, nil, nil, i18n.NewError(ctx, code.ChatAgentNoBackend)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil, nil, nil, operationFailedWithCause(ctx, err)
	}
	if be == nil {
		return nil, nil, nil, i18n.NewError(ctx, code.ChatAgentNotChattable)
	}
	kind := be.Kind()
	if kind == nil {
		return nil, nil, nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}

	var prov *llm_provider_entity.LLMProvider
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeBuiltin:
		prov, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
		if err != nil {
			return nil, nil, nil, operationFailedWithCause(ctx, err)
		}
		if prov == nil || !prov.IsActive() {
			return nil, nil, nil, i18n.NewError(ctx, code.ChatAgentNotChattable)
		}
	case agent_backend_entity.TypeClaudeCode, agent_backend_entity.TypeCodex, agent_backend_entity.TypePiAgent:
		if be.LLMProviderKey != "" {
			prov, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
			if err != nil {
				return nil, nil, nil, operationFailedWithCause(ctx, err)
			}
			if prov == nil || !prov.IsActive() ||
				!kind.ProviderTypeMatch(llm_provider_entity.ProviderType(prov.Type)) {
				return nil, nil, nil, i18n.NewError(ctx, code.ChatAgentNotChattable)
			}
			if remoteProviderKnownMissing(be) {
				return nil, nil, nil, remoteProviderNotConfiguredError(ctx, be.LLMProviderKey)
			}
			if be.IsRemote() {
				break
			}
			if s.gateway == nil || s.gateway.Status().State != "running" {
				return nil, nil, nil, i18n.NewError(ctx, code.ChatBackendGatewayUnavailable)
			}
		}
		// LLMProviderKey == "" → CLI 自身 login 状态生效，不强制 gateway。
	case agent_backend_entity.TypeOpenClaw:
		if be.IsRemote() {
			return nil, nil, nil, fmt.Errorf("openclaw remote secret enrollment is unavailable")
		}
	default:
		return nil, nil, nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}

	return a, be, prov, nil
}

// AgentBackendHasCapability 报告某 agent 的后端 runtime 是否声明指定能力(领域无关探针)。
//
// 复用 resolveAgentBackend 的 agent → backend 解析链;对本地后端经全局 runtime 注册表
// (RuntimeFor)取能力矩阵。后端缺失/类型无法解析 → (false, nil)。
// MVP: 远程后端的能力探测需借 session(borrowRemoteRuntime),这里没有 session,
// 暂统一返回 (false, nil)。
func (s *chatSvc) AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error) {
	_, be, _, err := s.resolveAgentBackend(ctx, agentID)
	if err != nil {
		return false, err
	}
	if be == nil {
		return false, nil
	}
	if !be.IsLocal() {
		return false, nil
	}
	r := agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type))
	if r == nil {
		return false, nil
	}
	return r.Capabilities().Has(wantCap), nil
}

// Enqueue 在 AI 还在回答时把一条新的用户消息插入当前 turn。
//
//   - claudecode 走 PreToolUse hook + SteerInbox：runner.Steer 把 (queuedID, text)
//     Push 进 in-process 队列，hook 进程 GET 走附在 additionalContext 上。
//   - codex 走 turn/steer JSON-RPC，直接打到 Stream 上（queuedID 被 runner 忽略）。
//   - builtin 走 cago Runner.Steer，queuedID 透传 WithSteerID 用于后续 cancel。
//
// 不会创建 chat_messages 行 —— Enqueue 的语义是「下一条 AI 应该看到的指令」，
// 不是历史消息。如果模型最终响应并写回文本，会落到已存在的 assistant 流里。
//
// 返回 Cancellable=true 当且仅当后端 runner 实现 SteerCanceler。前端按此
// 决定 chip 上的 X 按钮是真撤回还是替换为锁图标。
//
// 返回错误码：
//   - ChatSteerNoActive: 没有正在进行的 turn
//   - ChatSteerUnsupported: 后端不实现 Steerer
//   - ChatSteerInternal: 实现层 I/O 失败
func (s *chatSvc) Enqueue(ctx context.Context, req *EnqueueRequest) (*EnqueueResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	_, be, _, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}

	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.Enqueue: selectRunner failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatSteerUnsupported)
	}
	steerer, ok := runner.(agentruntime.Steerer)
	if !ok {
		return nil, i18n.NewError(ctx, code.ChatSteerUnsupported)
	}
	queuedID := newQueuedID()
	if err := steerer.Steer(ctx, sess.ID, queuedID, text); err != nil {
		if errors.Is(err, agentruntime.ErrNoActiveTurn) {
			return nil, i18n.NewError(ctx, code.ChatSteerNoActive)
		}
		logger.Ctx(ctx).Warn("chat_svc.Enqueue: steerer.Steer failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("queuedId", queuedID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatSteerInternal)
	}
	_, cancellable := runner.(agentruntime.SteerCanceler)
	return &EnqueueResponse{
		SessionID:   sess.ID,
		Queued:      true,
		QueuedID:    queuedID,
		Cancellable: cancellable,
	}, nil
}

// CancelQueued 撤回 Enqueue 投递但尚未被 AI 消费的排队消息。QueuedID 为空
// 表示清空整条队列。codex 后端 runner 不实现 SteerCanceler，会返
// ChatCancelUnsupported 让前端把 chip 的 X 渲染为锁图标。
//
// 返回错误码：
//   - ChatSessionNotFound / InvalidParameter
//   - ChatCancelUnsupported: 后端不实现 SteerCanceler
//   - ChatSteerNoActive:   turn 已结束或 runner 不再持有该 session
//   - ChatCancelNotFound:  非空 queuedID 但已被 AI 消费 / 不存在
//
// Stop 中断当前 turn。三件事按顺序做：
//
//  1. LoadAndDelete activeCancels —— 原子拿到 turnCtx 的 cancel；拿不到说明 turn
//     已自然完成 / 还没起 / 已被另一个 Stop 拉走，返 ChatStopNoActive。
//  2. Store aborted flag —— runTurn 收尾 LoadAndDelete 看到就走 StreamAborted 路径
//     并跳过 DrainPending 自动接续。
//  3. 先 cancel turnCtx，让已接受的本地 Pi 流立即进入自己的 bounded settlement
//     window；再以同一 generation 的内存 aborter 尝试写 abort。写端最多等待同一
//     500ms 边界，不能把 Stop 卡在满管道前。其它后端仍先 cancel，再尽力通过仓储
//     解析 runner.Abort。启动期还没绑定 aborter 时也保持 cancel-first，不给未确认
//     prompt settlement grace，也不让 Stop 的仓储查询延迟同步 preflight / SQL 取消。
func (s *chatSvc) Stop(ctx context.Context, req *StopRequest) (*StopResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	raw, ok := s.activeCancels.LoadAndDelete(req.SessionID)
	if !ok {
		// 内存里没有活跃 turn,两种情况:
		//   (a) turn 自然刚跑完、Stop 与收尾 race —— DB 已是 idle/error,无害。
		//   (b)「重启遗孤」:app crash / wails dev 热重载 / 第二实例 让 turn goroutine
		//      死了,但 DB agent_status 还停在 running/waiting(ResetActiveSessions
		//      只在主实例 Startup 后才扫,这些路径漏网),而 activeCancels(内存)已空。
		//      此时前端按 DB 状态把「停止」按钮亮着可点,旧逻辑直接返 ChatStopNoActive
		//      被前端静默吞掉 → 会话永远停不掉。这里 reconcile 回 idle 让停止生效。
		return s.reconcileOrphanStop(ctx, req.SessionID)
	}
	control, _ := raw.(*activeTurnControl)
	s.aborted.Store(req.SessionID, struct{}{})
	logger.Ctx(ctx).Info("chat_svc.Stop: aborting turn",
		zap.Int64("sessionId", req.SessionID))

	gracefulAborter, gracefulAbort := control.gracefulAborter()
	// Activation/staging SQL, prepared Start, and accepted stream drain all carry
	// this generation-specific context. Cancel first so a blocked abort write can
	// never delay SQL/pre-prompt cancellation or the accepted stream's settlement timer.
	if control != nil && control.cancel != nil {
		control.cancel()
	}
	if gracefulAbort {
		abortCtx, cancelAbort := context.WithTimeout(ctx, piStopAbortWriteBound)
		_, abortErr := gracefulAborter.Abort(abortCtx, req.SessionID, 0)
		cancelAbort()
		if abortErr != nil && !errors.Is(abortErr, agentruntime.ErrNoActiveTurn) {
			logger.Ctx(ctx).Warn("chat_svc.Stop: local Pi abort failed",
				zap.Int64("sessionId", req.SessionID),
				zap.String("backendType", string(agent_backend_entity.TypePiAgent)),
				zap.Error(abortErr))
		}
	}

	// Other backends keep the prior best-effort runner.Abort lookup after cancel.
	// A bound local Pi aborter is generation-specific and must not be redispatched
	// through the global runtime after its active control has been removed.
	if !gracefulAbort {
		if sess, err := chat_repo.Session().Find(ctx, req.SessionID); err == nil && sess != nil {
			if _, be, _, berr := s.resolveAgentBackend(ctx, sess.AgentID); berr == nil && be != nil {
				// 中断没下发下去不致命(前面已 cancel turnCtx 兜底),布尔判据这里
				// 无人可报;失败的留底由 requestRuntimeAbort 自己记。
				_, _ = s.requestRuntimeAbort(ctx, be, req.SessionID, 0)
			}
		}
	}
	return &StopResponse{Stopped: true}, nil
}

// reconcileOrphanStop 处理「Stop 时内存里没有活跃**用户**轮」的情况:
//   - 会话查不到 / 已是终态(idle/error)→ turn 早就收尾,返 ChatStopNoActive(无害的
//     「太晚了」,前端静默)。
//   - 会话还停在 running/waiting,先问 runtime 有没有带外轮在飞(自主续轮 / 后台
//     subagent 活动轮不进 activeCancels,却是此刻真正活跃的那一轮)→ 有就中断它,再
//     按被中断轮的类型决定谁 reconcile 会话状态(决策 3):
//   - 中断的是自主轮 → 状态留给 driveAutonomousTurn 收尾(idle/error),Stop 不落库;
//   - 中断的是 subagent 活动轮 → driveSubagentActivity 不写会话状态,由这里自己把
//     running/waiting 翻回 idle 并持久化(复用遗孤路径的翻写逻辑),一次点停止即收干净;
//   - runtime 报 ErrNoActiveTurn / 解析不出 runner → 才是真「重启遗孤」→ 翻回 idle 并
//     落库(等同 abort 收尾),让前端那颗按 DB 状态一直亮着的「停止」按钮真能把会话停下来。
//
// 不去 emit StreamSessionStatus:遗孤会话没有活跃 stream 订阅(stream 名按
// sessionID+assistantMsgID 双键),推了也送不到;前端 doStop 成功后会主动 reload
// 把按钮收回去。
func (s *chatSvc) reconcileOrphanStop(ctx context.Context, sessionID int64) (*StopResponse, error) {
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil || sess == nil {
		return nil, i18n.NewError(ctx, code.ChatStopNoActive)
	}
	if sess.AgentStatus != "running" && sess.AgentStatus != "waiting" {
		return nil, i18n.NewError(ctx, code.ChatStopNoActive)
	}
	outcome, interrupted := s.abortOutOfBandTurn(ctx, sess)
	if interrupted {
		// 确有一轮被中断,会话仍在跑。中断的是 subagent 活动轮时,会话的
		// running/waiting 已无合法依据且那一轮不写状态 —— 由这里接管翻 idle 落库;
		// 中断的是自主轮时状态留给它自己收尾,不落库。
		if outcome.TurnKind == agentruntime.TurnKindSubagentActivity {
			if perr := s.reconcileSessionToIdle(ctx, sess); perr != nil {
				return nil, perr
			}
		}
		return &StopResponse{Stopped: true}, nil
	}
	if perr := s.reconcileSessionToIdle(ctx, sess); perr != nil {
		return nil, perr
	}
	return &StopResponse{Stopped: true}, nil
}

// reconcileSessionToIdle 把会话 running/waiting 翻回 idle 并清 attention,复用
// persistSessionStatus(重试一次 + 失败上抛不静默)。遗孤路径与「被中断的是 subagent
// 活动轮」的接管路径共用这一份翻写逻辑。
func (s *chatSvc) reconcileSessionToIdle(ctx context.Context, sess *chat_entity.Session) error {
	logger.Ctx(ctx).Info("chat_svc.Stop: reconciling session to idle",
		zap.Int64("sessionId", sess.ID),
		zap.String("prevStatus", sess.AgentStatus))
	sess.AgentStatus = "idle"
	sess.NeedsAttention = false
	return s.persistSessionStatus(ctx, sess)
}

// abortOutOfBandTurn 在「没有活跃用户轮」的前提下,把中断请求交给 runtime 试一次,
// 报告是否真有一轮被中断,以及被中断的那一轮的类型(AbortOutcome.TurnKind)。
//
// 带外轮(自主续轮 / 后台 subagent 活动轮)独占帧流期间不进 activeCancels —— 它就是
// 该会话此刻活跃的那一轮,而 Abort 的契约正是「中断该会话当前活跃的那一轮」,两者
// 都不活跃时才返回 ErrNoActiveTurn。因此这里可以直接拿 Abort 的返回值当判据:
// 中断成功 = 确有一轮被中断,会话仍在跑,调用方不能再把它当遗孤 reconcile 成 idle(那会
// 在 CLI 还在产帧时谎报 idle);ErrNoActiveTurn / 解析不出 runner = 内存里真的什么
// 都没有,交回遗孤路径。被中断轮的类型由 reconcileOrphanStop 拿来分流:自主轮留给它
// 自己收尾,subagent 活动轮则由 Stop 接管翻 idle(决策 3)。turnToken 固定传 0(中断
// 当前活跃的带外轮,等价旧行为)。
func (s *chatSvc) abortOutOfBandTurn(ctx context.Context, sess *chat_entity.Session) (agentruntime.AbortOutcome, bool) {
	_, be, _, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil || be == nil {
		logger.Ctx(ctx).Warn("chat_svc.Stop: cannot resolve backend to interrupt out-of-band turn",
			zap.Int64("sessionId", sess.ID), zap.Error(err))
		return agentruntime.AbortOutcome{}, false
	}
	outcome, ok := s.requestRuntimeAbort(ctx, be, sess.ID, 0)
	if !ok {
		return agentruntime.AbortOutcome{}, false
	}
	logger.Ctx(ctx).Info("chat_svc.Stop: interrupted out-of-band turn",
		zap.Int64("sessionId", sess.ID),
		zap.String("backendType", be.Type),
		zap.String("turnKind", string(outcome.TurnKind)),
		zap.String("prevStatus", sess.AgentStatus))
	return outcome, true
}

// requestRuntimeAbort 把「中断该会话当前活跃的那一轮」尽力下发给 runtime,报告是否
// 真有一轮被中断以及被中断轮的类型:Abort 返 nil = 确有一轮被中断(AbortOutcome 携带
// 轮类型);ErrNoActiveTurn / 解析不出 runner / runner 不支持中断 = 内存里没有可中断
// 的轮(返回零值 outcome + false)。任何一步失败都只记日志、不返回错误 —— 三个调用方
// (Stop 里活跃用户轮的 best-effort 中断、Stop 的遗孤路径 abortOutOfBandTurn、自主续轮
// 落库失败处置 failAutonomousTurnPersist)要么只要这个布尔判据 + 轮类型、要么连它都
// 不要,失败各自另有兜底(已 cancel turnCtx / 交回遗孤 reconcile / 前两步的可观察结果
// 已经产生)。
//
// **会阻塞**:claudecode 的 Abort 写完 control_request 后要等 CLI 的 control_response,
// 而那条回执要常驻 readLoop 前进才派发得了。调用方若同时还担着「让帧流继续被消费」的
// 责任(failAutonomousTurnPersist 的抽干),必须以非阻塞方式调用,否则两者互相等着。
func (s *chatSvc) requestRuntimeAbort(ctx context.Context, be *agent_backend_entity.AgentBackend, sessionID int64, turnToken uint64) (agentruntime.AbortOutcome, bool) {
	backendType := ""
	if be != nil {
		backendType = be.Type
	}
	runner, err := s.selectRunner(ctx, be, sessionID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.requestRuntimeAbort: selectRunner failed, cannot interrupt turn",
			zap.Int64("sessionId", sessionID), zap.String("backendType", backendType), zap.Error(err))
		return agentruntime.AbortOutcome{}, false
	}
	aborter, ok := runner.(agentruntime.Aborter)
	if !ok {
		return agentruntime.AbortOutcome{}, false
	}
	outcome, aerr := aborter.Abort(ctx, sessionID, turnToken)
	if aerr != nil {
		if !errors.Is(aerr, agentruntime.ErrNoActiveTurn) {
			logger.Ctx(ctx).Warn("chat_svc.requestRuntimeAbort: runner.Abort failed",
				zap.Int64("sessionId", sessionID), zap.String("backendType", backendType), zap.Error(aerr))
		}
		return agentruntime.AbortOutcome{}, false
	}
	return outcome, true
}

// StopBackgroundTask 停掉某个后台任务 / 子 agent(run_in_background),而不是中断整个 turn。
// 流程:
//  1. 按 toolUseID 从持久化 subagent_state 读出 CLI task_id + 当前状态;
//  2. 已终态 / 找不到 overlay → 幂等成功(任务已不在跑,前端 reload 自然对齐);
//  3. 缺 task_id(老会话的块没记)→ ChatStopBgTaskUnknown,让前端提示;
//  4. resolve runner,断言 BackgroundTaskStopper(否则 ChatStopBgUnsupported,正常已被
//     capability 位挡在前端),下发 stop_task;
//  5. 成功后主动把块 flip 成 "canceled" —— 前端 reload 立即显示「已停止」;CLI 停任务后
//     另发的 task_notification(canceled/failed)经既有自主轮再幂等收敛一次。
func (s *chatSvc) StopBackgroundTask(ctx context.Context, req *StopBackgroundTaskRequest) (*StopBackgroundTaskResponse, error) {
	if req == nil || req.SessionID <= 0 || req.ToolUseID == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	taskID, status, found, err := chat_repo.Message().FindSubagentState(ctx, req.SessionID, req.ToolUseID)
	if err != nil {
		return nil, err
	}
	if !found || (status != "" && status != "running") {
		// 任务已终态 / 无 overlay:当已停处理,幂等。
		return &StopBackgroundTaskResponse{Stopped: true}, nil
	}
	if taskID == "" {
		return nil, i18n.NewError(ctx, code.ChatStopBgTaskUnknown)
	}

	_, be, _, berr := s.resolveAgentBackend(ctx, sess.AgentID)
	if berr != nil {
		return nil, berr
	}
	runner, rerr := s.selectRunner(ctx, be, sess.ID)
	if rerr != nil {
		return nil, rerr
	}
	stopper, ok := runner.(agentruntime.BackgroundTaskStopper)
	if !ok {
		return nil, i18n.NewError(ctx, code.ChatStopBgUnsupported)
	}

	logger.Ctx(ctx).Info("chat_svc.StopBackgroundTask: stopping background task",
		zap.Int64("sessionId", req.SessionID),
		zap.String("toolUseId", req.ToolUseID),
		zap.String("taskId", taskID))

	if serr := stopper.StopBackgroundTask(ctx, req.SessionID, taskID); serr != nil {
		if errors.Is(serr, agentruntime.ErrNoActiveTurn) {
			// 子进程已 evict → 任务随之消失,当已停处理(幂等)。
			return &StopBackgroundTaskResponse{Stopped: true}, nil
		}
		logger.Ctx(ctx).Warn("chat_svc.StopBackgroundTask: runner stop failed",
			zap.Int64("sessionId", req.SessionID),
			zap.String("taskId", taskID),
			zap.Error(serr))
		return nil, i18n.NewError(ctx, code.ChatStopInternal)
	}

	// 主动翻 canceled(summary 留空:不写后端硬编码文案,「已停止」由前端 StatusPill 出 i18n)。
	if ferr := chat_repo.Message().FlipSubagentStatus(ctx, req.SessionID, req.ToolUseID, "canceled", ""); ferr != nil {
		logger.Ctx(ctx).Warn("chat_svc.StopBackgroundTask: flip subagent_state failed",
			zap.Int64("sessionId", req.SessionID),
			zap.String("toolUseId", req.ToolUseID),
			zap.Error(ferr))
	}
	return &StopBackgroundTaskResponse{Stopped: true}, nil
}

const (
	permissionModeDefault           = "default"
	permissionModeAcceptEdits       = "acceptEdits"
	permissionModePlan              = "plan"
	permissionModeBypassPermissions = "bypassPermissions"
)

// permissionModeMetaFor 反查 agentruntime 注册表里 runtime 的 PermissionModeMeta;
// 未注册 / 不支持 permission mode(AllowedModes 空)的 backend 返 (零值, false)。
// 替代 chat_svc 原来按 backendType 字面量分支的 4 处 switch。
func permissionModeMetaFor(bt agent_backend_entity.BackendType) (capability.PermissionModeMeta, bool) {
	r := agentruntime.RuntimeFor(bt)
	if r == nil {
		return capability.PermissionModeMeta{}, false
	}
	meta := r.Capabilities().PermissionModeMeta
	if len(meta.AllowedModes) == 0 {
		return capability.PermissionModeMeta{}, false
	}
	return meta, true
}

// isKnownPermissionMode 判定 mode 是否被某个已注册 runtime 接受。仅用于
// SetPermissionMode 入口的 fail-fast 预校验(避开一次 DB 查询),后续的真实
// 校验由 validateRequestedPermissionMode 按 backendType 精确做。
func isKnownPermissionMode(mode string) bool {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return false
	}
	for _, r := range agentruntime.RegisteredRuntimes() {
		if slices.Contains(r.Capabilities().PermissionModeMeta.AllowedModes, mode) {
			return true
		}
	}
	return false
}

func normalizeStoredPermissionMode(backendType agent_backend_entity.BackendType, raw string) string {
	mode := strings.TrimSpace(raw)
	meta, ok := permissionModeMetaFor(backendType)
	if !ok {
		return ""
	}
	if slices.Contains(meta.AllowedModes, mode) {
		return mode
	}
	return meta.DefaultMode
}

func validateRequestedPermissionMode(ctx context.Context, backendType agent_backend_entity.BackendType, raw string) (string, error) {
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return "", i18n.NewError(ctx, code.ChatPermissionModeInvalid)
	}
	meta, ok := permissionModeMetaFor(backendType)
	if !ok || !slices.Contains(meta.AllowedModes, mode) {
		return "", i18n.NewError(ctx, code.ChatPermissionModeInvalid)
	}
	return mode, nil
}

// createPermissionMode 解析新建会话的初始权限模式。planFirst 决定是否套用
// 「先 plan 后 bypass」派生: 交互式会话(有人审阅计划再批准)传 true, 自律会话
// (subagent 调用, 没人审批)传 false —— 后者必须尊重配置的 bypass
// 直接起手, 否则会卡在 plan mode 出计划等审批, 配的 bypass 从未生效。
func createPermissionMode(ctx context.Context, be *agent_backend_entity.AgentBackend, raw string, planFirst bool) (string, error) {
	if be == nil {
		return "", nil
	}
	backendType := agent_backend_entity.BackendType(be.Type)
	if strings.TrimSpace(raw) != "" {
		return validateRequestedPermissionMode(ctx, backendType, raw)
	}
	// claudecode + admin 配 bypass 时, 交互式新会话以 plan 起手: CLI 仍按 bypass 启动(由
	// runtime resolveLaunchMode 保证), session.PermissionMode=plan 让前端 pill 显
	// 示 Plan, spawn 后由 runtime SetPermissionMode 把 CLI 切到 plan。"先 plan 后
	// bypass"工作流靠这条派生 + 现有 PlanApproveCard 主按钮(launch==bypass → Bypass)
	// 完成闭环。自律会话(planFirst=false)跳过这条, 直接落 bypass。
	if planFirst && be.IsClaudeCode() && strings.TrimSpace(be.DefaultPermissionMode) == "bypassPermissions" {
		return "plan", nil
	}
	// backend.DefaultPermissionMode 管理员预设兜底(目前 entity.Check 仅放行
	// claudecode 写入);白名单门禁在 entity 层,chat_svc 不按 type 分支。
	if def := strings.TrimSpace(be.DefaultPermissionMode); def != "" {
		return validateRequestedPermissionMode(ctx, backendType, def)
	}
	meta, ok := permissionModeMetaFor(backendType)
	if !ok {
		return "", nil
	}
	return meta.LaunchDefaultMode, nil
}

func (s *chatSvc) applyRequestedPermissionMode(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	raw string,
	allowWaiting bool,
) error {
	if sess == nil || be == nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	backendType := agent_backend_entity.BackendType(be.Type)
	// 不支持运行时切 mode 的 runtime(meta.SwitchableDuringTurn=false,目前 codex)
	// 在 turn 飞行中拒收 —— 切到 plan 会让 codex CLI 重起 turn,而我们已有
	// pending steer/answer 等状态不能丢。
	if meta, ok := permissionModeMetaFor(backendType); ok && !meta.SwitchableDuringTurn &&
		(sess.AgentStatus == "running" || (sess.AgentStatus == "waiting" && !allowWaiting)) {
		return i18n.NewError(ctx, code.ChatSendInFlight)
	}
	mode, err := validateRequestedPermissionMode(ctx, backendType, raw)
	if err != nil {
		return err
	}
	return s.persistPermissionMode(ctx, sess, be, mode)
}

func (s *chatSvc) canContinuePlanWaiting(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	allow bool,
) (bool, error) {
	if !allow || sess == nil || be == nil || sess.AgentStatus != "waiting" || !be.IsCodex() {
		return false, nil
	}
	msgs, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return false, operationFailedWithCause(ctx, err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || msgs[i].Role != "assistant" {
			continue
		}
		bs, err := msgs[i].GetBlocks()
		if err != nil {
			return false, i18n.NewError(ctx, code.ChatBlocksMalformed)
		}
		return hasActionablePlanBlock(bs), nil
	}
	return false, nil
}

func (s *chatSvc) persistPermissionMode(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	mode string,
) error {
	backendType := agent_backend_entity.BackendType(be.Type)
	// 支持判定走 meta:AllowedModes 非空即支持。等价于原 switch
	// {ClaudeCode,Codex}/default → Unsupported,但不再有字面量耦合。
	if _, ok := permissionModeMetaFor(backendType); !ok {
		return i18n.NewError(ctx, code.ChatPermissionModeUnsupported)
	}
	mode, err := validateRequestedPermissionMode(ctx, backendType, mode)
	if err != nil {
		return err
	}
	// runtime 是否能在运行时下发(setter)由"是否实现 PermissionModeSetter 接口"决定 —
	// 现状:claudecode 实现,codex 未实现(也不需要,collaborationMode 是 per-turn)。
	// 历史是按 backendType==ClaudeCode 显式 if,改成 runner type-assert 后行为不变。
	runner, rerr := s.selectRunner(ctx, be, sess.ID)
	if rerr != nil {
		return i18n.NewError(ctx, code.ChatPermissionModeUnsupported)
	}
	setter, _ := runner.(agentruntime.PermissionModeSetter)

	sess.PermissionMode = mode
	if err := chat_repo.Session().UpdatePermissionMode(ctx, sess.ID, mode); err != nil {
		logger.Ctx(ctx).Error("permission mode persist failed",
			zap.Int64("sessionID", sess.ID),
			zap.String("backendType", be.Type),
			zap.String("mode", mode),
			zap.Error(err))
		return i18n.NewError(ctx, code.ChatPermissionModeInternal)
	}

	if setter == nil {
		return nil
	}
	if err := setter.SetPermissionMode(ctx, sess.ID, mode); err != nil {
		if errors.Is(err, agentruntime.ErrNoActiveTurn) {
			logger.Ctx(ctx).Debug("permission mode persisted but no active CLI; will apply on next spawn",
				zap.Int64("sessionID", sess.ID),
				zap.String("mode", mode))
			return nil
		}
		logger.Ctx(ctx).Error("permission mode runtime dispatch failed",
			zap.Int64("sessionID", sess.ID),
			zap.String("mode", mode),
			zap.Error(err))
		return i18n.NewError(ctx, code.ChatPermissionModeInternal)
	}
	return nil
}

func (s *chatSvc) refreshPermissionModeForAutoContinue(ctx context.Context, sess *chat_entity.Session) {
	if sess == nil || sess.ID <= 0 {
		return
	}
	fresh, err := chat_repo.Session().Find(ctx, sess.ID)
	if err != nil || fresh == nil {
		if err != nil {
			logger.Ctx(ctx).Warn("refresh permission mode for auto-continue failed",
				zap.Int64("sessionID", sess.ID),
				zap.Error(err))
		}
		return
	}
	sess.PermissionMode = fresh.PermissionMode
}

func normalizeModelOverride(ctx context.Context, model string) (string, error) {
	if model == "" {
		return "", nil
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", i18n.NewError(ctx, code.InvalidParameter)
	}
	return model, nil
}

// modelOverrideSwitchable 报告该后端是否消费会话级模型覆盖(v1 有实测依据的四类:
// builtin / claudecode / codex / piagent,与前端 ModelPill 的 modelSwitchable 同一集合)。
// openclaw 切换是 follow-up,不在 v1 范围 —— 若给这类会话下发 override,openclaw runtime
// 会忽略它,而 runTurn 又拿 req.ModelOverride 跟 result.Model 比对产出偏离提示,每轮都会
// 误报「所选 X 未生效,实际 Y」。门禁放在 runTurn(而非 SetSessionModel):会话的 agent 可能
// 在设了 override 之后改绑到非 v1 后端,落库的 override 不该在新后端上继续生效。
func modelOverrideSwitchable(be *agent_backend_entity.AgentBackend) bool {
	if be == nil {
		return false
	}
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeBuiltin,
		agent_backend_entity.TypeClaudeCode,
		agent_backend_entity.TypeCodex,
		agent_backend_entity.TypePiAgent:
		return true
	default:
		return false
	}
}

// modelOverrideForBackend 返回本轮到 runtime 下发的会话级模型覆盖。仅 v1 可切换后端
// 透传;其它后端一律空串,防止 override 被忽略后逐轮误报偏离提示。
func modelOverrideForBackend(sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) string {
	if !modelOverrideSwitchable(be) {
		return ""
	}
	return strings.TrimSpace(sess.ModelOverride)
}

// SetSessionModel sets or clears the persisted model override for an existing session.
// Model ids are intentionally not checked against the provider catalog: CLI-login
// sessions and provider-compatible custom ids are valid inputs at this service boundary.
func (s *chatSvc) SetSessionModel(ctx context.Context, sessionID int64, model string) error {
	if sessionID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	model, err := normalizeModelOverride(ctx, model)
	if err != nil {
		return err
	}
	sess, err := chat_repo.Session().Find(ctx, sessionID)
	if err != nil {
		return operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	// 单列写入:model_override 在整行 Save 里被 Omit(见 sessionRepo.Update),只能通过
	// 专用方法写 —— 否则用户在轮次中途换的模型会被该轮收尾的整行 Save 盖回旧值。
	if err := chat_repo.Session().UpdateModelOverride(ctx, sessionID, model); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.SetSessionModel: persist model override failed",
			zap.Int64("sessionId", sessionID),
			zap.Error(err))
		return operationFailedWithCause(ctx, err)
	}
	return nil
}

// SetPermissionMode 让前端把 CLI 会话切到指定 mode。
//
// claudecode 使用 Claude permission mode；codex 使用 Codex collaboration mode
// 的 default / plan 子集。写 DB 在 runtime 之前，进程未启动时也会在下次启动生效。
//
// 错误码：
//   - mode 不在白名单 → ChatPermissionModeInvalid
//   - builtin / 不支持的后端 → ChatPermissionModeUnsupported
//   - DB 写失败 → ChatPermissionModeInternal
//   - Claude runtime 返 ErrNoActiveTurn → 成功（下次 spawn 生效）
//   - Claude runtime 返其它 err → ChatPermissionModeInternal
func (s *chatSvc) SetPermissionMode(ctx context.Context, req *SetPermissionModeRequest) (*SetPermissionModeResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if !isKnownPermissionMode(req.Mode) {
		return nil, i18n.NewError(ctx, code.ChatPermissionModeInvalid)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	_, be, _, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}

	// 后端变更类操作同样必须先对账未决的 Pi transcript recovery：会话可能带着 Pi
	// 替换标记（标记把 provider session + running 态原子落库）后切换了后端，把
	// permission mode 下发到新后端前必须先把标记恢复/清理掉，否则 provider 状态
	// 会建立在未决恢复之上。这里不加 session turn 锁：SetPermissionMode 是允许
	// claudecode 轮内切换的轻量操作（与 Enqueue 一致），真正在跑的轮子在其起点已
	// 通过 gate 对账；未决标记只可能由持有锁的 Pi 轮产生，而 Pi 后端在下方
	// supported 检查就被拒，故此处对账不会与创建标记的轮子并发。
	if _, rerr := s.reconcileTranscriptReplacement(ctx, sess, be); rerr != nil {
		return nil, operationFailedWithCause(ctx, rerr,
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type))
	}

	backendType := agent_backend_entity.BackendType(be.Type)
	meta, supported := permissionModeMetaFor(backendType)
	if !supported {
		return nil, i18n.NewError(ctx, code.ChatPermissionModeUnsupported)
	}
	mode, err := validateRequestedPermissionMode(ctx, backendType, req.Mode)
	if err != nil {
		return nil, err
	}
	if !meta.SwitchableDuringTurn &&
		(sess.AgentStatus == "running" || sess.AgentStatus == "waiting") {
		return nil, i18n.NewError(ctx, code.ChatSendInFlight)
	}
	if err := s.persistPermissionMode(ctx, sess, be, mode); err != nil {
		return nil, err
	}
	return &SetPermissionModeResponse{Applied: true, Mode: mode}, nil
}

func (s *chatSvc) CancelQueued(ctx context.Context, req *CancelQueuedRequest) (*CancelQueuedResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	_, be, _, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}

	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatCancelUnsupported)
	}
	canceler, ok := runner.(agentruntime.SteerCanceler)
	if !ok {
		return nil, i18n.NewError(ctx, code.ChatCancelUnsupported)
	}
	removed, err := canceler.CancelSteer(ctx, sess.ID, req.QueuedID)
	if err != nil {
		switch {
		case errors.Is(err, agentruntime.ErrNoActiveTurn):
			return nil, i18n.NewError(ctx, code.ChatSteerNoActive)
		case errors.Is(err, agentruntime.ErrSteerNotFound):
			return nil, i18n.NewError(ctx, code.ChatCancelNotFound)
		default:
			return nil, i18n.NewError(ctx, code.ChatSteerInternal)
		}
	}
	if removed == nil {
		removed = []string{}
	}
	return &CancelQueuedResponse{Removed: removed}, nil
}

// newQueuedID generates a UUID v4 string used as the SteerInbox / cago
// pending-steer key. Same approach as agentruntime/claudecode.newUUIDv4 but
// kept local to avoid leaking a public helper across packages — Enqueue is
// the only caller that needs it.
func newQueuedID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.BigEndian.PutUint64(b[0:8], uint64(time.Now().UnixNano()))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Regenerate 截到目标 assistant 消息之前，用同一段 user 文本重新走一遍 turn。
//
// 支持策略：
//   - builtin: history 每轮从 chat_messages 重建，删 DB 即足够。
//   - claudecode: 透传 ForkAnchor 给 runner，由 CLI fork 到新 session。
//   - codex: 根据目标 user 到末尾的 user 消息数计算 thread/rollback 的 numTurns。
//   - piagent: 透传持久化的精确 user entry ID；旧空 anchor 显式拒绝。
func (s *chatSvc) Regenerate(ctx context.Context, req *RegenerateRequest) (*SendResponse, error) {
	if req == nil || req.SessionID <= 0 || req.MessageID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	target, err := chat_repo.Message().Find(ctx, req.MessageID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if target == nil || (target.SessionID >= 0 && target.SessionID != sess.ID) {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}
	if target.Role != "assistant" {
		return nil, i18n.NewError(ctx, code.ChatRegenerateNotAssistant)
	}

	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}
	gate, err := s.acquireTurnGate(ctx, sess, be)
	if err != nil {
		return nil, err
	}
	gateOwned := true
	defer func() {
		if gateOwned {
			gate.lock.Unlock()
		}
	}()
	if gate.reconciled {
		target, err = chat_repo.Message().Find(ctx, req.MessageID)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		if target == nil || target.SessionID != sess.ID {
			return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
		}
		if target.Role != "assistant" {
			return nil, i18n.NewError(ctx, code.ChatRegenerateNotAssistant)
		}
	}
	if target.SessionID != sess.ID {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}

	// 找紧邻 target 之前的最后一条 user 消息（按 seq）。
	all, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	var userAnchor *chat_entity.Message
	for _, m := range all {
		if m.Seq < target.Seq && m.Role == "user" {
			userAnchor = m
		}
	}
	if userAnchor == nil {
		return nil, i18n.NewError(ctx, code.ChatRegenerateNoUserAnchor)
	}
	userBlocks, err := userAnchor.GetBlocks()
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatBlocksMalformed)
	}

	forkAnchor, ferr := s.backendForkAnchor(ctx, sess, be, userAnchor)
	if ferr != nil {
		return nil, ferr
	}

	if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, false); err != nil {
		return nil, err
	}

	// preTx 在同一事务里先截掉 user 锚点（含）开始的全部历史，
	// 然后 startTurn 的标准路径会以新的 NextSeq 写回 user + assistant。
	anchorSeq := userAnchor.Seq
	var replacement *transcriptReplacementLifecycle
	preTx := func(txCtx context.Context) error {
		_, derr := chat_repo.Message().DeleteFromSeq(txCtx, sess.ID, anchorSeq)
		return derr
	}
	if be.IsPiAgent() {
		replacement = newTranscriptReplacementLifecycle(sess.ID, anchorSeq, req.MessageID)
		preTx = nil
	}
	gateOwned = false
	return s.startTurn(ctx, sess, a, be, prov, userBlocks, preTx, replacement, forkAnchor, turnExtras{}, gate.lock)
}

// Edit 编辑历史 user 消息后用新文本重跑 turn。截到目标 user 消息（含）开始的全部
// chat_messages，把新文本作为 user 消息再走 startTurn。
//
// 跟 Regenerate 的区别：
//   - target 必须是 user 而非 assistant
//   - 用 req.Text 替换原始文本（Regenerate 是回放原文）
//   - fork anchor 直接拿 target.ForkAnchor（Regenerate 是先找上一条 user）
func (s *chatSvc) Edit(ctx context.Context, req *EditRequest) (*SendResponse, error) {
	if req == nil || req.SessionID <= 0 || req.MessageID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	if len(text) > chat_entity.MessageTextMaxBytes {
		return nil, i18n.NewError(ctx, code.ChatTextTooLong)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	target, err := chat_repo.Message().Find(ctx, req.MessageID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if target == nil || (target.SessionID >= 0 && target.SessionID != sess.ID) {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}
	if target.Role != "user" {
		return nil, i18n.NewError(ctx, code.ChatEditNotUser)
	}

	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}
	gate, err := s.acquireTurnGate(ctx, sess, be)
	if err != nil {
		return nil, err
	}
	gateOwned := true
	defer func() {
		if gateOwned {
			gate.lock.Unlock()
		}
	}()
	if gate.reconciled {
		target, err = chat_repo.Message().Find(ctx, req.MessageID)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		if target == nil || target.SessionID != sess.ID {
			return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
		}
		if target.Role != "user" {
			return nil, i18n.NewError(ctx, code.ChatEditNotUser)
		}
	}
	if target.SessionID != sess.ID {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}
	targetBlocks, err := target.GetBlocks()
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatBlocksMalformed)
	}

	forkAnchor, ferr := s.backendForkAnchor(ctx, sess, be, target)
	if ferr != nil {
		return nil, ferr
	}

	if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, false); err != nil {
		return nil, err
	}

	anchorSeq := target.Seq
	var replacement *transcriptReplacementLifecycle
	preTx := func(txCtx context.Context) error {
		_, derr := chat_repo.Message().DeleteFromSeq(txCtx, sess.ID, anchorSeq)
		return derr
	}
	if be.IsPiAgent() {
		replacement = newTranscriptReplacementLifecycle(sess.ID, anchorSeq, req.MessageID)
		preTx = nil
	}
	gateOwned = false
	return s.startTurn(
		ctx,
		sess,
		a,
		be,
		prov,
		replaceTextPreserveImages(text, targetBlocks),
		preTx,
		replacement,
		forkAnchor,
		turnExtras{},
		gate.lock,
	)
}

func replaceTextPreserveImages(text string, old []blocks.ContentBlock) []blocks.ContentBlock {
	out := []blocks.ContentBlock{&blocks.TextBlock{Text: text}}
	for _, b := range old {
		switch img := b.(type) {
		case blocks.ImageBlock:
			out = append(out, img)
		case *blocks.ImageBlock:
			if img != nil {
				out = append(out, img)
			}
		}
	}
	return out
}

// transcriptReplacementLifecycle owns one Pi replacement generation. Its
// marker-derived hidden namespace preserves the exact original rows until the
// prepared process acknowledges the prompt.
type transcriptReplacementLifecycle struct {
	sessionID        int64
	fromSeq          int
	requestMessageID int64
	recovery         *chat_repo.ReplacementRecovery
}

func newTranscriptReplacementLifecycle(sessionID int64, fromSeq int, requestMessageID int64) *transcriptReplacementLifecycle {
	return &transcriptReplacementLifecycle{
		sessionID:        sessionID,
		fromSeq:          fromSeq,
		requestMessageID: requestMessageID,
	}
}

func (r *transcriptReplacementLifecycle) activate(
	txCtx context.Context,
	sess *chat_entity.Session,
	providerSessionID string,
	userMsg, assistantMsg *chat_entity.Message,
) error {
	if r == nil || sess == nil {
		return nil
	}
	recovery := &chat_repo.ReplacementRecovery{
		SessionID:            r.sessionID,
		FromSeq:              r.fromSeq,
		RequestMessageID:     r.requestMessageID,
		OldProviderSessionID: sess.ProviderSessionID,
		NewProviderSessionID: providerSessionID,
		OldAgentStatus:       sess.AgentStatus,
		OldLastMessageAt:     sess.LastMessageAt,
		State:                chat_repo.ReplacementRecoveryPending,
	}
	marker, err := chat_repo.NewReplacementRecoveryMarker(recovery)
	if err != nil {
		return err
	}
	if err := chat_repo.Message().Create(txCtx, marker); err != nil {
		return err
	}
	recovery.MarkerID = marker.ID
	recovery.RecoverySessionID, err = chat_repo.ReplacementRecoverySessionID(marker.ID)
	if err != nil {
		return err
	}
	if err := chat_repo.EnsureReplacementRecoveryNamespaceAvailable(txCtx, recovery.RecoverySessionID); err != nil {
		return err
	}
	if _, err := chat_repo.MoveMessagesFromSeq(txCtx, r.sessionID, recovery.RecoverySessionID, r.fromSeq); err != nil {
		return err
	}

	userMsg.SessionID = r.sessionID
	userMsg.Seq = r.fromSeq
	if err := chat_repo.Message().Create(txCtx, userMsg); err != nil {
		return err
	}
	assistantMsg.SessionID = r.sessionID
	assistantMsg.Seq = r.fromSeq + 1
	if err := chat_repo.Message().Create(txCtx, assistantMsg); err != nil {
		return err
	}
	recovery.UserMessageID = userMsg.ID
	recovery.AssistantMessageID = assistantMsg.ID
	finalMarker, err := chat_repo.NewReplacementRecoveryMarker(recovery)
	if err != nil {
		return err
	}
	finalMarker.ID = marker.ID
	finalMarker.SessionID = recovery.RecoverySessionID
	finalMarker.Createtime = marker.Createtime
	if err := chat_repo.Message().Update(txCtx, finalMarker); err != nil {
		return err
	}
	r.recovery = recovery
	return nil
}

const transcriptRecoveryTimeout = 5 * time.Second

func replacementRecoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := db.WithContextDB(context.Background(), db.Ctx(ctx))
	return context.WithTimeout(base, transcriptRecoveryTimeout)
}

func (s *chatSvc) restoreTranscriptReplacement(
	ctx context.Context,
	replacement *transcriptReplacementLifecycle,
	sess *chat_entity.Session,
) error {
	if replacement == nil || replacement.recovery == nil {
		return nil
	}
	recoveryCtx, cancel := replacementRecoveryContext(ctx)
	defer cancel()
	recovery := replacement.recovery
	if err := db.Ctx(recoveryCtx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(recoveryCtx, tx)
		if err := chat_repo.EnsureReplacementActiveTailOwned(txCtx, recovery); err != nil {
			return err
		}
		if err := chat_repo.RestoreReplacementSession(txCtx, recovery); err != nil {
			return err
		}
		deleted, err := chat_repo.DeleteOwnedReplacementMessages(
			txCtx, recovery.SessionID, recovery.UserMessageID, recovery.AssistantMessageID,
		)
		if err != nil {
			return err
		}
		if deleted != 2 {
			return chat_repo.ErrReplacementOwnershipLost
		}
		moved, err := chat_repo.MoveMessagesFromSeq(
			txCtx, recovery.RecoverySessionID, recovery.SessionID, recovery.FromSeq,
		)
		if err != nil {
			return err
		}
		if moved == 0 {
			return chat_repo.ErrReplacementOwnershipLost
		}
		deleted, err = chat_repo.DeleteReplacementRecovery(txCtx, recovery.RecoverySessionID)
		if err != nil {
			return err
		}
		if deleted != 1 {
			return chat_repo.ErrReplacementOwnershipLost
		}
		return nil
	}); err != nil {
		return err
	}
	sess.ProviderSessionID = recovery.OldProviderSessionID
	sess.AgentStatus = recovery.OldAgentStatus
	sess.LastMessageAt = recovery.OldLastMessageAt
	sess.ApplyDerivedFields()
	return nil
}

func (s *chatSvc) cleanupTranscriptReplacementRecovery(
	ctx context.Context,
	recovery *chat_repo.ReplacementRecovery,
) error {
	recoveryCtx, cancel := replacementRecoveryContext(ctx)
	defer cancel()
	return db.Ctx(recoveryCtx).Transaction(func(tx *gorm.DB) error {
		deleted, err := chat_repo.DeleteReplacementRecovery(
			db.WithContextDB(recoveryCtx, tx), recovery.RecoverySessionID,
		)
		if err != nil {
			return err
		}
		if deleted == 0 {
			return chat_repo.ErrReplacementOwnershipLost
		}
		return nil
	})
}

// reconcileTranscriptReplacement is the single session-level recovery boundary
// for a session that may still own a Pi replacement marker. Pi activation writes
// AgentStatus=running and the new provider session atomically with the marker, so
// that durable state keeps the gate active even if the configured backend changes.
// Callers hold the session turn lock before entering it.
func (s *chatSvc) reconcileTranscriptReplacement(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
) (bool, error) {
	if sess == nil || be == nil ||
		(!be.IsPiAgent() && (sess.AgentStatus != "running" || !sess.HasProviderSession())) {
		return false, nil
	}
	recovery, err := chat_repo.FindReplacementRecoveryForSession(ctx, sess.ID)
	if err != nil || recovery == nil {
		return false, err
	}
	if recovery.State == chat_repo.ReplacementRecoveryAcknowledged {
		if sess.ProviderSessionID != recovery.NewProviderSessionID {
			return false, chat_repo.ErrReplacementOwnershipLost
		}
		return true, s.cleanupTranscriptReplacementRecovery(ctx, recovery)
	}
	replacement := &transcriptReplacementLifecycle{
		sessionID:        recovery.SessionID,
		fromSeq:          recovery.FromSeq,
		requestMessageID: recovery.RequestMessageID,
		recovery:         recovery,
	}
	if err := s.restoreTranscriptReplacement(ctx, replacement, sess); err != nil {
		return false, err
	}
	return true, nil
}

func (s *chatSvc) finalizeTranscriptReplacement(
	ctx context.Context,
	replacement *transcriptReplacementLifecycle,
) error {
	if replacement == nil || replacement.recovery == nil {
		return nil
	}
	recoveryCtx, cancel := replacementRecoveryContext(ctx)
	defer cancel()
	recovery := replacement.recovery
	var acknowledgeErr error
	for range 2 {
		candidate := *recovery
		acknowledgeErr = db.Ctx(recoveryCtx).Transaction(func(tx *gorm.DB) error {
			return chat_repo.AcknowledgeReplacementRecovery(db.WithContextDB(recoveryCtx, tx), &candidate)
		})
		if acknowledgeErr == nil {
			recovery.State = chat_repo.ReplacementRecoveryAcknowledged
			break
		}
	}
	if acknowledgeErr != nil {
		return fmt.Errorf("acknowledge Pi transcript recovery: %w", acknowledgeErr)
	}
	if err := db.Ctx(recoveryCtx).Transaction(func(tx *gorm.DB) error {
		deleted, err := chat_repo.DeleteReplacementRecovery(
			db.WithContextDB(recoveryCtx, tx), recovery.RecoverySessionID,
		)
		if err != nil {
			return err
		}
		if deleted == 0 {
			return chat_repo.ErrReplacementOwnershipLost
		}
		return nil
	}); err != nil {
		return fmt.Errorf("cleanup Pi transcript recovery: %w", err)
	}
	return nil
}

func messageHasImage(m *chat_entity.Message) bool {
	bs, err := m.GetBlocks()
	if err != nil {
		return false
	}
	for _, b := range bs {
		switch img := b.(type) {
		case blocks.ImageBlock:
			return true
		case *blocks.ImageBlock:
			if img != nil {
				return true
			}
		}
	}
	return false
}

// backendForkAnchor 是 Regenerate / Edit 共享的"按后端类型决定 fork 锚点"分流逻辑。
// claudecode 首轮 user msg 没有 anchor 时会清空 sess.ProviderSessionID，让上层
// startTurn → runner 当作新建会话发起；Pi 只有明确失败且从未建立原生会话的首轮
// 才能无 fork 重试，已经建立过上下文的会话丢失 provider ID 后必须 fail closed。
func (s *chatSvc) backendForkAnchor(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	userMsg *chat_entity.Message,
) (string, error) {
	if !sess.HasProviderSession() {
		if be.IsPiAgent() {
			failedFirstTurn, err := s.isFailedFirstPiTurn(ctx, sess, userMsg)
			if err != nil {
				return "", err
			}
			if !failedFirstTurn {
				return "", i18n.NewError(ctx, code.ChatProviderSessionGone)
			}
		}
		return "", nil
	}
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeBuiltin:
		return "", nil
	case agent_backend_entity.TypeClaudeCode:
		anchor := userMsg.ForkAnchor
		if anchor == "" {
			sess.SetProviderSession("")
		}
		return anchor, nil
	case agent_backend_entity.TypeCodex:
		return s.codexRollbackAnchor(ctx, sess, userMsg)
	case agent_backend_entity.TypePiAgent:
		anchor, ok := normalizedPiForkAnchor(userMsg)
		if !ok {
			return "", i18n.NewError(ctx, code.ChatRegenerateNoUserAnchor)
		}
		return anchor, nil
	default:
		runner := agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type))
		if _, ok := runner.(agentruntime.Rewinder); !ok {
			return "", i18n.NewError(ctx, code.ChatRegenerateUnsupported)
		}
		return "", nil
	}
}

func normalizedPiForkAnchor(userMsg *chat_entity.Message) (string, bool) {
	if userMsg == nil || userMsg.ForkAnchor == "" || strings.TrimSpace(userMsg.ForkAnchor) != userMsg.ForkAnchor {
		return "", false
	}
	// Entry IDs are opaque native Pi identities. Reject malformed persisted values
	// instead of trimming them into a different provider identity.
	return userMsg.ForkAnchor, true
}

func (s *chatSvc) isFailedFirstPiTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	userMsg *chat_entity.Message,
) (bool, error) {
	_, hasForkAnchor := normalizedPiForkAnchor(userMsg)
	if sess == nil || userMsg == nil || hasForkAnchor {
		return false, nil
	}
	messages, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return false, operationFailedWithCause(ctx, err)
	}
	if len(messages) != 2 {
		return false, nil
	}
	var firstUser, failedAssistant *chat_entity.Message
	for _, message := range messages {
		switch message.Role {
		case "user":
			if firstUser != nil {
				return false, nil
			}
			firstUser = message
		case "assistant":
			if failedAssistant != nil {
				return false, nil
			}
			failedAssistant = message
		default:
			return false, nil
		}
	}
	if firstUser == nil || failedAssistant == nil || firstUser.ID != userMsg.ID ||
		firstUser.Seq >= failedAssistant.Seq || strings.TrimSpace(failedAssistant.ErrorText) == "" {
		return false, nil
	}
	assistantBlocks, err := failedAssistant.GetBlocks()
	if err != nil {
		return false, i18n.NewError(ctx, code.ChatBlocksMalformed)
	}
	if len(assistantBlocks) != 0 {
		return false, nil
	}
	return true, nil
}

func (s *chatSvc) codexRollbackAnchor(ctx context.Context, sess *chat_entity.Session, userMsg *chat_entity.Message) (string, error) {
	msgs, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return "", operationFailedWithCause(ctx, err)
	}
	numTurns := 0
	for _, m := range msgs {
		if m.Seq >= userMsg.Seq && m.Role == "user" {
			numTurns++
		}
	}
	if numTurns <= 0 {
		return "", i18n.NewError(ctx, code.ChatRegenerateNoUserAnchor)
	}
	return strconv.Itoa(numTurns), nil
}

// startTurn is the common tail shared by Send and Regenerate: acquire the
// per-session lock, persist a fresh user+assistant pair in a transaction (with
// an optional pre-step running inside the same tx), then kick off runTurn. Pi
// replacements first prepare, then atomically activate recovery+messages+native
// identity before Start is allowed to send the prompt.
//
// Caller is responsible for resolving sess/a/be/prov consistently with the
// session's actual agent (Send for new sessions, Regenerate for in-place
// rewind). userBlocks is the user message body that will be re-played to the runtime.
//
// preTx, if non-nil, runs at the very top of a normal turn transaction. Non-Pi
// rewinds use it to free seq numbers. Pi replacement activation owns its full
// transaction separately. Returning an error aborts the whole turn (and unlocks).
// turnExtras 是从 SendRequest 透传到 runTurn 的领域无关可选项;普通会话一律零值。
// 在同一会话的自动续轮(auto-continue)里需要保持不变,所以随 runTurn 一路携带。
type turnExtras struct {
	// emitTurnStartedBypass: 见 SendRequest.EmitTurnStartedBypass。startTurn 在建好
	// assistant 消息后, 经会话级旁路把 per-turn 流名推给已打开的查看者。
	emitTurnStartedBypass bool
}

type sessionTurnGate struct {
	lock       *trylockMutex
	reconciled bool
}

func (s *chatSvc) acquireTurnGate(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
) (*sessionTurnGate, error) {
	lock := s.lockFor(sess.ID)
	if !lock.TryLock() {
		return nil, i18n.NewError(ctx, code.ChatSendInFlight)
	}
	reconciled, err := s.reconcileTranscriptReplacement(ctx, sess, be)
	if err != nil {
		lock.Unlock()
		return nil, operationFailedWithCause(ctx, err,
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type))
	}
	return &sessionTurnGate{lock: lock, reconciled: reconciled}, nil
}

func (s *chatSvc) startTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
	userBlocks []blocks.ContentBlock,
	preTx func(txCtx context.Context) error,
	replacement *transcriptReplacementLifecycle,
	forkAnchor string,
	extras turnExtras,
	prelocked *trylockMutex,
) (*SendResponse, error) {
	lock := prelocked
	if lock == nil {
		gate, err := s.acquireTurnGate(ctx, sess, be)
		if err != nil {
			return nil, err
		}
		lock = gate.lock
	}

	userMsg := &chat_entity.Message{SessionID: sess.ID, Role: "user", DeviceID: be.DeviceID}
	_ = userMsg.SetBlocks(userBlocks)

	model := ""
	if prov != nil {
		model = prov.Model
	}
	assistantMsg := &chat_entity.Message{
		SessionID:  sess.ID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
		Model:      model,
	}

	var (
		prepared                *preparedTurnRun
		turnCtx                 context.Context
		cancel                  context.CancelFunc
		stopRequestCancel       func() bool
		turnControl             *activeTurnControl
		startupCancelRegistered bool
	)
	clearSynchronousTurn := func() {
		if stopRequestCancel != nil {
			stopRequestCancel()
			stopRequestCancel = nil
		}
		if cancel != nil {
			cancel()
		}
		if startupCancelRegistered {
			s.activeCancels.CompareAndDelete(sess.ID, turnControl)
			s.aborted.Delete(sess.ID)
			startupCancelRegistered = false
		}
	}
	// Pi prepares/restores its RPC process and, when requested, forks before the
	// transaction, but deliberately withholds the prompt. Register cancellation
	// before preflight so Stop and request cancellation reach both phases.
	if replacement != nil && be.IsPiAgent() {
		runCtx := db.WithContextDB(context.Background(), db.Ctx(ctx))
		turnCtx, cancel = context.WithCancel(runCtx)
		turnControl = &activeTurnControl{cancel: cancel}
		stopRequestCancel = context.AfterFunc(ctx, cancel)
		s.activeCancels.Store(sess.ID, turnControl)
		startupCancelRegistered = true
		var err error
		prepared, err = s.prepareTurnRun(turnCtx, sess, a, be, prov, userMsg, assistantMsg, forkAnchor, false, true)
		if err != nil {
			clearSynchronousTurn()
			lock.Unlock()
			logger.Ctx(ctx).Warn("chat_svc.startTurn: pi fork startup failed",
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type),
				zap.String("forkAnchor", forkAnchor),
				zap.String("errorType", fmt.Sprintf("%T", err)))
			return nil, err
		}
	}

	if replacement == nil {
		sess.AgentStatus = "running"
		if err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
			txCtx := db.WithContextDB(ctx, tx)
			if preTx != nil {
				if err := preTx(txCtx); err != nil {
					return err
				}
			}
			nextSeq, err := chat_repo.Message().NextSeq(txCtx, sess.ID)
			if err != nil {
				return err
			}
			userMsg.Seq = nextSeq
			if err := chat_repo.Message().Create(txCtx, userMsg); err != nil {
				return err
			}
			assistantMsg.Seq = nextSeq + 1
			if err := chat_repo.Message().Create(txCtx, assistantMsg); err != nil {
				return err
			}
			sess.LastMessageAt = time.Now().UnixMilli()
			return chat_repo.Session().Update(txCtx, sess)
		}); err != nil {
			lock.Unlock()
			return nil, operationFailedWithCause(ctx, err,
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type))
		}
	} else {
		providerSessionID, err := prepared.providerSessionIDBeforeStart()
		if err != nil {
			clearSynchronousTurn()
			s.discardPreparedTurn(sess.ID, prepared)
			lock.Unlock()
			return nil, operationFailedWithCause(ctx, err,
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type))
		}
		runningSession := *sess
		runningSession.AgentStatus = "running"
		runningSession.LastMessageAt = time.Now().UnixMilli()
		runningSession.SetProviderSession(providerSessionID)
		if err := db.Ctx(turnCtx).Transaction(func(tx *gorm.DB) error {
			txCtx := db.WithContextDB(turnCtx, tx)
			if err := replacement.activate(txCtx, sess, providerSessionID, userMsg, assistantMsg); err != nil {
				return err
			}
			return chat_repo.Session().Update(txCtx, &runningSession)
		}); err != nil {
			clearSynchronousTurn()
			s.discardPreparedTurn(sess.ID, prepared)
			lock.Unlock()
			return nil, operationFailedWithCause(ctx, err,
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type))
		}
		*sess = runningSession
	}

	if prepared != nil {
		if err := prepared.start(turnCtx); err != nil {
			mappingSession := *sess
			mappingSession.ProviderSessionID = ""
			err = s.mapTurnError(ctx, &mappingSession, be, err)
			clearSynchronousTurn()
			s.discardPreparedTurn(sess.ID, prepared)
			if restoreErr := s.restoreTranscriptReplacement(ctx, replacement, sess); restoreErr != nil {
				err = errors.Join(err, fmt.Errorf("restore Pi transcript: %w", restoreErr))
			}
			lock.Unlock()
			logger.Ctx(ctx).Warn("chat_svc.startTurn: pi prompt startup failed",
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type),
				zap.String("forkAnchor", forkAnchor),
				zap.String("errorType", fmt.Sprintf("%T", err)))
			return nil, err
		}
		if finalizeErr := s.finalizeTranscriptReplacement(ctx, replacement); finalizeErr != nil {
			clearSynchronousTurn()
			s.discardPreparedTurn(sess.ID, prepared)
			if replacement.recovery.State == chat_repo.ReplacementRecoveryPending {
				if restoreErr := s.restoreTranscriptReplacement(ctx, replacement, sess); restoreErr != nil {
					finalizeErr = errors.Join(finalizeErr, fmt.Errorf("restore Pi transcript: %w", restoreErr))
				}
			} else {
				sess.AgentStatus = "error"
				sess.ApplyDerivedFields()
				recoveryCtx, cancelRecovery := replacementRecoveryContext(ctx)
				if statusErr := chat_repo.Session().Update(recoveryCtx, sess); statusErr != nil {
					finalizeErr = errors.Join(finalizeErr, fmt.Errorf("persist failed Pi turn status: %w", statusErr))
				}
				cancelRecovery()
			}
			lock.Unlock()
			return nil, operationFailedWithCause(ctx, finalizeErr,
				zap.Int64("sessionId", sess.ID),
				zap.Int64("agentId", a.ID),
				zap.String("backendType", be.Type),
				zap.String("recoveryState", string(replacement.recovery.State)))
		}
		if stopRequestCancel != nil {
			stopRequestCancel()
			stopRequestCancel = nil
		}
	}

	stream := StreamName(sess.ID, assistantMsg.ID)

	// 非查看者发起的轮(群成员轮经 scheduler dispatch):per-turn 流名只有发起者能从
	// Send 响应拿到,该会话已打开(可能在后台)的 ChatPanel 拿不到 → 不接流、不翻 running。
	// 复用 autonomous 会话级旁路把流名 + 新 assistant 行推给它,让它走与自主轮相同的
	// openStream 路径实时渲染。前端 Send 默认不带此标志,避免发起者重复 openStream 双开流。
	if extras.emitTurnStartedBypass {
		s.emitter.Emit(ctx, AutonomousStreamName(sess.ID), ChatStreamEvent{
			Kind:             StreamAutonomousStarted,
			Stream:           stream,
			AssistantMessage: chatMessageForEvent(sess, assistantMsg),
		})
	}

	s.markStreamRunningForTest(assistantMsg.ID)
	if prepared == nil {
		runCtx := db.WithContextDB(context.Background(), db.Ctx(ctx))
		turnCtx, cancel = context.WithCancel(runCtx)
		turnControl = &activeTurnControl{cancel: cancel}
		// Non-prepared turns become cancellable immediately before async dispatch.
		s.activeCancels.Store(sess.ID, turnControl)
	}
	// Prepared Pi turns were registered before synchronous preflight; all other
	// turns are registered above. Either way Stop can cancel before gogo.Go runs.
	gogo.Go(func() error {
		// defer 顺序：LIFO。先注册 unlock，最后释放；中间的 cancel cleanup
		// 跑在 lock 还持有期间，新 turn 起不来 → 直接 Delete 安全。
		defer lock.Unlock()
		defer s.markStreamDoneForTest(assistantMsg.ID)
		defer func() {
			s.activeCancels.CompareAndDelete(sess.ID, turnControl)
			cancel() // 兜底：runTurn 自己没 cancel（正常完成路径）也补一刀，无副作用
		}()
		s.runTurn(turnCtx, sess, a, be, prov, userMsg, assistantMsg, stream, forkAnchor, false, prepared, extras)
		return nil
	}, gogo.WithIgnorePanic())

	return &SendResponse{
		SessionID:          sess.ID,
		UserMessageID:      userMsg.ID,
		AssistantMessageID: assistantMsg.ID,
		Stream:             stream,
	}, nil
}

func (s *chatSvc) discardPreparedTurn(sessionID int64, prepared *preparedTurnRun) {
	if prepared == nil {
		return
	}
	if prepared.deferred != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), transcriptRecoveryTimeout)
		_ = prepared.deferred.Close(closeCtx)
		cancel()
	} else if prepared.events != nil {
		if aborter, ok := prepared.runner.(agentruntime.Aborter); ok {
			_, _ = aborter.Abort(context.Background(), sessionID, 0)
		}
	}
	if prepared.events == nil {
		prepared.releaseResources()
		return
	}
	gogo.Go(func() error {
		for range prepared.events {
		}
		prepared.releaseResources()
		return nil
	}, gogo.WithIgnorePanic())
}

func (s *chatSvc) startCompactTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
	prelocked *trylockMutex,
) (*CompactResponse, error) {
	lock := prelocked
	if lock == nil {
		gate, err := s.acquireTurnGate(ctx, sess, be)
		if err != nil {
			return nil, err
		}
		lock = gate.lock
	}

	model := ""
	if prov != nil {
		model = prov.Model
	}
	assistantMsg := &chat_entity.Message{
		SessionID:  sess.ID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
		Model:      model,
	}

	if err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		nextSeq, err := chat_repo.Message().NextSeq(txCtx, sess.ID)
		if err != nil {
			return err
		}
		assistantMsg.Seq = nextSeq
		if err := chat_repo.Message().Create(txCtx, assistantMsg); err != nil {
			return err
		}
		sess.AgentStatus = "running"
		sess.NeedsAttention = false
		sess.LastMessageAt = time.Now().UnixMilli()
		return chat_repo.Session().Update(txCtx, sess)
	}); err != nil {
		lock.Unlock()
		return nil, operationFailedWithCause(ctx, err,
			zap.Int64("sessionId", sess.ID),
			zap.Int64("agentId", a.ID),
			zap.String("backendType", be.Type))
	}

	stream := StreamName(sess.ID, assistantMsg.ID)
	s.markStreamRunningForTest(assistantMsg.ID)
	runCtx := db.WithContextDB(context.Background(), db.Ctx(ctx))
	turnCtx, cancel := context.WithCancel(runCtx)
	turnControl := &activeTurnControl{cancel: cancel}
	s.activeCancels.Store(sess.ID, turnControl)
	gogo.Go(func() error {
		defer lock.Unlock()
		defer s.markStreamDoneForTest(assistantMsg.ID)
		defer func() {
			s.activeCancels.CompareAndDelete(sess.ID, turnControl)
			cancel()
		}()
		s.runTurn(turnCtx, sess, a, be, prov, nil, assistantMsg, stream, "", true, nil, turnExtras{})
		return nil
	}, gogo.WithIgnorePanic())

	return &CompactResponse{
		SessionID:          sess.ID,
		AssistantMessageID: assistantMsg.ID,
		Stream:             stream,
	}, nil
}

// markSessionWaiting 把 session 翻成「等用户操作」态：AgentStatus="waiting"，
// 并推送带派生 NeedsAttention=true 的 StreamSessionStatus patch 给前端。turn 进行中遇到 AskUserQuestion / ToolPermission
// 请求时调用，应答后由 markSessionRunning 翻回。
//
// 写库用 context.WithoutCancel(ctx)：用户在等待期间点「停止」会 cancel turnCtx，但等待状态本身
// 仍需要持久化（否则下次 LoadSession 会显示旧的 running，sidebar attention 也会丢）。
// 当前 sess.AgentStatus 已经是目标值时短路，避免重复 emit。
func (s *chatSvc) markSessionWaiting(ctx context.Context, sess *chat_entity.Session, stream string) {
	if sess == nil || sess.IsWaitingForUser() {
		return
	}
	sess.AgentStatus = "waiting"
	sess.NeedsAttention = true
	_ = chat_repo.Session().Update(context.WithoutCancel(ctx), sess)
	logger.Ctx(ctx).Info("chat_svc: session_status emit",
		zap.Int64("sessionId", sess.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Bool("needsAttention", sess.NeedsAttention),
		zap.String("source", "markSessionWaiting"))
	s.emitter.Emit(ctx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
			BgRunning:      s.bgRunningActive(sess.ID),
		},
	})
}

// markSessionRunning 把 session 从 waiting 翻回 running：清掉 NeedsAttention。
// 在 EventAskUserQuestionAnswered / EventToolPermissionResolved 处调用。
func (s *chatSvc) markSessionRunning(ctx context.Context, sess *chat_entity.Session, stream string) {
	if sess == nil || (sess.AgentStatus == "running" && !sess.IsWaitingForUser()) {
		return
	}
	sess.AgentStatus = "running"
	sess.NeedsAttention = false
	_ = chat_repo.Session().Update(context.WithoutCancel(ctx), sess)
	logger.Ctx(ctx).Info("chat_svc: session_status emit",
		zap.Int64("sessionId", sess.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Bool("needsAttention", sess.NeedsAttention),
		zap.String("source", "markSessionRunning"))
	s.emitter.Emit(ctx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
			BgRunning:      s.bgRunningActive(sess.ID),
		},
	})
}

// persistSessionStatus 持久化 turn 收尾的 session 状态翻转(idle/error/waiting)：
// 写失败时重试一次,仍失败则把错误返回出来 —— 不再像旧的 `_ = Update` 那样静默吞掉。
//
// 背景:状态写丢失会让会话永久停在 running(turn 其实已 finalize 成 idle),
// 唯一兜底是下次启动 ResetActiveSessions 把残留 running 翻成 error。session 162
// 正是 abort finalize 成 idle 后这一刀写库失败被 `_` 吞掉,结果 DB 卡在 running、
// updatetime 停在写丢失之前。这里做到「重试 + 不静默」,并把真实失败原因(锁竞争 /
// 收尾期连接关闭等)暴露到日志,便于后续定位根因。
func (s *chatSvc) persistSessionStatus(ctx context.Context, sess *chat_entity.Session) error {
	if sess == nil {
		return nil
	}
	err := chat_repo.Session().Update(ctx, sess)
	if err == nil {
		return nil
	}
	logger.Ctx(ctx).Warn("chat_svc: session status persist failed, retrying",
		zap.Int64("sessionId", sess.ID),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Error(err))
	if err := chat_repo.Session().Update(ctx, sess); err != nil {
		logger.Ctx(ctx).Error("chat_svc: session status persist failed after retry",
			zap.Int64("sessionId", sess.ID),
			zap.String("agentStatus", sess.AgentStatus),
			zap.Error(err))
		return err
	}
	return nil
}

type preparedTurnRun struct {
	runner     agentruntime.Runtime
	events     <-chan agentruntime.Event
	result     *agentruntime.RunResult
	req        agentruntime.RunRequest
	deferred   piagentrt.PreparedRun
	deferStart bool
	release    func()
	releaseOne sync.Once
}

func (p *preparedTurnRun) providerSessionIDBeforeStart() (string, error) {
	if p == nil || p.deferred == nil {
		return "", errors.New("pi prepared run has no pre-prompt identity")
	}
	identity, ok := p.deferred.(piagentrt.PreparedRunIdentity)
	if !ok {
		return "", errors.New("pi prepared run does not expose pre-prompt identity")
	}
	providerSessionID := strings.TrimSpace(identity.ProviderSessionID())
	if providerSessionID == "" {
		return "", errors.New("pi prepared run returned an empty pre-prompt identity")
	}
	return providerSessionID, nil
}

func (p *preparedTurnRun) start(ctx context.Context) error {
	if p == nil {
		return nil
	}
	var err error
	switch {
	case p.deferred != nil:
		p.events, p.result, err = p.deferred.Start(ctx)
	case p.deferStart:
		p.events, p.result, err = p.runner.Run(ctx, p.req)
	}
	return err
}

func (p *preparedTurnRun) releaseResources() {
	if p == nil {
		return
	}
	p.releaseOne.Do(p.release)
}

func (s *chatSvc) prepareTurnRun(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
	userMsg, assistantMsg *chat_entity.Message,
	forkAnchor string,
	compact bool,
	deferPrompt bool,
) (*preparedTurnRun, error) {
	var (
		runner  agentruntime.Runtime
		release = func() {}
		err     error
	)
	if be.IsRemote() {
		runner, release, err = s.borrowRemoteRuntimeForTurn(ctx, be, sess.ID)
	} else {
		runner, err = s.selectRunner(ctx, be, sess.ID)
	}
	if err != nil {
		// 真错(RemoteRunnerDialFailed / AgentBackendInvalidDevice / AgentBackendInvalidType)
		// 必须透传给调用方交给 failTurn,否则前端永远只看到误导的
		// "unsupported backend type: X",远端 daemon 离线 / DeviceID 失效 /
		// 类型未注册三种情况无法区分。
		fields := make([]zap.Field, 0, 6)
		fields = append(fields,
			zap.Int64("sessionID", sess.ID),
			zap.String("backendType", be.Type),
			zap.String("deviceID", be.DeviceID),
		)
		fields = append(fields, chatRuntimeErrorLogFields(err)...)
		logger.Ctx(ctx).Error("chat_svc.prepareTurnRun: selectRunner failed", fields...)
		return nil, err
	}
	s.bindLocalPiAbort(sess.ID, be, runner)
	fail := func(err error) (*preparedTurnRun, error) {
		release()
		return nil, err
	}
	if userMsg != nil && messageHasImage(userMsg) && !runner.Capabilities().Has(capability.CapImageInput) {
		return fail(agentruntime.ErrUnsupported)
	}

	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil {
		return fail(err)
	}
	req := agentruntime.RunRequest{
		Backend:           be,
		Provider:          prov,
		ModelOverride:     modelOverrideForBackend(sess, be),
		AgentID:           a.ID,
		SessionID:         sess.ID,
		Cwd:               cwd,
		SystemPrompt:      strings.Join(a.GetPrompt(), "\n"),
		ProviderSessionID: sess.ProviderSessionID,
		Compact:           compact,
		ForkAnchor:        forkAnchor,
		MCPServers:        appendTurnMCP(ctx, nil, a, sess.ID, runner.Capabilities().Has(capability.CapMCPTools)),
		EnabledPlugins:    enabledPluginsForTurn(ctx, a, runner.Capabilities().Has(capability.CapSkills)),
	}
	if userMsg != nil {
		req.UserText = textOfMessage(userMsg)
		if bs, err := userMsg.GetBlocks(); err == nil {
			req.UserBlocks = bs
		}
	}
	if be.IsBuiltin() {
		// builtin 没有持久化 session — 把历史从 chat_messages 重建后透传。
		msgs, err := chat_repo.Message().List(ctx, sess.ID)
		if err != nil {
			return fail(err)
		}
		history := make([]agentruntime.HistoryMessage, 0, len(msgs))
		for _, m := range msgs {
			if m.ID == assistantMsg.ID || (userMsg != nil && m.ID == userMsg.ID) {
				continue
			}
			bs, _ := m.GetBlocks()
			history = append(history, agentruntime.HistoryMessage{Role: m.Role, Blocks: bs})
		}
		req.History = history
	}
	if be.IsRemote() {
		// 远端 backend: daemon 自家有 ProviderLookup + Gateway,该自家解。
		// GatewayURL/Token 是 desktop 的 127.0.0.1，Provider 又含明文 APIKey，
		// 都不跨机器；daemon 按 LLMProviderKey 从自己的配置解析。
		req.LLMProviderKey = be.LLMProviderKey
		req.Provider = nil
	} else if shouldSignChatGateway(be) {
		// Claude Code local 需要 gateway token 给 PostToolUse hook；Codex local
		// 没有 hook，不能覆盖其原生 login 并误打到本地 gateway。
		req.GatewayURL, req.GatewayToken = s.signChatTokenFor(ctx, be, sess.ID)
	}
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeClaudeCode:
		req.PermissionMode = normalizeStoredPermissionMode(agent_backend_entity.TypeClaudeCode, sess.PermissionMode)
	case agent_backend_entity.TypeCodex:
		req.CollaborationMode = normalizeStoredPermissionMode(agent_backend_entity.TypeCodex, sess.PermissionMode)
	}

	prepared := &preparedTurnRun{runner: runner, req: req, release: release}
	if deferPrompt {
		if preparer, ok := runner.(piagentrt.RunPreparer); ok {
			deferred, err := preparer.PrepareRun(ctx, req)
			if err != nil {
				return fail(s.mapTurnError(ctx, sess, be, err))
			}
			prepared.deferred = deferred
		} else {
			prepared.deferStart = true
		}
		return prepared, nil
	}

	events, result, err := runner.Run(ctx, req)
	if err != nil {
		// 用户在 Run 返回前就点了停止(网关型后端要先握手,这个窗口是真实存在的):
		// 这是中止而不是故障,交给调用方按 idle 收敛,别让会话卡在 running / 弹错误卡。
		if s.turnAbortedByUser(sess.ID, err) {
			return fail(errTurnAbortedBeforeStream)
		}
		return fail(s.mapTurnError(ctx, sess, be, err))
	}
	prepared.events = events
	prepared.result = result
	return prepared, nil
}

// errTurnAbortedBeforeStream 表示这一轮在 runner.Run 返回之前就被用户点了停止。
// 它不是故障:调用方走 abortTurnBeforeStream 按 idle 收敛,而不是 failTurn。
var errTurnAbortedBeforeStream = errors.New("chat_svc: turn aborted before stream")

func (s *chatSvc) bindLocalPiAbort(
	sessionID int64,
	be *agent_backend_entity.AgentBackend,
	runner agentruntime.Runtime,
) {
	if be == nil || !be.IsPiAgent() || be.IsRemote() || runner == nil {
		return
	}
	aborter, ok := runner.(agentruntime.Aborter)
	if !ok {
		return
	}
	raw, ok := s.activeCancels.Load(sessionID)
	if !ok {
		return
	}
	control, _ := raw.(*activeTurnControl)
	control.setGracefulAbort(aborter)
}

func (s *chatSvc) persistUserAnchor(
	ctx context.Context,
	userMsg *chat_entity.Message,
	anchor string,
	hardFailure bool,
) error {
	if userMsg == nil || strings.TrimSpace(anchor) == "" {
		return nil
	}
	userMsg.ForkAnchor = anchor
	if !hardFailure {
		_ = chat_repo.Message().Update(ctx, userMsg)
		return nil
	}
	if err := chat_repo.Message().Update(ctx, userMsg); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.persistUserAnchor: message update failed, retrying",
			zap.Int64("sessionId", userMsg.SessionID),
			zap.Int64("messageId", userMsg.ID),
			zap.String("forkAnchor", userMsg.ForkAnchor),
			zap.Error(err))
		if retryErr := chat_repo.Message().Update(ctx, userMsg); retryErr != nil {
			logger.Ctx(ctx).Error("chat_svc.persistUserAnchor: message update failed after retry",
				zap.Int64("sessionId", userMsg.SessionID),
				zap.Int64("messageId", userMsg.ID),
				zap.String("forkAnchor", userMsg.ForkAnchor),
				zap.Error(retryErr))
			return fmt.Errorf("persist user anchor: %w", retryErr)
		}
	}
	return nil
}

func (s *chatSvc) runTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
	userMsg, assistantMsg *chat_entity.Message,
	stream string,
	forkAnchor string,
	compact bool,
	prepared *preparedTurnRun,
	extras turnExtras,
) {
	startedAt := time.Now()

	if prepared == nil {
		var err error
		prepared, err = s.prepareTurnRun(ctx, sess, a, be, prov, userMsg, assistantMsg, forkAnchor, compact, false)
		if err != nil {
			if errors.Is(err, errTurnAbortedBeforeStream) {
				s.abortTurnBeforeStream(ctx, sess, assistantMsg, stream)
				return
			}
			s.failTurn(ctx, sess, assistantMsg, stream, err)
			return
		}
	}
	defer prepared.releaseResources()
	runner := prepared.runner
	events := prepared.events
	result := prepared.result
	req := prepared.req
	// 登记本 turn 的活跃流名,供工具审批(BeginToolApproval)把审批卡路由到此流。
	// stream 在 SteerConsumed 分段时不变(同 turn 一个流名),Store 一次即可;收尾时清掉。
	s.activeTurnStreams.Store(sess.ID, stream)
	defer s.activeTurnStreams.Delete(sess.ID)
	if result != nil && (be.IsClaudeCode() || be.IsCodex() || be.IsPiAgent()) {
		s.persistProviderSessionID(ctx, sess, result.ProviderSessionID, "runner-start")
	}
	// runtime 若支持「自主续轮」(claudecode / remote claudecode 在 run_in_background
	// 任务完成后**自主**跑一轮),惰性起每会话 watcher 把它落成纯 assistant 轮。session
	// 已在 Run 内 spawn,此刻订阅 AutonomousTurns 才能拿到该会话的 channel;每会话去重,
	// 重复调用幂等。watcher 在子进程 evict / CloseSession(channel close)时自行退出。
	if src, ok := runner.(agentruntime.AutonomousTurnSource); ok {
		s.startAutonomousWatcher(sess.ID, be, src)
	}
	// runtime 若支持「后台 subagent 内部活动流」(本地 claudecode 在
	// run_in_background subagent 空闲态产出内部工具调用),惰性起每会话 watcher 把每轮活动
	// 嵌套渲染回发起卡并跨消息落库。每会话去重,channel close 时自行退出。
	// 注: remote claudecode (agentred) 目前未实现 SubagentActivitySource,
	// 仅本地 claudecode runtime 走这条路径。
	if src, ok := runner.(agentruntime.SubagentActivitySource); ok {
		s.startSubagentActivityWatcher(sess.ID, be, src)
	}
	// runtime spawn 新 CLI 子进程时把实际下发的 --permission-mode 同步回吐到
	// result.LaunchPermissionMode(claudecode 专用,其它 runtime 留空);这里把
	// 它落库到 session.PermissionModeAtLaunch。历史上由 runtime 直接 chat_repo
	// 写,导致 agentred daemon 进程 nil panic,搬到此处后 runtime 不再反向依
	// 赖 repository。值与库内一致时跳过,避免每轮多一次 UPDATE。
	if result != nil && result.LaunchPermissionMode != "" &&
		result.LaunchPermissionMode != sess.PermissionModeAtLaunch {
		sess.PermissionModeAtLaunch = result.LaunchPermissionMode
		if perr := chat_repo.Session().UpdatePermissionModeAtLaunch(
			ctx, sess.ID, result.LaunchPermissionMode); perr != nil {
			logger.Ctx(ctx).Warn("chat_svc: persist permission_mode_at_launch failed",
				zap.Int64("sessionId", sess.ID),
				zap.String("mode", result.LaunchPermissionMode),
				zap.Error(perr))
		}
	}

	var (
		acc           = turn.New()
		streamStopErr error
		segmentStart  = startedAt
		dispEmit      = &dispatcherEmitter{svc: s}
		turnCtx       = s.newTurnContext(assistantMsg, sess, stream, be.Type)
	)
	for ev := range events {
		if streamStopErr != nil {
			if eventShowsProgressAfterError(ev) {
				fields := make([]zap.Field, 0, 6)
				fields = append(fields,
					zap.Int64("sessionId", sess.ID),
					zap.Int64("assistantMsgId", assistantMsg.ID),
					zap.String("clearedBy", fmt.Sprintf("%T", ev)),
				)
				fields = append(fields, chatRuntimeErrorLogFields(streamStopErr)...)
				logger.Ctx(ctx).Info("chat_svc.runTurn: stream error cleared by progress event", fields...)
				streamStopErr = nil
			} else {
				continue
			}
		}
		// SteerConsumed + ErrorEvent 不走 dispatcher:
		//   - SteerConsumed:turn-segmentation 紧耦合 assistantMsg/segmentStart/acc/turnCtx
		//     的整体切换,handler 接口表达不了 4 个 local 的同步替换。
		//   - ErrorEvent:旧路径只设 streamStopErr,真正的 StreamError emit 在 finalize
		//     阶段(带 ChatMessage 完整快照);ErrorHandler 单独 emit 会与 finalize 重复
		//     且缺 Message 字段。
		switch e := ev.(type) {
		case agentruntime.SteerConsumed:
			nextAssistant, payload, perr := s.persistConsumedSteers(
				ctx, sess, be, assistantMsg, acc, segmentStart,
				assistantMsg.Model, e.Steers,
			)
			if perr != nil {
				logger.Ctx(ctx).Warn("chat_svc: streamStopErr set by persistConsumedSteers",
					zap.Int64("sessionId", sess.ID),
					zap.Int64("assistantMsgId", assistantMsg.ID),
					zap.Error(perr))
				streamStopErr = perr
				continue
			}
			if nextAssistant != nil && payload != nil {
				assistantMsg = nextAssistant
				acc = turn.New()
				segmentStart = time.Now()
				turnCtx = s.newTurnContext(assistantMsg, sess, stream, be.Type)
				s.emitter.Emit(ctx, stream, *payload)
			}
			continue
		case agentruntime.ErrorEvent:
			if e.Err != nil {
				fields := make([]zap.Field, 0, 6)
				fields = append(fields,
					zap.Int64("sessionId", sess.ID),
					zap.Int64("assistantMsgId", assistantMsg.ID),
					zap.String("stream", stream),
				)
				fields = append(fields, chatRuntimeErrorLogFields(e.Err)...)
				logger.Ctx(ctx).Warn("chat_svc.runTurn: ErrorEvent intercepted", fields...)
				streamStopErr = e.Err
			}
			continue
		}
		if err := s.dispatcher.Apply(ctx, ev, acc, dispEmit, nil, turnCtx); err != nil {
			logger.Ctx(ctx).Warn("chat dispatcher Apply failed",
				zap.String("eventType", fmt.Sprintf("%T", ev)),
				zap.Error(err))
		}
		if shouldCheckpointAssistantAfterEvent(ev) {
			s.checkpointAssistantNew(ctx, assistantMsg, acc)
		}
	}
	turnCtx.ClearWaits()

	if req.CollaborationMode == permissionModePlan && !compact && acc.Empty() {
		acc.AddText("Plan mode completed without executable changes.")
	}
	if compact && streamStopErr == nil && !hasCompactBoundaryBlock(acc.Snapshot()) {
		if err := s.dispatcher.Apply(ctx, agentruntime.CompactBoundary{Trigger: "manual"}, acc, dispEmit, nil, turnCtx); err != nil {
			logger.Ctx(ctx).Warn("chat compact fallback boundary failed", zap.Error(err))
		}
	}
	finalBlocks := acc.Finalize()
	// abort flag 提前到这里取(原在下方 LoadAndDelete) —— 若已 abort,需要在 SetBlocks
	// 之前把仍 running 的 subagent 状态改成 "canceled"。否则 CLI 被 interrupt
	// 后没有 SubagentDone 事件到达,running 会被原样落 DB 让前端 AgentSpawnCard
	// 永远 spin。
	_, aborted := s.aborted.LoadAndDelete(sess.ID)
	if aborted {
		handlers.MarkRunningSubagentsCancelled(finalBlocks)
	}
	// 把本会话登记的工具审批 block merge 进 assistant 消息(*ToolApprovalBlock
	// 实现 cago ContentBlock);仍 pending 的在 take 内被标 expired。
	for _, b := range s.takeToolApprovals(sess.ID) {
		finalBlocks = append(finalBlocks, b)
	}
	// 未答的 AskUserQuestion 在 turn 结束后会变死卡(runner 已 Close，再提交走
	// ErrNoActiveTurn / 无 waiter 必然失败)。与 MarkRunningSubagentsCancelled /
	// takeToolApprovals 同模式标 expired：落库让 reload 可见，下方 finalCtx 就绪后
	// 对被标记的 block emit 锁定 patch，让在屏活卡不用 reload 立即锁。
	expiredAsks := handlers.MarkUnansweredUserAsksExpired(finalBlocks)

	assistantMsg.DurationMs = int(time.Since(segmentStart).Milliseconds())
	stopErr := streamStopErr
	var anchorPersistErr error
	if result != nil {
		if result.Usage != nil {
			assistantMsg.PromptTokens = result.Usage.PromptTokens
			assistantMsg.CompletionTokens = result.Usage.CompletionTokens
			assistantMsg.CachedTokens = result.Usage.CachedTokens
			assistantMsg.CacheCreationTokens = result.Usage.CacheCreationTokens
			assistantMsg.ReasoningTokens = result.Usage.ReasoningTokens
		}
		// runner 上报的实际模型 id 覆盖创建时的占位值：
		//   - builtin: 与原值相同（都来自 prov.Model）→ 不变
		//   - claudecode CLI login: 创建时 model="" → 这里被填上 system.init.model 真值
		//   - codex CLI login: 同上，填上 Agentre 的 codex 默认模型
		// LoadSession 后续就能用这个字段查 cago catalog 解析 contextWindow。
		if result.Model != "" {
			assistantMsg.Model = result.Model
		}
		if stopErr == nil && result.StopErr != nil {
			stopErr = s.mapTurnError(ctx, sess, be, result.StopErr)
			fields := make([]zap.Field, 0, 6)
			fields = append(fields,
				zap.Int64("sessionId", sess.ID),
				zap.Int64("assistantMsgId", assistantMsg.ID),
				zap.String("stream", stream),
			)
			fields = append(fields, chatRuntimeErrorLogFields(stopErr)...)
			logger.Ctx(ctx).Warn("chat_svc.runTurn: stopErr promoted from RunResult.StopErr", fields...)
		}
		// Send 时 sess 之前没有 session id，runner 返回新 id 落库；
		// Regenerate-fork 时 sess 有旧 id 但 runner 返回 fork 出来的新 id，必须覆盖。
		// claudecode resume 同 session 时返回的 id 与原 id 相同，覆盖无副作用。
		if result.ProviderSessionID != "" {
			sess.SetProviderSession(result.ProviderSessionID)
		}
		// Runtime 抽到的本轮 user anchor 必须可靠落库；短暂写失败重试一次，
		// 持续失败则保留已生成回答但把 turn 标成 error，不能伪装成可继续分叉的成功轮。
		if err := s.persistUserAnchor(
			context.WithoutCancel(ctx),
			userMsg,
			result.UserAnchor,
			be.IsPiAgent(),
		); err != nil {
			anchorPersistErr = err
			stopErr = errors.Join(stopErr, err)
		}
		// codex app-server 上报的 modelContextWindow 落到 session 字段，下次
		// LoadSession 用 resolveContextWindowWithRuntime 优先读这个值——比
		// provider 静态配置和 catalog 兜底都准。仅在 runner 真的探到时更新，
		// 否则保留旧值，避免 claudecode / builtin 的 0 把先前 codex 写入的覆盖掉。
		if result.ContextWindow > 0 {
			sess.ContextWindow = result.ContextWindow
		}
		// 本轮请求的模型与实际运行模型不一致时补一条 UI-only 偏离提示。必须排在
		// assistantMsg.Model 被 result.Model 覆盖之后:比对的是 runner 上报的实际值。
		if notice := modelDeviationNotice(req.ModelOverride, result.Model); notice != nil {
			finalBlocks = append(finalBlocks, *notice)
		}
	}
	_ = assistantMsg.SetBlocks(finalBlocks)
	// aborted 已在 acc.Finalize() 之后取出(见上方 MarkRunningSubagentsCancelled 调用)；
	// 这里的判定决定 StreamAborted vs StreamError/Done,以及 abort 路径跳过自动接续。
	awaitingPlanAction := stopErr == nil && !aborted &&
		!compact &&
		req.CollaborationMode == permissionModePlan &&
		hasActionablePlanBlock(finalBlocks)

	if stopErr != nil && (!aborted || anchorPersistErr != nil) {
		assistantMsg.ErrorText = stopErr.Error()
	}
	// finalCtx：去掉 cancel 信号但保留 DB 句柄。abort 路径下 turnCtx 已 cancel，
	// 用它写 Update 会静默失败，partial 内容就丢了。
	finalCtx := context.WithoutCancel(ctx)
	_ = chat_repo.Message().Update(finalCtx, assistantMsg)

	// 对 finalize 时标 expired 的 AskUserQuestion emit 锁定 patch(形态同
	// UserAskResolvedHandler):前端按 requestId merge,把在屏活卡立即翻到失效态,
	// 无需等下一次 LoadSession 回放持久化的 expired block。
	for _, blk := range expiredAsks {
		dispEmit.Emit(finalCtx, stream, map[string]any{
			"kind":            "ask_user_question",
			"requestId":       blk.RequestID,
			"askUserQuestion": blk,
		})
	}

	// turn 结束（无错且未 abort）→ 看 runner 还有没有 mid-turn 排进来但 hook 没拉走的
	// 残留 Steer 消息。有的话合并成一条 user msg、emit StreamSteerConsumed、
	// 复用当前 goroutine + 锁递归跑下一轮 —— 替代旧 Stop hook block=continue
	// 把戏（旧路径在 Claude TUI 会渲染成红色 "Stop hook error" 误导文案，
	// 且 hook 自身执行期内到达的新消息会因 stop_hook_active=true 被静默丢掉）。
	// abort 路径：跳过自动接续，让用户自己决定要不要再发。
	var pending []agentruntime.ConsumedSteer
	if stopErr == nil && !aborted {
		if drainer, ok := runner.(agentruntime.SteerDrainer); ok {
			pending = nonEmptyConsumedSteers(drainer.DrainPending(finalCtx, sess.ID))
		}
	}

	sess.LastMessageAt = time.Now().UnixMilli()
	// 即将自动接续的中间态：不要把 session 状态打成 idle，等最终轮收尾再翻。
	if len(pending) == 0 {
		switch {
		case stopErr != nil && (!aborted || anchorPersistErr != nil):
			sess.AgentStatus = "error"
			sess.NeedsAttention = false
		case awaitingPlanAction:
			sess.AgentStatus = "waiting"
			sess.NeedsAttention = true
		default:
			sess.AgentStatus = "idle"
			// turn 真正结束（含 abort）：清掉 ask/审批待响应留下的 attention 标记，
			// 防止用户在等待期间点「停止」后 sidebar bubble 永远亮着。
			sess.NeedsAttention = false
		}
	}
	_ = s.persistSessionStatus(finalCtx, sess)
	if aborted || stopErr != nil {
		if s.clearBgRunning(sess.ID) {
			s.emitBgRunningStatus(finalCtx, sess, stream)
		}
	} else {
		s.reconcileBgRunningOnFinalize(finalCtx, sess, finalBlocks, stream)
	}
	// 诊断: 落库的最终(或自动接续中间态)agent_status。下面那段只在 error/waiting 时
	// emit+log,idle 收尾历史上完全没日志 —— 这正是 agentre.log 里看不到 running→idle
	// 翻转、排查「状态停在 running / 被过期快照盖回 idle」时无从对时间线的原因。这里补一条
	// 覆盖所有终态(含 pending>0 自动接续仍 running 的中间态)。
	logger.Ctx(finalCtx).Info("chat_svc: agent_status finalized",
		zap.Int64("sessionId", sess.ID),
		zap.Int64("assistantMsgId", assistantMsg.ID),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Bool("needsAttention", sess.NeedsAttention),
		zap.Bool("aborted", aborted),
		zap.Int("pending", len(pending)))
	// 最后一轮收尾统一先推 session_status，再推 done/error/aborted。前端底部输出由
	// LiveStream 生命周期驱动，tab/toolbar/sidebar 由 session-status-store 驱动；若
	// idle 只靠 done 后异步 reload 回填，两套视图必然存在不一致窗口。
	if len(pending) == 0 {
		fields := make([]zap.Field, 0, 11)
		fields = append(fields,
			zap.Int64("sessionId", sess.ID),
			zap.Int64("assistantMsgId", assistantMsg.ID),
			zap.String("stream", stream),
			zap.String("agentStatus", sess.AgentStatus),
			zap.Bool("needsAttention", sess.NeedsAttention),
			zap.Bool("aborted", aborted),
			zap.Bool("awaitingPlanAction", awaitingPlanAction),
			zap.String("source", "finalize"),
		)
		fields = append(fields, chatRuntimeErrorLogFields(stopErr)...)
		logger.Ctx(finalCtx).Info("chat_svc.runTurn: session status emit", fields...)
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind: StreamSessionStatus,
			SessionStatus: &ChatSessionStatusPatch{
				AgentStatus:    sess.AgentStatus,
				NeedsAttention: sess.NeedsAttention,
				BgRunning:      s.bgRunningActive(sess.ID),
			},
		})
	}

	if len(pending) > 0 {
		nextUser, nextAssistant, payload, perr := s.persistAutoContinueTurn(finalCtx, sess, be, assistantMsg, assistantMsg.Model, pending)
		if perr == nil {
			s.emitter.Emit(finalCtx, stream, *payload)
			if be.IsClaudeCode() || be.IsCodex() || be.IsPiAgent() {
				s.refreshPermissionModeForAutoContinue(finalCtx, sess)
			}
			// 同 goroutine + 同锁 + 同 stream 名递归跑下一轮：runTurn 内部
			// chatMessageForEvent / StreamDone 会以 nextAssistant 为目标 emit，
			// 前端 store 通过 StreamSteerConsumed.AssistantMessage 已经把活动
			// assistant 切到 nextAssistant。
			// 自动续轮沿用本轮 extras:群成员会话的 MCP 注入 + 群上下文 suffix
			// 需要在同一会话的整个生命周期内保持,而非只在首轮生效。
			s.runTurn(ctx, sess, a, be, prov, nextUser, nextAssistant, stream, "", false, nil, extras)
			return
		}
		// 写新轮失败 → pending 已经从 SteerInbox drain 走，无法回滚，只能丢。
		// 至少 (a) 落日志 + sessionID 方便排查；(b) emit 一个只带 QueuedIDs
		// 的 StreamSteerConsumed 让前端清掉 chip，否则用户看到 chip 永远不消失
		// 但消息没被任何 turn 处理。补 idle 让 list UI 状态回正。
		logger.Default().Error("chat_svc: persist auto-continue turn failed; pending messages lost",
			zap.Int64("sessionId", sess.ID),
			zap.Int("pendingCount", len(pending)),
			zap.Error(perr),
		)
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind:      StreamSteerConsumed,
			QueuedIDs: consumedSteerIDs(pending),
		})
		sess.AgentStatus = "idle"
		sess.NeedsAttention = false
		_ = s.persistSessionStatus(finalCtx, sess)
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind: StreamSessionStatus,
			SessionStatus: &ChatSessionStatusPatch{
				AgentStatus:    sess.AgentStatus,
				NeedsAttention: sess.NeedsAttention,
				BgRunning:      s.bgRunningActive(sess.ID),
			},
		})
	}

	final := chatMessageForEvent(sess, assistantMsg)
	switch {
	case anchorPersistErr != nil:
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind:    StreamError,
			Error:   stopErr.Error(),
			Message: final,
		})
	case aborted:
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamAborted, Message: final})
	case stopErr != nil:
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind:    StreamError,
			Error:   stopErr.Error(),
			Message: final,
		})
	default:
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamDone, Message: final})
	}
	// turn 正常收尾(含 abort)的唯一终态回灌点。错误路径走 failTurn 后 return,
	// 自动接续路径在递归 runTurn 的 finalize 回灌(本帧 len(pending)>0 已提前 return)。
	s.publishTurnResult(sess.ID, TurnResult{
		SessionID:          sess.ID,
		AssistantMessageID: assistantMsg.ID,
		Aborted:            aborted,
		Err:                stopErr,
	})
}

func (s *chatSvc) persistProviderSessionID(ctx context.Context, sess *chat_entity.Session, providerSessionID, reason string) {
	sid := strings.TrimSpace(providerSessionID)
	if sess == nil || sid == "" || sid == sess.ProviderSessionID {
		return
	}
	sess.SetProviderSession(sid)
	if err := chat_repo.Session().Update(context.WithoutCancel(ctx), sess); err != nil {
		logger.Ctx(ctx).Warn("chat_svc: persist provider_session_id failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("providerSessionID", sid),
			zap.String("reason", reason),
			zap.Error(err))
	}
}

func eventShowsProgressAfterError(ev agentruntime.Event) bool {
	switch ev.(type) {
	case agentruntime.TextDelta,
		agentruntime.ThinkingDelta,
		agentruntime.ToolCall,
		agentruntime.ToolResult,
		agentruntime.UserAskRequest,
		agentruntime.UserAskResolved,
		agentruntime.ToolPermissionRequest,
		agentruntime.ToolPermissionResolved,
		agentruntime.SubagentStarted,
		agentruntime.SubagentProgress,
		agentruntime.SubagentDone,
		agentruntime.SubagentModel,
		agentruntime.Retry,
		agentruntime.PlanUpdated,
		agentruntime.CompactBoundary,
		agentruntime.SteerConsumed:
		return true
	default:
		return false
	}
}

func shouldCheckpointAssistantAfterEvent(ev agentruntime.Event) bool {
	switch ev.(type) {
	case agentruntime.ToolResult,
		agentruntime.UserAskRequest,
		agentruntime.UserAskResolved,
		agentruntime.ToolPermissionRequest,
		agentruntime.ToolPermissionResolved,
		agentruntime.PlanUpdated:
		return true
	default:
		return false
	}
}

// persistAutoContinueTurn 把 turn 结束时 DrainPending 取到的排队消息合并成一条
// user msg + 一条空 assistant msg，落 DB 并构造 StreamSteerConsumed 事件。
// 调用方负责 emit 事件并递归调 runTurn 跑下一轮。
//
// previousAssistant 是刚收尾的 assistant（已 Update 过）；payload 里把它放在
// PreviousAssistantMessage —— 前端 applySteerConsumed 会把它定位到现有位置，
// 然后在它后面插入新的 user + assistant。
func (s *chatSvc) persistAutoContinueTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	previousAssistant *chat_entity.Message,
	model string,
	pending []agentruntime.ConsumedSteer,
) (*chat_entity.Message, *chat_entity.Message, *ChatStreamEvent, error) {
	merged := joinSteerTexts(pending)
	newUser := &chat_entity.Message{SessionID: sess.ID, Role: "user", DeviceID: be.DeviceID}
	if err := newUser.SetBlocks([]blocks.ContentBlock{&blocks.TextBlock{Text: merged}}); err != nil {
		return nil, nil, nil, fmt.Errorf("set merged user blocks: %w", err)
	}
	newAssistant := &chat_entity.Message{
		SessionID:  sess.ID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
		Model:      model,
	}

	if err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		nextSeq, err := chat_repo.Message().NextSeq(txCtx, sess.ID)
		if err != nil {
			return err
		}
		newUser.Seq = nextSeq
		if err := chat_repo.Message().Create(txCtx, newUser); err != nil {
			return err
		}
		newAssistant.Seq = nextSeq + 1
		if err := chat_repo.Message().Create(txCtx, newAssistant); err != nil {
			return err
		}
		sess.LastMessageAt = time.Now().UnixMilli()
		return chat_repo.Session().Update(txCtx, sess)
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("persist auto-continue: %w", err)
	}

	userEvent, err := toChatMessage(newUser)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("encode auto-continue user msg: %w", err)
	}
	userEvent.SessionID = sess.ID
	// R17: 合并多条残留 steers 成一条 user msg 时,取第一条非空来源(来源应一致;
	// 不一致时按先到者标,保持极简)。本机/未知保持空。
	for _, st := range pending {
		if st.SourcePeer != "" || st.SourceName != "" {
			userEvent.SourceDevice = st.SourcePeer
			userEvent.SourceDeviceName = st.SourceName
			break
		}
	}

	payload := &ChatStreamEvent{
		Kind:                     StreamSteerConsumed,
		QueuedIDs:                consumedSteerIDs(pending),
		PreviousAssistantMessage: chatMessageForEvent(sess, previousAssistant),
		UserMessages:             []ChatMessage{userEvent},
		AssistantMessage:         chatMessageForEvent(sess, newAssistant),
	}
	return newUser, newAssistant, payload, nil
}

// joinSteerTexts 把多条排队消息合并成一段（用 "\n\n" 分隔，模型常见的段落分隔
// 习惯；3 条以上也保持可读）。空切片返空串。
func joinSteerTexts(steers []agentruntime.ConsumedSteer) string {
	if len(steers) == 0 {
		return ""
	}
	if len(steers) == 1 {
		return steers[0].Text
	}
	var b strings.Builder
	for i, st := range steers {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(st.Text)
	}
	return b.String()
}

func (s *chatSvc) persistConsumedSteers(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	current *chat_entity.Message,
	acc *turn.Accumulator,
	segmentStart time.Time,
	model string,
	steers []agentruntime.ConsumedSteer,
) (*chat_entity.Message, *ChatStreamEvent, error) {
	steers = nonEmptyConsumedSteers(steers)
	if len(steers) == 0 {
		return nil, nil, nil
	}

	_ = current.SetBlocks(acc.Finalize())
	current.DurationMs = int(time.Since(segmentStart).Milliseconds())

	userMsgs := make([]*chat_entity.Message, 0, len(steers))
	nextAssistant := &chat_entity.Message{
		SessionID:  sess.ID,
		DeviceID:   be.DeviceID,
		Role:       "assistant",
		BlocksJSON: "[]",
		Model:      model,
	}

	if err := db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := db.WithContextDB(ctx, tx)
		if err := chat_repo.Message().Update(txCtx, current); err != nil {
			return err
		}
		nextSeq, err := chat_repo.Message().NextSeq(txCtx, sess.ID)
		if err != nil {
			return err
		}
		for i, steer := range steers {
			msg := &chat_entity.Message{
				SessionID: sess.ID,
				DeviceID:  be.DeviceID,
				Role:      "user",
				Seq:       nextSeq + i,
			}
			if err := msg.SetBlocks([]blocks.ContentBlock{&blocks.TextBlock{Text: steer.Text}}); err != nil {
				return err
			}
			if err := chat_repo.Message().Create(txCtx, msg); err != nil {
				return err
			}
			userMsgs = append(userMsgs, msg)
		}
		nextAssistant.Seq = nextSeq + len(userMsgs)
		if err := chat_repo.Message().Create(txCtx, nextAssistant); err != nil {
			return err
		}
		sess.LastMessageAt = time.Now().UnixMilli()
		return chat_repo.Session().Update(txCtx, sess)
	}); err != nil {
		return nil, nil, fmt.Errorf("persist consumed steer: %w", err)
	}

	userEvents := make([]ChatMessage, 0, len(userMsgs))
	for i, msg := range userMsgs {
		cm, err := toChatMessage(msg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode consumed steer message: %w", err)
		}
		cm.SessionID = sess.ID
		// R17: 把提交方来源带进 UserMessages —— 他端消息才有;本机/未知保持空,
		// 前端看到空 sourceDevice 就不渲染来源标识。
		if i < len(steers) {
			cm.SourceDevice = steers[i].SourcePeer
			cm.SourceDeviceName = steers[i].SourceName
		}
		userEvents = append(userEvents, cm)
	}

	return nextAssistant, &ChatStreamEvent{
		Kind:                     StreamSteerConsumed,
		QueuedIDs:                consumedSteerIDs(steers),
		PreviousAssistantMessage: chatMessageForEvent(sess, current),
		UserMessages:             userEvents,
		AssistantMessage:         chatMessageForEvent(sess, nextAssistant),
	}, nil
}

func nonEmptyConsumedSteers(steers []agentruntime.ConsumedSteer) []agentruntime.ConsumedSteer {
	if len(steers) == 0 {
		return nil
	}
	out := make([]agentruntime.ConsumedSteer, 0, len(steers))
	for _, steer := range steers {
		if steer.Text == "" {
			continue
		}
		out = append(out, steer)
	}
	return out
}

func consumedSteerIDs(steers []agentruntime.ConsumedSteer) []string {
	ids := make([]string, 0, len(steers))
	for _, steer := range steers {
		if steer.QueuedID != "" {
			ids = append(ids, steer.QueuedID)
		}
	}
	return ids
}

func chatMessageForEvent(sess *chat_entity.Session, msg *chat_entity.Message) *ChatMessage {
	final, err := toChatMessage(msg)
	if err != nil {
		return nil
	}
	if sess != nil {
		final.SessionID = sess.ID
	}
	return &final
}

func shouldSignChatGateway(be *agent_backend_entity.AgentBackend) bool {
	if be == nil || be.IsBuiltin() {
		return false
	}
	if be.IsClaudeCode() {
		return true
	}
	return be.LLMProviderKey != ""
}

// remoteProviderKnownMissing returns true only when the watcher cache has a
// recorded provider list for the remote device and that list does not contain
// the backend's provider key. A nil list means "no heartbeat data yet", so the
// runtime path is allowed to try and report the authoritative daemon error.
func remoteProviderKnownMissing(be *agent_backend_entity.AgentBackend) bool {
	if be == nil || !be.IsRemote() || strings.TrimSpace(be.LLMProviderKey) == "" {
		return false
	}
	deviceID, ok := be.DeviceIDInt()
	if !ok {
		return false
	}
	rds := remote_device_svc.Default()
	if rds == nil {
		return false
	}
	providers := rds.ListDeviceProviders(deviceID)
	if providers == nil {
		return false
	}
	for _, p := range providers {
		if p.Key == be.LLMProviderKey {
			return false
		}
	}
	return true
}

func remoteProviderNotConfiguredError(ctx context.Context, providerKey string) error {
	key := strings.TrimSpace(providerKey)
	if key == "" {
		key = "unknown"
	}
	return i18n.NewError(ctx, code.ChatRemoteProviderNotConfigured, key, key)
}

// signChatTokenFor 为需要 gateway 的 CLI 后端签一个 **会话级常驻** token。
// 返回 (gatewayURL, token)，任意一者为空时调用方按"不签"处理（CLI 走自身 login）。
//
// Claude Code local 会使用 token 访问 /hook/v1/inbox；绑定了 LLM provider 的
// Claude Code / Codex 会用它走 LLM 转发。Codex local 不应调用这里。
//
// 关键不变量:同一 session 跨轮返回 **同一个永久 token**。该 token 在首轮 spawn 时
// 烤进 claude 子进程 env(AGENTRE_GATEWAY_TOKEN),后续轮复用子进程时 env 不重建 ——
// 旧实现每轮重签 15min TTL 的新 token、却只有首轮那个被烤进去,导致长会话(>15min)
// 子进程手里的 token 过期、PostToolUse hook 撞 401、SteerInbox 整轮 drain 不到、
// steer 被压到轮末 DrainPending。改成 ttl=0 永久 + 跨轮复用,寿命跟随子进程,
// session 删除时由 Delete→revokeChatToken 撤销。
func (s *chatSvc) signChatTokenFor(ctx context.Context, be *agent_backend_entity.AgentBackend, sessionID int64) (string, string) {
	if be == nil || s.gateway == nil {
		return "", ""
	}
	if s.gateway.Status().State != "running" {
		return "", ""
	}
	if sessionID > 0 {
		if v, ok := s.chatTokens.Load(sessionID); ok {
			return s.gateway.URL(), v.(string)
		}
	}
	tok, err := s.gateway.IssueToken(ctx, be, 0)
	if err != nil {
		return "", ""
	}
	if sessionID > 0 {
		// 并发首轮兜底:别的 goroutine 抢先签好就用它的,撤掉自己这条避免泄漏。
		if actual, loaded := s.chatTokens.LoadOrStore(sessionID, tok); loaded {
			s.gateway.RevokeToken(tok)
			return s.gateway.URL(), actual.(string)
		}
	}
	return s.gateway.URL(), tok
}

// revokeChatToken 撤销并清掉某 session 的常驻 token。Delete 关闭常驻子进程后调用,
// 让 token 寿命跟随子进程 —— 之后该 id 若复活会重签一个新的。
func (s *chatSvc) revokeChatToken(sessionID int64) {
	if sessionID <= 0 {
		return
	}
	if v, ok := s.chatTokens.LoadAndDelete(sessionID); ok && s.gateway != nil {
		s.gateway.RevokeToken(v.(string))
	}
}

// mapProviderSessionError 命中 Claude Code 或通用 runtime 的 SessionNotFound
// sentinel 时做两件事：
//  1. 清空 sess.ProviderSessionID 并立即持久化（context.WithoutCancel 防 abort
//     路径下 turnCtx 已 cancel 导致静默失败）—— 下一轮 Send 才能 spawn 全新
//     CLI 会话，而不是一直拿 --resume 撞同一个失效 id。
//  2. 把 err 替换成 ChatProviderSessionGone 的 i18n 错误，前端拿到的就是
//     "CLI 会话已过期 …" 中文人话，不是英文 stderr。
//
// 非 ErrSessionNotFound 原样返回，让上层走 default 失败路径。
func (s *chatSvc) mapProviderSessionError(ctx context.Context, sess *chat_entity.Session, src error) error {
	if !providerSessionNotFound(src) {
		return src
	}
	if sess != nil && sess.HasProviderSession() {
		sess.SetProviderSession("")
		_ = chat_repo.Session().Update(context.WithoutCancel(ctx), sess)
	}
	return i18n.NewError(ctx, code.ChatProviderSessionGone)
}

func providerSessionNotFound(err error) bool {
	return errors.Is(err, claudecode.ErrSessionNotFound) || errors.Is(err, agentruntime.ErrSessionNotFound)
}

// mapTurnError 把一轮的终止原因翻成**交到用户面前的那句话**。
//
// 远端的两种非失败终止在这里分道:R15 规定它们都沿用既有的 error 态、不新增第五个
// AgentStatus 取值,「由消息文案区分其与真实错误」—— 而消息文案就是这个返回值(经
// assistantMsg.ErrorText 持久化)。三句话必须互不相同:被打断(daemon 重启 / 会话在
// 那台机器上已中断)、连不上了(重连彻底失败)、真的跑失败了(原样透出后端错误)。
func (s *chatSvc) mapTurnError(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend, src error) error {
	if src == nil {
		return nil
	}
	if errors.Is(src, remote.ErrRunInterrupted) {
		return i18n.NewError(ctx, code.ChatRemoteRunInterrupted)
	}
	if errors.Is(src, remote.ErrDaemonDisconnected) {
		return i18n.NewError(ctx, code.ChatRemoteDaemonUnreachable)
	}
	if providerSessionNotFound(src) {
		return s.mapProviderSessionError(ctx, sess, src)
	}
	var rpcErr *daemonrpc.Error
	if errors.As(src, &rpcErr) && rpcErr.Code == daemonrpc.ErrProviderMissing.Code {
		key := ""
		if be != nil {
			key = be.LLMProviderKey
		}
		return remoteProviderNotConfiguredError(ctx, key)
	}
	return src
}

func chatRuntimeErrorLogFields(err error) []zap.Field {
	if err == nil {
		return nil
	}
	fields := []zap.Field{
		zap.String("errorClass", fmt.Sprintf("%T", err)),
		zap.Int("errorBytes", len(err.Error())),
	}
	var appErr *httputils.Error
	if errors.As(err, &appErr) {
		return append(fields, zap.Int("errorCode", appErr.Code))
	}
	var rpcErr *daemonrpc.Error
	if errors.As(err, &rpcErr) {
		return append(fields, zap.Int("errorCode", rpcErr.Code))
	}
	return fields
}

func (s *chatSvc) failTurn(ctx context.Context, sess *chat_entity.Session, msg *chat_entity.Message, stream string, err error) {
	// 一次落地点收所有 turn 级别错误(selectRunner / resolveSessionCwd / runner.Run /
	// stream loop streamStopErr 等),给运维保留安全分类与定位 ID；完整错误仅继续走
	// 既有前端 StreamError 与持久化 ErrorText 边界。
	fields := make([]zap.Field, 0, 7)
	fields = append(fields,
		zap.Int64("sessionId", sess.ID),
		zap.Int64("messageId", msg.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
	)
	fields = append(fields, chatRuntimeErrorLogFields(err)...)
	logger.Ctx(ctx).Warn("chat_svc.failTurn: turn failed", fields...)
	// 终态一律用 WithoutCancel 落库:失败路径最常见的触发方式就是用户点「停止」把
	// turnCtx cancel 掉,若沿用同一个 ctx,这两条 Update 会被 DB 层直接拒掉,结果
	// agent_status 永远停在 running、error_text 也写不进去(前端既不报错也停不掉)。
	finalCtx := context.WithoutCancel(ctx)
	msg.ErrorText = err.Error()
	if uerr := chat_repo.Message().Update(finalCtx, msg); uerr != nil {
		logger.Ctx(finalCtx).Error("chat_svc.failTurn: persist error text failed",
			zap.Int64("messageId", msg.ID), zap.Error(uerr))
	}
	sess.AgentStatus = "error"
	sess.NeedsAttention = false
	if uerr := chat_repo.Session().Update(finalCtx, sess); uerr != nil {
		logger.Ctx(finalCtx).Error("chat_svc.failTurn: persist session status failed",
			zap.Int64("sessionId", sess.ID), zap.Error(uerr))
	}
	// session_status 必须先于 StreamError emit:前端 chat-streams-host 收到 error
	// 立刻 finishStream 删 LiveStream entry → StreamSubscriber 紧接着 unmount,后到
	// 的 session_status 永远收不到。后台 session 出错时只靠 bumpDone 不会翻 tab 红点。
	logger.Ctx(finalCtx).Info("chat_svc: session_status emit",
		zap.Int64("sessionId", sess.ID),
		zap.Int64("assistantMsgId", msg.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Bool("needsAttention", sess.NeedsAttention),
		zap.String("source", "failTurn"))
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
			BgRunning:      s.bgRunningActive(sess.ID),
		},
	})
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
		Kind:    StreamError,
		Error:   err.Error(),
		Message: chatMessageForEvent(sess, msg),
	})
	// 错误路径的唯一终态回灌点。failTurn 直线到此(无内部 early return),尾端单点
	// publish 即覆盖全部退出路径;与 finalize 互斥(调用方 failTurn 后立即 return)。
	s.publishTurnResult(sess.ID, TurnResult{
		SessionID:          sess.ID,
		AssistantMessageID: msg.ID,
		Err:                err,
	})
}

// turnAbortedByUser 判定「runner.Run 返回的这个错误其实是用户点了停止」。
// 只认两种信号:runtime 显式回 ErrAborted,或本会话已被 Stop 标记且错误确实是
// ctx 取消。普通故障(拨号失败等)即使碰巧带着 abort 标记也仍按错误处理,免得把
// 真故障伪装成"用户停的"。
func (s *chatSvc) turnAbortedByUser(sessionID int64, err error) bool {
	if errors.Is(err, agentruntime.ErrAborted) {
		s.aborted.LoadAndDelete(sessionID)
		return true
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := s.aborted.Load(sessionID); !ok {
		return false
	}
	s.aborted.LoadAndDelete(sessionID)
	return true
}

// abortTurnBeforeStream 收敛「Run 还没返回就被 Stop」的那一轮。此时 runtime 侧
// 还没注册 activeTurn(OpenClaw 要先跟网关握手),既没有流也没有产出,但会话已经是
// running —— 必须在这里落回 idle,否则侧栏一直转圈、且只有重启 app 才洗得掉。
// 与流式中途 abort 对齐:发 StreamAborted 而不是 StreamError,不写 ErrorText。
func (s *chatSvc) abortTurnBeforeStream(ctx context.Context, sess *chat_entity.Session, msg *chat_entity.Message, stream string) {
	finalCtx := context.WithoutCancel(ctx)
	logger.Ctx(finalCtx).Info("chat_svc: turn aborted before stream started",
		zap.Int64("sessionId", sess.ID),
		zap.Int64("assistantMsgId", msg.ID),
		zap.String("stream", stream))
	if uerr := chat_repo.Message().Update(finalCtx, msg); uerr != nil {
		logger.Ctx(finalCtx).Error("chat_svc.abortTurnBeforeStream: persist message failed",
			zap.Int64("messageId", msg.ID), zap.Error(uerr))
	}
	sess.AgentStatus = "idle"
	sess.NeedsAttention = false
	if uerr := chat_repo.Session().Update(finalCtx, sess); uerr != nil {
		logger.Ctx(finalCtx).Error("chat_svc.abortTurnBeforeStream: persist session status failed",
			zap.Int64("sessionId", sess.ID), zap.Error(uerr))
	}
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
			BgRunning:      s.bgRunningActive(sess.ID),
		},
	})
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
		Kind:    StreamAborted,
		Message: chatMessageForEvent(sess, msg),
	})
	s.publishTurnResult(sess.ID, TurnResult{
		SessionID:          sess.ID,
		AssistantMessageID: msg.ID,
	})
}

func (s *chatSvc) lockFor(sessionID int64) *trylockMutex {
	v, _ := s.locks.LoadOrStore(sessionID, &trylockMutex{})
	return v.(*trylockMutex)
}

type trylockMutex struct{ mu sync.Mutex }

func (t *trylockMutex) TryLock() bool { return t.mu.TryLock() }
func (t *trylockMutex) Unlock()       { t.mu.Unlock() }

// mentionXMLRe 匹配 @ 提及序列化进消息正文的 XML 标签,捕获其可读 label。
var mentionXMLRe = regexp.MustCompile(`<(agent|project)\b[^>]*>([\s\S]*?)</(?:agent|project)>`)

// sessionTitleFromFirstMessage 从首条用户消息派生会话标题。
// @ 提及会把 `<agent id="1">名字</agent>` 这类 XML 写进消息正文，正文里是对的，
// 但标题会显示在 tab / 侧栏 / 标题栏 —— 直接用会露出一坨裸 XML。这里把标签还原成可读的 `@名字`。
func sessionTitleFromFirstMessage(text string) string {
	out := mentionXMLRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := mentionXMLRe.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		return "@" + html.UnescapeString(sub[2])
	})
	return strings.TrimSpace(out)
}

func textOfMessage(m *chat_entity.Message) string {
	bs, _ := m.GetBlocks()
	for _, b := range bs {
		if tb, ok := b.(blocks.TextBlock); ok {
			return tb.Text
		}
		if tb, ok := b.(*blocks.TextBlock); ok && tb != nil {
			return tb.Text
		}
	}
	return ""
}
func (s *chatSvc) Rename(ctx context.Context, req *RenameRequest) (*RenameResponse, error) {
	title := strings.TrimSpace(req.Title)
	if utf8.RuneCountInString(title) > renameTitleMaxRunes {
		return nil, i18n.NewError(ctx, code.ChatTitleTooLong)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	sess.Title = title
	if err := chat_repo.Session().Update(ctx, sess); err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	return &RenameResponse{}, nil
}

func (s *chatSvc) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	if err := chat_repo.Session().SoftDelete(ctx, req.SessionID); err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	// DB 已删，释放该 session 的常驻 CLI 子进程（best-effort，cache miss 时 no-op）。
	claudecodert.Default().CloseSession(ctx, req.SessionID)
	codexrt.Default().CloseSession(ctx, req.SessionID)
	// 子进程已关，撤销并清掉它的常驻 gateway token（token 寿命跟随子进程）。
	s.revokeChatToken(req.SessionID)
	return &DeleteResponse{}, nil
}

// MarkSessionRead 推进会话 last_read_at 到至少 req.Timestamp (unix ms)。
// Timestamp <= 0 时改用 time.Now()。repo 层 MarkRead 自带「仅当新 ts 严格大于旧值时
// 才写入」的单调语义，所以乱序到达的 stream-done 不会把已读时间冲回旧值。
func (s *chatSvc) MarkSessionRead(ctx context.Context, req *MarkSessionReadRequest) (*MarkSessionReadResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	ts := req.Timestamp
	if ts <= 0 {
		ts = time.Now().UnixMilli()
	}
	if err := chat_repo.Session().MarkRead(ctx, req.SessionID, ts); err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	return &MarkSessionReadResponse{}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func uniqueNonZeroBackendIDs(agents []*agent_entity.Agent) []int64 {
	seen := make(map[int64]struct{}, len(agents))
	out := make([]int64, 0, len(agents))
	for _, a := range agents {
		if a.AgentBackendID == 0 {
			continue
		}
		if _, ok := seen[a.AgentBackendID]; ok {
			continue
		}
		seen[a.AgentBackendID] = struct{}{}
		out = append(out, a.AgentBackendID)
	}
	return out
}

// latestAssistantModel 反向扫描，取最近一条带模型 id 的 assistant message。
// 用于 LoadSession 解析 contextWindow——runner 上报的实际模型比 provider 静态配置准，
// 尤其在 claudecode / codex 走 CLI 自身 login 没绑 LLMProvider 的场景。
// 全部消息都没 Model 时返回空串。
func latestAssistantModel(msgs []*chat_entity.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != "assistant" {
			continue
		}
		if m.Model != "" {
			return m.Model
		}
	}
	return ""
}

// resolveContextWindowWithRuntime 统一解析会话当前应展示的上下文窗口（tokens）。
// 0 由前端约定为「不展示用量条」。
//
// 优先级（高 → 低）：
//  1. session.ContextWindow > 0：runner 上轮上报的 modelContextWindow（codex
//     app-server 推送），最权威——用户在 CLI 内 /model 切换后立刻生效；
//  2. provider.ContextWindow > 0：用户在 LLMProvider 显式配置——某些 vendor
//     给非标准窗口、或绑精简号时表达 UI 上限的明确意图；
//  3. latestAssistantModel → llmcatalog.Lookup：从历史 assistant message
//     的 Model 字段查表（claudecode CLI login / 显式 --model 都能命中）；
//  4. provider.Model → llmcatalog.Lookup：新会话还没 assistant message 时的兜底。
//
// 与之前 resolveContextWindow* 两套函数的合并：所有 backend 都走这一条路径，
// 仅靠优先级 1 / 2 之间的相对顺序自动表达"runtime 实测优先于静态配置"。
func resolveContextWindowWithRuntime(sess *chat_entity.Session, p *llm_provider_entity.LLMProvider, msgs []*chat_entity.Message) int {
	if sess != nil && sess.ContextWindow > 0 {
		return sess.ContextWindow
	}
	if p != nil && p.ContextWindow > 0 {
		return p.ContextWindow
	}
	if model := latestAssistantModel(msgs); model != "" {
		if info, ok := llmcatalog.Lookup(model); ok {
			return info.ContextWindow
		}
	}
	if p != nil {
		if info, ok := llmcatalog.Lookup(p.Model); ok {
			return info.ContextWindow
		}
	}
	return 0
}

func uniqueProviderKeys(backends map[int64]*agent_backend_entity.AgentBackend) []string {
	seen := make(map[string]struct{}, len(backends))
	out := make([]string, 0, len(backends))
	for _, b := range backends {
		if b == nil || b.LLMProviderKey == "" {
			continue
		}
		if _, ok := seen[b.LLMProviderKey]; ok {
			continue
		}
		seen[b.LLMProviderKey] = struct{}{}
		out = append(out, b.LLMProviderKey)
	}
	return out
}

// ── RemoteRuntime cache (Pool-backed) ────────────────────────────────────────

// remoteRuntimeEntry tracks a shared *remote.Runtime built on top of a Pool
// Lease, and the set of session IDs currently using it. Pool 负责底层 conn
// 复用 + idle 回收 + daemon drop evict;chat_svc 这层只是把 lease.Client()
// 升成 *remote.Runtime(handlers conn-scoped,一台 device 装一组就够)。
type remoteRuntimeEntry struct {
	runtime  *remote.Runtime
	lease    remote_device_svc.Lease
	sessions map[int64]*remoteRuntimeGeneration
}

// remoteRuntimeGeneration is the exact lease owner for one turn. A stale
// release compares this pointer before deleting the session reference, so it
// cannot release the device lease after a newer same-SessionID retry begins.
type remoteRuntimeGeneration struct{}

// pool 返回当前生效的 ConnPool。测试通过 setConnPoolForTest 注入 mock。
func (s *chatSvc) pool() remote_device_svc.ConnPool {
	if s.testHookPool != nil {
		return s.testHookPool
	}
	return remote_device_svc.Default().Pool()
}

// borrowRemoteRuntime 返回该 device 共享的 *remote.Runtime。第一次 borrow
// 会从 Pool 借一个 lease 并 wrap 成 runtime;后续同 device borrow 直接命中
// remoteCache。 lease.Closed() 关闭(daemon drop / idle / Pool.Close)→
// watchLeaseClosed 把 entry 从 map 摘掉,下次 borrow 走冷路径重建。
//
// 同 sessionID 多次 borrow 对 sessions set 幂等。
func (s *chatSvc) borrowRemoteRuntime(ctx context.Context, be *agent_backend_entity.AgentBackend, sessionID int64) (*remote.Runtime, error) {
	return s.borrowRemoteRuntimeOwned(ctx, be, sessionID, nil)
}

func (s *chatSvc) borrowRemoteRuntimeForTurn(
	ctx context.Context,
	be *agent_backend_entity.AgentBackend,
	sessionID int64,
) (*remote.Runtime, func(), error) {
	deviceID, ok := be.DeviceIDInt()
	if !ok {
		return nil, func() {}, i18n.NewError(ctx, code.AgentBackendInvalidDevice)
	}
	generation := &remoteRuntimeGeneration{}
	rt, err := s.borrowRemoteRuntimeOwned(ctx, be, sessionID, generation)
	if err != nil {
		return nil, func() {}, err
	}
	return rt, func() { s.releaseRemoteRuntimeGeneration(deviceID, sessionID, generation) }, nil
}

func (s *chatSvc) borrowRemoteRuntimeOwned(
	ctx context.Context,
	be *agent_backend_entity.AgentBackend,
	sessionID int64,
	generation *remoteRuntimeGeneration,
) (*remote.Runtime, error) {
	deviceID, ok := be.DeviceIDInt()
	if !ok {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidDevice)
	}
	rt, fp, err := s.remoteRuntimeForDevice(ctx, deviceID, []int64{sessionID}, generation)
	if err != nil {
		logger.Ctx(ctx).Error("borrowRemoteRuntime: pool.Borrow",
			zap.Int64("deviceID", deviceID), zap.Error(err))
		return nil, i18n.NewError(ctx, code.RemoteRunnerDialFailed)
	}
	// 会话在库里记下「跑在哪台 daemon 的哪个实例上」——游标端口的读写守卫全靠它,
	// 不写就永远判失效,断连补齐退化成断连即终止(见 remote_reconnect.go);而 App
	// 重启后「该连谁」也全靠这一行(见 CatchUpRemoteSessions)。runtime 是 device 级
	// 共享的,同一台设备上的第二条会话走 cache 命中那条路,它自己的执行位置同样要落库。
	s.recordExecDaemon(ctx, sessionID, deviceID, fp)

	// 同步拉一次远端 backend 的 capability 矩阵缓存到本地,之后 rt.Capabilities()
	// 直接返实际能力。已缓存过的 backendType 直接 noop(cache 命中不会再发 RPC)。
	// 失败不阻断 borrow —— Capabilities() 回退到 defaultCapsBeforePrefetch 占位,
	// UI gating 不挂死。
	if err := rt.Prefetch(ctx, agent_backend_entity.BackendType(be.Type)); err != nil {
		logger.Ctx(ctx).Warn("borrowRemoteRuntime: capability prefetch failed",
			zap.Int64("deviceID", deviceID),
			zap.String("backendType", be.Type),
			zap.Error(err))
	}
	return rt, nil
}

// remoteRuntimeForDevice 取(或建)某台配对 daemon 上共享的 *remote.Runtime,并把
// sessionIDs 记进它的引用集;顺带交回该 daemon 此刻的实例标识。
//
// 它与 borrowRemoteRuntime 分开,是因为补齐(CatchUpRemoteSessions)手上只有
// (设备, 会话) 而**没有 agent backend**:重启后要连回的那台 daemon 是从
// chat_sessions.exec_device_id 读出来的,不经过 turn 的后端选择。
// owner 非 nil 时这次借用带着一轮自己的 generation token(见 remoteRuntimeGeneration):
// 迟到的旧 release 比对指针后不会顶掉同会话新一轮的引用。控制路径(owner==nil)只在
// 引用缺失时补一个占位,不覆盖当前轮的 owner。
func (s *chatSvc) remoteRuntimeForDevice(
	ctx context.Context,
	deviceID int64,
	sessionIDs []int64,
	owner *remoteRuntimeGeneration,
) (*remote.Runtime, string, error) {
	// Fast path: cache hit
	s.remoteMu.Lock()
	if s.remoteCache == nil {
		s.remoteCache = map[int64]*remoteRuntimeEntry{}
	}
	if entry, ok := s.remoteCache[deviceID]; ok {
		addSessionRefs(entry, sessionIDs, owner)
		s.remoteMu.Unlock()
		return entry.runtime, s.daemonFingerprint(ctx, deviceID), nil
	}
	s.remoteMu.Unlock()

	// Cold path: 借 lease + wrap runtime
	lease, err := s.pool().Borrow(ctx, deviceID)
	if err != nil {
		return nil, "", err
	}
	fp := s.daemonFingerprint(ctx, deviceID)

	// entry 先建出来:重连端口要往里换 lease,所以它必须先于 runtime 存在。
	entry := &remoteRuntimeEntry{lease: lease, sessions: map[int64]*remoteRuntimeGeneration{}}
	addSessionRefs(entry, sessionIDs, owner)
	rt := remote.New(lease.Client(),
		remote.WithDaemonFingerprint(fp),
		remote.WithConnStateObserver(remote.ConnStateFunc(s.onRemoteConnState)),
		remote.WithDurabilityObserver(remote.DurabilityFunc(func(supported bool) {
			s.onRemoteDaemonDurability(deviceID, supported)
		})),
		remote.WithReconnect(remote.ReconnectFunc(func(rctx context.Context) (agentruntime.DaemonClientPort, string, error) {
			return s.reconnectRemote(rctx, deviceID, entry)
		})),
	)
	entry.runtime = rt

	// Re-lock and insert. TOCTOU 输家:用赢家的 entry,释放自己的 lease。
	s.remoteMu.Lock()
	if existing, ok := s.remoteCache[deviceID]; ok {
		addSessionRefs(existing, sessionIDs, owner)
		s.remoteMu.Unlock()
		lease.Release()
		return existing.runtime, fp, nil
	}
	s.remoteCache[deviceID] = entry
	s.remoteMu.Unlock()

	go s.watchLeaseClosed(deviceID, entry, lease)
	return rt, fp, nil
}

// addSessionRefs 调用方必须持 remoteMu。owner 非 nil = 这一轮的 generation token,
// 直接安装(它接管这条会话的引用);owner 为 nil 的控制路径只在引用缺失时补占位,
// 不覆盖当前轮的 owner —— 覆盖了的话那一轮的 release 就比不上指针、永远释放不掉。
func addSessionRefs(entry *remoteRuntimeEntry, sessionIDs []int64, owner *remoteRuntimeGeneration) {
	for _, sid := range sessionIDs {
		if owner != nil {
			entry.sessions[sid] = owner
			continue
		}
		if entry.sessions[sid] == nil {
			entry.sessions[sid] = &remoteRuntimeGeneration{}
		}
	}
}

// watchLeaseClosed 监听某条 lease 的 Closed()(Pool 那侧通知它失效),然后把 chat_svc
// 这边的 cache entry 摘掉,下次 borrow 走冷路径重建 runtime。
//
// 只在**这条** lease 仍是该 entry 当前那条时才摘:runtime 已经自己重连换过 lease 的话
// entry 还活着,摘掉它会让下一轮 borrow 为同一台设备造出第二个 *remote.Runtime,两个
// runtime 在同一条池化连接上抢注同名 handler,在飞会话的事件会被路由到不认识它的那个
// 然后静默丢弃。lease 参数因此是显式的,不能读 entry.lease —— 那是会变的。
func (s *chatSvc) watchLeaseClosed(deviceID int64, entry *remoteRuntimeEntry, lease remote_device_svc.Lease) {
	<-lease.Closed()
	s.remoteMu.Lock()
	cur, ok := s.remoteCache[deviceID]
	if ok && cur == entry && entry.lease == lease {
		delete(s.remoteCache, deviceID)
	}
	s.remoteMu.Unlock()
}

// releaseRemoteRuntime decrements the session refcount for deviceID. 当
// 最后一个 session release 时,把 lease 还给 Pool(Pool 自己负责 idle 回收 +
// 后续 borrow 复用)。
func (s *chatSvc) releaseRemoteRuntime(deviceID, sessionID int64) {
	s.remoteMu.Lock()
	entry, ok := s.remoteCache[deviceID]
	if !ok {
		s.remoteMu.Unlock()
		return
	}
	generation := entry.sessions[sessionID]
	s.remoteMu.Unlock()
	if generation != nil {
		s.releaseRemoteRuntimeGeneration(deviceID, sessionID, generation)
	}
}

func (s *chatSvc) releaseRemoteRuntimeGeneration(
	deviceID, sessionID int64,
	generation *remoteRuntimeGeneration,
) {
	s.remoteMu.Lock()
	entry, ok := s.remoteCache[deviceID]
	if !ok || entry.sessions[sessionID] != generation {
		s.remoteMu.Unlock()
		return
	}
	delete(entry.sessions, sessionID)
	if len(entry.sessions) > 0 {
		s.remoteMu.Unlock()
		return
	}
	delete(s.remoteCache, deviceID)
	s.remoteMu.Unlock()
	entry.lease.Release()
}

// remoteRuntimeCount returns the number of sessions currently sharing the
// runtime for deviceID. Returns 0 if no entry exists. Test-only helper.
func (s *chatSvc) remoteRuntimeCount(deviceID int64) int {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if entry, ok := s.remoteCache[deviceID]; ok {
		return len(entry.sessions)
	}
	return 0
}

// selectRunner 选取本地 / 远端 runner 并把它包装成统一接口。
//
//   - be.IsLocal() → agentruntime.RuntimeFor 注册表里的全局 runner;
//   - be.IsRemote() → borrowRemoteRuntime 拿/起 device-shared 的 *remote.Runtime。
//
// 同一 sessionID 多次调用对远端 cache 是 idempotent (set 语义),Steer / Abort /
// SetPermissionMode 这些 mid-turn 操作可以放心调,不会把 refcount 拉高。
// 调用方无需(也禁止)调 releaseRemoteRuntime —— 释放由 runTurn 的 defer 统一负责。
// 在 Stop / Enqueue / SetPermissionMode 这些 mid-turn 操作里 defer release 会
// 导致提前 release lease,把后续 turn 弄炸。
func (s *chatSvc) selectRunner(ctx context.Context, be *agent_backend_entity.AgentBackend, sessionID int64) (agentruntime.Runtime, error) {
	if be == nil {
		return nil, i18n.NewError(ctx, code.AgentBackendNotFound)
	}
	if be.IsLocal() {
		r := agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type))
		if r == nil {
			return nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
		}
		return r, nil
	}
	return s.borrowRemoteRuntime(ctx, be, sessionID)
}

func (s *chatSvc) EnsureSession(ctx context.Context, req *EnsureSessionRequest) (*EnsureSessionResponse, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	switch req.Purpose {
	case SessionPurposeSubagentCall:
		return s.createSubagentSession(ctx, req.AgentID, req.ProjectID, req.Title)
	case SessionPurposeUserChat:
		return s.createUserChatSession(ctx, req.AgentID, req.ProjectID, req.Title)
	default:
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
}

// createUserChatSession 建一个普通用户会话(每次新建)。与 createSubagentSession 同形, 唯一区别:
// Purpose 留空 —— 这是用户在侧栏可见、可继续对话的正常会话, 不是隐藏的隔离子会话。
// 供 ! 命令在「新会话占位态」(还没 sessionId)先坐实会话, 之后命令有 cwd 可解析、卡片有 transcript 可渲染。
func (s *chatSvc) createUserChatSession(ctx context.Context, agentID, projectID int64, title string) (*EnsureSessionResponse, error) {
	if agentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	// 普通用户会话是交互式的(有人审阅), 套用「先 plan 后 bypass」派生 → planFirst=true。
	permissionMode := s.launchPermissionModeForAgent(ctx, agentID, true)
	sess := &chat_entity.Session{
		AgentID:                agentID,
		ProjectID:              projectID,
		PermissionMode:         permissionMode,
		PermissionModeAtLaunch: permissionMode,
		Title:                  strings.TrimSpace(title),
		AgentStatus:            "idle",
		Status:                 consts.ACTIVE,
		// Purpose 留空 = 普通用户会话。
	}
	if err := chat_repo.Session().Create(ctx, sess); err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentId", agentID))
	}
	return &EnsureSessionResponse{SessionID: sess.ID, Created: true}, nil
}

// createSubagentSession 为子 agent 调用建一个全新的一次性隔离会话(每次新建)。
// 不做幂等复用 —— 每次 agent_call 都要干净的隔离上下文。
func (s *chatSvc) createSubagentSession(ctx context.Context, agentID, projectID int64, title string) (*EnsureSessionResponse, error) {
	if agentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	// 子 agent 调用是自律执行(没人审阅计划), 直接尊重配置的 bypass → planFirst=false。
	permissionMode := s.launchPermissionModeForAgent(ctx, agentID, false)
	sess := &chat_entity.Session{
		AgentID:                agentID,
		ProjectID:              projectID,
		Purpose:                chat_entity.SessionPurposeSubagent,
		PermissionMode:         permissionMode,
		PermissionModeAtLaunch: permissionMode,
		Title:                  strings.TrimSpace(title),
		AgentStatus:            "idle",
		Status:                 consts.ACTIVE,
	}
	if err := chat_repo.Session().Create(ctx, sess); err != nil {
		return nil, operationFailedWithCause(ctx, err, zap.Int64("agentId", agentID))
	}
	return &EnsureSessionResponse{SessionID: sess.ID, Created: true}, nil
}

// launchPermissionModeForAgent 解析某 agent 后端在新建会话时的默认权限模式。
// 只做轻量只读解析(agent → backend → createPermissionMode), 不做 provider/gateway 可聊性校验
// —— 那些属于 send 起手时的职责。解析不出(agent/后端缺失或后端无权限模式概念)时返回空串,
// 由 runtime 首轮回填 at_launch 兜底。
// planFirst: 交互式会话传 true(套用「先 plan 后 bypass」派生), 自律会话
// (subagent 调用)传 false(直接尊重配置的 bypass)。
func (s *chatSvc) launchPermissionModeForAgent(ctx context.Context, agentID int64, planFirst bool) string {
	a, err := agent_repo.Agent().Find(ctx, agentID)
	if err != nil || a == nil {
		return ""
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil || be == nil {
		return ""
	}
	mode, err := createPermissionMode(ctx, be, "", planFirst)
	if err != nil {
		return ""
	}
	return mode
}
