package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/agentre-ai/agentre/e2e/fakes"
	"github.com/agentre-ai/agentre/internal/daemon"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

const (
	defaultAgentredHost = "0.0.0.0"
	defaultAgentredPort = 7456
)

type runDaemon interface {
	Run(context.Context) error
}

type runDeps struct {
	dataDir   func() (string, error)
	newDaemon func(daemon.Options) (runDaemon, error)
}

type resolvedRunConfig struct {
	listen       state.ListenPrefs
	serverURL    string
	hasOverrides bool
}

func newRunCmd() *cobra.Command {
	return newRunCmdWithDeps(runDeps{
		dataDir: paths.AgentredDataDir,
		newDaemon: func(opts daemon.Options) (runDaemon, error) {
			return daemon.New(opts)
		},
	})
}

func newRunCmdWithDeps(deps runDeps) *cobra.Command {
	var (
		tlsCert   string
		tlsKey    string
		host      string
		port      int
		serverURL string
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Boot the daemon (foreground; SIGINT/SIGTERM to stop)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := deps.dataDir()
			if err != nil {
				return err
			}
			st, err := state.Load(dir)
			if err != nil {
				return err
			}
			config, err := resolveRunConfig(cmd, st.Snapshot(), host, port, tlsCert, tlsKey, serverURL)
			if err != nil {
				return err
			}
			if config.hasOverrides {
				st.Mutate(func(s *state.State) {
					s.Listen = config.listen
					s.HubServerURL = config.serverURL
				})
				if err := st.Save(); err != nil {
					return fmt.Errorf("save daemon runtime configuration: %w", err)
				}
			}

			// e2e 构建(go build -tags e2e)在这里安装确定性 fake runtime,让 web 端到端
			// 对 agentred 的 runtime.run 得到字节稳定的回复;默认构建里是空操作。
			fakes.InstallAgentred(cmd.Context())
			d, err := deps.newDaemon(daemon.Options{
				DataDir:      dir,
				LANHost:      config.listen.LanHost,
				LANPort:      config.listen.LanPort,
				TLSCertFile:  config.listen.TLSCertFile,
				TLSKeyFile:   config.listen.TLSKeyFile,
				HubServerURL: config.serverURL,
			})
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return d.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "PEM certificate path (or AGENTRED_TLS_CERT); enables wss://")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "PEM private key path (or AGENTRED_TLS_KEY); required with --tls-cert")
	cmd.Flags().StringVar(&host, "host", defaultAgentredHost, "LAN listen host (or AGENTRED_HOST)")
	cmd.Flags().IntVar(&port, "port", defaultAgentredPort, "LAN listen port (or AGENTRED_PORT)")
	cmd.Flags().StringVar(&serverURL, "server", strings.TrimSpace(os.Getenv("AGENTRED_SERVER_URL")), "account server base URL (or AGENTRED_SERVER_URL)")
	return cmd
}

func resolveRunConfig(cmd *cobra.Command, persisted state.State, flagHost string, flagPort int,
	flagTLSCert, flagTLSKey, flagServerURL string) (resolvedRunConfig, error) {
	config := resolvedRunConfig{}
	config.listen.LanHost, config.hasOverrides = resolveString(
		cmd.Flags().Changed("host"), flagHost, "AGENTRED_HOST", persisted.Listen.LanHost, defaultAgentredHost,
	)

	port, portOverride, err := resolvePort(cmd.Flags().Changed("port"), flagPort, persisted.Listen.LanPort)
	if err != nil {
		return resolvedRunConfig{}, err
	}
	config.listen.LanPort = port
	config.hasOverrides = config.hasOverrides || portOverride

	config.listen.TLSCertFile, portOverride = resolveString(
		cmd.Flags().Changed("tls-cert"), flagTLSCert, "AGENTRED_TLS_CERT", persisted.Listen.TLSCertFile, "",
	)
	config.hasOverrides = config.hasOverrides || portOverride
	config.listen.TLSKeyFile, portOverride = resolveString(
		cmd.Flags().Changed("tls-key"), flagTLSKey, "AGENTRED_TLS_KEY", persisted.Listen.TLSKeyFile, "",
	)
	config.hasOverrides = config.hasOverrides || portOverride
	config.serverURL, portOverride = resolveString(
		cmd.Flags().Changed("server"), flagServerURL, "AGENTRED_SERVER_URL", persisted.HubServerURL, "",
	)
	config.hasOverrides = config.hasOverrides || portOverride

	config.listen.LanHost = strings.TrimSpace(config.listen.LanHost)
	config.listen.TLSCertFile = strings.TrimSpace(config.listen.TLSCertFile)
	config.listen.TLSKeyFile = strings.TrimSpace(config.listen.TLSKeyFile)
	config.serverURL = strings.TrimSpace(config.serverURL)
	if config.serverURL != "" {
		config.serverURL, err = validServerURL(config.serverURL)
		if err != nil {
			return resolvedRunConfig{}, err
		}
	}
	return config, nil
}

func resolveString(flagChanged bool, flagValue, envName, persisted, fallback string) (string, bool) {
	if flagChanged {
		return flagValue, true
	}
	if value, ok := os.LookupEnv(envName); ok {
		return value, true
	}
	if persisted != "" {
		return persisted, false
	}
	return fallback, false
}

func resolvePort(flagChanged bool, flagValue, persisted int) (int, bool, error) {
	if flagChanged {
		return flagValue, true, nil
	}
	if raw, ok := os.LookupEnv("AGENTRED_PORT"); ok {
		port, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return 0, false, newUsageError("AGENTRED_PORT must be an integer")
		}
		return port, true, nil
	}
	if persisted != 0 {
		return persisted, false, nil
	}
	return defaultAgentredPort, false, nil
}
