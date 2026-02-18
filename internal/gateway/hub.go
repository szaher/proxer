package gateway

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/szaher/try/proxer/internal/protocol"
)

var (
	ErrUnknownSession          = errors.New("unknown agent session")
	ErrTunnelNotConnected      = errors.New("tunnel not connected")
	ErrConnectorNotConnected   = errors.New("connector not connected")
	ErrAgentQueueFull          = errors.New("agent queue is full")
	ErrGlobalBackpressure      = errors.New("gateway is under backpressure")
	ErrProxyRequestTimeout     = errors.New("proxy request timed out")
	ErrUnknownPendingRequest   = errors.New("unknown pending request")
	ErrResponseSessionMismatch = errors.New("response session mismatch")
	ErrResponseTunnelMismatch  = errors.New("response tunnel mismatch")
)

type TunnelMetrics struct {
	TunnelID         string    `json:"tunnel_id"`
	RequestCount     int64     `json:"request_count"`
	ErrorCount       int64     `json:"error_count"`
	BytesIn          int64     `json:"bytes_in"`
	BytesOut         int64     `json:"bytes_out"`
	TotalLatencyMs   int64     `json:"total_latency_ms"`
	AverageLatencyMs float64   `json:"average_latency_ms"`
	LastStatus       int       `json:"last_status"`
	LastError        string    `json:"last_error,omitempty"`
	LastSeen         time.Time `json:"last_seen,omitempty"`
}

type TunnelSnapshot struct {
	ID            string             `json:"id"`
	Target        string             `json:"target"`
	RequiresToken bool               `json:"requires_token"`
	AgentID       string             `json:"agent_id"`
	PublicURL     string             `json:"public_url"`
	Metrics       TunnelMetrics      `json:"metrics"`
	Connection    ConnectionSnapshot `json:"connection"`
}

type ConnectionSnapshot struct {
	Connected bool `json:"connected"`
}

type ConnectorConnection struct {
	ConnectorID string    `json:"connector_id"`
	AgentID     string    `json:"agent_id"`
	Connected   bool      `json:"connected"`
	LastSeen    time.Time `json:"last_seen"`
}

type session struct {
	id          string
	agentID     string
	tunnels     map[string]protocol.TunnelConfig
	connectorID string
	queue       chan *protocol.ProxyRequest
	lastSeen    time.Time
}

type dispatchResult struct {
	response *protocol.ProxyResponse
	err      error
}

type pendingRequest struct {
	requestID string
	sessionID string
	tunnelID  string
	resultCh  chan dispatchResult
}

type Hub struct {
	agentToken           string
	publicBaseURL        string
	requestTimeout       time.Duration
	sessionTTL           time.Duration
	maxPendingPerSession int
	maxPendingGlobal     int

	mu                sync.RWMutex
	sessions          map[string]*session
	tunnelSessions    map[string]string
	connectorSessions map[string]string
	configs           map[string]protocol.TunnelConfig
	pending           map[string]pendingRequest
	metrics           map[string]*TunnelMetrics
	latencySamples    []int64

	requestCounter uint64
	sessionCounter uint64
}

type HubStatus struct {
	ActiveSessions       int     `json:"active_sessions"`
	ActiveTunnelSessions int     `json:"active_tunnel_sessions"`
	ActiveConnectors     int     `json:"active_connectors"`
	PendingRequests      int     `json:"pending_requests"`
	MaxPendingGlobal     int     `json:"max_pending_global"`
	MaxPendingPerSession int     `json:"max_pending_per_session"`
	QueueDepthTotal      int     `json:"queue_depth_total"`
	QueueDepthMax        int     `json:"queue_depth_max"`
	P50LatencyMs         int64   `json:"p50_latency_ms"`
	P95LatencyMs         int64   `json:"p95_latency_ms"`
	RequestCount         int64   `json:"request_count"`
	ErrorCount           int64   `json:"error_count"`
	ErrorRate            float64 `json:"error_rate"`
}

func NewHub(agentToken, publicBaseURL string, requestTimeout time.Duration, maxPendingPerSession, maxPendingGlobal int) *Hub {
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}
	if maxPendingPerSession <= 0 {
		maxPendingPerSession = 1024
	}
	if maxPendingGlobal <= 0 {
		maxPendingGlobal = 10000
	}

	return &Hub{
		agentToken:           agentToken,
		publicBaseURL:        strings.TrimRight(publicBaseURL, "/"),
		requestTimeout:       requestTimeout,
		sessionTTL:           90 * time.Second,
		maxPendingPerSession: maxPendingPerSession,
		maxPendingGlobal:     maxPendingGlobal,
		sessions:             make(map[string]*session),
		tunnelSessions:       make(map[string]string),
		connectorSessions:    make(map[string]string),
		configs:              make(map[string]protocol.TunnelConfig),
		pending:              make(map[string]pendingRequest),
		metrics:              make(map[string]*TunnelMetrics),
		latencySamples:       make([]int64, 0, 512),
	}
}

func (h *Hub) RequestTimeout() time.Duration {
	return h.requestTimeout
}

func (h *Hub) Register(message *protocol.RegisterRequest) (*protocol.RegisterResponse, error) {
	if message == nil {
		return nil, errors.New("missing registration payload")
	}
	if strings.TrimSpace(message.Token) != h.agentToken {
		return nil, errors.New("agent token mismatch")
	}

	sanitized := make([]protocol.TunnelConfig, 0, len(message.Tunnels))
	for _, tunnel := range message.Tunnels {
		id := strings.TrimSpace(tunnel.ID)
		target := strings.TrimSpace(tunnel.Target)
		if id == "" || target == "" {
			continue
		}
		sanitized = append(sanitized, protocol.TunnelConfig{
			ID:     id,
			Target: target,
			Token:  strings.TrimSpace(tunnel.Token),
		})
	}
	if len(sanitized) == 0 {
		return nil, errors.New("at least one valid tunnel is required")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	agentID := strings.TrimSpace(message.AgentID)
	if agentID == "" {
		agentID = "anonymous-agent"
	}

	for sessionID, existing := range h.sessions {
		if existing.agentID == agentID {
			h.removeSessionLocked(sessionID)
		}
	}

	sessionID := h.nextSessionID()
	s := &session{
		id:       sessionID,
		agentID:  agentID,
		tunnels:  make(map[string]protocol.TunnelConfig),
		queue:    make(chan *protocol.ProxyRequest, h.maxPendingPerSession),
		lastSeen: time.Now().UTC(),
	}
	h.sessions[sessionID] = s

	routes := make([]protocol.TunnelRoute, 0, len(sanitized))
	for _, tunnel := range sanitized {
		if oldSessionID, ok := h.tunnelSessions[tunnel.ID]; ok && oldSessionID != sessionID {
			h.removeTunnelFromSessionLocked(oldSessionID, tunnel.ID)
		}
		h.tunnelSessions[tunnel.ID] = sessionID
		h.configs[tunnel.ID] = tunnel
		s.tunnels[tunnel.ID] = tunnel
		if _, ok := h.metrics[tunnel.ID]; !ok {
			h.metrics[tunnel.ID] = &TunnelMetrics{TunnelID: tunnel.ID}
		}
		routes = append(routes, protocol.TunnelRoute{
			ID:        tunnel.ID,
			PublicURL: fmt.Sprintf("%s/t/%s/", h.publicBaseURL, tunnel.ID),
		})
	}

	sort.Slice(routes, func(i, j int) bool {
		return routes[i].ID < routes[j].ID
	})

	return &protocol.RegisterResponse{
		Accepted:      true,
		Message:       "registered",
		SessionID:     sessionID,
		PublicBaseURL: h.publicBaseURL,
		Tunnels:       routes,
	}, nil
}

func (h *Hub) RegisterConnectorSession(connectorID, agentID string) (*protocol.RegisterResponse, error) {
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return nil, errors.New("missing connector id")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "connector-agent"
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	for sessionID, existing := range h.sessions {
		if existing.agentID == agentID {
			h.removeSessionLocked(sessionID)
		}
	}
	if existingSessionID, ok := h.connectorSessions[connectorID]; ok {
		h.removeSessionLocked(existingSessionID)
	}

	sessionID := h.nextSessionID()
	s := &session{
		id:          sessionID,
		agentID:     agentID,
		connectorID: connectorID,
		tunnels:     make(map[string]protocol.TunnelConfig),
		queue:       make(chan *protocol.ProxyRequest, h.maxPendingPerSession),
		lastSeen:    time.Now().UTC(),
	}
	h.sessions[sessionID] = s
	h.connectorSessions[connectorID] = sessionID

	return &protocol.RegisterResponse{
		Accepted:      true,
		Message:       "registered connector session",
		SessionID:     sessionID,
		PublicBaseURL: h.publicBaseURL,
	}, nil
}

func (h *Hub) PullRequest(ctx context.Context, sessionID string) (*protocol.ProxyRequest, error) {
	h.mu.Lock()
	h.cleanupStaleLocked(time.Now().UTC())
	s, ok := h.sessions[sessionID]
	if !ok {
		h.mu.Unlock()
		return nil, ErrUnknownSession
	}
	s.lastSeen = time.Now().UTC()
	queue := s.queue
	h.mu.Unlock()

	select {
	case request := <-queue:
		return request, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *Hub) Heartbeat(sessionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())
	s, ok := h.sessions[sessionID]
	if !ok {
		return ErrUnknownSession
	}
	s.lastSeen = time.Now().UTC()
	return nil
}

func (h *Hub) SubmitProxyResponse(sessionID string, response *protocol.ProxyResponse) error {
	if response == nil {
		return errors.New("missing response payload")
	}
	requestID := strings.TrimSpace(response.RequestID)
	if requestID == "" {
		return errors.New("missing request_id")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	s, ok := h.sessions[sessionID]
	if !ok {
		return ErrUnknownSession
	}
	s.lastSeen = time.Now().UTC()

	pending, ok := h.pending[requestID]
	if !ok {
		return ErrUnknownPendingRequest
	}
	if pending.sessionID != sessionID {
		return ErrResponseSessionMismatch
	}
	if strings.TrimSpace(response.TunnelID) != pending.tunnelID {
		return ErrResponseTunnelMismatch
	}

	delete(h.pending, requestID)
	h.recordSuccessfulAttemptLocked(response)
	pending.resultCh <- dispatchResult{response: response}
	return nil
}

func (h *Hub) GetTunnelToken(tunnelID string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	cfg, ok := h.configs[tunnelID]
	if !ok {
		return ""
	}
	return cfg.Token
}

func (h *Hub) IsTunnelConnected(tunnelID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())
	_, ok := h.tunnelSessions[tunnelID]
	return ok
}

func (h *Hub) IsConnectorConnected(connectorID string) bool {
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())
	_, ok := h.connectorSessions[connectorID]
	return ok
}

func (h *Hub) GetConnectorConnection(connectorID string) (ConnectorConnection, bool) {
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return ConnectorConnection{}, false
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	sessionID, ok := h.connectorSessions[connectorID]
	if !ok {
		return ConnectorConnection{
			ConnectorID: connectorID,
			Connected:   false,
		}, false
	}
	s, ok := h.sessions[sessionID]
	if !ok {
		delete(h.connectorSessions, connectorID)
		return ConnectorConnection{
			ConnectorID: connectorID,
			Connected:   false,
		}, false
	}
	return ConnectorConnection{
		ConnectorID: connectorID,
		AgentID:     s.agentID,
		Connected:   true,
		LastSeen:    s.lastSeen,
	}, true
}

func (h *Hub) EnsureTunnelMetric(tunnelID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.metrics[tunnelID]; !ok {
		h.metrics[tunnelID] = &TunnelMetrics{TunnelID: tunnelID}
	}
}

func (h *Hub) GetTunnelMetrics(tunnelID string) TunnelMetrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())
	return h.copyMetricLocked(tunnelID)
}

func (h *Hub) RecordProxyFailure(tunnelID string, bytesIn int64, errMsg string) {
	h.recordFailedAttempt(tunnelID, bytesIn, errMsg)
}

func (h *Hub) RecordProxyResponse(response *protocol.ProxyResponse) {
	if response == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recordSuccessfulAttemptLocked(response)
}

func (h *Hub) DispatchProxyRequest(ctx context.Context, tunnelID string, req *protocol.ProxyRequest) (*protocol.ProxyResponse, error) {
	if req == nil {
		return nil, errors.New("missing proxy request")
	}

	h.mu.Lock()
	h.cleanupStaleLocked(time.Now().UTC())
	sessionID, ok := h.tunnelSessions[tunnelID]
	if !ok {
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "tunnel not connected")
		return nil, ErrTunnelNotConnected
	}
	session, ok := h.sessions[sessionID]
	if !ok {
		delete(h.tunnelSessions, tunnelID)
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "tunnel session unavailable")
		return nil, ErrTunnelNotConnected
	}
	requestID, resultCh, err := h.enqueueDispatchLocked(sessionID, session, tunnelID, req)
	if err != nil {
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), err.Error())
		return nil, err
	}
	requestQueue := session.queue
	h.mu.Unlock()

	return h.waitForProxyResponse(ctx, tunnelID, requestID, requestQueue, req, resultCh)
}

func (h *Hub) DispatchProxyRequestToConnector(ctx context.Context, connectorID, tunnelID string, req *protocol.ProxyRequest) (*protocol.ProxyResponse, error) {
	if req == nil {
		return nil, errors.New("missing proxy request")
	}
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return nil, errors.New("missing connector id")
	}

	h.mu.Lock()
	h.cleanupStaleLocked(time.Now().UTC())
	sessionID, ok := h.connectorSessions[connectorID]
	if !ok {
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "connector not connected")
		return nil, ErrConnectorNotConnected
	}
	session, ok := h.sessions[sessionID]
	if !ok {
		delete(h.connectorSessions, connectorID)
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "connector session unavailable")
		return nil, ErrConnectorNotConnected
	}
	requestID, resultCh, err := h.enqueueDispatchLocked(sessionID, session, tunnelID, req)
	if err != nil {
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), err.Error())
		return nil, err
	}
	requestQueue := session.queue
	h.mu.Unlock()

	return h.waitForProxyResponse(ctx, tunnelID, requestID, requestQueue, req, resultCh)
}

func (h *Hub) SnapshotTunnels() []TunnelSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	snapshots := make([]TunnelSnapshot, 0, len(h.tunnelSessions))
	for tunnelID, sessionID := range h.tunnelSessions {
		session, ok := h.sessions[sessionID]
		if !ok {
			continue
		}
		cfg := h.configs[tunnelID]
		metric := h.copyMetricLocked(tunnelID)
		snapshots = append(snapshots, TunnelSnapshot{
			ID:            tunnelID,
			Target:        cfg.Target,
			RequiresToken: cfg.Token != "",
			AgentID:       session.agentID,
			PublicURL:     fmt.Sprintf("%s/t/%s/", h.publicBaseURL, tunnelID),
			Metrics:       metric,
			Connection: ConnectionSnapshot{
				Connected: true,
			},
		})
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].ID < snapshots[j].ID
	})
	return snapshots
}

func (h *Hub) Status() HubStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupStaleLocked(time.Now().UTC())

	status := HubStatus{
		ActiveSessions:       len(h.sessions),
		ActiveTunnelSessions: len(h.tunnelSessions),
		ActiveConnectors:     len(h.connectorSessions),
		PendingRequests:      len(h.pending),
		MaxPendingGlobal:     h.maxPendingGlobal,
		MaxPendingPerSession: h.maxPendingPerSession,
	}

	for _, s := range h.sessions {
		depth := len(s.queue)
		status.QueueDepthTotal += depth
		if depth > status.QueueDepthMax {
			status.QueueDepthMax = depth
		}
	}

	for _, metric := range h.metrics {
		status.RequestCount += metric.RequestCount
		status.ErrorCount += metric.ErrorCount
	}
	if status.RequestCount > 0 {
		status.ErrorRate = float64(status.ErrorCount) / float64(status.RequestCount)
	}

	if len(h.latencySamples) > 0 {
		ordered := make([]int64, len(h.latencySamples))
		copy(ordered, h.latencySamples)
		sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
		status.P50LatencyMs = percentileValue(ordered, 50)
		status.P95LatencyMs = percentileValue(ordered, 95)
	}

	return status
}

func (h *Hub) enqueueDispatchLocked(sessionID string, session *session, tunnelID string, req *protocol.ProxyRequest) (string, chan dispatchResult, error) {
	if len(h.pending) >= h.maxPendingGlobal {
		return "", nil, ErrGlobalBackpressure
	}
	if len(session.queue) >= h.maxPendingPerSession {
		return "", nil, ErrAgentQueueFull
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = h.nextRequestID()
	}
	req.RequestID = requestID
	req.TunnelID = tunnelID

	resultCh := make(chan dispatchResult, 1)
	h.pending[requestID] = pendingRequest{
		requestID: requestID,
		sessionID: sessionID,
		tunnelID:  tunnelID,
		resultCh:  resultCh,
	}
	return requestID, resultCh, nil
}

func (h *Hub) waitForProxyResponse(
	ctx context.Context,
	tunnelID, requestID string,
	requestQueue chan *protocol.ProxyRequest,
	req *protocol.ProxyRequest,
	resultCh chan dispatchResult,
) (*protocol.ProxyResponse, error) {
	select {
	case requestQueue <- req:
	default:
		h.mu.Lock()
		delete(h.pending, requestID)
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "agent queue is full")
		return nil, ErrAgentQueueFull
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			h.recordFailedAttempt(tunnelID, int64(len(req.Body)), result.err.Error())
			return nil, result.err
		}
		if result.response == nil {
			h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "nil proxy response")
			return nil, errors.New("received nil proxy response")
		}
		return result.response, nil
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, requestID)
		h.mu.Unlock()
		h.recordFailedAttempt(tunnelID, int64(len(req.Body)), "timeout waiting for agent response")
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrProxyRequestTimeout
		}
		return nil, ctx.Err()
	}
}

func (h *Hub) nextRequestID() string {
	value := atomic.AddUint64(&h.requestCounter, 1)
	return fmt.Sprintf("req-%d-%d", time.Now().UnixNano(), value)
}

func (h *Hub) nextSessionID() string {
	value := atomic.AddUint64(&h.sessionCounter, 1)
	return fmt.Sprintf("sess-%d-%d", time.Now().UnixNano(), value)
}

func (h *Hub) recordFailedAttempt(tunnelID string, bytesIn int64, errMsg string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	metric, ok := h.metrics[tunnelID]
	if !ok {
		metric = &TunnelMetrics{TunnelID: tunnelID}
		h.metrics[tunnelID] = metric
	}
	metric.RequestCount++
	metric.ErrorCount++
	metric.BytesIn += bytesIn
	metric.LastStatus = 502
	metric.LastError = errMsg
	metric.LastSeen = time.Now().UTC()
	if metric.RequestCount > 0 {
		metric.AverageLatencyMs = float64(metric.TotalLatencyMs) / float64(metric.RequestCount)
	}
}

func (h *Hub) recordSuccessfulAttemptLocked(response *protocol.ProxyResponse) {
	metric, ok := h.metrics[response.TunnelID]
	if !ok {
		metric = &TunnelMetrics{TunnelID: response.TunnelID}
		h.metrics[response.TunnelID] = metric
	}
	metric.RequestCount++
	if response.Error != "" || response.Status >= 500 {
		metric.ErrorCount++
	}
	metric.BytesIn += response.BytesIn
	metric.BytesOut += response.BytesOut
	metric.TotalLatencyMs += response.LatencyMs
	metric.LastStatus = response.Status
	metric.LastError = response.Error
	metric.LastSeen = time.Now().UTC()
	if metric.RequestCount > 0 {
		metric.AverageLatencyMs = float64(metric.TotalLatencyMs) / float64(metric.RequestCount)
	}
	if response.LatencyMs > 0 {
		h.appendLatencyLocked(response.LatencyMs)
	}
}

func (h *Hub) copyMetricLocked(tunnelID string) TunnelMetrics {
	metric, ok := h.metrics[tunnelID]
	if !ok {
		return TunnelMetrics{TunnelID: tunnelID}
	}
	copied := *metric
	return copied
}

func (h *Hub) cleanupStaleLocked(now time.Time) {
	for sessionID, s := range h.sessions {
		if now.Sub(s.lastSeen) > h.sessionTTL {
			h.removeSessionLocked(sessionID)
		}
	}
}

func (h *Hub) removeSessionLocked(sessionID string) {
	s, ok := h.sessions[sessionID]
	if !ok {
		return
	}

	for tunnelID := range s.tunnels {
		if owner, exists := h.tunnelSessions[tunnelID]; exists && owner == sessionID {
			delete(h.tunnelSessions, tunnelID)
			delete(h.configs, tunnelID)
		}
	}
	if s.connectorID != "" {
		if owner, exists := h.connectorSessions[s.connectorID]; exists && owner == sessionID {
			delete(h.connectorSessions, s.connectorID)
		}
	}
	delete(h.sessions, sessionID)

	for requestID, pending := range h.pending {
		if pending.sessionID != sessionID {
			continue
		}
		delete(h.pending, requestID)
		select {
		case pending.resultCh <- dispatchResult{err: ErrUnknownSession}:
		default:
		}
	}
}

func (h *Hub) removeTunnelFromSessionLocked(sessionID, tunnelID string) {
	s, ok := h.sessions[sessionID]
	if !ok {
		return
	}
	delete(s.tunnels, tunnelID)
	if owner, exists := h.tunnelSessions[tunnelID]; exists && owner == sessionID {
		delete(h.tunnelSessions, tunnelID)
		delete(h.configs, tunnelID)
	}
}

func (h *Hub) appendLatencyLocked(latencyMs int64) {
	const maxSamples = 512
	if len(h.latencySamples) >= maxSamples {
		copy(h.latencySamples, h.latencySamples[1:])
		h.latencySamples = h.latencySamples[:maxSamples-1]
	}
	h.latencySamples = append(h.latencySamples, latencyMs)
}

func percentileValue(sorted []int64, percentile int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	index := int(float64(percentile) / 100.0 * float64(len(sorted)-1))
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
