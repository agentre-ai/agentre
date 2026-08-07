//go:build windows

package piagent

import (
	"errors"
	"syscall"
)

// errNoTreeTarget reports that a pid does not name a process tree this package
// may signal as a group.
var errNoTreeTarget = errors.New("piagent: pid does not lead a signalable process tree")

// signalTree has no POSIX process-group equivalent on Windows: the tree is torn
// down through taskkill /T in killProcessTree, and single signals go through
// os.Process. Refusing here keeps every caller on that path.
func signalTree(_ int, _ syscall.Signal) error { return errNoTreeTarget }
