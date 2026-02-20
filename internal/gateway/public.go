package gateway

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type publicSignupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type publicPlanView struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	MaxRoutes       int     `json:"max_routes"`
	MaxConnectors   int     `json:"max_connectors"`
	MaxRPS          float64 `json:"max_rps"`
	MaxMonthlyGB    float64 `json:"max_monthly_gb"`
	TLSEnabled      bool    `json:"tls_enabled"`
	PriceMonthlyUSD float64 `json:"price_monthly_usd"`
	PriceAnnualUSD  float64 `json:"price_annual_usd"`
	PublicOrder     int     `json:"public_order"`
}

func (s *Server) handlePublicPlans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	plans := s.planStore.ListPlans()
	views := make([]publicPlanView, 0, len(plans))
	for _, plan := range plans {
		views = append(views, publicPlanView{
			ID:              plan.ID,
			Name:            plan.Name,
			Description:     plan.Description,
			MaxRoutes:       plan.MaxRoutes,
			MaxConnectors:   plan.MaxConnectors,
			MaxRPS:          plan.MaxRPS,
			MaxMonthlyGB:    plan.MaxMonthlyGB,
			TLSEnabled:      plan.TLSEnabled,
			PriceMonthlyUSD: plan.PriceMonthlyUSD,
			PriceAnnualUSD:  plan.PriceAnnualUSD,
			PublicOrder:     plan.PublicOrder,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plans": views,
	})
}

func (s *Server) handlePublicDownloads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.downloads == nil {
		writeJSON(w, http.StatusOK, unavailableDownloadsResponse("", "download provider is not configured"))
		return
	}
	writeJSON(w, http.StatusOK, s.downloads.Resolve(r.Context()))
}

func (s *Server) handlePublicSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.cfg.PublicSignupEnabled {
		http.Error(w, "public signup is disabled", http.StatusForbidden)
		return
	}

	clientIP := signupClientIP(r)
	if !s.allowSignupForIP(clientIP) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"message":    "signup rate limit exceeded",
			"retry_hint": "try again shortly",
		})
		return
	}

	var request publicSignupRequest
	if !s.decodeJSON(w, r, &request, "public signup payload") {
		return
	}

	username := normalizeUsername(request.Username)
	if username == "" {
		http.Error(w, "username is required", http.StatusBadRequest)
		return
	}
	if _, exists := s.authStore.GetUser(username); exists {
		http.Error(w, "username already exists", http.StatusConflict)
		return
	}

	tenantID := s.generateTenantSlugFromUsername(username)
	tenantName := fmt.Sprintf("%s workspace", username)
	tenantExisted := s.ruleStore.HasTenant(tenantID)
	createdTenant, err := s.ruleStore.UpsertTenant(Tenant{ID: tenantID, Name: tenantName})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := s.authStore.RegisterUser(RegisterUserInput{
		Username: username,
		Password: request.Password,
		TenantID: tenantID,
		Role:     RoleTenantAdmin,
		Status:   "active",
	})
	if err != nil {
		if !tenantExisted {
			s.ruleStore.DeleteTenant(tenantID)
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	assignment, err := s.planStore.AssignTenantPlan(tenantID, "free", "public-signup")
	if err != nil {
		http.Error(w, fmt.Sprintf("assign free plan: %v", err), http.StatusInternalServerError)
		return
	}
	s.refreshTenantUsage(tenantID)

	sessionID, err := s.authStore.NewSession(user.Username)
	if err != nil {
		http.Error(w, fmt.Sprintf("create session: %v", err), http.StatusInternalServerError)
		return
	}
	s.setSessionCookie(w, sessionID)

	writeJSON(w, http.StatusCreated, map[string]any{
		"message":    "signup successful",
		"user":       user,
		"tenant":     createdTenant,
		"assignment": assignment,
		"redirect":   "/app",
	})
	s.persistState()
}

func (s *Server) allowSignupForIP(clientIP string) bool {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	ratePerSecond := float64(s.cfg.PublicSignupRPM) / 60.0
	return s.rateLimiter.Allow("public-signup:"+clientIP, ratePerSecond)
}

func signupClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	return extractIP(r.RemoteAddr)
}

func (s *Server) generateTenantSlugFromUsername(username string) string {
	base := slugifyTenantID(username)
	const maxLen = 64
	candidate := base
	for suffix := 2; s.ruleStore.HasTenant(candidate); suffix++ {
		suffixPart := "-" + strconv.Itoa(suffix)
		trimmedBase := base
		maxBaseLen := maxLen - len(suffixPart)
		if maxBaseLen < 1 {
			maxBaseLen = 1
		}
		if len(trimmedBase) > maxBaseLen {
			trimmedBase = strings.Trim(trimmedBase[:maxBaseLen], "-_")
		}
		if trimmedBase == "" {
			trimmedBase = "tenant"
		}
		candidate = trimmedBase + suffixPart
	}
	return candidate
}

func slugifyTenantID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "tenant"
	}

	var builder strings.Builder
	builder.Grow(len(value))
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		default:
			if builder.Len() > 0 && !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		slug = "tenant"
	}
	if first := slug[0]; !(first >= 'a' && first <= 'z') && !(first >= '0' && first <= '9') {
		slug = "tenant-" + slug
	}
	if len(slug) > 64 {
		slug = strings.Trim(slug[:64], "-_")
	}
	if slug == "" {
		slug = "tenant"
	}
	return slug
}
