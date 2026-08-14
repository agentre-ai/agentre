package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/agentre-ai/agentre/e2e/composition"
	"github.com/agentre-ai/agentre/e2e/preflight"
	"github.com/agentre-ai/agentre/internal/app"
	"github.com/agentre-ai/agentre/internal/bootstrap"
	"github.com/agentre-ai/agentre/internal/desktop"
)

var assets = os.DirFS("frontend/dist")

type e2eDependencies struct {
	validate   func(preflight.Environment) (preflight.Config, error)
	install    func(context.Context, preflight.Config) error
	runDesktop func(context.Context, desktop.Options) error
}

func main() {
	ctx := context.Background()
	if err := runE2E(ctx, preflight.FromEnvironment(), e2eDependencies{
		validate:   preflight.Validate,
		install:    composition.Install,
		runDesktop: desktop.Run,
	}); err != nil {
		log.Fatalf("e2e app: %v", err)
	}
}

func runE2E(ctx context.Context, env preflight.Environment, deps e2eDependencies) error {
	config, err := deps.validate(env)
	if err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	for _, name := range []string{"AGENTRE_E2E_MANIFEST", "AGENTRE_E2E_TOKEN"} {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("consume %s: %w", name, err)
		}
	}
	if err := deps.runDesktop(ctx, desktop.Options{
		Assets:      assets,
		RuntimeMode: app.RuntimeModeHeadless,
		AfterBootstrap: func(ctx context.Context, _ *bootstrap.Runtime) error {
			return deps.install(ctx, config)
		},
	}); err != nil {
		return fmt.Errorf("desktop run: %w", err)
	}
	return nil
}
