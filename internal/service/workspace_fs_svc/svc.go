// Package workspace_fs_svc 是「文件」面板的目录 / Git 两个模式的后端入口:
// 按会话浏览工作目录、取 git 变动与分支清单。
//
// 三个方法的第一个入参一律是 sessionID 而不是 cwd 路径(设计决策 2):会话的
// 工作目录解析本来就是后端职责,且本地 / 远端规则不同;让前端传路径会把那套
// 规则复制一份到前端,并让路径成为可被伪造的入参。
//
// 解析出的 deviceID 决定走哪条路:
//   - deviceID == 0:本机会话,直接在进程内调叶子包 internal/pkg/workspacefs。
//   - deviceID != 0:远端会话,借 remote_device_svc 的租约调 workspacefs.* RPC,
//     daemon 那头调的是同一份叶子包实现(设计决策 4),两端行为不会分叉。
//
// 服务层不碰 DB:会话解析走自声明的窄接口 SessionWorkspaceResolver,由
// chat_svc 在 composition root 注入。
package workspace_fs_svc

//go:generate mockgen -source svc.go -destination mock_workspace_fs_svc/mock_svc.go

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs"
	"github.com/agentre-ai/agentre/internal/pkg/workspacefs/wire"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// SessionWorkspaceResolver 把 sessionID 解析成 {deviceID, cwd}。deviceID 为 0
// 表示本机会话。
//
// 这是本服务**自己**声明的窄接口(ISP + DIP):实现方是 chat_svc —— 它已经持有
// resolveSessionCwd 与 backend 查询;本服务因此不必 import chat_repo /
// agent_repo / agent_backend_repo 去跨域读别人的表,单测也只需要注入一个闭包
// 而不必连 DB。注入模式与 chat_svc.RegisterCwdResolver 一致(设计决策 3)。
type SessionWorkspaceResolver func(ctx context.Context, sessionID int64) (deviceID int64, cwd string, err error)

var resolveWorkspaceFn SessionWorkspaceResolver

// RegisterSessionWorkspaceResolver 由 bootstrap 注入 chat_svc 的实现。
func RegisterSessionWorkspaceResolver(fn SessionWorkspaceResolver) { resolveWorkspaceFn = fn }

// WorkspaceFsSvc 给 Wails 绑定层调。
type WorkspaceFsSvc interface {
	// ListDir 列出会话工作目录下 relPath 这一层(relPath 为空即工作目录本身)。
	ListDir(ctx context.Context, sessionID int64, relPath string, includeIgnored bool) (*ListDirView, error)
	// GitChanges 取 scope 档的 git 变动。baseRef 仅 scope=="branch" 时有意义,
	// 为空或已失效时回落到远端 / 本机推断出的默认基线。
	GitChanges(ctx context.Context, sessionID int64, scope, baseRef string) (*GitChangesView, error)
	// GitBranches 取分支清单 + 当前分支 + 推断出的默认基线。
	GitBranches(ctx context.Context, sessionID int64) (*GitBranchesView, error)
}

// ── views ───────────────────────────────────────────────────────────────────

type EntryView struct {
	Name       string `json:"name"`
	IsDir      bool   `json:"isDir"`
	Size       int64  `json:"size"`
	Mtime      int64  `json:"mtime"` // unix seconds
	Symlink    bool   `json:"symlink"`
	GitIgnored bool   `json:"gitIgnored"`
}

type ListDirView struct {
	Path      string      `json:"path"` // 解析后的绝对路径
	Entries   []EntryView `json:"entries"`
	Truncated bool        `json:"truncated"` // 超单层上限被截断
}

type ChangeView struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath"` // 仅 Status=="renamed" 时非空
	Status  string `json:"status"`  // modified | added | deleted | renamed | untracked
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
}

type GitChangesView struct {
	NotARepo bool `json:"notARepo"`
	// BaseRef 是本次实际比较用的基线。scope=="uncommitted" 时恒为空;
	// scope=="branch" 时为空表示推断不出默认分支,前端据此出「选一个基线」空态。
	BaseRef   string       `json:"baseRef"`
	Changes   []ChangeView `json:"changes"`
	Truncated bool         `json:"truncated"`
}

type BranchView struct {
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
}

type GitBranchesView struct {
	NotARepo        bool         `json:"notARepo"`
	CurrentBranch   string       `json:"currentBranch"`   // detached HEAD 时为空
	DefaultBaseline string       `json:"defaultBaseline"` // 推断不出时为空
	Branches        []BranchView `json:"branches"`
}

// ── impl ────────────────────────────────────────────────────────────────────

var defaultSvc WorkspaceFsSvc = &workspaceFsImpl{}

func Default() WorkspaceFsSvc { return defaultSvc }

type workspaceFsImpl struct {
	// rdSvc / resolver 默认走包级单例;单测注入 mock 与闭包。
	rdSvc    remote_device_svc.RemoteDeviceSvc
	resolver SessionWorkspaceResolver
}

func (s *workspaceFsImpl) deviceSvc() remote_device_svc.RemoteDeviceSvc {
	if s.rdSvc != nil {
		return s.rdSvc
	}
	return remote_device_svc.Default()
}

// workspace 解析会话的 {deviceID, cwd}。cwd 为空(自由会话 / 远端设备上没配
// 项目路径)按「没有工作目录」降级,前端出对应空态。
func (s *workspaceFsImpl) workspace(ctx context.Context, sessionID int64) (int64, string, error) {
	if sessionID <= 0 {
		return 0, "", i18n.NewError(ctx, code.InvalidParameter)
	}
	fn := s.resolver
	if fn == nil {
		fn = resolveWorkspaceFn
	}
	if fn == nil {
		return 0, "", i18n.NewError(ctx, code.WorkspaceFsNoCwd)
	}
	deviceID, cwd, err := fn(ctx, sessionID)
	if err != nil {
		return 0, "", err
	}
	if cwd == "" {
		return 0, "", i18n.NewError(ctx, code.WorkspaceFsNoCwd)
	}
	return deviceID, cwd, nil
}

// call 跑一次远端往返。租约在返回前释放,调用方不持有它。
func (s *workspaceFsImpl) call(ctx context.Context, deviceID int64, method string, req, resp any) error {
	lease, err := s.deviceSvc().Pool().Borrow(ctx, deviceID)
	if err != nil {
		return mapBorrowErr(ctx, err)
	}
	defer lease.Release()
	if cerr := lease.Client().Call(ctx, method, req, resp); cerr != nil {
		return mapCallErr(ctx, cerr)
	}
	return nil
}

func (s *workspaceFsImpl) ListDir(ctx context.Context, sessionID int64, relPath string, includeIgnored bool) (*ListDirView, error) {
	deviceID, cwd, err := s.workspace(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if deviceID == 0 {
		res, lerr := workspacefs.ListDir(ctx, cwd, relPath, includeIgnored, workspacefs.DefaultMaxEntries)
		if lerr != nil {
			return nil, mapLocalErr(ctx, lerr)
		}
		view := &ListDirView{Path: res.Path, Entries: make([]EntryView, len(res.Entries)), Truncated: res.Truncated}
		for i, e := range res.Entries {
			view.Entries[i] = EntryView{
				Name: e.Name, IsDir: e.IsDir, Size: e.Size,
				Mtime: e.ModTime.Unix(), Symlink: e.Symlink, GitIgnored: e.GitIgnored,
			}
		}
		return view, nil
	}

	var resp wire.ListDirResp
	req := wire.ListDirReq{Root: cwd, RelPath: relPath, IncludeIgnored: includeIgnored}
	if cerr := s.call(ctx, deviceID, wire.MethodListDir, req, &resp); cerr != nil {
		return nil, cerr
	}
	view := &ListDirView{Path: resp.Path, Entries: make([]EntryView, len(resp.Entries)), Truncated: resp.Truncated}
	for i, e := range resp.Entries {
		view.Entries[i] = EntryView{
			Name: e.Name, IsDir: e.IsDir, Size: e.Size,
			Mtime: e.ModTime, Symlink: e.Symlink, GitIgnored: e.GitIgnored,
		}
	}
	return view, nil
}

func (s *workspaceFsImpl) GitBranches(ctx context.Context, sessionID int64) (*GitBranchesView, error) {
	deviceID, cwd, err := s.workspace(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.gitBranches(ctx, deviceID, cwd)
}

// gitBranches 是 GitBranches 与「本分支」档基线解析共用的取数步骤。
func (s *workspaceFsImpl) gitBranches(ctx context.Context, deviceID int64, cwd string) (*GitBranchesView, error) {
	if deviceID == 0 {
		res, lerr := workspacefs.GitBranches(ctx, cwd)
		if lerr != nil {
			return nil, mapLocalErr(ctx, lerr)
		}
		view := &GitBranchesView{
			NotARepo:        res.NotARepo,
			CurrentBranch:   res.CurrentBranch,
			DefaultBaseline: res.DefaultBaseline,
			Branches:        make([]BranchView, len(res.Branches)),
		}
		for i, b := range res.Branches {
			view.Branches[i] = BranchView{Name: b.Name, Remote: b.Remote}
		}
		return view, nil
	}

	var resp wire.GitBranchesResp
	if cerr := s.call(ctx, deviceID, wire.MethodGitBranches, wire.GitBranchesReq{Root: cwd}, &resp); cerr != nil {
		return nil, cerr
	}
	view := &GitBranchesView{
		NotARepo:        resp.NotARepo,
		CurrentBranch:   resp.CurrentBranch,
		DefaultBaseline: resp.DefaultBaseline,
		Branches:        make([]BranchView, len(resp.Branches)),
	}
	for i, b := range resp.Branches {
		view.Branches[i] = BranchView{Name: b.Name, Remote: b.Remote}
	}
	return view, nil
}

func (s *workspaceFsImpl) GitChanges(ctx context.Context, sessionID int64, scope, baseRef string) (*GitChangesView, error) {
	sc, err := parseScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	deviceID, cwd, err := s.workspace(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	// 「本分支」档必须带一个确定的基线过去:叶子包对空基线返回
	// ErrBaselineRequired 而不静默兜底,基线的推断与失效回落是本层的职责。
	usedBase := ""
	if sc == workspacefs.ScopeBranch {
		branches, berr := s.gitBranches(ctx, deviceID, cwd)
		if berr != nil {
			return nil, berr
		}
		if branches.NotARepo {
			return &GitChangesView{NotARepo: true, Changes: []ChangeView{}}, nil
		}
		usedBase = pickBaseline(baseRef, branches)
		if usedBase == "" {
			// origin/HEAD / main / master 都不可得:成功返回空基线的空结果,
			// 让前端出「推断不出默认分支,请选一个基线」空态。
			return &GitChangesView{Changes: []ChangeView{}}, nil
		}
	}

	if deviceID == 0 {
		res, lerr := workspacefs.GitChanges(ctx, cwd, sc, usedBase, workspacefs.DefaultMaxEntries)
		if lerr != nil {
			return nil, mapLocalErr(ctx, lerr)
		}
		view := &GitChangesView{
			NotARepo: res.NotARepo, BaseRef: usedBase,
			Changes: make([]ChangeView, len(res.Changes)), Truncated: res.Truncated,
		}
		for i, c := range res.Changes {
			view.Changes[i] = ChangeView{
				Path: c.Path, OldPath: c.OldPath, Status: string(c.Status),
				Added: c.Added, Deleted: c.Deleted, Binary: c.Binary,
			}
		}
		return view, nil
	}

	var resp wire.GitChangesResp
	// scope 已经过 parseScope 校验,必是 wire.Scope* 之一,直接透传原串,不靠
	// "叶子包 scope 与 wire scope 字面量恰好相等"这个隐含前提。
	req := wire.GitChangesReq{Root: cwd, Scope: scope, BaseRef: usedBase}
	if cerr := s.call(ctx, deviceID, wire.MethodGitChanges, req, &resp); cerr != nil {
		return nil, cerr
	}
	view := &GitChangesView{
		NotARepo: resp.NotARepo, BaseRef: usedBase,
		Changes: make([]ChangeView, len(resp.Changes)), Truncated: resp.Truncated,
	}
	for i, c := range resp.Changes {
		view.Changes[i] = ChangeView{
			Path: c.Path, OldPath: c.OldPath, Status: c.Status,
			Added: c.Added, Deleted: c.Deleted, Binary: c.Binary,
		}
	}
	return view, nil
}

// pickBaseline 在「用户选过的基线」与「推断出的默认基线」之间定夺:选过的值
// 必须仍在分支清单里才作数,否则回落到默认推断(设计决策 9 的失效回落)。
// 分支清单同时就是基线选择器的选项集合,本地与远端因此用同一份判定,而不是
// 本地查 ref 存在性、远端另猜一套。
func pickBaseline(baseRef string, branches *GitBranchesView) string {
	if baseRef != "" {
		for _, b := range branches.Branches {
			if b.Name == baseRef {
				return baseRef
			}
		}
	}
	return branches.DefaultBaseline
}

func parseScope(ctx context.Context, scope string) (workspacefs.GitChangesScope, error) {
	switch scope {
	case wire.ScopeUncommitted:
		return workspacefs.ScopeUncommitted, nil
	case wire.ScopeBranch:
		return workspacefs.ScopeBranch, nil
	}
	return "", i18n.NewError(ctx, code.InvalidParameter)
}

// ── error mapping ───────────────────────────────────────────────────────────

// mapLocalErr 翻译本机分支的叶子包 sentinel。
func mapLocalErr(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, workspacefs.ErrPathRefused):
		return i18n.NewError(ctx, code.WorkspaceFsPathRefused)
	case errors.Is(err, workspacefs.ErrBaselineRequired):
		return i18n.NewError(ctx, code.WorkspaceFsBaselineRequired)
	}
	return i18n.NewError(ctx, code.WorkspaceFsReadFailed)
}

func mapBorrowErr(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, remote_device_svc.ErrDeviceNotFound):
		return i18n.NewError(ctx, code.RemoteDeviceNotFound)
	case errors.Is(err, remote_device_svc.ErrDeviceUnauthorized):
		return i18n.NewError(ctx, code.RemoteDeviceUnauthorized)
	}
	return i18n.NewError(ctx, code.WorkspaceFsDeviceOffline)
}

// mapCallErr 翻译远端调用错误。-32601 单独成一类:旧 agentred 根本没有
// workspacefs.* 方法族,那是「升级 daemon」而不是「调用失败」,必须能在 UI 上
// 区分开 —— 这正是新开方法族而不是给 remotefs.* 加字段的原因(设计决策 5)。
func mapCallErr(ctx context.Context, err error) error {
	if isMethodNotFound(err) {
		return i18n.NewError(ctx, code.WorkspaceFsDaemonOutdated)
	}
	switch wire.FromJSONRPCError(err) {
	case wire.ErrPathRefused:
		return i18n.NewError(ctx, code.WorkspaceFsPathRefused)
	case wire.ErrBaselineRequired:
		return i18n.NewError(ctx, code.WorkspaceFsBaselineRequired)
	}
	return i18n.NewError(ctx, code.RemoteRunnerCallFailed)
}

func isMethodNotFound(err error) bool {
	var rpcErr *jsonrpc.Error
	return errors.As(err, &rpcErr) && rpcErr.Code == jsonrpc.ErrMethodNotFound.Code
}
