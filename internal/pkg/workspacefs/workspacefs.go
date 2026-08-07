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

// DefaultMaxEntries 是单层目录列举的条目上限,与
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
