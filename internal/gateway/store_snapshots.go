package gateway

import (
	"sort"
	"strings"
	"time"
)

type authUserSnapshot struct {
	User         User   `json:"user"`
	PasswordHash string `json:"password_hash"`
}

type ruleStoreSnapshot struct {
	Tenants      []Tenant            `json:"tenants"`
	Environments []TenantEnvironment `json:"environments"`
	Rules        []Rule              `json:"rules"`
}

type connectorCredentialSnapshot struct {
	ConnectorID string    `json:"connector_id"`
	SecretHash  string    `json:"secret_hash"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type connectorStoreSnapshot struct {
	Connectors  []Connector                   `json:"connectors"`
	Credentials []connectorCredentialSnapshot `json:"credentials"`
}

type planStoreSnapshot struct {
	Plans       []Plan                 `json:"plans"`
	Assignments []TenantPlanAssignment `json:"assignments"`
	Usage       []UsageSnapshot        `json:"usage"`
}

type incidentStoreSnapshot struct {
	Items   []SystemIncident `json:"items"`
	Counter uint64           `json:"counter"`
}

type tlsCertificateRecordSnapshot struct {
	Meta    TLSCertificate `json:"meta"`
	CertPEM string         `json:"cert_pem"`
	KeyEnc  string         `json:"key_enc"`
}

func (s *AuthStore) SnapshotUsers() []authUserSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]authUserSnapshot, 0, len(s.users))
	for _, record := range s.users {
		users = append(users, authUserSnapshot{
			User:         record.user,
			PasswordHash: record.passwordHash,
		})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].User.Username < users[j].User.Username })
	return users
}

func (s *AuthStore) RestoreUsers(users []authUserSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]authUserRecord, len(users))
	s.sessions = make(map[string]authSession)

	for _, snapshot := range users {
		username := normalizeUsername(snapshot.User.Username)
		if username == "" || strings.TrimSpace(snapshot.PasswordHash) == "" {
			continue
		}
		user := snapshot.User
		user.Username = username
		role := strings.TrimSpace(user.Role)
		if role == "" {
			role = RoleMember
		}
		if role == "admin" {
			if strings.TrimSpace(user.TenantID) != "" {
				role = RoleTenantAdmin
			} else {
				role = RoleSuperAdmin
			}
		}
		if role != RoleSuperAdmin && role != RoleTenantAdmin && role != RoleMember {
			role = RoleMember
		}
		user.Role = role
		if role == RoleSuperAdmin {
			user.TenantID = ""
		} else {
			user.TenantID = normalizeIdentifier(user.TenantID)
			if user.TenantID == "" {
				user.TenantID = DefaultTenantID
			}
		}
		status := strings.ToLower(strings.TrimSpace(user.Status))
		if status == "" {
			status = "active"
		}
		if status != "active" && status != "disabled" {
			status = "active"
		}
		user.Status = status
		now := time.Now().UTC()
		if user.CreatedAt.IsZero() {
			user.CreatedAt = now
		}
		if user.UpdatedAt.IsZero() {
			user.UpdatedAt = user.CreatedAt
		}
		s.users[username] = authUserRecord{
			user:         user,
			passwordHash: snapshot.PasswordHash,
		}
	}
}

func (s *AuthStore) EnsureSuperAdmin(username, password string) error {
	username = normalizeUsername(username)
	if username == "" {
		username = "admin"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	record, exists := s.users[username]
	if !exists {
		if strings.TrimSpace(password) == "" {
			password = "admin123"
		}
		user, err := s.registerUserLocked(RegisterUserInput{
			Username: username,
			Password: password,
			Role:     RoleSuperAdmin,
			Status:   "active",
		})
		if err != nil {
			return err
		}
		record.user = user
		s.users[username] = record
		return nil
	}

	record.user.Role = RoleSuperAdmin
	record.user.TenantID = ""
	record.user.Status = "active"
	record.user.UpdatedAt = now
	if strings.TrimSpace(password) != "" {
		record.passwordHash = hashPassword(password)
	}
	s.users[username] = record
	return nil
}

func (s *RuleStore) Snapshot() ruleStoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		tenants = append(tenants, tenant)
	}
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].ID < tenants[j].ID })

	envs := make([]TenantEnvironment, 0, len(s.envs))
	for _, env := range s.envs {
		copied := env
		copied.Variables = copyStringMap(env.Variables)
		envs = append(envs, copied)
	}
	sort.Slice(envs, func(i, j int) bool { return envs[i].TenantID < envs[j].TenantID })

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

	return ruleStoreSnapshot{
		Tenants:      tenants,
		Environments: envs,
		Rules:        rules,
	}
}

func (s *RuleStore) Restore(snapshot ruleStoreSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tenants = make(map[string]Tenant)
	s.envs = make(map[string]TenantEnvironment)
	s.rules = make(map[string]Rule)

	for _, tenant := range snapshot.Tenants {
		tenantID := normalizeIdentifier(tenant.ID)
		if !identifierPattern.MatchString(tenantID) {
			continue
		}
		tenant.ID = tenantID
		if strings.TrimSpace(tenant.Name) == "" {
			tenant.Name = tenantID
		}
		now := time.Now().UTC()
		if tenant.CreatedAt.IsZero() {
			tenant.CreatedAt = now
		}
		if tenant.UpdatedAt.IsZero() {
			tenant.UpdatedAt = tenant.CreatedAt
		}
		s.tenants[tenantID] = tenant
	}

	for _, env := range snapshot.Environments {
		tenantID := normalizeIdentifier(env.TenantID)
		if tenantID == "" {
			continue
		}
		if _, ok := s.tenants[tenantID]; !ok {
			continue
		}
		env.TenantID = tenantID
		if env.Scheme != "https" {
			env.Scheme = "http"
		}
		if strings.TrimSpace(env.Host) == "" {
			env.Host = "host.docker.internal"
		}
		if env.DefaultPort < 1 || env.DefaultPort > 65535 {
			env.DefaultPort = 3000
		}
		env.Variables = copyStringMap(env.Variables)
		if env.UpdatedAt.IsZero() {
			env.UpdatedAt = time.Now().UTC()
		}
		s.envs[tenantID] = env
	}

	for tenantID := range s.tenants {
		if _, ok := s.envs[tenantID]; ok {
			continue
		}
		s.envs[tenantID] = TenantEnvironment{
			TenantID:    tenantID,
			Scheme:      "http",
			Host:        "host.docker.internal",
			DefaultPort: 3000,
			Variables:   map[string]string{},
			UpdatedAt:   time.Now().UTC(),
		}
	}

	for _, rule := range snapshot.Rules {
		tenantID := normalizeIdentifier(rule.TenantID)
		routeID := normalizeIdentifier(rule.ID)
		if !identifierPattern.MatchString(tenantID) || !identifierPattern.MatchString(routeID) {
			continue
		}
		if _, ok := s.tenants[tenantID]; !ok {
			continue
		}
		rule.TenantID = tenantID
		rule.ID = routeID
		rule.ConnectorID = normalizeIdentifier(rule.ConnectorID)
		rule.LocalScheme = strings.ToLower(strings.TrimSpace(rule.LocalScheme))
		if rule.LocalScheme != "https" {
			rule.LocalScheme = "http"
		}
		if strings.TrimSpace(rule.LocalHost) == "" && rule.ConnectorID != "" {
			rule.LocalHost = "127.0.0.1"
		}
		if rule.CreatedAt.IsZero() {
			rule.CreatedAt = time.Now().UTC()
		}
		if rule.UpdatedAt.IsZero() {
			rule.UpdatedAt = rule.CreatedAt
		}
		s.rules[ruleKey(tenantID, routeID)] = rule
	}

	if len(s.tenants) == 0 {
		now := time.Now().UTC()
		s.tenants[DefaultTenantID] = Tenant{
			ID:        DefaultTenantID,
			Name:      "Default",
			CreatedAt: now,
			UpdatedAt: now,
		}
		s.envs[DefaultTenantID] = TenantEnvironment{
			TenantID:    DefaultTenantID,
			Scheme:      "http",
			Host:        "host.docker.internal",
			DefaultPort: 3000,
			Variables:   map[string]string{},
			UpdatedAt:   now,
		}
	}
}

func (s *ConnectorStore) Snapshot() connectorStoreSnapshot {
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

	credentials := make([]connectorCredentialSnapshot, 0, len(s.credentials))
	for _, credential := range s.credentials {
		credentials = append(credentials, connectorCredentialSnapshot{
			ConnectorID: credential.ConnectorID,
			SecretHash:  credential.SecretHash,
			UpdatedAt:   credential.UpdatedAt,
		})
	}
	sort.Slice(credentials, func(i, j int) bool { return credentials[i].ConnectorID < credentials[j].ConnectorID })

	return connectorStoreSnapshot{Connectors: connectors, Credentials: credentials}
}

func (s *ConnectorStore) Restore(snapshot connectorStoreSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connectors = make(map[string]Connector)
	s.credentials = make(map[string]connectorCredential)
	s.pairTokens = make(map[string]pairTokenRecord)

	for _, connector := range snapshot.Connectors {
		connectorID := normalizeIdentifier(connector.ID)
		tenantID := normalizeIdentifier(connector.TenantID)
		if !identifierPattern.MatchString(connectorID) || !identifierPattern.MatchString(tenantID) {
			continue
		}
		connector.ID = connectorID
		connector.TenantID = tenantID
		if strings.TrimSpace(connector.Name) == "" {
			connector.Name = connectorID
		}
		now := time.Now().UTC()
		if connector.CreatedAt.IsZero() {
			connector.CreatedAt = now
		}
		if connector.UpdatedAt.IsZero() {
			connector.UpdatedAt = connector.CreatedAt
		}
		s.connectors[connectorID] = connector
	}

	for _, credential := range snapshot.Credentials {
		connectorID := normalizeIdentifier(credential.ConnectorID)
		if connectorID == "" {
			continue
		}
		if _, ok := s.connectors[connectorID]; !ok {
			continue
		}
		if strings.TrimSpace(credential.SecretHash) == "" {
			continue
		}
		s.credentials[connectorID] = connectorCredential{
			ConnectorID: connectorID,
			SecretHash:  strings.TrimSpace(credential.SecretHash),
			UpdatedAt:   credential.UpdatedAt,
		}
	}
}

func (s *PlanStore) Snapshot() planStoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plans := make([]Plan, 0, len(s.plans))
	for _, plan := range s.plans {
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })

	assignments := make([]TenantPlanAssignment, 0, len(s.assignments))
	for _, assignment := range s.assignments {
		assignments = append(assignments, assignment)
	}
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].TenantID < assignments[j].TenantID })

	usage := make([]UsageSnapshot, 0, len(s.usage))
	for _, value := range s.usage {
		usage = append(usage, value)
	}
	sort.Slice(usage, func(i, j int) bool {
		if usage[i].TenantID == usage[j].TenantID {
			return usage[i].MonthKey < usage[j].MonthKey
		}
		return usage[i].TenantID < usage[j].TenantID
	})

	return planStoreSnapshot{
		Plans:       plans,
		Assignments: assignments,
		Usage:       usage,
	}
}

func (s *PlanStore) Restore(snapshot planStoreSnapshot) {
	defaults := NewPlanStore()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.plans = make(map[string]Plan, len(defaults.plans))
	for id, plan := range defaults.plans {
		s.plans[id] = plan
	}
	s.assignments = make(map[string]TenantPlanAssignment)
	s.usage = make(map[string]UsageSnapshot)

	for _, plan := range snapshot.Plans {
		planID := normalizeIdentifier(plan.ID)
		if !identifierPattern.MatchString(planID) {
			continue
		}
		if plan.MaxRoutes <= 0 || plan.MaxConnectors <= 0 || plan.MaxRPS <= 0 || plan.MaxMonthlyGB <= 0 {
			continue
		}
		plan.ID = planID
		if defaults, ok := defaultPlanPricingByID[planID]; ok &&
			plan.PriceMonthlyUSD == 0 &&
			plan.PriceAnnualUSD == 0 &&
			plan.PublicOrder == 0 {
			plan.PriceMonthlyUSD = defaults.PriceMonthlyUSD
			plan.PriceAnnualUSD = defaults.PriceAnnualUSD
			plan.PublicOrder = defaults.PublicOrder
		}
		now := time.Now().UTC()
		if plan.CreatedAt.IsZero() {
			plan.CreatedAt = now
		}
		if plan.UpdatedAt.IsZero() {
			plan.UpdatedAt = plan.CreatedAt
		}
		s.plans[planID] = plan
	}

	for _, assignment := range snapshot.Assignments {
		tenantID := normalizeIdentifier(assignment.TenantID)
		planID := normalizeIdentifier(assignment.PlanID)
		if !identifierPattern.MatchString(tenantID) || !identifierPattern.MatchString(planID) {
			continue
		}
		if _, ok := s.plans[planID]; !ok {
			continue
		}
		assignment.TenantID = tenantID
		assignment.PlanID = planID
		if assignment.AssignedAt.IsZero() {
			assignment.AssignedAt = time.Now().UTC()
		}
		s.assignments[tenantID] = assignment
	}

	for _, item := range snapshot.Usage {
		tenantID := normalizeIdentifier(item.TenantID)
		monthKey := normalizeMonthKey(item.MonthKey)
		if tenantID == "" || monthKey == "" {
			continue
		}
		item.TenantID = tenantID
		item.MonthKey = monthKey
		s.usage[usageKey(tenantID, monthKey)] = item
	}
}

func (s *IncidentStore) Snapshot() incidentStoreSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]SystemIncident, 0, len(s.items))
	for _, incident := range s.items {
		items = append(items, incident)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return incidentStoreSnapshot{Items: items, Counter: s.counter}
}

func (s *IncidentStore) Restore(snapshot incidentStoreSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = make(map[string]SystemIncident, len(snapshot.Items))
	for _, incident := range snapshot.Items {
		incidentID := strings.TrimSpace(incident.ID)
		if incidentID == "" {
			continue
		}
		if incident.CreatedAt.IsZero() {
			incident.CreatedAt = time.Now().UTC()
		}
		s.items[incidentID] = incident
	}
	s.counter = snapshot.Counter
}

func (s *TLSStore) SnapshotRecords() []tlsCertificateRecordSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	records := make([]tlsCertificateRecordSnapshot, 0, len(s.cert))
	for _, record := range s.cert {
		records = append(records, tlsCertificateRecordSnapshot{
			Meta:    record.meta,
			CertPEM: record.certPEM,
			KeyEnc:  record.keyEnc,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Meta.ID < records[j].Meta.ID })
	return records
}

func (s *TLSStore) RestoreRecords(records []tlsCertificateRecordSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cert = make(map[string]tlsCertificateRecord)
	for _, snapshot := range records {
		id := normalizeIdentifier(snapshot.Meta.ID)
		hostname := strings.ToLower(strings.TrimSpace(snapshot.Meta.Hostname))
		if !identifierPattern.MatchString(id) || hostname == "" {
			continue
		}
		if strings.TrimSpace(snapshot.CertPEM) == "" || strings.TrimSpace(snapshot.KeyEnc) == "" {
			continue
		}
		meta := snapshot.Meta
		meta.ID = id
		meta.Hostname = hostname
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = time.Now().UTC()
		}
		if meta.UpdatedAt.IsZero() {
			meta.UpdatedAt = meta.CreatedAt
		}
		s.cert[id] = tlsCertificateRecord{
			meta:    meta,
			certPEM: snapshot.CertPEM,
			keyEnc:  snapshot.KeyEnc,
		}
	}
}
