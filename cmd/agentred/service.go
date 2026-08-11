package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type serviceManagerFactory func() (ServiceManager, error)
type serviceAction func(context.Context, ServiceManager) (ServiceStatus, error)
type serviceStatusLoader func() (map[string]any, error)

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
			started, err := manager.Start(ctx)
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
			return manager.Start(ctx)
		}, printServiceStatus),
		newServiceActionCmd("status", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return manager.Status(ctx)
		}, func(w io.Writer, status ServiceStatus) {
			printServiceInspection(w, status, deps.localStatus)
		}),
		newServiceActionCmd("restart", deps.managerFactory, func(ctx context.Context, manager ServiceManager) (ServiceStatus, error) {
			return manager.Restart(ctx)
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

func loadLocalDaemonStatus() (map[string]any, error) {
	body, err := localGET("/local/status")
	if err != nil {
		return nil, err
	}
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	return status, nil
}

func printServiceInspection(w io.Writer, status ServiceStatus, load serviceStatusLoader) {
	if status.Running && load != nil {
		localStatus, err := load()
		if err == nil {
			printStatus(w, localStatus)
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
