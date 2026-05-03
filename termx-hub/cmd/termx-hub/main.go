package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	hubv1 "github.com/lozzow/termx/termx-core/remote/hubv1"
	"github.com/lozzow/termx/termx-hub/internal/controlclient"
	"github.com/lozzow/termx/termx-hub/internal/httpapi"
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
		Managed:    svc,
		Registry:   reg,
		ICEServers: stunServersFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS")),
		DebugToken: strings.TrimSpace(os.Getenv("TERMX_HUB_DEBUG_TOKEN")),
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
