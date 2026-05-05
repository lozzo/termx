package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubturn "github.com/lozzow/termx/termx-remote/hub/turn"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

func main() {
	addr := strings.TrimSpace(os.Getenv("TERMX_HUB_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8447"
	}
	handler, _, cleanup, err := newHubHandlerFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	if cleaner, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	}); ok {
		cleanupCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		go cleaner.StartCleanup(cleanupCtx, ticker.C)
	}
	log.Printf("termx hub listening on http://%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func newHubHandlerFromEnv() (http.Handler, *registry.Registry, func(), error) {
	turnServer, err := turnServerFromEnv()
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() {
		if turnServer != nil {
			if err := turnServer.Close(); err != nil {
				log.Printf("close turn server: %v", err)
			}
		}
	}
	reg := registry.New(registry.Config{})
	svc := cloud.NewService(cloud.Config{Registry: reg, AllowRelayByDefault: relayAvailableFromEnv(turnServer)})
	return httpapi.NewHandler(httpapi.Config{
		Cloud:          svc,
		Registry:       reg,
		ICE:            iceServiceFromEnv(turnServer),
		ICEServers:     stunServersFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS")),
		HubID:          hubIDFromEnv(),
		DebugToken:     strings.TrimSpace(os.Getenv("TERMX_HUB_DEBUG_TOKEN")),
		AllowedOrigins: csvList(os.Getenv("TERMX_HUB_ALLOWED_ORIGINS")),
	}), reg, cleanup, nil
}

func hubIDFromEnv() string {
	hubID := strings.TrimSpace(os.Getenv("TERMX_HUB_ID"))
	if hubID == "" {
		hubID = strings.TrimSpace(os.Getenv("HOSTNAME"))
	}
	if hubID == "" {
		hubID = "termx-hub-local"
	}
	return hubID
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func floatFromEnv(name string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(os.Getenv(name)), 64)
	return value
}

func intFromEnv(name string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	return value
}

func csvList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stunServersFromEnv(raw string) []hubv1.RTCIceServerConfig {
	var servers []hubv1.RTCIceServerConfig
	for _, part := range strings.Split(raw, ",") {
		url := strings.TrimSpace(part)
		if url == "" || !strings.HasPrefix(strings.ToLower(url), "stun:") {
			continue
		}
		servers = append(servers, hubv1.RTCIceServerConfig{URLs: []string{url}})
	}
	return servers
}

func iceServiceFromEnv(turnServer *hubturn.Server) *ice.Service {
	stunURLs := urlsFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS"), "stun:", "stuns:")
	turnURLs := urlsFromEnv(os.Getenv("TERMX_HUB_TURN_SERVERS"), "turn:", "turns:")
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_SHARED_SECRET"))
	if turnServer != nil {
		return ice.NewService(ice.Config{
			STUNURLs:   stunURLs,
			TURNServer: turnServer,
		})
	}
	if len(turnURLs) == 0 || secret == "" {
		return nil
	}
	return ice.NewService(ice.Config{
		SharedSecret: secret,
		STUNURLs:     stunURLs,
		TURNURLs:     turnURLs,
	})
}

func relayAvailableFromEnv(turnServer *hubturn.Server) bool {
	if turnServer != nil {
		return true
	}
	turnURLs := urlsFromEnv(os.Getenv("TERMX_HUB_TURN_SERVERS"), "turn:", "turns:")
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_SHARED_SECRET"))
	return len(turnURLs) > 0 && secret != ""
}

func turnServerFromEnv() (*hubturn.Server, error) {
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_SECRET"))
	if secret == "" {
		return nil, nil
	}
	addr := strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_ADDR"))
	if addr == "" {
		addr = "0.0.0.0:3478"
	}
	server, err := hubturn.NewServer(hubturn.Config{
		ListenAddr: addr,
		PublicIP:   strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_PUBLIC_IP")),
		Secret:     secret,
		Realm:      "termx",
	})
	if err != nil {
		return nil, err
	}
	log.Printf("termx hub TURN listening on %s", addr)
	return server, nil
}

func urlsFromEnv(raw string, prefixes ...string) []string {
	var urls []string
	for _, part := range strings.Split(raw, ",") {
		url := strings.TrimSpace(part)
		if url == "" {
			continue
		}
		lower := strings.ToLower(url)
		for _, prefix := range prefixes {
			if strings.HasPrefix(lower, prefix) {
				urls = append(urls, url)
				break
			}
		}
	}
	return urls
}
