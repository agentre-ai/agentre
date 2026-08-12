package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serviceCommandCall struct {
	name string
	args []string
}

type fakeServiceCommandRunner struct {
	calls   []serviceCommandCall
	results []fakeServiceCommandResult
}

type fakeServiceCommandResult struct {
	output string
	err    error
}

func (f *fakeServiceCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, serviceCommandCall{name: name, args: append([]string(nil), args...)})
	if len(f.results) == 0 {
		return nil, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return []byte(result.output), result.err
}

func TestGivenSystemdManagerWhenInstallingThenItWritesUserUnitAndEnablesStartup(t *testing.T) {
	home := t.TempDir()
	runner := &fakeServiceCommandRunner{}
	manager := newSystemdServiceManager(serviceManagerConfig{
		BinaryPath: "/opt/Agentre % Tools/agentred",
		DataDir:    "/home/alice/.config/agentred 100%",
		HomeDir:    home,
		UserName:   "alice",
		Runner:     runner,
	})

	status, err := manager.Install(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{Installed: true}, status)

	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	body, err := os.ReadFile(unitPath) //nolint:gosec // unitPath is assembled under the test's temporary home directory.
	require.NoError(t, err)
	unit := string(body)
	assert.Contains(t, unit, `ExecStart="/opt/Agentre %% Tools/agentred" run`)
	assert.Contains(t, unit, `Environment="AGENTRED_DATA_DIR=/home/alice/.config/agentred 100%%"`)
	assert.Contains(t, unit, "WantedBy=default.target")
	assert.Equal(t, []serviceCommandCall{
		{name: "systemctl", args: []string{"--user", "daemon-reload"}},
		{name: "systemctl", args: []string{"--user", "enable", "agentred.service"}},
		{name: "loginctl", args: []string{"enable-linger", "alice"}},
	}, runner.calls)
}

func TestGivenLingerPolicyRejectsWhenSystemdServiceInstallsThenInstallSucceedsWithRepairCommand(t *testing.T) {
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{}, {}, {err: errors.New("access denied")},
	}}
	manager := newSystemdServiceManager(serviceManagerConfig{
		BinaryPath: "/usr/local/bin/agentred",
		DataDir:    "/home/alice/.config/agentred",
		HomeDir:    t.TempDir(),
		UserName:   "alice",
		Runner:     runner,
	})

	status, err := manager.Install(context.Background())
	require.NoError(t, err, "host linger policy must not undo successful service registration")
	assert.True(t, status.Installed)
	assert.Contains(t, strings.Join(status.Details, "\n"), "loginctl enable-linger alice")
	assert.Contains(t, strings.Join(status.Details, "\n"), "access denied")
}

func TestGivenSystemdUnitWhenInspectingAndManagingThenCommandsAndStatesAreStable(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "active\n"},
		{}, {output: "inactive\n", err: &exec.ExitError{}},
		{}, {output: "active\n"},
	}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	status, err := manager.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)

	status, err = manager.Stop(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)

	status, err = manager.Restart(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, []serviceCommandCall{
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "stop", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "restart", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
	}, runner.calls)
}

func TestGivenInstalledSystemdUnitWhenStartingAndUninstallingThenLifecycleCommandsAreGenerated(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{}, {output: "active\n"},
		{}, {},
	}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	status, err = manager.Uninstall(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Installed)
	_, err = os.Stat(unitPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, []serviceCommandCall{
		{name: "systemctl", args: []string{"--user", "start", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "disable", "--now", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "daemon-reload"}},
	}, runner.calls)
}

func TestGivenSystemdLifecycleTransitionsWhenManagingThenActionsWaitForTerminalStates(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{}, {output: "activating\n", err: &exec.ExitError{}}, {output: "active\n"},
		{}, {output: "deactivating\n", err: &exec.ExitError{}}, {output: "inactive\n", err: &exec.ExitError{}},
	}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	started, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, started.Running)
	stopped, err := manager.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, stopped.Running)
	assert.Equal(t, []serviceCommandCall{
		{name: "systemctl", args: []string{"--user", "start", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "stop", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
		{name: "systemctl", args: []string{"--user", "is-active", "agentred.service"}},
	}, runner.calls)
}

func TestGivenSystemdNeverActivatesWhenStartingThenCancellationIsActionable(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{}, {output: "activating\n", err: &exec.ExitError{}},
	}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := manager.Start(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "wait for systemd unit agentred.service to become active")
	assert.Contains(t, err.Error(), "Run manually: systemctl --user status agentred.service")
}

func TestGivenSystemdCommandFailureWhenStartingThenErrorIncludesRecoveryCommand(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{err: errors.New("dbus unavailable")}}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	_, err := manager.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Run manually: systemctl --user start agentred.service")
}

func TestGivenSystemdFailedStateWhenInspectingThenItIsReportedAsStopped(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{
		output: "failed\n",
		err:    &exec.ExitError{},
	}}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	status, err := manager.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)
	assert.Contains(t, status.Details, "State: failed")
}

func TestGivenSystemdStatusPermissionFailureWhenInspectingThenItIsNotReportedAsStopped(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, ".config", "systemd", "user", "agentred.service")
	require.NoError(t, os.MkdirAll(filepath.Dir(unitPath), 0o755))
	require.NoError(t, os.WriteFile(unitPath, []byte("unit"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{
		output: "Failed to connect to bus: Permission denied\n",
		err:    &exec.ExitError{},
	}}}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: home, Runner: runner})

	_, err := manager.Status(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Permission denied")
	assert.Contains(t, err.Error(), "Run manually: systemctl --user is-active agentred.service")
}

func TestGivenMissingSystemdUnitWhenUninstallingThenOperationIsIdempotent(t *testing.T) {
	runner := &fakeServiceCommandRunner{}
	manager := newSystemdServiceManager(serviceManagerConfig{HomeDir: t.TempDir(), Runner: runner})

	status, err := manager.Uninstall(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{}, status)
	assert.Empty(t, runner.calls)
}
