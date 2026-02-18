package store

import (
	"context"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed sqlite_migrations/*.sql
var sqliteMigrationsFS embed.FS

type SQLiteSnapshotStore struct {
	path string
	mu   sync.Mutex
}

func NewSQLiteSnapshotStore(path string) (*SQLiteSnapshotStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, fmt.Errorf("sqlite3 binary not found in PATH: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}

	store := &SQLiteSnapshotStore{path: path}
	if err := store.applyMigrations(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteSnapshotStore) Driver() string {
	return "sqlite"
}

func (s *SQLiteSnapshotStore) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	hexPayload, err := s.execNoLock("SELECT hex(payload) FROM proxer_state WHERE id=1;")
	if err != nil {
		return nil, err
	}
	hexPayload = strings.TrimSpace(hexPayload)
	if hexPayload == "" {
		return nil, nil
	}
	payload, err := hex.DecodeString(hexPayload)
	if err != nil {
		return nil, fmt.Errorf("decode persisted payload: %w", err)
	}
	return payload, nil
}

func (s *SQLiteSnapshotStore) Save(payload []byte) error {
	hexPayload := strings.ToUpper(hex.EncodeToString(payload))
	query := fmt.Sprintf("INSERT INTO proxer_state(id, payload, updated_at) VALUES (1, CAST(X'%s' AS TEXT), datetime('now')) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at;", hexPayload)

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.execNoLock(query)
	return err
}

func (s *SQLiteSnapshotStore) Health() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := "ok"
	if _, err := s.execNoLock("SELECT 1;"); err != nil {
		status = "error"
	}
	return map[string]any{
		"driver": "sqlite",
		"path":   s.path,
		"status": status,
	}
}

func (s *SQLiteSnapshotStore) applyMigrations() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.execNoLock("CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);"); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	appliedRaw, err := s.execNoLock("SELECT version FROM schema_migrations ORDER BY version;")
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	applied := make(map[int]struct{})
	for _, line := range strings.Split(strings.TrimSpace(appliedRaw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		version, convErr := strconv.Atoi(line)
		if convErr != nil {
			continue
		}
		applied[version] = struct{}{}
	}

	entries, err := fs.ReadDir(sqliteMigrationsFS, "sqlite_migrations")
	if err != nil {
		return fmt.Errorf("read sqlite migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) == 0 {
			continue
		}
		version, convErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		if convErr != nil {
			continue
		}
		if _, ok := applied[version]; ok {
			continue
		}

		sqlBytes, readErr := fs.ReadFile(sqliteMigrationsFS, filepath.Join("sqlite_migrations", name))
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", name, readErr)
		}
		migrationSQL := strings.TrimSpace(string(sqlBytes))
		if migrationSQL == "" {
			continue
		}
		query := fmt.Sprintf("BEGIN; %s INSERT INTO schema_migrations(version, applied_at) VALUES(%d, datetime('now')); COMMIT;", migrationSQL, version)
		if _, runErr := s.execNoLock(query); runErr != nil {
			return fmt.Errorf("apply migration %s: %w", name, runErr)
		}
	}
	return nil
}

func (s *SQLiteSnapshotStore) execNoLock(query string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sqlite3", "-batch", "-noheader", s.path, query)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if trimmed == "" {
			return "", fmt.Errorf("sqlite3 query failed: %w", err)
		}
		return "", fmt.Errorf("sqlite3 query failed: %w: %s", err, trimmed)
	}
	return trimmed, nil
}
