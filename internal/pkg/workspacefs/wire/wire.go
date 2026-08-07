// Package wire 定义 agentre ↔ agentred 的 workspacefs.* RPC 协议:参数 / 结果 /
// 错误 sentinel 与 JSON-RPC error code 的双向翻译。daemon 端 handler 与(后续
// 切片的)host 端 svc 共享这一份类型,避免 JSON shape 漂移。
//
// 与 internal/pkg/remotefs/wire 是刻意分开的独立方法族(spec 设计决策 5):
// remotefs.* 面向"浏览远端机器任意绝对路径"(带 $HOME 兜底与路径黑名单);
// workspacefs.* 面向"浏览某个会话已解析出的工作目录"(root 由调用方传入,
// relPath 相对 root)。给 remotefs.* 加字段的话,旧 daemon 会静默忽略新字段
// 并按旧语义应答,版本偏斜就不可见了;新方法族在旧 daemon 上直接回
// -32601(method not found),能明确提示需要升级 daemon。
//
// 命名约定与 internal/pkg/remotefs/wire 一致:
//   - 方法在 "workspacefs.*" 命名空间下
//   - 字段名 lowerCamelCase
//   - 错误码 -32040..-32041 是稳定 wire 值,与既有方法族的 code 段不重叠
//     (remotefs.* 占 -32030..-32035,agentruntime remote wire 占
//     -32010..-32014),wrapGuarded handler 返回 *rpc.Error 由本包翻译,
//     客户端用 FromJSONRPCError rehydrate。
package wire

import (
	"errors"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// ── RPC method names ────────────────────────────────────────────────────────

const (
	MethodListDir     = "workspacefs.listDir"
	MethodGitChanges  = "workspacefs.gitChanges"
	MethodGitBranches = "workspacefs.gitBranches"
)

// ── Error codes ─────────────────────────────────────────────────────────────

const (
	ErrCodePathRefused      = -32040
	ErrCodeBaselineRequired = -32041
)

// ── Sentinel errors ─────────────────────────────────────────────────────────

var (
	// ErrPathRefused 镜像 internal/pkg/workspacefs.ErrPathRefused:relPath 越界
	// (含 ".."、绝对路径,或解析后逃出 root)。
	ErrPathRefused = errors.New("workspacefs: path refused")
	// ErrBaselineRequired 镜像 internal/pkg/workspacefs.ErrBaselineRequired:
	// GitChanges 的 scope=="branch" 时 baseRef 为空。
	ErrBaselineRequired = errors.New("workspacefs: baseline required for branch scope")
)

// ToJSONRPCError 把 workspacefs sentinel 包成 *rpc.Error,daemon handler 返回。
// 非 sentinel 返 nil,调用方应自己包装(ErrInternal 之类)。
func ToJSONRPCError(err error) *rpc.Error {
	switch {
	case errors.Is(err, ErrPathRefused):
		return &rpc.Error{Code: ErrCodePathRefused, Message: err.Error()}
	case errors.Is(err, ErrBaselineRequired):
		return &rpc.Error{Code: ErrCodeBaselineRequired, Message: err.Error()}
	}
	return nil
}

// FromJSONRPCError 反向把 *rpc.Error 翻成 sentinel。未知 code 返原 err。
// host 侧 svc 拿到后再 i18n.NewError(ctx, code.WorkspaceFsXxx) 包给前端。
func FromJSONRPCError(err error) error {
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) {
		return err
	}
	switch rpcErr.Code {
	case ErrCodePathRefused:
		return ErrPathRefused
	case ErrCodeBaselineRequired:
		return ErrBaselineRequired
	}
	return err
}

// ── ListDir ─────────────────────────────────────────────────────────────────

// ListDirReq.Root 是会话已解析出的工作目录绝对路径,由调用方(host 侧 svc)
// 负责解析——协议本身不做 $HOME 兜底,这是与 remotefs.ListDirReq 的关键差异
// (spec 设计决策 2:后端方法的入参是 sessionID 而非 cwd 路径,cwd 解析在 host
// 层完成,daemon 侧收到的已经是解析结果,必填且必须是绝对路径)。
type ListDirReq struct {
	Root           string `json:"root"`
	RelPath        string `json:"relPath,omitempty"`
	IncludeIgnored bool   `json:"includeIgnored,omitempty"`
}

type Entry struct {
	Name       string `json:"name"`
	IsDir      bool   `json:"isDir"`
	Size       int64  `json:"size"`  // 字节;目录恒为 0
	ModTime    int64  `json:"mtime"` // unix seconds
	Symlink    bool   `json:"symlink,omitempty"`
	GitIgnored bool   `json:"gitIgnored,omitempty"` // 非 git 目录恒为 false
}

type ListDirResp struct {
	Path      string  `json:"path"` // = Root 解析 RelPath 后的绝对路径
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated,omitempty"` // 超 maxEntries 时为 true
}

// ── GitChanges ──────────────────────────────────────────────────────────────

// Git 变动 scope 取值,与 internal/pkg/workspacefs.GitChangesScope 的字符串值
// 一一对应(设计决策 8:"未提交 / 本分支"两档)。
const (
	ScopeUncommitted = "uncommitted"
	ScopeBranch      = "branch"
)

// GitChangesReq.BaseRef 仅 Scope==ScopeBranch 时必填;缺失时 daemon 返回
// ErrBaselineRequired。
type GitChangesReq struct {
	Root    string `json:"root"`
	Scope   string `json:"scope"` // ScopeUncommitted | ScopeBranch
	BaseRef string `json:"baseRef,omitempty"`
}

type Change struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"` // 仅 Status=="renamed" 时非空
	Status  string `json:"status"`            // modified | added | deleted | renamed | untracked
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"` // true 时 Added/Deleted 恒为 0
}

type GitChangesResp struct {
	NotARepo  bool     `json:"notARepo,omitempty"` // true 时其余字段恒为零值
	Changes   []Change `json:"changes"`
	Truncated bool     `json:"truncated,omitempty"`
}

// ── GitBranches ─────────────────────────────────────────────────────────────

type GitBranchesReq struct {
	Root string `json:"root"`
}

type Branch struct {
	Name   string `json:"name"` // 短名,如 "main" 或远程跟踪分支的 "origin/main"
	Remote bool   `json:"remote,omitempty"`
}

type GitBranchesResp struct {
	NotARepo bool     `json:"notARepo,omitempty"` // true 时 Branches 恒为空
	Branches []Branch `json:"branches"`
}
