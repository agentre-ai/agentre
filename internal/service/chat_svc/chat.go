// Package chat_svc 提供聊天会话 / 消息的业务逻辑层。
package chat_svc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
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
	"go.uber.org/zap"
	"gorm.io/gorm"

	daemonrpc "agentre/internal/daemon/rpc"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/model/entity/project_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/canonical"
	"agentre/internal/pkg/agentruntime/capability"

	// 显式 blank import 触发本地 runtime 子包 init() 把 *Runtime 注册到 RuntimeFor。
	// remote 是显式构造,不参与全局注册;以下三种为本地后端,必须自注册才能被
	// selectRunner 解析到。claudecodert 别名避免与 pkg/claudecode CLI 库名字撞车。
	_ "agentre/internal/pkg/agentruntime/runtimes/builtin"
	claudecodert "agentre/internal/pkg/agentruntime/runtimes/claudecode"
	codexrt "agentre/internal/pkg/agentruntime/runtimes/codex"
	"agentre/internal/pkg/agentruntime/runtimes/remote"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/httpgateway"
	"agentre/internal/pkg/llmcatalog"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/project_repo"
	chatblocks "agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/chat_svc/handlers"
	"agentre/internal/service/chat_svc/turn"
	"agentre/internal/service/chat_svc/view"
	"agentre/internal/service/remote_device_svc"
	"agentre/pkg/claudecode"
)

const (
	maxSendImages      = 4
	maxSendImageBytes  = 5 * 1024 * 1024
	dataURLBase64Token = ";base64,"
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
	Send(ctx context.Context, req *SendRequest) (*SendResponse, error)
	Compact(ctx context.Context, req *CompactRequest) (*CompactResponse, error)
	GetGoal(ctx context.Context, req *GoalRequest) (*GoalResponse, error)
	SetGoal(ctx context.Context, req *SetGoalRequest) (*GoalResponse, error)
	StartGoal(ctx context.Context, req *StartGoalRequest) (*StartGoalResponse, error)
	ClearGoal(ctx context.Context, req *ClearGoalRequest) (*ClearGoalResponse, error)
	Enqueue(ctx context.Context, req *EnqueueRequest) (*EnqueueResponse, error)
	CancelQueued(ctx context.Context, req *CancelQueuedRequest) (*CancelQueuedResponse, error)
	Stop(ctx context.Context, req *StopRequest) (*StopResponse, error)
	SetPermissionMode(ctx context.Context, req *SetPermissionModeRequest) (*SetPermissionModeResponse, error)
	Regenerate(ctx context.Context, req *RegenerateRequest) (*SendResponse, error)
	Edit(ctx context.Context, req *EditRequest) (*SendResponse, error)
	Rename(ctx context.Context, req *RenameRequest) (*RenameResponse, error)
	Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
	MarkSessionRead(ctx context.Context, req *MarkSessionReadRequest) (*MarkSessionReadResponse, error)
	AnswerUserQuestion(ctx context.Context, req *AnswerUserQuestionRequest) (*AnswerUserQuestionResponse, error)
	AnswerToolPermission(ctx context.Context, req *AnswerToolPermissionRequest) (*AnswerToolPermissionResponse, error)
	ResolvePlanAction(ctx context.Context, req *ResolvePlanActionRequest) (*ResolvePlanActionResponse, error)
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

// chatTokenTTL 单轮 chat 用 token 的有效期；
// 比 test 路径的 60s 长，留出大段代码生成的余量。
const chatTokenTTL = 15 * time.Minute

const renameTitleMaxRunes = 200

type chatSvc struct {
	emitter Emitter
	// dispatcher 是 svc-bound turn.Dispatcher,注册了带 chat_svc 适配器的 18 个 handler。
	// 在 NewChat 时构造一次(svc-bound,handlers 的 Writer/Persister 持 *chatSvc 引用)。
	// AGENTRE_NEW_DISPATCHER=1 时 runTurn drain loop 通过它处理 Event;默认关。
	dispatcher *turn.Dispatcher
	locks      *sync.Map
	// activeCancels：sessionID(int64) → context.CancelFunc。startTurn 在 gogo.Go
	// 之前 store；runTurn 收尾 / Stop 触发时 LoadAndDelete。Stop 用它 cancel turnCtx，
	// 给嵌套 DB / cago / select 兜底解锁。
	activeCancels *sync.Map
	// aborted：sessionID(int64) → struct{}。Stop 触发时 store；runTurn 收尾时
	// LoadAndDelete 判定是否走 StreamAborted 路径 + 跳过 DrainPending 自动接续。
	aborted *sync.Map
	gateway httpgateway.TokenIssuer

	// remoteCache 是 device → (runtime, lease) 的 session 引用计数缓存。
	// runtime 复用底层 lease.Client(),lease 由 remote_device_svc.Pool 管理 conn
	// 复用 + idle 回收 + daemon drop evict。lease.Closed() 关闭时 watchLeaseClosed
	// 把 entry 从 map 摘掉,下次 borrow 走冷路径重建。
	remoteMu    sync.Mutex
	remoteCache map[int64]*remoteRuntimeEntry
	// testHookPool 如果非 nil,代替 remote_device_svc.Default().Pool() 用于测试注入。
	testHookPool remote_device_svc.ConnPool
}

// ── ListAgents ───────────────────────────────────────────────────────────────

func (s *chatSvc) ListAgents(ctx context.Context, _ *ListAgentsRequest) (*ListAgentsResponse, error) {
	agents, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	resp := &ListAgentsResponse{Agents: make([]ChatAgentItem, 0, len(agents))}
	if len(agents) == 0 {
		return resp, nil
	}

	backendIDs := uniqueNonZeroBackendIDs(agents)
	backends, err := agent_backend_repo.AgentBackend().BatchFind(ctx, backendIDs)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	providerKeys := uniqueProviderKeys(backends)
	providers, err := llm_provider_repo.LLMProvider().BatchFindByKey(ctx, providerKeys)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	totals, err := chat_repo.Session().CountByAgents(ctx, agentIDs)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	sessionIDs, err := chat_repo.Session().ListIDsByAgents(ctx, agentIDs)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
			Pinned:        a.IsSystem(),
			ActiveCount:   counts[a.ID],
			TotalSessions: totals[a.ID],
			SessionIDs:    ids,
		}
		if be := backends[a.AgentBackendID]; be != nil {
			item.BackendType = be.Type
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
			case agent_backend_entity.TypeClaudeCode, agent_backend_entity.TypeCodex:
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
			default:
				item.ChattableHint = "未知 Agent 后端类型"
			}
		} else if a.IsSystem() {
			item.ChattableHint = "CEO 助手还没绑定后端，请在组织架构页配置"
		} else {
			item.ChattableHint = "该 Agent 还没绑定后端"
		}

		sessions, err := chat_repo.Session().ListByAgent(ctx, a.ID, 5)
		if err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
		}
		item.RecentCount = len(sessions)
		item.Sessions = make([]ChatSessionLite, 0, len(sessions))
		for _, sess := range sessions {
			item.Sessions = append(item.Sessions, ChatSessionLite{
				ID:             sess.ID,
				Title:          sess.Title,
				Status:         sess.AgentStatus,
				NeedsAttention: sess.IsWaitingForUser(),
				LastMessageAt:  sess.LastMessageAt,
				LastReadAt:     sess.LastReadAt,
			})
		}

		// sidebar 折叠态 attention bubble：拉所有 running/waiting/error 会话。
		// 不受 5 行常规列表的约束；limit=20 防异常数据撑爆 UI，前端去重与本组 sessions 的重叠。
		attention, err := chat_repo.Session().ListAttentionByAgent(ctx, a.ID, 20)
		if err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
		}
		item.AttentionSessions = make([]ChatSessionLite, 0, len(attention))
		for _, sess := range attention {
			item.AttentionSessions = append(item.AttentionSessions, ChatSessionLite{
				ID:             sess.ID,
				Title:          sess.Title,
				Status:         sess.AgentStatus,
				NeedsAttention: sess.IsWaitingForUser(),
				LastMessageAt:  sess.LastMessageAt,
				LastReadAt:     sess.LastReadAt,
			})
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

	sessions, err := chat_repo.Session().ListByAgentPaged(ctx, req.AgentID, req.Offset, limit)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	total, err := chat_repo.Session().CountByAgent(ctx, req.AgentID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}

	resp := &ListAgentSessionsResponse{
		Sessions: make([]ChatSessionLite, 0, len(sessions)),
		Total:    total,
		HasMore:  int64(req.Offset+len(sessions)) < total,
	}
	for _, sess := range sessions {
		resp.Sessions = append(resp.Sessions, ChatSessionLite{
			ID:             sess.ID,
			Title:          sess.Title,
			Status:         sess.AgentStatus,
			NeedsAttention: sess.IsWaitingForUser(),
			LastMessageAt:  sess.LastMessageAt,
			LastReadAt:     sess.LastReadAt,
		})
	}
	return resp, nil
}

func (s *chatSvc) LoadSession(ctx context.Context, req *LoadSessionRequest) (*LoadSessionResponse, error) {
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	msgs, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	resp := &LoadSessionResponse{
		Session: ChatSessionDetail{
			ID:                     sess.ID,
			AgentID:                sess.AgentID,
			Title:                  sess.Title,
			AgentStatus:            sess.AgentStatus,
			NeedsAttention:         sess.IsWaitingForUser(),
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
	if _, activeTurn := s.activeCancels.Load(sess.ID); activeTurn &&
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if a == nil {
		return nil, i18n.NewError(ctx, code.AgentNotFound)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if be == nil {
		return nil, i18n.NewError(ctx, code.ChatLaunchCommandNotAvailable)
	}
	if be.IsBuiltin() {
		return nil, i18n.NewError(ctx, code.ChatLaunchCommandNotAvailable)
	}

	var prov *llm_provider_entity.LLMProvider
	if be.LLMProviderKey != "" {
		prov, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
		if err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
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
		case blocks.ToolUseBlock:
			out.Blocks = append(out.Blocks, toolUseToChatBlock(tb.ID, tb.Name, tb.Input))
		case *blocks.ToolUseBlock:
			out.Blocks = append(out.Blocks, toolUseToChatBlock(tb.ID, tb.Name, tb.Input))
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
			// SubagentStateBlock: 累计态(tokens/duration/status),前端 AgentSpawnCard
			// 通过外层 Task tool 的 canonical.agentSpawn 读 —— live 路径靠
			// dispatcher_emitter 注入,replay 不重算,STEPS / SUMMARY 仍完整,
			// 只是 badge 缺失(明确接受)。
			// PermissionModeChangeBlock: 审计 block,无 UI 元素。两者一并 skip,
			// 不下行到前端(否则会被打成 type=unknown 让用户看到 debug 卡)。
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
// 与外层 toolUseToChatBlock 的差别仅在于带 ParentToolCallID(json: parentToolUseId),
// 前端 chat.tsx collectChildren 据此把它从主流程移走、挂到外层 AgentSpawnCard.childBlocks。
// canonical 故意不算 —— 内层是被父 agent.spawn 包住的 step,不需要独立 canonical 路由。
func nestedToolUseToChatBlock(b *chatblocks.NestedToolUseBlock) ChatBlock {
	cb := ChatBlock{
		Type:             "tool_use",
		ToolUseID:        b.ID,
		ToolName:         b.Name,
		ParentToolCallID: b.ParentToolCallID,
	}
	if len(b.Input) > 0 {
		cb.ToolInput = b.Input
	}
	return cb
}

// nestedToolResultToChatBlock 镜像 nestedToolUseToChatBlock —— 内层 tool_result
// 带 ParentToolCallID,Content 已经是拍平字符串(NestedToolResultBlock 不嵌套 ContentBlock)。
func nestedToolResultToChatBlock(b *chatblocks.NestedToolResultBlock) ChatBlock {
	return ChatBlock{
		Type:             "tool_result",
		ToolUseID:        b.ToolCallID,
		Text:             b.Content,
		IsError:          b.IsError,
		ParentToolCallID: b.ParentToolCallID,
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	if strings.TrimSpace(sess.ProviderSessionID) == "" {
		return nil, i18n.NewError(ctx, code.ChatCompactNoSession)
	}

	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}
	if !be.IsCodex() {
		return nil, i18n.NewError(ctx, code.ChatCompactUnsupported)
	}
	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.Compact: selectRunner failed",
			zap.Int64("sessionId", sess.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.ChatCompactUnsupported)
	}
	if !runner.Capabilities().Has(capability.CapCompact) {
		return nil, i18n.NewError(ctx, code.ChatCompactUnsupported)
	}
	return s.startCompactTurn(ctx, sess, a, be, prov)
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
	permissionMode, err := createPermissionMode(ctx, be, req.PermissionMode)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
			logger.Ctx(ctx).Warn("chat_svc.StartGoal: persist provider_session_id failed",
				zap.Int64("sessionId", sess.ID),
				zap.String("providerSessionID", providerSessionID),
				zap.Error(err))
			return nil, i18n.NewError(ctx, code.OperationFailed)
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
		return nil, nil, nil, nil, i18n.NewError(ctx, code.OperationFailed)
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
			return nil, i18n.NewError(ctx, code.OperationFailed)
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
		// 项目上下文（可选）：仅在新建会话时生效；已存在的会话不再换项目。
		projectID, perr := s.resolveProjectContext(ctx, req.ProjectID, targetAgentID)
		if perr != nil {
			return nil, perr
		}
		permissionMode, perr := createPermissionMode(ctx, be, req.PermissionMode)
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
			Title:                  sessionTitleFromFirstMessage(text),
			AgentStatus:            "running",
			Status:                 consts.ACTIVE,
		}
		if err := chat_repo.Session().Create(ctx, sess); err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
		}
	} else {
		planWaiting, err := s.canContinuePlanWaiting(ctx, sess, be, opts.allowPlanWaiting)
		if err != nil {
			return nil, err
		}
		if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, planWaiting); err != nil {
			return nil, err
		}
		sess.AgentStatus = "running"
		_ = chat_repo.Session().Update(ctx, sess)
	}

	return s.startTurn(ctx, sess, a, be, prov, userBlocksForSend(text, imageBlocks), nil /*preTxHook*/, "" /*forkAnchor*/)
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
		return 0, i18n.NewError(ctx, code.OperationFailed)
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
			return false, i18n.NewError(ctx, code.OperationFailed)
		}
		if parent == nil {
			break
		}
		ids = append(ids, parent.ID)
		cur = parent
	}
	mapByProj, err := project_repo.ProjectAgent().ListByProjects(ctx, ids)
	if err != nil {
		return false, i18n.NewError(ctx, code.OperationFailed)
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
		return nil, nil, nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if a == nil {
		return nil, nil, nil, i18n.NewError(ctx, code.NotFound)
	}
	be, err := agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
	if err != nil {
		return nil, nil, nil, i18n.NewError(ctx, code.OperationFailed)
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
			return nil, nil, nil, i18n.NewError(ctx, code.OperationFailed)
		}
		if prov == nil || !prov.IsActive() {
			return nil, nil, nil, i18n.NewError(ctx, code.ChatAgentNotChattable)
		}
	case agent_backend_entity.TypeClaudeCode, agent_backend_entity.TypeCodex:
		if be.LLMProviderKey != "" {
			prov, err = llm_provider_repo.LLMProvider().FindByKey(ctx, be.LLMProviderKey)
			if err != nil {
				return nil, nil, nil, i18n.NewError(ctx, code.OperationFailed)
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
	default:
		return nil, nil, nil, i18n.NewError(ctx, code.AgentBackendInvalidType)
	}

	return a, be, prov, nil
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
//  3. runner.Abort + cancel turnCtx —— 双信号：runner 解阻塞 I/O（claudecode 写
//     control_request、codex 发 turn/interrupt、builtin 靠 ctx），cancel 给嵌套
//     DB / select 兜底。Abort 失败不致命，cancel 仍生效。
func (s *chatSvc) Stop(ctx context.Context, req *StopRequest) (*StopResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	raw, ok := s.activeCancels.LoadAndDelete(req.SessionID)
	if !ok {
		return nil, i18n.NewError(ctx, code.ChatStopNoActive)
	}
	cancel, _ := raw.(context.CancelFunc)
	s.aborted.Store(req.SessionID, struct{}{})
	logger.Ctx(ctx).Info("chat_svc.Stop: aborting turn",
		zap.Int64("sessionId", req.SessionID))

	// runner.Abort：尽力派发，失败不阻塞 cancel
	if sess, err := chat_repo.Session().Find(ctx, req.SessionID); err == nil && sess != nil {
		if _, be, _, berr := s.resolveAgentBackend(ctx, sess.AgentID); berr == nil && be != nil {
			if runner, rerr := s.selectRunner(ctx, be, sess.ID); rerr == nil {
				if ab, ok := runner.(agentruntime.Aborter); ok {
					if aerr := ab.Abort(ctx, req.SessionID); aerr != nil &&
						!errors.Is(aerr, agentruntime.ErrNoActiveTurn) {
						// Abort 失败不致命(后面还要 cancel ctx 兜底),但要留底
						// 方便追"按了停止仍持续 10s 才退"的 issue。ErrNoActiveTurn
						// 是 runner 自然完成 / 已被清理的正常竞态,不打。
						logger.Ctx(ctx).Warn("chat_svc.Stop: runner.Abort failed",
							zap.Int64("sessionId", req.SessionID),
							zap.String("backendType", be.Type),
							zap.Error(aerr))
					}
				}
			}
		}
	}
	if cancel != nil {
		cancel()
	}
	return &StopResponse{Stopped: true}, nil
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

func createPermissionMode(ctx context.Context, be *agent_backend_entity.AgentBackend, raw string) (string, error) {
	if be == nil {
		return "", nil
	}
	backendType := agent_backend_entity.BackendType(be.Type)
	if strings.TrimSpace(raw) != "" {
		return validateRequestedPermissionMode(ctx, backendType, raw)
	}
	// claudecode + admin 配 bypass 时, 新会话以 plan 起手: CLI 仍按 bypass 启动(由
	// runtime resolveLaunchMode 保证), session.PermissionMode=plan 让前端 pill 显
	// 示 Plan, spawn 后由 runtime SetPermissionMode 把 CLI 切到 plan。"先 plan 后
	// bypass"工作流靠这条派生 + 现有 PlanApproveCard 主按钮(launch==bypass → Bypass)
	// 完成闭环。
	if be.IsClaudeCode() && strings.TrimSpace(be.DefaultPermissionMode) == "bypassPermissions" {
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
		return false, i18n.NewError(ctx, code.OperationFailed)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	_, be, _, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
func (s *chatSvc) Regenerate(ctx context.Context, req *RegenerateRequest) (*SendResponse, error) {
	if req == nil || req.SessionID <= 0 || req.MessageID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}

	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	target, err := chat_repo.Message().Find(ctx, req.MessageID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if target == nil || target.SessionID != sess.ID {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}
	if target.Role != "assistant" {
		return nil, i18n.NewError(ctx, code.ChatRegenerateNotAssistant)
	}

	// 找紧邻 target 之前的最后一条 user 消息（按 seq）。
	all, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
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

	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}

	forkAnchor, ferr := s.backendForkAnchor(ctx, sess, be, userAnchor)
	if ferr != nil {
		return nil, ferr
	}

	if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, false); err != nil {
		return nil, err
	}
	sess.AgentStatus = "running"
	_ = chat_repo.Session().Update(ctx, sess)

	// preTx 在同一事务里先截掉 user 锚点（含）开始的全部历史，
	// 然后 startTurn 的标准路径会以新的 NextSeq 写回 user + assistant。
	anchorSeq := userAnchor.Seq
	preTx := func(txCtx context.Context) error {
		_, derr := chat_repo.Message().DeleteFromSeq(txCtx, sess.ID, anchorSeq)
		return derr
	}
	return s.startTurn(ctx, sess, a, be, prov, userBlocks, preTx, forkAnchor)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}

	target, err := chat_repo.Message().Find(ctx, req.MessageID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if target == nil || target.SessionID != sess.ID {
		return nil, i18n.NewError(ctx, code.ChatMessageNotFound)
	}
	if target.Role != "user" {
		return nil, i18n.NewError(ctx, code.ChatEditNotUser)
	}
	targetBlocks, err := target.GetBlocks()
	if err != nil {
		return nil, i18n.NewError(ctx, code.ChatBlocksMalformed)
	}

	a, be, prov, err := s.resolveAgentBackend(ctx, sess.AgentID)
	if err != nil {
		return nil, err
	}

	forkAnchor, ferr := s.backendForkAnchor(ctx, sess, be, target)
	if ferr != nil {
		return nil, ferr
	}

	if err := s.applyRequestedPermissionMode(ctx, sess, be, req.PermissionMode, false); err != nil {
		return nil, err
	}
	sess.AgentStatus = "running"
	_ = chat_repo.Session().Update(ctx, sess)

	anchorSeq := target.Seq
	preTx := func(txCtx context.Context) error {
		_, derr := chat_repo.Message().DeleteFromSeq(txCtx, sess.ID, anchorSeq)
		return derr
	}
	return s.startTurn(ctx, sess, a, be, prov, replaceTextPreserveImages(text, targetBlocks), preTx, forkAnchor)
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
// 副作用：claudecode 首轮 user msg 没有 anchor 时会清空 sess.ProviderSessionID，
// 让上层 startTurn → runner 当作新建会话发起。
func (s *chatSvc) backendForkAnchor(
	ctx context.Context,
	sess *chat_entity.Session,
	be *agent_backend_entity.AgentBackend,
	userMsg *chat_entity.Message,
) (string, error) {
	if !sess.HasProviderSession() {
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
	default:
		runner := agentruntime.RuntimeFor(agent_backend_entity.BackendType(be.Type))
		if _, ok := runner.(agentruntime.Rewinder); !ok {
			return "", i18n.NewError(ctx, code.ChatRegenerateUnsupported)
		}
		return "", nil
	}
}

func (s *chatSvc) codexRollbackAnchor(ctx context.Context, sess *chat_entity.Session, userMsg *chat_entity.Message) (string, error) {
	msgs, err := chat_repo.Message().List(ctx, sess.ID)
	if err != nil {
		return "", i18n.NewError(ctx, code.OperationFailed)
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
// an optional pre-step running inside the same tx — e.g. deleting old
// messages on Regenerate), then kick off the async runTurn.
//
// Caller is responsible for resolving sess/a/be/prov consistently with the
// session's actual agent (Send for new sessions, Regenerate for in-place
// rewind). userBlocks is the user message body that will be re-played to the runtime.
//
// preTx, if non-nil, runs at the very top of the transaction — before NextSeq —
// so it can free up seq numbers by truncating older rows. Returning a non-nil
// error from preTx aborts the whole turn (and unlocks).
func (s *chatSvc) startTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
	userBlocks []blocks.ContentBlock,
	preTx func(txCtx context.Context) error,
	forkAnchor string,
) (*SendResponse, error) {
	lock := s.lockFor(sess.ID)
	if !lock.TryLock() {
		return nil, i18n.NewError(ctx, code.ChatSendInFlight)
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
		// 持久化失败比较罕见(SQLite 锁 / disk full),前端只看到 OperationFailed,
		// 真错(包括 preTx hook 的 sentinel)要进日志才能排查。
		logger.Ctx(ctx).Error("chat_svc.startTurn: persist user+assistant messages failed",
			zap.Int64("sessionId", sess.ID),
			zap.Int64("agentId", a.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}

	stream := StreamName(sess.ID, assistantMsg.ID)

	s.markStreamRunningForTest(assistantMsg.ID)
	runCtx := db.WithContextDB(context.Background(), db.Ctx(ctx))
	// turnCtx：Stop 用它 cancel；activeCancels 必须在 gogo.Go **之前** store —— 用户
	// 可能在 goroutine 调度起来之前就点了「停止」。
	turnCtx, cancel := context.WithCancel(runCtx)
	s.activeCancels.Store(sess.ID, cancel)
	gogo.Go(func() error {
		// defer 顺序：LIFO。先注册 unlock，最后释放；中间的 cancel cleanup
		// 跑在 lock 还持有期间，新 turn 起不来 → 直接 Delete 安全。
		defer lock.Unlock()
		defer s.markStreamDoneForTest(assistantMsg.ID)
		defer func() {
			s.activeCancels.Delete(sess.ID)
			cancel() // 兜底：runTurn 自己没 cancel（正常完成路径）也补一刀，无副作用
		}()
		s.runTurn(turnCtx, sess, a, be, prov, userMsg, assistantMsg, stream, forkAnchor, false)
		return nil
	}, gogo.WithIgnorePanic())

	return &SendResponse{
		SessionID:          sess.ID,
		UserMessageID:      userMsg.ID,
		AssistantMessageID: assistantMsg.ID,
		Stream:             stream,
	}, nil
}

func (s *chatSvc) startCompactTurn(
	ctx context.Context,
	sess *chat_entity.Session,
	a *agent_entity.Agent,
	be *agent_backend_entity.AgentBackend,
	prov *llm_provider_entity.LLMProvider,
) (*CompactResponse, error) {
	lock := s.lockFor(sess.ID)
	if !lock.TryLock() {
		return nil, i18n.NewError(ctx, code.ChatSendInFlight)
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
		logger.Ctx(ctx).Error("chat_svc.startCompactTurn: persist assistant message failed",
			zap.Int64("sessionId", sess.ID),
			zap.Int64("agentId", a.ID),
			zap.String("backendType", be.Type),
			zap.Error(err))
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}

	stream := StreamName(sess.ID, assistantMsg.ID)
	s.markStreamRunningForTest(assistantMsg.ID)
	runCtx := db.WithContextDB(context.Background(), db.Ctx(ctx))
	turnCtx, cancel := context.WithCancel(runCtx)
	s.activeCancels.Store(sess.ID, cancel)
	gogo.Go(func() error {
		defer lock.Unlock()
		defer s.markStreamDoneForTest(assistantMsg.ID)
		defer func() {
			s.activeCancels.Delete(sess.ID)
			cancel()
		}()
		s.runTurn(turnCtx, sess, a, be, prov, nil, assistantMsg, stream, "", true)
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
		},
	})
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
) {
	startedAt := time.Now()

	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		// 真错(RemoteRunnerDialFailed / AgentBackendInvalidDevice / AgentBackendInvalidType)
		// 必须透传给 failTurn,否则前端永远只看到误导的 "unsupported backend type: X",
		// 远端 daemon 离线 / DeviceID 失效 / 类型未注册三种情况无法区分。
		logger.Ctx(ctx).Error("runTurn: selectRunner failed",
			zap.Int64("sessionID", sess.ID),
			zap.String("backendType", be.Type),
			zap.String("deviceID", be.DeviceID),
			zap.Error(err))
		s.failTurn(ctx, sess, assistantMsg, stream, err)
		return
	}
	if be.IsRemote() {
		if deviceID, ok := be.DeviceIDInt(); ok {
			defer s.releaseRemoteRuntime(deviceID, sess.ID)
		}
	}
	if userMsg != nil && messageHasImage(userMsg) && !runner.Capabilities().Has(capability.CapImageInput) {
		s.failTurn(ctx, sess, assistantMsg, stream, agentruntime.ErrUnsupported)
		return
	}

	cwd, cwdErr := resolveSessionCwd(ctx, sess, be)
	if cwdErr != nil {
		s.failTurn(ctx, sess, assistantMsg, stream, cwdErr)
		return
	}
	req := agentruntime.RunRequest{
		Backend:           be,
		Provider:          prov,
		AgentID:           a.ID,
		SessionID:         sess.ID,
		Cwd:               cwd,
		SystemPrompt:      strings.Join(a.GetPrompt(), "\n"),
		ProviderSessionID: sess.ProviderSessionID,
		Compact:           compact,
		ForkAnchor:        forkAnchor,
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
			s.failTurn(ctx, sess, assistantMsg, stream, err)
			return
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
		//  - GatewayURL/Token: desktop 本机 127.0.0.1 在 daemon 主机上不可达,
		//    必须由 daemon 端 handlers/runtime.go 用 h.deps.Gateway.URL() 填。
		//  - Provider: 含明文 APIKey,不该每个 turn 越线漂移到远端机器;daemon
		//    自家 ProviderLookup 会按 LLMProviderKey 从本机 keychain 解出。
		// 注:LLMProviderKey 已对齐 (UUID, 双方各自配同一 provider 即可)。
		req.LLMProviderKey = be.LLMProviderKey
		req.Provider = nil
	} else if shouldSignChatGateway(be) {
		// Claude Code local 仍需要 gateway token 给 PostToolUse hook 访问
		// /hook/v1/inbox；Codex local 没有 hook，不能注入 gateway，否则会覆盖
		// codex login 并把模型请求误打到本地 /v1/responses。
		req.GatewayURL, req.GatewayToken = s.signChatTokenFor(ctx, be)
	}
	switch agent_backend_entity.BackendType(be.Type) {
	case agent_backend_entity.TypeClaudeCode:
		req.PermissionMode = normalizeStoredPermissionMode(agent_backend_entity.TypeClaudeCode, sess.PermissionMode)
	case agent_backend_entity.TypeCodex:
		req.CollaborationMode = normalizeStoredPermissionMode(agent_backend_entity.TypeCodex, sess.PermissionMode)
	}

	events, result, err := runner.Run(ctx, req)
	if err != nil {
		s.failTurn(ctx, sess, assistantMsg, stream, s.mapTurnError(ctx, sess, be, err))
		return
	}
	if result != nil && (be.IsClaudeCode() || be.IsCodex()) {
		s.persistProviderSessionID(ctx, sess, result.ProviderSessionID, "runner-start")
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
				logger.Ctx(ctx).Info("chat_svc: streamStopErr cleared by progress event",
					zap.Int64("sessionId", sess.ID),
					zap.Int64("assistantMsgId", assistantMsg.ID),
					zap.String("clearedBy", fmt.Sprintf("%T", ev)),
					zap.Error(streamStopErr))
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
				logger.Ctx(ctx).Warn("chat_svc: ErrorEvent intercepted, streamStopErr set",
					zap.Int64("sessionId", sess.ID),
					zap.Int64("assistantMsgId", assistantMsg.ID),
					zap.String("stream", stream),
					zap.Error(e.Err))
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
	_ = assistantMsg.SetBlocks(finalBlocks)

	assistantMsg.DurationMs = int(time.Since(segmentStart).Milliseconds())
	stopErr := streamStopErr
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
			logger.Ctx(ctx).Warn("chat_svc: stopErr promoted from RunResult.StopErr",
				zap.Int64("sessionId", sess.ID),
				zap.Int64("assistantMsgId", assistantMsg.ID),
				zap.String("stream", stream),
				zap.Error(stopErr))
		}
		// Send 时 sess 之前没有 session id，runner 返回新 id 落库；
		// Regenerate-fork 时 sess 有旧 id 但 runner 返回 fork 出来的新 id，必须覆盖。
		// claudecode resume 同 session 时返回的 id 与原 id 相同，覆盖无副作用。
		if result.ProviderSessionID != "" {
			sess.SetProviderSession(result.ProviderSessionID)
		}
		// claudecode 后端从 JSONL 抽到了本轮 user prompt 的 anchor（parentUuid）。
		// 写到 user msg 上，下次"重新生成 assistant"时 chat_svc.Regenerate 会读它。
		// 用 context.WithoutCancel 确保 abort 后这里仍能写 —— 已经流出去的内容
		// 应当持久化，下一轮即便重发也要能用 anchor 反查。
		if result.UserAnchor != "" && userMsg != nil {
			userMsg.ForkAnchor = result.UserAnchor
			_ = chat_repo.Message().Update(context.WithoutCancel(ctx), userMsg)
		}
		// codex app-server 上报的 modelContextWindow 落到 session 字段，下次
		// LoadSession 用 resolveContextWindowWithRuntime 优先读这个值——比
		// provider 静态配置和 catalog 兜底都准。仅在 runner 真的探到时更新，
		// 否则保留旧值，避免 claudecode / builtin 的 0 把先前 codex 写入的覆盖掉。
		if result.ContextWindow > 0 {
			sess.ContextWindow = result.ContextWindow
		}
	}
	// aborted 已在 acc.Finalize() 之后取出(见上方 MarkRunningSubagentsCancelled 调用)；
	// 这里的判定决定 StreamAborted vs StreamError/Done,以及 abort 路径跳过自动接续。
	awaitingPlanAction := stopErr == nil && !aborted &&
		!compact &&
		req.CollaborationMode == permissionModePlan &&
		hasActionablePlanBlock(finalBlocks)

	if stopErr != nil && !aborted {
		assistantMsg.ErrorText = stopErr.Error()
	}
	// finalCtx：去掉 cancel 信号但保留 DB 句柄。abort 路径下 turnCtx 已 cancel，
	// 用它写 Update 会静默失败，partial 内容就丢了。
	finalCtx := context.WithoutCancel(ctx)
	_ = chat_repo.Message().Update(finalCtx, assistantMsg)

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
		case stopErr != nil && !aborted:
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
	_ = chat_repo.Session().Update(finalCtx, sess)
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
	// 末端状态翻转主动推一帧 session_status:后台 session 出错/等审批时,前端 tab
	// 只订阅本会话 stream,StreamError 走 finishStream→bumpDone 不动 agentStatus,
	// 不补一刀 tab 红点要等下次 ListChatAgents 才同步。idle 不发 —— turn 正常收尾
	// 走 StreamDone,前端 chat-panel doneTick effect 会 reloadSession 主动拉一次。
	if (stopErr != nil && !aborted) || awaitingPlanAction {
		logger.Ctx(finalCtx).Info("chat_svc: session_status emit",
			zap.Int64("sessionId", sess.ID),
			zap.Int64("assistantMsgId", assistantMsg.ID),
			zap.String("stream", stream),
			zap.String("agentStatus", sess.AgentStatus),
			zap.Bool("needsAttention", sess.NeedsAttention),
			zap.Bool("aborted", aborted),
			zap.Bool("awaitingPlanAction", awaitingPlanAction),
			zap.Error(stopErr),
			zap.String("source", "finalize"))
		s.emitter.Emit(finalCtx, stream, ChatStreamEvent{
			Kind: StreamSessionStatus,
			SessionStatus: &ChatSessionStatusPatch{
				AgentStatus:    sess.AgentStatus,
				NeedsAttention: sess.NeedsAttention,
			},
		})
	}

	if len(pending) > 0 {
		nextUser, nextAssistant, payload, perr := s.persistAutoContinueTurn(finalCtx, sess, be, assistantMsg, assistantMsg.Model, pending)
		if perr == nil {
			s.emitter.Emit(finalCtx, stream, *payload)
			if be.IsClaudeCode() || be.IsCodex() {
				s.refreshPermissionModeForAutoContinue(finalCtx, sess)
			}
			// 同 goroutine + 同锁 + 同 stream 名递归跑下一轮：runTurn 内部
			// chatMessageForEvent / StreamDone 会以 nextAssistant 为目标 emit，
			// 前端 store 通过 StreamSteerConsumed.AssistantMessage 已经把活动
			// assistant 切到 nextAssistant。
			s.runTurn(ctx, sess, a, be, prov, nextUser, nextAssistant, stream, "", false)
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
		_ = chat_repo.Session().Update(finalCtx, sess)
	}

	final := chatMessageForEvent(sess, assistantMsg)
	switch {
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
	s.emitter.Emit(finalCtx, stream, ChatStreamEvent{Kind: StreamClosed})
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
	for _, msg := range userMsgs {
		cm, err := toChatMessage(msg)
		if err != nil {
			return nil, nil, fmt.Errorf("encode consumed steer message: %w", err)
		}
		cm.SessionID = sess.ID
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

// signChatTokenFor 为需要 gateway 的 CLI 后端本轮请求签一次性 token。
// 返回 (gatewayURL, token)，任意一者为空时调用方按"不签"处理（CLI 走自身 login）。
//
// Claude Code local 会使用 token 访问 /hook/v1/inbox；绑定了 LLM provider 的
// Claude Code / Codex 会用它走 LLM 转发。Codex local 不应调用这里。
//
// Token TTL = chatTokenTTL；当前不显式 RevokeToken：单轮结束后 cago retry 看到 401
// 自然触发新一轮 Send 重签。如担心库存增长，后续可在 turn 结束分支调 RevokeToken。
func (s *chatSvc) signChatTokenFor(ctx context.Context, be *agent_backend_entity.AgentBackend) (string, string) {
	if be == nil || s.gateway == nil {
		return "", ""
	}
	if s.gateway.Status().State != "running" {
		return "", ""
	}
	tok, err := s.gateway.IssueToken(ctx, be, chatTokenTTL)
	if err != nil {
		return "", ""
	}
	return s.gateway.URL(), tok
}

// mapClaudeProviderError 命中 claudecode.ErrSessionNotFound（CLI 报告
// "No conversation found with session ID: ..."）时做两件事：
//  1. 清空 sess.ProviderSessionID 并立即持久化（context.WithoutCancel 防 abort
//     路径下 turnCtx 已 cancel 导致静默失败）—— 下一轮 Send 才能 spawn 全新
//     CLI 会话，而不是一直拿 --resume 撞同一个失效 id。
//  2. 把 err 替换成 ChatProviderSessionGone 的 i18n 错误，前端拿到的就是
//     "CLI 会话已过期 …" 中文人话，不是英文 stderr。
//
// 非 ErrSessionNotFound 原样返回，让上层走 default 失败路径。
func (s *chatSvc) mapClaudeProviderError(ctx context.Context, sess *chat_entity.Session, src error) error {
	if !errors.Is(src, claudecode.ErrSessionNotFound) {
		return src
	}
	if sess != nil && sess.HasProviderSession() {
		sess.SetProviderSession("")
		_ = chat_repo.Session().Update(context.WithoutCancel(ctx), sess)
	}
	return i18n.NewError(ctx, code.ChatProviderSessionGone)
}

func (s *chatSvc) mapTurnError(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend, src error) error {
	if src == nil {
		return nil
	}
	if errors.Is(src, claudecode.ErrSessionNotFound) {
		return s.mapClaudeProviderError(ctx, sess, src)
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

func (s *chatSvc) failTurn(ctx context.Context, sess *chat_entity.Session, msg *chat_entity.Message, stream string, err error) {
	// 一次落地点收所有 turn 级别错误(selectRunner / resolveSessionCwd / runner.Run /
	// stream loop streamStopErr 等),给运维一条结构化日志带 session / message / stream
	// 三个定位用 ID,前端只看到 ErrorText 文案,真错的 message 在这里留底。
	logger.Ctx(ctx).Warn("chat_svc: turn failed",
		zap.Int64("sessionId", sess.ID),
		zap.Int64("messageId", msg.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Error(err))
	msg.ErrorText = err.Error()
	_ = chat_repo.Message().Update(ctx, msg)
	sess.AgentStatus = "error"
	sess.NeedsAttention = false
	_ = chat_repo.Session().Update(ctx, sess)
	// session_status 必须先于 StreamError emit:前端 chat-streams-host 收到 error
	// 立刻 finishStream 删 LiveStream entry → StreamSubscriber 紧接着 unmount,后到
	// 的 session_status 永远收不到。后台 session 出错时只靠 bumpDone 不会翻 tab 红点。
	logger.Ctx(ctx).Info("chat_svc: session_status emit",
		zap.Int64("sessionId", sess.ID),
		zap.Int64("assistantMsgId", msg.ID),
		zap.String("stream", stream),
		zap.String("agentStatus", sess.AgentStatus),
		zap.Bool("needsAttention", sess.NeedsAttention),
		zap.String("source", "failTurn"))
	s.emitter.Emit(ctx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
		},
	})
	s.emitter.Emit(ctx, stream, ChatStreamEvent{
		Kind:    StreamError,
		Error:   err.Error(),
		Message: chatMessageForEvent(sess, msg),
	})
	s.emitter.Emit(ctx, stream, ChatStreamEvent{Kind: StreamClosed})
}

func (s *chatSvc) lockFor(sessionID int64) *trylockMutex {
	v, _ := s.locks.LoadOrStore(sessionID, &trylockMutex{})
	return v.(*trylockMutex)
}

type trylockMutex struct{ mu sync.Mutex }

func (t *trylockMutex) TryLock() bool { return t.mu.TryLock() }
func (t *trylockMutex) Unlock()       { t.mu.Unlock() }

func sessionTitleFromFirstMessage(text string) string {
	return strings.TrimSpace(text)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	sess.Title = title
	if err := chat_repo.Session().Update(ctx, sess); err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	return &RenameResponse{}, nil
}

func (s *chatSvc) Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error) {
	if err := chat_repo.Session().SoftDelete(ctx, req.SessionID); err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	// DB 已删，释放该 session 的常驻 CLI 子进程（best-effort，cache miss 时 no-op）。
	claudecodert.Default().CloseSession(ctx, req.SessionID)
	codexrt.Default().CloseSession(ctx, req.SessionID)
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
		return nil, i18n.NewError(ctx, code.OperationFailed)
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
	sessions map[int64]struct{}
}

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
	deviceID, ok := be.DeviceIDInt()
	if !ok {
		return nil, i18n.NewError(ctx, code.AgentBackendInvalidDevice)
	}

	// Fast path: cache hit
	s.remoteMu.Lock()
	if s.remoteCache == nil {
		s.remoteCache = map[int64]*remoteRuntimeEntry{}
	}
	if entry, ok := s.remoteCache[deviceID]; ok {
		entry.sessions[sessionID] = struct{}{}
		s.remoteMu.Unlock()
		return entry.runtime, nil
	}
	s.remoteMu.Unlock()

	// Cold path: 借 lease + wrap runtime
	lease, err := s.pool().Borrow(ctx, deviceID)
	if err != nil {
		logger.Ctx(ctx).Error("borrowRemoteRuntime: pool.Borrow",
			zap.Int64("deviceID", deviceID), zap.Error(err))
		return nil, i18n.NewError(ctx, code.RemoteRunnerDialFailed)
	}
	rt := remote.New(lease.Client())

	// 同步拉一次远端 backend 的 capability 矩阵缓存到本地,之后 rt.Capabilities()
	// 直接返实际能力。失败不阻断 borrow —— Capabilities() 回退到
	// defaultCapsBeforePrefetch 占位,UI gating 不挂死。
	if err := rt.Prefetch(ctx, agent_backend_entity.BackendType(be.Type)); err != nil {
		logger.Ctx(ctx).Warn("borrowRemoteRuntime: capability prefetch failed",
			zap.Int64("deviceID", deviceID),
			zap.String("backendType", be.Type),
			zap.Error(err))
	}

	// Re-lock and insert. TOCTOU 输家:用赢家的 entry,释放自己的 lease。
	s.remoteMu.Lock()
	if existing, ok := s.remoteCache[deviceID]; ok {
		existing.sessions[sessionID] = struct{}{}
		s.remoteMu.Unlock()
		lease.Release()
		return existing.runtime, nil
	}
	entry := &remoteRuntimeEntry{
		runtime:  rt,
		lease:    lease,
		sessions: map[int64]struct{}{sessionID: {}},
	}
	s.remoteCache[deviceID] = entry
	s.remoteMu.Unlock()

	go s.watchLeaseClosed(deviceID, entry)
	return rt, nil
}

// watchLeaseClosed 监听 lease.Closed()(Pool 那侧通知 entry 失效),
// 然后把 chat_svc 这边的 cache entry 摘掉,下次 borrow 走冷路径重建 runtime。
func (s *chatSvc) watchLeaseClosed(deviceID int64, entry *remoteRuntimeEntry) {
	<-entry.lease.Closed()
	s.remoteMu.Lock()
	if cur, ok := s.remoteCache[deviceID]; ok && cur == entry {
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
