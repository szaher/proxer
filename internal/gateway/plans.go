package gateway

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Plan struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	MaxRoutes       int       `json:"max_routes"`
	MaxConnectors   int       `json:"max_connectors"`
	MaxRPS          float64   `json:"max_rps"`
	MaxMonthlyGB    float64   `json:"max_monthly_gb"`
	TLSEnabled      bool      `json:"tls_enabled"`
	PriceMonthlyUSD float64   `json:"price_monthly_usd"`
	PriceAnnualUSD  float64   `json:"price_annual_usd"`
	PublicOrder     int       `json:"public_order"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type planPricingDefaults struct {
	PriceMonthlyUSD float64
	PriceAnnualUSD  float64
	PublicOrder     int
}

var defaultPlanPricingByID = map[string]planPricingDefaults{
	"free": {
		PriceMonthlyUSD: 0,
		PriceAnnualUSD:  0,
		PublicOrder:     1,
	},
	"pro": {
		PriceMonthlyUSD: 20,
		PriceAnnualUSD:  200,
		PublicOrder:     2,
	},
	"business": {
		PriceMonthlyUSD: 100,
		PriceAnnualUSD:  1000,
		PublicOrder:     3,
	},
}

type TenantPlanAssignment struct {
	TenantID   string    `json:"tenant_id"`
	PlanID     string    `json:"plan_id"`
	AssignedBy string    `json:"assigned_by"`
	AssignedAt time.Time `json:"assigned_at"`
}

type UsageSnapshot struct {
	TenantID        string    `json:"tenant_id"`
	MonthKey        string    `json:"month_key"`
	RoutesUsed      int       `json:"routes_used"`
	ConnectorsUsed  int       `json:"connectors_used"`
	BytesIn         int64     `json:"bytes_in"`
	BytesOut        int64     `json:"bytes_out"`
	Requests        int64     `json:"requests"`
	BlockedRequests int64     `json:"blocked_requests"`
	Warned80        bool      `json:"warned_80"`
	Warned95        bool      `json:"warned_95"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type PlanStore struct {
	mu          sync.RWMutex
	plans       map[string]Plan
	assignments map[string]TenantPlanAssignment
	usage       map[string]UsageSnapshot
}

func NewPlanStore() *PlanStore {
	now := time.Now().UTC()
	plans := map[string]Plan{
		"free": {
			ID:              "free",
			Name:            "Free",
			Description:     "Starter plan",
			MaxRoutes:       5,
			MaxConnectors:   2,
			MaxRPS:          10,
			MaxMonthlyGB:    10,
			TLSEnabled:      false,
			PriceMonthlyUSD: 0,
			PriceAnnualUSD:  0,
			PublicOrder:     1,
			CreatedBy:       "system",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		"pro": {
			ID:              "pro",
			Name:            "Pro",
			Description:     "Professional plan",
			MaxRoutes:       50,
			MaxConnectors:   10,
			MaxRPS:          100,
			MaxMonthlyGB:    500,
			TLSEnabled:      true,
			PriceMonthlyUSD: 20,
			PriceAnnualUSD:  200,
			PublicOrder:     2,
			CreatedBy:       "system",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		"business": {
			ID:              "business",
			Name:            "Business",
			Description:     "Business scale plan",
			MaxRoutes:       250,
			MaxConnectors:   50,
			MaxRPS:          500,
			MaxMonthlyGB:    5000,
			TLSEnabled:      true,
			PriceMonthlyUSD: 100,
			PriceAnnualUSD:  1000,
			PublicOrder:     3,
			CreatedBy:       "system",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
	}
	return &PlanStore{
		plans:       plans,
		assignments: make(map[string]TenantPlanAssignment),
		usage:       make(map[string]UsageSnapshot),
	}
}

func (s *PlanStore) ListPlans() []Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plans := make([]Plan, 0, len(s.plans))
	for _, plan := range s.plans {
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].PublicOrder == plans[j].PublicOrder {
			return plans[i].ID < plans[j].ID
		}
		return plans[i].PublicOrder < plans[j].PublicOrder
	})
	return plans
}

func (s *PlanStore) GetPlan(planID string) (Plan, bool) {
	planID = normalizeIdentifier(planID)
	if planID == "" {
		return Plan{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.plans[planID]
	return plan, ok
}

func (s *PlanStore) UpsertPlan(input Plan) (Plan, error) {
	planID := normalizeIdentifier(input.ID)
	if !identifierPattern.MatchString(planID) {
		return Plan{}, fmt.Errorf("invalid plan id %q", planID)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = strings.ToUpper(planID)
	}
	if input.MaxRoutes <= 0 || input.MaxConnectors <= 0 {
		return Plan{}, fmt.Errorf("max routes/connectors must be > 0")
	}
	if input.MaxRPS <= 0 || input.MaxMonthlyGB <= 0 {
		return Plan{}, fmt.Errorf("max rps/monthly gb must be > 0")
	}
	if input.PriceMonthlyUSD < 0 || input.PriceAnnualUSD < 0 {
		return Plan{}, fmt.Errorf("plan pricing must be >= 0")
	}
	if input.PublicOrder < 0 {
		return Plan{}, fmt.Errorf("public_order must be >= 0")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	existing, ok := s.plans[planID]
	if !ok {
		existing.CreatedAt = now
	}
	existing.ID = planID
	existing.Name = name
	existing.Description = strings.TrimSpace(input.Description)
	existing.MaxRoutes = input.MaxRoutes
	existing.MaxConnectors = input.MaxConnectors
	existing.MaxRPS = input.MaxRPS
	existing.MaxMonthlyGB = input.MaxMonthlyGB
	existing.TLSEnabled = input.TLSEnabled
	existing.PriceMonthlyUSD = input.PriceMonthlyUSD
	existing.PriceAnnualUSD = input.PriceAnnualUSD
	existing.PublicOrder = input.PublicOrder
	existing.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if existing.CreatedBy == "" {
		existing.CreatedBy = "system"
	}
	if defaults, ok := defaultPlanPricingByID[planID]; ok &&
		existing.PriceMonthlyUSD == 0 &&
		existing.PriceAnnualUSD == 0 &&
		existing.PublicOrder == 0 {
		existing.PriceMonthlyUSD = defaults.PriceMonthlyUSD
		existing.PriceAnnualUSD = defaults.PriceAnnualUSD
		existing.PublicOrder = defaults.PublicOrder
	}
	existing.UpdatedAt = now
	s.plans[planID] = existing
	return existing, nil
}

func (s *PlanStore) AssignTenantPlan(tenantID, planID, assignedBy string) (TenantPlanAssignment, error) {
	tenantID = normalizeIdentifier(tenantID)
	planID = normalizeIdentifier(planID)
	if !identifierPattern.MatchString(tenantID) {
		return TenantPlanAssignment{}, fmt.Errorf("invalid tenant id %q", tenantID)
	}
	if !identifierPattern.MatchString(planID) {
		return TenantPlanAssignment{}, fmt.Errorf("invalid plan id %q", planID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.plans[planID]; !ok {
		return TenantPlanAssignment{}, fmt.Errorf("plan %q not found", planID)
	}
	assignment := TenantPlanAssignment{
		TenantID:   tenantID,
		PlanID:     planID,
		AssignedBy: strings.TrimSpace(assignedBy),
		AssignedAt: time.Now().UTC(),
	}
	s.assignments[tenantID] = assignment
	return assignment, nil
}

func (s *PlanStore) GetTenantPlan(tenantID string) (Plan, string) {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	planID := "free"
	if assignment, ok := s.assignments[tenantID]; ok {
		planID = assignment.PlanID
	}
	plan, ok := s.plans[planID]
	if !ok {
		plan = s.plans["free"]
		planID = "free"
	}
	return plan, planID
}

func (s *PlanStore) GetTenantAssignment(tenantID string) (TenantPlanAssignment, bool) {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return TenantPlanAssignment{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	assignment, ok := s.assignments[tenantID]
	return assignment, ok
}

func (s *PlanStore) ListAssignments() []TenantPlanAssignment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	assignments := make([]TenantPlanAssignment, 0, len(s.assignments))
	for _, assignment := range s.assignments {
		assignments = append(assignments, assignment)
	}
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].TenantID < assignments[j].TenantID
	})
	return assignments
}

func (s *PlanStore) RecordRequest(tenantID string, bytesIn, bytesOut int64) UsageSnapshot {
	return s.recordUsage(tenantID, func(usage *UsageSnapshot) {
		usage.Requests++
		usage.BytesIn += bytesIn
		usage.BytesOut += bytesOut
	})
}

func (s *PlanStore) RecordBlockedRequest(tenantID string) UsageSnapshot {
	return s.recordUsage(tenantID, func(usage *UsageSnapshot) {
		usage.BlockedRequests++
	})
}

func (s *PlanStore) UpdateEntityUsage(tenantID string, routesUsed, connectorsUsed int) UsageSnapshot {
	return s.recordUsage(tenantID, func(usage *UsageSnapshot) {
		usage.RoutesUsed = routesUsed
		usage.ConnectorsUsed = connectorsUsed
	})
}

func (s *PlanStore) GetUsage(tenantID, monthKey string) UsageSnapshot {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	monthKey = normalizeMonthKey(monthKey)
	if monthKey == "" {
		monthKey = time.Now().UTC().Format("2006-01")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	usage, ok := s.usage[usageKey(tenantID, monthKey)]
	if !ok {
		return UsageSnapshot{
			TenantID: tenantID,
			MonthKey: monthKey,
		}
	}
	return usage
}

func (s *PlanStore) ListUsageByTenant(tenantID string) []UsageSnapshot {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]UsageSnapshot, 0)
	for _, usage := range s.usage {
		if usage.TenantID == tenantID {
			out = append(out, usage)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MonthKey < out[j].MonthKey })
	return out
}

func (s *PlanStore) MarkWarnings(tenantID string, warned80, warned95 bool) UsageSnapshot {
	return s.recordUsage(tenantID, func(usage *UsageSnapshot) {
		if warned80 {
			usage.Warned80 = true
		}
		if warned95 {
			usage.Warned95 = true
		}
	})
}

func (s *PlanStore) recordUsage(tenantID string, mutate func(*UsageSnapshot)) UsageSnapshot {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	monthKey := time.Now().UTC().Format("2006-01")
	key := usageKey(tenantID, monthKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	usage, ok := s.usage[key]
	if !ok {
		usage = UsageSnapshot{
			TenantID: tenantID,
			MonthKey: monthKey,
		}
	}
	mutate(&usage)
	usage.UpdatedAt = time.Now().UTC()
	s.usage[key] = usage
	return usage
}

func usageKey(tenantID, monthKey string) string {
	return tenantID + ":" + monthKey
}

func normalizeMonthKey(month string) string {
	month = strings.TrimSpace(month)
	if month == "" {
		return ""
	}
	if len(month) != 7 || month[4] != '-' {
		return ""
	}
	return month
}
