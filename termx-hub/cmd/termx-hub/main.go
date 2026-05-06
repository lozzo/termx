package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/grpcadapter"
	hubheartbeat "github.com/lozzow/termx/termx-remote/hub/heartbeat"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/ice"
	"github.com/lozzow/termx/termx-remote/hub/registry"
	hubturn "github.com/lozzow/termx/termx-remote/hub/turn"
	hubv1 "github.com/lozzow/termx/termx-remote/protocol/hubv1"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"
)

func main() {
	addr := strings.TrimSpace(os.Getenv("TERMX_HUB_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8447"
	}
	runtime, err := newHubRuntimeFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Cleanup()
	heartbeatCfg := heartbeatConfigFromEnv()
	heartbeatCfg.TrafficReader = runtime.TrafficReader
	stopHeartbeat := startHubHeartbeatLoop(heartbeatCfg, runtime.Registry)
	defer stopHeartbeat()
	if cleaner, ok := runtime.Handler.(interface {
		StartCleanup(context.Context, <-chan time.Time)
	}); ok {
		cleanupCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		go cleaner.StartCleanup(cleanupCtx, ticker.C)
	}
	log.Printf("termx hub listening on http://%s", addr)
	if err := serveHub(addr, runtime); err != nil {
		log.Fatal(err)
	}
}

type hubRuntime struct {
	Handler       http.Handler
	GRPCServer    *grpc.Server
	Registry      *registry.Registry
	TrafficReader hubturn.TrafficReader
	Cleanup       func()
}

func newHubHandlerFromEnv() (http.Handler, *registry.Registry, func(), error) {
	runtime, err := newHubRuntimeFromEnv()
	if err != nil {
		return nil, nil, nil, err
	}
	return runtime.Handler, runtime.Registry, runtime.Cleanup, nil
}

func newHubRuntimeFromEnv() (hubRuntime, error) {
	turnServer, err := turnServerFromEnv()
	if err != nil {
		return hubRuntime{}, err
	}
	cleanup := func() {
		if turnServer != nil {
			if err := turnServer.Close(); err != nil {
				log.Printf("close turn server: %v", err)
			}
		}
	}
	reg := registry.New(registry.Config{})
	allowRelay := relayAvailableFromEnv(turnServer)
	svc := cloud.NewService(cloud.Config{Registry: reg, AllowRelayByDefault: allowRelay})
	iceServers := stunServersFromEnv(os.Getenv("TERMX_HUB_STUN_SERVERS"))
	iceSvc := iceServiceFromEnv(turnServer)
	runtime := hubRuntime{
		Handler: httpapi.NewHandler(httpapi.Config{
			Cloud:          svc,
			Registry:       reg,
			ICE:            iceSvc,
			ICEServers:     iceServers,
			AllowedOrigins: csvList(os.Getenv("TERMX_HUB_ALLOWED_ORIGINS")),
		}),
		GRPCServer: grpcadapter.NewServerWithConfig(grpcadapter.ServerConfig{
			Registry:   reg,
			Cloud:      svc,
			ICE:        iceSvc,
			ICEServers: iceServers,
			AllowRelay: allowRelay,
		}),
		Registry: reg,
		Cleanup:  cleanup,
	}
	if turnServer != nil {
		runtime.TrafficReader = turnServer
	}
	return runtime, nil
}

func serveHub(addr string, runtime hubRuntime) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	mux := cmux.New(listener)
	grpcListener := mux.Match(cmux.HTTP2())
	httpListener := mux.Match(cmux.HTTP1Fast())
	httpServer := &http.Server{Handler: runtime.Handler}
	errCh := make(chan error, 3)
	go func() {
		if runtime.GRPCServer == nil {
			errCh <- nil
			return
		}
		if err := runtime.GRPCServer.Serve(grpcListener); err != nil && !errors.Is(err, net.ErrClosed) && !isClosedNetworkError(err) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	go func() {
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) && !isClosedNetworkError(err) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	go func() {
		if err := mux.Serve(); err != nil && !errors.Is(err, net.ErrClosed) && !isClosedNetworkError(err) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	return <-errCh
}

func isClosedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "use of closed network connection") || strings.Contains(text, "listener closed")
}

func heartbeatConfigFromEnv() hubheartbeat.Config {
	cfg := hubheartbeat.Config{
		ControlURL:    strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_URL")),
		Secret:        strings.TrimSpace(os.Getenv("TERMX_HUB_CONTROL_SECRET")),
		HubID:         hubIDFromEnv(),
		Name:          strings.TrimSpace(os.Getenv("TERMX_HUB_NAME")),
		Region:        strings.TrimSpace(os.Getenv("TERMX_HUB_REGION")),
		HTTPURL:       strings.TrimSpace(os.Getenv("TERMX_HUB_PUBLIC_HTTP_URL")),
		GRPCURL:       strings.TrimSpace(os.Getenv("TERMX_HUB_PUBLIC_GRPC_URL")),
		BandwidthMbps: floatFromEnv("TERMX_HUB_BANDWIDTH_MBPS"),
		CPUCores:      floatFromEnv("TERMX_HUB_CPU_CORES"),
		MemoryGB:      floatFromEnv("TERMX_HUB_MEMORY_GB"),
		MaxAgents:     intFromEnv("TERMX_HUB_MAX_AGENTS"),
		Interval:      durationFromEnv("TERMX_HUB_HEARTBEAT_INTERVAL", time.Minute),
	}
	return cfg
}

func startHubHeartbeatLoop(cfg hubheartbeat.Config, reg *registry.Registry) func() {
	if !cfg.Enabled() {
		return func() {}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	go hubheartbeat.RunLoop(ctx, cfg, reg)
	return cancel
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
