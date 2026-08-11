package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeServiceManager struct {
	calls        []string
	status       ServiceStatus
	statusByCall map[string]ServiceStatus
	errAt        string
}

func (f *fakeServiceManager) result(call string) (ServiceStatus, error) {
	f.calls = append(f.calls, call)
	if f.errAt == call {
		return ServiceStatus{}, errors.New("manager failed")
	}
	if status, ok := f.statusByCall[call]; ok {
		return status, nil
	}
	return f.status, nil
}

func (f *fakeServiceManager) Install(context.Context) (ServiceStatus, error) {
	return f.result("install")
}
func (f *fakeServiceManager) Start(context.Context) (ServiceStatus, error) {
	return f.result("start")
}
func (f *fakeServiceManager) Stop(context.Context) (ServiceStatus, error) {
	return f.result("stop")
}
func (f *fakeServiceManager) Restart(context.Context) (ServiceStatus, error) {
	return f.result("restart")
}
func (f *fakeServiceManager) Uninstall(context.Context) (ServiceStatus, error) {
	return f.result("uninstall")
}
func (f *fakeServiceManager) Status(context.Context) (ServiceStatus, error) {
	return f.result("status")
}

func executeServiceCommand(t *testing.T, manager ServiceManager, args ...string) (string, error) {
	t.Helper()
	cmd := newServiceCmdWithManager(manager)
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestGivenServiceCommandWhenInspectingSubcommandsThenLifecycleSurfaceIsStable(t *testing.T) {
	cmd := newServiceCmdWithManager(&fakeServiceManager{})
	got := map[string]bool{}
	for _, child := range cmd.Commands() {
		got[child.Name()] = true
	}
	assert.Equal(t, map[string]bool{
		"install": true, "start": true, "status": true, "restart": true, "stop": true, "uninstall": true,
	}, got)
}

func TestGivenInstallWithStartWhenExecutedThenManagerInstallsBeforeStarting(t *testing.T) {
	manager := &fakeServiceManager{status: ServiceStatus{Installed: true, Running: true}}
	out, err := executeServiceCommand(t, manager, "install", "--start")
	require.NoError(t, err)
	assert.Equal(t, []string{"install", "start"}, manager.calls)
	assert.Equal(t, "Daemon running", strings.Split(strings.TrimSpace(out), "\n")[0])
}

func TestGivenInstallWithStartAndLingerWarningWhenExecutedThenRepairDetailIsPreserved(t *testing.T) {
	manager := &fakeServiceManager{statusByCall: map[string]ServiceStatus{
		"install": {Installed: true, Details: []string{"Run: loginctl enable-linger alice"}},
		"start":   {Installed: true, Running: true},
	}}
	out, err := executeServiceCommand(t, manager, "install", "--start")
	require.NoError(t, err)
	assert.Contains(t, out, "Run: loginctl enable-linger alice")
}

func TestGivenServiceLifecycleActionsWhenExecutedThenTheyDelegateAndPrintStableState(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		status    ServiceStatus
		wantCall  string
		wantFirst string
	}{
		{name: "installed but stopped", args: []string{"install"}, status: ServiceStatus{Installed: true}, wantCall: "install", wantFirst: "Daemon stopped"},
		{name: "start", args: []string{"start"}, status: ServiceStatus{Installed: true, Running: true}, wantCall: "start", wantFirst: "Daemon running"},
		{name: "status not installed", args: []string{"status"}, status: ServiceStatus{}, wantCall: "status", wantFirst: "Service not installed"},
		{name: "restart", args: []string{"restart"}, status: ServiceStatus{Installed: true, Running: true}, wantCall: "restart", wantFirst: "Daemon running"},
		{name: "stop", args: []string{"stop"}, status: ServiceStatus{Installed: true}, wantCall: "stop", wantFirst: "Daemon stopped"},
		{name: "idempotent uninstall", args: []string{"uninstall"}, status: ServiceStatus{}, wantCall: "uninstall", wantFirst: "Service not installed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeServiceManager{status: tt.status}
			out, err := executeServiceCommand(t, manager, tt.args...)
			require.NoError(t, err)
			assert.Equal(t, []string{tt.wantCall}, manager.calls)
			assert.Equal(t, tt.wantFirst, strings.Split(strings.TrimSpace(out), "\n")[0])
		})
	}
}

func TestGivenRunningServiceWhenStatusIsRequestedThenLocalDaemonStatusUsesFrozenRenderer(t *testing.T) {
	manager := &fakeServiceManager{status: ServiceStatus{Installed: true, Running: true}}
	cmd := newServiceCmdWithDeps(serviceCommandDeps{
		managerFactory: func() (ServiceManager, error) { return manager, nil },
		localStatus: func() (map[string]any, error) {
			return map[string]any{
				"pid":              float64(42),
				"version":          "v1.2.3 (abcdef1)",
				"listenURLs":       []any{"ws://127.0.0.1:7456"},
				"pairedPeers":      []any{},
				"activeSessions":   float64(0),
				"llmProviderCount": float64(0),
			}, nil
		},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status"})
	require.NoError(t, cmd.Execute())
	assert.True(t, strings.HasPrefix(out.String(), "Daemon running, pid 42\n"))
	assert.Contains(t, out.String(), "Version: v1.2.3 (abcdef1)")
}

func TestGivenManagerFailureWhenServiceActionRunsThenCommandReturnsActionableError(t *testing.T) {
	manager := &fakeServiceManager{errAt: "restart"}
	out, err := executeServiceCommand(t, manager, "restart")
	require.Error(t, err)
	assert.Empty(t, out)
	assert.Contains(t, err.Error(), "restart service")
	assert.Contains(t, err.Error(), "manager failed")
}
