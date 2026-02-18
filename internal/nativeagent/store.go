package nativeagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func NewDefaultStore() (*Store, error) {
	path, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	return NewStore(path), nil
}

func (s *Store) Load() (AgentSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) Save(settings AgentSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(settings)
}

func (s *Store) Update(mutator func(*AgentSettings) error) (AgentSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	settings, err := s.loadLocked()
	if err != nil {
		return AgentSettings{}, err
	}
	if err := mutator(&settings); err != nil {
		return AgentSettings{}, err
	}
	settings.UpdatedAt = time.Now().UTC()
	if err := s.saveLocked(settings); err != nil {
		return AgentSettings{}, err
	}
	return settings, nil
}

func (s *Store) loadLocked() (AgentSettings, error) {
	if s.path == "" {
		return AgentSettings{}, fmt.Errorf("settings path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return AgentSettings{}, fmt.Errorf("create settings directory: %w", err)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			settings := defaultSettings()
			if err := s.saveLocked(settings); err != nil {
				return AgentSettings{}, err
			}
			return settings, nil
		}
		return AgentSettings{}, fmt.Errorf("read settings: %w", err)
	}
	settings := defaultSettings()
	if err := json.Unmarshal(data, &settings); err != nil {
		return AgentSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = SchemaVersion
	}
	if strings.TrimSpace(settings.LaunchMode) == "" {
		settings.LaunchMode = LaunchModeTrayWindow
	}
	if settings.Profiles == nil {
		settings.Profiles = []AgentProfile{}
	}
	for i := range settings.Profiles {
		settings.Profiles[i] = applyProfileDefaults(settings.Profiles[i])
	}
	return settings, nil
}

func (s *Store) saveLocked(settings AgentSettings) error {
	if s.path == "" {
		return fmt.Errorf("settings path is empty")
	}
	if settings.SchemaVersion == 0 {
		settings.SchemaVersion = SchemaVersion
	}
	if strings.TrimSpace(settings.LaunchMode) == "" {
		settings.LaunchMode = LaunchModeTrayWindow
	}
	if settings.CreatedAt.IsZero() {
		settings.CreatedAt = time.Now().UTC()
	}
	if settings.UpdatedAt.IsZero() {
		settings.UpdatedAt = time.Now().UTC()
	}
	if settings.Profiles == nil {
		settings.Profiles = []AgentProfile{}
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write temporary settings file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("persist settings: %w", err)
	}
	return nil
}

func profileIndexByIDOrName(settings AgentSettings, idOrName string) int {
	needle := strings.TrimSpace(idOrName)
	if needle == "" {
		return -1
	}
	for idx, profile := range settings.Profiles {
		if strings.EqualFold(strings.TrimSpace(profile.ID), needle) || strings.EqualFold(strings.TrimSpace(profile.Name), needle) {
			return idx
		}
	}
	return -1
}

func profileByID(settings AgentSettings, profileID string) (AgentProfile, bool) {
	for _, profile := range settings.Profiles {
		if strings.EqualFold(strings.TrimSpace(profile.ID), strings.TrimSpace(profileID)) {
			return profile, true
		}
	}
	return AgentProfile{}, false
}

func ensureUniqueProfileName(settings AgentSettings, name string, exceptID string) error {
	for _, profile := range settings.Profiles {
		if !strings.EqualFold(strings.TrimSpace(profile.Name), strings.TrimSpace(name)) {
			continue
		}
		if exceptID != "" && strings.EqualFold(strings.TrimSpace(profile.ID), strings.TrimSpace(exceptID)) {
			continue
		}
		return fmt.Errorf("profile with name %q already exists", name)
	}
	return nil
}
