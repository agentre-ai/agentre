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

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/group_repo"
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
	// HandleInvite 是 group_invite MCP tool 的服务端入口:协调者把部门池内 agent 拉进群。
	HandleInvite(ctx context.Context, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error)
	StopGroup(ctx context.Context, id int64) error
	PauseGroup(ctx context.Context, id int64) error
	ResumeGroup(ctx context.Context, id int64) error
	RenameGroup(ctx context.Context, id int64, title string) error
	SetGroupPinned(ctx context.Context, id int64, pinned bool) error
	ArchiveGroup(ctx context.Context, id int64) error
	// MCPHandler 返回 group_send MCP handler，供 bootstrap(D2) 注册到 gateway /mcp/group/。
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
	schedulers     map[int64]*scheduler                            // groupID -> 运行态(Task C5)
	ingestLocks    *sync.Map                                       // groupID -> *sync.Mutex(串行化 IngestAgentMessage 临界区)
	mcp            *groupMCP                                       // group_send MCP server(D2 注册到 gateway)
	gatewayBaseURL string                                          // 本机 gateway base(D2 注入)
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
	}
	// 绑定 MCP ingest 回调(仅取方法值, 不调用) → group_send tool 路由到 IngestAgentMessage。
	s.mcp.ingest = s.IngestAgentMessage
	s.mcp.invite = s.HandleInvite
	return s
}

// defaultNameResolver 把 agent id 解析成展示名(找不到/出错返回空串)。
func defaultNameResolver(ctx context.Context, agentID int64) string {
	a, err := agent_repo.Agent().Find(ctx, agentID)
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
		Title:              req.Title,
		CoordinatorAgentID: req.CoordinatorAgentID,
		DepartmentID:       req.DepartmentID,
		ProjectID:          req.ProjectID,
		RunStatus:          group_entity.RunIdle,
		Status:             consts.ACTIVE,
	}
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	if !s.backendSupportsGroup(ctx, req.CoordinatorAgentID) {
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	if g.DepartmentID == 0 {
		// 从协调者 agent 派生部门:决定 group_invite 的可招募池(部门内 agent)。
		coordinator, err := agent_repo.Agent().Find(ctx, req.CoordinatorAgentID)
		if err != nil {
			return nil, err
		}
		if coordinator != nil {
			g.DepartmentID = coordinator.DepartmentID
		}
	}
	if err := group_repo.Group().Create(ctx, g); err != nil {
		return nil, err
	}
	if _, err := s.ensureMember(ctx, g, req.CoordinatorAgentID, group_entity.RoleCoordinator); err != nil {
		return nil, err
	}
	// memberCount 含协调者(已入群);逐个初始成员入群前先卡 maxMembers,与
	// AddGroupMember 同一上限语义,避免建群一次性绕过 8 人上限。
	memberCount := 1
	for _, agentID := range req.MemberAgentIDs {
		if agentID == req.CoordinatorAgentID {
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
		zap.Int64("groupID", g.ID), zap.Int64("coordinatorAgentID", req.CoordinatorAgentID))
	return s.LoadGroup(ctx, g.ID)
}

// HandleInvite 是 group_invite MCP tool 的服务端入口:协调者把部门招募池内的 agent 拉进群。
// callerMemberID=调用成员(必须是协调者); names/ids 二选一指定被邀请 agent; reason 仅日志。
// 逐个经 backendSupportsGroup + maxMembers 门控,幂等 ensureMember,落 system "X 加入了群聊"。
func (s *groupSvc) HandleInvite(ctx context.Context, callerMemberID int64, names []string, ids []int64, reason string) ([]InviteResult, error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	if !caller.IsCoordinator() {
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
	pool, err := agent_repo.Agent().ListByDepartment(ctx, g.DepartmentID)
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
		if a.ID == g.CoordinatorAgentID {
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
		if _, err := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, a.Name+" 加入了群聊", nil, false, 0); err != nil {
			logger.Ctx(ctx).Warn("group_svc.HandleInvite: system message persist failed", zap.Error(err))
		}
		results = append(results, InviteResult{AgentID: a.ID, Name: a.Name})
	}
	logger.Ctx(ctx).Info("group_svc.HandleInvite: invited",
		zap.Int64("groupId", g.ID), zap.Int("count", len(results)))
	return results, nil
}

// ensureMember 幂等地把 agent 加入群(建 member + backing session)。
// 已存在且 active → 直接返回; 已存在但 left → 复活(Update); 不存在 → 新建(Create)。
func (s *groupSvc) ensureMember(ctx context.Context, g *group_entity.Group, agentID int64, role string) (*group_entity.GroupMember, error) {
	existing, err := group_repo.Member().FindByGroupAndAgent(ctx, g.ID, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsActive() {
		return existing, nil
	}
	sessID, err := s.gw.EnsureGroupMemberSession(ctx, agentID, g.ProjectID, g.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		m := &group_entity.GroupMember{
			GroupID:          g.ID,
			AgentID:          agentID,
			BackingSessionID: sessID,
			Role:             role,
			Status:           group_entity.MemberActive,
			JoinedAt:         s.now(),
		}
		if err := group_repo.Member().Create(ctx, m); err != nil {
			return nil, err
		}
		return m, nil
	}
	// existing != nil && !existing.IsActive(): 之前离开过的成员复活。
	// group_members 有 UNIQUE(group_id, agent_id), 必须 Update 而非 Create。
	existing.BackingSessionID = sessID
	existing.Role = role
	existing.Status = group_entity.MemberActive
	existing.JoinedAt = s.now()
	if err := group_repo.Member().Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
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
	return &GroupDetail{Group: g, Members: members, Messages: msgs}, nil
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
	s.mcp.RevokeMember(memberID) // 离群即吊销其 group_send token(spec §17)
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
	if len(recipientIDs) == 0 && !toUser { // 用户没选收件人 → 默认投协调者(spec §17)
		for _, m := range members {
			if m.IsCoordinator() {
				recipientIDs = []int64{m.ID}
				break
			}
		}
	}
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindUser, 0, req.Text, recipientIDs, toUser, 0); err != nil {
		return err
	}
	// 用户发言重置 round_count(仅 UI 计数)
	g.RoundCount = 0
	_ = group_repo.Group().Update(ctx, g)
	logger.Ctx(ctx).Info("group_svc.SendGroupMessage: sent",
		zap.Int64("groupID", g.ID), zap.Int64s("recipientMemberIDs", recipientIDs), zap.Bool("toUser", toUser))
	// 把 agent 收件人入队 + 踢调度器(C5 实现真逻辑; 本 Task 占位)
	s.enqueueDeliveries(g.ID, recipientIDs, req.Text, "你")
	s.kick(ctx, g.ID)
	return nil
}

// resolveRecipientsFromRequest: 用户消息收件人已由前端解析成结构化字段; 后端不做文本 mention 解析。
func (s *groupSvc) resolveRecipientsFromRequest(req *SendGroupMessageRequest) ([]int64, bool) {
	return req.RecipientMemberIDs, req.ToUser
}

func (s *groupSvc) persistMessage(ctx context.Context, g *group_entity.Group, kind string, senderMemberID int64, content string, recipients []int64, toUser bool, sourceMsgID int64) (*group_entity.GroupMessage, error) {
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

// MCPHandler 供 bootstrap(D2) 注册到 gateway /mcp/group/。
func (s *groupSvc) MCPHandler() http.Handler { return s.mcp }

// SetGatewayBaseURL bootstrap(D2) 注入本机 gateway base(如 http://127.0.0.1:<port>)。
func (s *groupSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// NewGroupMCPForTest 仅测试用 —— 直接构造 handler 注入 ingest 回调。
func NewGroupMCPForTest(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return newGroupMCP(ingest)
}

// buildGroupMCP 为某成员投递签发一次性 token, 返回注入到 RunRequest.MCPServers 的 group MCP server。
// (C5 launchDelivery 调用; 本任务后暂未被引用是预期的。)
func (s *groupSvc) buildGroupMCP(g *group_entity.Group, m *group_entity.GroupMember) []agentruntime.MCPServerSpec {
	tok := s.mcp.MintToken(g.ID, m.ID)
	// 所有成员可 group_send; 仅协调者 turn 额外注入 group_invite(招募部门同事)。
	tools := []string{"group_send"}
	if m.IsCoordinator() {
		tools = append(tools, "group_invite")
	}
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + tok},
		Tools:   tools,
	}}
}

// buildGroupSystemPrompt 拼接注入到成员 turn 的群聊 system prompt 后缀(角色 + roster + tool 用法 + worktree 引导)。
func (s *groupSvc) buildGroupSystemPrompt(g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	var b strings.Builder
	role := "成员"
	if me.IsCoordinator() {
		role = "协调者(部门 leader)"
	}
	fmt.Fprintf(&b, "\n\n## 群聊「%s」\n你是本群的%s。", g.Title, role)
	b.WriteString("\n当前成员：")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s（%s）", s.names(context.Background(), m.AgentID), m.Role)
	}
	b.WriteString("\n\n你只会收到 @ 到你的消息。要发言请调用 `group_send` 工具：body=正文，mentions=收件成员显示名数组（@用户 = 回复人类）。一个回合可多次调用、可分别对不同人发不同内容。**不调用 group_send 的内容不会进群**。")
	if me.IsCoordinator() {
		b.WriteString("\n作为协调者，调用 `group_invite` 工具邀请本部门同事进群：agentNames 填显示名数组（或 agentIds 填 id），reason 可选。")
		if roster := s.recruitableRoster(context.Background(), g, members); roster != "" {
			b.WriteString("\n可招募同事：" + roster)
		}
	}
	b.WriteString("\n若你要修改文件且可能与他人并发，请先 `git worktree add` 在自己的工作树里作业。")
	return b.String()
}

// recruitableRoster 列出部门内、尚未进群、且后端支持 CapMCPTools 的 agent(名字·id),
// 供协调者 system prompt 提示可 group_invite 的对象。空字符串=没有可招募对象。
func (s *groupSvc) recruitableRoster(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember) string {
	if g.DepartmentID == 0 {
		return ""
	}
	pool, err := agent_repo.Agent().ListByDepartment(ctx, g.DepartmentID)
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
	s.mcp.RevokeGroup(id) // 停止即吊销全群 group_send token(spec §17)
	g.RunStatus = group_entity.RunIdle
	if err := group_repo.Group().Update(ctx, g); err != nil {
		return err
	}
	logger.Ctx(ctx).Info("group_svc.StopGroup: stopped", zap.Int64("groupId", id))
	s.emitter.Emit(ctx, groupEventName(id), map[string]any{"kind": "run_status", "runStatus": group_entity.RunIdle})
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
	s.emitter.Emit(ctx, groupEventName(id), map[string]any{"kind": "run_status", "runStatus": status})
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

// ArchiveGroup 归档(软删): 先 stopAll 再 status=DELETE。
func (s *groupSvc) ArchiveGroup(ctx context.Context, id int64) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	s.stopAll(ctx, id)
	s.mcp.RevokeGroup(id) // 归档即吊销全群 group_send token(spec §17)
	g.Status = consts.DELETE
	logger.Ctx(ctx).Info("group_svc.ArchiveGroup: archived", zap.Int64("groupId", id))
	return group_repo.Group().Update(ctx, g)
}
