package nativeagent

import (
	"path/filepath"
	"testing"
)

func TestStoreLoadCreatesDefaults(t *testing.T) {
	t.Parallel()
	store := NewStore(filepath.Join(t.TempDir(), "settings.json"))
	settings, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if settings.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", settings.SchemaVersion, SchemaVersion)
	}
	if settings.LaunchMode != LaunchModeTrayWindow {
		t.Fatalf("LaunchMode = %q, want %q", settings.LaunchMode, LaunchModeTrayWindow)
	}
	if len(settings.Profiles) != 0 {
		t.Fatalf("expected no profiles, got %d", len(settings.Profiles))
	}
}

func TestParseTunnelMappings(t *testing.T) {
	t.Parallel()
	tunnels, err := parseTunnelMappings("app3000=http://127.0.0.1:3000,app8080@tok=http://localhost:8080")
	if err != nil {
		t.Fatalf("parseTunnelMappings() error = %v", err)
	}
	if len(tunnels) != 2 {
		t.Fatalf("len(tunnels) = %d, want 2", len(tunnels))
	}
	if tunnels[0].ID != "app3000" || tunnels[0].Target != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected tunnel[0] = %+v", tunnels[0])
	}
	if tunnels[1].ID != "app8080" || tunnels[1].Token != "tok" {
		t.Fatalf("unexpected tunnel[1] = %+v", tunnels[1])
	}
}
