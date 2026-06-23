package cliprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStart_GivenEnvShebangInterpreterNextToBinary_WhenStartingProcess_ThenItBoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env shebang lookup is a Unix behavior")
	}

	binDir := t.TempDir()
	interpreter := filepath.Join(binDir, "agentre-test-node")
	shim := filepath.Join(binDir, "agentre-test-cli")
	require.NoError(t, os.WriteFile(interpreter, []byte("#!/bin/sh\nprintf 'booted:%s\\n' \"$AGENTRE_CLIPROCESS_TEST\"\n"), 0o755))
	require.NoError(t, os.WriteFile(shim, []byte("#!/usr/bin/env agentre-test-node\n"), 0o755))

	t.Setenv("PATH", "/usr/bin:/bin")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h, err := Start(ctx, Options{
		Binary: shim,
		Env:    []string{"AGENTRE_CLIPROCESS_TEST=ok"},
	}, errors.New("missing"))
	require.NoError(t, err)

	out, readErr := io.ReadAll(h.Stdout())
	require.NoError(t, readErr)
	assert.Equal(t, "booted:ok\n", string(out))
	assert.NoError(t, h.Wait())
}

func TestStart_GivenMissingBinary_WhenStartingProcess_ThenReturnsCallerSentinel(t *testing.T) {
	errMissing := errors.New("custom missing sentinel")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	h, err := Start(ctx, Options{Binary: "agentre-definitely-missing-binary"}, errMissing)

	require.Nil(t, h)
	require.ErrorIs(t, err, errMissing)
}
