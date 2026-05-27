package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newPairCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pair",
		Short: "Mint a one-shot pairing code + advertise listen URLs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			body, err := localGET("/local/pair")
			if err != nil {
				return err
			}
			var v map[string]any
			if err := json.Unmarshal(body, &v); err != nil {
				return err
			}
			fmt.Printf("Pairing code: %v\n", v["code"])
			fmt.Printf("Expires in %v seconds.\n", v["ttlSeconds"])
			fmt.Println("On desktop, use any of:")
			for _, u := range toAnySlice(v["listenURLs"]) {
				fmt.Printf("  %v\n", u)
			}
			return nil
		},
	}
}
