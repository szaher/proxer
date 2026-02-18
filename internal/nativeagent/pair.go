package nativeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/szaher/try/proxer/internal/protocol"
)

func pairWithGateway(ctx context.Context, gatewayBaseURL, agentID, pairToken string) (protocol.PairAgentResponse, error) {
	payload := protocol.PairAgentRequest{
		PairToken: strings.TrimSpace(pairToken),
		AgentID:   strings.TrimSpace(agentID),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return protocol.PairAgentResponse{}, fmt.Errorf("encode pair request: %w", err)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, strings.TrimRight(gatewayBaseURL, "/")+"/api/agent/pair", bytes.NewReader(body))
	if err != nil {
		return protocol.PairAgentResponse{}, fmt.Errorf("build pair request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return protocol.PairAgentResponse{}, fmt.Errorf("send pair request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		content, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		return protocol.PairAgentResponse{}, fmt.Errorf("pair request failed (status %d): %s", response.StatusCode, strings.TrimSpace(string(content)))
	}

	var pairResp protocol.PairAgentResponse
	if err := json.NewDecoder(response.Body).Decode(&pairResp); err != nil {
		return protocol.PairAgentResponse{}, fmt.Errorf("decode pair response: %w", err)
	}
	if strings.TrimSpace(pairResp.ConnectorID) == "" || strings.TrimSpace(pairResp.ConnectorSecret) == "" {
		return protocol.PairAgentResponse{}, fmt.Errorf("pair response did not include connector credentials")
	}
	return pairResp, nil
}
