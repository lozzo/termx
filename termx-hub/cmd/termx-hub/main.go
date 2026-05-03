package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-hub/internal/controlclient"
	"github.com/lozzow/termx/termx-hub/internal/httpapi"
	"github.com/lozzow/termx/termx-hub/internal/ice"
	"github.com/lozzow/termx/termx-hub/internal/managed"
	"github.com/lozzow/termx/termx-hub/internal/registry"
)

var newControlVerifier registryVerifierFactory = func(baseURL string, sharedSecret string) controlVerifier {
	return controlclient.NewManagedTicketVerifier(controlclient.ManagedTicketVerifierConfig{
		BaseURL:      baseURL,
		SharedSecret: sharedSecret,
	})
}

type controlVerifier interface {
	registry.AuthorityVerifier
	managed.TicketVerifier
	httpapi.AgentPolicyProvider
}

type registryVerifierFactory func(baseURL string, sharedSecret string) controlVerifier

func main() {
	addr := strings.TrimSpace(os.Getenv("TERMX_HUB_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8447"
	}
	handler, err := newHubHandlerFromEnv()
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
	log.Printf("termx hub listening on http://%s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func newHubHandlerFromEnv() (http.Handler, error) {
	controlURL := strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_URL"))
	secret := strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_SECRET"))
	if controlURL == "" || secret == "" {
		return nil, errors.New("TERMX_HUB_CONTROL_URL and TERMX_HUB_CONTROL_SECRET are required")
	}
	verifier := newControlVerifier(controlURL, secret)
	reg := registry.New(registry.Config{Verifier: verifier})
	svc := managed.NewService(managed.Config{Registry: reg, Tickets: verifier})
	return httpapi.NewHandler(httpapi.Config{
		Managed:     svc,
		Registry:    reg,
		AgentPolicy: verifier,
		ICE:         iceServiceFromEnv(),
		ICEServers:  stunServersFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS")),
		DebugToken:  strings.TrimSpace(os.Getenv("TERMX_HUB_DEBUG_TOKEN")),
	}), nil
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
