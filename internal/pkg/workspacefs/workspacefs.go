// Package workspacefs 是会话工作目录浏览的叶子包:本机分支与 daemon 侧
// handler(下个切片接入)共用同一份目录列举 / git 变动核心逻辑,避免两端
// 实现分叉。不 import service / repository,只依赖标准库与 os/exec 调 git。
package workspacefs

import (
	"errors"
	"time"
)

// ErrPathRefused 表示请求的相对路径越界(含 ".."、绝对路径,或解析后逃出
// cwd)。错误文案不回显具体原因,避免成为路径探测信道,与
// internal/pkg/remotefs/pathguard 的既有取舍一致。
var ErrPathRefused = errors.New("workspacefs: path refused")

// ErrBaselineRequired 表示调用 GitChanges 时 scope==ScopeBranch 但未提供
// baseline。baseline 的推断/持久化/校验是调用方职责(DefaultBaseline /
// RefExists),GitChanges 本身不对空 baseline 做静默兜底。
var ErrBaselineRequired = errors.New("workspacefs: baseline required for branch scope")

// DefaultMaxEntries 是单层目录列举 / git 变动列举的条目上限,与
// internal/daemon/remotefs/handler.go 的 defaultMaxEntries 保持一致的数值。
const DefaultMaxEntries = 2000

// Entry 是一层目录列举中的一个条目。
type Entry struct {
	Name       string    // 文件/目录名(不含路径)
	IsDir      bool      //
	Size       int64     // 字节;目录恒为 0
	ModTime    time.Time //
	Symlink    bool      // 是否符号链接
	GitIgnored bool      // 是否被 git 忽略;非 git 目录恒为 false
}

// ListDirResult 是 ListDir 的返回结果。
type ListDirResult struct {
	Path      string  // 解析后的绝对路径
	Entries   []Entry //
	Truncated bool    // 超过 maxEntries 时为 true
}

// ChangeStatus 是 git 变动列表中单个文件的状态分类(Git 模式设计的"五类状态":
// 修改 / 新增 / 删除 / 重命名 / 未跟踪)。
type ChangeStatus string

const (
	ChangeModified  ChangeStatus = "modified"
	ChangeAdded     ChangeStatus = "added"
	ChangeDeleted   ChangeStatus = "deleted"
	ChangeRenamed   ChangeStatus = "renamed"
	ChangeUntracked ChangeStatus = "untracked"
)

// Change 是 GitChanges 结果中的一个文件。
type Change struct {
	Path    string       // 当前路径;Status==ChangeRenamed 时是重命名后的新路径
	OldPath string       // 仅 Status==ChangeRenamed 时非空,是重命名前的路径
	Status  ChangeStatus //
	Added   int          // 新增行数;Binary 为 true 时恒为 0
	Deleted int          // 删除行数;Binary 为 true 时恒为 0,未跟踪文件恒为 0(没有"删除"基线可比)
	Binary  bool         // 二进制或超过 1MiB 的文件;为 true 时不显示行数角标
}

// GitChangesScope 选择 GitChanges 计算"未提交档"还是"本分支档"(Git 模式设计
// 决策 8)。
type GitChangesScope string

const (
	// ScopeUncommitted 是"未提交档":工作区(含暂存区)相对 HEAD 的变动。
	ScopeUncommitted GitChangesScope = "uncommitted"
	// ScopeBranch 是"本分支档":工作区相对 baseline 与 HEAD 的 merge-base 的
	// 变动,已提交与未提交的改动一并计入。
	ScopeBranch GitChangesScope = "branch"
)

// GitChangesResult 是 GitChanges 的返回结果。
type GitChangesResult struct {
	NotARepo  bool     // dir 不在任何 git 工作树内;为 true 时其余字段恒为零值
	Changes   []Change //
	Truncated bool     // 超过 maxEntries 时为 true
}

// Branch 是 GitBranches 结果中的一项。
type Branch struct {
	Name   string // 短名,如 "main" 或远程跟踪分支的 "origin/main"
	Remote bool   // 是否远程跟踪分支
}

// GitBranchesResult 是 GitBranches 的返回结果。
type GitBranchesResult struct {
	NotARepo bool     // dir 不在任何 git 工作树内;为 true 时 Branches 恒为空
	Branches []Branch //
}
