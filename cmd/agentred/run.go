package main

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/agentre-ai/agentre/e2e/fakes"
	"github.com/agentre-ai/agentre/internal/daemon"
	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

func newRunCmd() *cobra.Command {
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
			serverURL = strings.TrimSpace(serverURL)
			if serverURL != "" {
				var err error
				serverURL, err = validServerURL(serverURL)
				if err != nil {
					return err
				}
			}
			dir, err := paths.AgentredDataDir()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			// e2e 构建(go build -tags e2e)在这里安装确定性 fake runtime,让 web 端到端
			// 对 agentred 的 runtime.run 得到字节稳定的回复;默认构建里是空操作。
			fakes.InstallAgentred(cmd.Context())
			d, err := daemon.New(daemon.Options{
				DataDir:      dir,
				LANHost:      host,
				LANPort:      port,
				TLSCertFile:  tlsCert,
				TLSKeyFile:   tlsKey,
				HubServerURL: serverURL,
			})
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			return d.Run(ctx)
		},
	}
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "PEM certificate path; enables wss://")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "PEM private key path; required with --tls-cert")
	cmd.Flags().StringVar(&host, "host", "0.0.0.0", "LAN listen host")
	cmd.Flags().IntVar(&port, "port", 7456, "LAN listen port")
	cmd.Flags().StringVar(&serverURL, "server", strings.TrimSpace(os.Getenv("AGENTRED_SERVER_URL")), "account server base URL (or AGENTRED_SERVER_URL)")
	return cmd
}
