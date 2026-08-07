//go:build !windows

package piagent

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Given a pid whose process group is the caller's own group, When the process
// tree rooted at it is signaled, Then the caller's own group must not receive
// the signal.
//
// A group signal that escapes its target tree hits every sibling in the
// caller's group. Under `go test` that group holds the test runner, the go
// command and its compile/link/vet children, so a single mistargeted group
// signal takes the whole build down instead of one Pi process tree.
func TestSignalProcessTreeNeverSignalsCallersOwnProcessGroup(t *testing.T) {
	if syscall.Getpgrp() == os.Getpid() {
		t.Skip("the test process is its own group leader: a single-process fallback would be indistinguishable from a group signal")
	}

	received := make(chan os.Signal, 1)
	// SIGWINCH is inert for every process in the group, so an escaping signal is
	// observable without harming the runner that this test protects.
	signal.Notify(received, syscall.SIGWINCH)
	t.Cleanup(func() { signal.Stop(received) })

	// The group id is always its own group leader, so this pid passes any
	// "is this a real process group?" check while still naming our own group.
	_ = signalProcessTree(&os.Process{Pid: syscall.Getpgrp()}, syscall.SIGWINCH)

	select {
	case sig := <-received:
		t.Fatalf("the caller's own process group received %v: the tree signal escaped its target tree", sig)
	case <-time.After(300 * time.Millisecond):
	}
}

// Given a child that leads its own process group and holds a grandchild in that
// group, When the tree is killed, Then the grandchild dies with it.
func TestKillProcessTreeReapsGrandchildInTheSameGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// #nosec G204 -- fixed shell script; the only argument is a test-owned temp path.
	cmd := exec.Command("/bin/sh", "-c", `sleep 30 & printf '%s\n' "$!" > "$1"; wait`, "sh", pidFile)
	applyProcessTreeAttributes(cmd)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = killProcessTree(cmd.Process)
		_ = cmd.Wait()
	})

	grandchild := readPIDEventually(t, pidFile)
	require.NoError(t, killProcessTree(cmd.Process))

	// Reap the group leader first: an unreaped zombie still answers kill(pid, 0),
	// so the liveness assertions below would read it as alive.
	require.Error(t, cmd.Wait(), "the group leader must die from the tree kill, not exit cleanly")
	assertProcessGoneEventually(t, grandchild)
	assertProcessGoneEventually(t, cmd.Process.Pid)
}

// Given a pid that no longer names any process group, When the tree is
// signaled, Then the caller's own group is still left alone.
func TestSignalProcessTreeOnAReapedPidLeavesCallersGroupAlone(t *testing.T) {
	if syscall.Getpgrp() == os.Getpid() {
		t.Skip("the test process is its own group leader: a single-process fallback would be indistinguishable from a group signal")
	}

	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	applyProcessTreeAttributes(cmd)
	require.NoError(t, cmd.Start())
	reaped := cmd.Process.Pid
	require.NoError(t, cmd.Wait())

	received := make(chan os.Signal, 1)
	signal.Notify(received, syscall.SIGWINCH)
	t.Cleanup(func() { signal.Stop(received) })

	_ = signalProcessTree(&os.Process{Pid: reaped}, syscall.SIGWINCH)

	select {
	case sig := <-received:
		t.Fatalf("the caller's own process group received %v for a reaped pid %d", sig, reaped)
	case <-time.After(300 * time.Millisecond):
	}
}
