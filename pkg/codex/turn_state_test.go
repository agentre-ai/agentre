package codex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurnStateMachine_Behavior(t *testing.T) {
	t.Run("Given a new turn when work waits and resumes then only legal non-terminal states are entered", func(t *testing.T) {
		machine := newTurnStateMachine()
		assert.Equal(t, TurnStateStarting, machine.State())

		for _, next := range []TurnState{
			TurnStateRunning,
			TurnStateWaiting,
			TurnStateRunning,
			TurnStateInterrupting,
			TurnStateCanceled,
		} {
			changed, err := machine.Transition(next)
			require.NoError(t, err)
			assert.True(t, changed)
			assert.Equal(t, next, machine.State())
		}
		assert.True(t, machine.Terminal())
	})

	t.Run("Given a terminal turn when the same terminal is repeated then it is idempotent", func(t *testing.T) {
		machine := newTurnStateMachine()
		_, err := machine.Transition(TurnStateRunning)
		require.NoError(t, err)
		_, err = machine.Transition(TurnStateCompleted)
		require.NoError(t, err)

		changed, err := machine.Transition(TurnStateCompleted)

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Equal(t, TurnStateCompleted, machine.State())
	})

	t.Run("Given a terminal turn when late progress or another terminal arrives then state cannot be rewritten", func(t *testing.T) {
		for _, next := range []TurnState{TurnStateRunning, TurnStateWaiting, TurnStateFailed, TurnStateCanceled} {
			machine := newTurnStateMachine()
			_, err := machine.Transition(TurnStateRunning)
			require.NoError(t, err)
			_, err = machine.Transition(TurnStateCompleted)
			require.NoError(t, err)

			changed, err := machine.Transition(next)

			assert.False(t, changed, next)
			assert.ErrorIs(t, err, ErrTurnTerminal, next)
			assert.Equal(t, TurnStateCompleted, machine.State(), next)
		}
	})

	t.Run("Given out-of-order states when an impossible backward transition arrives then it is diagnosed", func(t *testing.T) {
		machine := newTurnStateMachine()
		_, err := machine.Transition(TurnStateRunning)
		require.NoError(t, err)

		changed, err := machine.Transition(TurnStateStarting)

		assert.False(t, changed)
		assert.ErrorIs(t, err, ErrInvalidTurnTransition)
		assert.Equal(t, TurnStateRunning, machine.State())
	})
}
