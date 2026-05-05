package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/controlclient"
	"github.com/lozzow/termx/termx-remote/hub/heartbeat"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
)

var newControlVerifier registryVerifierFactory = func(baseURL string, sharedSecret string) controlVerifier {
	return controlclient.NewAgentControlClient(controlclient.AgentControlClientConfig{
		BaseURL:      baseURL,
		SharedSecret: sharedSecret,
	})
}

type controlVerifier interface {
	VerifyAgentRegistration(context.Context, registry.AgentRegistration) error
	cloud.ConnectionTicketVerifier
	httpapi.AgentPolicyProvider
}

type registryVerifierFactory func(baseURL string, sharedSecret string) controlVerifier

func main() {
	addr := strings.TrimSpace(os.Getenv("TERMX_HUB_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8447"
	}
	handler, registry, err := newHubHandlerFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	if cleaner, ok := handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	}); ok {
		cleanupCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		go cleaner.StartCleanup(cleanupCtx, ticker.C)
	}
	heartbeatCfg := heartbeatConfigFromEnv(addr)
	if heartbeatCfg.Enabled() {
		heartbeatCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go heartbeat.RunLoop(heartbeatCtx, heartbeatCfg, registry)
	}
	log.Printf("termx hub listening on http://%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func newHubHandlerFromEnv() (http.Handler, *registry.Registry, error) {
	controlURL := strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_URL"))
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_SECRET"))
	if controlURL == "" || secret == "" {
		return nil, nil, errors.New("TERMX_HUB_CONTROL_URL and TERMX_HUB_CONTROL_SECRET are required")
	}
	verifier := newControlVerifier(controlURL, secret)
	tickets := verifier
	reg := registry.New(registry.Config{Verifier: hubAuthority{
		control: verifier,
		tickets: tickets,
	}})
	svc := cloud.NewService(cloud.Config{Registry: reg, Tickets: tickets})
	return httpapi.NewHandler(httpapi.Config{
		Cloud:          svc,
		Registry:       reg,
		AgentPolicy:    verifier,
		ICE:            iceServiceFromEnv(),
		ICEServers:     stunServersFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS")),
		HubID:          hubIDFromEnv(),
		InternalSecret: secret,
		DebugToken:     strings.TrimSpace(os.Getenv("TERMX_HUB_DEBUG_TOKEN")),
		AllowedOrigins: csvList(os.Getenv("TERMX_HUB_ALLOWED_ORIGINS")),
	}), reg, nil
}

type hubAuthority struct {
	control controlVerifier
	tickets cloud.ConnectionTicketVerifier
}

func (a hubAuthority) VerifyAgentRegistration(ctx context.Context, in registry.AgentRegistration) error {
	return a.control.VerifyAgentRegistration(ctx, in)
}

func (a hubAuthority) VerifyOfferTicket(ctx context.Context, in registry.OfferTicket) error {
	return a.tickets.VerifyOfferTicket(ctx, in)
}

func heartbeatConfigFromEnv(addr string) heartbeat.Config {
	interval := durationFromEnv("TERMX_HUB_HEARTBEAT_INTERVAL", 30*time.Second)
	httpURL := strings.TrimSpace(os.Getenv("TERMX_HUB_PUBLIC_HTTP_URL"))
	if httpURL == "" {
		httpURL = "http://" + strings.TrimSpace(addr)
	}
	hubID := hubIDFromEnv()
	name := strings.TrimSpace(os.Getenv("TERMX_HUB_NAME"))
	if name == "" {
		name = hubID
	}
	return heartbeat.Config{
		ControlURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_URL")), "/"),
		Secret:        strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_SECRET")),
		HubID:         hubID,
		Name:          name,
		Region:        strings.TrimSpace(os.Getenv("TERMX_HUB_REGION")),
		HTTPURL:       httpURL,
		GRPCURL:       strings.TrimSpace(os.Getenv("TERMX_HUB_GRPC_URL")),
		BandwidthMbps: floatFromEnv("TERMX_HUB_BANDWIDTH_MBPS"),
		CPUCores:      floatFromEnv("TERMX_HUB_CPU_CORES"),
		MemoryGB:      floatFromEnv("TERMX_HUB_MEMORY_GB"),
		MaxAgents:     intFromEnv("TERMX_HUB_MAX_AGENTS"),
		Interval:      interval,
		Client:        &http.Client{Timeout: 10 * time.Second},
	}
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

func iceServiceFromEnv() *ice.Service {
	stunURLs := urlsFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS"), "stun:", "stuns:")
	turnURLs := urlsFromEnv(os.Getenv("TERMX_HUB_TURN_SERVERS"), "turn:", "turns:")
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_TURN_SHARED_SECRET"))
	if len(turnURLs) == 0 || secret == "" {
		return nil
	}
	return ice.NewService(ice.Config{
		SharedSecret: secret,
		STUNURLs:     stunURLs,
		TURNURLs:     turnURLs,
	})
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
