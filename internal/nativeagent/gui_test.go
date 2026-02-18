package nativeagent

import "testing"

func TestParseProfileRoute(t *testing.T) {
	t.Parallel()
	profile, action, err := parseProfileRoute("/api/profiles/my%20profile/pair")
	if err != nil {
		t.Fatalf("parseProfileRoute() error = %v", err)
	}
	if profile != "my profile" {
		t.Fatalf("profile = %q, want %q", profile, "my profile")
	}
	if action != "pair" {
		t.Fatalf("action = %q, want %q", action, "pair")
	}
}

func TestProfilePayloadToInputTLSFlag(t *testing.T) {
	t.Parallel()
	flag := true
	payload := profilePayload{
		Name:           "p1",
		GatewayBaseURL: "http://127.0.0.1:18080",
		AgentID:        "a1",
		Mode:           ModeConnector,
		Runtime: runtimePayload{
			RequestTimeout:       "45s",
			PollWait:             "25s",
			HeartbeatInterval:    "10s",
			MaxResponseBodyBytes: 20 << 20,
			TLSSkipVerify:        &flag,
			LogLevel:             "info",
		},
	}
	input := payload.toInput()
	if !input.RuntimeTLSSkipVerifySet {
		t.Fatalf("RuntimeTLSSkipVerifySet = false, want true")
	}
	if !input.Runtime.TLSSkipVerify {
		t.Fatalf("Runtime.TLSSkipVerify = false, want true")
	}
}
