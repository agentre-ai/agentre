// Package cliprober 把「按类型在 $PATH 中找 CLI binary」与「fork CLI 子进程跑一轮 ping」
// 抽到一个无 GORM、无 cago db、无 entity 依赖的纯函数包。
// 主进程 agent_backend_svc 与 daemon handlers 共用,让远端 device 也能复用同一套 prober 逻辑。
package cliprober

import (
	"errors"
	"strings"

	"agentre/internal/pkg/clienv"
)

// ErrInvalidType 调用方传了 cliprober 不识别的 type 字面量。
var ErrInvalidType = errors.New("cliprober: invalid backend type")

// 与 agent_backend_entity.TypeClaudeCode / TypeCodex 一致;
// 此处用字面量是为了把 cliprober 与 entity 解耦。
var cliBinaryForType = map[string]string{
	"claudecode": "claude",
	"codex":      "codex",
}

// ResolveCLIPath 在本机 $PATH 中查找 type 对应 binary 的绝对路径。
//
// 行为:
//   - type 不在 claudecode / codex 范围 → ErrInvalidType
//   - 找到 → (path, true, nil)
//   - 找不到 → ("", false, nil)(非错误 —— 让调用方决定是否提示用户)
//
// 路径搜索委托 clienv.ResolveBinary:跳过 .app/Contents/ wrapper、用增强 PATH 等。
func ResolveCLIPath(backendType string) (string, bool, error) {
	binary, ok := cliBinaryForType[strings.TrimSpace(backendType)]
	if !ok {
		return "", false, ErrInvalidType
	}
	path, found := clienv.ResolveBinary(binary)
	return path, found, nil
}
