package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print daemon state via the local unix socket",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			body, err := localGET("/local/status")
			if err != nil {
				return fmt.Errorf("daemon not running? %w", err)
			}
			var v map[string]any
			if err := json.Unmarshal(body, &v); err != nil {
				return err
			}
			fmt.Printf("Daemon running, pid %v\n", v["pid"])
			fmt.Println("Listening on:")
			for _, u := range toAnySlice(v["listenURLs"]) {
				fmt.Printf("  %v\n", u)
			}
			fmt.Printf("Paired devices: %d\n", len(toAnySlice(v["pairedPeers"])))
			fmt.Printf("Active sessions: %v\n", v["activeSessions"])
			fmt.Printf("LLM providers: %v\n", v["llmProviderCount"])
			return nil
		},
	}
}
