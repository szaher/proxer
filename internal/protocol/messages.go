package protocol

type TunnelConfig struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Token  string `json:"token,omitempty"`
}

type TunnelRoute struct {
	ID        string `json:"id"`
	PublicURL string `json:"public_url"`
}

type RegisterRequest struct {
	AgentID         string         `json:"agent_id"`
	Token           string         `json:"token,omitempty"`
	Tunnels         []TunnelConfig `json:"tunnels,omitempty"`
	ConnectorID     string         `json:"connector_id,omitempty"`
	ConnectorSecret string         `json:"connector_secret,omitempty"`
}

type RegisterResponse struct {
	Accepted      bool          `json:"accepted"`
	Message       string        `json:"message,omitempty"`
	SessionID     string        `json:"session_id,omitempty"`
	PublicBaseURL string        `json:"public_base_url,omitempty"`
	Tunnels       []TunnelRoute `json:"tunnels,omitempty"`
}

type PullResponse struct {
	Request *ProxyRequest `json:"request,omitempty"`
}

type PairAgentRequest struct {
	PairToken string `json:"pair_token"`
	AgentID   string `json:"agent_id,omitempty"`
}

type PairAgentResponse struct {
	ConnectorID     string `json:"connector_id"`
	ConnectorSecret string `json:"connector_secret"`
	TenantID        string `json:"tenant_id"`
}

type LocalTarget struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type SubmitResponseRequest struct {
	SessionID string         `json:"session_id"`
	Response  *ProxyResponse `json:"response"`
}

type HeartbeatRequest struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id,omitempty"`
}

type ProxyRequest struct {
	RequestID   string              `json:"request_id"`
	TunnelID    string              `json:"tunnel_id"`
	ConnectorID string              `json:"connector_id,omitempty"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	Query       string              `json:"query,omitempty"`
	Headers     map[string][]string `json:"headers,omitempty"`
	Body        []byte              `json:"body,omitempty"`
	RemoteAddr  string              `json:"remote_addr,omitempty"`
	LocalTarget *LocalTarget        `json:"local_target,omitempty"`
}

type ProxyResponse struct {
	RequestID string              `json:"request_id"`
	TunnelID  string              `json:"tunnel_id"`
	Status    int                 `json:"status"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Body      []byte              `json:"body,omitempty"`
	Error     string              `json:"error,omitempty"`
	LatencyMs int64               `json:"latency_ms,omitempty"`
	BytesIn   int64               `json:"bytes_in,omitempty"`
	BytesOut  int64               `json:"bytes_out,omitempty"`
}
