package gateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/szaher/try/proxer/internal/httpx"
	"github.com/szaher/try/proxer/internal/protocol"
	storepkg "github.com/szaher/try/proxer/internal/store"
)

const sessionCookieName = "proxer_session"

var errBodyTooLarge = errors.New("body too large")

type Server struct {
	cfg                  Config
	logger               *log.Logger
	hub                  *Hub
	ruleStore            *RuleStore
	authStore            *AuthStore
	connectorStore       *ConnectorStore
	planStore            *PlanStore
	rateLimiter          *RateLimiter
	incidentStore        *IncidentStore
	tlsStore             *TLSStore
	persistence          storepkg.SnapshotStore
	forwardHTTP          *http.Client
	maxRequestBodyBytes  int64
	maxResponseBodyBytes int64

	httpServer  *http.Server
	listener    net.Listener
	tlsServer   *http.Server
	tlsListener net.Listener

	requestCounter uint64
	startedAt      time.Time
}

type tunnelView struct {
	TenantID        string             `json:"tenant_id"`
	RouteID         string             `json:"route_id"`
	ID              string             `json:"id"`
	TunnelKey       string             `json:"tunnel_key"`
	Target          string             `json:"target"`
	RequiresToken   bool               `json:"requires_token"`
	AgentID         string             `json:"agent_id,omitempty"`
	PublicURL       string             `json:"public_url"`
	LegacyPublicURL string             `json:"legacy_public_url,omitempty"`
	Metrics         TunnelMetrics      `json:"metrics"`
	Connection      ConnectionSnapshot `json:"connection"`
	Source          string             `json:"source"`
}

type routeView struct {
	TenantID        string        `json:"tenant_id"`
	RouteID         string        `json:"route_id"`
	ID              string        `json:"id"`
	TunnelKey       string        `json:"tunnel_key"`
	Target          string        `json:"target"`
	MaxRPS          float64       `json:"max_rps,omitempty"`
	ConnectorID     string        `json:"connector_id,omitempty"`
	LocalScheme     string        `json:"local_scheme,omitempty"`
	LocalHost       string        `json:"local_host,omitempty"`
	LocalPort       int           `json:"local_port,omitempty"`
	LocalBasePath   string        `json:"local_base_path,omitempty"`
	PublicURL       string        `json:"public_url"`
	LegacyPublicURL string        `json:"legacy_public_url,omitempty"`
	TokenConfigured bool          `json:"token_configured"`
	Connected       bool          `json:"connected"`
	AgentID         string        `json:"agent_id,omitempty"`
	Metrics         TunnelMetrics `json:"metrics"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type tenantView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	RouteCount int       `json:"route_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type upsertRuleRequest struct {
	ID            string  `json:"id"`
	Target        string  `json:"target"`
	Token         string  `json:"token"`
	MaxRPS        float64 `json:"max_rps"`
	ConnectorID   string  `json:"connector_id"`
	LocalScheme   string  `json:"local_scheme"`
	LocalHost     string  `json:"local_host"`
	LocalPort     int     `json:"local_port"`
	LocalBasePath string  `json:"local_base_path"`
}

type upsertTenantRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type upsertEnvironmentRequest struct {
	Scheme      string            `json:"scheme"`
	Host        string            `json:"host"`
	DefaultPort int               `json:"default_port"`
	Variables   map[string]string `json:"variables"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TenantID   string `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
}

type connectorView struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Connected   bool      `json:"connected"`
	AgentID     string    `json:"agent_id,omitempty"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	PairCommand string    `json:"pair_command,omitempty"`
}

type createConnectorRequest struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

type pairConnectorResponse struct {
	Connector connectorView `json:"connector"`
	PairToken PairToken     `json:"pair_token"`
	Command   string        `json:"command"`
}

type resolvedProxyPath struct {
	TenantID    string
	RouteID     string
	ForwardPath string
}

func NewServer(cfg Config, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		cfg.MaxRequestBodyBytes = 10 << 20
	}
	if cfg.MaxResponseBodyBytes <= 0 {
		cfg.MaxResponseBodyBytes = 20 << 20
	}
	if cfg.ProxyRequestTimeout <= 0 {
		if cfg.RequestTimeout > 0 {
			cfg.ProxyRequestTimeout = cfg.RequestTimeout
		} else {
			cfg.ProxyRequestTimeout = 30 * time.Second
		}
	}
	if cfg.MaxPendingPerSession <= 0 {
		cfg.MaxPendingPerSession = 1024
	}
	if cfg.MaxPendingGlobal <= 0 {
		cfg.MaxPendingGlobal = 10000
	}

	superAdminUser := strings.TrimSpace(cfg.SuperAdminUsername)
	if superAdminUser == "" {
		superAdminUser = strings.TrimSpace(cfg.AdminUsername)
	}
	if superAdminUser == "" {
		superAdminUser = "admin"
	}
	superAdminPass := strings.TrimSpace(cfg.SuperAdminPassword)
	if superAdminPass == "" {
		superAdminPass = strings.TrimSpace(cfg.AdminPassword)
	}
	if superAdminPass == "" {
		superAdminPass = "admin123"
	}
	authStore, err := NewAuthStore(superAdminUser, superAdminPass, cfg.SessionTTL)
	if err != nil {
		// Keep constructor signature simple and fail fast for invalid auth setup.
		panic(fmt.Errorf("initialize auth store: %w", err))
	}

	hub := NewHub(cfg.AgentToken, cfg.PublicBaseURL, cfg.ProxyRequestTimeout, cfg.MaxPendingPerSession, cfg.MaxPendingGlobal)
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	persistence, err := storepkg.NewSnapshotStore(cfg.StorageDriver, cfg.SQLitePath)
	if err != nil {
		panic(fmt.Errorf("initialize state persistence: %w", err))
	}

	server := &Server{
		cfg:            cfg,
		logger:         logger,
		hub:            hub,
		ruleStore:      NewRuleStore(),
		authStore:      authStore,
		connectorStore: NewConnectorStore(cfg.PairTokenTTL),
		planStore:      NewPlanStore(),
		rateLimiter:    NewRateLimiter(),
		incidentStore:  NewIncidentStore(),
		tlsStore:       NewTLSStore(cfg.TLSKeyEncryptionKey),
		persistence:    persistence,
		forwardHTTP: &http.Client{
			Transport: transport,
		},
		maxRequestBodyBytes:  cfg.MaxRequestBodyBytes,
		maxResponseBodyBytes: cfg.MaxResponseBodyBytes,
		startedAt:            time.Now().UTC(),
	}

	if err := server.restorePersistentState(); err != nil {
		panic(fmt.Errorf("restore persisted state: %w", err))
	}
	if err := server.authStore.EnsureSuperAdmin(superAdminUser, superAdminPass); err != nil {
		panic(fmt.Errorf("ensure super admin user: %w", err))
	}
	server.refreshUsageAllTenants()
	server.persistState()
	return server
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleFrontend)
	mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/auth/register", s.handleAuthRegister)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/me/dashboard", s.handleMeDashboard)
	mux.HandleFunc("/api/me/routes", s.handleMeRoutes)
	mux.HandleFunc("/api/me/connectors", s.handleMeConnectors)
	mux.HandleFunc("/api/me/usage", s.handleMeUsage)
	mux.HandleFunc("/api/admin/users", s.handleAdminUsers)
	mux.HandleFunc("/api/admin/users/", s.handleAdminUserByID)
	mux.HandleFunc("/api/admin/stats", s.handleAdminStats)
	mux.HandleFunc("/api/admin/incidents", s.handleAdminIncidents)
	mux.HandleFunc("/api/admin/system-status", s.handleAdminSystemStatus)
	mux.HandleFunc("/api/admin/plans", s.handleAdminPlans)
	mux.HandleFunc("/api/admin/plans/", s.handleAdminPlanByID)
	mux.HandleFunc("/api/admin/tenants/", s.handleAdminTenantsSubresource)
	mux.HandleFunc("/api/admin/tls/certificates", s.handleAdminTLSCertificates)
	mux.HandleFunc("/api/admin/tls/certificates/", s.handleAdminTLSCertificateByID)
	mux.HandleFunc("/api/tunnels", s.handleTunnels)
	mux.HandleFunc("/api/connectors", s.handleConnectors)
	mux.HandleFunc("/api/connectors/", s.handleConnectorByID)
	mux.HandleFunc("/api/tenants", s.handleTenants)
	mux.HandleFunc("/api/tenants/", s.handleTenantSubresources)
	// Backward-compatible default-tenant endpoints.
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/rules/", s.handleRuleByID)
	mux.HandleFunc("/api/agent/pair", s.handleAgentPair)
	mux.HandleFunc("/api/agent/register", s.handleAgentRegister)
	mux.HandleFunc("/api/agent/pull", s.handleAgentPull)
	mux.HandleFunc("/api/agent/respond", s.handleAgentRespond)
	mux.HandleFunc("/api/agent/heartbeat", s.handleAgentHeartbeat)
	mux.HandleFunc("/t/", s.handleProxy)

	s.httpServer = &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go s.runPersistenceLoop(ctx)

	listener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.cfg.ListenAddr, err)
	}
	s.listener = listener

	errCh := make(chan error, 2)
	go func() {
		if serveErr := s.httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve gateway: %w", serveErr)
		}
	}()

	if strings.TrimSpace(s.cfg.TLSListenAddr) != "" {
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
				serverName := ""
				if info != nil {
					serverName = info.ServerName
				}
				return s.tlsStore.CertificateForHostname(serverName)
			},
		}
		s.tlsServer = &http.Server{
			Addr:              s.cfg.TLSListenAddr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			TLSConfig:         tlsConfig,
		}
		rawTLSListener, tlsErr := net.Listen("tcp", s.cfg.TLSListenAddr)
		if tlsErr != nil {
			return fmt.Errorf("listen on tls addr %s: %w", s.cfg.TLSListenAddr, tlsErr)
		}
		s.tlsListener = tls.NewListener(rawTLSListener, tlsConfig)
		go func() {
			if serveErr := s.tlsServer.Serve(s.tlsListener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				errCh <- fmt.Errorf("serve tls gateway: %w", serveErr)
			}
		}()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := s.httpServer.Shutdown(shutdownCtx); shutdownErr != nil {
			return fmt.Errorf("shutdown gateway: %w", shutdownErr)
		}
		if s.tlsServer != nil {
			if shutdownErr := s.tlsServer.Shutdown(shutdownCtx); shutdownErr != nil {
				return fmt.Errorf("shutdown tls gateway: %w", shutdownErr)
			}
		}
		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		default:
		}
		return nil
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	}
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return s.cfg.ListenAddr
	}
	return s.listener.Addr().String()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Proxer Frontend App</title>
  <meta name="description" content="Login, create tenant routes, and configure tenant environments." />
  <style>
    :root {
      --bg: #0f172a;
      --card: #16213a;
      --text: #e5eefc;
      --muted: #9fb0d3;
      --accent: #4ad0ff;
      --line: #2a4069;
      --ok: #22c55e;
      --warn: #f59e0b;
      --err: #f87171;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif;
      background: radial-gradient(circle at 20% 10%, #1b3154, var(--bg));
      color: var(--text);
    }
    main { max-width: 1180px; margin: 0 auto; padding: 24px; }
    h1 { margin: 0 0 8px; }
    h2 { margin: 0 0 14px; }
    h3 { margin: 0 0 12px; }
    .muted { color: var(--muted); }
    .card {
      background: color-mix(in oklab, var(--card), transparent 4%);
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 16px;
      margin-bottom: 16px;
    }
    .grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
    .grid3 { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 12px; }
    .grid4 { display: grid; grid-template-columns: 1fr 1fr 1fr 1fr; gap: 12px; }
    @media (max-width: 980px) {
      .grid2, .grid3, .grid4 { grid-template-columns: 1fr; }
    }
    label { display: block; font-size: 13px; color: var(--muted); margin-bottom: 6px; }
    input, select {
      width: 100%;
      background: #0f1b31;
      border: 1px solid var(--line);
      color: var(--text);
      border-radius: 8px;
      padding: 10px 12px;
      margin-bottom: 12px;
    }
    .row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
    button {
      background: #1e3a5f;
      color: var(--text);
      border: 1px solid #365682;
      border-radius: 8px;
      padding: 8px 12px;
      cursor: pointer;
    }
    button.primary {
      background: linear-gradient(120deg, #0ea5e9, #0369a1);
      border-color: #38bdf8;
    }
    button.danger {
      background: #4a1d26;
      border-color: #7f1d1d;
    }
    code { color: #9ee4ff; }
    table { width: 100%; border-collapse: collapse; }
    th, td {
      text-align: left;
      border-bottom: 1px solid var(--line);
      padding: 8px;
      font-size: 14px;
      vertical-align: top;
    }
    .pill {
      border-radius: 999px;
      padding: 2px 8px;
      font-size: 12px;
      display: inline-block;
    }
    .ok { background: #143122; color: #86efac; border: 1px solid #166534; }
    .warn { background: #3b2b15; color: #fcd34d; border: 1px solid #a16207; }
    .status { min-height: 20px; margin-bottom: 8px; }
    .status.error { color: var(--err); }
    .status.success { color: var(--ok); }
    .hidden { display: none; }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  </style>
</head>
<body>
  <main>
    <h1>Proxer Frontend App</h1>
    <p class="muted">Login to manage tenant routes and configure each tenant environment.</p>

    <section id="authCard" class="card">
      <h2>Login</h2>
      <div id="authStatus" class="status"></div>
      <form id="loginForm" class="grid2">
        <div>
          <label for="loginUser">Username</label>
          <input id="loginUser" name="loginUser" placeholder="admin" required />
        </div>
        <div>
          <label for="loginPass">Password</label>
          <input id="loginPass" name="loginPass" type="password" placeholder="******" required />
        </div>
        <div class="row">
          <button class="primary" type="submit">Login</button>
        </div>
      </form>

      <h3>Register User</h3>
      <p class="muted">Creates a new member user and tenant (or uses an existing tenant if provided).</p>
      <form id="registerForm" class="grid4">
        <div>
          <label for="tenantId">Tenant ID</label>
          <input id="registerTenantId" name="registerTenantId" placeholder="team-a" required />
        </div>
        <div>
          <label for="tenantName">Tenant Name</label>
          <input id="registerTenantName" name="registerTenantName" placeholder="Team A" />
        </div>
        <div>
          <label for="registerUser">Username</label>
          <input id="registerUser" name="registerUser" placeholder="alice" required />
        </div>
        <div>
          <label for="registerPass">Password</label>
          <input id="registerPass" name="registerPass" type="password" placeholder="min 6 chars" required />
        </div>
        <div class="row">
          <button type="submit">Register</button>
        </div>
      </form>
    </section>

    <section id="appShell" class="hidden">
      <section class="card">
        <div class="row">
          <h2 style="margin:0;">Session</h2>
          <span id="meBadge" class="pill ok"></span>
          <button id="logoutBtn" class="danger" type="button">Logout</button>
        </div>
      </section>

      <section class="card">
        <h2>Tenant Management</h2>
        <div id="tenantStatus" class="status"></div>
        <form id="tenantForm" class="grid2">
          <div>
            <label for="tenantId">Tenant ID</label>
            <input id="tenantId" name="tenantId" placeholder="team-a" required />
          </div>
          <div>
            <label for="tenantName">Tenant Name</label>
            <input id="tenantName" name="tenantName" placeholder="Team A" />
          </div>
          <div class="row">
            <button class="primary" type="submit">Save Tenant</button>
          </div>
        </form>

        <div class="row">
          <label for="activeTenant" style="margin: 0; min-width: 130px;">Active Tenant</label>
          <select id="activeTenant"></select>
        </div>

        <table>
          <thead>
            <tr>
              <th>Tenant</th>
              <th>Name</th>
              <th>Routes</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="tenantRows"></tbody>
        </table>
      </section>

      <section class="card">
        <h2>Environment Setup</h2>
        <p class="muted">Define default upstream host/scheme/port for the selected tenant.</p>
        <div id="envStatus" class="status"></div>
        <form id="envForm" class="grid4">
          <div>
            <label for="envScheme">Scheme</label>
            <select id="envScheme">
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </div>
          <div>
            <label for="envHost">Host</label>
            <input id="envHost" placeholder="host.docker.internal" required />
          </div>
          <div>
            <label for="envPort">Default Port</label>
            <input id="envPort" type="number" min="1" max="65535" placeholder="3000" required />
          </div>
          <div>
            <label>&nbsp;</label>
            <button class="primary" type="submit">Save Environment</button>
          </div>
        </form>
      </section>

      <section class="card">
        <h2>Connectors</h2>
        <p class="muted">Create a connector per tenant, pair the host agent, then bind routes to that connector.</p>
        <div id="connectorStatus" class="status"></div>
        <form id="connectorForm" class="grid3">
          <div>
            <label for="connectorId">Connector ID</label>
            <input id="connectorId" placeholder="dev-laptop" required />
          </div>
          <div>
            <label for="connectorName">Connector Name</label>
            <input id="connectorName" placeholder="Dev Laptop" />
          </div>
          <div>
            <label>&nbsp;</label>
            <button class="primary" type="submit">Create Connector</button>
          </div>
        </form>

        <table>
          <thead>
            <tr>
              <th>Connector</th>
              <th>Tenant</th>
              <th>Status</th>
              <th>Last Seen</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="connectorRows"></tbody>
        </table>
      </section>

      <section class="card">
        <h2>Route Management</h2>
        <p class="muted">Routes are isolated per tenant. Same route ID can exist in different tenants.</p>
        <div id="routeStatus" class="status"></div>
        <form id="routeForm">
          <div class="grid4">
            <div>
              <label for="routeId">Route ID</label>
              <input id="routeId" name="routeId" placeholder="web" required />
            </div>
            <div>
              <label for="routeTarget">Manual Target URL (Direct Mode)</label>
              <input id="routeTarget" name="routeTarget" placeholder="http://host.docker.internal:3000" />
            </div>
            <div>
              <label for="routeConnector">Connector (Optional)</label>
              <select id="routeConnector"></select>
            </div>
            <div>
              <label for="routeLocalHost">Local Host (Connector Mode)</label>
              <input id="routeLocalHost" name="routeLocalHost" placeholder="127.0.0.1" />
            </div>
          </div>
          <div class="grid4">
            <div>
              <label for="routeLocalScheme">Local Scheme</label>
              <select id="routeLocalScheme">
                <option value="http">http</option>
                <option value="https">https</option>
              </select>
            </div>
            <div>
              <label for="routePort">Local Port (Connector Mode)</label>
              <input id="routePort" name="routePort" type="number" min="1" max="65535" placeholder="3000" />
            </div>
            <div>
              <label for="routeBasePath">Local Base Path (Connector Mode)</label>
              <input id="routeBasePath" name="routeBasePath" placeholder="/api" />
            </div>
            <div></div>
          </div>
          <div class="grid2">
            <div>
              <label for="routeToken">Access Token (Optional)</label>
              <input id="routeToken" name="routeToken" placeholder="my-secret-token" />
            </div>
            <div>
              <label>&nbsp;</label>
              <div class="row">
                <input id="useEnvironment" type="checkbox" style="width:auto; margin:0;" />
                <label for="useEnvironment" style="margin:0;">Build target from tenant environment</label>
              </div>
            </div>
          </div>
          <div class="row">
            <button class="primary" type="submit">Save Route</button>
            <button id="resetRoute" type="button">Reset</button>
          </div>
        </form>

        <table>
          <thead>
            <tr>
              <th>Route</th>
              <th>Connector</th>
              <th>Target</th>
              <th>Public URL</th>
              <th>Token</th>
              <th>Connection</th>
              <th>Requests</th>
              <th>Errors</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody id="routeRows"></tbody>
        </table>
      </section>

      <section class="card">
        <h2>Live Metrics Across Tenants</h2>
        <table>
          <thead>
            <tr>
              <th>Tenant</th>
              <th>Route</th>
              <th>Source</th>
              <th>Agent</th>
              <th>Status</th>
              <th>Avg Latency</th>
              <th>Bytes In</th>
              <th>Bytes Out</th>
              <th>Last Status</th>
            </tr>
          </thead>
          <tbody id="tunnelRows"></tbody>
        </table>
      </section>
    </section>
  </main>

  <script>
    const authCard = document.getElementById('authCard');
    const appShell = document.getElementById('appShell');
    const authStatusEl = document.getElementById('authStatus');
    const meBadge = document.getElementById('meBadge');

    const loginForm = document.getElementById('loginForm');
    const registerForm = document.getElementById('registerForm');

    const tenantForm = document.getElementById('tenantForm');
    const tenantIdEl = document.getElementById('tenantId');
    const tenantNameEl = document.getElementById('tenantName');
    const tenantStatusEl = document.getElementById('tenantStatus');
    const activeTenantEl = document.getElementById('activeTenant');

    const connectorForm = document.getElementById('connectorForm');
    const connectorIdEl = document.getElementById('connectorId');
    const connectorNameEl = document.getElementById('connectorName');
    const connectorStatusEl = document.getElementById('connectorStatus');

    const routeForm = document.getElementById('routeForm');
    const routeIdEl = document.getElementById('routeId');
    const routeTargetEl = document.getElementById('routeTarget');
    const routeConnectorEl = document.getElementById('routeConnector');
    const routeLocalHostEl = document.getElementById('routeLocalHost');
    const routeLocalSchemeEl = document.getElementById('routeLocalScheme');
    const routePortEl = document.getElementById('routePort');
    const routeBasePathEl = document.getElementById('routeBasePath');
    const routeTokenEl = document.getElementById('routeToken');
    const useEnvironmentEl = document.getElementById('useEnvironment');
    const routeStatusEl = document.getElementById('routeStatus');

    const envForm = document.getElementById('envForm');
    const envSchemeEl = document.getElementById('envScheme');
    const envHostEl = document.getElementById('envHost');
    const envPortEl = document.getElementById('envPort');
    const envStatusEl = document.getElementById('envStatus');

    const state = {
      me: null,
      activeTenant: 'default',
      tenants: [],
      envByTenant: {},
      connectors: []
    };

    function setStatus(element, msg, kind) {
      element.textContent = msg || '';
      element.className = 'status ' + (kind || '');
    }

    async function parseBody(res) {
      const text = await res.text();
      if (!text) return null;
      try { return JSON.parse(text); } catch (_err) { return text; }
    }

    async function api(url, options) {
      const res = await fetch(url, options || {});
      const body = await parseBody(res);
      if (!res.ok) {
        if (res.status === 401) {
          showAuthed(false);
        }
        const msg = typeof body === 'string' ? body : JSON.stringify(body);
        throw new Error(msg || ('request failed: ' + res.status));
      }
      return body;
    }

    function clearNode(node) {
      while (node.firstChild) node.removeChild(node.firstChild);
    }

    function createCell(text) {
      const td = document.createElement('td');
      td.textContent = text === undefined || text === null ? '' : String(text);
      return td;
    }

    function pill(text, className) {
      const span = document.createElement('span');
      span.className = 'pill ' + className;
      span.textContent = text;
      return span;
    }

    function showAuthed(authed) {
      authCard.classList.toggle('hidden', authed);
      appShell.classList.toggle('hidden', !authed);
      if (!authed) {
        state.me = null;
        meBadge.textContent = '';
      }
    }

    function isAdmin() {
      return state.me && state.me.user && state.me.user.role === 'admin';
    }

    async function refreshAuth() {
      try {
        const me = await api('/api/auth/me');
        state.me = me;
        showAuthed(true);
        meBadge.textContent = me.user.username + ' (' + me.user.role + ')';
        tenantForm.style.display = isAdmin() ? '' : 'none';
        if (!isAdmin()) {
          setStatus(tenantStatusEl, 'Tenant creation is admin-only. You can manage routes for your tenant.', '');
        } else {
          setStatus(tenantStatusEl, '', '');
        }
      } catch (_err) {
        showAuthed(false);
      }
    }

    function resetRouteForm() {
      routeIdEl.value = '';
      routeTargetEl.value = '';
      routeConnectorEl.value = '';
      routeLocalHostEl.value = '127.0.0.1';
      routeLocalSchemeEl.value = 'http';
      routePortEl.value = '';
      routeBasePathEl.value = '';
      routeTokenEl.value = '';
      useEnvironmentEl.checked = false;
      routeIdEl.focus();
    }

    function fillRouteForm(route) {
      routeIdEl.value = route.route_id;
      routeTargetEl.value = route.target;
      routeConnectorEl.value = route.connector_id || '';
      routeLocalHostEl.value = route.local_host || '127.0.0.1';
      routeLocalSchemeEl.value = route.local_scheme || 'http';
      routePortEl.value = route.local_port || '';
      routeBasePathEl.value = route.local_base_path || '';
      routeTokenEl.value = '';
      routeTokenEl.placeholder = route.token_configured ? 'Leave blank to remove token or set new one' : 'my-secret-token';
      useEnvironmentEl.checked = false;
    }

    function tenantRouteEndpoint(tenantId) {
      return '/api/tenants/' + encodeURIComponent(tenantId) + '/routes';
    }

    function selectActiveTenant(tenantId) {
      state.activeTenant = tenantId;
      activeTenantEl.value = tenantId;
      refreshRoutes().catch((err) => setStatus(routeStatusEl, err.message, 'error'));
    }

    async function refreshTenants() {
      const payload = await api('/api/tenants');
      const tenants = (payload && payload.tenants) || [];
      state.tenants = tenants;

      const tenantRows = document.getElementById('tenantRows');
      clearNode(tenantRows);

      const hasActive = tenants.some((t) => t.id === state.activeTenant);
      if (!hasActive && tenants.length > 0) {
        state.activeTenant = tenants[0].id;
      }

      clearNode(activeTenantEl);
      tenants.forEach((tenant) => {
        const option = document.createElement('option');
        option.value = tenant.id;
        option.textContent = tenant.id + ' (' + tenant.name + ')';
        activeTenantEl.appendChild(option);
      });
      if (state.activeTenant) {
        activeTenantEl.value = state.activeTenant;
      }

      if (tenants.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 4;
        td.className = 'muted';
        td.textContent = 'No tenants yet. Create one above.';
        tr.appendChild(td);
        tenantRows.appendChild(tr);
        return;
      }

      tenants.forEach((tenant) => {
        const tr = document.createElement('tr');
        tr.appendChild(createCell(tenant.id));
        tr.appendChild(createCell(tenant.name));
        tr.appendChild(createCell(tenant.route_count));

        const actionCell = document.createElement('td');
        const useBtn = document.createElement('button');
        useBtn.textContent = 'Use';
        useBtn.addEventListener('click', () => selectActiveTenant(tenant.id));
        actionCell.appendChild(useBtn);

        if (tenant.id !== 'default' && isAdmin()) {
          actionCell.appendChild(document.createTextNode(' '));
          const deleteBtn = document.createElement('button');
          deleteBtn.className = 'danger';
          deleteBtn.textContent = 'Delete';
          deleteBtn.addEventListener('click', async () => {
            if (!confirm('Delete tenant "' + tenant.id + '" and all its routes?')) return;
            try {
              await api('/api/tenants/' + encodeURIComponent(tenant.id), { method: 'DELETE' });
              setStatus(tenantStatusEl, 'Deleted tenant ' + tenant.id, 'success');
              await refreshAll();
            } catch (err) {
              setStatus(tenantStatusEl, err.message, 'error');
            }
          });
          actionCell.appendChild(deleteBtn);
        }
        tr.appendChild(actionCell);
        tenantRows.appendChild(tr);
      });
    }

    async function refreshEnvironment() {
      if (!state.activeTenant) return;
      const payload = await api('/api/tenants/' + encodeURIComponent(state.activeTenant) + '/environment');
      const env = payload.environment || {};
      state.envByTenant[state.activeTenant] = env;
      envSchemeEl.value = env.scheme || 'http';
      envHostEl.value = env.host || 'host.docker.internal';
      envPortEl.value = env.default_port || 3000;
    }

    function connectorsForActiveTenant() {
      return state.connectors.filter((connector) => connector.tenant_id === state.activeTenant);
    }

    function refreshConnectorSelector() {
      clearNode(routeConnectorEl);
      const empty = document.createElement('option');
      empty.value = '';
      empty.textContent = 'Direct / legacy tunnel';
      routeConnectorEl.appendChild(empty);

      connectorsForActiveTenant().forEach((connector) => {
        const option = document.createElement('option');
        option.value = connector.id;
        option.textContent = connector.id + (connector.connected ? ' (online)' : ' (offline)');
        routeConnectorEl.appendChild(option);
      });
    }

    async function refreshConnectors() {
      const payload = await api('/api/connectors');
      const connectors = (payload && payload.connectors) || [];
      state.connectors = connectors;

      const rows = document.getElementById('connectorRows');
      clearNode(rows);
      refreshConnectorSelector();

      const filtered = connectorsForActiveTenant();
      if (filtered.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 5;
        td.className = 'muted';
        td.textContent = 'No connectors for tenant ' + state.activeTenant + '.';
        tr.appendChild(td);
        rows.appendChild(tr);
        return;
      }

      filtered.forEach((connector) => {
        const tr = document.createElement('tr');
        tr.appendChild(createCell(connector.id));
        tr.appendChild(createCell(connector.tenant_id));

        const statusCell = document.createElement('td');
        statusCell.appendChild(connector.connected ? pill('online', 'ok') : pill('offline', 'warn'));
        tr.appendChild(statusCell);

        tr.appendChild(createCell(connector.last_seen || '-'));

        const actions = document.createElement('td');
        const pairBtn = document.createElement('button');
        pairBtn.textContent = 'Pair';
        pairBtn.addEventListener('click', async () => {
          try {
            const pair = await api('/api/connectors/' + encodeURIComponent(connector.id) + '/pair', { method: 'POST' });
            setStatus(connectorStatusEl, 'Pair token created. Command: ' + pair.command, 'success');
          } catch (err) {
            setStatus(connectorStatusEl, err.message, 'error');
          }
        });
        actions.appendChild(pairBtn);

        actions.appendChild(document.createTextNode(' '));
        const rotateBtn = document.createElement('button');
        rotateBtn.textContent = 'Rotate';
        rotateBtn.addEventListener('click', async () => {
          try {
            const rotated = await api('/api/connectors/' + encodeURIComponent(connector.id) + '/rotate', { method: 'POST' });
            setStatus(connectorStatusEl, 'Connector secret rotated: ' + rotated.connector_secret, 'success');
          } catch (err) {
            setStatus(connectorStatusEl, err.message, 'error');
          }
        });
        actions.appendChild(rotateBtn);

        actions.appendChild(document.createTextNode(' '));
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'danger';
        deleteBtn.textContent = 'Delete';
        deleteBtn.addEventListener('click', async () => {
          if (!confirm('Delete connector "' + connector.id + '"?')) return;
          try {
            await api('/api/connectors/' + encodeURIComponent(connector.id), { method: 'DELETE' });
            setStatus(connectorStatusEl, 'Connector deleted: ' + connector.id, 'success');
            await refreshConnectors();
          } catch (err) {
            setStatus(connectorStatusEl, err.message, 'error');
          }
        });
        actions.appendChild(deleteBtn);

        tr.appendChild(actions);
        rows.appendChild(tr);
      });
    }

    async function refreshRoutes() {
      const payload = await api(tenantRouteEndpoint(state.activeTenant));
      const routes = (payload && payload.routes) || [];

      const rows = document.getElementById('routeRows');
      clearNode(rows);

      if (routes.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 9;
        td.className = 'muted';
        td.textContent = 'No routes for tenant ' + state.activeTenant + '. Create one above.';
        tr.appendChild(td);
        rows.appendChild(tr);
        return;
      }

      routes.forEach((route) => {
        const tr = document.createElement('tr');
        tr.appendChild(createCell(route.route_id));
        tr.appendChild(createCell(route.connector_id || '-'));
        tr.appendChild(createCell(route.target));

        const publicUrlCell = document.createElement('td');
        const link = document.createElement('a');
        link.href = route.public_url;
        link.textContent = route.public_url;
        link.target = '_blank';
        publicUrlCell.appendChild(link);
        if (route.legacy_public_url) {
          publicUrlCell.appendChild(document.createElement('br'));
          const legacy = document.createElement('span');
          legacy.className = 'muted';
          legacy.textContent = 'legacy: ' + route.legacy_public_url;
          publicUrlCell.appendChild(legacy);
        }
        tr.appendChild(publicUrlCell);

        const tokenCell = document.createElement('td');
        tokenCell.appendChild(route.token_configured ? pill('enabled', 'warn') : pill('none', 'ok'));
        tr.appendChild(tokenCell);

        const connCell = document.createElement('td');
        if (route.connector_id) {
          connCell.appendChild(route.connected ? pill('connector online', 'ok') : pill('connector offline', 'warn'));
        } else {
          connCell.appendChild(route.connected ? pill('agent connected', 'ok') : pill('direct mode', 'warn'));
        }
        tr.appendChild(connCell);

        tr.appendChild(createCell(route.metrics.request_count));
        tr.appendChild(createCell(route.metrics.error_count));

        const actionCell = document.createElement('td');
        const editBtn = document.createElement('button');
        editBtn.textContent = 'Edit';
        editBtn.addEventListener('click', () => fillRouteForm(route));

        const delBtn = document.createElement('button');
        delBtn.className = 'danger';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', async () => {
          if (!confirm('Delete route "' + route.route_id + '" from tenant "' + route.tenant_id + '"?')) return;
          try {
            await api(tenantRouteEndpoint(route.tenant_id) + '/' + encodeURIComponent(route.route_id), { method: 'DELETE' });
            setStatus(routeStatusEl, 'Deleted route ' + route.route_id, 'success');
            await refreshAll();
          } catch (err) {
            setStatus(routeStatusEl, err.message, 'error');
          }
        });

        actionCell.appendChild(editBtn);
        actionCell.appendChild(document.createTextNode(' '));
        actionCell.appendChild(delBtn);
        tr.appendChild(actionCell);

        rows.appendChild(tr);
      });
    }

    async function refreshTunnels() {
      const payload = await api('/api/tunnels');
      const tunnels = (payload && payload.tunnels) || [];
      const rows = document.getElementById('tunnelRows');
      clearNode(rows);

      if (tunnels.length === 0) {
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 9;
        td.className = 'muted';
        td.textContent = 'No traffic yet.';
        tr.appendChild(td);
        rows.appendChild(tr);
        return;
      }

      tunnels.forEach((tunnel) => {
        const tr = document.createElement('tr');
        tr.appendChild(createCell(tunnel.tenant_id));
        tr.appendChild(createCell(tunnel.route_id));
        tr.appendChild(createCell(tunnel.source));
        tr.appendChild(createCell(tunnel.agent_id || '-'));

        const statusCell = document.createElement('td');
        statusCell.appendChild(tunnel.connection.connected ? pill('connected', 'ok') : pill('standby', 'warn'));
        tr.appendChild(statusCell);

        tr.appendChild(createCell(Number(tunnel.metrics.average_latency_ms || 0).toFixed(2) + ' ms'));
        tr.appendChild(createCell(tunnel.metrics.bytes_in));
        tr.appendChild(createCell(tunnel.metrics.bytes_out));
        tr.appendChild(createCell(tunnel.metrics.last_status || '-'));
        rows.appendChild(tr);
      });
    }

    async function refreshAll() {
      await refreshTenants();
      await refreshEnvironment();
      await refreshConnectors();
      await refreshRoutes();
      await refreshTunnels();
    }

    loginForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(authStatusEl, '', '');
      try {
        await api('/api/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: document.getElementById('loginUser').value,
            password: document.getElementById('loginPass').value
          })
        });
        document.getElementById('loginPass').value = '';
        await refreshAuth();
        await refreshAll();
        setStatus(routeStatusEl, 'Logged in successfully', 'success');
      } catch (err) {
        setStatus(authStatusEl, err.message, 'error');
      }
    });

    registerForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(authStatusEl, '', '');
      try {
        await api('/api/auth/register', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            username: document.getElementById('registerUser').value,
            password: document.getElementById('registerPass').value,
            tenant_id: document.getElementById('registerTenantId').value,
            tenant_name: document.getElementById('registerTenantName').value
          })
        });
        setStatus(authStatusEl, 'User registered. You can now login.', 'success');
      } catch (err) {
        setStatus(authStatusEl, err.message, 'error');
      }
    });

    tenantForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(tenantStatusEl, '', '');
      try {
        await api('/api/tenants', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ id: tenantIdEl.value, name: tenantNameEl.value })
        });
        setStatus(tenantStatusEl, 'Tenant saved: ' + tenantIdEl.value, 'success');
        tenantIdEl.value = '';
        tenantNameEl.value = '';
        await refreshAll();
      } catch (err) {
        setStatus(tenantStatusEl, err.message, 'error');
      }
    });

    connectorForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(connectorStatusEl, '', '');
      try {
        await api('/api/connectors', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id: connectorIdEl.value,
            name: connectorNameEl.value,
            tenant_id: state.activeTenant
          })
        });
        connectorIdEl.value = '';
        connectorNameEl.value = '';
        setStatus(connectorStatusEl, 'Connector created for tenant ' + state.activeTenant, 'success');
        await refreshConnectors();
      } catch (err) {
        setStatus(connectorStatusEl, err.message, 'error');
      }
    });

    activeTenantEl.addEventListener('change', () => {
      state.activeTenant = activeTenantEl.value;
      Promise.all([refreshEnvironment(), refreshConnectors(), refreshRoutes()]).catch((err) => setStatus(routeStatusEl, err.message, 'error'));
    });

    envForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(envStatusEl, '', '');
      try {
        await api('/api/tenants/' + encodeURIComponent(state.activeTenant) + '/environment', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            scheme: envSchemeEl.value,
            host: envHostEl.value,
            default_port: Number(envPortEl.value) || 0,
            variables: {}
          })
        });
        setStatus(envStatusEl, 'Environment saved for ' + state.activeTenant, 'success');
        await refreshEnvironment();
      } catch (err) {
        setStatus(envStatusEl, err.message, 'error');
      }
    });

    routeForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      setStatus(routeStatusEl, '', '');
      try {
        const connectorId = routeConnectorEl.value.trim();
        let target = routeTargetEl.value.trim();
        const env = state.envByTenant[state.activeTenant] || {};
        let localPort = Number(routePortEl.value || env.default_port || 0);
        let localHost = routeLocalHostEl.value.trim() || '127.0.0.1';
        let localScheme = routeLocalSchemeEl.value || 'http';
        let localBasePath = routeBasePathEl.value.trim();

        if (useEnvironmentEl.checked) {
          const scheme = env.scheme || 'http';
          const host = env.host || 'host.docker.internal';
          const port = Number(routePortEl.value || env.default_port || 0);
          if (!port || port < 1 || port > 65535) {
            throw new Error('Provide a valid port for environment-based target');
          }
          let basePath = routeBasePathEl.value.trim();
          if (basePath === '') basePath = '/';
          if (!basePath.startsWith('/')) basePath = '/' + basePath;
          target = scheme + '://' + host + ':' + port + basePath;
        }

        if (connectorId) {
          if (!localPort || localPort < 1 || localPort > 65535) {
            throw new Error('Provide a valid local port for connector mode');
          }
        } else if (!target) {
          throw new Error('Provide a manual target URL, enable environment-based target, or choose a connector');
        }

        await api(tenantRouteEndpoint(state.activeTenant), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id: routeIdEl.value,
            target: target,
            token: routeTokenEl.value,
            connector_id: connectorId,
            local_scheme: localScheme,
            local_host: localHost,
            local_port: localPort,
            local_base_path: localBasePath
          })
        });
        setStatus(routeStatusEl, 'Route saved: ' + routeIdEl.value + ' in tenant ' + state.activeTenant, 'success');
        resetRouteForm();
        await refreshAll();
      } catch (err) {
        setStatus(routeStatusEl, err.message, 'error');
      }
    });

    document.getElementById('resetRoute').addEventListener('click', () => {
      setStatus(routeStatusEl, '', '');
      resetRouteForm();
    });

    document.getElementById('logoutBtn').addEventListener('click', async () => {
      try {
        await api('/api/auth/logout', { method: 'POST' });
      } catch (_err) {}
      showAuthed(false);
      setStatus(authStatusEl, 'Logged out', 'success');
    });

    refreshAuth().then(async () => {
      if (state.me) {
        await refreshAll();
      }
    }).catch(() => showAuthed(false));
    setInterval(() => {
      if (!state.me) return;
      refreshAll().catch((err) => setStatus(routeStatusEl, err.message, 'error'));
    }, 3000);
  </script>
</body>
</html>`))
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request loginRequest
	if !s.decodeJSON(w, r, &request, "login payload") {
		return
	}

	user, ok := s.authStore.Authenticate(request.Username, request.Password)
	if !ok {
		http.Error(w, "invalid username or password", http.StatusUnauthorized)
		return
	}

	sessionID, err := s.authStore.NewSession(user.Username)
	if err != nil {
		http.Error(w, fmt.Sprintf("create session: %v", err), http.StatusInternalServerError)
		return
	}

	s.setSessionCookie(w, sessionID)
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "logged in",
		"user":    user,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.authStore.DeleteSession(cookie.Value)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"message": "logged out"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	tenants := s.filterTenantsForUser(user)
	writeJSON(w, http.StatusOK, map[string]any{
		"user":    user,
		"tenants": tenants,
	})
}

func (s *Server) handleAuthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request registerRequest
	if !s.decodeJSON(w, r, &request, "register payload") {
		return
	}

	tenantID := strings.TrimSpace(request.TenantID)
	if tenantID == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if !s.ruleStore.HasTenant(tenantID) {
		if _, err := s.ruleStore.UpsertTenant(Tenant{ID: tenantID, Name: request.TenantName}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.refreshTenantUsage(tenantID)
	}

	user, err := s.authStore.RegisterUser(RegisterUserInput{
		Username: request.Username,
		Password: request.Password,
		TenantID: tenantID,
		Role:     RoleMember,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": "user registered",
		"user":    user,
	})
	s.persistState()
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	tunnels := s.buildTunnelViews()
	payload := map[string]any{
		"status":       "ok",
		"transport":    "http-long-poll",
		"tunnel_count": len(tunnels),
		"storage":      s.storageHealth(),
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	tunnels := s.buildTunnelViews()
	filtered := make([]tunnelView, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if s.canAccessTenant(user, tunnel.TenantID) {
			filtered = append(filtered, tunnel)
		}
	}

	payload := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"tunnels":      filtered,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleConnectors(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"connectors":   s.buildConnectorViewsForUser(user),
		})
	case http.MethodPost:
		var request createConnectorRequest
		if !s.decodeJSON(w, r, &request, "connector payload") {
			return
		}

		tenantID := strings.TrimSpace(request.TenantID)
		if tenantID == "" {
			tenantID = strings.TrimSpace(user.TenantID)
		}
		if tenantID == "" && s.isSuperAdmin(user) {
			tenantID = DefaultTenantID
		}
		if !s.canAccessTenant(user, tenantID) {
			http.Error(w, "forbidden tenant access", http.StatusForbidden)
			return
		}
		if !s.canMutateTenant(user, tenantID) {
			http.Error(w, "forbidden tenant access", http.StatusForbidden)
			return
		}
		if !s.ruleStore.HasTenant(tenantID) {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}
		if err := s.enforceConnectorLimit(tenantID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		connector, err := s.connectorStore.Create(Connector{
			ID:       request.ID,
			TenantID: tenantID,
			Name:     request.Name,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"message":   "connector created",
			"connector": s.buildConnectorView(connector),
		})
		s.refreshTenantUsage(tenantID)
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleConnectorByID(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	connectorID, action, err := parseConnectorPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	connector, ok := s.connectorStore.Get(connectorID)
	if !ok {
		http.Error(w, "connector not found", http.StatusNotFound)
		return
	}
	if !s.canAccessTenant(user, connector.TenantID) {
		http.Error(w, "forbidden connector access", http.StatusForbidden)
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.canMutateTenant(user, connector.TenantID) {
			http.Error(w, "forbidden connector access", http.StatusForbidden)
			return
		}
		if ok := s.connectorStore.Delete(connectorID); !ok {
			http.Error(w, "connector not found", http.StatusNotFound)
			return
		}
		s.refreshTenantUsage(connector.TenantID)
		s.persistState()
		w.WriteHeader(http.StatusNoContent)
	case "pair":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.canMutateTenant(user, connector.TenantID) {
			http.Error(w, "forbidden connector access", http.StatusForbidden)
			return
		}
		pairToken, err := s.connectorStore.NewPairToken(connectorID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		command := fmt.Sprintf("PROXER_GATEWAY_BASE_URL=%s PROXER_AGENT_PAIR_TOKEN=%s proxer-agent",
			strings.TrimRight(s.cfg.PublicBaseURL, "/"), pairToken.Token)
		writeJSON(w, http.StatusOK, pairConnectorResponse{
			Connector: s.buildConnectorView(connector),
			PairToken: pairToken,
			Command:   command,
		})
	case "rotate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.canMutateTenant(user, connector.TenantID) {
			http.Error(w, "forbidden connector access", http.StatusForbidden)
			return
		}
		secret, err := s.connectorStore.RotateCredential(connectorID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message":          "connector credential rotated",
			"connector_id":     connectorID,
			"connector_secret": secret,
		})
		s.persistState()
	default:
		http.Error(w, "invalid connector path", http.StatusBadRequest)
	}
}

func (s *Server) handleAgentPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request protocol.PairAgentRequest
	if !s.decodeJSON(w, r, &request, "pair payload") {
		return
	}

	connector, secret, err := s.connectorStore.ConsumePairToken(request.PairToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, protocol.PairAgentResponse{
		ConnectorID:     connector.ID,
		ConnectorSecret: secret,
		TenantID:        connector.TenantID,
	})
}

func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		payload := map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"tenants":      s.filterTenantsForUser(user),
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodPost:
		if !s.requireSuperAdmin(w, user) {
			return
		}
		var request upsertTenantRequest
		if !s.decodeJSON(w, r, &request, "tenant payload") {
			return
		}
		tenant, err := s.ruleStore.UpsertTenant(Tenant{ID: request.ID, Name: request.Name})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "tenant upserted",
			"tenant":  tenant,
		})
		s.refreshTenantUsage(tenant.ID)
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTenantSubresources(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	segments, err := parseTenantSubresourcePath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch len(segments) {
	case 1:
		tenantID := segments[0]
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.requireSuperAdmin(w, user) {
			return
		}
		if ok := s.ruleStore.DeleteTenant(tenantID); !ok {
			http.Error(w, "tenant not found or cannot be deleted", http.StatusNotFound)
			return
		}
		s.refreshTenantUsage(tenantID)
		s.persistState()
		w.WriteHeader(http.StatusNoContent)
		return
	case 2:
		tenantID := segments[0]
		if !s.canAccessTenant(user, tenantID) {
			http.Error(w, "forbidden tenant access", http.StatusForbidden)
			return
		}
		switch segments[1] {
		case "routes":
			s.handleTenantRoutes(w, r, user, tenantID)
			return
		case "environment":
			s.handleTenantEnvironment(w, r, user, tenantID)
			return
		default:
			http.Error(w, "invalid tenant subresource path", http.StatusBadRequest)
			return
		}
	case 3:
		tenantID := segments[0]
		if !s.canAccessTenant(user, tenantID) {
			http.Error(w, "forbidden tenant access", http.StatusForbidden)
			return
		}
		if segments[1] != "routes" {
			http.Error(w, "invalid tenant subresource path", http.StatusBadRequest)
			return
		}
		routeID := segments[2]
		s.handleTenantRouteByID(w, r, user, tenantID, routeID)
		return
	default:
		http.Error(w, "invalid tenant subresource path", http.StatusBadRequest)
		return
	}
}

func (s *Server) handleTenantEnvironment(w http.ResponseWriter, r *http.Request, user User, tenantID string) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		http.Error(w, "missing tenant id", http.StatusBadRequest)
		return
	}
	if !s.ruleStore.HasTenant(tenantID) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		env, ok := s.ruleStore.GetEnvironment(tenantID)
		if !ok {
			http.Error(w, "environment not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id":   tenantID,
			"environment": env,
		})
	case http.MethodPut:
		if !s.canMutateTenantConfig(user, tenantID) {
			http.Error(w, "forbidden tenant configuration access", http.StatusForbidden)
			return
		}
		var request upsertEnvironmentRequest
		if !s.decodeJSON(w, r, &request, "environment payload") {
			return
		}
		env, err := s.ruleStore.UpsertEnvironment(TenantEnvironment{
			TenantID:    tenantID,
			Scheme:      request.Scheme,
			Host:        request.Host,
			DefaultPort: request.DefaultPort,
			Variables:   request.Variables,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message":     "environment upserted",
			"tenant_id":   tenantID,
			"environment": env,
		})
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTenantRoutes(w http.ResponseWriter, r *http.Request, user User, tenantID string) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		http.Error(w, "missing tenant id", http.StatusBadRequest)
		return
	}
	if !s.ruleStore.HasTenant(tenantID) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		payload := map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"tenant_id":    tenantID,
			"routes":       s.buildRouteViews(tenantID),
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodPost:
		if !s.canMutateTenant(user, tenantID) {
			http.Error(w, "forbidden route mutation", http.StatusForbidden)
			return
		}
		var request upsertRuleRequest
		if !s.decodeJSON(w, r, &request, "route payload") {
			return
		}
		if err := s.enforceRouteLimit(tenantID, request.ID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := s.validateConnectorRouteBinding(tenantID, request.ConnectorID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		route, err := s.ruleStore.UpsertForTenant(tenantID, Rule{
			ID:            request.ID,
			Target:        request.Target,
			Token:         request.Token,
			MaxRPS:        request.MaxRPS,
			ConnectorID:   request.ConnectorID,
			LocalScheme:   request.LocalScheme,
			LocalHost:     request.LocalHost,
			LocalPort:     request.LocalPort,
			LocalBasePath: request.LocalBasePath,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.hub.EnsureTunnelMetric(MakeTunnelKey(route.TenantID, route.ID))
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "route upserted",
			"route":   s.buildRouteView(route),
		})
		s.refreshTenantUsage(tenantID)
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTenantRouteByID(w http.ResponseWriter, r *http.Request, user User, tenantID, routeID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.canMutateTenant(user, tenantID) {
		http.Error(w, "forbidden route mutation", http.StatusForbidden)
		return
	}

	if ok := s.ruleStore.DeleteForTenant(tenantID, routeID); !ok {
		http.Error(w, "route not found", http.StatusNotFound)
		return
	}
	s.refreshTenantUsage(tenantID)
	s.persistState()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.canAccessTenant(user, DefaultTenantID) {
		http.Error(w, "forbidden tenant access", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodGet:
		payload := map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"tenant_id":    DefaultTenantID,
			"rules":        s.buildRouteViews(DefaultTenantID),
		}
		writeJSON(w, http.StatusOK, payload)
	case http.MethodPost:
		if !s.canMutateTenant(user, DefaultTenantID) {
			http.Error(w, "forbidden route mutation", http.StatusForbidden)
			return
		}
		var request upsertRuleRequest
		if !s.decodeJSON(w, r, &request, "rule payload") {
			return
		}
		if err := s.enforceRouteLimit(DefaultTenantID, request.ID); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := s.validateConnectorRouteBinding(DefaultTenantID, request.ConnectorID); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rule, err := s.ruleStore.UpsertForTenant(DefaultTenantID, Rule{
			ID:            request.ID,
			Target:        request.Target,
			Token:         request.Token,
			MaxRPS:        request.MaxRPS,
			ConnectorID:   request.ConnectorID,
			LocalScheme:   request.LocalScheme,
			LocalHost:     request.LocalHost,
			LocalPort:     request.LocalPort,
			LocalBasePath: request.LocalBasePath,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.hub.EnsureTunnelMetric(MakeTunnelKey(DefaultTenantID, rule.ID))
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "rule upserted",
			"rule":    s.buildRouteView(rule),
		})
		s.refreshTenantUsage(DefaultTenantID)
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRuleByID(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.canAccessTenant(user, DefaultTenantID) {
		http.Error(w, "forbidden tenant access", http.StatusForbidden)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.canMutateTenant(user, DefaultTenantID) {
		http.Error(w, "forbidden route mutation", http.StatusForbidden)
		return
	}

	routeID, err := parseRulePathID(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ok := s.ruleStore.DeleteForTenant(DefaultTenantID, routeID); !ok {
		http.Error(w, "rule not found", http.StatusNotFound)
		return
	}
	s.refreshTenantUsage(DefaultTenantID)
	s.persistState()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload protocol.RegisterRequest
	if !s.decodeJSON(w, r, &payload, "register payload") {
		return
	}

	var (
		response *protocol.RegisterResponse
		err      error
	)
	connectorID := strings.TrimSpace(payload.ConnectorID)
	if connectorID != "" {
		if !s.connectorStore.Authenticate(connectorID, payload.ConnectorSecret) {
			http.Error(w, "invalid connector credentials", http.StatusUnauthorized)
			return
		}
		response, err = s.hub.RegisterConnectorSession(connectorID, payload.AgentID)
	} else {
		response, err = s.hub.Register(&payload)
	}
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "token mismatch") {
			status = http.StatusUnauthorized
		}
		http.Error(w, err.Error(), status)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAgentPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	wait := 25 * time.Second
	if waitRaw := strings.TrimSpace(r.URL.Query().Get("wait")); waitRaw != "" {
		if seconds, err := strconv.Atoi(waitRaw); err == nil && seconds > 0 && seconds <= 60 {
			wait = time.Duration(seconds) * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), wait)
	defer cancel()
	request, err := s.hub.PullRequest(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrUnknownSession) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if request == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, protocol.PullResponse{Request: request})
}

func (s *Server) handleAgentRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload protocol.SubmitResponseRequest
	if !s.decodeJSON(w, r, &payload, "response payload") {
		return
	}

	if strings.TrimSpace(payload.SessionID) == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	if err := s.hub.SubmitProxyResponse(payload.SessionID, payload.Response); err != nil {
		if errors.Is(err, ErrUnknownSession) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, ErrUnknownPendingRequest) || errors.Is(err, ErrResponseSessionMismatch) || errors.Is(err, ErrResponseTunnelMismatch) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload protocol.HeartbeatRequest
	if !s.decodeJSON(w, r, &payload, "heartbeat payload") {
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	if err := s.hub.Heartbeat(payload.SessionID); err != nil {
		if errors.Is(err, ErrUnknownSession) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	requestID := s.nextRequestID()
	w.Header().Set("X-Proxer-Request-ID", requestID)

	resolved, err := s.resolveProxyPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	lookupKeys := s.lookupTunnelKeys(resolved.TenantID, resolved.RouteID)
	rule, hasRule := s.ruleStore.GetForTenant(resolved.TenantID, resolved.RouteID)
	plan, planID := s.planStore.GetTenantPlan(resolved.TenantID)

	if !s.rateLimiter.Allow("tenant:"+resolved.TenantID, plan.MaxRPS) {
		s.planStore.RecordBlockedRequest(resolved.TenantID)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":     "tenant_rate_limit_exceeded",
			"message":   "tenant request rate exceeded",
			"tenant_id": resolved.TenantID,
			"route_id":  resolved.RouteID,
			"plan_id":   planID,
		})
		return
	}
	routeRate := computeRouteRateLimit(plan)
	if hasRule && rule.MaxRPS > 0 {
		routeRate = rule.MaxRPS
		if routeRate > plan.MaxRPS {
			routeRate = plan.MaxRPS
		}
	}
	if !s.rateLimiter.Allow("route:"+resolved.TenantID+":"+resolved.RouteID, routeRate) {
		s.planStore.RecordBlockedRequest(resolved.TenantID)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":      "route_rate_limit_exceeded",
			"message":    "route request rate exceeded",
			"tenant_id":  resolved.TenantID,
			"route_id":   resolved.RouteID,
			"plan_id":    planID,
			"route_rps":  routeRate,
			"tenant_rps": plan.MaxRPS,
		})
		return
	}

	usage := s.planStore.GetUsage(resolved.TenantID, "")
	monthlyCapBytes := int64(plan.MaxMonthlyGB * bytesPerGB)
	if monthlyCapBytes > 0 && usage.BytesIn+usage.BytesOut >= monthlyCapBytes {
		s.planStore.RecordBlockedRequest(resolved.TenantID)
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":              "monthly_traffic_cap_exceeded",
			"message":            "monthly traffic cap exceeded",
			"tenant_id":          resolved.TenantID,
			"route_id":           resolved.RouteID,
			"plan_id":            planID,
			"monthly_cap_bytes":  monthlyCapBytes,
			"monthly_used_bytes": usage.BytesIn + usage.BytesOut,
			"blocked_requests":   usage.BlockedRequests + 1,
			"request_id":         requestID,
		})
		return
	}

	accessToken := r.URL.Query().Get("access_token")
	forwardQuery := r.URL.RawQuery

	requiredTunnelToken := s.lookupTunnelToken(lookupKeys)
	if requiredTunnelToken == "" && hasRule {
		requiredTunnelToken = rule.Token
	}
	if requiredTunnelToken != "" {
		providedToken := r.Header.Get("X-Proxer-Tunnel-Token")
		if providedToken == "" {
			providedToken = accessToken
		}
		if subtle.ConstantTimeCompare([]byte(requiredTunnelToken), []byte(providedToken)) != 1 {
			http.Error(w, "forbidden: missing or invalid tunnel token", http.StatusForbidden)
			return
		}
	}

	body, err := readAllWithLimit(r.Body, s.maxRequestBodyBytes)
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			http.Error(w, "request body exceeds limit", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, fmt.Sprintf("read request body: %v", err), http.StatusBadRequest)
		return
	}

	headers := httpx.CloneHTTPHeader(r.Header)
	enrichForwardHeaders(headers, r)
	headers["X-Proxer-Request-ID"] = []string{requestID}

	proxyReq := &protocol.ProxyRequest{
		RequestID:  requestID,
		Method:     r.Method,
		Path:       resolved.ForwardPath,
		Query:      forwardQuery,
		Headers:    headers,
		Body:       body,
		RemoteAddr: r.RemoteAddr,
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.hub.RequestTimeout())
	defer cancel()

	var (
		proxyResp   *protocol.ProxyResponse
		dispatchKey string
	)

	if hasRule && rule.UsesConnector() {
		dispatchKey = MakeTunnelKey(resolved.TenantID, resolved.RouteID)
		proxyReq.TunnelID = dispatchKey
		proxyReq.ConnectorID = rule.ConnectorID
		proxyReq.LocalTarget = &protocol.LocalTarget{
			Scheme: rule.LocalScheme,
			Host:   rule.LocalHost,
			Port:   rule.LocalPort,
		}
		proxyReq.Path = joinWithBasePath(rule.LocalBasePath, resolved.ForwardPath)

		proxyResp, err = s.hub.DispatchProxyRequestToConnector(ctx, rule.ConnectorID, dispatchKey, proxyReq)
		if err != nil {
			s.writeDispatchError(w, dispatchKey, int64(len(proxyReq.Body)), err)
			return
		}
	} else if key, connected := s.firstConnectedTunnelKey(lookupKeys); connected {
		dispatchKey = key
		proxyResp, err = s.hub.DispatchProxyRequest(ctx, dispatchKey, proxyReq)
		if err != nil {
			s.writeDispatchError(w, dispatchKey, int64(len(proxyReq.Body)), err)
			return
		}
	} else if hasRule {
		dispatchKey = MakeTunnelKey(resolved.TenantID, resolved.RouteID)
		proxyResp, err = s.forwardDirect(ctx, rule, proxyReq)
		if err != nil {
			s.hub.RecordProxyFailure(dispatchKey, int64(len(proxyReq.Body)), err.Error())
			s.maybeRecordProxyIncident(err, dispatchKey)
			status := http.StatusBadGateway
			switch {
			case errors.Is(err, ErrProxyRequestTimeout) || errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			case errors.Is(err, errBodyTooLarge):
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, fmt.Sprintf("direct forward failed: %v", err), status)
			return
		}
		proxyResp.RequestID = requestID
		s.hub.RecordProxyResponse(proxyResp)
	} else {
		http.Error(w, fmt.Sprintf("route %q not found for tenant %q", resolved.RouteID, resolved.TenantID), http.StatusNotFound)
		return
	}

	if proxyResp == nil {
		http.Error(w, "proxy response was nil", http.StatusBadGateway)
		return
	}

	if strings.TrimSpace(proxyResp.RequestID) == "" {
		proxyResp.RequestID = requestID
	}
	s.recordTrafficUsage(resolved.TenantID, plan, int64(len(body)), int64(len(proxyResp.Body)))
	s.writeProxyResponse(w, resolved.TenantID, resolved.RouteID, dispatchKey, proxyResp)
}

func (s *Server) forwardDirect(ctx context.Context, rule Rule, proxyReq *protocol.ProxyRequest) (*protocol.ProxyResponse, error) {
	start := time.Now()

	targetURL, err := buildTargetURL(rule.Target, proxyReq.Path, proxyReq.Query)
	if err != nil {
		return nil, fmt.Errorf("build target URL: %w", err)
	}

	outboundReq, err := http.NewRequestWithContext(ctx, proxyReq.Method, targetURL, bytes.NewReader(proxyReq.Body))
	if err != nil {
		return nil, fmt.Errorf("construct outbound request: %w", err)
	}

	for header, values := range proxyReq.Headers {
		if httpx.IsHopByHopHeader(header) || strings.EqualFold(header, "Host") || strings.EqualFold(header, "Content-Length") {
			continue
		}
		for _, value := range values {
			outboundReq.Header.Add(header, value)
		}
	}
	outboundReq.Header.Set("X-Proxer-Tunnel-ID", rule.ID)
	outboundReq.Header.Set("X-Proxer-Tunnel-Key", MakeTunnelKey(rule.TenantID, rule.ID))
	outboundReq.Header.Set("X-Proxer-Tenant-ID", rule.TenantID)
	outboundReq.Header.Set("X-Proxer-Route-ID", rule.ID)
	outboundReq.Header.Set("X-Proxer-Route-Mode", "direct")

	outboundResp, err := s.forwardHTTP.Do(outboundReq)
	if err != nil {
		return nil, fmt.Errorf("forward request to target %s: %w", rule.Target, err)
	}
	defer outboundResp.Body.Close()

	responseBody, err := readAllWithLimit(outboundResp.Body, s.maxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read upstream response: %w", err)
	}

	response := &protocol.ProxyResponse{
		RequestID: proxyReq.RequestID,
		TunnelID:  MakeTunnelKey(rule.TenantID, rule.ID),
		Status:    outboundResp.StatusCode,
		Headers:   httpx.CloneHTTPHeader(outboundResp.Header),
		Body:      responseBody,
		BytesIn:   int64(len(proxyReq.Body)),
		BytesOut:  int64(len(responseBody)),
		LatencyMs: time.Since(start).Milliseconds(),
	}
	return response, nil
}

func (s *Server) writeProxyResponse(w http.ResponseWriter, tenantID, routeID, tunnelKey string, proxyResp *protocol.ProxyResponse) {
	status := proxyResp.Status
	if status <= 0 {
		status = http.StatusBadGateway
	}

	if requestID := strings.TrimSpace(proxyResp.RequestID); requestID != "" {
		w.Header().Set("X-Proxer-Request-ID", requestID)
	}
	w.Header().Set("X-Proxer-Tunnel-ID", routeID)
	w.Header().Set("X-Proxer-Tunnel-Key", tunnelKey)
	w.Header().Set("X-Proxer-Tenant-ID", tenantID)
	w.Header().Set("X-Proxer-Route-ID", routeID)
	httpx.WriteHeaderMap(w.Header(), proxyResp.Headers)
	w.WriteHeader(status)
	if _, err := w.Write(proxyResp.Body); err != nil {
		s.logger.Printf("write proxied response failed: %v", err)
	}
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) (User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return User{}, false
	}

	user, ok := s.authStore.ResolveSession(cookie.Value)
	if !ok {
		s.clearSessionCookie(w)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return User{}, false
	}
	return user, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sessionID string) {
	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().UTC().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Server) canAccessTenant(user User, tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false
	}
	if s.isSuperAdmin(user) {
		return true
	}
	return strings.TrimSpace(user.TenantID) == tenantID
}

func (s *Server) isSuperAdmin(user User) bool {
	return strings.TrimSpace(user.Role) == RoleSuperAdmin
}

func (s *Server) isTenantAdmin(user User) bool {
	return strings.TrimSpace(user.Role) == RoleTenantAdmin
}

func (s *Server) isMember(user User) bool {
	return strings.TrimSpace(user.Role) == RoleMember
}

func (s *Server) canMutateTenant(user User, tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false
	}
	if s.isSuperAdmin(user) {
		return true
	}
	if strings.TrimSpace(user.TenantID) != tenantID {
		return false
	}
	if s.isTenantAdmin(user) {
		return true
	}
	if s.isMember(user) {
		return s.cfg.MemberWriteEnabled
	}
	return false
}

func (s *Server) canMutateTenantConfig(user User, tenantID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false
	}
	if s.isSuperAdmin(user) {
		return true
	}
	if !s.isTenantAdmin(user) {
		return false
	}
	return strings.TrimSpace(user.TenantID) == tenantID
}

func (s *Server) requireSuperAdmin(w http.ResponseWriter, user User) bool {
	if s.isSuperAdmin(user) {
		return true
	}
	http.Error(w, "super admin required", http.StatusForbidden)
	return false
}

func (s *Server) filterTenantsForUser(user User) []tenantView {
	all := s.buildTenantViews()
	if s.isSuperAdmin(user) {
		return all
	}
	filtered := make([]tenantView, 0, len(all))
	for _, tenant := range all {
		if tenant.ID == user.TenantID {
			filtered = append(filtered, tenant)
		}
	}
	return filtered
}

func (s *Server) buildConnectorViewsForUser(user User) []connectorView {
	tenantIDs := []string{}
	if s.isSuperAdmin(user) {
		for _, tenant := range s.ruleStore.ListTenants() {
			tenantIDs = append(tenantIDs, tenant.ID)
		}
	} else {
		tenantIDs = append(tenantIDs, user.TenantID)
	}

	connectors := s.connectorStore.ListForTenants(tenantIDs)
	views := make([]connectorView, 0, len(connectors))
	for _, connector := range connectors {
		views = append(views, s.buildConnectorView(connector))
	}
	return views
}

func (s *Server) buildConnectorView(connector Connector) connectorView {
	view := connectorView{
		ID:        connector.ID,
		TenantID:  connector.TenantID,
		Name:      connector.Name,
		CreatedAt: connector.CreatedAt,
		UpdatedAt: connector.UpdatedAt,
	}
	if connection, connected := s.hub.GetConnectorConnection(connector.ID); connected {
		view.Connected = connection.Connected
		view.AgentID = connection.AgentID
		view.LastSeen = connection.LastSeen
	}
	return view
}

func (s *Server) buildTenantViews() []tenantView {
	tenants := s.ruleStore.ListTenants()
	routeCounts := s.ruleStore.RouteCountByTenant()

	views := make([]tenantView, 0, len(tenants))
	for _, tenant := range tenants {
		views = append(views, tenantView{
			ID:         tenant.ID,
			Name:       tenant.Name,
			RouteCount: routeCounts[tenant.ID],
			CreatedAt:  tenant.CreatedAt,
			UpdatedAt:  tenant.UpdatedAt,
		})
	}
	return views
}

func (s *Server) buildTunnelViews() []tunnelView {
	connected := s.hub.SnapshotTunnels()
	viewsByKey := make(map[string]tunnelView, len(connected))

	for _, tunnel := range connected {
		tenantID, routeID := ParseTunnelKey(tunnel.ID)
		if routeID == "" {
			continue
		}
		canonicalKey := MakeTunnelKey(tenantID, routeID)
		legacyURL := ""
		if tenantID == DefaultTenantID {
			legacyURL = s.legacyRoutePublicURL(routeID)
		}

		viewsByKey[canonicalKey] = tunnelView{
			TenantID:        tenantID,
			RouteID:         routeID,
			ID:              routeID,
			TunnelKey:       tunnel.ID,
			Target:          tunnel.Target,
			RequiresToken:   tunnel.RequiresToken,
			AgentID:         tunnel.AgentID,
			PublicURL:       s.routePublicURL(tenantID, routeID),
			LegacyPublicURL: legacyURL,
			Metrics:         s.metricForRoute(tenantID, routeID),
			Connection:      tunnel.Connection,
			Source:          "agent",
		}
	}

	for _, rule := range s.ruleStore.ListAll() {
		canonicalKey := MakeTunnelKey(rule.TenantID, rule.ID)
		legacyURL := ""
		if rule.TenantID == DefaultTenantID {
			legacyURL = s.legacyRoutePublicURL(rule.ID)
		}

		if existing, ok := viewsByKey[canonicalKey]; ok {
			existing.Source = "agent+rule"
			if !existing.RequiresToken && strings.TrimSpace(rule.Token) != "" {
				existing.RequiresToken = true
			}
			if existing.Target == "" {
				existing.Target = rule.Target
			}
			viewsByKey[canonicalKey] = existing
			continue
		}

		viewsByKey[canonicalKey] = tunnelView{
			TenantID:        rule.TenantID,
			RouteID:         rule.ID,
			ID:              rule.ID,
			TunnelKey:       canonicalKey,
			Target:          rule.Target,
			RequiresToken:   strings.TrimSpace(rule.Token) != "",
			PublicURL:       s.routePublicURL(rule.TenantID, rule.ID),
			LegacyPublicURL: legacyURL,
			Metrics:         s.metricForRoute(rule.TenantID, rule.ID),
			Connection:      ConnectionSnapshot{Connected: false},
			Source:          "rule",
		}
		if rule.UsesConnector() {
			if connectorConn, connected := s.hub.GetConnectorConnection(rule.ConnectorID); connected {
				view := viewsByKey[canonicalKey]
				view.Connection.Connected = true
				view.AgentID = connectorConn.AgentID
				view.Source = "connector+rule"
				viewsByKey[canonicalKey] = view
			} else {
				view := viewsByKey[canonicalKey]
				view.Source = "connector"
				viewsByKey[canonicalKey] = view
			}
		}
	}

	views := make([]tunnelView, 0, len(viewsByKey))
	for _, view := range viewsByKey {
		views = append(views, view)
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].TenantID == views[j].TenantID {
			return views[i].RouteID < views[j].RouteID
		}
		return views[i].TenantID < views[j].TenantID
	})

	return views
}

func (s *Server) buildRouteViews(tenantID string) []routeView {
	routes := s.ruleStore.ListForTenant(tenantID)
	connected := s.hub.SnapshotTunnels()
	connectedByKey := make(map[string]TunnelSnapshot, len(connected))
	for _, tunnel := range connected {
		t, r := ParseTunnelKey(tunnel.ID)
		if r == "" {
			continue
		}
		connectedByKey[MakeTunnelKey(t, r)] = tunnel
	}

	views := make([]routeView, 0, len(routes))
	for _, route := range routes {
		views = append(views, s.buildRouteViewWithConnected(route, connectedByKey))
	}

	sort.Slice(views, func(i, j int) bool {
		if views[i].TenantID == views[j].TenantID {
			return views[i].RouteID < views[j].RouteID
		}
		return views[i].TenantID < views[j].TenantID
	})
	return views
}

func (s *Server) buildRouteView(route Rule) routeView {
	connected := s.hub.SnapshotTunnels()
	connectedByKey := make(map[string]TunnelSnapshot, len(connected))
	for _, tunnel := range connected {
		t, r := ParseTunnelKey(tunnel.ID)
		if r == "" {
			continue
		}
		connectedByKey[MakeTunnelKey(t, r)] = tunnel
	}
	return s.buildRouteViewWithConnected(route, connectedByKey)
}

func (s *Server) buildRouteViewWithConnected(route Rule, connectedByKey map[string]TunnelSnapshot) routeView {
	canonicalKey := MakeTunnelKey(route.TenantID, route.ID)
	legacyURL := ""
	if route.TenantID == DefaultTenantID {
		legacyURL = s.legacyRoutePublicURL(route.ID)
	}

	view := routeView{
		TenantID:        route.TenantID,
		RouteID:         route.ID,
		ID:              route.ID,
		TunnelKey:       canonicalKey,
		Target:          route.Target,
		MaxRPS:          route.MaxRPS,
		ConnectorID:     route.ConnectorID,
		LocalScheme:     route.LocalScheme,
		LocalHost:       route.LocalHost,
		LocalPort:       route.LocalPort,
		LocalBasePath:   route.LocalBasePath,
		PublicURL:       s.routePublicURL(route.TenantID, route.ID),
		LegacyPublicURL: legacyURL,
		TokenConfigured: strings.TrimSpace(route.Token) != "",
		Metrics:         s.metricForRoute(route.TenantID, route.ID),
		CreatedAt:       route.CreatedAt,
		UpdatedAt:       route.UpdatedAt,
	}

	if route.UsesConnector() {
		if connectorConn, ok := s.hub.GetConnectorConnection(route.ConnectorID); ok {
			view.Connected = connectorConn.Connected
			view.AgentID = connectorConn.AgentID
		}
	} else if connected, ok := connectedByKey[canonicalKey]; ok {
		view.Connected = true
		view.AgentID = connected.AgentID
	}
	return view
}

func (s *Server) resolveProxyPath(path string) (resolvedProxyPath, error) {
	if !strings.HasPrefix(path, "/t/") {
		return resolvedProxyPath{}, errors.New("invalid route; expected /t/{route}/... or /t/{tenant}/{route}/...")
	}

	suffix := strings.TrimPrefix(path, "/t/")
	suffix = strings.TrimPrefix(suffix, "/")
	if strings.TrimSpace(suffix) == "" {
		return resolvedProxyPath{}, errors.New("missing route path")
	}

	segments := strings.Split(suffix, "/")
	if len(segments) == 0 {
		return resolvedProxyPath{}, errors.New("missing route path")
	}

	first := strings.TrimSpace(segments[0])
	if first == "" {
		return resolvedProxyPath{}, errors.New("missing route id")
	}

	// Legacy: /t/{route}/... -> default tenant.
	if len(segments) == 1 {
		return resolvedProxyPath{
			TenantID:    DefaultTenantID,
			RouteID:     first,
			ForwardPath: "/",
		}, nil
	}

	second := strings.TrimSpace(segments[1])
	if second == "" {
		return resolvedProxyPath{
			TenantID:    DefaultTenantID,
			RouteID:     first,
			ForwardPath: "/",
		}, nil
	}

	tenantCandidate := first
	routeCandidate := second
	multiTenantForwardPath := joinForwardPath(segments[2:])

	if s.shouldUseTenantRoute(tenantCandidate, routeCandidate) {
		return resolvedProxyPath{
			TenantID:    tenantCandidate,
			RouteID:     routeCandidate,
			ForwardPath: multiTenantForwardPath,
		}, nil
	}

	// Fallback to legacy interpretation for backward compatibility:
	// /t/{route}/{path...}
	legacyForwardPath := joinForwardPath(segments[1:])
	return resolvedProxyPath{
		TenantID:    DefaultTenantID,
		RouteID:     first,
		ForwardPath: legacyForwardPath,
	}, nil
}

func (s *Server) shouldUseTenantRoute(tenantID, routeID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	routeID = strings.TrimSpace(routeID)
	if tenantID == "" || routeID == "" {
		return false
	}
	if !s.ruleStore.HasTenant(tenantID) {
		return false
	}
	if _, ok := s.ruleStore.GetForTenant(tenantID, routeID); ok {
		return true
	}
	for _, key := range s.lookupTunnelKeys(tenantID, routeID) {
		if s.hub.IsTunnelConnected(key) {
			return true
		}
	}
	return true
}

func (s *Server) lookupTunnelKeys(tenantID, routeID string) []string {
	candidates := []string{MakeTunnelKey(tenantID, routeID)}
	if tenantID == DefaultTenantID {
		candidates = append(candidates, strings.TrimSpace(routeID))
	}

	unique := make([]string, 0, len(candidates))
	seen := make(map[string]struct{})
	for _, key := range candidates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	return unique
}

func (s *Server) firstConnectedTunnelKey(candidates []string) (string, bool) {
	for _, key := range candidates {
		if s.hub.IsTunnelConnected(key) {
			return key, true
		}
	}
	return "", false
}

func (s *Server) lookupTunnelToken(candidates []string) string {
	for _, key := range candidates {
		if token := strings.TrimSpace(s.hub.GetTunnelToken(key)); token != "" {
			return token
		}
	}
	return ""
}

func (s *Server) metricForRoute(tenantID, routeID string) TunnelMetrics {
	candidates := s.lookupTunnelKeys(tenantID, routeID)
	if len(candidates) == 0 {
		metric := TunnelMetrics{TunnelID: MakeTunnelKey(tenantID, routeID)}
		return metric
	}

	combined := TunnelMetrics{TunnelID: MakeTunnelKey(tenantID, routeID)}
	latestSeen := time.Time{}
	for _, key := range candidates {
		metric := s.hub.GetTunnelMetrics(key)
		combined.RequestCount += metric.RequestCount
		combined.ErrorCount += metric.ErrorCount
		combined.BytesIn += metric.BytesIn
		combined.BytesOut += metric.BytesOut
		combined.TotalLatencyMs += metric.TotalLatencyMs
		if metric.LastSeen.After(latestSeen) {
			latestSeen = metric.LastSeen
			combined.LastSeen = metric.LastSeen
			combined.LastStatus = metric.LastStatus
			combined.LastError = metric.LastError
		}
	}
	if combined.RequestCount > 0 {
		combined.AverageLatencyMs = float64(combined.TotalLatencyMs) / float64(combined.RequestCount)
	}
	return combined
}

func (s *Server) routePublicURL(tenantID, routeID string) string {
	base := strings.TrimRight(s.cfg.PublicBaseURL, "/")
	return base + "/t/" + url.PathEscape(tenantID) + "/" + url.PathEscape(routeID) + "/"
}

func (s *Server) legacyRoutePublicURL(routeID string) string {
	base := strings.TrimRight(s.cfg.PublicBaseURL, "/")
	return base + "/t/" + url.PathEscape(routeID) + "/"
}

func parseTenantSubresourcePath(path string) ([]string, error) {
	if !strings.HasPrefix(path, "/api/tenants/") {
		return nil, errors.New("invalid tenant path")
	}
	suffix := strings.TrimPrefix(path, "/api/tenants/")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		return nil, errors.New("missing tenant path")
	}

	rawSegments := strings.Split(suffix, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, raw := range rawSegments {
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			return nil, fmt.Errorf("decode tenant path segment: %w", err)
		}
		decoded = strings.TrimSpace(decoded)
		if decoded == "" {
			return nil, errors.New("tenant path contains empty segment")
		}
		segments = append(segments, decoded)
	}
	return segments, nil
}

func parseRulePathID(path string) (string, error) {
	if !strings.HasPrefix(path, "/api/rules/") {
		return "", errors.New("invalid rules path")
	}
	rawID := strings.TrimPrefix(path, "/api/rules/")
	if strings.TrimSpace(rawID) == "" {
		return "", errors.New("missing rule id")
	}
	decodedID, err := url.PathUnescape(rawID)
	if err != nil {
		return "", fmt.Errorf("decode rule id: %w", err)
	}
	decodedID = strings.TrimSpace(decodedID)
	if decodedID == "" {
		return "", errors.New("missing rule id")
	}
	return decodedID, nil
}

func parseConnectorPath(path string) (connectorID, action string, err error) {
	if !strings.HasPrefix(path, "/api/connectors/") {
		return "", "", errors.New("invalid connectors path")
	}
	suffix := strings.TrimPrefix(path, "/api/connectors/")
	suffix = strings.Trim(suffix, "/")
	if suffix == "" {
		return "", "", errors.New("missing connector id")
	}

	rawSegments := strings.Split(suffix, "/")
	if len(rawSegments) > 2 {
		return "", "", errors.New("invalid connectors path")
	}

	decodedConnectorID, err := url.PathUnescape(rawSegments[0])
	if err != nil {
		return "", "", fmt.Errorf("decode connector id: %w", err)
	}
	decodedConnectorID = strings.TrimSpace(decodedConnectorID)
	if decodedConnectorID == "" {
		return "", "", errors.New("missing connector id")
	}

	if len(rawSegments) == 1 {
		return decodedConnectorID, "", nil
	}

	decodedAction, err := url.PathUnescape(rawSegments[1])
	if err != nil {
		return "", "", fmt.Errorf("decode connector action: %w", err)
	}
	decodedAction = strings.TrimSpace(decodedAction)
	if decodedAction == "" {
		return "", "", errors.New("missing connector action")
	}
	return decodedConnectorID, decodedAction, nil
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

func joinForwardPath(segments []string) string {
	if len(segments) == 0 {
		return "/"
	}
	joined := strings.Join(segments, "/")
	joined = strings.TrimPrefix(joined, "/")
	if joined == "" {
		return "/"
	}
	return "/" + joined
}

func joinWithBasePath(basePath, path string) string {
	basePath = strings.TrimSpace(basePath)
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if basePath == "" || basePath == "/" {
		return path
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = strings.TrimSuffix(basePath, "/")
	return basePath + path
}

func enrichForwardHeaders(headers map[string][]string, r *http.Request) {
	appendForwardHeader(headers, "X-Forwarded-Host", r.Host)
	appendForwardHeader(headers, "X-Forwarded-Proto", requestProto(r))
	if port := requestPort(r); port != "" {
		appendForwardHeader(headers, "X-Forwarded-Port", port)
	}
	if remoteIP := extractIP(r.RemoteAddr); remoteIP != "" {
		appendForwardHeader(headers, "X-Forwarded-For", remoteIP)
	}
}

func appendForwardHeader(headers map[string][]string, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	headers[key] = append(headers[key], value)
}

func requestProto(r *http.Request) string {
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestPort(r *http.Request) string {
	if port := strings.TrimSpace(r.Header.Get("X-Forwarded-Port")); port != "" {
		return port
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
			_ = parsedHost
			return parsedPort
		}
	}
	if requestProto(r) == "https" {
		return "443"
	}
	return "80"
}

func extractIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, target any, label string) bool {
	reader := http.MaxBytesReader(w, r.Body, s.maxRequestBodyBytes)
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "payload exceeds request body limit", http.StatusRequestEntityTooLarge)
			return false
		}
		http.Error(w, fmt.Sprintf("invalid %s: %v", label, err), http.StatusBadRequest)
		return false
	}
	return true
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

func (s *Server) writeDispatchError(w http.ResponseWriter, tunnelKey string, bytesIn int64, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, ErrAgentQueueFull), errors.Is(err, ErrGlobalBackpressure):
		status = http.StatusServiceUnavailable
	case errors.Is(err, ErrProxyRequestTimeout), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	case errors.Is(err, ErrTunnelNotConnected), errors.Is(err, ErrConnectorNotConnected), errors.Is(err, ErrUnknownSession):
		status = http.StatusBadGateway
	}
	s.hub.RecordProxyFailure(tunnelKey, bytesIn, err.Error())
	s.maybeRecordProxyIncident(err, tunnelKey)
	http.Error(w, fmt.Sprintf("proxy dispatch failed: %v", err), status)
}

func (s *Server) validateConnectorRouteBinding(tenantID, connectorID string) error {
	connectorID = strings.TrimSpace(connectorID)
	if connectorID == "" {
		return nil
	}
	connector, ok := s.connectorStore.Get(connectorID)
	if !ok {
		return fmt.Errorf("connector %q not found", connectorID)
	}
	if strings.TrimSpace(connector.TenantID) != strings.TrimSpace(tenantID) {
		return fmt.Errorf("connector %q belongs to tenant %q, not %q", connectorID, connector.TenantID, tenantID)
	}
	return nil
}

func (s *Server) nextRequestID() string {
	value := atomic.AddUint64(&s.requestCounter, 1)
	return fmt.Sprintf("gw-%d-%d", time.Now().UnixNano(), value)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}
