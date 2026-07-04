package chat_svc

import (
	"sync"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// bgRunningSet 是单会话「运行中后台 subagent 的 tool_use_id 集合」。用集合而非计数器：
// add/remove 幂等，杜绝加减泄漏。
type bgRunningSet struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func (s *chatSvc) bgSet(sessionID int64) *bgRunningSet {
	v, _ := s.bgRunning.LoadOrStore(sessionID, &bgRunningSet{ids: map[string]struct{}{}})
	return v.(*bgRunningSet)
}

// addBgRunning 把 ids 加入会话集合，有真正新增时返 true。
func (s *chatSvc) addBgRunning(sessionID int64, ids ...string) bool {
	if sessionID <= 0 || len(ids) == 0 {
		return false
	}
	set := s.bgSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	changed := false
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := set.ids[id]; !ok {
			set.ids[id] = struct{}{}
			changed = true
		}
	}
	return changed
}

// removeBgRunning 移除一个 id，有真正移除时返 true。
func (s *chatSvc) removeBgRunning(sessionID int64, id string) bool {
	if sessionID <= 0 || id == "" {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, ok := set.ids[id]; !ok {
		return false
	}
	delete(set.ids, id)
	return true
}

// clearBgRunning 清空会话集合，原本非空时返 true。
func (s *chatSvc) clearBgRunning(sessionID int64) bool {
	if sessionID <= 0 {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	if len(set.ids) == 0 {
		return false
	}
	set.ids = map[string]struct{}{}
	return true
}

// bgRunningActive 报告会话是否有后台 subagent 在跑（集合非空）。
func (s *chatSvc) bgRunningActive(sessionID int64) bool {
	if sessionID <= 0 {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	return len(set.ids) > 0
}

// runningBgSubagentIDs 从一批已 finalize 的块里挑出「运行中后台 subagent」的父 tool_use_id。
// 判据与前端 background-tasks/derive.ts 同款：SubagentStateBlock.Status=="running" 且其父
// tool_use(ParentToolCallID)入参 run_in_background===true。前台 subagent(无该入参)不纳入。
func runningBgSubagentIDs(finalBlocks []cagoblocks.ContentBlock) []string {
	inputByToolUse := map[string]map[string]any{}
	for _, b := range finalBlocks {
		switch tu := b.(type) {
		case *cagoblocks.ToolUseBlock:
			inputByToolUse[tu.ID] = tu.Input
		case cagoblocks.ToolUseBlock:
			inputByToolUse[tu.ID] = tu.Input
		}
	}
	var out []string
	for _, b := range finalBlocks {
		sb, ok := b.(*blocks.SubagentStateBlock)
		if !ok || sb.Status != "running" || sb.ParentToolCallID == "" {
			continue
		}
		input := inputByToolUse[sb.ParentToolCallID]
		if bg, _ := input["run_in_background"].(bool); bg {
			out = append(out, sb.ParentToolCallID)
		}
	}
	return out
}
