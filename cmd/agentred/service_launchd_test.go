package main

import (
	"context"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGivenLaunchdManagerWhenInstallingThenItWritesLaunchAgentWithoutStartingIt(t *testing.T) {
	home := t.TempDir()
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{
		BinaryPath: "/Applications/Agentre & Tools/agentred",
		DataDir:    "/Users/alice/Library/Application Support/agentred",
		HomeDir:    home,
		UID:        501,
		Runner:     runner,
	})

	status, err := manager.Install(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{Installed: true}, status)

	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	body, err := os.ReadFile(plistPath) //nolint:gosec // plistPath is assembled under the test's temporary home directory.
	require.NoError(t, err)
	plist := string(body)
	var document any
	require.NoError(t, xml.Unmarshal(body, &document), "generated LaunchAgent must be valid XML")
	assert.Contains(t, plist, "<string>/Applications/Agentre &amp; Tools/agentred</string>")
	assert.Contains(t, plist, "<string>run</string>")
	assert.Contains(t, plist, "<key>AGENTRED_DATA_DIR</key>")
	assert.Contains(t, plist, "<string>/Users/alice/Library/Application Support/agentred</string>")
	assert.Contains(t, plist, "<key>RunAtLoad</key>")
	assert.Contains(t, plist, "<key>KeepAlive</key>")
	assert.Equal(t, []serviceCommandCall{
		{name: "launchctl", args: []string{"bootout", "gui/501/ai.agentre.agentred"}},
	}, runner.calls, "install without --start must register the plist without launching the daemon")
}

func TestGivenLaunchAgentWhenInspectingAndManagingThenLoadedRunningStateIsReported(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "state = running\n"},
		{}, {output: "Could not find service", err: &exec.ExitError{}},
		{output: "Could not find service", err: &exec.ExitError{}}, {}, {output: "state = running\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.True(t, status.Running)

	status, err = manager.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Running)

	status, err = manager.Restart(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, []serviceCommandCall{
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"bootout", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"bootstrap", "gui/501", plistPath}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
	}, runner.calls)
}

func TestGivenInstalledLaunchAgentWhenStartingAndUninstallingThenLifecycleCommandsAreGenerated(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}}, {}, {output: "state = running\n"},
		{}, {output: "Could not find service", err: &exec.ExitError{}},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	status, err = manager.Uninstall(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Installed)
	_, err = os.Stat(plistPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, []serviceCommandCall{
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"bootstrap", "gui/501", plistPath}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"bootout", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
	}, runner.calls)
}

func TestGivenMissingLaunchAgentWhenUninstallingThenOperationIsIdempotent(t *testing.T) {
	runner := &fakeServiceCommandRunner{}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: t.TempDir(), UID: 501, Runner: runner})

	status, err := manager.Uninstall(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{}, status)
	assert.Empty(t, runner.calls)
}

func TestGivenLaunchdMissingServiceWhenInspectingThenItIsReportedAsStopped(t *testing.T) {
	for _, output := range []string{
		"Could not find service \"ai.agentre.agentred\" in domain for user gui: 501\n",
		"Bad request.\nCould not find service \"ai.agentre.agentred\" in domain for user domain\n",
		"Bootstrap failed: 125: Domain does not support specified action\nService cannot load in requested session\n",
	} {
		t.Run(output, func(t *testing.T) {
			home := t.TempDir()
			plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
			require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
			require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
			runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{
				output: output,
				err:    &exec.ExitError{},
			}}}
			manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

			status, err := manager.Status(context.Background())
			require.NoError(t, err)
			assert.True(t, status.Installed)
			assert.False(t, status.Running)
			assert.Contains(t, status.Details, "Loaded: false")
		})
	}
}

func TestGivenLaunchdStatusPermissionFailureWhenInspectingThenItIsNotReportedAsStopped(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{
		output: "Could not access service: Operation not permitted\n",
		err:    &exec.ExitError{},
	}}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	_, err := manager.Status(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Operation not permitted")
	assert.Contains(t, err.Error(), "Run manually: launchctl print gui/501/ai.agentre.agentred")
}

func TestGivenLoadedLaunchAgentWhenRestartingThenKickstartReplacesBootoutBootstrap(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "state = running\n"},
		{},
		{output: "state = running\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Restart(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, []serviceCommandCall{
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"kickstart", "-k", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
	}, runner.calls)
}

func TestGivenUnloadedLaunchAgentWhenStartingThenBootstrapWaitsUntilRunning(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
		{},
		{output: "state = waiting\n"},
		{output: "state = running\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Equal(t, []serviceCommandCall{
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"bootstrap", "gui/501", plistPath}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
		{name: "launchctl", args: []string{"print", "gui/501/ai.agentre.agentred"}},
	}, runner.calls)
}

func TestGivenLaunchAgentStillRunningAfterBootoutWhenStoppingThenItWaitsUntilUnloaded(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{},
		{output: "state = running\n"},
		{output: "Could not find service", err: &exec.ExitError{}},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Stop(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)
	assert.Len(t, runner.calls, 3)
}

func TestGivenLaunchAgentInXPCProxyWhenStartingThenItKeepsWaitingUntilRunning(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
		{},
		{output: `gui/501/ai.agentre.agentred = {
	active count = 1
	state = xpcproxy
	runs = 1
	pid = 40108
	last exit code = (never exited)
	properties = keepalive | runatload | inferred program
}
`},
		{output: `gui/501/ai.agentre.agentred = {
	active count = 1
	state = running
	runs = 1
	pid = 40108
	last exit code = (never exited)
}
`},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Len(t, runner.calls, 4, "an active xpcproxy job must be polled until launchd reports it running")
}

func TestGivenLaunchAgentInXPCProxyWithHistoricalFailureWhenStartingThenItKeepsWaitingUntilRunning(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
		{},
		{output: `gui/501/ai.agentre.agentred = {
	active count = 1
	state = xpcproxy
	runs = 2
	pid = 40108
	last exit code = 78
}
`},
		{output: `gui/501/ai.agentre.agentred = {
	active count = 1
	state = running
	runs = 2
	pid = 40108
	last exit code = 78
}
`},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Len(t, runner.calls, 4, "a historical exit code must not terminate an active xpcproxy startup")
}

func TestGivenLaunchAgentPreviouslyExitedCleanlyWhenStartingThenItKeepsWaitingUntilRunning(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
		{},
		{output: "state = waiting\nlast exit code = 0\n"},
		{output: "state = running\nlast exit code = 0\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	status, err := manager.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Running)
	assert.Len(t, runner.calls, 4, "a historical successful exit must not be treated as terminal failure")
}

func TestGivenLaunchAgentExitsWhileStartingThenItReturnsFailureWithoutWaitingForTimeout(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Could not find service", err: &exec.ExitError{}},
		{},
		{output: "state = waiting\nlast exit code = 1\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	_, err := manager.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminal failure")
	assert.Contains(t, err.Error(), "last exit code = 1")
	assert.Contains(t, err.Error(), "Run manually: launchctl print gui/501/ai.agentre.agentred")
	assert.Len(t, runner.calls, 3, "terminal daemon failure must not be retried until timeout")
}

func TestGivenLaunchAgentNeverRunsWhenStartingThenContextDeadlineReturnsActionableFailure(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "state = waiting\n"},
		{},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_, err := manager.Start(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "wait for launchd target gui/501/ai.agentre.agentred to run")
	assert.Contains(t, err.Error(), "Run manually: launchctl print gui/501/ai.agentre.agentred")
}

func TestGivenLaunchAgentNeverRunsWhenStartingThenContextCancellationReturnsActionableFailure(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "state = waiting\n"},
		{},
		{output: "state = waiting\n"},
	}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := manager.Start(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "wait for launchd target gui/501/ai.agentre.agentred to run")
	assert.Contains(t, err.Error(), "Run manually: launchctl print gui/501/ai.agentre.agentred")
}

func TestGivenLaunchdStopPermissionFailureWhenStoppingThenItReturnsRecoveryCommand(t *testing.T) {
	home := t.TempDir()
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "ai.agentre.agentred.plist")
	require.NoError(t, os.MkdirAll(filepath.Dir(plistPath), 0o755))
	require.NoError(t, os.WriteFile(plistPath, []byte("plist"), 0o644))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{
		output: "Boot-out failed: 1: Operation not permitted\n",
		err:    &exec.ExitError{},
	}}}
	manager := newLaunchdServiceManager(serviceManagerConfig{HomeDir: home, UID: 501, Runner: runner})

	_, err := manager.Stop(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Operation not permitted")
	assert.Contains(t, err.Error(), "Run manually: launchctl bootout gui/501/ai.agentre.agentred")
}
