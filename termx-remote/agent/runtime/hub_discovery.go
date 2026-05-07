package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/discovery"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
)

func (m *Manager) discoverHub(ctx context.Context) error {
	m.mu.RLock()
	controlURL := m.cfg.ControlURL
	accessToken := m.cfg.AccessToken
	preferredRegion := m.cfg.Region
	m.mu.RUnlock()
	if controlURL == "" || accessToken == "" {
		return nil
	}
	hubs, err := discovery.DiscoverHubs(ctx, controlURL, accessToken)
	if err != nil {
		return err
	}
	if hub, ok := selectDiscoveredHub(hubs, hubSelectionOptions{
		PreferredRegion: preferredRegion,
		Now:             time.Now().UTC(),
	}); ok {
		selectedURL := m.selectLowLatencyHubURL(ctx, hubs, hub)
		m.mu.Lock()
		hubURLs := append([]string(nil), m.cfg.HubURLs...)
		previousDiscovered := strings.TrimSpace(m.discoveredHubURL)
		if previousDiscovered != "" && previousDiscovered != selectedURL {
			hubURLs = removeString(hubURLs, previousDiscovered)
		}
		if !containsString(hubURLs, selectedURL) {
			hubURLs = append(hubURLs, selectedURL)
		}
		if previousDiscovered != "" && previousDiscovered != selectedURL {
			m.stopRemovedHubSignalingLocked(hubURLs)
		}
		if strings.TrimSpace(m.cfg.HubURL) == "" || strings.TrimSpace(m.cfg.HubURL) == previousDiscovered {
			m.cfg.HubURL = selectedURL
		}
		m.cfg.HubURLs = hubURLs
		m.discoveredHubURL = selectedURL
		m.configureHubEndpointLocked(selectedURL, HubKindOnline, HubSourceWebControl, remotertc.AnswerOptions{}, false)
		m.mu.Unlock()
	}
	return nil
}

func (m *Manager) selectLowLatencyHubURL(ctx context.Context, hubs []discovery.Hub, fallback discovery.Hub) string {
	urls := make([]string, 0, len(hubs))
	now := time.Now().UTC()
	for _, hub := range hubs {
		if !hubUsable(hub, now) {
			continue
		}
		if url := strings.TrimSpace(hub.HTTPURL); url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) > 0 {
		probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for _, result := range discovery.ProbeHubs(probeCtx, urls, 3*time.Second, 3) {
			if result.Available && strings.TrimSpace(result.URL) != "" {
				return strings.TrimSpace(result.URL)
			}
		}
	}
	return strings.TrimSpace(fallback.HTTPURL)
}

type hubSelectionOptions struct {
	PreferredRegion string
	Now             time.Time
}

func selectDiscoveredHub(hubs []discovery.Hub, opts hubSelectionOptions) (discovery.Hub, bool) {
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	preferredRegion := strings.TrimSpace(opts.PreferredRegion)
	var selected discovery.Hub
	selectedSet := false
	for _, hub := range hubs {
		if !hubUsable(hub, now) {
			continue
		}
		if !selectedSet || compareHubRank(hub, selected, preferredRegion, now) > 0 {
			selected = hub
			selectedSet = true
		}
	}
	return selected, selectedSet
}

func hubUsable(hub discovery.Hub, now time.Time) bool {
	if strings.TrimSpace(hub.HTTPURL) == "" || strings.TrimSpace(hub.Status) != "online" {
		return false
	}
	if hub.Capacity <= 0 {
		return false
	}
	if expiresAt, ok := parseHubExpiry(hub.ExpiresAt); ok && !expiresAt.After(now) {
		return false
	}
	return hubHealthOK(hub.Health)
}

func compareHubRank(a discovery.Hub, b discovery.Hub, preferredRegion string, now time.Time) int {
	if preferredRegion != "" {
		aRegion := strings.TrimSpace(a.Region) == preferredRegion
		bRegion := strings.TrimSpace(b.Region) == preferredRegion
		if aRegion != bRegion {
			if aRegion {
				return 1
			}
			return -1
		}
	}
	if a.Weight != b.Weight {
		if a.Weight > b.Weight {
			return 1
		}
		return -1
	}
	if a.Capacity != b.Capacity {
		if a.Capacity > b.Capacity {
			return 1
		}
		return -1
	}
	aExpiry, aHasExpiry := parseHubExpiry(a.ExpiresAt)
	bExpiry, bHasExpiry := parseHubExpiry(b.ExpiresAt)
	if aHasExpiry != bHasExpiry {
		if aHasExpiry {
			return 1
		}
		return -1
	}
	if aHasExpiry && !aExpiry.Equal(bExpiry) {
		if aExpiry.After(bExpiry) && aExpiry.After(now) {
			return 1
		}
		return -1
	}
	aID := strings.TrimSpace(a.ID)
	bID := strings.TrimSpace(b.ID)
	if aID < bID {
		return 1
	}
	if aID > bID {
		return -1
	}
	return 0
}

func parseHubExpiry(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, true
}

func hubHealthOK(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return true
	}
	var health map[string]any
	if err := json.Unmarshal([]byte(raw), &health); err != nil {
		return false
	}
	if ok, exists := health["ok"].(bool); exists {
		return ok
	}
	if healthy, exists := health["healthy"].(bool); exists {
		return healthy
	}
	if status, exists := health["status"].(string); exists {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "", "ok", "online", "healthy":
			return true
		default:
			return false
		}
	}
	return true
}
