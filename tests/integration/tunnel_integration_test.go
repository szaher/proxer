package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/szaher/try/proxer/internal/agent"
	"github.com/szaher/try/proxer/internal/gateway"
	"github.com/szaher/try/proxer/internal/protocol"
)

func TestGatewayRoutesMultiplePortsAndTracksMetrics(t *testing.T) {
	targetOne := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"service":"one","path":"%s","query":"%s"}`,
			r.URL.Path, r.URL.RawQuery)))
	}))
	defer targetOne.Close(t)

	targetTwo := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"service":"two","path":"%s","query":"%s"}`,
			r.URL.Path, r.URL.RawQuery)))
	}))
	defer targetTwo.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayLogger := log.New(io.Discard, "", 0)
	gatewayServer := gateway.NewServer(gatewayCfg, gatewayLogger)

	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}

	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	agentCfg := agent.Config{
		GatewayBaseURL:    fmt.Sprintf("http://%s", gatewayAddr),
		AgentToken:        "test-token",
		AgentID:           "integration-agent",
		HeartbeatInterval: 200 * time.Millisecond,
		RequestTimeout:    5 * time.Second,
		PollWait:          1 * time.Second,
		Tunnels: []protocol.TunnelConfig{
			{ID: "app3000", Target: targetOne.URL},
			{ID: "app5173", Target: targetTwo.URL},
		},
	}

	agentClient := agent.New(agentCfg, log.New(io.Discard, "", 0))
	agentErrCh := make(chan error, 1)
	go func() {
		agentErrCh <- agentClient.Run(ctx)
	}()

	if err := waitForTunnelCount(authedClient, fmt.Sprintf("http://%s/api/tunnels", gatewayAddr), 2, 8*time.Second); err != nil {
		t.Fatalf("tunnels were not registered: %v", err)
	}

	respOneBody := mustProxyRequest(t, fmt.Sprintf("http://%s/t/app3000/hello?x=1&access_token=agent-token", gatewayAddr), "one")
	if !strings.Contains(respOneBody, `"path":"/hello"`) {
		t.Fatalf("unexpected response payload for app3000: %s", respOneBody)
	}
	if !strings.Contains(respOneBody, `"query":"x=1&access_token=agent-token"`) {
		t.Fatalf("expected query string passthrough for app3000, got: %s", respOneBody)
	}

	respTwoBody := mustProxyRequest(t, fmt.Sprintf("http://%s/t/app5173/status?y=2", gatewayAddr), "two")
	if !strings.Contains(respTwoBody, `"path":"/status"`) {
		t.Fatalf("unexpected response payload for app5173: %s", respTwoBody)
	}

	tunnelsResponse := fetchTunnelResponse(t, authedClient, fmt.Sprintf("http://%s/api/tunnels", gatewayAddr))
	metricsByID := map[string]float64{}
	for _, tunnel := range tunnelsResponse.Tunnels {
		metricsByID[tunnel.ID] = tunnel.Metrics.AverageLatencyMs
		if tunnel.Metrics.RequestCount == 0 {
			t.Fatalf("expected request_count > 0 for tunnel %s", tunnel.ID)
		}
	}

	if _, ok := metricsByID["app3000"]; !ok {
		t.Fatalf("app3000 metrics not found")
	}
	if _, ok := metricsByID["app5173"]; !ok {
		t.Fatalf("app5173 metrics not found")
	}

	cancel()
	select {
	case err := <-agentErrCh:
		if err != nil {
			t.Fatalf("agent returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for agent shutdown")
	}

	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestGatewayRuleAPIConfiguresDirectForwarding(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Set-Cookie", "upstream_cookie=one; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "upstream_cookie_two=two; Path=/")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "rule",
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"body":    string(requestBody),
			"headers": r.Header,
		})
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	upsertBody, err := json.Marshal(map[string]string{
		"id":     "rule3000",
		"target": target.URL,
		"token":  "",
	})
	if err != nil {
		t.Fatalf("marshal rule payload: %v", err)
	}

	resp, err := authedClient.Post(fmt.Sprintf("http://%s/api/rules", gatewayAddr), "application/json", bytes.NewReader(upsertBody))
	if err != nil {
		t.Fatalf("post /api/rules failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("unexpected status from /api/rules: %d body=%s", resp.StatusCode, string(body))
	}
	_ = resp.Body.Close()

	reqBody := `{"k":"v","n":123}`
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/t/rule3000/check?ok=1&ok=2&access_token=keepme", gatewayAddr), bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("build proxied request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Cookie", "session=abc123; pref=dark")
	req.Header.Add("X-Custom-Header", "alpha")
	req.Header.Add("X-Custom-Header", "beta")

	proxyResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("perform proxied request: %v", err)
	}
	defer proxyResp.Body.Close()

	if proxyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(proxyResp.Body)
		t.Fatalf("unexpected proxied status: %d body=%s", proxyResp.StatusCode, string(body))
	}

	setCookies := proxyResp.Header.Values("Set-Cookie")
	if len(setCookies) < 2 {
		t.Fatalf("expected upstream Set-Cookie headers to be preserved, got: %v", setCookies)
	}

	var echoed struct {
		Service string              `json:"service"`
		Method  string              `json:"method"`
		Path    string              `json:"path"`
		Query   string              `json:"query"`
		Body    string              `json:"body"`
		Headers map[string][]string `json:"headers"`
	}
	if err := json.NewDecoder(proxyResp.Body).Decode(&echoed); err != nil {
		t.Fatalf("decode proxied echo response: %v", err)
	}

	if echoed.Service != "rule" {
		t.Fatalf("unexpected service value: %s", echoed.Service)
	}
	if echoed.Method != http.MethodPost {
		t.Fatalf("expected POST method, got %s", echoed.Method)
	}
	if echoed.Path != "/check" {
		t.Fatalf("unexpected path: %s", echoed.Path)
	}
	if !strings.Contains(echoed.Query, "ok=1") || !strings.Contains(echoed.Query, "ok=2") || !strings.Contains(echoed.Query, "access_token=keepme") {
		t.Fatalf("query params were not preserved: %s", echoed.Query)
	}
	if echoed.Body != reqBody {
		t.Fatalf("body mismatch: got=%s expected=%s", echoed.Body, reqBody)
	}
	if !containsHeaderValue(echoed.Headers, "Authorization", "Bearer test-token") {
		t.Fatalf("authorization header was not forwarded: %v", echoed.Headers["Authorization"])
	}
	if !containsHeaderValue(echoed.Headers, "Cookie", "session=abc123; pref=dark") {
		t.Fatalf("cookie header was not forwarded: %v", echoed.Headers["Cookie"])
	}
	if !containsHeaderValue(echoed.Headers, "X-Custom-Header", "alpha") || !containsHeaderValue(echoed.Headers, "X-Custom-Header", "beta") {
		t.Fatalf("custom header values were not forwarded: %v", echoed.Headers["X-Custom-Header"])
	}
	if len(echoed.Headers["X-Forwarded-Host"]) == 0 {
		t.Fatalf("X-Forwarded-Host was not set")
	}

	tunnelsResponse := fetchTunnelResponse(t, authedClient, fmt.Sprintf("http://%s/api/tunnels", gatewayAddr))
	found := false
	for _, tunnel := range tunnelsResponse.Tunnels {
		if tunnel.ID == "rule3000" {
			found = true
			if tunnel.Metrics.RequestCount == 0 {
				t.Fatalf("expected request_count > 0 for rule3000")
			}
		}
	}
	if !found {
		t.Fatalf("expected tunnel rule3000 in /api/tunnels")
	}

	deleteReq, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://%s/api/rules/rule3000", gatewayAddr), nil)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	deleteResp, err := authedClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete /api/rules/rule3000 failed: %v", err)
	}
	if deleteResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(deleteResp.Body)
		_ = deleteResp.Body.Close()
		t.Fatalf("unexpected delete status: %d body=%s", deleteResp.StatusCode, string(body))
	}
	_ = deleteResp.Body.Close()

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestMultiTenantRoutesCanReuseSameRouteID(t *testing.T) {
	targetA := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "team-a",
			"tenant":  "team-a",
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
		})
	}))
	defer targetA.Close(t)

	targetB := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "team-b",
			"tenant":  "team-b",
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
		})
	}))
	defer targetB.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]string{
		"id":   "team-a",
		"name": "Team A",
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]string{
		"id":   "team-b",
		"name": "Team B",
	}, http.StatusOK)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/team-a/routes", gatewayAddr), map[string]string{
		"id":     "web",
		"target": targetA.URL,
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/team-b/routes", gatewayAddr), map[string]string{
		"id":     "web",
		"target": targetB.URL,
	}, http.StatusOK)

	mustPutJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/team-a/environment", gatewayAddr), map[string]any{
		"scheme":       "http",
		"host":         "host.docker.internal",
		"default_port": 3000,
		"variables": map[string]string{
			"APP_NAME": "team-a-app",
		},
	}, http.StatusOK)

	bodyA := mustProxyRequest(t, fmt.Sprintf("http://%s/t/team-a/web/home?x=1", gatewayAddr), "team-a")
	if !strings.Contains(bodyA, `"path":"/home"`) {
		t.Fatalf("unexpected tenant A response payload: %s", bodyA)
	}

	bodyB := mustProxyRequest(t, fmt.Sprintf("http://%s/t/team-b/web/home?x=2", gatewayAddr), "team-b")
	if !strings.Contains(bodyB, `"path":"/home"`) {
		t.Fatalf("unexpected tenant B response payload: %s", bodyB)
	}

	tenantsResp, err := authedClient.Get(fmt.Sprintf("http://%s/api/tenants", gatewayAddr))
	if err != nil {
		t.Fatalf("get /api/tenants failed: %v", err)
	}
	defer tenantsResp.Body.Close()
	if tenantsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tenantsResp.Body)
		t.Fatalf("unexpected /api/tenants status: %d body=%s", tenantsResp.StatusCode, string(body))
	}

	var tenantsPayload struct {
		Tenants []struct {
			ID         string `json:"id"`
			RouteCount int    `json:"route_count"`
		} `json:"tenants"`
	}
	if err := json.NewDecoder(tenantsResp.Body).Decode(&tenantsPayload); err != nil {
		t.Fatalf("decode /api/tenants payload: %v", err)
	}
	countByTenant := map[string]int{}
	for _, tenant := range tenantsPayload.Tenants {
		countByTenant[tenant.ID] = tenant.RouteCount
	}
	if countByTenant["team-a"] != 1 || countByTenant["team-b"] != 1 {
		t.Fatalf("unexpected tenant route counts: %+v", countByTenant)
	}

	envResp, err := authedClient.Get(fmt.Sprintf("http://%s/api/tenants/team-a/environment", gatewayAddr))
	if err != nil {
		t.Fatalf("get /api/tenants/team-a/environment failed: %v", err)
	}
	defer envResp.Body.Close()
	if envResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(envResp.Body)
		t.Fatalf("unexpected environment status: %d body=%s", envResp.StatusCode, string(body))
	}

	var envPayload struct {
		Environment struct {
			Host      string            `json:"host"`
			Variables map[string]string `json:"variables"`
		} `json:"environment"`
	}
	if err := json.NewDecoder(envResp.Body).Decode(&envPayload); err != nil {
		t.Fatalf("decode environment payload: %v", err)
	}
	if envPayload.Environment.Host != "host.docker.internal" {
		t.Fatalf("unexpected environment host: %s", envPayload.Environment.Host)
	}
	if envPayload.Environment.Variables["APP_NAME"] != "team-a-app" {
		t.Fatalf("unexpected environment variable map: %+v", envPayload.Environment.Variables)
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestConnectorPairingCreatesSessionAndRoutesToLocalhostTarget(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "connector",
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
		})
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]string{
		"id":   "team-connector",
		"name": "Team Connector",
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/connectors", gatewayAddr), map[string]string{
		"id":        "conn-a",
		"name":      "Connector A",
		"tenant_id": "team-connector",
	}, http.StatusCreated)

	pairReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/connectors/conn-a/pair", gatewayAddr), nil)
	if err != nil {
		t.Fatalf("build pair request: %v", err)
	}
	pairResp, err := authedClient.Do(pairReq)
	if err != nil {
		t.Fatalf("pair connector failed: %v", err)
	}
	defer pairResp.Body.Close()
	if pairResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(pairResp.Body)
		t.Fatalf("unexpected pair status: %d body=%s", pairResp.StatusCode, string(body))
	}
	var pairPayload struct {
		PairToken struct {
			Token string `json:"token"`
		} `json:"pair_token"`
	}
	if err := json.NewDecoder(pairResp.Body).Decode(&pairPayload); err != nil {
		t.Fatalf("decode pair payload: %v", err)
	}
	if strings.TrimSpace(pairPayload.PairToken.Token) == "" {
		t.Fatalf("missing pair token")
	}

	agentCfg := agent.Config{
		GatewayBaseURL:       fmt.Sprintf("http://%s", gatewayAddr),
		AgentID:              "connector-agent",
		HeartbeatInterval:    200 * time.Millisecond,
		RequestTimeout:       5 * time.Second,
		PollWait:             1 * time.Second,
		PairToken:            pairPayload.PairToken.Token,
		MaxResponseBodyBytes: 20 << 20,
	}
	agentClient := agent.New(agentCfg, log.New(io.Discard, "", 0))
	agentErrCh := make(chan error, 1)
	go func() {
		agentErrCh <- agentClient.Run(ctx)
	}()

	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	port, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/team-connector/routes", gatewayAddr), map[string]any{
		"id":              "web",
		"connector_id":    "conn-a",
		"local_scheme":    "http",
		"local_host":      "127.0.0.1",
		"local_port":      port,
		"local_base_path": "",
	}, http.StatusOK)

	var body string
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://%s/t/team-connector/web/health?x=1", gatewayAddr))
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			if requestID := strings.TrimSpace(resp.Header.Get("X-Proxer-Request-ID")); requestID == "" {
				t.Fatalf("missing X-Proxer-Request-ID header")
			}
			body = string(data)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !strings.Contains(body, `"service":"connector"`) {
		t.Fatalf("unexpected connector route response: %s", body)
	}

	cancel()
	select {
	case err := <-agentErrCh:
		if err != nil {
			t.Fatalf("agent returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for agent shutdown")
	}
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestGatewayReturns413ForOversizedRequestBody(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:           "127.0.0.1:0",
		AgentToken:           "test-token",
		PublicBaseURL:        "http://localhost:8080",
		RequestTimeout:       5 * time.Second,
		MaxRequestBodyBytes:  64,
		MaxResponseBodyBytes: 1 << 20,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/rules", gatewayAddr), map[string]string{
		"id":     "tiny",
		"target": target.URL,
	}, http.StatusOK)

	largeBody := strings.Repeat("a", 1024)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/t/tiny/upload", gatewayAddr), bytes.NewBufferString(largeBody))
	if err != nil {
		t.Fatalf("build oversized request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send oversized request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 413, got %d body=%s", resp.StatusCode, string(body))
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestGatewayReturns503WhenBackpressureLimitIsHit(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"service": "slow"})
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:           "127.0.0.1:0",
		AgentToken:           "test-token",
		PublicBaseURL:        "http://localhost:8080",
		RequestTimeout:       2 * time.Second,
		ProxyRequestTimeout:  2 * time.Second,
		MaxPendingPerSession: 1,
		MaxPendingGlobal:     1,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	agentCfg := agent.Config{
		GatewayBaseURL:       fmt.Sprintf("http://%s", gatewayAddr),
		AgentToken:           "test-token",
		AgentID:              "slow-agent",
		HeartbeatInterval:    200 * time.Millisecond,
		RequestTimeout:       2 * time.Second,
		PollWait:             1 * time.Second,
		MaxResponseBodyBytes: 20 << 20,
		Tunnels: []protocol.TunnelConfig{
			{ID: "slow", Target: target.URL},
		},
	}
	agentClient := agent.New(agentCfg, log.New(io.Discard, "", 0))
	agentErrCh := make(chan error, 1)
	go func() {
		agentErrCh <- agentClient.Run(ctx)
	}()

	if err := waitForTunnelCount(authedClient, fmt.Sprintf("http://%s/api/tunnels", gatewayAddr), 1, 8*time.Second); err != nil {
		t.Fatalf("tunnel was not registered: %v", err)
	}

	firstReqDone := make(chan struct{})
	go func() {
		defer close(firstReqDone)
		resp, err := http.Get(fmt.Sprintf("http://%s/t/slow/work", gatewayAddr))
		if err == nil && resp != nil {
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(80 * time.Millisecond)

	secondResp, err := http.Get(fmt.Sprintf("http://%s/t/slow/work", gatewayAddr))
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusServiceUnavailable {
		body, _ := io.ReadAll(secondResp.Body)
		t.Fatalf("expected 503, got %d body=%s", secondResp.StatusCode, string(body))
	}

	select {
	case <-firstReqDone:
	case <-time.After(3 * time.Second):
		t.Fatalf("first request did not complete")
	}

	cancel()
	select {
	case err := <-agentErrCh:
		if err != nil {
			t.Fatalf("agent returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for agent shutdown")
	}
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestPlanRouteLimitIsEnforced(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close(t)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]any{
		"id":   "route-cap",
		"name": "Route Cap Tenant",
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/admin/tenants/route-cap/assign-plan", gatewayAddr), map[string]any{
		"plan_id": "free",
	}, http.StatusOK)

	for i := 1; i <= 5; i++ {
		mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/route-cap/routes", gatewayAddr), map[string]any{
			"id":     fmt.Sprintf("r%d", i),
			"target": target.URL,
		}, http.StatusOK)
	}

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/tenants/route-cap/routes", gatewayAddr), map[string]any{
		"id":     "r6",
		"target": target.URL,
	}, http.StatusForbidden)

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestTenantRateLimitingReturns429(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{\"service\":\"rl\"}`))
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/admin/plans", gatewayAddr), map[string]any{
		"id":             "tiny",
		"name":           "Tiny",
		"max_routes":     5,
		"max_connectors": 2,
		"max_rps":        0.5,
		"max_monthly_gb": 50,
		"tls_enabled":    false,
	}, http.StatusCreated)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/admin/tenants/default/assign-plan", gatewayAddr), map[string]any{
		"plan_id": "tiny",
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/rules", gatewayAddr), map[string]any{
		"id":     "ratelimit",
		"target": target.URL,
	}, http.StatusOK)

	firstResp, err := http.Get(fmt.Sprintf("http://%s/t/ratelimit/", gatewayAddr))
	if err != nil {
		t.Fatalf("first proxied request failed: %v", err)
	}
	_ = firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", firstResp.StatusCode)
	}

	secondResp, err := http.Get(fmt.Sprintf("http://%s/t/ratelimit/", gatewayAddr))
	if err != nil {
		t.Fatalf("second proxied request failed: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(secondResp.Body)
		t.Fatalf("expected 429, got %d body=%s", secondResp.StatusCode, string(body))
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestSuperAdminBootstrapCanAccessAdminEndpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:         "127.0.0.1:0",
		AgentToken:         "test-token",
		PublicBaseURL:      "http://localhost:8080",
		RequestTimeout:     5 * time.Second,
		SuperAdminUsername: "root-admin",
		SuperAdminPassword: "root-pass-123",
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	mustPostJSONStatus(t, client, fmt.Sprintf("http://%s/api/auth/login", gatewayAddr), map[string]string{
		"username": "root-admin",
		"password": "root-pass-123",
	}, http.StatusOK)

	resp, err := client.Get(fmt.Sprintf("http://%s/api/admin/stats", gatewayAddr))
	if err != nil {
		t.Fatalf("get /api/admin/stats failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 from /api/admin/stats, got %d body=%s", resp.StatusCode, string(body))
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestSQLiteStatePersistenceAcrossRestart(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skipf("sqlite3 not available: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "proxer-state.db")
	baseCfg := gateway.Config{
		ListenAddr:         "127.0.0.1:0",
		AgentToken:         "test-token",
		PublicBaseURL:      "http://localhost:8080",
		RequestTimeout:     5 * time.Second,
		StorageDriver:      "sqlite",
		SQLitePath:         dbPath,
		SuperAdminUsername: "persist-admin",
		SuperAdminPassword: "persist-pass-123",
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	server1 := gateway.NewServer(baseCfg, log.New(io.Discard, "", 0))
	serverErrCh1 := make(chan error, 1)
	go func() {
		serverErrCh1 <- server1.Start(ctx1)
	}()
	addr1, err := waitForGatewayAddr(server1, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway #1 addr: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", addr1), 5*time.Second); err != nil {
		t.Fatalf("gateway #1 health: %v", err)
	}

	jar1, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client1 := &http.Client{Jar: jar1}
	mustPostJSONStatus(t, client1, fmt.Sprintf("http://%s/api/auth/login", addr1), map[string]any{
		"username": "persist-admin",
		"password": "persist-pass-123",
	}, http.StatusOK)
	mustPostJSONStatus(t, client1, fmt.Sprintf("http://%s/api/tenants", addr1), map[string]any{
		"id":   "persist-team",
		"name": "Persist Team",
	}, http.StatusOK)

	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("persist-ok"))
	}))
	defer target.Close(t)
	mustPostJSONStatus(t, client1, fmt.Sprintf("http://%s/api/tenants/persist-team/routes", addr1), map[string]any{
		"id":     "persist-route",
		"target": target.URL,
	}, http.StatusOK)

	cancel1()
	select {
	case err := <-serverErrCh1:
		if err != nil {
			t.Fatalf("gateway #1 shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway #1 shutdown")
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	server2 := gateway.NewServer(baseCfg, log.New(io.Discard, "", 0))
	serverErrCh2 := make(chan error, 1)
	go func() {
		serverErrCh2 <- server2.Start(ctx2)
	}()
	addr2, err := waitForGatewayAddr(server2, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway #2 addr: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", addr2), 5*time.Second); err != nil {
		t.Fatalf("gateway #2 health: %v", err)
	}

	jar2, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client2 := &http.Client{Jar: jar2}
	mustPostJSONStatus(t, client2, fmt.Sprintf("http://%s/api/auth/login", addr2), map[string]any{
		"username": "persist-admin",
		"password": "persist-pass-123",
	}, http.StatusOK)

	resp, err := client2.Get(fmt.Sprintf("http://%s/api/tenants/persist-team/routes", addr2))
	if err != nil {
		t.Fatalf("get persisted routes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected route list status after restart: %d body=%s", resp.StatusCode, string(body))
	}

	var payload struct {
		Routes []struct {
			ID       string `json:"id"`
			TenantID string `json:"tenant_id"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode route list payload: %v", err)
	}
	found := false
	for _, route := range payload.Routes {
		if route.ID == "persist-route" && route.TenantID == "persist-team" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("persisted route not found after restart: %+v", payload.Routes)
	}

	cancel2()
	select {
	case err := <-serverErrCh2:
		if err != nil {
			t.Fatalf("gateway #2 shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway #2 shutdown")
	}
}

func TestRouteSpecificRateLimitIsEnforced(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"route-limit"}`))
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:     "127.0.0.1:0",
		AgentToken:     "test-token",
		PublicBaseURL:  "http://localhost:8080",
		RequestTimeout: 5 * time.Second,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}
	authedClient := loginAsAdmin(t, gatewayAddr)

	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/admin/plans", gatewayAddr), map[string]any{
		"id":             "route-max",
		"name":           "Route Max",
		"max_routes":     10,
		"max_connectors": 10,
		"max_rps":        100,
		"max_monthly_gb": 100,
		"tls_enabled":    false,
	}, http.StatusCreated)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/admin/tenants/default/assign-plan", gatewayAddr), map[string]any{
		"plan_id": "route-max",
	}, http.StatusOK)
	mustPostJSONStatus(t, authedClient, fmt.Sprintf("http://%s/api/rules", gatewayAddr), map[string]any{
		"id":      "route-rps",
		"target":  target.URL,
		"max_rps": 0.5,
	}, http.StatusOK)

	firstResp, err := http.Get(fmt.Sprintf("http://%s/t/route-rps/", gatewayAddr))
	if err != nil {
		t.Fatalf("first proxied request failed: %v", err)
	}
	_ = firstResp.Body.Close()
	if firstResp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", firstResp.StatusCode)
	}

	secondResp, err := http.Get(fmt.Sprintf("http://%s/t/route-rps/", gatewayAddr))
	if err != nil {
		t.Fatalf("second proxied request failed: %v", err)
	}
	defer secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusTooManyRequests {
		body, _ := io.ReadAll(secondResp.Body)
		t.Fatalf("expected route-level 429, got %d body=%s", secondResp.StatusCode, string(body))
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestMemberWriteToggleEnforced(t *testing.T) {
	target := startTestHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"service":"rbac"}`))
	}))
	defer target.Close(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:         "127.0.0.1:0",
		AgentToken:         "test-token",
		PublicBaseURL:      "http://localhost:8080",
		RequestTimeout:     5 * time.Second,
		MemberWriteEnabled: false,
		SuperAdminUsername: "admin",
		SuperAdminPassword: "admin123",
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	adminClient := loginAsUser(t, gatewayAddr, "admin", "admin123")
	mustPostJSONStatus(t, adminClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]any{
		"id":   "team-rbac",
		"name": "Team RBAC",
	}, http.StatusOK)
	mustPostJSONStatus(t, adminClient, fmt.Sprintf("http://%s/api/admin/users", gatewayAddr), map[string]any{
		"username":  "member-rbac",
		"password":  "member-pass-123",
		"role":      "member",
		"tenant_id": "team-rbac",
		"status":    "active",
	}, http.StatusCreated)
	mustPostJSONStatus(t, adminClient, fmt.Sprintf("http://%s/api/admin/users", gatewayAddr), map[string]any{
		"username":  "tenant-admin-rbac",
		"password":  "tenant-admin-pass-123",
		"role":      "tenant_admin",
		"tenant_id": "team-rbac",
		"status":    "active",
	}, http.StatusCreated)

	memberClient := loginAsUser(t, gatewayAddr, "member-rbac", "member-pass-123")

	routeBody, _ := json.Marshal(map[string]any{
		"id":     "m-route",
		"target": target.URL,
	})
	memberRouteResp, err := memberClient.Post(fmt.Sprintf("http://%s/api/tenants/team-rbac/routes", gatewayAddr), "application/json", bytes.NewReader(routeBody))
	if err != nil {
		t.Fatalf("member route post failed: %v", err)
	}
	defer memberRouteResp.Body.Close()
	if memberRouteResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(memberRouteResp.Body)
		t.Fatalf("expected member route mutation to be forbidden, got %d body=%s", memberRouteResp.StatusCode, string(body))
	}

	connectorBody, _ := json.Marshal(map[string]any{
		"id":        "member-conn",
		"name":      "Member Connector",
		"tenant_id": "team-rbac",
	})
	memberConnectorResp, err := memberClient.Post(fmt.Sprintf("http://%s/api/connectors", gatewayAddr), "application/json", bytes.NewReader(connectorBody))
	if err != nil {
		t.Fatalf("member connector post failed: %v", err)
	}
	defer memberConnectorResp.Body.Close()
	if memberConnectorResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(memberConnectorResp.Body)
		t.Fatalf("expected member connector mutation to be forbidden, got %d body=%s", memberConnectorResp.StatusCode, string(body))
	}

	tenantAdminClient := loginAsUser(t, gatewayAddr, "tenant-admin-rbac", "tenant-admin-pass-123")
	mustPostJSONStatus(t, tenantAdminClient, fmt.Sprintf("http://%s/api/tenants/team-rbac/routes", gatewayAddr), map[string]any{
		"id":     "ta-route",
		"target": target.URL,
	}, http.StatusOK)
	mustPostJSONStatus(t, tenantAdminClient, fmt.Sprintf("http://%s/api/connectors", gatewayAddr), map[string]any{
		"id":        "ta-conn",
		"name":      "Tenant Admin Connector",
		"tenant_id": "team-rbac",
	}, http.StatusCreated)

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func waitForHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

func waitForGatewayAddr(server *gateway.Server, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := server.Addr()
		if !strings.HasSuffix(addr, ":0") {
			return addr, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout waiting for gateway addr")
}

func waitForTunnelCount(client *http.Client, url string, expected int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response := fetchTunnelResponseNoFail(client, url)
		if response != nil && len(response.Tunnels) >= expected {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("expected >= %d tunnels", expected)
}

type tunnelAPIResponse struct {
	Tunnels []struct {
		ID      string `json:"id"`
		Metrics struct {
			RequestCount     int64   `json:"request_count"`
			AverageLatencyMs float64 `json:"average_latency_ms"`
		} `json:"metrics"`
	} `json:"tunnels"`
}

func fetchTunnelResponseNoFail(client *http.Client, url string) *tunnelAPIResponse {
	resp, err := client.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var payload tunnelAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil
	}
	return &payload
}

func fetchTunnelResponse(t *testing.T, client *http.Client, url string) tunnelAPIResponse {
	t.Helper()
	payload := fetchTunnelResponseNoFail(client, url)
	if payload == nil {
		t.Fatalf("failed to fetch tunnel response from %s", url)
	}
	return *payload
}

func mustProxyRequest(t *testing.T, url string, expectedService string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("proxy status mismatch: got=%d body=%s", resp.StatusCode, string(body))
	}
	if tunnelID := resp.Header.Get("X-Proxer-Tunnel-ID"); tunnelID == "" {
		t.Fatalf("missing X-Proxer-Tunnel-ID header")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read proxy response body: %v", err)
	}
	payload := string(body)
	if !strings.Contains(payload, fmt.Sprintf(`"service":"%s"`, expectedService)) {
		t.Fatalf("expected service %s in payload, got %s", expectedService, payload)
	}
	return payload
}

func TestPublicSignupDisabledReturns403(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:             "127.0.0.1:0",
		AgentToken:             "test-token",
		PublicBaseURL:          "http://localhost:8080",
		RequestTimeout:         5 * time.Second,
		DevMode:                false,
		PublicSignupEnabled:    false,
		PublicSignupRPM:        30,
		PublicDownloadCacheTTL: 5 * time.Minute,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	mustPostJSONStatus(t, http.DefaultClient, fmt.Sprintf("http://%s/api/public/signup", gatewayAddr), map[string]any{
		"username": "signup_user",
		"password": "signup_pass_123",
	}, http.StatusForbidden)

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestPublicSignupCreatesTenantAdminAndSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:             "127.0.0.1:0",
		AgentToken:             "test-token",
		PublicBaseURL:          "http://localhost:8080",
		RequestTimeout:         5 * time.Second,
		DevMode:                true,
		PublicSignupEnabled:    true,
		PublicSignupRPM:        60,
		PublicDownloadCacheTTL: 5 * time.Minute,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	reqBody, _ := json.Marshal(map[string]any{
		"username": "new_tenant_admin",
		"password": "signup_pass_123",
	})
	resp, err := client.Post(fmt.Sprintf("http://%s/api/public/signup", gatewayAddr), "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("public signup request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 from public signup, got %d body=%s", resp.StatusCode, string(body))
	}
	var signupPayload struct {
		User struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			TenantID string `json:"tenant_id"`
		} `json:"user"`
		Assignment struct {
			PlanID string `json:"plan_id"`
		} `json:"assignment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signupPayload); err != nil {
		t.Fatalf("decode signup payload: %v", err)
	}
	if signupPayload.User.Role != gateway.RoleTenantAdmin {
		t.Fatalf("expected tenant_admin role, got %q", signupPayload.User.Role)
	}
	if signupPayload.Assignment.PlanID != "free" {
		t.Fatalf("expected free plan assignment, got %q", signupPayload.Assignment.PlanID)
	}
	if signupPayload.User.TenantID == "" {
		t.Fatalf("expected tenant id in signup payload")
	}

	meResp, err := client.Get(fmt.Sprintf("http://%s/api/auth/me", gatewayAddr))
	if err != nil {
		t.Fatalf("auth me request failed: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(meResp.Body)
		t.Fatalf("expected 200 from /api/auth/me, got %d body=%s", meResp.StatusCode, string(body))
	}

	plansResp, err := client.Get(fmt.Sprintf("http://%s/api/public/plans", gatewayAddr))
	if err != nil {
		t.Fatalf("public plans request failed: %v", err)
	}
	defer plansResp.Body.Close()
	if plansResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(plansResp.Body)
		t.Fatalf("expected 200 from /api/public/plans, got %d body=%s", plansResp.StatusCode, string(body))
	}
	var plansPayload struct {
		Plans []struct {
			ID              string  `json:"id"`
			PriceMonthlyUSD float64 `json:"price_monthly_usd"`
		} `json:"plans"`
	}
	if err := json.NewDecoder(plansResp.Body).Decode(&plansPayload); err != nil {
		t.Fatalf("decode plans payload: %v", err)
	}
	var sawPro bool
	for _, plan := range plansPayload.Plans {
		if plan.ID == "pro" {
			sawPro = true
			if plan.PriceMonthlyUSD <= 0 {
				t.Fatalf("expected public pro plan monthly price > 0, got %v", plan.PriceMonthlyUSD)
			}
		}
	}
	if !sawPro {
		t.Fatalf("expected pro plan in public plans payload")
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestPublicSignupSlugCollisionAddsSuffix(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:             "127.0.0.1:0",
		AgentToken:             "test-token",
		PublicBaseURL:          "http://localhost:8080",
		RequestTimeout:         5 * time.Second,
		DevMode:                true,
		PublicSignupEnabled:    true,
		PublicSignupRPM:        60,
		PublicDownloadCacheTTL: 5 * time.Minute,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	adminClient := loginAsAdmin(t, gatewayAddr)
	mustPostJSONStatus(t, adminClient, fmt.Sprintf("http://%s/api/tenants", gatewayAddr), map[string]any{
		"id":   "collision",
		"name": "Collision",
	}, http.StatusOK)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]any{
		"username": "collision",
		"password": "signup_pass_123",
	})
	resp, err := client.Post(fmt.Sprintf("http://%s/api/public/signup", gatewayAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("public signup request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, string(payload))
	}
	var signupPayload struct {
		User struct {
			TenantID string `json:"tenant_id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signupPayload); err != nil {
		t.Fatalf("decode signup payload: %v", err)
	}
	if signupPayload.User.TenantID != "collision-2" {
		t.Fatalf("expected tenant suffix collision-2, got %q", signupPayload.User.TenantID)
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestPublicSignupRateLimitReturns429(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:             "127.0.0.1:0",
		AgentToken:             "test-token",
		PublicBaseURL:          "http://localhost:8080",
		RequestTimeout:         5 * time.Second,
		DevMode:                true,
		PublicSignupEnabled:    true,
		PublicSignupRPM:        1,
		PublicDownloadCacheTTL: 5 * time.Minute,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	mustPostJSONStatus(t, http.DefaultClient, fmt.Sprintf("http://%s/api/public/signup", gatewayAddr), map[string]any{
		"username": "signup_rl_1",
		"password": "signup_pass_123",
	}, http.StatusCreated)
	mustPostJSONStatus(t, http.DefaultClient, fmt.Sprintf("http://%s/api/public/signup", gatewayAddr), map[string]any{
		"username": "signup_rl_2",
		"password": "signup_pass_123",
	}, http.StatusTooManyRequests)

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func TestPublicDownloadsReturnsUnavailableWhenRepoNotConfigured(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gatewayCfg := gateway.Config{
		ListenAddr:             "127.0.0.1:0",
		AgentToken:             "test-token",
		PublicBaseURL:          "http://localhost:8080",
		RequestTimeout:         5 * time.Second,
		DevMode:                true,
		PublicSignupEnabled:    true,
		PublicSignupRPM:        30,
		PublicDownloadCacheTTL: 5 * time.Minute,
	}
	gatewayServer := gateway.NewServer(gatewayCfg, log.New(io.Discard, "", 0))
	gatewayErrCh := make(chan error, 1)
	go func() {
		gatewayErrCh <- gatewayServer.Start(ctx)
	}()

	gatewayAddr, err := waitForGatewayAddr(gatewayServer, 5*time.Second)
	if err != nil {
		t.Fatalf("gateway did not publish a listener address: %v", err)
	}
	if err := waitForHTTP(fmt.Sprintf("http://%s/api/health", gatewayAddr), 5*time.Second); err != nil {
		t.Fatalf("gateway health never became ready: %v", err)
	}

	resp, err := http.Get(fmt.Sprintf("http://%s/api/public/downloads", gatewayAddr))
	if err != nil {
		t.Fatalf("request public downloads failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(body))
	}
	var payload struct {
		Available bool   `json:"available"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode downloads payload: %v", err)
	}
	if payload.Available {
		t.Fatalf("expected unavailable downloads when repo is not configured")
	}
	if strings.TrimSpace(payload.Message) == "" {
		t.Fatalf("expected unavailable downloads payload to include a message")
	}

	cancel()
	select {
	case err := <-gatewayErrCh:
		if err != nil {
			t.Fatalf("gateway returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for gateway shutdown")
	}
}

func containsHeaderValue(headers map[string][]string, key, expected string) bool {
	values, ok := headers[key]
	if !ok {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func mustPostJSONStatus(t *testing.T, client *http.Client, endpoint string, payload any, expectedStatus int) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload for %s: %v", endpoint, err)
	}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s failed: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status for %s: got=%d expected=%d body=%s", endpoint, resp.StatusCode, expectedStatus, string(responseBody))
	}
}

func mustPutJSONStatus(t *testing.T, client *http.Client, endpoint string, payload any, expectedStatus int) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload for %s: %v", endpoint, err)
	}
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build PUT request for %s: %v", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("put %s failed: %v", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status for %s: got=%d expected=%d body=%s", endpoint, resp.StatusCode, expectedStatus, string(responseBody))
	}
}

func loginAsAdmin(t *testing.T, gatewayAddr string) *http.Client {
	t.Helper()
	return loginAsUser(t, gatewayAddr, "admin", "admin123")
}

func loginAsUser(t *testing.T, gatewayAddr, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	payload := map[string]string{
		"username": username,
		"password": password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal login payload: %v", err)
	}
	resp, err := client.Post(fmt.Sprintf("http://%s/api/auth/login", gatewayAddr), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: status=%d body=%s", resp.StatusCode, string(responseBody))
	}
	return client
}

type testHTTPServer struct {
	URL    string
	server *http.Server
}

func startTestHTTPServer(t *testing.T, handler http.Handler) *testHTTPServer {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for test server: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()
	return &testHTTPServer{
		URL:    "http://" + listener.Addr().String(),
		server: server,
	}
}

func (s *testHTTPServer) Close(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown test server: %v", err)
	}
}
