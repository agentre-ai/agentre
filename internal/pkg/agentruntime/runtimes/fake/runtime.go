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

// GroupInviteDirectivePrefix 触发动态招募:e2e-group-invite:<agentName>:<reason>。
const GroupInviteDirectivePrefix = "e2e-group-invite:"

// SystemAssertDirectivePrefix 触发 system prompt 可观测断言:e2e-assert-system:<needle>。
const SystemAssertDirectivePrefix = "e2e-assert-system:"

// GroupCreateDirectivePrefix 触发单聊建群的用户指令:
// e2e-group-create:<title>:<成员名逗号分隔>:<brief>[:<workflowId>]。
// 末段 workflowId 可选(整数,>0 才透传),用于「拉群带流程」绑定。
const GroupCreateDirectivePrefix = "e2e-group-create:"

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

// taskAssignedRe 匹配派活消息抬头「任务 #N：」(HandleTaskCreate 的 content 格式;
// 完成回执是「任务 #N 已完成」、取消是「任务 #N 已取消」,编号后无全角冒号,不会误匹配)。
var taskAssignedRe = regexp.MustCompile(`任务 #(\d+)：`)

// Runtime 实现 agentruntime.Runtime。
type Runtime struct{}

// New 返回一个 fake runtime。
func New() *Runtime { return &Runtime{} }

// Capabilities 返回驱动聊天 + 群聊 UI 的最小能力集:CapAbort 支撑停止按钮;
// CapMCPTools 让群聊门控(group_svc.backendSupportsGroup)放行,否则 e2e 里
// 建群入口没有任何可选 agent。fake 实际忽略注入的 MCPServers(只回显文本)。
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
		// 群成员 turn:像真 CLI 一样,把本轮回复经注入的 group MCP server 调 group_send
		// 冒泡进群转录区(IngestAgentMessage → group_messages)。无注入(单聊 / 非群)→ 跳过,
		// 行为不变。ctx 取消时上面的循环已提前 return,不会走到这里。
		// mentions=["用户"] 回人类来源(ingest 后无 agent 收件人 → 本轮自然收敛,不触发 agent 互投)。
		if spec, ok := findGroupToolServer(req.MCPServers, "group_send"); ok {
			if err := postToolCall(ctx, spec, "group_send", map[string]any{
				"body":     reply,
				"mentions": []string{"用户"},
			}); err != nil {
				// 尽力而为:发失败不报 ErrorEvent(避免误把 backing session 标成出错),
				// 只写日志;群气泡缺失会被 e2e spec 当作显式失败信号抓到。
				fmt.Fprintf(os.Stderr, "fake: group_send failed: %v\n", err)
			}
		}
		// 任务接缝(spec §9):主持人收到 e2e-task 指令 → 建卡派活;成员收到派活抬头 → 交付。
		// 与 group_send 一样尽力而为:失败只写 stderr,缺卡/缺交付由 e2e spec 显式抓红。
		if spec, ok := findGroupToolServer(req.MCPServers, "group_task_create"); ok {
			if task, found := parseTaskCreateDirective(req.UserText); found {
				args := map[string]any{
					"assignee": task.assignee,
					"title":    task.title,
					"brief":    task.brief,
				}
				if task.parentTaskNo > 0 {
					args["parentTaskId"] = task.parentTaskNo
				}
				if err := postToolCall(ctx, spec, "group_task_create", args); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_create failed: %v\n", err)
				}
			}
		}
		if spec, ok := findGroupToolServer(req.MCPServers, "group_task_complete"); ok {
			if no, found := parseTaskCompleteEmptyDirective(req.UserText); found {
				if err := postToolCall(ctx, spec, "group_task_complete", map[string]any{
					"taskId": no, "result": "",
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_complete failed: %v\n", err)
				}
			} else if m := taskAssignedRe.FindStringSubmatch(req.UserText); m != nil && !strings.Contains(req.UserText, OpenTaskMarker) {
				no, _ := strconv.Atoi(m[1])
				if err := postToolCall(ctx, spec, "group_task_complete", map[string]any{
					"taskId": no, "result": TaskResultPrefix + "task #" + m[1],
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_complete failed: %v\n", err)
				}
			}
		}
		if spec, ok := findGroupToolServer(req.MCPServers, "group_task_cancel"); ok {
			if no, reason, found := parseTaskCancelDirective(req.UserText); found {
				if err := postToolCall(ctx, spec, "group_task_cancel", map[string]any{
					"taskId": no, "reason": reason,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_task_cancel failed: %v\n", err)
				}
			}
		}
		if spec, ok := findGroupToolServer(req.MCPServers, "group_invite"); ok {
			if name, reason, found := parseGroupInviteDirective(req.UserText); found {
				if err := postToolCall(ctx, spec, "group_invite", map[string]any{
					"agentNames": []string{name}, "reason": reason,
				}); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_invite failed: %v\n", err)
				}
			}
		}
		// 建群接缝(spec §7.1):单聊注入 group_create 时,按指令调 tool;
		// 该调用会挂起直到用户在 UI 批准(e2e spec 负责点批准);run ctx 取消
		// (停止会话)会同步中断该 HTTP 请求,失败只写 stderr。
		if spec, ok := findGroupToolServer(req.MCPServers, "group_create"); ok {
			if title, members, brief, workflowID, found := parseGroupCreateDirective(req.UserText); found {
				args := map[string]any{"title": title, "memberNames": members, "brief": brief}
				if workflowID > 0 {
					args["workflowId"] = workflowID // 拉群带流程:绑定已建流程
				}
				if err := postToolCall(ctx, spec, "group_create", args); err != nil {
					fmt.Fprintf(os.Stderr, "fake: group_create failed: %v\n", err)
				}
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

func parseGroupInviteDirective(text string) (agentName, reason string, ok bool) {
	return parseTwoPartDirective(text, GroupInviteDirectivePrefix)
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

// parseGroupCreateDirective 解析建群指令(取指令所在行;三段冒号分隔,成员逗号分隔;
// 缺段/空段 → !ok)。title 不支持含冒号(SplitN 三段切分);e2e 指令是测试接缝,
// 标题用时间戳即可。
func parseGroupCreateDirective(text string) (title string, members []string, brief string, workflowID int, ok bool) {
	idx := strings.Index(text, GroupCreateDirectivePrefix)
	if idx < 0 {
		return "", nil, "", 0, false
	}
	rest := text[idx+len(GroupCreateDirectivePrefix):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	// 末段 workflowId 可选:limit 4 让 3 段(无 workflow)与 4 段(带 workflow)都解析,
	// 且不破坏既有 3 段契约(brief 不含冒号,e2e 受控)。
	parts := strings.SplitN(rest, ":", 4)
	if len(parts) < 3 {
		return "", nil, "", 0, false
	}
	title, brief = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[2])
	for _, m := range strings.Split(parts[1], ",") {
		if m = strings.TrimSpace(m); m != "" {
			members = append(members, m)
		}
	}
	if len(parts) == 4 { // 可选 workflowId:非整数/缺省 → 0=不绑定
		if n, err := strconv.Atoi(strings.TrimSpace(parts[3])); err == nil {
			workflowID = n
		}
	}
	if title == "" || len(members) == 0 || brief == "" {
		return "", nil, "", 0, false
	}
	return title, members, brief, workflowID, true
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
