package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const launchdServiceLabel = "ai.agentre.agentred"

type launchdServiceManager struct {
	config    serviceManagerConfig
	plistPath string
	domain    string
	target    string
}

func newLaunchdServiceManager(config serviceManagerConfig) ServiceManager {
	domain := "gui/" + strconv.Itoa(config.UID)
	return &launchdServiceManager{
		config:    config,
		plistPath: filepath.Join(config.HomeDir, "Library", "LaunchAgents", launchdServiceLabel+".plist"),
		domain:    domain,
		target:    domain + "/" + launchdServiceLabel,
	}
}

func (m *launchdServiceManager) Install(ctx context.Context) (ServiceStatus, error) {
	if err := writeServiceFile(m.plistPath, []byte(m.plist())); err != nil {
		return ServiceStatus{}, err
	}
	if err := m.bootout(ctx); err != nil {
		return ServiceStatus{}, err
	}
	return ServiceStatus{Installed: true}, nil
}

func (m *launchdServiceManager) Start(ctx context.Context) (ServiceStatus, error) {
	if err := requireInstalled(m.plistPath); err != nil {
		return ServiceStatus{}, err
	}
	if err := m.bootstrap(ctx); err != nil {
		return ServiceStatus{}, err
	}
	return m.Status(ctx)
}

func (m *launchdServiceManager) Stop(ctx context.Context) (ServiceStatus, error) {
	if err := requireInstalled(m.plistPath); err != nil {
		return ServiceStatus{}, err
	}
	output, err := m.config.Runner.Run(ctx, "launchctl", "bootout", m.target)
	if err != nil && !isLaunchdMissingService(output) {
		return ServiceStatus{}, serviceCommandError("launchctl", []string{"bootout", m.target}, output, err)
	}
	return m.Status(ctx)
}

func (m *launchdServiceManager) Restart(ctx context.Context) (ServiceStatus, error) {
	if err := requireInstalled(m.plistPath); err != nil {
		return ServiceStatus{}, err
	}
	if err := m.bootstrap(ctx); err != nil {
		return ServiceStatus{}, err
	}
	return m.Status(ctx)
}

func (m *launchdServiceManager) Uninstall(ctx context.Context) (ServiceStatus, error) {
	installed, err := serviceFileExists(m.plistPath)
	if err != nil {
		return ServiceStatus{}, err
	}
	if !installed {
		return ServiceStatus{}, nil
	}
	output, err := m.config.Runner.Run(ctx, "launchctl", "bootout", m.target)
	if err != nil && !isLaunchdMissingService(output) {
		return ServiceStatus{}, serviceCommandError("launchctl", []string{"bootout", m.target}, output, err)
	}
	if err := os.Remove(m.plistPath); err != nil && !os.IsNotExist(err) {
		return ServiceStatus{}, fmt.Errorf("remove LaunchAgent plist: %w", err)
	}
	return ServiceStatus{}, nil
}

func (m *launchdServiceManager) Status(ctx context.Context) (ServiceStatus, error) {
	installed, err := serviceFileExists(m.plistPath)
	if err != nil {
		return ServiceStatus{}, err
	}
	if !installed {
		return ServiceStatus{}, nil
	}
	args := []string{"print", m.target}
	output, err := m.config.Runner.Run(ctx, "launchctl", args...)
	if err != nil && !isLaunchdMissingService(output) {
		return ServiceStatus{}, serviceCommandError("launchctl", args, output, err)
	}
	loaded := err == nil
	running := loaded && strings.Contains(string(output), "state = running")
	return ServiceStatus{
		Installed: true,
		Running:   running,
		Details: []string{
			"Manager: launchd LaunchAgent",
			"Plist: " + m.plistPath,
			fmt.Sprintf("Loaded: %t", loaded),
			fmt.Sprintf("Running: %t", running),
		},
	}, nil
}

func isLaunchdMissingService(output []byte) bool {
	detail := strings.ToLower(string(output))
	return strings.Contains(detail, "could not find service") ||
		strings.Contains(detail, "service not found") ||
		strings.Contains(detail, "no such process") ||
		strings.Contains(detail, "service cannot load in requested session")
}

func (m *launchdServiceManager) bootstrap(ctx context.Context) error {
	if err := m.bootout(ctx); err != nil {
		return err
	}
	return m.run(ctx, "launchctl", "bootstrap", m.domain, m.plistPath)
}

func (m *launchdServiceManager) bootout(ctx context.Context) error {
	output, err := m.config.Runner.Run(ctx, "launchctl", "bootout", m.target)
	if err != nil && !isLaunchdMissingService(output) {
		return serviceCommandError("launchctl", []string{"bootout", m.target}, output, err)
	}
	return nil
}

func (m *launchdServiceManager) run(ctx context.Context, name string, args ...string) error {
	output, err := m.config.Runner.Run(ctx, name, args...)
	if err != nil {
		return serviceCommandError(name, args, output, err)
	}
	return nil
}

func (m *launchdServiceManager) plist() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + xmlText(launchdServiceLabel) + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlText(m.config.BinaryPath) + `</string>
    <string>run</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>AGENTRED_DATA_DIR</key>
    <string>` + xmlText(m.config.DataDir) + `</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
`
}

func xmlText(value string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(value))
	return escaped.String()
}
