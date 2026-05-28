package chat_svc

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
)

// runGit 在 dir 下执行 git args。测试 helper, 失败直接 t.Fatal。
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: test helper, args 来自测试内常量
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRunGitState_HappyPath(t *testing.T) {
	Convey("Given a temp git repo on branch ai-chat with 2 dirty files", t, func() {
		dir := t.TempDir()
		runGit(t, dir, "init", "-q", "-b", "ai-chat")
		runGit(t, dir, "config", "user.email", "t@t")
		runGit(t, dir, "config", "user.name", "t")
		runGit(t, dir, "commit", "--allow-empty", "-m", "init")

		// 制造两个 untracked 文件 (dir 来自 t.TempDir, 路径可控)
		_ = exec.Command("touch", filepath.Join(dir, "a.txt")).Run() //nolint:gosec // G204: test, path 来自 TempDir
		_ = exec.Command("touch", filepath.Join(dir, "b.txt")).Run() //nolint:gosec // G204: test, path 来自 TempDir

		Convey("When runGitState executes", func() {
			st := runGitState(context.Background(), dir)
			So(st.NotARepo, ShouldBeFalse)
			So(st.Branch, ShouldEqual, "ai-chat")
			So(st.Dirty, ShouldEqual, 2)
			So(st.HasUpstream, ShouldBeFalse) // 没 push 过, 无 upstream
		})
	})
}

func TestRunGitState_NotARepo(t *testing.T) {
	Convey("Given a plain empty dir (not a git repo)", t, func() {
		dir := t.TempDir()
		Convey("When runGitState executes", func() {
			st := runGitState(context.Background(), dir)
			So(st.NotARepo, ShouldBeTrue)
		})
	})
}

func TestRunGitState_Worktree(t *testing.T) {
	Convey("Given a repo with an attached worktree", t, func() {
		main := t.TempDir()
		runGit(t, main, "init", "-q", "-b", "main")
		runGit(t, main, "config", "user.email", "t@t")
		runGit(t, main, "config", "user.name", "t")
		runGit(t, main, "commit", "--allow-empty", "-m", "init")
		wt := filepath.Join(t.TempDir(), "wt-feat")
		runGit(t, main, "worktree", "add", "-b", "feat", wt)

		Convey("When runGitState runs inside the worktree", func() {
			st := runGitState(context.Background(), wt)
			So(st.Branch, ShouldEqual, "feat")
			So(st.Worktree, ShouldNotEqual, "") // 非主仓 → 非空
		})
	})
}

func TestGetSessionGitState_LocalBackend(t *testing.T) {
	Convey("Given a local-backend session whose cwd resolves to a real git repo", t, func() {
		dir := t.TempDir()
		runGit(t, dir, "init", "-q", "-b", "main")
		runGit(t, dir, "config", "user.email", "t@t")
		runGit(t, dir, "config", "user.name", "t")
		runGit(t, dir, "commit", "--allow-empty", "-m", "init")

		sess := &chat_entity.Session{ID: 42, ProjectID: 0}
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeBuiltin)}
		// 用 stub 把 resolveSessionCwd 绕到 dir
		RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
			return dir, nil
		})
		t.Cleanup(func() { RegisterCwdResolver(nil) })

		Convey("When GetSessionGitState is called", func() {
			s := &chatSvc{}
			resp, err := s.getSessionGitStateForSession(context.Background(), sess, be)
			So(err, ShouldBeNil)
			So(resp.State.Branch, ShouldEqual, "main")
		})
	})
}

func TestGetSessionGitState_RemoteBackend_StubsNotARepo(t *testing.T) {
	Convey("Given a remote backend session (MVP not wired through daemon yet)", t, func() {
		sess := &chat_entity.Session{ID: 42, ProjectID: 1}
		be := &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "dev-1"}
		Convey("Then service returns notARepo=true without erroring", func() {
			s := &chatSvc{}
			resp, err := s.getSessionGitStateForSession(context.Background(), sess, be)
			So(err, ShouldBeNil)
			So(resp.State.NotARepo, ShouldBeTrue)
		})
	})
}

func TestGetSessionGitState_SessionNotFound(t *testing.T) {
	Convey("Given req.SessionID = 0", t, func() {
		s := &chatSvc{}
		_, err := s.GetSessionGitState(context.Background(), &GetSessionGitStateRequest{SessionID: 0})
		So(err, ShouldNotBeNil)
	})
}
