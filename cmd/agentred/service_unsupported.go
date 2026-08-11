package main

import (
	"context"
	"fmt"
)

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
