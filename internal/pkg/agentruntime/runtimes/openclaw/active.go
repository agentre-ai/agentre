package openclaw

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/openclawgateway"
)

const gatewayCancelledStatus = "cancelled" //nolint:misspell // OpenClaw may emit this British spelling.

type activeTurn struct {
	runtime *Runtime
	ctx     context.Context
	client  *openclawgateway.Client

	sessionID  int64
	sessionKey string
	runID      string
	out        chan agentruntime.Event
	result     *agentruntime.RunResult

	finishOnce sync.Once
	abortMu    sync.Mutex
	aborted    bool

	approvalMu        sync.Mutex
	approvalResolveMu sync.Mutex
	approvals         map[string]*approvalState
	initialApprovals  []gatewayExecApprovalRecord

	lastAgentSeq int64
	lastChatSeq  int64
	assistant    string
	thinking     string
}

func (a *activeTurn) consume() {
	for _, record := range a.initialApprovals {
		a.handleApprovalRequested(record)
	}
	for {
		select {
		case event := <-a.client.Events():
			if a.handleGatewayEvent(event) {
				a.reconcile()
			}
		case <-a.client.Gaps():
			a.reconcileApprovals()
			a.reconcile()
		case <-a.client.Ready():
			a.reconcileApprovals()
			a.reconcile()
		case <-a.client.Errors():
			// A transport error is not terminal. Client reconnects; Ready then
			// triggers agent.wait. Never resubmit the user message here.
		case <-a.ctx.Done():
			a.abortMu.Lock()
			aborted := a.aborted
			a.abortMu.Unlock()
			if aborted {
				a.finish(agentruntime.ErrAborted)
			} else {
				a.finish(a.ctx.Err())
			}
			return
		}
		if a.finished() {
			return
		}
	}
}

func (a *activeTurn) abort(ctx context.Context) error {
	a.abortMu.Lock()
	if a.aborted {
		a.abortMu.Unlock()
		return nil
	}
	a.aborted = true
	a.abortMu.Unlock()
	var response struct {
		OK      bool `json:"ok"`
		Aborted bool `json:"aborted"`
	}
	err := a.client.Call(ctx, "chat.abort", map[string]any{
		"sessionKey": a.sessionKey,
		"runId":      a.runID,
	}, &response)
	if err != nil {
		return err
	}
	return nil
}

func (a *activeTurn) reconcile() {
	if a.finished() {
		return
	}
	var response struct {
		RunID      string `json:"runId"`
		Status     string `json:"status"`
		Error      string `json:"error"`
		StopReason string `json:"stopReason"`
	}
	err := a.client.Call(a.ctx, "agent.wait", map[string]any{
		"runId":     a.runID,
		"timeoutMs": 0,
	}, &response)
	if err != nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "ok", "completed", "done":
		a.finish(nil)
	case "error", "failed":
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = "OpenClaw run failed"
		}
		a.finish(errors.New(message))
	case "aborted", gatewayCancelledStatus:
		a.finish(agentruntime.ErrAborted)
	}
}

func (a *activeTurn) emit(event agentruntime.Event) bool {
	select {
	case a.out <- event:
		return true
	case <-a.ctx.Done():
		return false
	}
}

func (a *activeTurn) finish(stopErr error) {
	a.finishOnce.Do(func() {
		a.expirePendingApprovals()
		a.result.StopErr = stopErr
		switch {
		case stopErr == nil:
			a.emit(agentruntime.Done{})
		case errors.Is(stopErr, agentruntime.ErrAborted):
			a.emit(agentruntime.Done{})
		default:
			a.emit(agentruntime.ErrorEvent{Err: stopErr})
		}
		a.runtime.unregister(a)
		close(a.out)
		a.client.Close()
	})
}

func (a *activeTurn) finished() bool {
	a.runtime.mu.RLock()
	active := a.runtime.active[a.sessionID]
	a.runtime.mu.RUnlock()
	return active != a
}

func eventError(prefix, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "unknown error"
	}
	return fmt.Errorf("%s: %s", prefix, message)
}
