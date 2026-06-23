// Package group_svc 提供群聊编排应用服务(架在 chat_svc 之上)。
package group_svc

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
)

// Emitter 群事件出口(由 app 层注入 → wailsruntime.EventsEmit)。
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, string, any) {}

// GroupSvc 群聊编排服务。
type GroupSvc interface {
	ListGroups(ctx context.Context) ([]*group_entity.Group, error)
	CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error)
	LoadGroup(ctx context.Context, id int64) (*GroupDetail, error)
	AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error)
	RemoveGroupMember(ctx context.Context, memberID int64) error
	SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error
	IngestAgentMessage(ctx context.Context, memberID int64, body string, mentions []string) error
	// HandleInvite 是 group_invite MCP tool 的服务端入口:主持人把可用 agent 拉进群(可跨部门)。
	HandleInvite(ctx context.Context, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error)
	// HandleTaskCreate 是 group_task_create MCP tool 的服务端入口:建卡即派活。
	HandleTaskCreate(ctx context.Context, callerMemberID int64, assigneeName, title, brief string, parentTaskNo int) (*group_entity.GroupTask, error)
	// HandleTaskComplete 是 group_task_complete MCP tool 的服务端入口:仅执行人可交付,
	// result 必填(软验收门),completed 消息投回建卡人。
	HandleTaskComplete(ctx context.Context, callerMemberID int64, taskNo int, result string) (*group_entity.GroupTask, error)
	// HandleTaskCancel 是 group_task_cancel MCP tool 的服务端入口:仅建卡人或主持人可取消。
	HandleTaskCancel(ctx context.Context, callerMemberID int64, taskNo int, reason string) (*group_entity.GroupTask, error)
	// HandleGroupCreate 是 group_create MCP tool 的服务端入口:单聊轮经审批门拉起团队
	// (发起者=主持人,项目继承发起会话)。拒绝/超时编码为返回文本(nil err),error 仅内部故障。
	HandleGroupCreate(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64, memberNicknames map[string]string) (string, error)
	// BuildCreateTurnMCP 实现 chat_svc.TurnMCPProvider:给普通单聊轮注入 group_create(群成员轮跳过)。
	BuildCreateTurnMCP(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec
	// BuildSendTurnExtras 实现 chat_svc.TurnExtrasProvider:给群成员 backing session 直接发起的
	// 轮补齐群上下文(group_send MCP + 群 system-prompt 后缀),修设计问题⑥。
	BuildSendTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) ([]agentruntime.MCPServerSpec, string, bool)
	StopGroup(ctx context.Context, id int64) error
	PauseGroup(ctx context.Context, id int64) error
	ResumeGroup(ctx context.Context, id int64) error
	RenameGroup(ctx context.Context, id int64, title string) error
	// SetMemberNickname 设/清某成员在本群的群昵称(群内有效名);空白=清除回落 agent 名,
	// 非空须不与同群其它成员有效名冲突(@mention 唯一)。
	SetMemberNickname(ctx context.Context, memberID int64, nickname string) error
	SetGroupPinned(ctx context.Context, id int64, pinned bool) error
	// DeleteGroup 删除群(软删 status=DELETE)。deleteSessions=true 时一并软删全群成员的
	// backing session;false 时保留会话原样(仍带 group_id)。
	DeleteGroup(ctx context.Context, id int64, deleteSessions bool) error
	// MCPHandler 返回 group_send MCP handler，供 bootstrap 注册到 gateway /mcp/group/。
	MCPHandler() http.Handler
	// SetGatewayBaseURL 注入本机 gateway base(如 http://127.0.0.1:<port>)，供拼装 agent 子进程的 MCP 配置。
	SetGatewayBaseURL(u string)
}

type groupSvc struct {
	gw             ChatGateway
	emitter        Emitter
	now            func() int64
	names          func(ctx context.Context, agentID int64) string // agent id -> 展示名
	mu             sync.Mutex                                      // 保护 schedulers
	schedulers     map[int64]*scheduler                            // groupID -> 运行态调度器
	ingestLocks    *sync.Map                                       // groupID -> *sync.Mutex(串行化 IngestAgentMessage 临界区)
	mcp            *groupMCP                                       // group_send MCP server，注册到 gateway
	gatewayBaseURL string                                          // 本机 gateway base，由 bootstrap 注入

	approvalTimeout time.Duration // group_create 审批超时(默认 4min,对齐 orgtool 的 CLI 硬顶余量)
}

var defaultGroup GroupSvc = newGroupSvc(chatSvcGateway{}, NoopEmitter{})

func Default() GroupSvc     { return defaultGroup }
func SetDefault(s GroupSvc) { defaultGroup = s }
func SetEmitter(e Emitter) {
	if g, ok := defaultGroup.(*groupSvc); ok && e != nil {
		g.emitter = e
	}
}

func newGroupSvc(gw ChatGateway, e Emitter) *groupSvc {
	s := &groupSvc{
		gw:          gw,
		emitter:     e,
		now:         func() int64 { return time.Now().UnixMilli() },
		names:       defaultNameResolver,
		schedulers:  map[int64]*scheduler{},
		ingestLocks: &sync.Map{},
		mcp:         newGroupMCP(nil),
		// gatewayBaseURL set later via SetGatewayBaseURL
		approvalTimeout: 4 * time.Minute, // spike 实测 CLI 硬顶 ~285s,留 25s 余量(对齐 orgtool)
	}
	// 绑定 MCP ingest 回调(仅取方法值, 不调用) → group_send tool 路由到 IngestAgentMessage。
	s.mcp.ingest = s.IngestAgentMessage
	s.mcp.invite = s.HandleInvite
	// 任务三件套:mcp 层只拿 task_no / err(与实体解耦),路由到 HandleTask*。
	s.mcp.taskCreate = func(ctx context.Context, memberID int64, assignee, title, brief string, parentTaskNo int) (int, error) {
		t, err := s.HandleTaskCreate(ctx, memberID, assignee, title, brief, parentTaskNo)
		if err != nil {
			return 0, err
		}
		return t.TaskNo, nil
	}
	s.mcp.taskComplete = func(ctx context.Context, memberID int64, taskNo int, result string) error {
		_, err := s.HandleTaskComplete(ctx, memberID, taskNo, result)
		return err
	}
	s.mcp.taskCancel = func(ctx context.Context, memberID int64, taskNo int, reason string) error {
		_, err := s.HandleTaskCancel(ctx, memberID, taskNo, reason)
		return err
	}
	// group_create:单聊轮经审批门拉起团队(spec §7.1)。
	s.mcp.groupCreate = s.HandleGroupCreate
	// group_send 鉴权:无状态 token 验签通过后, 再按 DB 成员资格判定是否仍可发言。
	s.mcp.authz = s.memberCanPost
	return s
}

// memberCanPost 判定某成员当前是否仍可向群发言(group_send 鉴权):成员存在且 active、
// 属于该群、且群仍 active。成员离群(status=left)/ 群归档(status!=ACTIVE)即自动失权 ——
// 取代旧的内存 token 吊销表(无状态 token 无法 delete,改为按 DB 现状实时鉴权,且跨重启仍生效)。
// 停止(StopGroup)只改 RunStatus、不动 Status,故停止后成员仍有发言权 —— 恢复即用,
// 不会再让复用的常驻子进程拿 401 被误报"需要重新授权"。
func (s *groupSvc) memberCanPost(ctx context.Context, groupID, memberID int64) bool {
	m, err := group_repo.Member().Find(ctx, memberID)
	if err != nil || m == nil || !m.IsActive() || m.GroupID != groupID {
		return false
	}
	g, err := group_repo.Group().Find(ctx, groupID)
	return err == nil && g != nil && g.IsActive()
}

// defaultNameResolver 把 agent id 解析成展示名(找不到/出错返回空串)。
func defaultNameResolver(ctx context.Context, agentID int64) string {
	repo := agent_repo.Agent()
	if repo == nil {
		return ""
	}
	a, err := repo.Find(ctx, agentID)
	if err != nil || a == nil {
		return ""
	}
	return a.Name
}

// NewForTest 注入 mock 网关构造服务(单测用)。
func NewForTest(gw ChatGateway) GroupSvc { return newGroupSvc(gw, NoopEmitter{}) }

// NewForTestWithNames 注入 mock 网关 + 固定名字表构造服务(单测用)。
func NewForTestWithNames(gw ChatGateway, names map[int64]string) GroupSvc {
	s := newGroupSvc(gw, NoopEmitter{})
	s.names = func(_ context.Context, id int64) string { return names[id] }
	return s
}

func (s *groupSvc) ListGroups(ctx context.Context) ([]*group_entity.Group, error) {
	return group_repo.Group().List(ctx)
}

func (s *groupSvc) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error) {
	g := &group_entity.Group{
		Title:        req.Title,
		HostAgentID:  req.HostAgentID,
		DepartmentID: req.DepartmentID,
		ProjectID:    req.ProjectID,
		WorkflowID:   req.WorkflowID,
		RunStatus:    group_entity.RunIdle,
		Status:       consts.ACTIVE,
	}
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	if !s.backendSupportsGroup(ctx, req.HostAgentID) {
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	if g.DepartmentID == 0 {
		// 从主持人 agent 派生部门:仅作组织归属展示,不再限制 group_invite 招募池(spec §7 修订 06-03)。
		host, err := agent_repo.Agent().Find(ctx, req.HostAgentID)
		if err != nil {
			return nil, err
		}
		if host != nil {
			g.DepartmentID = host.DepartmentID
		}
	}
	if err := group_repo.Group().Create(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.ensureMember(ctx, g, req.HostAgentID, group_entity.RoleHost); err != nil {
		return nil, err
	}
	// memberCount 含主持人(已入群);逐个初始成员入群前先卡 maxMembers,与
	// AddGroupMember 同一上限语义,避免建群一次性绕过 8 人上限。
	memberCount := 1
	for _, agentID := range req.MemberAgentIDs {
		if agentID == req.HostAgentID {
			continue
		}
		if memberCount >= maxMembers {
			return nil, i18n.NewError(ctx, code.GroupMemberLimit)
		}
		if !s.backendSupportsGroup(ctx, agentID) {
			return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
		}
		if _, err := s.ensureMember(ctx, g, agentID, group_entity.RoleMember); err != nil {
			return nil, err
		}
		memberCount++
	}
	logger.Ctx(ctx).Info("group_svc.CreateGroup: created",
		zap.Int64("groupID", g.ID), zap.Int64("hostAgentID", req.HostAgentID))
	return s.LoadGroup(ctx, g.ID)
}

// HandleInvite 是 group_invite MCP tool 的服务端入口:主持人把招募池内的 agent 拉进群(可跨部门)。
// callerMemberID=调用成员(必须是主持人); names/ids 二选一指定被邀请 agent; reason 仅日志。
// 逐个经 backendSupportsGroup + maxMembers 门控,幂等 ensureMember,落 system "X 加入了群聊"。
func (s *groupSvc) HandleInvite(ctx context.Context, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	if !caller.IsHost() {
		return nil, i18n.NewError(ctx, code.GroupInviteForbidden)
	}
	// per-group 串行化,与 IngestAgentMessage 共用,避免并发入群重号。
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	// 招募池=全部 active 且后端支持群聊的 agent(部门仅组织单位,跨部门招人直接成立,spec §7)。
	pool, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, err
	}
	// 在招募池内按 id / 名解析(去重)。
	wantIDs := map[int64]bool{}
	for _, id := range ids {
		wantIDs[id] = true
	}
	wantNames := map[string]bool{}
	for _, n := range names {
		wantNames[n] = true
	}
	var targets []*agent_entity.Agent
	seen := map[int64]bool{}
	for _, a := range pool {
		if !a.IsActive() || seen[a.ID] {
			continue
		}
		if wantIDs[a.ID] || wantNames[a.Name] {
			seen[a.ID] = true
			targets = append(targets, a)
		}
	}

	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	memberCount := len(members)
	results := []InviteResult{}
	for _, a := range targets {
		if a.ID == g.HostAgentID {
			continue
		}
		if memberCount >= maxMembers {
			return nil, i18n.NewError(ctx, code.GroupMemberLimit)
		}
		if !s.backendSupportsGroup(ctx, a.ID) {
			logger.Ctx(ctx).Info("group_svc.HandleInvite: backend lacks CapMCPTools, skip",
				zap.Int64("agentId", a.ID), zap.String("reason", reason))
			continue
		}
		m, err := s.ensureMember(ctx, g, a.ID, group_entity.RoleMember)
		if err != nil {
			return nil, err
		}
		if !m.IsActive() {
			continue
		}
		memberCount++
		if _, err := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, a.Name+" 加入了群聊", nil, false, 0, 0, ""); err != nil {
			logger.Ctx(ctx).Warn("group_svc.HandleInvite: system message persist failed", zap.Error(err))
		}
		results = append(results, InviteResult{AgentID: a.ID, Name: a.Name})
	}
	logger.Ctx(ctx).Info("group_svc.HandleInvite: invited",
		zap.Int64("groupId", g.ID), zap.Int("count", len(results)))
	return results, nil
}

// ensureMember 幂等地把 agent 加入群。backing session 由 scheduler 首次投递时懒创建。
// 已存在且 active → 直接返回; 已存在但 left → 复活(Update); 不存在 → 新建(Create)。
func (s *groupSvc) ensureMember(ctx context.Context, g *group_entity.Group, agentID int64, role string) (*group_entity.GroupMember, error) {
	existing, err := group_repo.Member().FindByGroupAndAgent(ctx, g.ID, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsActive() {
		return existing, nil
	}
	if existing == nil {
		m := &group_entity.GroupMember{
			GroupID:  g.ID,
			AgentID:  agentID,
			Role:     role,
			Status:   group_entity.MemberActive,
			JoinedAt: s.now(),
		}
		if err := group_repo.Member().Create(ctx, m); err != nil {
			return nil, err
		}
		return m, nil
	}
	// existing != nil && !existing.IsActive(): 之前离开过的成员复活。
	// group_members 有 UNIQUE(group_id, agent_id), 必须 Update 而非 Create。
	existing.Role = role
	existing.Status = group_entity.MemberActive
	existing.JoinedAt = s.now()
	if err := group_repo.Member().Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *groupSvc) memberSessionTitle(ctx context.Context, g *group_entity.Group, agentID int64) string {
	groupTitle := ""
	if g != nil {
		groupTitle = strings.TrimSpace(g.Title)
	}
	agentName := strings.TrimSpace(s.names(ctx, agentID))
	if agentName == "" {
		agentName = "Agent #" + strconv.FormatInt(agentID, 10)
	}
	if groupTitle == "" {
		return agentName
	}
	return groupTitle + " / " + agentName
}

func (s *groupSvc) LoadGroup(ctx context.Context, id int64) (*GroupDetail, error) {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	msgs, err := group_repo.Message().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	// 任务卡:仓储未装配(NewForTest 体系)/查询失败 → 降级跳过(与 openTaskSnapshot 同策略),仅留日志。
	var tasks []*group_entity.GroupTask
	if repo := group_repo.Task(); repo != nil {
		rows, err := repo.ListByGroup(ctx, id)
		if err != nil {
			logger.Ctx(ctx).Warn("group_svc.LoadGroup: list tasks failed", zap.Error(err))
		} else {
			tasks = rows
		}
	}
	return &GroupDetail{
		Group:           g,
		Members:         members,
		Messages:        msgs,
		MemberRunStates: s.memberRunStates(id, members),
		Tasks:           tasks,
	}, nil
}

func (s *groupSvc) AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(members) >= maxMembers {
		return nil, i18n.NewError(ctx, code.GroupMemberLimit)
	}
	if !s.backendSupportsGroup(ctx, agentID) {
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	return s.ensureMember(ctx, g, agentID, group_entity.RoleMember)
}

// backendSupportsGroup 门控成员后端是否支持群聊(必须声明 CapMCPTools 才能被注入 group_send tool)。
func (s *groupSvc) backendSupportsGroup(ctx context.Context, agentID int64) bool {
	ok, err := s.gw.AgentBackendHasCapability(ctx, agentID, capability.CapMCPTools)
	if err != nil {
		logger.Ctx(ctx).Warn("group_svc.backendSupportsGroup: capability probe failed",
			zap.Int64("agentID", agentID), zap.Error(err))
		return false
	}
	return ok
}

func (s *groupSvc) RemoveGroupMember(ctx context.Context, memberID int64) error {
	m, err := group_repo.Member().Find(ctx, memberID)
	if err != nil {
		return err
	}
	if m == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	m.Status = group_entity.MemberLeft
	if err := group_repo.Member().Update(ctx, m); err != nil {
		return err
	}
	// 离群级联:其名下 open 任务全部取消(spec §5)。与消息管线共用 per-group 锁。
	func() {
		mu := s.ingestMu(m.GroupID)
		mu.Lock()
		defer mu.Unlock()
		s.cancelTasksOfLeftMember(ctx, m.GroupID, m.ID)
	}()
	// 无需显式吊销 token:status=left 后 memberCanPost 即拒绝其 group_send(按 DB 现状实时鉴权)。
	// 软删该成员的 backing session, 否则它以 group_id>0 的 ACTIVE 会话残留, 经 ListAgents 的
	// IncludingGroups 变体继续出现在该 agent 侧栏。best-effort: 删除失败不回滚离群(主效果已生效)。
	if m.BackingSessionID > 0 {
		if err := s.gw.DeleteSession(ctx, m.BackingSessionID); err != nil {
			logger.Ctx(ctx).Warn("group_svc.RemoveGroupMember: delete backing session failed",
				zap.Int64("memberId", memberID), zap.Int64("sessionId", m.BackingSessionID), zap.Error(err))
		}
	}
	return nil
}

// SendGroupMessage 把一条用户消息投入群: 解析收件人 → 落 group_message → 入队 agent 收件人。
func (s *groupSvc) SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error {
	// per-group 串行化「解析→分配 seq→落库→入队」临界区, 与 IngestAgentMessage 共用同一把锁
	// (spec §17 并发写竞态): 否则用户 send 与 agent 的 mid-turn group_send 可读到同一 MAX(seq) → 重号。
	// 锁内不再 re-acquire ingestMu(kick/persistMessage/enqueueDeliveries 都不碰它), 故无自死锁。
	mu := s.ingestMu(req.GroupID)
	mu.Lock()
	defer mu.Unlock()
	g, err := group_repo.Group().Find(ctx, req.GroupID)
	if err != nil {
		return err
	}
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return err
	}
	recipientIDs, toUser := s.resolveRecipientsFromRequest(req)
	if len(recipientIDs) == 0 && !toUser { // 用户没选收件人 → 默认投主持人(spec §17)
		for _, m := range members {
			if m.IsHost() {
				recipientIDs = []int64{m.ID}
				break
			}
		}
	}
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindUser, 0, req.Text, recipientIDs, toUser, 0, 0, ""); err != nil {
		return err
	}
	// 用户发言重置 round_count(仅 UI 计数)
	g.RoundCount = 0
	_ = group_repo.Group().Update(ctx, g)
	logger.Ctx(ctx).Info("group_svc.SendGroupMessage: sent",
		zap.Int64("groupID", g.ID), zap.Int64s("recipientMemberIDs", recipientIDs), zap.Bool("toUser", toUser))
	// 把 agent 收件人入队 + 踢调度器。fromName 必须与 prompt 的「@用户 = 回复人类」
	// 词汇一致, 成员才能把回复路由回提问的人(回归: 抬头写「你」时成员认不出来源
	// 是人类, 把任务汇报 @ 给了别的成员)。
	s.enqueueDeliveries(g.ID, recipientIDs, req.Text, "用户", 0)
	s.kick(ctx, g.ID)
	return nil
}

// resolveRecipientsFromRequest: 用户消息收件人已由前端解析成结构化字段; 后端不做文本 mention 解析。
func (s *groupSvc) resolveRecipientsFromRequest(req *SendGroupMessageRequest) ([]int64, bool) {
	return req.RecipientMemberIDs, req.ToUser
}

func (s *groupSvc) persistMessage(ctx context.Context, g *group_entity.Group, kind string, senderMemberID int64, content string, recipients []int64, toUser bool, sourceMsgID int64, taskID int64, taskEvent string) (*group_entity.GroupMessage, error) {
	seq, err := group_repo.Message().NextSeq(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	m := &group_entity.GroupMessage{
		GroupID:         g.ID,
		Seq:             seq,
		SenderKind:      kind,
		SenderMemberID:  senderMemberID,
		ToUser:          toUser,
		Content:         content,
		SourceMessageID: sourceMsgID,
		TaskID:          taskID,
		TaskEvent:       taskEvent,
		Createtime:      s.now(),
	}
	m.SetRecipients(recipients)
	if err := group_repo.Message().Create(ctx, m); err != nil {
		return nil, err
	}
	s.emitter.Emit(ctx, groupEventName(g.ID), map[string]any{"kind": "message", "message": toGroupMessageEvent(m)})
	return m, nil
}

func groupEventName(groupID int64) string { return "group:event:" + strconv.FormatInt(groupID, 10) }

// GroupRunStateEventName 是面向侧栏的全局运行态频道:只发 run_status 与
// member_run_state(带 groupID / backingSessionID),不广播群消息。per-group 频道
// (group:event:<id>)仅打开的群页订阅;侧栏的群行与成员 backing session 行
// 需要不开群页也能实时拿到运行态,由前端常驻 GroupEventsHost 订阅这里。
const GroupRunStateEventName = "groups:run_state"

// emitRunStatus 把 run_status 变更同时发到 per-group 频道(群页)与全局频道(侧栏)。
func (s *groupSvc) emitRunStatus(ctx context.Context, groupID int64, status string) {
	payload := map[string]any{"kind": "run_status", "groupID": groupID, "runStatus": status}
	s.emitter.Emit(ctx, groupEventName(groupID), payload)
	s.emitter.Emit(ctx, GroupRunStateEventName, payload)
}

// MCPHandler 供 bootstrap 注册到 gateway /mcp/group/。
func (s *groupSvc) MCPHandler() http.Handler { return s.mcp }

// SetGatewayBaseURL 由 bootstrap 注入本机 gateway base(如 http://127.0.0.1:<port>)。
func (s *groupSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// NewGroupMCPForTest 仅测试用 —— 直接构造 handler 注入 ingest 回调(authz 留空=放行)。
func NewGroupMCPForTest(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return newGroupMCP(ingest)
}

// NewGroupMCPForTestWithAuthz 仅测试用 —— 在 ingest 之外再注入发言权判定, 验证 group_send 鉴权门控。
func NewGroupMCPForTestWithAuthz(
	ingest func(context.Context, int64, string, []string) error,
	authz func(context.Context, int64, int64) bool,
) *groupMCP {
	h := newGroupMCP(ingest)
	h.authz = authz
	return h
}

// buildGroupMCP 为某成员投递签发一次性 token, 返回注入到 RunRequest.MCPServers 的 group MCP server。
func (s *groupSvc) buildGroupMCP(g *group_entity.Group, m *group_entity.GroupMember) []agentruntime.MCPServerSpec {
	tok := s.mcp.MintToken(g.ID, m.ID)
	// 所有成员可 group_send + 任务三件套(跨成员派活/交接一律走任务卡);
	// 仅主持人 turn 额外注入 group_invite(招募可用 agent,可跨部门)。
	tools := []string{"group_send", "group_task_create", "group_task_complete", "group_task_cancel"}
	if m.IsHost() {
		tools = append(tools, "group_invite")
	}
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + tok},
		Tools:   tools,
	}}
}

// BuildSendTurnExtras 实现 chat_svc.TurnExtrasProvider:为群成员 backing session
// (groupID>0)补齐群上下文 —— group_send 等群工具 MCP + 群 system-prompt 后缀。解决
// 设计问题⑥:用户直接对成员 backing session 发起 Send/Edit/Regenerate(不经
// scheduler.launchDelivery)时 extras 为零值、群上下文丢失。与 launchDelivery 调用同一对
// buildGroupMCP/buildGroupSystemPrompt,构造逻辑单一。ok=false 表示不适用(非群会话 /
// 网关未就绪 / 该 agent 不是本群成员)。group_send 是群成员固有能力,不走 per-agent
// ToolEnabled 门控(镜像 launchDelivery 对成员一律注入)。
func (s *groupSvc) BuildSendTurnExtras(ctx context.Context, a *agent_entity.Agent, _ int64, groupID int64) ([]agentruntime.MCPServerSpec, string, bool) {
	if a == nil || groupID <= 0 || s.gatewayBaseURL == "" {
		return nil, "", false
	}
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil || g == nil {
		return nil, "", false
	}
	members, err := group_repo.Member().ListByGroup(ctx, groupID)
	if err != nil {
		return nil, "", false
	}
	var me *group_entity.GroupMember
	for _, m := range members {
		if m.AgentID == a.ID {
			me = m
			break
		}
	}
	if me == nil {
		return nil, "", false
	}
	return s.buildGroupMCP(g, me), s.buildGroupSystemPrompt(g, members, me), true
}

// buildGroupSystemPrompt 拼接注入到成员 turn 的群聊 system prompt 后缀
// (角色 + roster + tool 用法 + 任务协作纪律 + SOP + 未完成任务快照 + 交付物约定 + worktree 引导)。
func (s *groupSvc) buildGroupSystemPrompt(g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	bg := context.Background()
	var b strings.Builder
	role := "成员"
	if me.IsHost() {
		role = "主持人(部门负责人)"
	}
	fmt.Fprintf(&b, "\n\n## 群聊「%s」\n你是本群的%s。", g.Title, role)
	b.WriteString("\n当前成员：")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s（%s）", s.memberDisplayName(bg, m), m.Role)
	}
	b.WriteString("\n\n你只会收到 @ 到你的消息。要发言请调用 `group_send` 工具：body=正文，mentions=收件成员显示名数组（@用户 = 回复人类）。一个回合可多次调用、可分别对不同人发不同内容。**不调用 group_send 的内容不会进群**。")
	b.WriteString("\n回复路由：消息抬头「(来自 X)」标明来源，回复时默认 mentions 该来源——来源是用户时用 mentions:[\"用户\"]。除非任务确实需要协作，不要主动 @ 其他成员；任务完成直接向来源汇报，不要转发给主持人或其他成员。")

	b.WriteString("\n\n### 任务协作")
	b.WriteString("\n跨成员派活/交接一律用任务卡：`group_task_create`(assignee=成员显示名, title, brief 含验收标准, parentTaskId 可选回指) 建卡即派活；做完自己的任务调用 `group_task_complete`(taskId, result)——result 必须写清改动/产出与自测情况。`group_task_cancel`(taskId, reason) 仅建卡人或主持人可调；打回返工=新建任务卡。任务卡的完成汇报只走 group_task_complete，不要再额外 group_send 重复汇报。")
	b.WriteString("\n过程交付物（PRD 草稿/设计说明/测试报告等不进版本库的交接物）写到 `.agentre/handoff/" + strconv.FormatInt(g.ID, 10) + "/task-<编号>-<slug>.md`（自行 mkdir -p；首次写入前把 `.agentre/` 追加进 `.git/info/exclude`）；正式产物（代码、要进 repo 的文档）照常放仓库正常位置。brief/result 引用文件路径，不要贴全文。")

	if me.IsHost() {
		b.WriteString("\n\n### 主持人编排")
		b.WriteString("\n标准动作环：理解需求 → 拆解 → group_task_create 派活（可能改同一片代码的任务串行派）→ 收到 completed 后派验证任务（测试/审查可并行，parentTaskId 回指被验证任务）→ 全部通过后 group_send @用户 汇总；发现问题 → 新任务卡打回。")
		if g.WorkflowID > 0 {
			if repo := workflow_repo.Workflow(); repo != nil {
				if wf, err := repo.Find(bg, g.WorkflowID); err == nil && wf.IsActive() && strings.TrimSpace(wf.Content) != "" {
					b.WriteString("\n\n### 团队协作流程（按此编排，用户首条消息可临时覆盖）\n" + strings.TrimSpace(wf.Content))
				}
			}
		}
		b.WriteString("\n作为主持人，调用 `group_invite` 工具邀请 Agent 进群（可跨部门）：agentNames 填显示名数组（或 agentIds 填 id），reason 写明跨部门理由。")
		if roster := s.recruitableRoster(bg, members); roster != "" {
			b.WriteString("\n可招募：" + roster)
		}
	} else {
		b.WriteString("\n\n收到任务后：在项目目录执行 → 自测 → group_task_complete 交付；需要协作可自行 group_task_create 或 group_send。")
	}

	if snapshot := s.openTaskSnapshot(bg, g, members, me); snapshot != "" {
		b.WriteString("\n\n### 未完成任务\n" + snapshot)
	}

	b.WriteString("\n若你要修改文件且可能与他人并发，请先 `git worktree add` 在自己的工作树里作业。")
	return b.String()
}

// openTaskSnapshot 渲染未完成任务快照:主持人看全群,成员只看与自己相关的
// (自己是 assignee/creator,spec §4)。仓储未装配(NewForTest 体系)/查询失败 → 静默跳过。
func (s *groupSvc) openTaskSnapshot(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	repo := group_repo.Task()
	if repo == nil {
		return ""
	}
	tasks, err := repo.ListByGroup(ctx, g.ID)
	if err != nil {
		return ""
	}
	nameOf := func(memberID int64) string {
		for _, m := range members {
			if m.ID == memberID {
				return s.memberDisplayName(ctx, m)
			}
		}
		return "?"
	}
	var lines []string
	for _, t := range tasks {
		if !t.IsOpen() {
			continue
		}
		if !me.IsHost() && t.AssigneeMemberID != me.ID && t.CreatorMemberID != me.ID {
			continue
		}
		line := fmt.Sprintf("#%d %s → %s", t.TaskNo, t.Title, nameOf(t.AssigneeMemberID))
		if t.ParentTaskNo > 0 {
			line += fmt.Sprintf("（验证 #%d）", t.ParentTaskNo)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// recruitableRoster 列出全部 active、尚未进群、且后端支持 CapMCPTools 的 agent(名字·id),
// 供主持人 system prompt 提示可 group_invite 的对象(可跨部门)。空字符串=没有可招募对象。
func (s *groupSvc) recruitableRoster(ctx context.Context, members []*group_entity.GroupMember) string {
	pool, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return ""
	}
	inGroup := map[int64]bool{}
	for _, m := range members {
		inGroup[m.AgentID] = true
	}
	var parts []string
	for _, a := range pool {
		if !a.IsActive() || inGroup[a.ID] {
			continue
		}
		if !s.backendSupportsGroup(ctx, a.ID) {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(id=%d)", a.Name, a.ID))
	}
	return strings.Join(parts, "、")
}

// StopGroup 用户点「停止」: 中止所有在跑成员 turn + 清队列 + run_status=idle。
func (s *groupSvc) StopGroup(ctx context.Context, id int64) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	s.stopAll(ctx, id)
	// 停止不吊销 token:仅暂停 turn(只改 RunStatus)。成员 / 群仍有效 → 恢复发言即用,
	// 不会让复用的常驻子进程拿 401。失权只发生在离群 / 归档(memberCanPost 判定)。
	g.RunStatus = group_entity.RunIdle
	if err := group_repo.Group().Update(ctx, g); err != nil {
		return err
	}
	logger.Ctx(ctx).Info("group_svc.StopGroup: stopped", zap.Int64("groupId", id))
	s.emitRunStatus(ctx, id, group_entity.RunIdle)
	return nil
}

// PauseGroup 暂停: 停止填新槽; 在跑 turn 自然跑完(CanAdvance(paused)=false)。
func (s *groupSvc) PauseGroup(ctx context.Context, id int64) error {
	return s.setRunStatus(ctx, id, group_entity.RunPaused)
}

// ResumeGroup 恢复: run_status=running 后立刻 kick 填槽。
func (s *groupSvc) ResumeGroup(ctx context.Context, id int64) error {
	if err := s.setRunStatus(ctx, id, group_entity.RunRunning); err != nil {
		return err
	}
	s.kick(ctx, id)
	return nil
}

// setRunStatus 用户控制改 run_status(返回错误); 与 scheduler 内部的 transitionRunStatus 区分
// (后者无错误返回、带 only-on-change 守卫、在 sc.mu 内被 kick 调用)。
func (s *groupSvc) setRunStatus(ctx context.Context, id int64, status string) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	g.RunStatus = status
	if err := group_repo.Group().Update(ctx, g); err != nil {
		return err
	}
	logger.Ctx(ctx).Info("group_svc.setRunStatus: changed", zap.Int64("groupId", id), zap.String("runStatus", status))
	s.emitRunStatus(ctx, id, status)
	return nil
}

// RenameGroup 改群名(非空白)。
func (s *groupSvc) RenameGroup(ctx context.Context, id int64, title string) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	if strings.TrimSpace(title) == "" {
		return i18n.NewError(ctx, code.GroupTitleRequired)
	}
	g.Title = title
	return group_repo.Group().Update(ctx, g)
}

// memberDisplayName 给出某成员在本群的有效显示名:设了群昵称用昵称, 否则回落 agent 全局名。
// roster / @mention 匹配 / 注入 AI 的群名单 / 投递抬头 都经此口径(单一真相)。
func (s *groupSvc) memberDisplayName(ctx context.Context, m *group_entity.GroupMember) string {
	if m == nil {
		return ""
	}
	return m.DisplayName(s.names(ctx, m.AgentID))
}

// SetMemberNickname 设/清某成员在本群的群昵称(群内有效名)。空白=清除回落 agent 名。
// 非空时校验不与同群其它成员有效名冲突(@mention 须唯一), 冲突报 GroupNicknameTaken。
func (s *groupSvc) SetMemberNickname(ctx context.Context, memberID int64, nickname string) error {
	m, err := group_repo.Member().Find(ctx, memberID)
	if err != nil || m == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	trimmed := strings.TrimSpace(nickname)
	if trimmed != "" {
		members, err := group_repo.Member().ListByGroup(ctx, m.GroupID)
		if err != nil {
			return err
		}
		for _, other := range members {
			if other.ID == memberID || !other.IsActive() {
				continue
			}
			if s.memberDisplayName(ctx, other) == trimmed {
				return i18n.NewError(ctx, code.GroupNicknameTaken)
			}
		}
	}
	if err := group_repo.Member().SetNickname(ctx, memberID, trimmed); err != nil {
		return err
	}
	m.Nickname = trimmed
	s.emitter.Emit(ctx, groupEventName(m.GroupID), map[string]any{
		"kind":   "member_updated",
		"member": toGroupMemberEvent(m),
	})
	logger.Ctx(ctx).Info("group_svc.SetMemberNickname: set",
		zap.Int64("memberId", memberID), zap.Int64("groupId", m.GroupID), zap.String("nickname", trimmed))
	return nil
}

// SetGroupPinned 切换群用户置顶（侧栏混排列表浮顶）。
func (s *groupSvc) SetGroupPinned(ctx context.Context, id int64, pinned bool) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil {
		return err
	}
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	return group_repo.Group().SetPinned(ctx, id, pinned)
}

// DeleteGroup 删除群(软删 status=DELETE): 先 stopAll 再 status=DELETE。
// deleteSessions=true 时一并软删全群成员的 backing session;false 时保留会话原样。
func (s *groupSvc) DeleteGroup(ctx context.Context, id int64, deleteSessions bool) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	s.stopAll(ctx, id)
	// 删除无需显式吊销 token:status=DELETE 后 memberCanPost(群已非 active)即拒绝全群 group_send。
	// deleteSessions=true 时软删全群成员的 backing session, 否则它们仍以 group_id>0 的 ACTIVE 会话
	// 残留在各 agent 侧栏。best-effort: 单条删除失败不阻断删除。
	if deleteSessions {
		if members, err := group_repo.Member().ListByGroup(ctx, id); err == nil {
			for _, m := range members {
				if m.BackingSessionID <= 0 {
					continue
				}
				if derr := s.gw.DeleteSession(ctx, m.BackingSessionID); derr != nil {
					logger.Ctx(ctx).Warn("group_svc.DeleteGroup: delete backing session failed",
						zap.Int64("groupId", id), zap.Int64("sessionId", m.BackingSessionID), zap.Error(derr))
				}
			}
		}
	}
	g.Status = consts.DELETE
	logger.Ctx(ctx).Info("group_svc.DeleteGroup: deleted",
		zap.Int64("groupId", id), zap.Bool("deleteSessions", deleteSessions))
	return group_repo.Group().Update(ctx, g)
}
