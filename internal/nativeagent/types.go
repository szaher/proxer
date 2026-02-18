package nativeagent

import (
	"fmt"
	"strings"
	"time"

	"github.com/szaher/try/proxer/internal/protocol"
)

const (
	SchemaVersion = 1

	ModeConnector     = "connector"
	ModeLegacyTunnels = "legacy_tunnels"

	LaunchModeTrayWindow = "tray_window"

	RuntimeStateStopped  = "stopped"
	RuntimeStateStarting = "starting"
	RuntimeStateRunning  = "running"
	RuntimeStatePairing  = "pairing"
	RuntimeStateDegraded = "degraded"
	RuntimeStateError    = "error"
	RuntimeStateStopping = "stopping"
)

type AgentSettings struct {
	SchemaVersion   int            `json:"schema_version"`
	ActiveProfileID string         `json:"active_profile_id,omitempty"`
	StartAtLogin    bool           `json:"start_at_login"`
	LaunchMode      string         `json:"launch_mode"`
	Profiles        []AgentProfile `json:"profiles"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AgentProfile struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	GatewayBaseURL     string                  `json:"gateway_base_url"`
	AgentID            string                  `json:"agent_id"`
	Mode               string                  `json:"mode"`
	ConnectorID        string                  `json:"connector_id,omitempty"`
	ConnectorSecretRef SecretRef               `json:"connector_secret_ref,omitempty"`
	AgentTokenRef      SecretRef               `json:"agent_token_ref,omitempty"`
	Runtime            RuntimeOptions          `json:"runtime"`
	LegacyTunnels      []protocol.TunnelConfig `json:"legacy_tunnels,omitempty"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

type RuntimeOptions struct {
	RequestTimeout       string `json:"request_timeout"`
	PollWait             string `json:"poll_wait"`
	HeartbeatInterval    string `json:"heartbeat_interval"`
	MaxResponseBodyBytes int64  `json:"max_response_body_bytes"`
	ProxyURL             string `json:"proxy_url,omitempty"`
	NoProxy              string `json:"no_proxy,omitempty"`
	TLSSkipVerify        bool   `json:"tls_skip_verify"`
	CAFile               string `json:"ca_file,omitempty"`
	LogLevel             string `json:"log_level"`
}

type SecretRef struct {
	Key string `json:"key"`
}

type NativeStatusSnapshot struct {
	State       string     `json:"state"`
	Message     string     `json:"message,omitempty"`
	Error       string     `json:"error,omitempty"`
	ProfileID   string     `json:"profile_id,omitempty"`
	ProfileName string     `json:"profile_name,omitempty"`
	AgentID     string     `json:"agent_id,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	Mode        string     `json:"mode,omitempty"`
	PID         int        `json:"pid"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
}

type UpdateCheckResult struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	DownloadURL    string `json:"download_url,omitempty"`
	Message        string `json:"message"`
}

func defaultSettings() AgentSettings {
	now := time.Now().UTC()
	return AgentSettings{
		SchemaVersion: SchemaVersion,
		LaunchMode:    LaunchModeTrayWindow,
		Profiles:      []AgentProfile{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func defaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{
		RequestTimeout:       (45 * time.Second).String(),
		PollWait:             (25 * time.Second).String(),
		HeartbeatInterval:    (10 * time.Second).String(),
		MaxResponseBodyBytes: 20 << 20,
		LogLevel:             "info",
	}
}

func applyProfileDefaults(p AgentProfile) AgentProfile {
	if strings.TrimSpace(p.Mode) == "" {
		p.Mode = ModeConnector
	}
	if strings.TrimSpace(p.GatewayBaseURL) == "" {
		p.GatewayBaseURL = "http://127.0.0.1:18080"
	}
	if strings.TrimSpace(p.AgentID) == "" {
		p.AgentID = "local-agent"
	}
	if strings.TrimSpace(p.Runtime.RequestTimeout) == "" {
		p.Runtime.RequestTimeout = (45 * time.Second).String()
	}
	if strings.TrimSpace(p.Runtime.PollWait) == "" {
		p.Runtime.PollWait = (25 * time.Second).String()
	}
	if strings.TrimSpace(p.Runtime.HeartbeatInterval) == "" {
		p.Runtime.HeartbeatInterval = (10 * time.Second).String()
	}
	if p.Runtime.MaxResponseBodyBytes <= 0 {
		p.Runtime.MaxResponseBodyBytes = 20 << 20
	}
	if strings.TrimSpace(p.Runtime.LogLevel) == "" {
		p.Runtime.LogLevel = "info"
	}
	if strings.TrimSpace(p.ID) != "" && strings.TrimSpace(p.ConnectorSecretRef.Key) == "" {
		p.ConnectorSecretRef = SecretRef{Key: secretKeyForProfile(p.ID, "connector_secret")}
	}
	if strings.TrimSpace(p.ID) != "" && strings.TrimSpace(p.AgentTokenRef.Key) == "" {
		p.AgentTokenRef = SecretRef{Key: secretKeyForProfile(p.ID, "agent_token")}
	}
	return p
}

func validateProfile(p AgentProfile) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("profile name is required")
	}
	if strings.TrimSpace(p.GatewayBaseURL) == "" {
		return fmt.Errorf("gateway_base_url is required")
	}
	if strings.TrimSpace(p.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	mode := strings.TrimSpace(p.Mode)
	if mode != ModeConnector && mode != ModeLegacyTunnels {
		return fmt.Errorf("mode must be %q or %q", ModeConnector, ModeLegacyTunnels)
	}
	if mode == ModeLegacyTunnels && len(p.LegacyTunnels) == 0 {
		return fmt.Errorf("legacy_tunnels mode requires at least one tunnel")
	}
	if p.Runtime.MaxResponseBodyBytes <= 0 {
		return fmt.Errorf("max_response_body_bytes must be > 0")
	}
	if _, err := time.ParseDuration(p.Runtime.RequestTimeout); err != nil {
		return fmt.Errorf("invalid request_timeout: %w", err)
	}
	if _, err := time.ParseDuration(p.Runtime.PollWait); err != nil {
		return fmt.Errorf("invalid poll_wait: %w", err)
	}
	if _, err := time.ParseDuration(p.Runtime.HeartbeatInterval); err != nil {
		return fmt.Errorf("invalid heartbeat_interval: %w", err)
	}
	return nil
}
