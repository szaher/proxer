package nativeagent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/szaher/try/proxer/internal/protocol"
)

type fakeSecretStore struct {
	values map[string]string
}

type fixedErrorSecretStore struct {
	getErr error
}

func (s *fakeSecretStore) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *fakeSecretStore) Get(ctx context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (s *fakeSecretStore) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func (s *fixedErrorSecretStore) Set(ctx context.Context, key, value string) error {
	return nil
}

func (s *fixedErrorSecretStore) Get(ctx context.Context, key string) (string, error) {
	return "", s.getErr
}

func (s *fixedErrorSecretStore) Delete(ctx context.Context, key string) error {
	return nil
}

func newTestService(t *testing.T) (*Service, *fakeSecretStore) {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "settings.json"))
	secrets := &fakeSecretStore{values: map[string]string{}}
	runtime := NewRuntimeManager(filepath.Join(dir, "status.json"), filepath.Join(dir, "agent.log"))
	service := NewServiceWithDependencies(store, secrets, runtime, filepath.Join(dir, "status.json"), filepath.Join(dir, "agent.log"))
	return service, secrets
}

func newTestServiceWithSecretStore(t *testing.T, secretStore SecretStore) *Service {
	t.Helper()
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "settings.json"))
	runtime := NewRuntimeManager(filepath.Join(dir, "status.json"), filepath.Join(dir, "agent.log"))
	return NewServiceWithDependencies(store, secretStore, runtime, filepath.Join(dir, "status.json"), filepath.Join(dir, "agent.log"))
}

func TestServiceCreateUseDeleteProfile(t *testing.T) {
	t.Parallel()
	service, secrets := newTestService(t)

	created, err := service.CreateProfile(ProfileInput{
		Name:           "dev",
		GatewayBaseURL: "http://127.0.0.1:18080",
		AgentID:        "agent-1",
		Mode:           ModeLegacyTunnels,
		LegacyTunnels:  "app3000=http://127.0.0.1:3000",
		AgentToken:     "legacy-token",
		Runtime: RuntimeOptions{
			RequestTimeout:       "45s",
			PollWait:             "25s",
			HeartbeatInterval:    "10s",
			MaxResponseBodyBytes: 20 << 20,
			LogLevel:             "info",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected profile id to be set")
	}
	if created.AgentTokenRef.Key == "" {
		t.Fatalf("expected agent token secret ref")
	}
	if got := secrets.values[created.AgentTokenRef.Key]; got != "legacy-token" {
		t.Fatalf("agent token secret = %q, want %q", got, "legacy-token")
	}

	active, err := service.SetActiveProfile(created.ID)
	if err != nil {
		t.Fatalf("SetActiveProfile() error = %v", err)
	}
	if active.ID != created.ID {
		t.Fatalf("active profile id = %q, want %q", active.ID, created.ID)
	}

	profiles, err := service.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}

	if err := service.DeleteProfile(created.ID); err != nil {
		t.Fatalf("DeleteProfile() error = %v", err)
	}
	profiles, err = service.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles() after delete error = %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("expected no profiles after delete, got %d", len(profiles))
	}
}

func TestServicePairProfile(t *testing.T) {
	original := pairWithGatewayExchange
	defer func() {
		pairWithGatewayExchange = original
	}()
	pairWithGatewayExchange = func(ctx context.Context, gatewayBaseURL, agentID, pairToken string) (protocol.PairAgentResponse, error) {
		return protocol.PairAgentResponse{
			ConnectorID:     "conn-1",
			ConnectorSecret: "conn-secret",
			TenantID:        "tenant-a",
		}, nil
	}

	service, secrets := newTestService(t)

	created, err := service.CreateProfile(ProfileInput{
		Name:           "connector",
		GatewayBaseURL: "http://127.0.0.1:18080",
		AgentID:        "agent-2",
		Mode:           ModeConnector,
		Runtime: RuntimeOptions{
			RequestTimeout:       "45s",
			PollWait:             "25s",
			HeartbeatInterval:    "10s",
			MaxResponseBodyBytes: 20 << 20,
			LogLevel:             "info",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}

	updated, err := service.PairProfile(created.ID, "pair-token")
	if err != nil {
		t.Fatalf("PairProfile() error = %v", err)
	}
	if updated.ConnectorID != "conn-1" {
		t.Fatalf("ConnectorID = %q, want %q", updated.ConnectorID, "conn-1")
	}
	if updated.Mode != ModeConnector {
		t.Fatalf("Mode = %q, want %q", updated.Mode, ModeConnector)
	}
	if got := secrets.values[updated.ConnectorSecretRef.Key]; got != "conn-secret" {
		t.Fatalf("connector secret = %q, want %q", got, "conn-secret")
	}
}

func TestReadLogTailLines(t *testing.T) {
	t.Parallel()
	logPath := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(logPath, []byte("line1\nline2\nline3\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	lines, err := readLogTailLines(logPath, 2)
	if err != nil {
		t.Fatalf("readLogTailLines() error = %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("len(lines) = %d, want 2", len(lines))
	}
	if lines[0] != "line2" || lines[1] != "line3" {
		t.Fatalf("unexpected lines = %#v", lines)
	}
}

func TestServiceStartConnectorSecretUnavailableMapsToRemediation(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSecretStore(t, &fixedErrorSecretStore{
		getErr: fmt.Errorf("keychain call failed: %w", ErrSecretUnavailable),
	})

	profile, err := service.CreateProfile(ProfileInput{
		Name:           "connector-err",
		GatewayBaseURL: "http://127.0.0.1:18080",
		AgentID:        "agent-err",
		Mode:           ModeConnector,
		Runtime: RuntimeOptions{
			RequestTimeout:       "45s",
			PollWait:             "25s",
			HeartbeatInterval:    "10s",
			MaxResponseBodyBytes: 20 << 20,
			LogLevel:             "info",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}

	err = service.Start(profile.ID)
	if err == nil {
		t.Fatalf("Start() error = nil, want secret store remediation error")
	}
	if !strings.Contains(err.Error(), "system secret store unavailable") {
		t.Fatalf("Start() error = %q, want system secret store unavailable message", err.Error())
	}
}

func TestServiceStartConnectorSecretMissingMapsToPairHint(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSecretStore(t, &fixedErrorSecretStore{
		getErr: ErrSecretNotFound,
	})

	profile, err := service.CreateProfile(ProfileInput{
		Name:           "connector-missing",
		GatewayBaseURL: "http://127.0.0.1:18080",
		AgentID:        "agent-missing",
		Mode:           ModeConnector,
		Runtime: RuntimeOptions{
			RequestTimeout:       "45s",
			PollWait:             "25s",
			HeartbeatInterval:    "10s",
			MaxResponseBodyBytes: 20 << 20,
			LogLevel:             "info",
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}

	err = service.Start(profile.ID)
	if err == nil {
		t.Fatalf("Start() error = nil, want missing secret error")
	}
	if !strings.Contains(err.Error(), "pair profile again") {
		t.Fatalf("Start() error = %q, want pair profile again hint", err.Error())
	}
}
