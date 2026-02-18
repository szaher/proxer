package gateway

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

type usageGauge struct {
	Used    float64 `json:"used"`
	Limit   float64 `json:"limit"`
	Percent float64 `json:"percent"`
}

func (s *Server) handleMeDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	if s.isSuperAdmin(user) {
		tenants := s.ruleStore.ListTenants()
		routes := s.ruleStore.ListAll()
		connectors := s.connectorStore.ListAll()
		onlineConnectors := 0
		for _, connector := range connectors {
			if _, connected := s.hub.GetConnectorConnection(connector.ID); connected {
				onlineConnectors++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"role":              user.Role,
			"tenant_count":      len(tenants),
			"route_count":       len(routes),
			"connector_count":   len(connectors),
			"online_connectors": onlineConnectors,
			"system":            s.hub.Status(),
			"generated_at":      time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	tenantID := strings.TrimSpace(user.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	routes := s.buildRouteViews(tenantID)
	connectors := s.connectorStore.ListForTenants([]string{tenantID})
	onlineConnectors := 0
	connectorViews := make([]connectorView, 0, len(connectors))
	for _, connector := range connectors {
		view := s.buildConnectorView(connector)
		if view.Connected {
			onlineConnectors++
		}
		connectorViews = append(connectorViews, view)
	}
	plan, planID := s.planStore.GetTenantPlan(tenantID)
	usage := s.planStore.GetUsage(tenantID, "")
	trafficUsedGB := float64(usage.BytesIn+usage.BytesOut) / bytesPerGB
	trafficPercent := usagePercent(plan, usage)

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"plan": map[string]any{
			"id":   planID,
			"name": plan.Name,
		},
		"gauges": map[string]any{
			"routes": usageGauge{
				Used:    float64(len(routes)),
				Limit:   float64(plan.MaxRoutes),
				Percent: boundedPercent(float64(len(routes)), float64(plan.MaxRoutes)),
			},
			"connectors": usageGauge{
				Used:    float64(len(connectors)),
				Limit:   float64(plan.MaxConnectors),
				Percent: boundedPercent(float64(len(connectors)), float64(plan.MaxConnectors)),
			},
			"traffic": usageGauge{
				Used:    trafficUsedGB,
				Limit:   plan.MaxMonthlyGB,
				Percent: boundedPercent(trafficPercent, 1),
			},
		},
		"status": map[string]any{
			"routes_active":          countActiveRoutes(routes),
			"routes_degraded":        len(routes) - countActiveRoutes(routes),
			"connectors_online":      onlineConnectors,
			"connectors_offline":     len(connectors) - onlineConnectors,
			"blocked_requests_month": usage.BlockedRequests,
		},
		"usage":        usage,
		"routes":       routes,
		"connectors":   connectorViews,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleMeRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	if s.isSuperAdmin(user) {
		routes := make([]routeView, 0)
		for _, tenant := range s.ruleStore.ListTenants() {
			routes = append(routes, s.buildRouteViews(tenant.ID)...)
		}
		sort.Slice(routes, func(i, j int) bool {
			if routes[i].TenantID == routes[j].TenantID {
				return routes[i].RouteID < routes[j].RouteID
			}
			return routes[i].TenantID < routes[j].TenantID
		})
		writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
		return
	}

	tenantID := strings.TrimSpace(user.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"routes":    s.buildRouteViews(tenantID),
	})
}

func (s *Server) handleMeConnectors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"connectors": s.buildConnectorViewsForUser(user),
	})
}

func (s *Server) handleMeUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}

	if s.isSuperAdmin(user) {
		tenants := s.ruleStore.ListTenants()
		items := make([]map[string]any, 0, len(tenants))
		for _, tenant := range tenants {
			plan, planID := s.planStore.GetTenantPlan(tenant.ID)
			usage := s.planStore.GetUsage(tenant.ID, "")
			items = append(items, map[string]any{
				"tenant_id": tenant.ID,
				"plan_id":   planID,
				"plan":      plan,
				"usage":     usage,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": items})
		return
	}

	tenantID := strings.TrimSpace(user.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	plan, planID := s.planStore.GetTenantPlan(tenantID)
	usage := s.planStore.GetUsage(tenantID, "")
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"plan_id":   planID,
		"plan":      plan,
		"usage":     usage,
	})
}

func boundedPercent(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	percent := used / limit
	if percent < 0 {
		return 0
	}
	if percent > 1 {
		return 1
	}
	return percent
}

func countActiveRoutes(routes []routeView) int {
	count := 0
	for _, route := range routes {
		if route.Connected {
			count++
		}
	}
	return count
}
