package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/agentre-ai/agentre/internal/daemon"
	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

func newRunCmd() *cobra.Command {
	var (
		tlsCert string
		tlsKey  string
		host    string
		port    int
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Boot the daemon (foreground; SIGINT/SIGTERM to stop)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := paths.AgentredDataDir()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			d, err := daemon.New(daemon.Options{
				DataDir:     dir,
				LANHost:     host,
				LANPort:     port,
				TLSCertFile: tlsCert,
				TLSKeyFile:  tlsKey,
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
	return cmd
}
