package store

import "fmt"

type SnapshotStore interface {
	Driver() string
	Load() ([]byte, error)
	Save(payload []byte) error
	Health() map[string]any
}

func NewSnapshotStore(driver, sqlitePath string) (SnapshotStore, error) {
	driver = normalizeDriver(driver)
	switch driver {
	case "memory":
		return NewMemorySnapshotStore(), nil
	case "sqlite":
		return NewSQLiteSnapshotStore(sqlitePath)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", driver)
	}
}

func normalizeDriver(driver string) string {
	switch driver {
	case "":
		// Programmatic zero-value configs should stay test-friendly.
		// Env-based runtime defaults are handled in gateway config loading.
		return "memory"
	case "sqlite":
		return "sqlite"
	case "memory":
		return "memory"
	default:
		return driver
	}
}
