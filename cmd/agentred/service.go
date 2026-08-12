package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type serviceManagerFactory func() (ServiceManager, error)
type serviceAction func(context.Context, ServiceManager) (ServiceStatus, error)
type serviceStatusLoader func(context.Context) (map[string]any, error)

const (
	serviceReadyTimeout      = 15 * time.Second
	serviceReadyPollInterval = 100 * time.Millisecond
)

type serviceCommandDeps struct {
	managerFactory serviceManagerFactory
	localStatus    serviceStatusLoader
}

func newServiceCmd() *cobra.Command {
	return newServiceCmdWithDeps(serviceCommandDeps{
		managerFactory: newPlatformServiceManager,
		localStatus:    loadLocalDaemonStatus,
	})
}

func newServiceCmdWithManager(manager ServiceManager) *cobra.Command {
	return newServiceCmdWithFactory(func() (ServiceManager, error) { return manager, nil })
}

func newServiceCmdWithFactory(factory serviceManagerFactory) *cobra.Command {
	return newServiceCmdWithDeps(serviceCommandDeps{managerFactory: factory})
}

func newServiceCmdWithDeps(deps serviceCommandDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage agentred as a user-level background service",
		Args:  cobra.NoArgs,
	}

	var startAfterInstall bool
	install := newServiceActionCmd("install", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
		status, err := manager.Install(ctx)
		if err != nil {
			return ServiceStatus{}, err
		}
		if startAfterInstall {
			started, err := startService(ctx, manager, deps.localStatus)
			if err != nil {
				return ServiceStatus{}, err
			}
			started.Details = append(status.Details, started.Details...)
			return started, nil
		}
		return status, nil
	}, printServiceStatus)
	install.Flags().BoolVar(&startAfterInstall, "start", false, "start the service immediately after installation")
	cmd.AddCommand(
		install,
		newServiceActionCmd("start", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return startService(ctx, manager, deps.localStatus)
		}, printServiceStatus),
		newServiceActionCmd("status", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return manager.Status(ctx)
		}, func(w io.Writer, status ServiceStatus) {
			printServiceInspection(w, status, deps.localStatus)
		}),
		newServiceActionCmd("restart", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return restartService(ctx, manager, deps.localStatus)
		}, printServiceStatus),
		newServiceActionCmd("stop", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return manager.Stop(ctx)
		}, printServiceStatus),
		newServiceActionCmd("uninstall", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return manager.Uninstall(ctx)
		}, printServiceStatus),
	)
	return cmd
}

func newServiceActionCmd(name string, factory serviceManagerFactory, action serviceAction,
	printer func(io.Writer, ServiceStatus)) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: serviceActionDescription(name),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := factory()
			if err != nil {
				return fmt.Errorf("%s service: %w", name, err)
			}
			status, err := action(cmd.Context(), manager)
			if err != nil {
				return fmt.Errorf("%s service: %w", name, err)
			}
			printer(cmd.OutOrStdout(), status)
			return nil
		},
	}
}

func serviceActionDescription(action string) string {
	switch action {
	case "install":
		return "Install or update the user-level service"
	case "start":
		return "Start the installed service"
	case "status":
		return "Inspect service registration and daemon state"
	case "restart":
		return "Restart the installed service"
	case "stop":
		return "Stop the installed service"
	case "uninstall":
		return "Stop and remove the user-level service"
	default:
		return action
	}
}

func startService(ctx context.Context, manager ServiceManager, load serviceStatusLoader) (ServiceStatus, error) {
	status, err := manager.Start(ctx)
	if err != nil {
		return ServiceStatus{}, err
	}
	return waitForLocalDaemon(ctx, manager, status, load)
}

func restartService(ctx context.Context, manager ServiceManager, load serviceStatusLoader) (ServiceStatus, error) {
	previousPID := ""
	if load != nil {
		if current, err := load(ctx); err == nil {
			previousPID = daemonStatusPID(current)
		}
	}
	status, err := manager.Restart(ctx)
	if err != nil {
		return ServiceStatus{}, err
	}
	if !requiresRestartPIDChange(status) {
		previousPID = ""
	}
	return waitForLocalDaemonPIDChange(ctx, manager, status, load, previousPID)
}

func requiresRestartPIDChange(status ServiceStatus) bool {
	for _, detail := range status.Details {
		if strings.HasPrefix(detail, "Manager: launchd") || strings.HasPrefix(detail, "Manager: systemd") {
			return true
		}
	}
	return false
}

func waitForLocalDaemon(ctx context.Context, manager ServiceManager, status ServiceStatus,
	load serviceStatusLoader) (ServiceStatus, error) {
	return waitForLocalDaemonPIDChange(ctx, manager, status, load, "")
}

func waitForLocalDaemonPIDChange(ctx context.Context, manager ServiceManager, status ServiceStatus,
	load serviceStatusLoader, previousPID string) (ServiceStatus, error) {
	if load == nil {
		return status, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, serviceReadyTimeout)
	defer cancel()
	var (
		lastErr    error
		lastStatus = status
	)
	for {
		if localStatus, err := load(waitCtx); err == nil {
			currentPID := daemonStatusPID(localStatus)
			if previousPID == "" || (currentPID != "" && currentPID != previousPID) {
				return status, nil
			}
			lastErr = fmt.Errorf("local daemon still reports pre-restart PID %s", previousPID)
		} else {
			lastErr = err
		}
		observed, err := manager.Status(waitCtx)
		if err != nil {
			return ServiceStatus{}, fmt.Errorf("inspect service while waiting for local daemon status: %w", err)
		}
		lastStatus = observed
		if !observed.Running {
			return ServiceStatus{}, fmt.Errorf("wait for local daemon status: service stopped before becoming ready (last error: %v; service: %s); %s", lastErr, strings.Join(observed.Details, ", "), serviceReadinessDiagnostic(observed))
		}
		select {
		case <-waitCtx.Done():
			return ServiceStatus{}, fmt.Errorf("wait for local daemon status: %w (last error: %v; service: %s); %s", waitCtx.Err(), lastErr, strings.Join(lastStatus.Details, ", "), serviceReadinessDiagnostic(lastStatus))
		case <-time.After(serviceReadyPollInterval):
		}
	}
}

func serviceReadinessDiagnostic(status ServiceStatus) string {
	for _, detail := range status.Details {
		if target, ok := strings.CutPrefix(detail, "Target: "); ok {
			return fmt.Sprintf("launchctl target %s; Run manually: launchctl print %s", target, target)
		}
	}
	return "Run manually: agentred service status"
}

func daemonStatusPID(status map[string]any) string {
	if status == nil || status["pid"] == nil {
		return ""
	}
	return fmt.Sprint(status["pid"])
}

func loadLocalDaemonStatus(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://daemon/local/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := localClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return status, nil
}

func printServiceInspection(w io.Writer, status ServiceStatus, load serviceStatusLoader) {
	printServiceInspectionContext(context.Background(), w, status, load)
}

func printServiceInspectionContext(ctx context.Context, w io.Writer, status ServiceStatus, load serviceStatusLoader) {
	if status.Running && load != nil {
		localStatus, err := load(ctx)
		if err == nil {
			printServiceDaemonStatus(w, localStatus)
			printServiceDetails(w, status.Details)
			return
		}
		status.Details = append(status.Details, "Local status unavailable: "+err.Error())
	}
	printServiceStatus(w, status)
}

func printServiceStatus(w io.Writer, status ServiceStatus) {
	switch {
	case !status.Installed:
		_, _ = fmt.Fprintln(w, "Service not installed")
	case status.Running:
		_, _ = fmt.Fprintln(w, "Daemon running")
	default:
		_, _ = fmt.Fprintln(w, "Daemon stopped")
	}
	printServiceDetails(w, status.Details)
}

func printServiceDetails(w io.Writer, details []string) {
	for _, detail := range details {
		_, _ = fmt.Fprintln(w, detail)
	}
}
