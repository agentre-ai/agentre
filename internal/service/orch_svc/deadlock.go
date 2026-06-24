package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// detectAskCycle 合并 dispatch 等待边（父等子回报）+ ask 等待边，DFS 找有向环。
// 返回环上的 sessionID 列表。
//
// 边语义（"A 等 B"，即 A → B）：
//   - ask 边：A 发 ask 给 B（A 阻塞等 B 回复）→ A→B
//   - dispatch 边：父任务 P 处于 awaiting-children，等 dispatch 子任务 C 完成 → P→C
//
// 纯 dispatch 边构成 DAG（树形父子），只有 ask 边才能关闭回路。
// 组合图 DFS 可无误报地检出跨层死锁（如 C ask P 且 P await C）。
func (s *orchSvc) detectAskCycle(ctx context.Context, runID int64) ([]int64, bool) {
	// ── 1. 构造邻接表（多边，一个节点可以等多个目标） ──────────────────────
	edges := map[int64][]int64{}

	// ask 边：快照后立即释放锁，避免 DFS 期间持锁。
	s.askMu.Lock()
	for from, to := range s.askWaits {
		edges[from] = append(edges[from], to)
	}
	s.askMu.Unlock()

	// dispatch 边：父任务 P（awaiting-children）→ 非终态 dispatch 子任务 C。
	rows, _ := s.tasks.ListByRun(ctx, runID)
	// 先建 ID→Task 索引，方便 O(1) 查父任务。
	byID := make(map[int64]*orch_entity.Task, len(rows))
	for _, t := range rows {
		byID[t.ID] = t
	}
	for _, c := range rows {
		if c.Kind != orch_entity.TaskKindDispatch || c.IsTerminal() || c.ParentTaskID == 0 {
			continue
		}
		p, ok := byID[c.ParentTaskID]
		if !ok || p.Status != orch_entity.TaskAwaitingChildren {
			continue
		}
		// P 等 C 回报：P.SessionID → C.SessionID
		edges[p.SessionID] = append(edges[p.SessionID], c.SessionID)
	}

	// ── 2. DFS（三色标记：white=0, gray=1, black=2） ──────────────────────
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[int64]int{}
	var path []int64

	var dfs func(n int64) ([]int64, bool)
	dfs = func(n int64) ([]int64, bool) {
		color[n] = gray
		path = append(path, n)

		for _, nxt := range edges[n] {
			switch color[nxt] {
			case gray:
				// 回边 → 环：截取 path 中从 nxt 到当前节点的片段。
				start := 0
				for i, v := range path {
					if v == nxt {
						start = i
						break
					}
				}
				cyc := make([]int64, len(path)-start)
				copy(cyc, path[start:])
				return cyc, true
			case white:
				if cyc, ok := dfs(nxt); ok {
					return cyc, true
				}
			}
			// black：已完全探索，跳过。
		}

		path = path[:len(path)-1]
		color[n] = black
		return nil, false
	}

	// 收集所有节点（含只在目标侧出现的节点）。
	nodes := map[int64]struct{}{}
	for from, tos := range edges {
		nodes[from] = struct{}{}
		for _, to := range tos {
			nodes[to] = struct{}{}
		}
	}
	for n := range nodes {
		if color[n] == white {
			path = path[:0]
			if cyc, ok := dfs(n); ok {
				return cyc, true
			}
		}
	}
	return nil, false
}
