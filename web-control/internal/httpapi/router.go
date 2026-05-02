package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/machines"
)

const defaultServiceName = "termx-web-control"

type Config struct {
	ServiceName string
	Version     string
	Accounts    *account.Service
	Machines    *machines.Service
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
	return mux
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
