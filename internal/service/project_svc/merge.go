package project_svc

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
)

// Merge 见 ProjectSvc 接口注释（R11a）。
//
// 流程：①挑赢家(keep)/输家(drop) ②keep 借 drop 的本机路径(若 keep 自己还没有)
// ③五类引用逐一改挂到 keep ④复用既有 Delete() 软删 drop——Delete 内部的
// HasActiveChildren / CountActiveByProject 守卫在这里等于免费的二次校验：如果
// 上一步漏改了哪一类引用，Delete 会拒绝，合并失败但不留下半改的烂摊子。
func (s *projectSvc) Merge(ctx context.Context, req *MergeProjectsRequest) (*project_entity.Project, error) {
	if req == nil || req.SourceID <= 0 || req.TargetID <= 0 || req.SourceID == req.TargetID {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	a, err := project_repo.Project().Find(ctx, req.SourceID)
	if err != nil {
		return nil, err
	}
	b, err := project_repo.Project().Find(ctx, req.TargetID)
	if err != nil {
		return nil, err
	}
	if a == nil || b == nil {
		return nil, i18n.NewError(ctx, code.ProjectNotFound)
	}

	keep, drop := chooseMergeWinner(a, b)

	// 保留本机项目的本机路径：keep 自己已经配了路径就不动，没配才借 drop 的。
	if keep.LocalPathMissing && !drop.LocalPathMissing {
		keep.Path = drop.Path
		keep.LocalPathMissing = false
	}
	// keep 今天挂在即将消失的 drop 底下——不能留一个指向已消失项目的 parent_id，
	// 改挂到 drop 自己的父级（drop 是顶层时这里落 0，keep 随之升为顶层）。
	if keep.ParentID == drop.ID {
		keep.ParentID = drop.ParentID
	}
	if err := project_repo.Project().Update(ctx, keep); err != nil {
		return nil, err
	}

	if err := reassignSessions(ctx, drop.ID, keep.ID); err != nil {
		return nil, err
	}
	if err := reassignProjectAgents(ctx, drop.ID, keep.ID); err != nil {
		return nil, err
	}
	if err := reassignChildProjects(ctx, drop.ID, keep.ID); err != nil {
		return nil, err
	}
	if err := reassignIssues(ctx, drop.ID, keep.ID); err != nil {
		return nil, err
	}
	if err := reassignProjectLocations(ctx, drop.ID, keep.ID); err != nil {
		return nil, err
	}

	if err := s.Delete(ctx, drop.ID); err != nil {
		return nil, err
	}
	return keep, nil
}

// chooseMergeWinner 决定合并后保留哪一行的身份（R11a）：账号侧已认领的一方优先；
// 两边都认领或都未认领时，沿用先创建的那个的（Createtime 更早）。
func chooseMergeWinner(a, b *project_entity.Project) (keep, drop *project_entity.Project) {
	aClaimed := a.IsClaimed()
	bClaimed := b.IsClaimed()
	if aClaimed != bClaimed {
		if aClaimed {
			return a, b
		}
		return b, a
	}
	if a.Createtime <= b.Createtime {
		return a, b
	}
	return b, a
}

// reassignSessions 把 chat_sessions.project_id 从 dropID 改挂到 keepID。会话没有
// 天然唯一约束要防重——每条会话只属于一个项目，直接整行 Save 即可。
func reassignSessions(ctx context.Context, dropID, keepID int64) error {
	sessions, err := chat_repo.Session().ListByProject(ctx, dropID)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		sess.ProjectID = keepID
		if err := chat_repo.Session().Update(ctx, sess); err != nil {
			return err
		}
	}
	return nil
}

// reassignProjectAgents 把 project_agents 从 dropID 改挂到 keepID，去重：一个
// agent 不能同时是同一个项目的两条成员记录，drop 那条在 keep 已有时直接丢弃。
func reassignProjectAgents(ctx context.Context, dropID, keepID int64) error {
	keepMembers, err := project_repo.ProjectAgent().ListByProject(ctx, keepID)
	if err != nil {
		return err
	}
	keepAgentIDs := make(map[int64]struct{}, len(keepMembers))
	for _, m := range keepMembers {
		keepAgentIDs[m.AgentID] = struct{}{}
	}
	dropMembers, err := project_repo.ProjectAgent().ListByProject(ctx, dropID)
	if err != nil {
		return err
	}
	for _, m := range dropMembers {
		if _, already := keepAgentIDs[m.AgentID]; !already {
			if err := project_repo.ProjectAgent().Add(ctx, keepID, m.AgentID); err != nil {
				return err
			}
		}
		if err := project_repo.ProjectAgent().Remove(ctx, dropID, m.AgentID); err != nil {
			return err
		}
	}
	return nil
}

// reassignChildProjects 把子项目的 projects.parent_id 从 dropID 改挂到 keepID。
func reassignChildProjects(ctx context.Context, dropID, keepID int64) error {
	children, err := project_repo.Project().ListByParent(ctx, dropID)
	if err != nil {
		return err
	}
	for _, c := range children {
		c.ParentID = keepID
		if err := project_repo.Project().Update(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// reassignIssues 把 issues.project_id 从 dropID 改挂到 keepID。
func reassignIssues(ctx context.Context, dropID, keepID int64) error {
	issues, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{ProjectID: dropID})
	if err != nil {
		return err
	}
	for _, iss := range issues {
		iss.ProjectID = keepID
		if err := issue_repo.Issue().Update(ctx, iss); err != nil {
			return err
		}
	}
	return nil
}

// reassignProjectLocations 把 project_locations 从 dropID 改挂到 keepID（R4b 的
// 自然键处理）：两边对同一台 agentred(按指纹)各有一行时不能都改挂到 keepID，
// 否则撞 (project, fingerprint) 自然键——这里保留 keep 已有的那行，drop 的那行
// 直接删除。完整的「按 R4 取胜者 / 按 R5 记录落败者」需要账号级冲突记录基础设施
// （task 7/8/9 的范围，尚未落地）；在此之前用「keep 侧优先」这个确定性规则收敛，
// 保证合并后不残留任何指向已消失项目的行——这是本条唯一被测试覆盖的不变量。
func reassignProjectLocations(ctx context.Context, dropID, keepID int64) error {
	dropLocs, err := project_location_repo.ProjectLocation().ListByProject(ctx, dropID)
	if err != nil {
		return err
	}
	for _, loc := range dropLocs {
		if loc.DaemonFingerprint != "" {
			existing, ferr := project_location_repo.ProjectLocation().FindByProjectAndFingerprint(ctx, keepID, loc.DaemonFingerprint)
			if ferr != nil && !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return ferr
			}
			if existing != nil {
				if err := project_location_repo.ProjectLocation().Delete(ctx, loc.ID); err != nil {
					return err
				}
				continue
			}
		}
		loc.ProjectID = keepID
		if err := project_location_repo.ProjectLocation().Update(ctx, loc); err != nil {
			return err
		}
	}
	return nil
}
