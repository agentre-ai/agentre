package main

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"agentre/internal/app"
)

func TestSingleInstanceUniqueID(t *testing.T) {
	got := singleInstanceUniqueID("/tmp/agentre-a")
	if got == "" {
		t.Fatal("singleInstanceUniqueID returned empty string")
	}
	if got == singleInstanceUniqueID("/tmp/agentre-b") {
		t.Fatal("different data dirs should produce different single instance ids")
	}
	if got != singleInstanceUniqueID("/tmp/agentre-a") {
		t.Fatal("same data dir should produce a stable single instance id")
	}
}

func TestNewWailsOptionsConfiguresSingleInstanceLock(t *testing.T) {
	var assets fs.FS = fstest.MapFS{}
	opts := newWailsOptionsForDataDir(app.NewApp(), assets, "darwin", "/tmp/agentre-test")
	if opts.SingleInstanceLock == nil {
		t.Fatal("SingleInstanceLock is nil")
	}
	if opts.SingleInstanceLock.UniqueId != singleInstanceUniqueID("/tmp/agentre-test") {
		t.Fatalf("SingleInstanceLock.UniqueId = %q, want %q", opts.SingleInstanceLock.UniqueId, singleInstanceUniqueID("/tmp/agentre-test"))
	}
	if opts.SingleInstanceLock.OnSecondInstanceLaunch == nil {
		t.Fatal("OnSecondInstanceLaunch is nil")
	}
}
