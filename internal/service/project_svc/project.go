package project_svc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/model/entity/project_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/procattr"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/repository/project_repo"
)

// ProjectSvc Project 模块的应用服务。
type ProjectSvc interface {
	Create(ctx context.Context, req *CreateProjectRequest) (*project_entity.Project, error)
	Update(ctx context.Context, req *UpdateProjectRequest) (*project_entity.Project, error)
	Reorder(ctx context.Context, req *ReorderProjectsRequest) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*ProjectDetail, error)
	ListTree(ctx context.Context) ([]*ProjectNode, error)
	AddMember(ctx context.Context, projectID, agentID int64) error
	RemoveMember(ctx context.Context, projectID, agentID int64) error
	ListSessions(ctx context.Context, projectID int64) ([]*chat_entity.Session, error)
	DetectGitRepo(ctx context.Context, path string) (*GitRepoInfo, error)

	// cwd
	ResolveSessionCwd(ctx context.Context, session *chat_entity.Session) (string, error)
	ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error)
}

type projectSvc struct {
	now func() int64
}

var defaultProject ProjectSvc = &projectSvc{now: func() int64 { return time.Now().UnixMilli() }}

// Default 取默认服务单例。
func Default() ProjectSvc { return defaultProject }

// SetDefault 注入服务实现（测试用 / bootstrap 替换 stub git client 时用）。
func SetDefault(svc ProjectSvc) { defaultProject = svc }

// New 构造默认实现。
func New() ProjectSvc {
	return &projectSvc{now: func() int64 { return time.Now().UnixMilli() }}
}

// ──────────────────────────────────────────────────────────────────────────────
// CRUD
// ──────────────────────────────────────────────────────────────────────────────

func (s *projectSvc) Create(ctx context.Context, req *CreateProjectRequest) (*project_entity.Project, error) {
	now := s.now()
	p := &project_entity.Project{
		ParentID:    req.ParentID,
		Name:        strings.TrimSpace(req.Name),
		Icon:        strings.TrimSpace(req.Icon),
		Color:       strings.TrimSpace(req.Color),
		Description: strings.TrimSpace(req.Description),
		Path:        strings.TrimSpace(req.Path),
		Status:      consts.ACTIVE,
		Createtime:  now,
		Updatetime:  now,
	}
	if err := p.Check(ctx); err != nil {
		return nil, err
	}
	// 路径必须存在 —— 避免用户填错路径后 cwd 解析时才发现。
	if _, err := os.Stat(p.Path); err != nil {
		return nil, i18n.NewError(ctx, code.ProjectPathNotExist)
	}
	// 父项目存在且 active。
	if p.ParentID > 0 {
		parent, err := project_repo.Project().Find(ctx, p.ParentID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, i18n.NewError(ctx, code.ProjectParentNotFound)
		}
		if !parent.IsActive() {
			return nil, i18n.NewError(ctx, code.ProjectParentInactive)
		}
	}
	// 同级名字唯一。
	dup, err := project_repo.Project().FindByName(ctx, p.ParentID, p.Name)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.ProjectNameDuplicated)
	}
	next, err := project_repo.Project().NextSortOrder(ctx, p.ParentID)
	if err != nil {
		return nil, err
	}
	p.SortOrder = next

	if err := project_repo.Project().Create(ctx, p); err != nil {
		return nil, err
	}

	// 初始成员 —— 失败不回滚（用户可以在设置里再加），但记日志。
	for _, agentID := range req.InitialAgentIDs {
		if agentID <= 0 {
			continue
		}
		if err := project_repo.ProjectAgent().Add(ctx, p.ID, agentID); err != nil {
			logger.Ctx(ctx).Warn("project_svc.Create: initial agent add failed",
				zap.Int64("projectId", p.ID),
				zap.Int64("agentId", agentID), zap.Error(err))
		}
	}
	return p, nil
}

func (s *projectSvc) Update(ctx context.Context, req *UpdateProjectRequest) (*project_entity.Project, error) {
	existing, err := project_repo.Project().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, i18n.NewError(ctx, code.ProjectNotFound)
	}
	newName := strings.TrimSpace(req.Name)
	if newName != existing.Name {
		dup, err := project_repo.Project().FindByName(ctx, existing.ParentID, newName)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != existing.ID {
			return nil, i18n.NewError(ctx, code.ProjectNameDuplicated)
		}
	}
	existing.Name = newName
	existing.Icon = strings.TrimSpace(req.Icon)
	existing.Color = strings.TrimSpace(req.Color)
	existing.Description = strings.TrimSpace(req.Description)
	if err := existing.Check(ctx); err != nil {
		return nil, err
	}
	if err := project_repo.Project().Update(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *projectSvc) Reorder(ctx context.Context, req *ReorderProjectsRequest) error {
	if req == nil || req.ParentID < 0 || len(req.OrderedIDs) == 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	siblings, err := project_repo.Project().ListByParent(ctx, req.ParentID)
	if err != nil {
		return err
	}
	if len(siblings) != len(req.OrderedIDs) {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	allowed := make(map[int64]struct{}, len(siblings))
	for _, p := range siblings {
		allowed[p.ID] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(req.OrderedIDs))
	for _, id := range req.OrderedIDs {
		if id <= 0 {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := allowed[id]; !ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		if _, ok := seen[id]; ok {
			return i18n.NewError(ctx, code.InvalidParameter)
		}
		seen[id] = struct{}{}
	}
	return project_repo.Project().ReorderSiblings(ctx, req.ParentID, req.OrderedIDs)
}

func (s *projectSvc) Delete(ctx context.Context, id int64) error {
	existing, err := project_repo.Project().Find(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return i18n.NewError(ctx, code.ProjectNotFound)
	}
	hasChildren, err := project_repo.Project().HasActiveChildren(ctx, id)
	if err != nil {
		return err
	}
	if hasChildren {
		return i18n.NewError(ctx, code.ProjectHasChildren)
	}
	// 还有 running / waiting 会话时拒绝；idle / error 等允许（用户主动归档）。
	n, err := chat_repo.Session().CountActiveByProject(ctx, id, []string{"running", "waiting"})
	if err != nil {
		return err
	}
	if n > 0 {
		return i18n.NewError(ctx, code.ProjectHasActiveSessions)
	}
	return project_repo.Project().Delete(ctx, id)
}

// ──────────────────────────────────────────────────────────────────────────────
// Read / Tree
// ──────────────────────────────────────────────────────────────────────────────

func (s *projectSvc) Get(ctx context.Context, id int64) (*ProjectDetail, error) {
	p, err := project_repo.Project().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, i18n.NewError(ctx, code.ProjectNotFound)
	}
	direct, inherited, err := s.aggregateMembers(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := hydrateMemberAgents(ctx, direct, inherited); err != nil {
		return nil, err
	}
	return &ProjectDetail{Project: p, DirectMembers: direct, InheritedMembers: inherited}, nil
}

func (s *projectSvc) ListTree(ctx context.Context) ([]*ProjectNode, error) {
	rows, err := project_repo.Project().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*ProjectNode, len(rows))
	roots := make([]*ProjectNode, 0)
	for _, p := range rows {
		byID[p.ID] = &ProjectNode{Project: p}
	}
	for _, p := range rows {
		node := byID[p.ID]
		if p.ParentID == 0 {
			roots = append(roots, node)
			continue
		}
		parent, ok := byID[p.ParentID]
		if !ok {
			// 父项目被软删 / 不存在 —— 当顶层挂出，避免「漂浮」节点丢失。
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	return roots, nil
}

func (s *projectSvc) ListSessions(ctx context.Context, projectID int64) ([]*chat_entity.Session, error) {
	if projectID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	return chat_repo.Session().ListByProject(ctx, projectID)
}

// ──────────────────────────────────────────────────────────────────────────────
// Members
// ──────────────────────────────────────────────────────────────────────────────

func (s *projectSvc) AddMember(ctx context.Context, projectID, agentID int64) error {
	if projectID <= 0 || agentID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	p, err := project_repo.Project().Find(ctx, projectID)
	if err != nil {
		return err
	}
	if p == nil {
		return i18n.NewError(ctx, code.ProjectNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, agentID)
	if err != nil {
		return err
	}
	if a == nil {
		return i18n.NewError(ctx, code.ProjectAgentNotFound)
	}
	return project_repo.ProjectAgent().Add(ctx, projectID, agentID)
}

func (s *projectSvc) RemoveMember(ctx context.Context, projectID, agentID int64) error {
	if projectID <= 0 || agentID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return project_repo.ProjectAgent().Remove(ctx, projectID, agentID)
}

// ──────────────────────────────────────────────────────────────────────────────
// Git detect（轻量探测，不进入热路径）
// ──────────────────────────────────────────────────────────────────────────────

// DetectGitRepo 探测 path 是否 git 仓库，返回当前分支 / origin。
// 新建项目模态用 —— 用户选完目录后立刻探测一次。git 子命令失败（无 origin / detached HEAD）
// 不算硬错，只是少填字段；只在 Stat 出错时返回 IsGitRepo=false。
//
// path 是用户从「目录选择器」拿到的字符串 —— 已限定为本地路径，不会被远端 inject。
// gosec G204 在这里属于误报，但仍把 path 转成 absolute 再透传，避免 git 解释成
// option（如 --no-pager）。
func (s *projectSvc) DetectGitRepo(ctx context.Context, path string) (*GitRepoInfo, error) {
	path = strings.TrimSpace(path)
	out := &GitRepoInfo{}
	if path == "" {
		return out, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return out, nil //nolint:nilerr // 路径非法对 UI 是"不是 git 仓库"等价信号
	}
	if _, err := os.Stat(filepath.Join(abs, ".git")); err != nil {
		return out, nil //nolint:nilerr // 同上，缺 .git 等价"非 git 仓库"
	}
	out.IsGitRepo = true
	branchCmd := exec.CommandContext( //nolint:gosec // abs 来自本地目录选择器
		ctx, "git", "-C", abs, "rev-parse", "--abbrev-ref", "HEAD")
	procattr.ApplyNoConsoleWindow(branchCmd)
	if branch, err := branchCmd.Output(); err == nil {
		out.CurrentBranch = strings.TrimSpace(string(branch))
	}
	originCmd := exec.CommandContext( //nolint:gosec // 同上
		ctx, "git", "-C", abs, "remote", "get-url", "origin")
	procattr.ApplyNoConsoleWindow(originCmd)
	if origin, err := originCmd.Output(); err == nil {
		out.Origin = strings.TrimSpace(string(origin))
	}
	return out, nil
}
