//go:build e2e

// Package fake 提供 e2e 专用的确定性 agent runtime:不起任何子进程,按 req.UserText
// 回显一段固定前缀文本后正常结束。仅在 `-tags e2e` 构建中编译,生产二进制不含本包。
package fake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
)

// ReplyPrefix 是所有假回复的前缀,前端据此断言并与用户消息区分。
const ReplyPrefix = "e2e-fake-reply: "

// SystemAssertDirectivePrefix 触发 system prompt 可观测断言:e2e-assert-system:<needle>。
const SystemAssertDirectivePrefix = "e2e-assert-system:"

// WorkflowCreateDirectivePrefix 触发流程管理工具建流程的用户指令:
// e2e-workflow-create:<name>。需 agent 开启 workflow 工具(注入 /mcp/workflow/)。
const WorkflowCreateDirectivePrefix = "e2e-workflow-create:"

// SubagentCallDirectivePrefix 触发调用子 agent 的用户指令:
// e2e-subagent-call:<子agent名>:<交给它的prompt>。需 agent 开启 subagent 工具
// (注入 /mcp/subagent/);agent_call 无审批,同步阻塞到子 agent 跑完返回其文本。
const SubagentCallDirectivePrefix = "e2e-subagent-call:"

// OrgCreateDeptDirectivePrefix 触发组织架构工具建部门的用户指令:
// e2e-org-create-dept:<部门名>。需 agent 开启 org 工具(注入 /mcp/org/);
// org 写工具需用户审批,调用挂起直至 e2e spec 点批准。
const OrgCreateDeptDirectivePrefix = "e2e-org-create-dept:"

// OrchestrateDispatchDirectivePrefix 触发编排引擎派发子任务的用户指令:
// e2e-orch-dispatch:<agentName>:<brief>。需 agent 开启 orchestrate 工具(注入 /mcp/orchestrate/);
// dispatch 异步派发(不阻塞当前 turn),子 agent 在独立会话跑完后报告给 leader。
const OrchestrateDispatchDirectivePrefix = "e2e-orch-dispatch:"

// OrchestrateFinishDirectivePrefix 触发编排引擎完成 Run 的用户指令:
// e2e-orch-finish:<summary>。需 agent 开启 orchestrate 工具(注入 /mcp/orchestrate/);
// finish 把根 Task 标为 done 并把 Run 推进到 done。
const OrchestrateFinishDirectivePrefix = "e2e-orch-finish:"

// OrchestrateAskDirectivePrefix 触发编排 ask 的用户指令:e2e-orch-ask:<agentName>:<question>。
// 需 agent 开启 orchestrate 工具;ask 把带 ask_id 的问题注入对方活会话并阻塞等其 reply。
// 当 <question> 本身又是一条 e2e-orch-ask 指令时,接收方会 ask 回去 → 形成 A↔B 等待环,
// 驱动死锁检测 emit orch:run:deadlock(e2e 死锁场景)。
const OrchestrateAskDirectivePrefix = "e2e-orch-ask:"

// AskUserQuestionDirectivePrefix 触发 AskUserQuestion 失效终态的用户指令:e2e-ask:<question>。
// fake emit 一条 UserAskRequest(单问两选)后直接 Done 而不等回答 → chat_svc finalize 把未答的
// ask 标 expired,前端卡片转「已失效」终态(无需任何工具,纯 runtime 事件)。
const AskUserQuestionDirectivePrefix = "e2e-ask:"

// HookCreateDirectivePrefix 触发脚本 Hook 创作工具的用户指令:e2e-hook-create:<name>。
// 需 agent 开启 hook 工具(注入 /mcp/hook/);hook_create 是写工具需审批,挂起等 UI 批准
// (镜像 org_create_department 接缝)。
const HookCreateDirectivePrefix = "e2e-hook-create:"

// Runtime 实现 agentruntime.Runtime。
type Runtime struct{}

// New 返回一个 fake runtime。
func New() *Runtime { return &Runtime{} }

// Capabilities 返回最小能力集:CapAbort 支撑停止按钮;
// CapMCPTools 让 e2e 的 MCP 工具注入接缝生效(org/subagent/orchestrate 等写工具
// 需要 backend 声明此 cap 才会被注入)。fake 实际消费注入的 MCPServers(调各 tool
// endpoint),但不真正执行 LLM,只回显文本。
func (r *Runtime) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		Set: map[capability.Capability]bool{
			capability.CapAbort:    true,
			capability.CapMCPTools: true,
		},
	}
}

// Run 把 ReplyPrefix+UserText 分片流式发送后 emit Done。
func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	out := make(chan agentruntime.Event, 8)
	result := &agentruntime.RunResult{
		ProviderSessionID: fmt.Sprintf("e2e-fake-%d", req.SessionID),
		Model:             "e2e-fake-model",
	}
	reply := ReplyPrefix + req.UserText
	if needle, ok := parseOnePartDirective(req.UserText, SystemAssertDirectivePrefix); ok {
		if strings.Contains(req.SystemPrompt, needle) {
			reply += "\ne2e-system-ok:" + needle
		} else {
			reply += "\ne2e-system-missing:" + needle
		}
	}
	chunkDelay := configuredChunkDelay()
	go func() {
		defer close(out)
		for i, chunk := range splitChunks(reply, 8) {
			if i > 0 && chunkDelay > 0 {
				timer := time.NewTimer(chunkDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			select {
			case <-ctx.Done():
				return
			case out <- agentruntime.TextDelta{Text: chunk}:
			}
		}
		// 流程工具接缝:agent 开启 workflow 工具时注入 /mcp/workflow/;按 e2e-workflow-create
		// 指令调 workflow_create(挂起等 UI 批准,e2e spec 负责点批准)。失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "workflow_create"); ok {
			if name, found := parseWorkflowCreateDirective(req.UserText); found {
				if err := postToolCall(ctx, spec, "workflow_create", map[string]any{
					"name":    name,
					"content": "e2e-workflow-content: " + name,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: workflow_create failed: %v\n", err)
				}
			}
		}
		// subagent 接缝:agent 开启 subagent 工具时注入 /mcp/subagent/;按指令调 agent_call
		// (无审批,同步阻塞到子 agent 在隔离会话跑完返回其文本)。失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "agent_call"); ok {
			if name, prompt, found := parseTwoPartDirective(req.UserText, SubagentCallDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "agent_call", map[string]any{
					"agent_name": name,
					"prompt":     prompt,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: agent_call failed: %v\n", err)
				}
			}
		}
		// org 接缝:agent 开启 org 工具时注入 /mcp/org/;按指令调 org_create_department
		// (写工具需审批,挂起等 UI 批准,e2e spec 负责点批准)。失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "org_create_department"); ok {
			if name, found := parseOnePartDirective(req.UserText, OrgCreateDeptDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "org_create_department", map[string]any{
					"name": name,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: org_create_department failed: %v\n", err)
				}
			}
		}
		// orchestrate dispatch 接缝:leader agent 开启 orchestrate 工具时注入 /mcp/orchestrate/;
		// 按 e2e-orch-dispatch:<agentName>:<brief> 指令调 dispatch(异步派发,立即返回)。
		// 失败只写 stderr。dispatched 记录本轮是否已派发,使 dispatch 与 ask 互斥(见下)。
		dispatched := false
		if spec, ok := findGroupToolServer(req.MCPServers, "dispatch"); ok {
			if agentName, brief, found := parseTwoPartDirective(req.UserText, OrchestrateDispatchDirectivePrefix); found {
				dispatched = true
				if err := postToolCall(ctx, spec, "dispatch", map[string]any{
					"agent": agentName,
					"brief": brief,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate dispatch failed: %v\n", err)
				}
			}
		}
		// orchestrate ask/reply 接缝(ask 与 reply 同在 /mcp/orchestrate/):
		//   - 提问方轮含 e2e-orch-ask:<agent>:<question> → 调 ask(阻塞等 reply / 超时)。
		//   - 接收方轮收到注入的「【收到提问 ask_id=X】」→ 调 reply 回复;
		//     但若注入的问题体本身又是一条 e2e-orch-ask 指令(死锁场景),则优先 ask 回去,
		//     不 reply → A↔B 互等触发死锁检测。失败只写 stderr。
		// dispatch 与 ask 互斥(一轮只做一个编排动作):否则当 dispatch 的 brief 里又嵌一条
		// e2e-orch-ask 指令(死锁构造)时,本轮会既 dispatch 又对自己 ask,污染等待图。
		if spec, ok := findGroupToolServer(req.MCPServers, "ask"); ok && !dispatched {
			if agentName, question, found := parseTwoPartDirective(req.UserText, OrchestrateAskDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "ask", map[string]any{
					"agent":    agentName,
					"question": question,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate ask failed: %v\n", err)
				}
			} else if askID, found := parseInjectedAskID(req.UserText); found {
				if err := postToolCall(ctx, spec, "reply", map[string]any{
					"ask_id": askID,
					"answer": "e2e-orch-reply: ok",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate reply failed: %v\n", err)
				}
			}
		}
		// hook 接缝:agent 开启 hook 工具时注入 /mcp/hook/;按 e2e-hook-create:<name> 指令调
		// hook_create(写工具需审批,挂起等 UI 批准,镜像 org 接缝)。失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "hook_create"); ok {
			if name, found := parseOnePartDirective(req.UserText, HookCreateDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "hook_create", map[string]any{
					"name":         name,
					"interpreter":  "bash",
					"command":      "echo '{\"events\":[]}'",
					"scheduleExpr": "*/5 * * * *",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: hook_create failed: %v\n", err)
				}
			}
		}
		// orchestrate finish 接缝:按 e2e-orch-finish:<summary> 指令调 finish(收口 Run)。
		// 另外:子任务完成回报续轮时 UserText 包含「【子任务」前缀,自动调 finish 避免 leader
		// 永挂。两者都在同一个 finish tool server 上操作,失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "finish"); ok {
			if summary, found := parseOnePartDirective(req.UserText, OrchestrateFinishDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "finish", map[string]any{
					"summary": summary,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate finish (directive) failed: %v\n", err)
				}
			} else if strings.Contains(req.UserText, "【子任务") {
				// 子任务完成回报续轮:leader 收到「【子任务 #N 完成 · agent#N】\n<text>」→ 自动收口。
				if err := postToolCall(ctx, spec, "finish", map[string]any{
					"summary": "e2e-orchestration-complete",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate finish (auto) failed: %v\n", err)
				}
			}
		}
		// AskUserQuestion 失效终态接缝:e2e-ask:<question> → emit 一条未答的 UserAskRequest,
		// 随后直接 Done。chat_svc finalize 把未答的 ask 标 expired → 卡片转「已失效」。
		if question, found := parseOnePartDirective(req.UserText, AskUserQuestionDirectivePrefix); found {
			select {
			case <-ctx.Done():
				return
			case out <- agentruntime.UserAskRequest{
				RequestID:  fmt.Sprintf("e2e-ask-%d", req.SessionID),
				ToolCallID: fmt.Sprintf("e2e-ask-tc-%d", req.SessionID),
				Questions: []agentruntime.AskQuestion{{
					ID:       "q1",
					Question: question,
					Header:   "e2e",
					Options: []agentruntime.AskOption{
						{Label: "Yes", Description: "yes"},
						{Label: "No", Description: "no"},
					},
				}},
			}:
			}
		}
		select {
		case <-ctx.Done():
			return
		case out <- agentruntime.Done{}:
		}
	}()
	return out, result, nil
}

// parseInjectedAskID 从编排 ask 注入消息「【收到提问 ask_id=<uuid>】…」里解出 ask_id。
// 接收方 fake 据此调 reply。提问方自己的 e2e-orch-ask 指令文本不含 ask_id= → 返回 !ok。
func parseInjectedAskID(text string) (string, bool) {
	const marker = "ask_id="
	idx := strings.Index(text, marker)
	if idx < 0 {
		return "", false
	}
	rest := text[idx+len(marker):]
	if i := strings.IndexAny(rest, "】\n "); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

// findGroupToolServer 返回首个广告 tool 的注入 MCP server(无 → !ok)。
func findGroupToolServer(specs []agentruntime.MCPServerSpec, tool string) (agentruntime.MCPServerSpec, bool) {
	for _, s := range specs {
		if slices.Contains(s.Tools, tool) {
			return s, true
		}
	}
	return agentruntime.MCPServerSpec{}, false
}

func parseOnePartDirective(text, prefix string) (value string, ok bool) {
	idx := strings.Index(text, prefix)
	if idx < 0 {
		return "", false
	}
	rest := text[idx+len(prefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	return rest, true
}

func parseTwoPartDirective(text, prefix string) (first, second string, ok bool) {
	raw, ok := parseOnePartDirective(text, prefix)
	if !ok {
		return "", "", false
	}
	first, second, found := strings.Cut(raw, ":")
	first, second = strings.TrimSpace(first), strings.TrimSpace(second)
	if !found || first == "" || second == "" {
		return "", "", false
	}
	return first, second, true
}

// parseWorkflowCreateDirective 解出 e2e-workflow-create:<name>(取指令所在行;空段 → !ok)。
func parseWorkflowCreateDirective(text string) (name string, ok bool) {
	return parseOnePartDirective(text, WorkflowCreateDirectivePrefix)
}

// postToolCall 对注入的 group MCP server 发一次无状态 tools/call(原 postGroupSend 泛化)。
// handler 的 tools/call 分支无状态,无需先做 initialize 握手。
func postToolCall(ctx context.Context, spec agentruntime.MCPServerSpec, tool string, args map[string]any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": tool, "arguments": args},
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range spec.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected status %d", tool, resp.StatusCode)
	}
	return nil
}

func configuredChunkDelay() time.Duration {
	raw := os.Getenv("AGENTRE_E2E_FAKE_CHUNK_DELAY_MS")
	if raw == "" {
		return 0
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// splitChunks 按 rune 边界把 s 切成最多 n 个 rune 的片段。
func splitChunks(s string, n int) []string {
	if n <= 0 || s == "" {
		return nil
	}
	runes := []rune(s)
	out := make([]string, 0, (len(runes)+n-1)/n)
	for i := 0; i < len(runes); i += n {
		end := i + n
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
