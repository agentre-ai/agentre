package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cago-frame/cago/configs"
	"github.com/cago-frame/cago/database/db"

	"agentre/internal/repository/project_location_repo"
)

func TestInitCreatesCagoRuntime(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")
	t.Setenv("AGENTRE_DEBUG", "true")

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	cfg := runtime.Config()
	if cfg == nil {
		t.Fatal("Config() returned nil")
	}
	if cfg.AppName != "agentre" {
		t.Fatalf("AppName = %q, want agentre", cfg.AppName)
	}
	if cfg.Env != configs.TEST {
		t.Fatalf("Env = %q, want %q", cfg.Env, configs.TEST)
	}
	if !cfg.Debug {
		t.Fatal("Debug = false, want true")
	}
	if got := runtime.DataDir(); got != dataDir {
		t.Fatalf("DataDir = %q, want %q", got, dataDir)
	}

	var loggerCfg struct {
		LogFile struct {
			Filename string
		}
	}
	if err := cfg.Scan(context.Background(), "logger", &loggerCfg); err != nil {
		t.Fatalf("Scan(logger) error = %v", err)
	}
	wantLog := filepath.Join(dataDir, "logs", "agentre.log")
	if loggerCfg.LogFile.Filename != wantLog {
		t.Fatalf("log filename = %q, want %q", loggerCfg.LogFile.Filename, wantLog)
	}

	// SQLite 文件已被创建在 dataDir 下，且 gormigrate 跟踪表存在
	dbPath := filepath.Join(dataDir, "agentre.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("sqlite file %s missing: %v", dbPath, err)
	}

	gormDB := db.Default()
	if gormDB == nil {
		t.Fatal("db.Default() returned nil")
	}
	if !gormDB.Migrator().HasTable("migrations") {
		t.Fatal("gormigrate 'migrations' tracking table not created")
	}

	if _, err := os.Stat(filepath.Join(dataDir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config.json should not be created, stat error = %v", err)
	}
}

func TestSeedCEOAgent(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	gdb := db.Default()
	var count int64
	if err := gdb.Table("agents").Where("system_badge = ?", "DEFAULT").Count(&count).Error; err != nil {
		t.Fatalf("count CEO agent: %v", err)
	}
	if count != 1 {
		t.Fatalf("CEO agent count = %d, want 1", count)
	}

	var deptCount int64
	if err := gdb.Table("departments").Count(&deptCount).Error; err != nil {
		t.Fatalf("count departments: %v", err)
	}
	if deptCount != 0 {
		t.Fatalf("departments count = %d, want 0 (no default seed)", deptCount)
	}
}

// TestInitRegistersProjectLocationRepo 回归：远端 backend 拉 cwd 时会走
// project_location_repo.ProjectLocation()；bootstrap 漏注册会导致前端只看到
// 「Agent 调用失败：project_location_repo not registered」。
func TestInitRegistersProjectLocationRepo(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")

	prev := project_location_repo.ProjectLocation()
	project_location_repo.RegisterProjectLocation(nil)
	t.Cleanup(func() { project_location_repo.RegisterProjectLocation(prev) })

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	if project_location_repo.ProjectLocation() == nil {
		t.Fatal("project_location_repo.ProjectLocation() = nil after Init; bootstrap forgot to RegisterProjectLocation")
	}
}
