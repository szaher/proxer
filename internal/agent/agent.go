package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/szaher/try/proxer/internal/httpx"
	"github.com/szaher/try/proxer/internal/protocol"
)

var errSessionExpired = errors.New("agent session expired")

type Agent struct {
	cfg        Config
	logger     *log.Logger
	httpClient *http.Client
	tunnels    map[string]protocol.TunnelConfig
	eventHook  RuntimeEventHook

	sessionMu sync.RWMutex
	sessionID string
}

func New(cfg Config, logger *log.Logger) *Agent {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	tunnelMap := make(map[string]protocol.TunnelConfig, len(cfg.Tunnels))
	for _, tunnel := range cfg.Tunnels {
		tunnelMap[tunnel.ID] = tunnel
	}

	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}
	if proxyURL := strings.TrimSpace(cfg.ProxyURL); proxyURL != "" {
		if parsedProxyURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsedProxyURL)
		}
	}
	if strings.TrimSpace(cfg.NoProxy) != "" {
		_ = os.Setenv("NO_PROXY", strings.TrimSpace(cfg.NoProxy))
	}
	if cfg.TLSSkipVerify || strings.TrimSpace(cfg.CAFile) != "" {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		if cfg.TLSSkipVerify {
			tlsConfig.InsecureSkipVerify = true
		}
		if caFile := strings.TrimSpace(cfg.CAFile); caFile != "" {
			if pemData, err := os.ReadFile(caFile); err == nil {
				pool := x509.NewCertPool()
				if pool.AppendCertsFromPEM(pemData) {
					tlsConfig.RootCAs = pool
				}
			}
		}
		transport.TLSClientConfig = tlsConfig
	}

	return &Agent{
		cfg:    cfg,
		logger: logger,
		httpClient: &http.Client{
			Transport: transport,
		},
		tunnels:   tunnelMap,
		eventHook: cfg.EventHook,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	a.emit(RuntimeStateStarting, "agent starting", nil)
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go a.heartbeatLoop(ctx, heartbeatDone)

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			a.emit(RuntimeStateStopping, "agent stopping", nil)
			a.emit(RuntimeStateStopped, "agent stopped", nil)
			return nil
		}

		if a.getSessionID() == "" {
			if err := a.register(ctx); err != nil {
				a.logger.Printf("agent registration failed: %v", err)
				a.emit(RuntimeStateDegraded, "registration failed", err)
				if err := waitWithContext(ctx, backoff); err != nil {
					a.emit(RuntimeStateStopping, "agent stopping", nil)
					a.emit(RuntimeStateStopped, "agent stopped", nil)
					return nil
				}
				if backoff < 10*time.Second {
					backoff *= 2
				}
				continue
			}
			backoff = time.Second
			a.emit(RuntimeStateRunning, "agent registered", nil)
		}

		err := a.pullAndProcess(ctx)
		if err == nil {
			backoff = time.Second
			continue
		}

		if errors.Is(err, errSessionExpired) {
			a.logger.Printf("session expired; re-registering")
			a.emit(RuntimeStateDegraded, "session expired", err)
			a.setSessionID("")
			continue
		}

		a.logger.Printf("agent poll loop error: %v", err)
		a.emit(RuntimeStateDegraded, "poll loop error", err)
		if err := waitWithContext(ctx, backoff); err != nil {
			a.emit(RuntimeStateStopping, "agent stopping", nil)
			a.emit(RuntimeStateStopped, "agent stopped", nil)
			return nil
		}
		if backoff < 10*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) register(ctx context.Context) error {
	if err := a.ensureConnectorCredentials(ctx); err != nil {
		return err
	}

	registerReq := protocol.RegisterRequest{
		AgentID: a.cfg.AgentID,
	}
	if a.isConnectorMode() {
		registerReq.ConnectorID = a.cfg.ConnectorID
		registerReq.ConnectorSecret = a.cfg.ConnectorSecret
	} else {
		registerReq.Token = a.cfg.AgentToken
		registerReq.Tunnels = a.cfg.Tunnels
	}

	requestBody, err := json.Marshal(registerReq)
	if err != nil {
		return fmt.Errorf("encode register payload: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(a.cfg.GatewayBaseURL, "/")+"/api/agent/register", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("build register request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post register request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("register rejected (status %d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload protocol.RegisterResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}
	if !payload.Accepted || strings.TrimSpace(payload.SessionID) == "" {
		return errors.New("register response did not include an active session")
	}

	a.setSessionID(payload.SessionID)
	a.logger.Printf("registered with gateway: session=%s tunnels=%d", payload.SessionID, len(payload.Tunnels))
	return nil
}

func (a *Agent) ensureConnectorCredentials(ctx context.Context) error {
	if strings.TrimSpace(a.cfg.ConnectorID) != "" && strings.TrimSpace(a.cfg.ConnectorSecret) != "" {
		return nil
	}
	pairToken := strings.TrimSpace(a.cfg.PairToken)
	if pairToken == "" {
		return nil
	}
	a.emit(RuntimeStatePairing, "pairing connector", nil)

	requestBody, err := json.Marshal(protocol.PairAgentRequest{
		PairToken: pairToken,
		AgentID:   a.cfg.AgentID,
	})
	if err != nil {
		return fmt.Errorf("encode pair payload: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(a.cfg.GatewayBaseURL, "/")+"/api/agent/pair", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("build pair request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post pair request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("pair rejected (status %d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload protocol.PairAgentResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode pair response: %w", err)
	}
	if strings.TrimSpace(payload.ConnectorID) == "" || strings.TrimSpace(payload.ConnectorSecret) == "" {
		return fmt.Errorf("pair response did not include connector credentials")
	}

	a.cfg.ConnectorID = payload.ConnectorID
	a.cfg.ConnectorSecret = payload.ConnectorSecret
	a.cfg.PairToken = ""
	a.logger.Printf("paired connector %s", payload.ConnectorID)
	a.emit(RuntimeStateRunning, "paired connector", nil)
	return nil
}

func (a *Agent) pullAndProcess(ctx context.Context) error {
	sessionID := a.getSessionID()
	if sessionID == "" {
		return errSessionExpired
	}

	pullURL, err := url.Parse(strings.TrimRight(a.cfg.GatewayBaseURL, "/") + "/api/agent/pull")
	if err != nil {
		return fmt.Errorf("build pull URL: %w", err)
	}
	query := pullURL.Query()
	query.Set("session_id", sessionID)
	query.Set("wait", strconv.Itoa(int(a.cfg.PollWait.Seconds())))
	pullURL.RawQuery = query.Encode()

	requestCtx, cancel := context.WithTimeout(ctx, a.cfg.PollWait+5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, pullURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build pull request: %w", err)
	}

	response, err := a.httpClient.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return fmt.Errorf("pull request failed: %w", err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
		var payload protocol.PullResponse
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			return fmt.Errorf("decode pull response: %w", err)
		}
		if payload.Request == nil {
			return nil
		}
		proxyResp := a.handleProxyRequest(payload.Request)
		if err := a.submitResponse(ctx, sessionID, proxyResp); err != nil {
			return err
		}
		return nil
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return errSessionExpired
	default:
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("pull rejected (status %d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
}

func (a *Agent) submitResponse(ctx context.Context, sessionID string, proxyResp *protocol.ProxyResponse) error {
	requestBody, err := json.Marshal(protocol.SubmitResponseRequest{
		SessionID: sessionID,
		Response:  proxyResp,
	})
	if err != nil {
		return fmt.Errorf("encode submit response payload: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(a.cfg.GatewayBaseURL, "/")+"/api/agent/respond", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("build submit response request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("submit response failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return errSessionExpired
	}
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("submit response rejected (status %d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *Agent) heartbeatLoop(ctx context.Context, done <-chan struct{}) {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			sessionID := a.getSessionID()
			if sessionID == "" {
				continue
			}
			if err := a.sendHeartbeat(ctx, sessionID); err != nil {
				if errors.Is(err, errSessionExpired) {
					a.setSessionID("")
					continue
				}
				a.logger.Printf("heartbeat error: %v", err)
			}
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context, sessionID string) error {
	requestBody, err := json.Marshal(protocol.HeartbeatRequest{
		SessionID: sessionID,
		AgentID:   a.cfg.AgentID,
	})
	if err != nil {
		return fmt.Errorf("encode heartbeat payload: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(a.cfg.GatewayBaseURL, "/")+"/api/agent/heartbeat", bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("build heartbeat request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("post heartbeat request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return errSessionExpired
	}
	if response.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("heartbeat rejected (status %d): %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (a *Agent) handleProxyRequest(proxyReq *protocol.ProxyRequest) *protocol.ProxyResponse {
	start := time.Now()
	response := &protocol.ProxyResponse{
		RequestID: proxyReq.RequestID,
		TunnelID:  proxyReq.TunnelID,
		Status:    http.StatusBadGateway,
		BytesIn:   int64(len(proxyReq.Body)),
	}

	var err error
	targetBase := ""
	if proxyReq.LocalTarget != nil {
		targetBase, err = buildLocalTargetBaseURL(proxyReq.LocalTarget)
		if err != nil {
			response.Status = http.StatusBadRequest
			response.Error = fmt.Sprintf("invalid local target: %v", err)
			response.LatencyMs = time.Since(start).Milliseconds()
			return response
		}
	} else {
		tunnel, ok := a.tunnels[proxyReq.TunnelID]
		if !ok {
			response.Status = http.StatusNotFound
			response.Error = fmt.Sprintf("unknown tunnel id %q", proxyReq.TunnelID)
			response.LatencyMs = time.Since(start).Milliseconds()
			return response
		}
		targetBase = tunnel.Target
	}

	targetURL, err := buildTargetURL(targetBase, proxyReq.Path, proxyReq.Query)
	if err != nil {
		response.Error = fmt.Sprintf("build target URL: %v", err)
		response.LatencyMs = time.Since(start).Milliseconds()
		return response
	}

	requestCtx, cancel := context.WithTimeout(context.Background(), a.cfg.RequestTimeout)
	defer cancel()

	outboundReq, err := http.NewRequestWithContext(requestCtx, proxyReq.Method, targetURL, bytes.NewReader(proxyReq.Body))
	if err != nil {
		response.Error = fmt.Sprintf("construct outbound request: %v", err)
		response.LatencyMs = time.Since(start).Milliseconds()
		return response
	}

	for header, values := range proxyReq.Headers {
		if httpx.IsHopByHopHeader(header) || strings.EqualFold(header, "Host") || strings.EqualFold(header, "Content-Length") {
			continue
		}
		for _, value := range values {
			outboundReq.Header.Add(header, value)
		}
	}
	outboundReq.Header.Set("X-Proxer-Tunnel-ID", proxyReq.TunnelID)
	outboundReq.Header.Set("X-Proxer-Agent-ID", a.cfg.AgentID)
	if requestID := strings.TrimSpace(proxyReq.RequestID); requestID != "" {
		outboundReq.Header.Set("X-Proxer-Request-ID", requestID)
	}

	outboundResp, err := a.httpClient.Do(outboundReq)
	if err != nil {
		response.Error = fmt.Sprintf("forward request to local target: %v", err)
		response.LatencyMs = time.Since(start).Milliseconds()
		return response
	}
	defer outboundResp.Body.Close()

	respBody, err := readAllWithLimit(outboundResp.Body, a.cfg.MaxResponseBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			response.Status = http.StatusRequestEntityTooLarge
			response.Error = "local target response exceeded configured size limit"
			response.LatencyMs = time.Since(start).Milliseconds()
			return response
		}
		response.Error = fmt.Sprintf("read local target response: %v", err)
		response.Status = http.StatusBadGateway
		response.LatencyMs = time.Since(start).Milliseconds()
		return response
	}

	response.Status = outboundResp.StatusCode
	response.Headers = httpx.CloneHTTPHeader(outboundResp.Header)
	response.Body = respBody
	response.BytesOut = int64(len(respBody))
	response.LatencyMs = time.Since(start).Milliseconds()
	return response
}

func (a *Agent) getSessionID() string {
	a.sessionMu.RLock()
	defer a.sessionMu.RUnlock()
	return a.sessionID
}

func (a *Agent) setSessionID(sessionID string) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	a.sessionID = sessionID
}

func buildTargetURL(base, path, query string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if path == "" {
		path = "/"
	}
	relative := &url.URL{Path: path, RawQuery: query}
	resolved := baseURL.ResolveReference(relative)
	return resolved.String(), nil
}

func buildLocalTargetBaseURL(target *protocol.LocalTarget) (string, error) {
	if target == nil {
		return "", fmt.Errorf("missing local target")
	}
	scheme := strings.ToLower(strings.TrimSpace(target.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	host := strings.TrimSpace(target.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	if target.Port < 1 || target.Port > 65535 {
		return "", fmt.Errorf("port must be between 1 and 65535")
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, target.Port), nil
}

func readAllWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return io.ReadAll(reader)
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errBodyTooLarge
	}
	return body, nil
}

func (a *Agent) isConnectorMode() bool {
	return strings.TrimSpace(a.cfg.ConnectorID) != "" && strings.TrimSpace(a.cfg.ConnectorSecret) != ""
}

func (a *Agent) emit(state, message string, err error) {
	if a.eventHook == nil {
		return
	}
	event := RuntimeEvent{
		State:     state,
		Message:   strings.TrimSpace(message),
		AgentID:   strings.TrimSpace(a.cfg.AgentID),
		SessionID: strings.TrimSpace(a.getSessionID()),
		At:        time.Now().UTC(),
	}
	if err != nil {
		event.Error = err.Error()
	}
	a.eventHook(event)
}

var errBodyTooLarge = errors.New("body too large")

func waitWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
