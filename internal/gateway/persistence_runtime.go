package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func (s *Server) buildSnapshot() ServerSnapshot {
	return ServerSnapshot{
		Version:    1,
		SavedAt:    time.Now().UTC(),
		AuthUsers:  s.authStore.SnapshotUsers(),
		Rules:      s.ruleStore.Snapshot(),
		Connectors: s.connectorStore.Snapshot(),
		Plans:      s.planStore.Snapshot(),
		Incidents:  s.incidentStore.Snapshot(),
		TLSRecords: s.tlsStore.SnapshotRecords(),
	}
}

func (s *Server) restorePersistentState() error {
	if s.persistence == nil {
		return nil
	}
	payload, err := s.persistence.Load()
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	var snapshot ServerSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return fmt.Errorf("decode persisted snapshot: %w", err)
	}
	if snapshot.Version <= 0 {
		return nil
	}

	s.authStore.RestoreUsers(snapshot.AuthUsers)
	s.ruleStore.Restore(snapshot.Rules)
	s.connectorStore.Restore(snapshot.Connectors)
	s.planStore.Restore(snapshot.Plans)
	s.incidentStore.Restore(snapshot.Incidents)
	s.tlsStore.RestoreRecords(snapshot.TLSRecords)

	s.logger.Printf("restored persisted state using driver=%s saved_at=%s", s.persistence.Driver(), snapshot.SavedAt.Format(time.RFC3339))
	return nil
}

func (s *Server) persistState() {
	if s.persistence == nil {
		return
	}
	snapshot := s.buildSnapshot()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		s.logger.Printf("encode snapshot failed: %v", err)
		s.incidentStore.Add("warning", "storage", fmt.Sprintf("encode snapshot failed: %v", err))
		return
	}
	if err := s.persistence.Save(payload); err != nil {
		s.logger.Printf("persist state failed: %v", err)
		s.incidentStore.Add("warning", "storage", fmt.Sprintf("persist state failed: %v", err))
	}
}

func (s *Server) runPersistenceLoop(ctx context.Context) {
	if s.persistence == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.persistState()
			return
		case <-ticker.C:
			s.persistState()
		}
	}
}

func (s *Server) storageHealth() map[string]any {
	if s.persistence == nil {
		return map[string]any{
			"driver": s.cfg.StorageDriver,
			"status": "unknown",
		}
	}
	health := s.persistence.Health()
	if _, ok := health["driver"]; !ok {
		health["driver"] = s.persistence.Driver()
	}
	return health
}
