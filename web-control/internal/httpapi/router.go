package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/connect"
	"github.com/lozzow/termx/web-control/internal/hubregistry"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/rendezvous"
)

const defaultServiceName = "termx-web-control"

type Config struct {
	ServiceName           string
	Version               string
	Accounts              *account.Service
	Machines              *machines.Service
	Connect               *connect.Service
	Rendezvous            *rendezvous.Service
	HubRegistry           *hubregistry.Service
	HubSharedSecret       string
	MaxPublicP2PBodyBytes int64
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
	if cfg.MaxPublicP2PBodyBytes <= 0 {
		cfg.MaxPublicP2PBodyBytes = 64 * 1024
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
	mux.HandleFunc("POST /api/v1/auth/register", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Accounts == nil {
			writeError(w, http.StatusServiceUnavailable, "account_service_unavailable", "account service is not configured")
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		result, err := cfg.Accounts.Register(r.Context(), account.RegisterInput{Email: req.Email, Password: req.Password})
		if err != nil {
			writeError(w, http.StatusBadRequest, "register_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Accounts == nil {
			writeError(w, http.StatusServiceUnavailable, "account_service_unavailable", "account service is not configured")
			return
		}
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		result, err := cfg.Accounts.Login(r.Context(), account.LoginInput{Email: req.Email, Password: req.Password})
		if err != nil {
			writeError(w, http.StatusUnauthorized, "login_failed", "invalid email or password")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Accounts == nil {
			writeError(w, http.StatusServiceUnavailable, "account_service_unavailable", "account service is not configured")
			return
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		result, err := cfg.Accounts.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "refresh_failed", "invalid refresh token")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Accounts == nil {
			writeError(w, http.StatusServiceUnavailable, "account_service_unavailable", "account service is not configured")
			return
		}
		token, err := bearerToken(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "missing_token", "bearer token is required")
			return
		}
		result, err := cfg.Accounts.Me(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_token", "bearer token is invalid")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /api/v1/agent/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		var req struct {
			MachineID         string `json:"machine_id"`
			MachinePublicKey  string `json:"machine_public_key"`
			MachinePrivateKey string `json:"machine_private_key"`
			DisplayName       string `json:"display_name"`
			Hostname          string `json:"hostname"`
			Platform          string `json:"platform"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		result, err := cfg.Machines.Bootstrap(r.Context(), machines.BootstrapInput{
			MachineID:         req.MachineID,
			MachinePublicKey:  req.MachinePublicKey,
			MachinePrivateKey: req.MachinePrivateKey,
			DisplayName:       req.DisplayName,
			Hostname:          req.Hostname,
			Platform:          req.Platform,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "bootstrap_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	mux.HandleFunc("POST /api/devices/register", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		var req struct {
			DeviceID          string `json:"deviceId"`
			MachinePublicKey  string `json:"machinePublicKey"`
			MachinePrivateKey string `json:"machinePrivateKey"`
			DisplayName       string `json:"displayName"`
			Hostname          string `json:"hostname"`
			Platform          string `json:"platform"`
			Terminals         []struct {
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				Command []string `json:"command"`
				Cols    int      `json:"cols"`
				Rows    int      `json:"rows"`
				State   string   `json:"state"`
			} `json:"terminals"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxPublicP2PBodyBytes)
		if err := decodeJSON(r, &req); err != nil {
			if requestBodyTooLarge(err) {
				writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		terminals := make([]machines.RemoteTerminalInput, 0, len(req.Terminals))
		for _, terminal := range req.Terminals {
			terminals = append(terminals, machines.RemoteTerminalInput{
				ID:      terminal.ID,
				Name:    terminal.Name,
				Command: terminal.Command,
				Cols:    terminal.Cols,
				Rows:    terminal.Rows,
				State:   terminal.State,
			})
		}
		machine, err := cfg.Machines.RegisterRemoteDevice(r.Context(), machines.RegisterRemoteDeviceInput{
			UserID:            user.User.ID,
			MachineID:         req.DeviceID,
			MachinePublicKey:  req.MachinePublicKey,
			MachinePrivateKey: req.MachinePrivateKey,
			DisplayName:       req.DisplayName,
			Hostname:          req.Hostname,
			Platform:          req.Platform,
			Terminals:         terminals,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "register_device_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"device": machine})
	})
	mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		list, err := cfg.Machines.ListMachines(r.Context(), user.User.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_devices_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"devices": list})
	})
	mux.HandleFunc("GET /api/terminals", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		machinesList, err := cfg.Machines.ListMachines(r.Context(), user.User.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_machines_failed", err.Error())
			return
		}
		var terminals []machines.RemoteTerminal
		for _, machine := range machinesList {
			items, err := cfg.Machines.ListRemoteTerminals(r.Context(), user.User.ID, machine.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "list_terminals_failed", err.Error())
				return
			}
			terminals = append(terminals, items...)
		}
		writeJSON(w, http.StatusOK, map[string]any{"terminals": terminals})
	})
	mux.HandleFunc("POST /api/v1/machines/claim", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		var req struct {
			MachineID  string `json:"machine_id"`
			ClaimToken string `json:"claim_token"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		machine, err := cfg.Machines.Claim(r.Context(), machines.ClaimInput{UserID: user.User.ID, MachineID: req.MachineID, ClaimToken: req.ClaimToken})
		if err != nil {
			writeError(w, http.StatusForbidden, "claim_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, machine)
	})
	mux.HandleFunc("GET /api/v1/machines", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		list, err := cfg.Machines.ListMachines(r.Context(), user.User.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_machines_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"machines": list})
	})
	mux.HandleFunc("GET /api/v1/machines/{machine_id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		machine, err := cfg.Machines.GetMachine(r.Context(), user.User.ID, r.PathValue("machine_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "machine_not_found", "machine not found")
			return
		}
		writeJSON(w, http.StatusOK, machine)
	})
	mux.HandleFunc("POST /api/v1/machines/{machine_id}/app-certificates", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		var req struct {
			AppPublicKey         string `json:"app_public_key"`
			AppPrivateKey        string `json:"app_private_key"`
			AppDisplayName       string `json:"app_display_name"`
			CertificatePayload   string `json:"certificate_payload"`
			CertificateSignature string `json:"certificate_signature"`
			ExpiresAt            string `json:"expires_at"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_expiry", "expires_at must be RFC3339")
			return
		}
		cert, err := cfg.Machines.RegisterAppCertificate(r.Context(), machines.RegisterAppCertificateInput{
			UserID:               user.User.ID,
			MachineID:            r.PathValue("machine_id"),
			AppPublicKey:         req.AppPublicKey,
			AppPrivateKey:        req.AppPrivateKey,
			AppDisplayName:       req.AppDisplayName,
			CertificatePayload:   req.CertificatePayload,
			CertificateSignature: req.CertificateSignature,
			ExpiresAt:            expiresAt,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "register_certificate_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, cert)
	})
	mux.HandleFunc("GET /api/v1/machines/{machine_id}/app-certificates", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		certs, err := cfg.Machines.ListAppCertificates(r.Context(), user.User.ID, r.PathValue("machine_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "machine_not_found", "machine not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"app_certificates": certs})
	})
	mux.HandleFunc("DELETE /api/v1/machines/{machine_id}/app-certificates/{cert_id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Machines == nil {
			writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
			return
		}
		if err := cfg.Machines.RevokeAppCertificate(r.Context(), user.User.ID, r.PathValue("machine_id"), r.PathValue("cert_id")); err != nil {
			writeError(w, http.StatusNotFound, "certificate_not_found", "certificate not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/managed/connect-tickets", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Connect == nil {
			writeError(w, http.StatusServiceUnavailable, "connect_service_unavailable", "connect service is not configured")
			return
		}
		var req struct {
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
			TTLSeconds int64  `json:"ttl_seconds"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		if req.TTLSeconds > int64(math.MaxInt64/int64(time.Second)) {
			writeError(w, http.StatusBadRequest, "invalid_ttl", "ttl_seconds is too large")
			return
		}
		ticket, err := cfg.Connect.CreateManagedTicket(r.Context(), connect.CreateManagedTicketInput{
			UserID:     user.User.ID,
			MachineID:  req.MachineID,
			TerminalID: req.TerminalID,
			TTL:        time.Duration(req.TTLSeconds) * time.Second,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "create_managed_ticket_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, ticket)
	})
	mux.HandleFunc("POST /api/v1/hub/managed-tickets/check", func(w http.ResponseWriter, r *http.Request) {
		handleHubManagedTicket(w, r, cfg, false)
	})
	mux.HandleFunc("POST /api/v1/hub/managed-tickets/consume", func(w http.ResponseWriter, r *http.Request) {
		handleHubManagedTicket(w, r, cfg, true)
	})
	mux.HandleFunc("POST /api/v1/hub/agents/verify-registration", func(w http.ResponseWriter, r *http.Request) {
		handleHubAgentRegistration(w, r, cfg)
	})
	mux.HandleFunc("GET /api/v1/hubs", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authenticatedUser(w, r, cfg.Accounts); !ok {
			return
		}
		if cfg.HubRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "hub_registry_unavailable", "hub registry service is not configured")
			return
		}
		hubs, err := cfg.HubRegistry.DiscoverHubs(r.Context(), hubregistry.DiscoverHubsInput{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "discover_hubs_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hubs": hubs})
	})
	mux.HandleFunc("POST /api/v1/hub/report", func(w http.ResponseWriter, r *http.Request) {
		handleHubReport(w, r, cfg)
	})
	mux.HandleFunc("POST /api/v1/hub/agents/policy", func(w http.ResponseWriter, r *http.Request) {
		handleHubAgentPolicy(w, r, cfg)
	})
	mux.HandleFunc("POST /api/v1/machines/{machine_id}/agents/{agent_id}/force-offline", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.HubRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "hub_registry_unavailable", "hub registry service is not configured")
			return
		}
		var req struct {
			Reason string `json:"reason"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		if err := cfg.HubRegistry.ForceOfflineAgent(r.Context(), hubregistry.ForceOfflineInput{
			UserID:    user.User.ID,
			MachineID: r.PathValue("machine_id"),
			AgentID:   r.PathValue("agent_id"),
			Reason:    req.Reason,
		}); err != nil {
			status := http.StatusForbidden
			if errors.Is(err, hubregistry.ErrMachineNotOwned) {
				status = http.StatusNotFound
			}
			writeError(w, status, "force_offline_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/v1/public-p2p/channels", func(w http.ResponseWriter, r *http.Request) {
		user, ok := authenticatedUser(w, r, cfg.Accounts)
		if !ok {
			return
		}
		if cfg.Rendezvous == nil {
			writeError(w, http.StatusServiceUnavailable, "rendezvous_service_unavailable", "rendezvous service is not configured")
			return
		}
		var req struct {
			MachineID  string `json:"machine_id"`
			TerminalID string `json:"terminal_id"`
			TTLSeconds int64  `json:"ttl_seconds"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxPublicP2PBodyBytes)
		if err := decodeJSON(r, &req); err != nil {
			if requestBodyTooLarge(err) {
				writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
			return
		}
		result, err := cfg.Rendezvous.CreateChannel(r.Context(), rendezvous.CreateChannelInput{
			UserID:     user.User.ID,
			MachineID:  req.MachineID,
			TerminalID: req.TerminalID,
			TTL:        time.Duration(req.TTLSeconds) * time.Second,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "create_rendezvous_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, publicP2PChannelResponse(result))
	})
	mux.HandleFunc("POST /api/v1/public-p2p/channels/{channel_id}/offer", func(w http.ResponseWriter, r *http.Request) {
		handleRendezvousMessage(w, r, cfg.Rendezvous, cfg.MaxPublicP2PBodyBytes, rendezvous.MessageOffer)
	})
	mux.HandleFunc("POST /api/v1/public-p2p/channels/{channel_id}/answer", func(w http.ResponseWriter, r *http.Request) {
		handleRendezvousMessage(w, r, cfg.Rendezvous, cfg.MaxPublicP2PBodyBytes, rendezvous.MessageAnswer)
	})
	mux.HandleFunc("POST /api/v1/public-p2p/channels/{channel_id}/candidate", func(w http.ResponseWriter, r *http.Request) {
		handleRendezvousMessage(w, r, cfg.Rendezvous, cfg.MaxPublicP2PBodyBytes, rendezvous.MessageCandidate)
	})
	mux.HandleFunc("GET /api/v1/public-p2p/channels/{channel_id}/events", func(w http.ResponseWriter, r *http.Request) {
		if cfg.Rendezvous == nil {
			writeError(w, http.StatusServiceUnavailable, "rendezvous_service_unavailable", "rendezvous service is not configured")
			return
		}
		secret, ok := rendezvousSecretFromRequest(r, r.PathValue("channel_id"))
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing_rendezvous_secret", "rendezvous channel secret header is required")
			return
		}
		messages, err := cfg.Rendezvous.ListMessages(r.Context(), rendezvous.ListMessagesInput{
			ChannelID: r.PathValue("channel_id"),
			Secret:    secret,
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "list_rendezvous_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": rendezvousMessageResponses(messages)})
	})
	return mux
}

func handleRendezvousMessage(w http.ResponseWriter, r *http.Request, service *rendezvous.Service, maxBodyBytes int64, messageType string) {
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "rendezvous_service_unavailable", "rendezvous service is not configured")
		return
	}
	var req struct {
		ChannelSecret  string          `json:"channel_secret"`
		Payload        json.RawMessage `json:"payload"`
		Offer          json.RawMessage `json:"offer"`
		Answer         json.RawMessage `json:"answer"`
		Candidate      json.RawMessage `json:"candidate"`
		AppCertificate json.RawMessage `json:"app_certificate"`
		AppPublicKey   string          `json:"app_public_key"`
		Signature      json.RawMessage `json:"signature"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := decodeJSON(r, &req); err != nil {
		if requestBodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	payload := req.Payload
	if len(payload) == 0 {
		switch messageType {
		case rendezvous.MessageOffer:
			payload = envelopePayload(messageType, req.Offer, req.AppCertificate, "", req.Signature)
		case rendezvous.MessageAnswer:
			payload = envelopePayload(messageType, req.Answer, nil, "", nil)
		case rendezvous.MessageCandidate:
			payload = envelopePayload(messageType, req.Candidate, nil, req.AppPublicKey, nil)
		}
	}
	if len(payload) == 0 {
		writeError(w, http.StatusBadRequest, "missing_payload", "payload is required")
		return
	}
	if err := service.Send(r.Context(), rendezvous.SendMessageInput{
		ChannelID: r.PathValue("channel_id"),
		Secret:    req.ChannelSecret,
		Type:      messageType,
		Payload:   string(payload),
	}); err != nil {
		writeError(w, http.StatusForbidden, "send_rendezvous_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func handleHubReport(w http.ResponseWriter, r *http.Request, cfg Config) {
	if !authenticatedHub(w, r, cfg) {
		return
	}
	if cfg.HubRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "hub_registry_unavailable", "hub registry service is not configured")
		return
	}
	var req struct {
		HubID      string          `json:"hub_id"`
		Region     string          `json:"region"`
		HTTPURL    string          `json:"http_url"`
		Status     string          `json:"status"`
		Capacity   int             `json:"capacity"`
		HealthJSON json.RawMessage `json:"health_json"`
		TTLSeconds int64           `json:"ttl_seconds"`
		Agents     []struct {
			MachineID     string `json:"machine_id"`
			AgentID       string `json:"agent_id"`
			Status        string `json:"status"`
			TerminalCount int    `json:"terminal_count"`
		} `json:"agents"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxPublicP2PBodyBytes)
	if err := decodeJSON(r, &req); err != nil {
		if requestBodyTooLarge(err) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	if req.TTLSeconds > int64(math.MaxInt64/int64(time.Second)) {
		writeError(w, http.StatusBadRequest, "invalid_ttl", "ttl_seconds is too large")
		return
	}
	agents := make([]hubregistry.AgentReport, 0, len(req.Agents))
	for _, agent := range req.Agents {
		agents = append(agents, hubregistry.AgentReport{
			MachineID:     agent.MachineID,
			AgentID:       agent.AgentID,
			Status:        agent.Status,
			TerminalCount: agent.TerminalCount,
		})
	}
	result, err := cfg.HubRegistry.ReportHub(r.Context(), hubregistry.ReportHubInput{
		HubID:    req.HubID,
		Region:   req.Region,
		HTTPURL:  req.HTTPURL,
		Status:   req.Status,
		Capacity: req.Capacity,
		Health:   string(req.HealthJSON),
		TTL:      time.Duration(req.TTLSeconds) * time.Second,
		Agents:   agents,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "hub_report_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hub":            result.Hub,
		"agent_policies": result.AgentPolicies,
	})
}

func handleHubManagedTicket(w http.ResponseWriter, r *http.Request, cfg Config, consume bool) {
	if !authenticatedHub(w, r, cfg) {
		return
	}
	if cfg.Connect == nil {
		writeError(w, http.StatusServiceUnavailable, "connect_service_unavailable", "connect service is not configured")
		return
	}
	var req struct {
		TicketID   string `json:"ticket_id"`
		MachineID  string `json:"machine_id"`
		TerminalID string `json:"terminal_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	input := connect.VerifyManagedTicketInput{
		TicketID:   req.TicketID,
		MachineID:  req.MachineID,
		TerminalID: req.TerminalID,
	}
	var (
		ticket connect.ManagedTicket
		err    error
	)
	if consume {
		ticket, err = cfg.Connect.VerifyManagedTicket(r.Context(), input)
	} else {
		ticket, err = cfg.Connect.CheckManagedTicket(r.Context(), input)
	}
	if err != nil {
		writeError(w, http.StatusForbidden, "managed_ticket_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": ticket})
}

func handleHubAgentRegistration(w http.ResponseWriter, r *http.Request, cfg Config) {
	if !authenticatedHub(w, r, cfg) {
		return
	}
	if cfg.Machines == nil {
		writeError(w, http.StatusServiceUnavailable, "machine_service_unavailable", "machine service is not configured")
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		AgentID   string `json:"agent_id"`
		Signature struct {
			Algorithm string `json:"algorithm"`
			Nonce     string `json:"nonce"`
			Timestamp int64  `json:"timestamp"`
			Value     string `json:"value"`
		} `json:"signature"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	err := cfg.Machines.VerifyAgentRegistration(r.Context(), machines.VerifyAgentRegistrationInput{
		MachineID: req.MachineID,
		AgentID:   req.AgentID,
		Signature: machines.AgentRegistrationSignature{
			Algorithm: req.Signature.Algorithm,
			Nonce:     req.Signature.Nonce,
			Timestamp: req.Signature.Timestamp,
			Value:     req.Signature.Value,
		},
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "agent_registration_rejected", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleHubAgentPolicy(w http.ResponseWriter, r *http.Request, cfg Config) {
	if !authenticatedHub(w, r, cfg) {
		return
	}
	if cfg.HubRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "hub_registry_unavailable", "hub registry service is not configured")
		return
	}
	var req struct {
		MachineID string `json:"machine_id"`
		AgentID   string `json:"agent_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request body")
		return
	}
	policy, err := cfg.HubRegistry.GetAgentPolicy(r.Context(), hubregistry.AgentPolicyInput{
		MachineID: req.MachineID,
		AgentID:   req.AgentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "agent_policy_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
}

func authenticatedHub(w http.ResponseWriter, r *http.Request, cfg Config) bool {
	if strings.TrimSpace(cfg.HubSharedSecret) == "" {
		writeError(w, http.StatusServiceUnavailable, "hub_auth_unavailable", "hub shared secret is not configured")
		return false
	}
	if r.Header.Get("X-TermX-Hub-Secret") != cfg.HubSharedSecret {
		writeError(w, http.StatusUnauthorized, "invalid_hub_secret", "hub authentication failed")
		return false
	}
	return true
}

func envelopePayload(messageType string, nested json.RawMessage, appCertificate json.RawMessage, appPublicKey string, signature json.RawMessage) json.RawMessage {
	if len(nested) == 0 {
		return nil
	}
	key := messageType
	data := map[string]json.RawMessage{key: nested}
	if len(appCertificate) > 0 {
		data["app_certificate"] = appCertificate
	}
	if strings.TrimSpace(appPublicKey) != "" {
		encodedKey, err := json.Marshal(strings.TrimSpace(appPublicKey))
		if err != nil {
			return nil
		}
		data["app_public_key"] = encodedKey
	}
	if len(signature) > 0 {
		data["signature"] = signature
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return encoded
}

func publicP2PChannelResponse(channel rendezvous.Channel) map[string]any {
	return map[string]any{
		"id":                  channel.ID,
		"secret":              channel.Secret,
		"path":                channel.Path,
		"machine_id":          channel.MachineID,
		"terminal_id":         channel.TerminalID,
		"expires_at":          channel.ExpiresAt,
		"ice_servers":         channel.ICEServers,
		"channel_id":          channel.ID,
		"channel_secret":      channel.Secret,
		"public_stun_servers": publicSTUNServers(channel.ICEServers),
	}
}

func publicSTUNServers(servers []rendezvous.ICEServer) []string {
	result := make([]string, 0, len(servers))
	for _, server := range servers {
		result = append(result, server.URL)
	}
	return result
}

func rendezvousSecretFromRequest(r *http.Request, channelID string) (string, bool) {
	if secret := strings.TrimSpace(r.Header.Get("X-TermX-Rendezvous-Secret")); secret != "" {
		return secret, true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	value, ok := strings.CutPrefix(auth, "Rendezvous ")
	if !ok {
		return "", false
	}
	id, secret, ok := strings.Cut(value, ":")
	if !ok || strings.TrimSpace(id) != channelID || strings.TrimSpace(secret) == "" {
		return "", false
	}
	return strings.TrimSpace(secret), true
}

func rendezvousMessageResponses(messages []rendezvous.Message) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]any{
			"id":         msg.ID,
			"type":       msg.Type,
			"payload":    json.RawMessage(msg.Payload),
			"created_at": msg.CreatedAt,
		})
	}
	return result
}

func authenticatedUser(w http.ResponseWriter, r *http.Request, accounts *account.Service) (account.AuthResult, bool) {
	if accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "account_service_unavailable", "account service is not configured")
		return account.AuthResult{}, false
	}
	token, err := bearerToken(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_token", "bearer token is required")
		return account.AuthResult{}, false
	}
	result, err := accounts.Me(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "bearer token is invalid")
		return account.AuthResult{}, false
	}
	return result, true
}

func decodeJSON(r *http.Request, out any) error {
	if r.Body == nil {
		return errors.New("missing body")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func bearerToken(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", errors.New("missing authorization")
	}
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" {
		return "", errors.New("invalid authorization")
	}
	return strings.TrimSpace(token), nil
}

func requestBodyTooLarge(err error) bool {
	return err != nil && strings.Contains(err.Error(), "http: request body too large")
}
