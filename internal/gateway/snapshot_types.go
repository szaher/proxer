package gateway

import "time"

type ServerSnapshot struct {
	Version    int                            `json:"version"`
	SavedAt    time.Time                      `json:"saved_at"`
	AuthUsers  []authUserSnapshot             `json:"auth_users"`
	Rules      ruleStoreSnapshot              `json:"rules"`
	Connectors connectorStoreSnapshot         `json:"connectors"`
	Plans      planStoreSnapshot              `json:"plans"`
	Incidents  incidentStoreSnapshot          `json:"incidents"`
	TLSRecords []tlsCertificateRecordSnapshot `json:"tls_records"`
}
