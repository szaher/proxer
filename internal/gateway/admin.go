package gateway

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type adminCreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
}

type adminUpdateUserRequest struct {
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Password string `json:"password"`
}

type planUpsertRequest struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	MaxRoutes     int     `json:"max_routes"`
	MaxConnectors int     `json:"max_connectors"`
	MaxRPS        float64 `json:"max_rps"`
	MaxMonthlyGB  float64 `json:"max_monthly_gb"`
	TLSEnabled    bool    `json:"tls_enabled"`
}

type assignTenantPlanRequest struct {
	PlanID string `json:"plan_id"`
}

type patchTLSCertificateRequest struct {
	Active *bool `json:"active"`
}

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"users": s.authStore.ListUsers(),
		})
	case http.MethodPost:
		var request adminCreateUserRequest
		if !s.decodeJSON(w, r, &request, "admin user payload") {
			return
		}
		role := strings.TrimSpace(request.Role)
		if role == "" {
			role = RoleMember
		}
		tenantID := strings.TrimSpace(request.TenantID)
		if role != RoleSuperAdmin {
			if tenantID == "" {
				http.Error(w, "tenant_id is required for non-super-admin users", http.StatusBadRequest)
				return
			}
			if !s.ruleStore.HasTenant(tenantID) {
				http.Error(w, "tenant not found", http.StatusNotFound)
				return
			}
		}
		created, err := s.authStore.RegisterUser(RegisterUserInput{
			Username: request.Username,
			Password: request.Password,
			Role:     role,
			TenantID: tenantID,
			Status:   request.Status,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"message": "user created",
			"user":    created,
		})
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminUserByID(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"))
	if username == "" {
		http.Error(w, "missing user id", http.StatusBadRequest)
		return
	}

	var request adminUpdateUserRequest
	if !s.decodeJSON(w, r, &request, "admin user patch payload") {
		return
	}
	if request.Role != "" && strings.TrimSpace(request.Role) != RoleSuperAdmin {
		tenantID := strings.TrimSpace(request.TenantID)
		if tenantID == "" {
			existing, exists := s.authStore.GetUser(username)
			if exists {
				tenantID = existing.TenantID
			}
		}
		if tenantID == "" {
			http.Error(w, "tenant_id is required for non-super-admin users", http.StatusBadRequest)
			return
		}
		if !s.ruleStore.HasTenant(tenantID) {
			http.Error(w, "tenant not found", http.StatusNotFound)
			return
		}
	}

	updated, err := s.authStore.UpdateUser(UpdateUserInput{
		Username: username,
		Role:     request.Role,
		TenantID: request.TenantID,
		Status:   request.Status,
		Password: request.Password,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "user updated",
		"user":    updated,
	})
	s.persistState()
}

func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
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

	users := s.authStore.ListUsers()
	tenants := s.ruleStore.ListTenants()
	routes := s.ruleStore.ListAll()
	connectors := s.connectorStore.ListAll()

	activeConnectors := 0
	for _, connector := range connectors {
		if _, connected := s.hub.GetConnectorConnection(connector.ID); connected {
			activeConnectors++
		}
	}

	roles := map[string]int{}
	for _, u := range users {
		roles[u.Role]++
	}

	monthlyUsage := make([]UsageSnapshot, 0, len(tenants))
	for _, tenant := range tenants {
		monthlyUsage = append(monthlyUsage, s.planStore.GetUsage(tenant.ID, ""))
	}
	sort.Slice(monthlyUsage, func(i, j int) bool { return monthlyUsage[i].TenantID < monthlyUsage[j].TenantID })

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
		"user_count":        len(users),
		"tenant_count":      len(tenants),
		"route_count":       len(routes),
		"connector_count":   len(connectors),
		"active_connectors": activeConnectors,
		"roles":             roles,
		"monthly_usage":     monthlyUsage,
		"plan_assignments":  s.planStore.ListAssignments(),
		"active_tls_certs":  s.tlsStore.ActiveCertificateCount(),
		"storage_driver":    s.cfg.StorageDriver,
		"uptime_seconds":    int(time.Since(s.startedAt).Seconds()),
	})
}

func (s *Server) handleAdminIncidents(w http.ResponseWriter, r *http.Request) {
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

	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"incidents": s.incidentStore.List(limit),
	})
}

func (s *Server) handleAdminSystemStatus(w http.ResponseWriter, r *http.Request) {
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

	hubStatus := s.hub.Status()
	storage := s.storageHealth()
	if _, ok := storage["sqlite_path"]; !ok && strings.TrimSpace(s.cfg.SQLitePath) != "" {
		storage["sqlite_path"] = s.cfg.SQLitePath
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gateway": map[string]any{
			"status":          "ok",
			"listen_addr":     s.cfg.ListenAddr,
			"public_base_url": s.cfg.PublicBaseURL,
			"uptime_seconds":  int(time.Since(s.startedAt).Seconds()),
		},
		"storage": storage,
		"runtime": hubStatus,
		"tls": map[string]any{
			"tls_listen_addr":     s.cfg.TLSListenAddr,
			"active_certificates": s.tlsStore.ActiveCertificateCount(),
		},
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleAdminPlans(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"plans": s.planStore.ListPlans(),
		})
	case http.MethodPost:
		var request planUpsertRequest
		if !s.decodeJSON(w, r, &request, "plan payload") {
			return
		}
		plan, err := s.planStore.UpsertPlan(Plan{
			ID:            request.ID,
			Name:          request.Name,
			Description:   request.Description,
			MaxRoutes:     request.MaxRoutes,
			MaxConnectors: request.MaxConnectors,
			MaxRPS:        request.MaxRPS,
			MaxMonthlyGB:  request.MaxMonthlyGB,
			TLSEnabled:    request.TLSEnabled,
			CreatedBy:     user.Username,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"message": "plan upserted",
			"plan":    plan,
		})
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminPlanByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
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

	planID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/plans/"))
	if planID == "" {
		http.Error(w, "missing plan id", http.StatusBadRequest)
		return
	}

	var request planUpsertRequest
	if !s.decodeJSON(w, r, &request, "plan patch payload") {
		return
	}
	request.ID = planID
	plan, err := s.planStore.UpsertPlan(Plan{
		ID:            request.ID,
		Name:          request.Name,
		Description:   request.Description,
		MaxRoutes:     request.MaxRoutes,
		MaxConnectors: request.MaxConnectors,
		MaxRPS:        request.MaxRPS,
		MaxMonthlyGB:  request.MaxMonthlyGB,
		TLSEnabled:    request.TLSEnabled,
		CreatedBy:     user.Username,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": "plan updated",
		"plan":    plan,
	})
	s.persistState()
}

func (s *Server) handleAdminTenantsSubresource(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/tenants/"))
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[1]) != "assign-plan" {
		http.Error(w, "invalid admin tenant path", http.StatusBadRequest)
		return
	}
	tenantID := strings.TrimSpace(parts[0])
	if tenantID == "" {
		http.Error(w, "missing tenant id", http.StatusBadRequest)
		return
	}
	if !s.ruleStore.HasTenant(tenantID) {
		http.Error(w, "tenant not found", http.StatusNotFound)
		return
	}

	var request assignTenantPlanRequest
	if !s.decodeJSON(w, r, &request, "assign plan payload") {
		return
	}
	assignment, err := s.planStore.AssignTenantPlan(tenantID, request.PlanID, user.Username)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.refreshTenantUsage(tenantID)
	writeJSON(w, http.StatusOK, map[string]any{
		"message":    "plan assigned",
		"assignment": assignment,
	})
	s.persistState()
}

func (s *Server) handleAdminTLSCertificates(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"certificates": s.tlsStore.List(),
		})
	case http.MethodPost:
		var request TLSCertificateInput
		if !s.decodeJSON(w, r, &request, "tls certificate payload") {
			return
		}
		cert, err := s.tlsStore.Upsert(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"message":     "certificate upserted",
			"certificate": cert,
		})
		s.persistState()
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminTLSCertificateByID(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	if !s.requireSuperAdmin(w, user) {
		return
	}

	id := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/admin/tls/certificates/"))
	if id == "" {
		http.Error(w, "missing certificate id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPatch:
		var request patchTLSCertificateRequest
		if !s.decodeJSON(w, r, &request, "tls certificate patch payload") {
			return
		}
		if request.Active == nil {
			http.Error(w, "active is required", http.StatusBadRequest)
			return
		}
		cert, err := s.tlsStore.SetActive(id, *request.Active)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message":     "certificate updated",
			"certificate": cert,
		})
		s.persistState()
	case http.MethodDelete:
		if ok := s.tlsStore.Delete(id); !ok {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		s.persistState()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) maybeRecordProxyIncident(err error, tunnelKey string) {
	if err == nil {
		return
	}
	source := "proxy"
	severity := "warning"
	message := fmt.Sprintf("%s: %v", tunnelKey, err)
	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		severity = "critical"
	}
	s.incidentStore.Add(severity, source, message)
}
