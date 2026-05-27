//go:build !windows

package update_svc

import (
	"fmt"
	"os/exec"
)

// runInstaller 非 Windows 平台不会走 NSIS 安装路径，提供 stub 仅为通过编译。
func runInstaller(exePath string, args ...string) error {
	if output, err := exec.Command(exePath, args...).CombinedOutput(); err != nil { //nolint:gosec
		return fmt.Errorf("run installer failed: %s: %w", string(output), err)
	}
	return nil
}
