package main

import (
	"context"
	"errors"
	"testing"

	"github.com/agentre-ai/agentre/e2e/preflight"
	"github.com/agentre-ai/agentre/internal/app"
	"github.com/agentre-ai/agentre/internal/bootstrap"
	"github.com/agentre-ai/agentre/internal/desktop"
)

func TestRunE2EValidatesBeforeDesktopBootstrap(t *testing.T) {
	preflightErr := errors.New("invalid runner manifest")
	desktopCalled := false

	err := runE2E(context.Background(), preflight.Environment{}, e2eDependencies{
		validate: func(preflight.Environment) (preflight.Config, error) {
			return preflight.Config{}, preflightErr
		},
		runDesktop: func(context.Context, desktop.Options) error {
			desktopCalled = true
			return nil
		},
	})
	if !errors.Is(err, preflightErr) {
		t.Fatalf("runE2E error = %v, want %v", err, preflightErr)
	}
	if desktopCalled {
		t.Fatal("desktop bootstrap must not run when preflight fails")
	}
}

func TestRunE2EInstallsCompositionAfterBootstrapBeforeWails(t *testing.T) {
	var order []string
	wantConfig := preflight.Config{RunRoot: "/run", DataDir: "/run/data", KeychainDir: "/run/keychain"}

	err := runE2E(context.Background(), preflight.Environment{}, e2eDependencies{
		validate: func(preflight.Environment) (preflight.Config, error) {
			order = append(order, "preflight")
			return wantConfig, nil
		},
		install: func(_ context.Context, got preflight.Config) error {
			order = append(order, "composition")
			if got != wantConfig {
				t.Fatalf("composition config = %+v, want %+v", got, wantConfig)
			}
			return nil
		},
		runDesktop: func(ctx context.Context, opts desktop.Options) error {
			order = append(order, "bootstrap")
			if opts.RuntimeMode != app.RuntimeModeHeadless {
				t.Fatalf("RuntimeMode = %q, want %q", opts.RuntimeMode, app.RuntimeModeHeadless)
			}
			if opts.AfterBootstrap == nil {
				t.Fatal("AfterBootstrap must install E2E composition")
			}
			if err := opts.AfterBootstrap(ctx, &bootstrap.Runtime{}); err != nil {
				return err
			}
			order = append(order, "wails")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("runE2E: %v", err)
	}
	wantOrder := []string{"preflight", "bootstrap", "composition", "wails"}
	if len(order) != len(wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("order = %v, want %v", order, wantOrder)
		}
	}
}
