package httpapi

import (
	"encoding/json"
	"net/http"
)

const defaultServiceName = "termx-web-control"

type Config struct {
	ServiceName string
	Version     string
}

type HealthResponse struct {
	Service   string `json:"service"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	Runtime   string `json:"runtime"`
	Transport string `json:"transport"`
}

func NewRouter(cfg Config) http.Handler {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, HealthResponse{
			Service:   cfg.ServiceName,
			Status:    "ok",
			Version:   cfg.Version,
			Runtime:   "control-plane",
			Transport: "signaling-control-only",
		})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
