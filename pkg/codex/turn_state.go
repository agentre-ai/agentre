package codex

import (
	"errors"
	"fmt"
	"sync"
)

// TurnState is the app-server turn lifecycle owned by one Stream. Session and
// process lifetimes are intentionally separate: a terminal turn can leave its
// persistent app-server process reusable for the next turn.
type TurnState string

const (
	TurnStateStarting     TurnState = "starting"
	TurnStateRunning      TurnState = "running"
	TurnStateWaiting      TurnState = "waiting"
	TurnStateInterrupting TurnState = "interrupting"
	TurnStateCompleted    TurnState = "completed"
	TurnStateFailed       TurnState = "failed"
	TurnStateCanceled     TurnState = "canceled"
)

var (
	ErrInvalidTurnTransition = errors.New("codex: invalid turn state transition")
	ErrTurnTerminal          = errors.New("codex: turn already terminal")
)

type turnStateMachine struct {
	mu    sync.RWMutex
	state TurnState
}

func newTurnStateMachine() *turnStateMachine {
	return &turnStateMachine{state: TurnStateStarting}
}

func (m *turnStateMachine) State() TurnState {
	if m == nil {
		return TurnStateFailed
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *turnStateMachine) Terminal() bool {
	return isTerminalTurnState(m.State())
}

// Transition applies the only legal state transitions. Repeated delivery of
// the same state is idempotent; a conflicting terminal or late progress frame
// is diagnostic and cannot rewrite the terminal result.
func (m *turnStateMachine) Transition(next TurnState) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("%w: nil state machine", ErrInvalidTurnTransition)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.state
	if current == next {
		return false, nil
	}
	if isTerminalTurnState(current) {
		return false, fmt.Errorf("%w: %s -> %s", ErrTurnTerminal, current, next)
	}
	if !legalTurnTransition(current, next) {
		return false, fmt.Errorf("%w: %s -> %s", ErrInvalidTurnTransition, current, next)
	}
	m.state = next
	return true, nil
}

func legalTurnTransition(current, next TurnState) bool {
	switch current {
	case TurnStateStarting:
		switch next {
		case TurnStateRunning, TurnStateWaiting, TurnStateInterrupting,
			TurnStateCompleted, TurnStateFailed, TurnStateCanceled:
			return true
		}
	case TurnStateRunning:
		switch next {
		case TurnStateWaiting, TurnStateInterrupting,
			TurnStateCompleted, TurnStateFailed, TurnStateCanceled:
			return true
		}
	case TurnStateWaiting:
		switch next {
		case TurnStateRunning, TurnStateInterrupting,
			TurnStateCompleted, TurnStateFailed, TurnStateCanceled:
			return true
		}
	case TurnStateInterrupting:
		switch next {
		case TurnStateCompleted, TurnStateFailed, TurnStateCanceled:
			return true
		}
	}
	return false
}

func isTerminalTurnState(state TurnState) bool {
	switch state {
	case TurnStateCompleted, TurnStateFailed, TurnStateCanceled:
		return true
	default:
		return false
	}
}
