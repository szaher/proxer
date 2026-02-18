package gateway

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr           string
	TLSListenAddr        string
	AgentToken           string
	PublicBaseURL        string
	RequestTimeout       time.Duration
	ProxyRequestTimeout  time.Duration
	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64
	MaxPendingPerSession int
	MaxPendingGlobal     int
	PairTokenTTL         time.Duration
	AdminUsername        string
	AdminPassword        string
	SuperAdminUsername   string
	SuperAdminPassword   string
	SessionTTL           time.Duration
	StorageDriver        string
	SQLitePath           string
	TLSKeyEncryptionKey  string
	DevMode              bool
	MemberWriteEnabled   bool
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:           readEnv("PROXER_LISTEN_ADDR", ":8080"),
		TLSListenAddr:        strings.TrimSpace(os.Getenv("PROXER_TLS_LISTEN_ADDR")),
		AgentToken:           readEnv("PROXER_AGENT_TOKEN", "dev-agent-token"),
		PublicBaseURL:        readEnv("PROXER_PUBLIC_BASE_URL", "http://localhost:8080"),
		RequestTimeout:       30 * time.Second,
		ProxyRequestTimeout:  30 * time.Second,
		MaxRequestBodyBytes:  10 << 20,
		MaxResponseBodyBytes: 20 << 20,
		MaxPendingPerSession: 1024,
		MaxPendingGlobal:     10000,
		PairTokenTTL:         10 * time.Minute,
		AdminUsername:        readEnv("PROXER_ADMIN_USER", "admin"),
		AdminPassword:        readEnv("PROXER_ADMIN_PASSWORD", "admin123"),
		SuperAdminUsername:   strings.TrimSpace(os.Getenv("PROXER_SUPER_ADMIN_USER")),
		SuperAdminPassword:   strings.TrimSpace(os.Getenv("PROXER_SUPER_ADMIN_PASSWORD")),
		SessionTTL:           24 * time.Hour,
		StorageDriver:        readEnv("PROXER_STORAGE_DRIVER", "sqlite"),
		SQLitePath:           readEnv("PROXER_SQLITE_PATH", "/data/proxer.db"),
		TLSKeyEncryptionKey:  strings.TrimSpace(os.Getenv("PROXER_TLS_KEY_ENCRYPTION_KEY")),
		DevMode:              readEnvBool("PROXER_DEV_MODE", true),
		MemberWriteEnabled:   readEnvBool("PROXER_MEMBER_WRITE_ENABLED", true),
	}

	if timeoutStr := strings.TrimSpace(os.Getenv("PROXER_REQUEST_TIMEOUT")); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = timeout
	}
	if timeoutStr := strings.TrimSpace(os.Getenv("PROXER_PROXY_REQUEST_TIMEOUT")); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_PROXY_REQUEST_TIMEOUT: %w", err)
		}
		cfg.ProxyRequestTimeout = timeout
	}
	if sessionTTLStr := strings.TrimSpace(os.Getenv("PROXER_SESSION_TTL")); sessionTTLStr != "" {
		sessionTTL, err := time.ParseDuration(sessionTTLStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_SESSION_TTL: %w", err)
		}
		cfg.SessionTTL = sessionTTL
	}
	if pairTokenTTLStr := strings.TrimSpace(os.Getenv("PROXER_PAIR_TOKEN_TTL")); pairTokenTTLStr != "" {
		ttl, err := time.ParseDuration(pairTokenTTLStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_PAIR_TOKEN_TTL: %w", err)
		}
		cfg.PairTokenTTL = ttl
	}
	if maxReqBodyStr := strings.TrimSpace(os.Getenv("PROXER_MAX_REQUEST_BODY_BYTES")); maxReqBodyStr != "" {
		value, err := strconv.ParseInt(maxReqBodyStr, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_MAX_REQUEST_BODY_BYTES: %w", err)
		}
		cfg.MaxRequestBodyBytes = value
	}
	if maxRespBodyStr := strings.TrimSpace(os.Getenv("PROXER_MAX_RESPONSE_BODY_BYTES")); maxRespBodyStr != "" {
		value, err := strconv.ParseInt(maxRespBodyStr, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_MAX_RESPONSE_BODY_BYTES: %w", err)
		}
		cfg.MaxResponseBodyBytes = value
	}
	if maxSessionPendingStr := strings.TrimSpace(os.Getenv("PROXER_MAX_PENDING_PER_SESSION")); maxSessionPendingStr != "" {
		value, err := strconv.Atoi(maxSessionPendingStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_MAX_PENDING_PER_SESSION: %w", err)
		}
		cfg.MaxPendingPerSession = value
	}
	if maxGlobalPendingStr := strings.TrimSpace(os.Getenv("PROXER_MAX_PENDING_GLOBAL")); maxGlobalPendingStr != "" {
		value, err := strconv.Atoi(maxGlobalPendingStr)
		if err != nil {
			return Config{}, fmt.Errorf("parse PROXER_MAX_PENDING_GLOBAL: %w", err)
		}
		cfg.MaxPendingGlobal = value
	}

	if strings.TrimSpace(cfg.AgentToken) == "" {
		return Config{}, fmt.Errorf("PROXER_AGENT_TOKEN cannot be empty")
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return Config{}, fmt.Errorf("PROXER_ADMIN_PASSWORD cannot be empty")
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		return Config{}, fmt.Errorf("PROXER_MAX_REQUEST_BODY_BYTES must be > 0")
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		return Config{}, fmt.Errorf("PROXER_MAX_RESPONSE_BODY_BYTES must be > 0")
	}
	if cfg.MaxPendingPerSession <= 0 {
		return Config{}, fmt.Errorf("PROXER_MAX_PENDING_PER_SESSION must be > 0")
	}
	if cfg.MaxPendingGlobal <= 0 {
		return Config{}, fmt.Errorf("PROXER_MAX_PENDING_GLOBAL must be > 0")
	}
	if cfg.StorageDriver != "memory" && cfg.StorageDriver != "sqlite" {
		return Config{}, fmt.Errorf("PROXER_STORAGE_DRIVER must be memory or sqlite")
	}
	if strings.TrimSpace(cfg.SuperAdminUsername) == "" {
		cfg.SuperAdminUsername = cfg.AdminUsername
	}
	if strings.TrimSpace(cfg.SuperAdminPassword) == "" {
		cfg.SuperAdminPassword = cfg.AdminPassword
	}
	if !cfg.DevMode && (strings.TrimSpace(cfg.SuperAdminUsername) == "" || strings.TrimSpace(cfg.SuperAdminPassword) == "") {
		return Config{}, fmt.Errorf("PROXER_SUPER_ADMIN_USER and PROXER_SUPER_ADMIN_PASSWORD are required when PROXER_DEV_MODE=false")
	}
	return cfg, nil
}

func readEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func readEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
