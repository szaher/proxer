package nativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/szaher/try/proxer/internal/agent"
)

type RuntimeManager struct {
	statusPath string
	logPath    string

	mu      sync.RWMutex
	state   NativeStatusSnapshot
	cancel  context.CancelFunc
	doneCh  chan struct{}
	lastErr error
	running bool

	nextSubscriberID int
	subscribers      map[int]chan NativeStatusSnapshot
}

func NewRuntimeManager(statusPath, logPath string) *RuntimeManager {
	return &RuntimeManager{
		statusPath: statusPath,
		logPath:    logPath,
		state: NativeStatusSnapshot{
			State:     RuntimeStateStopped,
			UpdatedAt: time.Now().UTC(),
			PID:       os.Getpid(),
		},
		subscribers: map[int]chan NativeStatusSnapshot{},
	}
}

func (m *RuntimeManager) Start(profile AgentProfile, connectorSecret, agentToken string) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("agent runtime already running")
	}

	cfg, err := profileToAgentConfig(profile, connectorSecret, agentToken)
	if err != nil {
		m.mu.Unlock()
		return err
	}

	startedAt := time.Now().UTC()
	m.state = NativeStatusSnapshot{
		State:       RuntimeStateStarting,
		Message:     "runtime starting",
		ProfileID:   profile.ID,
		ProfileName: profile.Name,
		AgentID:     profile.AgentID,
		Mode:        profile.Mode,
		PID:         os.Getpid(),
		UpdatedAt:   startedAt,
		StartedAt:   &startedAt,
	}
	_ = writeStatusSnapshot(m.statusPath, m.state)
	m.broadcastLocked(cloneStatusSnapshot(m.state))

	logWriter, closeLog, err := openRuntimeLogWriter(m.logPath)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	logger := log.New(logWriter, "[agent] ", log.LstdFlags|log.Lmicroseconds)

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.doneCh = make(chan struct{})
	m.lastErr = nil
	m.running = true

	cfg.EventHook = func(ev agent.RuntimeEvent) {
		m.handleAgentEvent(profile, ev)
	}
	client := agent.New(cfg, logger)
	m.mu.Unlock()

	go func() {
		defer close(m.doneCh)
		defer closeLog()
		err := client.Run(ctx)

		m.mu.Lock()
		defer m.mu.Unlock()
		m.running = false
		m.cancel = nil
		m.lastErr = err
		if err != nil {
			m.state.State = RuntimeStateError
			m.state.Message = "runtime terminated with error"
			m.state.Error = err.Error()
		} else {
			m.state.State = RuntimeStateStopped
			m.state.Message = "runtime stopped"
			m.state.Error = ""
		}
		m.state.UpdatedAt = time.Now().UTC()
		_ = writeStatusSnapshot(m.statusPath, m.state)
		m.broadcastLocked(cloneStatusSnapshot(m.state))
	}()

	return nil
}

func (m *RuntimeManager) Stop(timeout time.Duration) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	doneCh := m.doneCh
	m.state.State = RuntimeStateStopping
	m.state.Message = "runtime stopping"
	m.state.UpdatedAt = time.Now().UTC()
	_ = writeStatusSnapshot(m.statusPath, m.state)
	m.broadcastLocked(cloneStatusSnapshot(m.state))
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	select {
	case <-doneCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for runtime shutdown")
	}
}

func (m *RuntimeManager) Status() NativeStatusSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneStatusSnapshot(m.state)
}

func (m *RuntimeManager) Wait(ctx context.Context) error {
	m.mu.RLock()
	doneCh := m.doneCh
	m.mu.RUnlock()
	if doneCh == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-doneCh:
		m.mu.RLock()
		defer m.mu.RUnlock()
		return m.lastErr
	}
}

func (m *RuntimeManager) handleAgentEvent(profile AgentProfile, ev agent.RuntimeEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.state.State = ev.State
	m.state.Message = ev.Message
	m.state.Error = ev.Error
	m.state.AgentID = profile.AgentID
	m.state.ProfileID = profile.ID
	m.state.ProfileName = profile.Name
	m.state.Mode = profile.Mode
	m.state.SessionID = ev.SessionID
	m.state.UpdatedAt = ev.At.UTC()
	if m.state.StartedAt == nil {
		at := ev.At.UTC()
		m.state.StartedAt = &at
	}
	if m.state.PID == 0 {
		m.state.PID = os.Getpid()
	}
	_ = writeStatusSnapshot(m.statusPath, m.state)
	m.broadcastLocked(cloneStatusSnapshot(m.state))
}

func (m *RuntimeManager) Subscribe(ctx context.Context, buffer int) <-chan NativeStatusSnapshot {
	if buffer <= 0 {
		buffer = 16
	}
	ch := make(chan NativeStatusSnapshot, buffer)

	m.mu.Lock()
	id := m.nextSubscriberID
	m.nextSubscriberID++
	m.subscribers[id] = ch
	current := cloneStatusSnapshot(m.state)
	m.mu.Unlock()

	sendLatestSnapshot(ch, current)

	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.subscribers, id)
		m.mu.Unlock()
		close(ch)
	}()

	return ch
}

func profileToAgentConfig(profile AgentProfile, connectorSecret, agentToken string) (agent.Config, error) {
	profile = applyProfileDefaults(profile)
	if err := validateProfile(profile); err != nil {
		return agent.Config{}, err
	}

	requestTimeout, err := time.ParseDuration(profile.Runtime.RequestTimeout)
	if err != nil {
		return agent.Config{}, fmt.Errorf("parse request_timeout: %w", err)
	}
	pollWait, err := time.ParseDuration(profile.Runtime.PollWait)
	if err != nil {
		return agent.Config{}, fmt.Errorf("parse poll_wait: %w", err)
	}
	heartbeatInterval, err := time.ParseDuration(profile.Runtime.HeartbeatInterval)
	if err != nil {
		return agent.Config{}, fmt.Errorf("parse heartbeat_interval: %w", err)
	}

	cfg := agent.Config{
		GatewayBaseURL:       profile.GatewayBaseURL,
		AgentID:              profile.AgentID,
		HeartbeatInterval:    heartbeatInterval,
		RequestTimeout:       requestTimeout,
		PollWait:             pollWait,
		MaxResponseBodyBytes: profile.Runtime.MaxResponseBodyBytes,
		ProxyURL:             profile.Runtime.ProxyURL,
		NoProxy:              profile.Runtime.NoProxy,
		TLSSkipVerify:        profile.Runtime.TLSSkipVerify,
		CAFile:               profile.Runtime.CAFile,
		LogLevel:             profile.Runtime.LogLevel,
	}

	switch profile.Mode {
	case ModeConnector:
		cfg.ConnectorID = strings.TrimSpace(profile.ConnectorID)
		cfg.ConnectorSecret = strings.TrimSpace(connectorSecret)
		if cfg.ConnectorID == "" || cfg.ConnectorSecret == "" {
			return agent.Config{}, fmt.Errorf("connector mode requires paired connector credentials")
		}
	case ModeLegacyTunnels:
		cfg.AgentToken = strings.TrimSpace(agentToken)
		cfg.Tunnels = profile.LegacyTunnels
		if cfg.AgentToken == "" {
			return agent.Config{}, fmt.Errorf("legacy_tunnels mode requires agent token secret")
		}
		if len(cfg.Tunnels) == 0 {
			return agent.Config{}, fmt.Errorf("legacy_tunnels mode requires at least one tunnel")
		}
	default:
		return agent.Config{}, fmt.Errorf("unsupported mode %q", profile.Mode)
	}

	return cfg, nil
}

func openRuntimeLogWriter(path string) (io.Writer, func(), error) {
	if strings.TrimSpace(path) == "" {
		return os.Stdout, func() {}, nil
	}
	if err := os.MkdirAll(strings.TrimSpace(filepathDir(path)), 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	writer := io.MultiWriter(os.Stdout, file)
	return writer, func() { _ = file.Close() }, nil
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, string(os.PathSeparator))
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

func writeStatusSnapshot(path string, snapshot NativeStatusSnapshot) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(strings.TrimSpace(filepathDir(path)), 0o700); err != nil {
		return fmt.Errorf("create status directory: %w", err)
	}
	snapshot.UpdatedAt = snapshot.UpdatedAt.UTC()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode status snapshot: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write status snapshot temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("persist status snapshot: %w", err)
	}
	return nil
}

func ReadStatusSnapshot(path string) (NativeStatusSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return NativeStatusSnapshot{State: RuntimeStateStopped, UpdatedAt: time.Now().UTC(), PID: os.Getpid()}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NativeStatusSnapshot{State: RuntimeStateStopped, UpdatedAt: time.Now().UTC(), PID: os.Getpid()}, nil
		}
		return NativeStatusSnapshot{}, err
	}
	var snapshot NativeStatusSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return NativeStatusSnapshot{}, err
	}
	if strings.TrimSpace(snapshot.State) == "" {
		snapshot.State = RuntimeStateStopped
	}
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = time.Now().UTC()
	}
	return snapshot, nil
}

func (m *RuntimeManager) broadcastLocked(snapshot NativeStatusSnapshot) {
	for _, subscriber := range m.subscribers {
		sendLatestSnapshot(subscriber, snapshot)
	}
}

func sendLatestSnapshot(ch chan NativeStatusSnapshot, snapshot NativeStatusSnapshot) {
	select {
	case ch <- snapshot:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- snapshot:
	default:
	}
}

func cloneStatusSnapshot(snapshot NativeStatusSnapshot) NativeStatusSnapshot {
	cloned := snapshot
	if snapshot.StartedAt != nil {
		startedAt := *snapshot.StartedAt
		cloned.StartedAt = &startedAt
	}
	return cloned
}
