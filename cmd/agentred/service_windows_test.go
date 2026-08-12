package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type windowsCommandExitError struct {
	code int
}

func (e windowsCommandExitError) Error() string {
	return "command exited"
}

func (e windowsCommandExitError) ExitCode() int {
	return e.code
}

func TestGivenWindowsManagerWhenInstallingThenItRegistersCurrentUserLogonTask(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "agentred 100%")
	runner := &fakeServiceCommandRunner{}
	manager := newWindowsServiceManager(serviceManagerConfig{
		BinaryPath: `C:\Program Files\Agentre 100%\agentred.exe`,
		DataDir:    dataDir,
		UserName:   `WORKSTATION\alice`,
		Runner:     runner,
	})

	status, err := manager.Install(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{Installed: true}, status)

	launcherPath := filepath.Join(dataDir, windowsServiceLauncherName)
	body, err := os.ReadFile(launcherPath) //nolint:gosec // launcherPath is assembled under the test's temporary data directory.
	require.NoError(t, err)
	launcher := string(body)
	assert.Contains(t, launcher, `$env:AGENTRED_DATA_DIR = '`+strings.ReplaceAll(dataDir, `'`, `''`)+`'`)
	assert.Contains(t, launcher, `& 'C:\Program Files\Agentre 100%\agentred.exe' run`)
	require.Len(t, runner.calls, 1)
	assert.Equal(t, "powershell.exe", runner.calls[0].name)
	require.Len(t, runner.calls[0].args, 4)
	assert.Equal(t, []string{"-NoProfile", "-NonInteractive", "-Command"}, runner.calls[0].args[:3])
	registration := runner.calls[0].args[3]
	assert.Contains(t, registration, `New-ScheduledTaskTrigger -AtLogOn -User 'WORKSTATION\alice'`)
	assert.Contains(t, registration, `New-ScheduledTaskPrincipal -UserId 'WORKSTATION\alice' -LogonType Interactive -RunLevel Limited`)
	assert.Contains(t, registration, `-ExecutionTimeLimit ([TimeSpan]::Zero)`)
	assert.Contains(t, registration, `-File "`+launcherPath+`"`)
	assert.Contains(t, registration, `Register-ScheduledTask -TaskName '`+windowsServiceTaskName+`'`)
}

func TestGivenInstalledWindowsTaskWhenManagingThenScheduledTaskStatesAreReported(t *testing.T) {
	dataDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, windowsServiceLauncherName), []byte("launcher"), 0o600))
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{
		{output: "Ready\r\n"},
		{}, {output: "Running\r\n"},
		{output: "Running\r\n"}, {}, {output: "Ready\r\n"},
		{output: "Ready\r\n"}, {}, {output: "Running\r\n"},
	}}
	manager := newWindowsServiceManager(serviceManagerConfig{DataDir: dataDir, Runner: runner})

	status, err := manager.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Installed)
	assert.False(t, status.Running)

	status, err = manager.Start(context.Background())
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
		windowsStatusCall(),
		{name: "schtasks.exe", args: []string{"/Run", "/TN", windowsServiceTaskName}}, windowsStatusCall(),
		windowsStatusCall(), {name: "schtasks.exe", args: []string{"/End", "/TN", windowsServiceTaskName}}, windowsStatusCall(),
		windowsStatusCall(), {name: "schtasks.exe", args: []string{"/Run", "/TN", windowsServiceTaskName}}, windowsStatusCall(),
	}, runner.calls)
}

func TestGivenMissingWindowsTaskWhenUninstallingThenOperationIsIdempotent(t *testing.T) {
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{err: windowsCommandExitError{code: 3}}}}
	manager := newWindowsServiceManager(serviceManagerConfig{DataDir: t.TempDir(), Runner: runner})

	status, err := manager.Uninstall(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ServiceStatus{}, status)
	assert.Equal(t, []serviceCommandCall{windowsStatusCall()}, runner.calls)
}

func TestGivenWindowsTaskQueryFailureWhenInspectingThenErrorIncludesRecoveryCommand(t *testing.T) {
	runner := &fakeServiceCommandRunner{results: []fakeServiceCommandResult{{err: errors.New("PowerShell unavailable")}}}
	manager := newWindowsServiceManager(serviceManagerConfig{DataDir: t.TempDir(), Runner: runner})

	_, err := manager.Status(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PowerShell unavailable")
	assert.Contains(t, err.Error(), "Run manually: powershell.exe")
}

func windowsStatusCall() serviceCommandCall {
	return serviceCommandCall{name: "powershell.exe", args: windowsStatusArgs()}
}
