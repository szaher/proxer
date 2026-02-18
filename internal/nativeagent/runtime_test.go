package nativeagent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/szaher/try/proxer/internal/agent"
)

func TestRuntimeManagerSubscribeReceivesEvents(t *testing.T) {
	t.Parallel()
	manager := NewRuntimeManager(filepath.Join(t.TempDir(), "status.json"), filepath.Join(t.TempDir(), "agent.log"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := manager.Subscribe(ctx, 8)

	select {
	case initial := <-events:
		if initial.State != RuntimeStateStopped {
			t.Fatalf("initial state = %q, want %q", initial.State, RuntimeStateStopped)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for initial event")
	}

	profile := AgentProfile{ID: "p1", Name: "dev", AgentID: "a1", Mode: ModeConnector}
	manager.handleAgentEvent(profile, agent.RuntimeEvent{
		State:     RuntimeStateRunning,
		Message:   "running",
		SessionID: "s-1",
		At:        time.Now().UTC(),
	})

	select {
	case event := <-events:
		if event.State != RuntimeStateRunning {
			t.Fatalf("event state = %q, want %q", event.State, RuntimeStateRunning)
		}
		if event.SessionID != "s-1" {
			t.Fatalf("session id = %q, want %q", event.SessionID, "s-1")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runtime event")
	}
}
