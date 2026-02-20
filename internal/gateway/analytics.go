package gateway

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type funnelEventInput struct {
	Event    string `json:"event"`
	PagePath string `json:"page_path,omitempty"`
	PlanID   string `json:"plan_id,omitempty"`
	Billing  string `json:"billing,omitempty"`
	Platform string `json:"platform,omitempty"`
	FileName string `json:"file_name,omitempty"`
	Outcome  string `json:"outcome,omitempty"`
	Source   string `json:"source,omitempty"`
}

type funnelEvent struct {
	Event     string    `json:"event"`
	PagePath  string    `json:"page_path,omitempty"`
	PlanID    string    `json:"plan_id,omitempty"`
	Billing   string    `json:"billing,omitempty"`
	Platform  string    `json:"platform,omitempty"`
	FileName  string    `json:"file_name,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	Source    string    `json:"source,omitempty"`
	ClientIP  string    `json:"client_ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type FunnelAnalyticsStore struct {
	mu        sync.RWMutex
	totals    map[string]int
	byDay     map[string]map[string]int
	recent    []funnelEvent
	maxRecent int
}

func NewFunnelAnalyticsStore() *FunnelAnalyticsStore {
	return &FunnelAnalyticsStore{
		totals:    make(map[string]int),
		byDay:     make(map[string]map[string]int),
		recent:    make([]funnelEvent, 0, 64),
		maxRecent: 250,
	}
}

func (s *FunnelAnalyticsStore) Record(input funnelEventInput, clientIP string) (funnelEvent, bool) {
	eventName := normalizeFunnelEventName(input.Event)
	if _, ok := allowedFunnelEvents[eventName]; !ok {
		return funnelEvent{}, false
	}

	event := funnelEvent{
		Event:     eventName,
		PagePath:  capEventField(strings.TrimSpace(input.PagePath), 96),
		PlanID:    capEventField(strings.TrimSpace(input.PlanID), 64),
		Billing:   capEventField(strings.TrimSpace(strings.ToLower(input.Billing)), 16),
		Platform:  capEventField(strings.TrimSpace(strings.ToLower(input.Platform)), 32),
		FileName:  capEventField(strings.TrimSpace(input.FileName), 128),
		Outcome:   capEventField(strings.TrimSpace(strings.ToLower(input.Outcome)), 32),
		Source:    capEventField(strings.TrimSpace(strings.ToLower(input.Source)), 32),
		ClientIP:  capEventField(strings.TrimSpace(clientIP), 64),
		CreatedAt: time.Now().UTC(),
	}

	if event.Source == "" {
		event.Source = "web"
	}

	dayKey := event.CreatedAt.Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()
	s.totals[event.Event]++
	if _, ok := s.byDay[dayKey]; !ok {
		s.byDay[dayKey] = make(map[string]int)
	}
	s.byDay[dayKey][event.Event]++
	s.recent = append([]funnelEvent{event}, s.recent...)
	if len(s.recent) > s.maxRecent {
		s.recent = s.recent[:s.maxRecent]
	}
	return event, true
}

func (s *FunnelAnalyticsStore) Summary() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totals := make(map[string]int, len(s.totals))
	for key, value := range s.totals {
		totals[key] = value
	}

	dayKeys := make([]string, 0, len(s.byDay))
	for key := range s.byDay {
		dayKeys = append(dayKeys, key)
	}
	sort.Strings(dayKeys)

	byDay := make([]map[string]any, 0, len(dayKeys))
	for _, day := range dayKeys {
		events := make(map[string]int, len(s.byDay[day]))
		for eventKey, value := range s.byDay[day] {
			events[eventKey] = value
		}
		byDay = append(byDay, map[string]any{
			"date":   day,
			"events": events,
		})
	}

	recent := make([]funnelEvent, len(s.recent))
	copy(recent, s.recent)

	return map[string]any{
		"totals":       totals,
		"by_day":       byDay,
		"recent":       recent,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}
}

func normalizeFunnelEventName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func capEventField(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(value) <= max {
		return value
	}
	return value[:max]
}

var allowedFunnelEvents = map[string]struct{}{
	"landing_view":     {},
	"pricing_toggle":   {},
	"plan_cta_click":   {},
	"signup_cta_click": {},
	"download_click":   {},
	"signup_submit":    {},
	"signup_success":   {},
	"signup_failure":   {},
}

func (s *Server) handlePublicAnalyticsEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request funnelEventInput
	if !s.decodeJSON(w, r, &request, "public analytics event payload") {
		return
	}
	if s.funnelAnalytics == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"message": "analytics store unavailable",
		})
		return
	}
	if _, ok := s.funnelAnalytics.Record(request, signupClientIP(r)); !ok {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"message": "event accepted",
	})
}

func (s *Server) handleAdminFunnelAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}
	if s.funnelAnalytics == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"totals":       map[string]int{},
			"by_day":       []any{},
			"recent":       []any{},
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	writeJSON(w, http.StatusOK, s.funnelAnalytics.Summary())
}
