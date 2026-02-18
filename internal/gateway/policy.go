package gateway

import (
	"fmt"
	"math"
	"strings"
)

const bytesPerGB = 1024 * 1024 * 1024

func computeRouteRateLimit(plan Plan) float64 {
	if plan.MaxRPS <= 0 {
		return 1
	}
	routes := plan.MaxRoutes
	if routes <= 0 {
		routes = 1
	}
	share := plan.MaxRPS / float64(routes)
	if share < 1 {
		share = 1
	}
	return share
}

func (s *Server) enforceRouteLimit(tenantID, routeID string) error {
	tenantID = normalizeIdentifier(tenantID)
	routeID = normalizeIdentifier(routeID)
	if tenantID == "" || routeID == "" {
		return fmt.Errorf("missing tenant or route id")
	}
	if _, exists := s.ruleStore.GetForTenant(tenantID, routeID); exists {
		return nil
	}
	plan, planID := s.planStore.GetTenantPlan(tenantID)
	routeCounts := s.ruleStore.RouteCountByTenant()
	current := routeCounts[tenantID]
	if plan.MaxRoutes > 0 && current >= plan.MaxRoutes {
		return fmt.Errorf("plan %q route limit reached: %d/%d", planID, current, plan.MaxRoutes)
	}
	return nil
}

func (s *Server) enforceConnectorLimit(tenantID string) error {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return fmt.Errorf("missing tenant id")
	}
	plan, planID := s.planStore.GetTenantPlan(tenantID)
	current := s.connectorStore.CountByTenant(tenantID)
	if plan.MaxConnectors > 0 && current >= plan.MaxConnectors {
		return fmt.Errorf("plan %q connector limit reached: %d/%d", planID, current, plan.MaxConnectors)
	}
	return nil
}

func (s *Server) refreshTenantUsage(tenantID string) {
	tenantID = normalizeIdentifier(tenantID)
	if tenantID == "" {
		return
	}
	routeCounts := s.ruleStore.RouteCountByTenant()
	routes := routeCounts[tenantID]
	connectors := s.connectorStore.CountByTenant(tenantID)
	s.planStore.UpdateEntityUsage(tenantID, routes, connectors)
}

func (s *Server) refreshUsageAllTenants() {
	tenantSet := make(map[string]struct{})
	for _, tenant := range s.ruleStore.ListTenants() {
		tenantSet[tenant.ID] = struct{}{}
	}
	for _, connector := range s.connectorStore.ListAll() {
		tenantSet[connector.TenantID] = struct{}{}
	}
	for tenantID := range tenantSet {
		s.refreshTenantUsage(tenantID)
	}
}

func (s *Server) recordTrafficUsage(tenantID string, plan Plan, bytesIn, bytesOut int64) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	before := s.planStore.GetUsage(tenantID, "")
	after := s.planStore.RecordRequest(tenantID, bytesIn, bytesOut)

	capBytes := int64(plan.MaxMonthlyGB * bytesPerGB)
	if capBytes <= 0 {
		return
	}
	beforeRatio := float64(before.BytesIn+before.BytesOut) / float64(capBytes)
	afterRatio := float64(after.BytesIn+after.BytesOut) / float64(capBytes)

	if afterRatio >= 0.80 && !before.Warned80 {
		s.planStore.MarkWarnings(tenantID, true, false)
		s.incidentStore.Add("warning", "traffic", fmt.Sprintf("tenant %s reached %.1f%% monthly traffic", tenantID, math.Min(afterRatio*100, 100)))
	}
	if afterRatio >= 0.95 && !before.Warned95 {
		s.planStore.MarkWarnings(tenantID, true, true)
		s.incidentStore.Add("critical", "traffic", fmt.Sprintf("tenant %s reached %.1f%% monthly traffic", tenantID, math.Min(afterRatio*100, 100)))
	}

	_ = beforeRatio
}

func usagePercent(plan Plan, usage UsageSnapshot) float64 {
	capBytes := plan.MaxMonthlyGB * bytesPerGB
	if capBytes <= 0 {
		return 0
	}
	used := float64(usage.BytesIn + usage.BytesOut)
	return used / capBytes
}
