package agent

import "time"

const (
	RuntimeStateStopped  = "stopped"
	RuntimeStateStarting = "starting"
	RuntimeStateRunning  = "running"
	RuntimeStatePairing  = "pairing"
	RuntimeStateDegraded = "degraded"
	RuntimeStateError    = "error"
	RuntimeStateStopping = "stopping"
)

type RuntimeEvent struct {
	State     string    `json:"state"`
	Message   string    `json:"message,omitempty"`
	Error     string    `json:"error,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	At        time.Time `json:"at"`
}

type RuntimeEventHook func(RuntimeEvent)

