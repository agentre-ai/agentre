//go:build !windows

package main

import (
	"context"
	"fmt"
	"os/user"
	"runtime"
	"strconv"
)

func newOSServiceManager(config serviceManagerConfig, currentUser *user.User) (ServiceManager, error) {
	switch runtime.GOOS {
	case "linux":
		return newSystemdServiceManager(config), nil
	case "darwin":
		uid, err := strconv.Atoi(currentUser.Uid)
		if err != nil {
			return nil, fmt.Errorf("resolve current user id: %w", err)
		}
		config.UID = uid
		return newLaunchdServiceManager(config), nil
	default:
		return &unsupportedServiceManager{platform: runtime.GOOS}, nil
	}
}

type unsupportedServiceManager struct {
	platform string
}

func (m *unsupportedServiceManager) unsupported() (ServiceStatus, error) {
	return ServiceStatus{}, fmt.Errorf("user service management is not supported on %s", m.platform)
}

func (m *unsupportedServiceManager) Install(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
func (m *unsupportedServiceManager) Start(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
func (m *unsupportedServiceManager) Stop(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
func (m *unsupportedServiceManager) Restart(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
func (m *unsupportedServiceManager) Uninstall(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
func (m *unsupportedServiceManager) Status(context.Context) (ServiceStatus, error) {
	return m.unsupported()
}
