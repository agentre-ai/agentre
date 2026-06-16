//go:build !windows

package procattr

import "os/exec"

func ApplyNoConsoleWindow(cmd *exec.Cmd) {}
