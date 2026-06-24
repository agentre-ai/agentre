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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
)

// ReplyPrefix 是所有假回复的前缀,前端据此断言并与用户消息区分。
const ReplyPrefix = "e2e-fake-reply: "

// TaskDirectivePrefix 触发建任务卡的用户指令:e2e-task:<assignee显示名>:<title>。
// e2e spec 用它驱动主持人轮确定性建卡(真实场景这是 LLM 的判断,fake 用文本模式顶替)。
const TaskDirectivePrefix = "e2e-task:"

// TaskOpenDirectivePrefix 触发只建 open 任务的用户指令:e2e-task-open:<assignee>:<title>。
// fake 会在 brief 里写入 OpenTaskMarker,成员收到派活后不会自动 complete。
const TaskOpenDirectivePrefix = "e2e-task-open:"

// TaskParentDirectivePrefix 触发带 parentTaskId 的建卡指令:
// e2e-task-parent:<assignee>:<title>:<parentTaskNo>。
const TaskParentDirectivePrefix = "e2e-task-parent:"

// TaskResultPrefix 是 fake 交付任务时 result 的前缀,DB oracle 据此锁定 fake 写入的行。
const TaskResultPrefix = "e2e-fake-result: "

// OpenTaskMarker 标记 create-only 任务,让成员 fake 收到派活抬头时保持 open。
const OpenTaskMarker = "e2e-open-task"

// TaskCompleteEmptyDirectivePrefix 触发空 result complete,用于 e2e 验证服务端软门。
const TaskCompleteEmptyDirectivePrefix = "e2e-task-complete-empty:"

// TaskCancelDirectivePrefix 触发取消任务:e2e-task-cancel:<taskNo>:<reason>。
const TaskCancelDirectivePrefix = "e2e-task-cancel:"

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

// taskAssignedRe 匹配派活消息抬头「任务 #N：」(HandleTaskCreate 的 content 格式;
// 完成回执是「任务 #N 已完成」、取消是「任务 #N 已取消」,编号后无全角冒号,不会误匹配)。
var taskAssignedRe = regexp.MustCompile(`任务 #(\d+)：`)

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
		// 失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "dispatch"); ok {
			if agentName, brief, found := parseTwoPartDirective(req.UserText, OrchestrateDispatchDirectivePrefix); found {
				if err := postToolCall(ctx, spec, "dispatch", map[string]any{
					"agent": agentName,
					"brief": brief,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: orchestrate dispatch failed: %v\n", err)
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
		select {
		case <-ctx.Done():
			return
		case out <- agentruntime.Done{}:
		}
	}()
	return out, result, nil
}

type taskCreateDirective struct {
	assignee     string
	title        string
	brief        string
	parentTaskNo int
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

// parseTaskDirective 从 UserText 中解出 e2e-task:<assignee>:<title>(取指令所在行,
// 缺段/空段 → !ok)。
func parseTaskDirective(text string) (assignee, title string, ok bool) {
	idx := strings.Index(text, TaskDirectivePrefix)
	if idx < 0 {
		return "", "", false
	}
	rest := text[idx+len(TaskDirectivePrefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	assignee, title, found := strings.Cut(rest, ":")
	assignee, title = strings.TrimSpace(assignee), strings.TrimSpace(title)
	if !found || assignee == "" || title == "" {
		return "", "", false
	}
	return assignee, title, true
}

func parseTaskCreateDirective(text string) (taskCreateDirective, bool) {
	if assignee, title, ok := parseTaskDirective(text); ok {
		return taskCreateDirective{assignee: assignee, title: title, brief: "e2e-brief: " + title}, true
	}
	if assignee, title, ok := parseTwoPartDirective(text, TaskOpenDirectivePrefix); ok {
		return taskCreateDirective{assignee: assignee, title: title, brief: OpenTaskMarker + ": " + title}, true
	}
	if assignee, rest, ok := parseTwoPartDirective(text, TaskParentDirectivePrefix); ok {
		title, parentRaw, found := strings.Cut(rest, ":")
		title, parentRaw = strings.TrimSpace(title), strings.TrimSpace(parentRaw)
		parentNo, err := strconv.Atoi(parentRaw)
		if found && title != "" && err == nil && parentNo > 0 {
			return taskCreateDirective{assignee: assignee, title: title, brief: "e2e-brief: " + title, parentTaskNo: parentNo}, true
		}
	}
	return taskCreateDirective{}, false
}

func parseTaskCompleteEmptyDirective(text string) (taskNo int, ok bool) {
	raw, ok := parseOnePartDirective(text, TaskCompleteEmptyDirectivePrefix)
	if !ok {
		return 0, false
	}
	no, err := strconv.Atoi(raw)
	if err != nil || no <= 0 {
		return 0, false
	}
	return no, true
}

func parseTaskCancelDirective(text string) (taskNo int, reason string, ok bool) {
	noRaw, reason, ok := parseTwoPartDirective(text, TaskCancelDirectivePrefix)
	if !ok {
		return 0, "", false
	}
	no, err := strconv.Atoi(noRaw)
	if err != nil || no <= 0 {
		return 0, "", false
	}
	return no, reason, true
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
