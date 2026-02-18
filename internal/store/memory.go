package store

import "sync"

type MemorySnapshotStore struct {
	mu      sync.RWMutex
	payload []byte
}

func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{}
}

func (s *MemorySnapshotStore) Driver() string {
	return "memory"
}

func (s *MemorySnapshotStore) Load() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.payload) == 0 {
		return nil, nil
	}
	copyPayload := make([]byte, len(s.payload))
	copy(copyPayload, s.payload)
	return copyPayload, nil
}

func (s *MemorySnapshotStore) Save(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.payload = make([]byte, len(payload))
	copy(s.payload, payload)
	return nil
}

func (s *MemorySnapshotStore) Health() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"driver":   "memory",
		"status":   "ok",
		"has_data": len(s.payload) > 0,
	}
}
