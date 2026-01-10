// Package state provides state classification for Claude Code agent sessions.
package state

import "fmt"

// State represents the classified agent state.
type State string

const (
	// StateComplete indicates the agent has finished its task successfully.
	StateComplete State = "complete"

	// StateWaiting indicates the agent is waiting for user input.
	StateWaiting State = "waiting"

	// StateStopped indicates the agent was stopped by the user.
	StateStopped State = "stopped"

	// StateIdle indicates the agent has been idle and is prompting for input.
	StateIdle State = "idle"
)

// String returns the string representation of the state.
func (s State) String() string {
	return string(s)
}

// IsValid checks if the state is a recognized value.
func (s State) IsValid() bool {
	switch s {
	case StateComplete, StateWaiting, StateStopped, StateIdle:
		return true
	default:
		return false
	}
}

// ParseState converts a string to a State, returning an error if invalid.
func ParseState(s string) (State, error) {
	state := State(s)
	if !state.IsValid() {
		return "", fmt.Errorf("invalid state: %q", s)
	}
	return state, nil
}

// Description returns a human-readable description of the state.
func (s State) Description() string {
	switch s {
	case StateComplete:
		return "Task completed successfully"
	case StateWaiting:
		return "Waiting for user input"
	case StateStopped:
		return "Stopped by user"
	case StateIdle:
		return "Idle - prompting for input"
	default:
		return "Unknown state"
	}
}
