package chat_svc

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/procattr"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/chat_repo"
)

// runGitState 在 cwd 下连跑几条 git 命令汇成 ChatSessionGitState。
// 任意一条 fork 失败时, 不冒泡 error: 把对应字段留 zero, NotARepo 兜底
// 设 true, 让前端整段折叠。这样的容错语义是 by-design —— UI chip 不应该
// 因为 git 异常而挂掉。
func runGitState(ctx context.Context, cwd string) ChatSessionGitState {
	st := ChatSessionGitState{UpdatedAt: time.Now().Unix()}
	if cwd == "" {
		st.NotARepo = true
		return st
	}

	if _, err := gitOutput(ctx, cwd, "rev-parse", "--is-inside-work-tree"); err != nil {
		st.NotARepo = true
		return st
	}

	if br, err := gitOutput(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		st.Branch = strings.TrimSpace(br)
	}

	gitDir, _ := gitOutput(ctx, cwd, "rev-parse", "--git-dir")
	commonDir, _ := gitOutput(ctx, cwd, "rev-parse", "--git-common-dir")
	gitDir, commonDir = strings.TrimSpace(gitDir), strings.TrimSpace(commonDir)
	if gitDir != "" && commonDir != "" && gitDir != commonDir {
		// gitDir 形如 <common>/worktrees/<name>; 取尾段做短名。
		st.Worktree = filepath.Base(gitDir)
	}

	if out, err := gitOutput(ctx, cwd, "status", "--porcelain=v1"); err == nil {
		st.Dirty = countNonEmptyLines(out)
	}

	if out, err := gitOutput(ctx, cwd, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) == 2 {
			st.Behind, _ = strconv.Atoi(parts[0])
			st.Ahead, _ = strconv.Atoi(parts[1])
			st.HasUpstream = true
		}
	}

	return st
}

func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	// args 全部来自本文件内的硬编码 git 子命令,无 user input;binary "git" 固定。
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: controlled args
	procattr.ApplyNoConsoleWindow(cmd)

	cmd.Dir = cwd
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func countNonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// GetSessionGitState 拉某 session 的 git 状态快照。
//   - 本地 backend: 调 runGitState 直接读 cwd。
//   - 远端 backend (claudecode/codex on agentred): MVP 阶段返回 notARepo=true,
//     daemon handler 留 follow-up PR。
func (s *chatSvc) GetSessionGitState(ctx context.Context, req *GetSessionGitStateRequest) (*GetSessionGitStateResponse, error) {
	if req == nil || req.SessionID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	sess, err := chat_repo.Session().Find(ctx, req.SessionID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	if sess == nil {
		return nil, i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	a, err := agent_repo.Agent().Find(ctx, sess.AgentID)
	if err != nil {
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	var be *agent_backend_entity.AgentBackend
	if a != nil && a.AgentBackendID > 0 {
		be, err = agent_backend_repo.AgentBackend().Find(ctx, a.AgentBackendID)
		if err != nil {
			return nil, i18n.NewError(ctx, code.OperationFailed)
		}
	}
	return s.getSessionGitStateForSession(ctx, sess, be)
}

func (s *chatSvc) getSessionGitStateForSession(ctx context.Context, sess *chat_entity.Session, be *agent_backend_entity.AgentBackend) (*GetSessionGitStateResponse, error) {
	if be != nil && be.IsRemote() {
		return notARepoResponse(), nil
	}
	cwd, err := resolveSessionCwd(ctx, sess, be)
	if err != nil || cwd == "" {
		// by-design: cwd 解析失败时 UI chip 不应崩,降级为 notARepo 让前端折叠。
		return notARepoResponse(), nil //nolint:nilerr // 见上方注释
	}
	st := runGitState(ctx, cwd)
	return &GetSessionGitStateResponse{State: st}, nil
}

func notARepoResponse() *GetSessionGitStateResponse {
	return &GetSessionGitStateResponse{State: ChatSessionGitState{
		NotARepo:  true,
		UpdatedAt: time.Now().Unix(),
	}}
}
