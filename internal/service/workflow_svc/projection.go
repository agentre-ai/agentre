// Package workflow_svc — projection.go：DAG(graph) → 注入 Leader 的散文提示词（唯一真源在 graph）。
package workflow_svc

import (
	"encoding/json"
	"strconv"
	"strings"
)

// FlowNode 流程图节点。Kind: "task"(委派,含 Brief) | "leader"(Leader 自己做)。
type FlowNode struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Brief       string `json:"brief,omitempty"`
	SharedFiles bool   `json:"sharedFiles,omitempty"`
}

// FlowEdge 流程图边。Kind: ""(sequence) | "bounce"(fail 打回)。
type FlowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind,omitempty"`
}

// FlowGraph 流程 DAG（workflows.graph 的 JSON 真源）。
type FlowGraph struct {
	Version int        `json:"version"`
	Nodes   []FlowNode `json:"nodes"`
	Edges   []FlowEdge `json:"edges"`
}

// ParseFlowGraph 解析 graph JSON；空/非法/无节点 → (zero,false)。
func ParseFlowGraph(s string) (FlowGraph, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return FlowGraph{}, false
	}
	var g FlowGraph
	if err := json.Unmarshal([]byte(s), &g); err != nil || len(g.Nodes) == 0 {
		return FlowGraph{}, false
	}
	return g, true
}

// ProjectGraph 把流程 DAG 确定性投影成 (content, outline)。
// content = 注入 Leader 的散文；outline = 各层代表 label（仅展示）。
func ProjectGraph(name string, g FlowGraph) (string, []string) {
	byID := make(map[string]FlowNode, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	seqPreds := map[string][]string{}
	seqSucc := map[string]int{}
	bounceTo := map[string]string{}
	for _, e := range g.Edges {
		if e.Kind == "bounce" {
			bounceTo[e.From] = byID[e.To].Label
			continue
		}
		seqPreds[e.To] = append(seqPreds[e.To], e.From)
		seqSucc[e.From]++
	}

	layer := map[string]int{}
	var depth func(id string, guard int) int
	depth = func(id string, guard int) int {
		if v, ok := layer[id]; ok {
			return v
		}
		best := 0
		if guard < 256 {
			for _, p := range seqPreds[id] {
				if d := depth(p, guard+1) + 1; d > best {
					best = d
				}
			}
		}
		layer[id] = best
		return best
	}
	maxLayer := 0
	for _, n := range g.Nodes {
		if d := depth(n.ID, 0); d > maxLayer {
			maxLayer = d
		}
	}
	layers := make([][]FlowNode, maxLayer+1)
	for _, n := range g.Nodes {
		layers[layer[n.ID]] = append(layers[layer[n.ID]], n)
	}

	var b strings.Builder
	var outline []string
	b.WriteString("# " + name + "\n")
	b.WriteString("You are the Leader. Every result returns to you; you decide the next move.\n")
	step := 0
	for _, grp := range layers {
		if len(grp) == 0 {
			continue
		}
		step++
		num := strconv.Itoa(step)
		if len(grp) == 1 {
			n := grp[0]
			outline = append(outline, n.Label)
			b.WriteString("\n" + num + ". " + singleLine(n, seqSucc[n.ID] == 0) + "\n")
			if to, ok := bounceTo[n.ID]; ok {
				b.WriteString("   On fail → send back to " + to + " (no new node).\n")
			}
			continue
		}
		outline = append(outline, grp[0].Label+" ∥ …")
		b.WriteString("\n" + num + ". In parallel:\n")
		shared := false
		for _, n := range grp {
			brief := n.Brief
			if brief == "" {
				brief = n.Label
			}
			b.WriteString("   - " + n.Label + " — dispatch: " + brief + "\n")
			shared = shared || n.SharedFiles
			if to, ok := bounceTo[n.ID]; ok {
				b.WriteString("     On fail → send back to " + to + " (no new node).\n")
			}
		}
		if shared {
			b.WriteString("   (use isolate=true if they touch the same files)\n")
		}
	}
	return b.String(), outline
}

func singleLine(n FlowNode, isSink bool) string {
	if isSink {
		return n.Label + " — finish with a summary @user."
	}
	if n.Kind == "task" {
		brief := n.Brief
		if brief == "" {
			brief = n.Label
		}
		return "Dispatch to the " + n.Label + " role: " + brief
	}
	if n.Brief != "" {
		return n.Label + " — " + n.Brief
	}
	return n.Label
}
