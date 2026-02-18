package nativeagent

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/szaher/try/proxer/internal/protocol"
)

func parseTunnelMappings(raw string) ([]protocol.TunnelConfig, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}
	entries := strings.Split(value, ",")
	tunnels := make([]protocol.TunnelConfig, 0, len(entries))
	seen := map[string]struct{}{}

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tunnel format %q; expected id=url or id@token=url", entry)
		}
		lhs := strings.TrimSpace(parts[0])
		rhs := strings.TrimSpace(parts[1])
		if lhs == "" || rhs == "" {
			return nil, fmt.Errorf("invalid tunnel mapping %q", entry)
		}
		id := lhs
		token := ""
		if at := strings.Index(lhs, "@"); at > 0 {
			id = strings.TrimSpace(lhs[:at])
			token = strings.TrimSpace(lhs[at+1:])
		}
		if id == "" {
			return nil, fmt.Errorf("tunnel id cannot be empty")
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("duplicate tunnel id %q", id)
		}
		if _, err := url.ParseRequestURI(rhs); err != nil {
			return nil, fmt.Errorf("invalid target URL for tunnel %q: %w", id, err)
		}
		seen[id] = struct{}{}
		tunnels = append(tunnels, protocol.TunnelConfig{ID: id, Target: rhs, Token: token})
	}

	sort.Slice(tunnels, func(i, j int) bool {
		return tunnels[i].ID < tunnels[j].ID
	})
	return tunnels, nil
}

func formatTunnelMappings(tunnels []protocol.TunnelConfig) string {
	if len(tunnels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(tunnels))
	for _, tunnel := range tunnels {
		lhs := tunnel.ID
		if strings.TrimSpace(tunnel.Token) != "" {
			lhs = lhs + "@" + tunnel.Token
		}
		parts = append(parts, lhs+"="+tunnel.Target)
	}
	return strings.Join(parts, ",")
}
