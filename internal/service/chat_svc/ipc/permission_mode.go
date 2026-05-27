package ipc

import (
	"context"
	"slices"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/code"
)

// ValidatePermissionMode 用 Capabilities().PermissionModeMeta 替代 chat_svc 旧的
// normalizeStoredPermissionMode / validateRequestedPermissionMode 硬编码 switch。
//
// raw=""   → 返回 Caps.PermissionModeMeta.DefaultMode (可能也为"")
// raw 命中 AllowedModes → 原值返回
// raw 不命中 → code.ChatPermissionModeInvalid
//
// chat_svc/chat.go 仍持有 normalizeStoredPermissionMode / validateRequestedPermissionMode
// 同名旧实现(早于此函数),新代码统一走这个 capability 驱动版本。
func ValidatePermissionMode(ctx context.Context, bt agent_backend_entity.BackendType, raw string) (string, error) {
	caps := capabilitiesFor(bt)
	mode := strings.TrimSpace(raw)
	if mode == "" {
		return caps.PermissionModeMeta.DefaultMode, nil
	}
	if slices.Contains(caps.PermissionModeMeta.AllowedModes, mode) {
		return mode, nil
	}
	return "", i18n.NewError(ctx, code.ChatPermissionModeInvalid)
}
