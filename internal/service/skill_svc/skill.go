package skill_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentskill"
)

// Service 技能包组合服务。依赖通过消费者侧窄接口注入(DIP)。
type Service struct {
	agent   AgentLookup
	backend BackendLookup
	remote  RemoteDiscoverer // 远端 backend 走 daemon 发现;本地 backend 不用
}

type discoveryResult struct {
	backendType agent_backend_entity.BackendType
	packs       []agentskill.SkillPack
}

// discover 拿该 agent backend 的已安装包(无发现器的 backend 为空)。
func (s *Service) discover(ctx context.Context, a *agent_entity.Agent) (discoveryResult, error) {
	be, err := s.backend.Find(ctx, a.AgentBackendID)
	if err != nil || be == nil {
		return discoveryResult{}, err
	}
	backendType := agent_backend_entity.BackendType(be.Type)
	// 远端 backend:技能包装在 daemon 那台机器上,desktop 本地的 claude plugin list
	// 看不到。经 RemoteDiscoverer 走 daemon skills.list 发现(借 device 连接池)。
	if be.IsRemote() {
		deviceID, ok := be.DeviceIDInt()
		if !ok || s.remote == nil {
			return discoveryResult{backendType: backendType, packs: []agentskill.SkillPack{}}, nil
		}
		packs, err := s.remote.ListSkills(ctx, deviceID, be.Type)
		if err != nil {
			return discoveryResult{}, err
		}
		if packs == nil {
			packs = []agentskill.SkillPack{}
		}
		return discoveryResult{backendType: backendType, packs: packs}, nil
	}
	d, ok := agentskill.DiscovererFor(backendType)
	if !ok {
		return discoveryResult{backendType: backendType, packs: []agentskill.SkillPack{}}, nil
	}
	packs, err := d.Discover(ctx, agentskill.DiscoverQuery{
		BackendType: backendType,
		CLIPath:     be.CLIPath,
	})
	return discoveryResult{backendType: backendType, packs: packs}, err
}

// mergeResult 合并后的包列表及对应的 enabled 标注。
type mergeResult struct {
	packs   []agentskill.SkillPack
	enabled []bool
}

// merge 推荐 + 发现 按 id 去重,标注 enabled。
// installed 先入,recommended 后 OR 入 Recommended 旗标。
func merge(recommended, installed []agentskill.SkillPack, enabledIDs []string) mergeResult {
	enabled := map[string]bool{}
	for _, id := range enabledIDs {
		enabled[id] = true
	}
	type entry struct {
		pack agentskill.SkillPack
		idx  int
	}
	byID := map[string]*entry{}
	order := []string{}

	add := func(p agentskill.SkillPack) {
		if ex, ok := byID[p.ID]; ok {
			if p.Recommended {
				ex.pack.Recommended = true
			}
			if p.Installed {
				ex.pack.Installed = true
				ex.pack.Source = agentskill.SourceInstalled
			}
			return
		}
		idx := len(order)
		cp := p
		byID[cp.ID] = &entry{pack: cp, idx: idx}
		order = append(order, cp.ID)
	}

	for _, p := range installed {
		add(p)
	}
	for _, p := range recommended {
		add(p)
	}

	packs := make([]agentskill.SkillPack, len(order))
	enabledFlags := make([]bool, len(order))
	for _, id := range order {
		e := byID[id]
		packs[e.idx] = e.pack
		enabledFlags[e.idx] = enabled[id]
	}
	return mergeResult{packs: packs, enabled: enabledFlags}
}

// ListAgentSkillPacks 合并推荐 + 发现 + agent 授权,产出目录。refresh 预留(未来强制重发现),当前忽略。
func (s *Service) ListAgentSkillPacks(ctx context.Context, agentID int64, _ bool) (SkillCatalogDTO, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return SkillCatalogDTO{}, err
	}
	discovered, err := s.discover(ctx, a)
	if err != nil {
		return SkillCatalogDTO{}, err
	}
	mr := merge(agentskill.RecommendedFor(discovered.backendType), discovered.packs, a.GetEnabledPackIDs())
	dto := make([]SkillPackDTO, 0, len(mr.packs))
	for i, p := range mr.packs {
		dto = append(dto, SkillPackDTO{
			ID:              p.ID,
			Name:            p.Name,
			Description:     p.Description,
			Skills:          p.Skills,
			Source:          string(p.Source),
			Recommended:     p.Recommended,
			Installed:       p.Installed,
			Enabled:         mr.enabled[i],
			GloballyEnabled: p.GloballyEnabled,
		})
	}
	return SkillCatalogDTO{Packs: dto}, nil
}

// EnabledPluginsMap 返回该 agent 的显式覆盖(强制开=true / 强制关=false)。
// 其余(含全局已开但未覆盖)不出现在 map → CLI 沿用全局 enabledPlugins,实现继承。
func (s *Service) EnabledPluginsMap(ctx context.Context, agentID int64) (map[string]bool, error) {
	a, err := s.agent.Find(ctx, agentID)
	if err != nil || a == nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, it := range a.GetSkills() {
		out[it.ID] = it.Enabled
	}
	return out, nil
}
