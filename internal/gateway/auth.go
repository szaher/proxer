package gateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	RoleSuperAdmin  = "super_admin"
	RoleTenantAdmin = "tenant_admin"
	RoleMember      = "member"
	// Backward compatibility for migrated/admin-created users.
	RoleAdmin = RoleSuperAdmin
)

type User struct {
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	TenantID  string    `json:"tenant_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type authUserRecord struct {
	user         User
	passwordHash string
}

type authSession struct {
	ID        string
	Username  string
	ExpiresAt time.Time
}

type AuthStore struct {
	sessionTTL time.Duration

	mu       sync.RWMutex
	users    map[string]authUserRecord
	sessions map[string]authSession
}

func NewAuthStore(adminUsername, adminPassword string, sessionTTL time.Duration) (*AuthStore, error) {
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}
	store := &AuthStore{
		sessionTTL: sessionTTL,
		users:      make(map[string]authUserRecord),
		sessions:   make(map[string]authSession),
	}

	adminUsername = normalizeUsername(adminUsername)
	if adminUsername == "" {
		adminUsername = "admin"
	}
	if strings.TrimSpace(adminPassword) == "" {
		return nil, fmt.Errorf("admin password cannot be empty")
	}

	_, err := store.registerUserLocked(RegisterUserInput{
		Username: adminUsername,
		Password: adminPassword,
		TenantID: "",
		Role:     RoleSuperAdmin,
		Status:   "active",
	})
	if err != nil {
		return nil, err
	}

	return store, nil
}

type RegisterUserInput struct {
	Username string
	Password string
	TenantID string
	Role     string
	Status   string
}

func (s *AuthStore) RegisterUser(input RegisterUserInput) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registerUserLocked(input)
}

func (s *AuthStore) registerUserLocked(input RegisterUserInput) (User, error) {
	username := normalizeUsername(input.Username)
	if !identifierPattern.MatchString(username) {
		return User{}, fmt.Errorf("invalid username %q", username)
	}
	if len(strings.TrimSpace(input.Password)) < 6 {
		return User{}, fmt.Errorf("password must be at least 6 characters")
	}
	if _, exists := s.users[username]; exists {
		return User{}, fmt.Errorf("username %q already exists", username)
	}

	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	if !identifierPattern.MatchString(tenantID) {
		return User{}, fmt.Errorf("invalid tenant id %q", tenantID)
	}

	role := strings.ToLower(strings.TrimSpace(input.Role))
	if role == "" {
		role = RoleMember
	}
	if role == "admin" {
		role = RoleSuperAdmin
	}
	if role != RoleMember && role != RoleTenantAdmin && role != RoleSuperAdmin {
		return User{}, fmt.Errorf("invalid role %q", role)
	}

	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "disabled" {
		return User{}, fmt.Errorf("invalid status %q", status)
	}

	if role == RoleSuperAdmin {
		tenantID = ""
	}
	if role != RoleSuperAdmin && tenantID == "" {
		tenantID = DefaultTenantID
	}

	now := time.Now().UTC()
	user := User{
		Username:  username,
		Role:      role,
		TenantID:  tenantID,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[username] = authUserRecord{
		user:         user,
		passwordHash: hashPassword(input.Password),
	}
	return user, nil
}

func (s *AuthStore) Authenticate(username, password string) (User, bool) {
	username = normalizeUsername(username)
	if username == "" || strings.TrimSpace(password) == "" {
		return User{}, false
	}

	s.mu.RLock()
	record, ok := s.users[username]
	s.mu.RUnlock()
	if !ok {
		return User{}, false
	}
	if record.passwordHash != hashPassword(password) {
		return User{}, false
	}
	if strings.TrimSpace(record.user.Status) != "active" {
		return User{}, false
	}
	return record.user, true
}

func (s *AuthStore) NewSession(username string) (string, error) {
	username = normalizeUsername(username)
	if username == "" {
		return "", fmt.Errorf("missing username")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredSessionsLocked(time.Now().UTC())

	if _, ok := s.users[username]; !ok {
		return "", fmt.Errorf("unknown user")
	}

	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	s.sessions[token] = authSession{
		ID:        token,
		Username:  username,
		ExpiresAt: time.Now().UTC().Add(s.sessionTTL),
	}
	return token, nil
}

func (s *AuthStore) ResolveSession(sessionID string) (User, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return User{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupExpiredSessionsLocked(now)

	session, ok := s.sessions[sessionID]
	if !ok {
		return User{}, false
	}
	if now.After(session.ExpiresAt) {
		delete(s.sessions, sessionID)
		return User{}, false
	}
	record, ok := s.users[session.Username]
	if !ok {
		delete(s.sessions, sessionID)
		return User{}, false
	}

	// Sliding expiration for active sessions.
	session.ExpiresAt = now.Add(s.sessionTTL)
	s.sessions[sessionID] = session
	return record.user, true
}

func (s *AuthStore) DeleteSession(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *AuthStore) ListUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]User, 0, len(s.users))
	for _, record := range s.users {
		users = append(users, record.user)
	}
	sortUsers(users)
	return users
}

func (s *AuthStore) GetUser(username string) (User, bool) {
	username = normalizeUsername(username)
	if username == "" {
		return User{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.users[username]
	if !ok {
		return User{}, false
	}
	return record.user, true
}

type UpdateUserInput struct {
	Username string
	Role     string
	TenantID string
	Status   string
	Password string
}

func (s *AuthStore) UpdateUser(input UpdateUserInput) (User, error) {
	username := normalizeUsername(input.Username)
	if username == "" {
		return User{}, fmt.Errorf("missing username")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.users[username]
	if !ok {
		return User{}, fmt.Errorf("user %q not found", username)
	}

	if role := strings.TrimSpace(input.Role); role != "" {
		normalized := strings.ToLower(role)
		if normalized == "admin" {
			normalized = RoleSuperAdmin
		}
		if normalized != RoleSuperAdmin && normalized != RoleTenantAdmin && normalized != RoleMember {
			return User{}, fmt.Errorf("invalid role %q", role)
		}
		record.user.Role = normalized
		if normalized == RoleSuperAdmin {
			record.user.TenantID = ""
		}
	}

	if tenantID := strings.TrimSpace(input.TenantID); tenantID != "" {
		if !identifierPattern.MatchString(tenantID) {
			return User{}, fmt.Errorf("invalid tenant id %q", tenantID)
		}
		if record.user.Role != RoleSuperAdmin {
			record.user.TenantID = tenantID
		}
	}

	if status := strings.ToLower(strings.TrimSpace(input.Status)); status != "" {
		if status != "active" && status != "disabled" {
			return User{}, fmt.Errorf("invalid status %q", status)
		}
		record.user.Status = status
	}

	if password := strings.TrimSpace(input.Password); password != "" {
		if len(password) < 6 {
			return User{}, fmt.Errorf("password must be at least 6 characters")
		}
		record.passwordHash = hashPassword(password)
	}

	record.user.UpdatedAt = time.Now().UTC()
	s.users[username] = record
	return record.user, nil
}

func (s *AuthStore) cleanupExpiredSessionsLocked(now time.Time) {
	for id, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func hashPassword(password string) string {
	password = strings.TrimSpace(password)
	sum := sha256.Sum256([]byte("proxer-v1:" + password))
	return hex.EncodeToString(sum[:])
}

func randomToken(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func sortUsers(users []User) {
	for i := 0; i < len(users)-1; i++ {
		for j := i + 1; j < len(users); j++ {
			if users[i].Username > users[j].Username {
				users[i], users[j] = users[j], users[i]
			}
		}
	}
}
