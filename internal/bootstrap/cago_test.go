package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cago-frame/cago/configs"
	"github.com/cago-frame/cago/database/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
)

func TestInitCreatesCagoRuntime(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")

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
	// 默认不开 debug —— debug 日志改由 app_settings 开关控制（见 applyDebugLoggingOnBoot）。
	if cfg.Debug {
		t.Fatal("Debug = true, want false (default)")
	}
	if got := runtime.DataDir(); got != dataDir {
		t.Fatalf("DataDir = %q, want %q", got, dataDir)
	}

	var loggerCfg struct {
		Level   string
		LogFile struct {
			Filename string
		}
	}
	if err := cfg.Scan(context.Background(), "logger", &loggerCfg); err != nil {
		t.Fatalf("Scan(logger) error = %v", err)
	}
	if loggerCfg.Level != "info" {
		t.Fatalf("logger level = %q, want info", loggerCfg.Level)
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

// 回归(dev group-3 并发群轮 SQLITE_BUSY):并发 turn 流式写库时,另一条写
// 0.5ms 即报 database is locked —— 连接没配 busy_timeout。DSN 必须带
// _pragma=busy_timeout(glebarez 驱动 per-connection 生效;启动后 Exec PRAGMA
// 只作用连接池里单个连接,不可用)。用真驱动验证 pragma 实际生效。
func TestSQLiteDSNSetsBusyTimeout(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(sqliteDSN(filepath.Join(t.TempDir(), "x.db"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("DB() error = %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var v int
	if err := gormDB.Raw("PRAGMA busy_timeout").Scan(&v).Error; err != nil {
		t.Fatalf("PRAGMA busy_timeout query error = %v", err)
	}
	if v != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", v)
	}
}

// TestInitIgnoresAGENTREDebugEnv 回归：旧的 AGENTRE_DEBUG 环境变量已被砍掉，
// 改由「设置 → 版本 & 更新 → Debug 日志」开关控制。即使设置该变量，启动也必须
// 保持默认 info 级别、cfg.Debug=false。
func TestInitIgnoresAGENTREDebugEnv(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")
	t.Setenv("AGENTRE_DEBUG", "true") // legacy var must be ignored

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	cfg := runtime.Config()
	if cfg.Debug {
		t.Fatal("Debug = true; AGENTRE_DEBUG must be ignored (use the in-app Debug toggle)")
	}

	var loggerCfg struct{ Level string }
	if err := cfg.Scan(context.Background(), "logger", &loggerCfg); err != nil {
		t.Fatalf("Scan(logger) error = %v", err)
	}
	if loggerCfg.Level != "info" {
		t.Fatalf("logger level = %q, want info", loggerCfg.Level)
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

func TestInitDoesNotResetActiveSessions(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	gdb := db.Default()
	now := time.Now().UnixMilli()
	if err := gdb.Exec(`INSERT INTO chat_sessions (id, agent_id, title, agent_status, status, createtime, updatetime)
VALUES (?, ?, ?, ?, ?, ?, ?)`, 9001, 1, "still running", "running", 1, now, now).Error; err != nil {
		t.Fatalf("insert running session: %v", err)
	}

	runtime2, err := Init(context.Background())
	if err != nil {
		t.Fatalf("second Init() error = %v", err)
	}
	t.Cleanup(runtime2.Close)

	var got string
	if err := db.Default().Table("chat_sessions").Select("agent_status").Where("id = ?", 9001).Scan(&got).Error; err != nil {
		t.Fatalf("load session status: %v", err)
	}
	if got != "running" {
		t.Fatalf("agent_status after Init = %q, want running", got)
	}
}

func TestResetStaleActiveSessionsMarksRunningAndWaitingAsError(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AGENTRE_DATA_DIR", dataDir)
	t.Setenv("AGENTRE_ENV", "test")

	runtime, err := Init(context.Background())
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(runtime.Close)

	gdb := db.Default()
	now := time.Now().UnixMilli()
	rows := []struct {
		id     int64
		status string
	}{
		{9101, "running"},
		{9102, "waiting"},
		{9103, "idle"},
	}
	for _, row := range rows {
		if err := gdb.Exec(`INSERT INTO chat_sessions (id, agent_id, title, agent_status, status, createtime, updatetime)
VALUES (?, ?, ?, ?, ?, ?, ?)`, row.id, 1, row.status, row.status, 1, now, now).Error; err != nil {
			t.Fatalf("insert %s session: %v", row.status, err)
		}
	}

	if err := ResetStaleActiveSessions(context.Background()); err != nil {
		t.Fatalf("ResetStaleActiveSessions() error = %v", err)
	}

	got := map[int64]string{}
	type row struct {
		ID          int64
		AgentStatus string
	}
	var out []row
	if err := db.Default().Table("chat_sessions").Select("id, agent_status").Where("id IN ?", []int64{9101, 9102, 9103}).Scan(&out).Error; err != nil {
		t.Fatalf("load statuses: %v", err)
	}
	for _, row := range out {
		got[row.ID] = row.AgentStatus
	}
	if got[9101] != "error" || got[9102] != "error" || got[9103] != "idle" {
		t.Fatalf("statuses after reset = %#v, want running/waiting error and idle unchanged", got)
	}
}
