package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubturn "github.com/lozzow/termx/termx-remote/hub/turn"
)

type Config struct {
	ControlURL    string
	Secret        string
	HubID         string
	Name          string
	Region        string
	HTTPURL       string
	GRPCURL       string
	BandwidthMbps float64
	CPUCores      float64
	MemoryGB      float64
	MaxAgents     int
	Interval      time.Duration
	Client        *http.Client
	TrafficReader hubturn.TrafficReader
}

type responseBody struct {
	OK         bool     `json:"ok"`
	KickAgents []string `json:"kick_agents"`
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.ControlURL) != "" && strings.TrimSpace(c.Secret) != "" && strings.TrimSpace(c.HubID) != "" && strings.TrimSpace(c.HTTPURL) != ""
}

func RunLoop(ctx context.Context, cfg Config, reg *registry.Registry) {
	if err := Post(ctx, cfg, reg); err != nil {
		log.Printf("hub heartbeat failed: %v", err)
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := Post(ctx, cfg, reg); err != nil {
				log.Printf("hub heartbeat failed: %v", err)
			}
		}
	}
}

func Post(ctx context.Context, cfg Config, reg *registry.Registry) error {
	if !cfg.Enabled() {
		return nil
	}
	agentIDs := []string{}
	if reg != nil {
		seen := map[string]bool{}
		for _, agent := range reg.Agents() {
			if agent.MachineID == "" || seen[agent.MachineID] {
				continue
			}
			seen[agent.MachineID] = true
			agentIDs = append(agentIDs, agent.MachineID)
		}
	}
	body := map[string]any{
		"hub_id":    cfg.HubID,
		"agent_ids": agentIDs,
		"static": map[string]any{
			"http_url":       cfg.HTTPURL,
			"grpc_url":       cfg.GRPCURL,
			"bandwidth_mbps": cfg.BandwidthMbps,
			"cpu_cores":      cfg.CPUCores,
			"memory_gb":      cfg.MemoryGB,
			"max_agents":     cfg.MaxAgents,
			"name":           cfg.Name,
			"region":         cfg.Region,
		},
	}
	if cfg.TrafficReader != nil {
		if traffic := trafficDeltas(cfg.TrafficReader.DrainTraffic()); len(traffic) > 0 {
			body["traffic"] = traffic
		}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.ControlURL, "/")+"/api/internal/hubs/heartbeat", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TermX-Hub-Secret", cfg.Secret)
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("control heartbeat rejected request: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result responseBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); err != nil && err != io.EOF {
		return fmt.Errorf("decode control heartbeat response: %w", err)
	}
	applyKickAgents(reg, result.KickAgents)
	return nil
}

func trafficDeltas(in []hubturn.TrafficDelta) []hubturn.TrafficDelta {
	out := make([]hubturn.TrafficDelta, 0, len(in))
	for _, delta := range in {
		if strings.TrimSpace(delta.AgentID) == "" || (delta.BytesIn <= 0 && delta.BytesOut <= 0) {
			continue
		}
		out = append(out, hubturn.TrafficDelta{
			AgentID:  strings.TrimSpace(delta.AgentID),
			BytesIn:  maxInt64(delta.BytesIn, 0),
			BytesOut: maxInt64(delta.BytesOut, 0),
		})
	}
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func applyKickAgents(reg *registry.Registry, kickAgents []string) {
	if reg == nil || len(kickAgents) == 0 {
		return
	}
	agents := reg.Agents()
	for _, kicked := range kickAgents {
		kicked = strings.TrimSpace(kicked)
		if kicked == "" {
			continue
		}
		for _, agent := range agents {
			if agent.MachineID != kicked && agent.ID != kicked {
				continue
			}
			reg.ForceOffline(registry.ForceOfflineInput{
				MachineID: agent.MachineID,
				AgentID:   agent.ID,
				Reason:    "control heartbeat kick",
			})
		}
	}
}
