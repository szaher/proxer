package gateway

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type SystemIncident struct {
	ID         string     `json:"id"`
	Severity   string     `json:"severity"`
	Source     string     `json:"source"`
	Message    string     `json:"message"`
	CreatedAt  time.Time  `json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type IncidentStore struct {
	mu      sync.RWMutex
	items   map[string]SystemIncident
	counter uint64
}

func NewIncidentStore() *IncidentStore {
	return &IncidentStore{
		items: make(map[string]SystemIncident),
	}
}

func (s *IncidentStore) Add(severity, source, message string) SystemIncident {
	severity = strings.ToLower(strings.TrimSpace(severity))
	if severity == "" {
		severity = "info"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "gateway"
	}
	message = strings.TrimSpace(message)

	incident := SystemIncident{
		ID:        fmt.Sprintf("inc-%d-%d", time.Now().UnixNano(), atomic.AddUint64(&s.counter, 1)),
		Severity:  severity,
		Source:    source,
		Message:   message,
		CreatedAt: time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[incident.ID] = incident
	return incident
}

func (s *IncidentStore) Resolve(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	incident, ok := s.items[id]
	if !ok {
		return false
	}
	now := time.Now().UTC()
	incident.ResolvedAt = &now
	s.items[id] = incident
	return true
}

func (s *IncidentStore) List(limit int) []SystemIncident {
	if limit <= 0 {
		limit = 100
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]SystemIncident, 0, len(s.items))
	for _, incident := range s.items {
		items = append(items, incident)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
