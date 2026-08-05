package pty

import (
	"time"

	"github.com/cago-frame/cago/pkg/gogo"
)

const detachedCleanupRetryInterval = 50 * time.Millisecond

// DetachedCleanupOutcome is the bounded authority obtained by a detached
// cleanup guardian after the caller's initial Handle.Close attempt failed.
type DetachedCleanupOutcome string

const (
	// DetachedCleanupCloseSucceeded means a paced Close retry transferred
	// cleanup authority to the handle implementation.
	DetachedCleanupCloseSucceeded DetachedCleanupOutcome = "closeSucceeded"
	// DetachedCleanupTerminalExited means Exit was observed and Data was fully
	// drained and closed, so the terminal settled without a successful Close.
	DetachedCleanupTerminalExited DetachedCleanupOutcome = "terminalExited"
)

// CleanupHandle is the narrow Handle surface needed to retain cleanup
// ownership. Implementations must follow Handle's Data and Exit channel
// contract.
type CleanupHandle interface {
	Close() error
	Data() <-chan []byte
	Exit() <-chan ExitInfo
}

// StartDetachedCleanup starts exactly one guardian after the caller's initial
// Close failed. The guardian continuously drains and discards Data, retries
// Close at a paced interval, and invokes settled exactly once after either a
// successful Close or a fully observed terminal exit.
func StartDetachedCleanup(handle CleanupHandle, settled func(DetachedCleanupOutcome)) {
	gogo.Go(func() error {
		settled(awaitDetachedCleanup(handle))
		return nil
	}, gogo.WithIgnorePanic())
}

func awaitDetachedCleanup(handle CleanupHandle) DetachedCleanupOutcome {
	dataCh := handle.Data()
	exitCh := handle.Exit()
	ticker := time.NewTicker(detachedCleanupRetryInterval)
	defer ticker.Stop()

	dataClosed := false
	exitObserved := false
	for {
		if dataClosed && exitObserved {
			return DetachedCleanupTerminalExited
		}
		select {
		case _, ok := <-dataCh:
			if !ok {
				dataCh = nil
				dataClosed = true
			}
		case _, ok := <-exitCh:
			exitCh = nil
			if ok {
				exitObserved = true
			}
		case <-ticker.C:
			if err := handle.Close(); err == nil {
				return DetachedCleanupCloseSucceeded
			}
		}
	}
}
