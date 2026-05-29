package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOSLabel(t *testing.T) {
	cases := []struct {
		name              string
		goos, goarch, ver string
		want              string
	}{
		{"darwin arm64 with version", "darwin", "arm64", "14.6", "macOS 14.6 (Apple Silicon)"},
		{"darwin amd64 with version", "darwin", "amd64", "13.2", "macOS 13.2 (Intel)"},
		{"darwin no version", "darwin", "arm64", "", "macOS (Apple Silicon)"},
		{"windows with version", "windows", "amd64", "10.0.19045", "Windows 10.0.19045 (amd64)"},
		{"windows no version", "windows", "amd64", "", "Windows (amd64)"},
		{"linux with pretty name", "linux", "amd64", "Ubuntu 22.04.1 LTS", "Ubuntu 22.04.1 LTS (amd64)"},
		{"linux no version", "linux", "arm64", "", "Linux (arm64)"},
		{"unknown os", "plan9", "amd64", "", "plan9 (amd64)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := osLabel(c.goos, c.goarch, c.ver); got != c.want {
				t.Fatalf("osLabel(%q, %q, %q) = %q, want %q", c.goos, c.goarch, c.ver, got, c.want)
			}
		})
	}
}

func TestGetBugReportInfoWiring(t *testing.T) {
	got := (&App{}).GetBugReportInfo()
	if got.OS != runtime.GOOS {
		t.Fatalf("OS = %q, want %q", got.OS, runtime.GOOS)
	}
	if got.Arch != runtime.GOARCH {
		t.Fatalf("Arch = %q, want %q", got.Arch, runtime.GOARCH)
	}
	if got.OSLabel == "" {
		t.Fatal("OSLabel is empty, want a non-empty label")
	}
}

// TestOpenLogsDirCreatesAndOpens 验证「打开日志」会建好日志目录并交给系统打开命令。
func TestOpenLogsDirCreatesAndOpens(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)

	var opened string
	prev := runOpenCmd
	runOpenCmd = func(name string, args ...string) error {
		if len(args) > 0 {
			opened = args[len(args)-1]
		}
		return nil
	}
	t.Cleanup(func() { runOpenCmd = prev })

	if err := (&App{}).OpenLogsDir(); err != nil {
		t.Fatalf("OpenLogsDir() error = %v", err)
	}

	logsDir := filepath.Join(dataDir, "logs")
	if _, err := os.Stat(logsDir); err != nil {
		t.Fatalf("logs dir not created: %v", err)
	}
	if opened != logsDir {
		t.Fatalf("opened path = %q, want %q", opened, logsDir)
	}
}
