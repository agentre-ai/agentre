// Package composition is the dedicated E2E composition root. Production
// entrypoints must never import it.
package composition

import (
	"context"
	"fmt"

	"github.com/agentre-ai/agentre/e2e/fakes"
	"github.com/agentre-ai/agentre/e2e/preflight"
)

// Config is the validated run-scoped storage contract passed by the E2E main.
type Config = preflight.Config

// Install replaces only external/runtime boundaries and seeds deterministic
// E2E state after production bootstrap has completed.
func Install(ctx context.Context, config Config) error {
	if config.RunRoot == "" || config.DataDir == "" || config.KeychainDir == "" {
		return fmt.Errorf("e2e composition requires validated preflight config")
	}
	return fakes.Install(ctx)
}
