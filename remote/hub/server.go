package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	PublicHost      string
	TURNPort        int
	ExtraICEServers []string
}

type TurnCredentialProvider interface {
	GenerateCredentials(deviceID string) (username, credential string)
}

type OfferRequest struct {
	SDP        string   `json:"sdp"`
	Candidates []string `json:"candidates,omitempty"`
}

type Offer struct {
	SessionID      string
	UserID         string
	DeviceID       string
	SDP            string
	Candidates     []string
	ICEServers     []string
	TurnUsername   string
	TurnCredential string
}

type Answer struct {
	SDP        string   `json:"sdp"`
	Candidates []string `json:"candidates,omitempty"`
}

type Server struct {
	cfg      Config
	auth     *Auth
	registry *Registry
	turn     TurnCredentialProvider
}

type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

func NewServer(cfg Config, auth *Auth, registry *Registry, turn TurnCredentialProvider) *Server {
	return &Server{
		cfg:      cfg,
		auth:     auth,
		registry: registry,
		turn:     turn,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/rtc/config", s.auth.Middleware(http.HandlerFunc(s.handleRTCConfig)))
	mux.Handle("POST /api/v1/devices/{id}/rtc/offer", s.auth.Middleware(http.HandlerFunc(s.handleRTCOffer)))
	return mux
}

func (s *Server) handleRTCConfig(w http.ResponseWriter, r *http.Request) {
	username, credential := s.generateTurnCredentials("")
	servers, _ := s.buildICEServers(username, credential)
	writeJSON(w, http.StatusOK, map[string]any{
		"iceServers": servers,
	})
}

func (s *Server) handleRTCOffer(w http.ResponseWriter, r *http.Request) {
	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SDP == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "sdp required"})
		return
	}

	deviceID := r.PathValue("id")
	registration, ok := s.registry.Lookup(deviceID)
	if !ok || registration.UserID != userIDFromContext(r.Context()) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}

	username, credential := s.generateTurnCredentials(deviceID)
	_, flatICEServers := s.buildICEServers(username, credential)
	answer, err := registration.Agent.HandleOffer(r.Context(), Offer{
		SessionID:      fmt.Sprintf("hub-%d", time.Now().UnixNano()),
		UserID:         userIDFromContext(r.Context()),
		DeviceID:       deviceID,
		SDP:            req.SDP,
		Candidates:     append([]string(nil), req.Candidates...),
		ICEServers:     flatICEServers,
		TurnUsername:   username,
		TurnCredential: credential,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

func (s *Server) buildICEServers(turnUsername, turnCredential string) ([]iceServer, []string) {
	port := s.cfg.TURNPort
	if port == 0 {
		port = 3478
	}
	servers := make([]iceServer, 0, 2+len(s.cfg.ExtraICEServers))
	flat := make([]string, 0, 3+len(s.cfg.ExtraICEServers))
	host := strings.TrimSpace(s.cfg.PublicHost)
	if host != "" {
		stunURL := fmt.Sprintf("stun:%s:%d", host, port)
		servers = append(servers, iceServer{URLs: []string{stunURL}})
		flat = append(flat, stunURL)

		if s.turn != nil && turnUsername != "" && turnCredential != "" {
			turnURLs := []string{
				fmt.Sprintf("turn:%s:%d?transport=udp", host, port),
				fmt.Sprintf("turn:%s:%d?transport=tcp", host, port),
			}
			servers = append(servers, iceServer{
				URLs:       turnURLs,
				Username:   turnUsername,
				Credential: turnCredential,
			})
			flat = append(flat, turnURLs...)
		}
	}
	for _, extra := range s.cfg.ExtraICEServers {
		if extra == "" {
			continue
		}
		servers = append(servers, iceServer{URLs: []string{extra}})
		flat = append(flat, extra)
	}
	return servers, flat
}

func (s *Server) generateTurnCredentials(deviceID string) (string, string) {
	if s.turn == nil {
		return "", ""
	}
	return s.turn.GenerateCredentials(deviceID)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
