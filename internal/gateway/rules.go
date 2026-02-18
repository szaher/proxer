package gateway

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultTenantID = "default"

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TenantEnvironment struct {
	TenantID    string            `json:"tenant_id"`
	Scheme      string            `json:"scheme"`
	Host        string            `json:"host"`
	DefaultPort int               `json:"default_port"`
	Variables   map[string]string `json:"variables,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type Rule struct {
	TenantID      string    `json:"tenant_id,omitempty"`
	ID            string    `json:"id"`
	Target        string    `json:"target"`
	Token         string    `json:"token,omitempty"`
	MaxRPS        float64   `json:"max_rps,omitempty"`
	ConnectorID   string    `json:"connector_id,omitempty"`
	LocalScheme   string    `json:"local_scheme,omitempty"`
	LocalHost     string    `json:"local_host,omitempty"`
	LocalPort     int       `json:"local_port,omitempty"`
	LocalBasePath string    `json:"local_base_path,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RuleStore struct {
	mu      sync.RWMutex
	tenants map[string]Tenant
	envs    map[string]TenantEnvironment
	rules   map[string]Rule
}

func NewRuleStore() *RuleStore {
	now := time.Now().UTC()
	defaultTenant := Tenant{
		ID:        DefaultTenantID,
		Name:      "Default",
		CreatedAt: now,
		UpdatedAt: now,
	}
	defaultEnv := TenantEnvironment{
		TenantID:    DefaultTenantID,
		Scheme:      "http",
		Host:        "host.docker.internal",
		DefaultPort: 3000,
		Variables:   map[string]string{},
		UpdatedAt:   now,
	}
	return &RuleStore{
		tenants: map[string]Tenant{DefaultTenantID: defaultTenant},
		envs:    map[string]TenantEnvironment{DefaultTenantID: defaultEnv},
		rules:   make(map[string]Rule),
	}
}

func (s *RuleStore) UpsertTenant(input Tenant) (Tenant, error) {
	tenantID := normalizeIdentifier(input.ID)
	if !identifierPattern.MatchString(tenantID) {
		return Tenant{}, fmt.Errorf("invalid tenant id %q (allowed: letters, numbers, _, -, max 64)", tenantID)
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = tenantID
	}

	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.tenants[tenantID]
	if !ok {
		existing.CreatedAt = now
	}
	existing.ID = tenantID
	existing.Name = name
	existing.UpdatedAt = now
	s.tenants[tenantID] = existing
	if _, ok := s.envs[tenantID]; !ok {
		s.envs[tenantID] = TenantEnvironment{
			TenantID:    tenantID,
			Scheme:      "http",
			Host:        "host.docker.internal",
			DefaultPort: 3000,
			Variables:   map[string]string{},
			UpdatedAt:   now,
		}
	}
	return existing, nil
}

func (s *RuleStore) DeleteTenant(tenantID string) bool {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" || tenantID == DefaultTenantID {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[tenantID]; !ok {
		return false
	}
	delete(s.tenants, tenantID)
	delete(s.envs, tenantID)
	for key, rule := range s.rules {
		if rule.TenantID == tenantID {
			delete(s.rules, key)
		}
	}
	return true
}

func (s *RuleStore) ListTenants() []Tenant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		tenants = append(tenants, tenant)
	}

	sort.Slice(tenants, func(i, j int) bool {
		return tenants[i].ID < tenants[j].ID
	})
	return tenants
}

func (s *RuleStore) HasTenant(tenantID string) bool {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tenants[tenantID]
	return ok
}

func (s *RuleStore) UpsertForTenant(tenantID string, input Rule) (Rule, error) {
	tenantID = normalizeIdentifier(tenantID)
	if !identifierPattern.MatchString(tenantID) {
		return Rule{}, fmt.Errorf("invalid tenant id %q", tenantID)
	}

	routeID := normalizeIdentifier(input.ID)
	if !identifierPattern.MatchString(routeID) {
		return Rule{}, fmt.Errorf("invalid route id %q (allowed: letters, numbers, _, -, max 64)", routeID)
	}

	target := strings.TrimSpace(input.Target)
	token := strings.TrimSpace(input.Token)
	connectorID := normalizeIdentifier(input.ConnectorID)
	localScheme := strings.ToLower(strings.TrimSpace(input.LocalScheme))
	localHost := strings.TrimSpace(input.LocalHost)
	localPort := input.LocalPort
	localBasePath := strings.TrimSpace(input.LocalBasePath)
	maxRPS := input.MaxRPS
	if maxRPS < 0 {
		return Rule{}, fmt.Errorf("max_rps cannot be negative")
	}

	if connectorID == "" {
		parsedTarget, err := url.Parse(target)
		if err != nil {
			return Rule{}, fmt.Errorf("invalid target URL: %w", err)
		}
		if parsedTarget.Scheme != "http" && parsedTarget.Scheme != "https" {
			return Rule{}, fmt.Errorf("target URL must use http or https")
		}
		if strings.TrimSpace(parsedTarget.Host) == "" {
			return Rule{}, fmt.Errorf("target URL must include a host")
		}
	} else {
		if !identifierPattern.MatchString(connectorID) {
			return Rule{}, fmt.Errorf("invalid connector id %q", connectorID)
		}
		if localScheme == "" {
			localScheme = "http"
		}
		if localScheme != "http" && localScheme != "https" {
			return Rule{}, fmt.Errorf("local_scheme must be http or https")
		}
		if localHost == "" {
			localHost = "127.0.0.1"
		}
		if strings.Contains(localHost, "://") {
			return Rule{}, fmt.Errorf("local_host should not include scheme")
		}
		if localPort < 1 || localPort > 65535 {
			return Rule{}, fmt.Errorf("local_port must be between 1 and 65535 when connector_id is set")
		}
		if localBasePath != "" && !strings.HasPrefix(localBasePath, "/") {
			localBasePath = "/" + localBasePath
		}
		if target == "" {
			target = fmt.Sprintf("%s://%s:%d%s", localScheme, localHost, localPort, localBasePath)
		}
	}

	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[tenantID]; !ok {
		return Rule{}, fmt.Errorf("tenant %q not found", tenantID)
	}

	key := ruleKey(tenantID, routeID)
	existing, ok := s.rules[key]
	if !ok {
		existing.CreatedAt = now
	}
	existing.TenantID = tenantID
	existing.ID = routeID
	existing.Target = target
	existing.Token = token
	existing.MaxRPS = maxRPS
	existing.ConnectorID = connectorID
	existing.LocalScheme = localScheme
	existing.LocalHost = localHost
	existing.LocalPort = localPort
	existing.LocalBasePath = localBasePath
	existing.UpdatedAt = now
	s.rules[key] = existing
	return existing, nil
}

func (r Rule) UsesConnector() bool {
	return strings.TrimSpace(r.ConnectorID) != ""
}

func (s *RuleStore) DeleteForTenant(tenantID, routeID string) bool {
	tenantID = normalizeIdentifier(tenantID)
	routeID = normalizeIdentifier(routeID)
	if tenantID == "" || routeID == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := ruleKey(tenantID, routeID)
	if _, ok := s.rules[key]; !ok {
		return false
	}
	delete(s.rules, key)
	return true
}

func (s *RuleStore) GetForTenant(tenantID, routeID string) (Rule, bool) {
	tenantID = normalizeIdentifier(tenantID)
	routeID = normalizeIdentifier(routeID)
	if tenantID == "" || routeID == "" {
		return Rule{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rule, ok := s.rules[ruleKey(tenantID, routeID)]
	return rule, ok
}

func (s *RuleStore) ListForTenant(tenantID string) []Rule {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]Rule, 0)
	for _, rule := range s.rules {
		if rule.TenantID == tenantID {
			rules = append(rules, rule)
		}
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].ID < rules[j].ID
	})
	return rules
}

func (s *RuleStore) ListAll() []Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]Rule, 0, len(s.rules))
	for _, rule := range s.rules {
		rules = append(rules, rule)
	}

	sort.Slice(rules, func(i, j int) bool {
		if rules[i].TenantID == rules[j].TenantID {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].TenantID < rules[j].TenantID
	})
	return rules
}

func (s *RuleStore) RouteCountByTenant() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := make(map[string]int)
	for tenantID := range s.tenants {
		counts[tenantID] = 0
	}
	for _, rule := range s.rules {
		counts[rule.TenantID]++
	}
	return counts
}

func (s *RuleStore) GetEnvironment(tenantID string) (TenantEnvironment, bool) {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return TenantEnvironment{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	env, ok := s.envs[tenantID]
	if !ok {
		return TenantEnvironment{}, false
	}
	// Deep copy map to avoid exposing internal references.
	env.Variables = copyStringMap(env.Variables)
	return env, true
}

func (s *RuleStore) UpsertEnvironment(input TenantEnvironment) (TenantEnvironment, error) {
	tenantID := normalizeIdentifier(input.TenantID)
	if !identifierPattern.MatchString(tenantID) {
		return TenantEnvironment{}, fmt.Errorf("invalid tenant id %q", tenantID)
	}

	scheme := strings.ToLower(strings.TrimSpace(input.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return TenantEnvironment{}, fmt.Errorf("scheme must be http or https")
	}

	host := strings.TrimSpace(input.Host)
	if host == "" {
		return TenantEnvironment{}, fmt.Errorf("host cannot be empty")
	}
	if strings.Contains(host, "://") {
		return TenantEnvironment{}, fmt.Errorf("host should not include scheme")
	}

	port := input.DefaultPort
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	if port < 1 || port > 65535 {
		return TenantEnvironment{}, fmt.Errorf("default_port must be between 1 and 65535")
	}

	now := time.Now().UTC()
	variables := copyStringMap(input.Variables)
	if variables == nil {
		variables = map[string]string{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[tenantID]; !ok {
		return TenantEnvironment{}, fmt.Errorf("tenant %q not found", tenantID)
	}

	env := TenantEnvironment{
		TenantID:    tenantID,
		Scheme:      scheme,
		Host:        host,
		DefaultPort: port,
		Variables:   variables,
		UpdatedAt:   now,
	}
	s.envs[tenantID] = env
	return env, nil
}

// Backward-compatible default-tenant helpers.
func (s *RuleStore) Upsert(input Rule) (Rule, error) {
	return s.UpsertForTenant(DefaultTenantID, input)
}

func (s *RuleStore) Delete(id string) bool {
	return s.DeleteForTenant(DefaultTenantID, id)
}

func (s *RuleStore) Get(id string) (Rule, bool) {
	return s.GetForTenant(DefaultTenantID, id)
}

func (s *RuleStore) List() []Rule {
	return s.ListForTenant(DefaultTenantID)
}

func normalizeIdentifier(value string) string {
	return strings.TrimSpace(value)
}

func ruleKey(tenantID, routeID string) string {
	return tenantID + "/" + routeID
}

func MakeTunnelKey(tenantID, routeID string) string {
	tenantID = normalizeIdentifier(tenantID)
	routeID = normalizeIdentifier(routeID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	return ruleKey(tenantID, routeID)
}

func copyStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for k, v := range input {
		output[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return output
}

func ParseTunnelKey(tunnelID string) (tenantID string, routeID string) {
	tunnelID = normalizeIdentifier(tunnelID)
	if tunnelID == "" {
		return DefaultTenantID, ""
	}
	parts := strings.SplitN(tunnelID, "/", 2)
	if len(parts) == 2 {
		tenantPart := normalizeIdentifier(parts[0])
		routePart := normalizeIdentifier(parts[1])
		if identifierPattern.MatchString(tenantPart) && identifierPattern.MatchString(routePart) {
			return tenantPart, routePart
		}
	}
	return DefaultTenantID, tunnelID
}
