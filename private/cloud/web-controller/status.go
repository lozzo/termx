package webcontroller

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StatusConfig 配置 Web Controller 的只读运维 surface。
// 上游只能是同机 loopback Control Plane/Hub；该 surface 不接收 cloud credential、signaling 或 terminal payload。
type StatusConfig struct {
	ControlPlaneURL string
	HubURL          string
	RelayURL        string
	HTTPClient      *http.Client
	Catalog         *Catalog
	Commerce        *CommerceService
}

// StatusHandler 返回独立 Web Controller 运维 handler。
// `/healthz` 只表示本进程存活，`/v1/status` 实时探测 owning service；上游失败会返回 503，不做 legacy fallback。
func StatusHandler(config StatusConfig) (http.Handler, error) {
	for _, origin := range []string{config.ControlPlaneURL, config.HubURL} {
		parsed, err := url.Parse(strings.TrimSpace(origin))
		if err != nil {
			return nil, fmt.Errorf("Web Controller upstream must be a loopback HTTP origin")
		}
		host := net.ParseIP(parsed.Hostname())
		if parsed.Scheme != "http" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || host == nil || !host.IsLoopback() {
			return nil, fmt.Errorf("Web Controller upstream must be a loopback HTTP origin")
		}
	}
	if strings.TrimSpace(config.RelayURL) == "" {
		return nil, fmt.Errorf("Web Controller Relay metadata is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/v1/status", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		controlReady := upstreamReady(client, config.ControlPlaneURL)
		hubReady := upstreamReady(client, config.HubURL)
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Type", "application/json")
		if !controlReady || !hubReady {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"control_plane_ready": controlReady,
			"hub_ready":           hubReady,
			"relay":               config.RelayURL,
		})
	})
	mux.HandleFunc("/v1/catalog", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if config.Catalog == nil {
			http.Error(writer, "catalog is not configured", http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(config.Catalog)
	})
	if config.Commerce != nil {
		mux.Handle("/v1/web/", CommerceHandler(config.Commerce))
	}
	return mux, nil
}

func upstreamReady(client *http.Client, origin string) bool {
	response, err := client.Get(origin + "/healthz")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusNoContent
}
