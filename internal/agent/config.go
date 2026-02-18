package agent

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/szaher/try/proxer/internal/protocol"
)

type Config struct {
	GatewayBaseURL       string
	AgentToken           string
	AgentID              string
	HeartbeatInterval    time.Duration
	RequestTimeout       time.Duration
	PollWait             time.Duration
	Tunnels              []protocol.TunnelConfig
	PairToken            string
	ConnectorID          string
	ConnectorSecret      string
	MaxResponseBodyBytes int64
	ProxyURL             string
	NoProxy              string
	TLSSkipVerify        bool
	CAFile               string
	LogLevel             string
	EventHook            RuntimeEventHook
}

func LoadConfigFromEnv() (Config, error) {
	agentID := readEnv("PROXER_AGENT_ID", "local-agent")
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(agentID) == "local-agent" && strings.TrimSpace(host) != "" {
		agentID = host
	}

	cfg := Config{
		GatewayBaseURL:       readEnv("PROXER_GATEWAY_BASE_URL", "http://gateway:8080"),
		AgentToken:           readEnv("PROXER_AGENT_TOKEN", "dev-agent-token"),
		AgentID:              agentID,
		HeartbeatInterval:    10 * time.Second,
		RequestTimeout:       45 * time.Second,
		PollWait:             25 * time.Second,
		PairToken:            readEnv("PROXER_AGENT_PAIR_TOKEN", ""),
		ConnectorID:          readEnv("PROXER_AGENT_CONNECTOR_ID", ""),
		ConnectorSecret:      readEnv("PROXER_AGENT_CONNECTOR_SECRET", ""),
		MaxResponseBodyBytes: 20 << 20,
		ProxyURL:             readEnv("PROXER_AGENT_PROXY_URL", ""),
		NoProxy:              readEnv("PROXER_AGENT_NO_PROXY", ""),
		TLSSkipVerify:        false,
		CAFile:               readEnv("PROXER_AGENT_CA_FILE", ""),
		LogLevel:             readEnv("PROXER_AGENT_LOG_LEVEL", "info"),
	}
	if tlsSkipVerifyRaw := strings.TrimSpace(os.Getenv("PROXER_AGENT_TLS_SKIP_VERIFY")); tlsSkipVerifyRaw != "" {
		parsed, err := strconv.ParseBool(tlsSkipVerifyRaw)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_AGENT_TLS_SKIP_VERIFY: %w", err)
		}
		cfg.TLSSkipVerify = parsed
	}

	if heartbeatStr := strings.TrimSpace(os.Getenv("PROXER_HEARTBEAT_INTERVAL")); heartbeatStr != "" {
		heartbeat, err := time.ParseDuration(heartbeatStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_HEARTBEAT_INTERVAL: %w", err)
		}
		cfg.HeartbeatInterval = heartbeat
	}

	if timeoutStr := strings.TrimSpace(os.Getenv("PROXER_AGENT_REQUEST_TIMEOUT")); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_AGENT_REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = timeout
	}

	if pollStr := strings.TrimSpace(os.Getenv("PROXER_AGENT_POLL_WAIT")); pollStr != "" {
		pollWait, err := time.ParseDuration(pollStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_AGENT_POLL_WAIT: %w", err)
		}
		cfg.PollWait = pollWait
	}

	if maxRespBodyStr := strings.TrimSpace(os.Getenv("PROXER_MAX_RESPONSE_BODY_BYTES")); maxRespBodyStr != "" {
		value, err := strconv.ParseInt(maxRespBodyStr, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_MAX_RESPONSE_BODY_BYTES: %w", err)
		}
		cfg.MaxResponseBodyBytes = value
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		return Config{}, fmt.Errorf("PROXER_MAX_RESPONSE_BODY_BYTES must be > 0")
	}

	parsedURL, err := url.Parse(cfg.GatewayBaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("parse PROXER_GATEWAY_BASE_URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return Config{}, fmt.Errorf("PROXER_GATEWAY_BASE_URL must use http or https")
	}
	if strings.TrimSpace(cfg.ProxyURL) != "" {
		if _, err := url.Parse(cfg.ProxyURL); err != nil {
			return Config{}, fmt.Errorf("parse PROXER_AGENT_PROXY_URL: %w", err)
		}
	}
	if strings.TrimSpace(cfg.CAFile) != "" {
		if _, err := os.Stat(cfg.CAFile); err != nil {
			return Config{}, fmt.Errorf("check PROXER_AGENT_CA_FILE: %w", err)
		}
	}

	isConnectorMode := strings.TrimSpace(cfg.PairToken) != "" ||
		(strings.TrimSpace(cfg.ConnectorID) != "" && strings.TrimSpace(cfg.ConnectorSecret) != "")

	if isConnectorMode {
		if strings.TrimSpace(cfg.PairToken) == "" {
			if strings.TrimSpace(cfg.ConnectorID) == "" || strings.TrimSpace(cfg.ConnectorSecret) == "" {
				return Config{}, fmt.Errorf("connector mode requires PROXER_AGENT_PAIR_TOKEN or both PROXER_AGENT_CONNECTOR_ID and PROXER_AGENT_CONNECTOR_SECRET")
			}
		}
		if tunnelsRaw := strings.TrimSpace(os.Getenv("PROXER_AGENT_TUNNELS")); tunnelsRaw != "" {
			tunnels, err := parseTunnels(tunnelsRaw)
			if err != nil {
				return Config{}, err
			}
			cfg.Tunnels = tunnels
		}
		return cfg, nil
	}

	tunnels, err := parseTunnels(readEnv("PROXER_AGENT_TUNNELS", "app3000=http://host.docker.internal:3000"))
	if err != nil {
		return Config{}, err
	}
	cfg.Tunnels = tunnels

	if strings.TrimSpace(cfg.AgentToken) == "" {
		return Config{}, fmt.Errorf("PROXER_AGENT_TOKEN cannot be empty")
	}

	return cfg, nil
}

func parseTunnels(raw string) ([]protocol.TunnelConfig, error) {
	entries := strings.Split(raw, ",")
	tunnels := make([]protocol.TunnelConfig, 0, len(entries))
	seen := make(map[string]struct{})

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tunnel format %q; expected id=url or id@token=url", entry)
		}

		lhs := strings.TrimSpace(parts[0])
		rhs := strings.TrimSpace(parts[1])

		id := lhs
		token := ""
		if at := strings.Index(lhs, "@"); at > 0 {
			id = strings.TrimSpace(lhs[:at])
			token = strings.TrimSpace(lhs[at+1:])
		}

		if id == "" {
			return nil, fmt.Errorf("tunnel id cannot be empty in %q", entry)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate tunnel id %q", id)
		}
		seen[id] = struct{}{}

		if _, err := url.ParseRequestURI(rhs); err != nil {
			return nil, fmt.Errorf("invalid tunnel target for %q: %w", id, err)
		}

		tunnels = append(tunnels, protocol.TunnelConfig{
			ID:     id,
			Target: rhs,
			Token:  token,
		})
	}

	if len(tunnels) == 0 {
		return nil, fmt.Errorf("at least one tunnel must be configured")
	}

	sort.Slice(tunnels, func(i, j int) bool {
		return tunnels[i].ID < tunnels[j].ID
	})
	return tunnels, nil
}

func readEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
