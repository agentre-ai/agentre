//go:build !windows

package piagent

import (
	"errors"
	"syscall"
)

// errNoTreeTarget reports that a pid does not name a process tree this package
// may signal as a group.
var errNoTreeTarget = errors.New("piagent: pid does not lead a signalable process tree")

// signalTree delivers sig to the process group led by pid.
//
// A group signal has no per-process blast radius, so it is only ever sent to a
// pid that leads its own group — which is exactly the shape execProcessRunner
// gives every Pi process (Setpgid). A pid that has been reaped, or that merely
// lives inside somebody else's group, names no tree of ours; signaling
// "its" group would hit every sibling of the caller instead, and under `go test`
// that group holds the test runner plus the whole toolchain. Such a pid is
// refused here so the caller falls back to signaling the single process.
func signalTree(pid int, sig syscall.Signal) error {
	if pid <= 1 {
		return errNoTreeTarget
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return errNoTreeTarget
	}
	if pgid != pid || pgid == syscall.Getpgrp() {
		return errNoTreeTarget
	}
	return syscall.Kill(-pgid, sig)
}
