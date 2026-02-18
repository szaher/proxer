package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Connector struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PairToken struct {
	Token       string    `json:"token"`
	ConnectorID string    `json:"connector_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

type connectorCredential struct {
	ConnectorID string
	SecretHash  string
	UpdatedAt   time.Time
}

type pairTokenRecord struct {
	token PairToken
	used  bool
}

type ConnectorStore struct {
	pairTokenTTL time.Duration

	mu          sync.RWMutex
	connectors  map[string]Connector
	credentials map[string]connectorCredential
	pairTokens  map[string]pairTokenRecord
}

func NewConnectorStore(pairTokenTTL time.Duration) *ConnectorStore {
	if pairTokenTTL <= 0 {
		pairTokenTTL = 10 * time.Minute
	}
	return &ConnectorStore{
		pairTokenTTL: pairTokenTTL,
		connectors:   make(map[string]Connector),
		credentials:  make(map[string]connectorCredential),
		pairTokens:   make(map[string]pairTokenRecord),
	}
}

func (s *ConnectorStore) Create(input Connector) (Connector, error) {
	id := normalizeIdentifier(input.ID)
	if !identifierPattern.MatchString(id) {
		return Connector{}, fmt.Errorf("invalid connector id %q (allowed: letters, numbers, _, -, max 64)", id)
	}

	tenantID := normalizeIdentifier(input.TenantID)
	if !identifierPattern.MatchString(tenantID) {
		return Connector{}, fmt.Errorf("invalid tenant id %q", tenantID)
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = id
	}

	now := time.Now().UTC()
	connector := Connector{
		ID:        id,
		TenantID:  tenantID,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredPairTokensLocked(now)

	if _, exists := s.connectors[id]; exists {
		return Connector{}, fmt.Errorf("connector %q already exists", id)
	}
	s.connectors[id] = connector
	return connector, nil
}

func (s *ConnectorStore) Get(id string) (Connector, bool) {
	id = normalizeIdentifier(id)
	if id == "" {
		return Connector{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	connector, ok := s.connectors[id]
	return connector, ok
}

func (s *ConnectorStore) ListForTenants(tenantIDs []string) []Connector {
	allowed := make(map[string]struct{}, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		tenantID = normalizeIdentifier(tenantID)
		if tenantID == "" {
			continue
		}
		allowed[tenantID] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	connectors := make([]Connector, 0)
	for _, connector := range s.connectors {
		if len(allowed) > 0 {
			if _, ok := allowed[connector.TenantID]; !ok {
				continue
			}
		}
		connectors = append(connectors, connector)
	}

	sort.Slice(connectors, func(i, j int) bool {
		if connectors[i].TenantID == connectors[j].TenantID {
			return connectors[i].ID < connectors[j].ID
		}
		return connectors[i].TenantID < connectors[j].TenantID
	})
	return connectors
}

func (s *ConnectorStore) ListAll() []Connector {
	s.mu.RLock()
	defer s.mu.RUnlock()

	connectors := make([]Connector, 0, len(s.connectors))
	for _, connector := range s.connectors {
		connectors = append(connectors, connector)
	}
	sort.Slice(connectors, func(i, j int) bool {
		if connectors[i].TenantID == connectors[j].TenantID {
			return connectors[i].ID < connectors[j].ID
		}
		return connectors[i].TenantID < connectors[j].TenantID
	})
	return connectors
}

func (s *ConnectorStore) CountByTenant(tenantID string) int {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, connector := range s.connectors {
		if connector.TenantID == tenantID {
			count++
		}
	}
	return count
}

func (s *ConnectorStore) Delete(id string) bool {
	id = normalizeIdentifier(id)
	if id == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.connectors[id]; !ok {
		return false
	}
	delete(s.connectors, id)
	delete(s.credentials, id)
	for token, record := range s.pairTokens {
		if record.token.ConnectorID == id {
			delete(s.pairTokens, token)
		}
	}
	return true
}

func (s *ConnectorStore) NewPairToken(connectorID string) (PairToken, error) {
	connectorID = normalizeIdentifier(connectorID)
	if connectorID == "" {
		return PairToken{}, fmt.Errorf("missing connector id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupExpiredPairTokensLocked(now)

	if _, ok := s.connectors[connectorID]; !ok {
		return PairToken{}, fmt.Errorf("connector %q not found", connectorID)
	}

	tokenValue, err := randomToken(24)
	if err != nil {
		return PairToken{}, err
	}

	token := PairToken{
		Token:       tokenValue,
		ConnectorID: connectorID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.pairTokenTTL),
	}
	s.pairTokens[token.Token] = pairTokenRecord{token: token}
	return token, nil
}

func (s *ConnectorStore) ConsumePairToken(pairToken string) (Connector, string, error) {
	pairToken = strings.TrimSpace(pairToken)
	if pairToken == "" {
		return Connector{}, "", fmt.Errorf("missing pair token")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupExpiredPairTokensLocked(now)

	record, ok := s.pairTokens[pairToken]
	if !ok {
		return Connector{}, "", fmt.Errorf("pair token is invalid or expired")
	}
	if record.used {
		return Connector{}, "", fmt.Errorf("pair token already used")
	}
	if now.After(record.token.ExpiresAt) {
		delete(s.pairTokens, pairToken)
		return Connector{}, "", fmt.Errorf("pair token is expired")
	}

	connector, ok := s.connectors[record.token.ConnectorID]
	if !ok {
		delete(s.pairTokens, pairToken)
		return Connector{}, "", fmt.Errorf("connector not found for pair token")
	}

	secret, err := randomToken(32)
	if err != nil {
		return Connector{}, "", err
	}
	s.credentials[connector.ID] = connectorCredential{
		ConnectorID: connector.ID,
		SecretHash:  hashConnectorSecret(secret),
		UpdatedAt:   now,
	}
	record.used = true
	s.pairTokens[pairToken] = record
	return connector, secret, nil
}

func (s *ConnectorStore) RotateCredential(connectorID string) (string, error) {
	connectorID = normalizeIdentifier(connectorID)
	if connectorID == "" {
		return "", fmt.Errorf("missing connector id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.connectors[connectorID]; !ok {
		return "", fmt.Errorf("connector %q not found", connectorID)
	}
	secret, err := randomToken(32)
	if err != nil {
		return "", err
	}
	s.credentials[connectorID] = connectorCredential{
		ConnectorID: connectorID,
		SecretHash:  hashConnectorSecret(secret),
		UpdatedAt:   time.Now().UTC(),
	}
	return secret, nil
}

func (s *ConnectorStore) Authenticate(connectorID, secret string) bool {
	connectorID = normalizeIdentifier(connectorID)
	secret = strings.TrimSpace(secret)
	if connectorID == "" || secret == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	credential, ok := s.credentials[connectorID]
	if !ok {
		return false
	}
	return credential.SecretHash == hashConnectorSecret(secret)
}

func (s *ConnectorStore) cleanupExpiredPairTokensLocked(now time.Time) {
	for token, record := range s.pairTokens {
		if now.After(record.token.ExpiresAt) || record.used {
			delete(s.pairTokens, token)
		}
	}
}

func hashConnectorSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	sum := sha256.Sum256([]byte("proxer-connector-v1:" + secret))
	return hex.EncodeToString(sum[:])
}
