package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lozzow/termx/tuiv2/shared"
)

func TestLoginCommandStoresTokensInConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode login body: %v", err)
		}
		if body["username"] != "alice" || body["password"] != "secret" {
			t.Fatalf("unexpected login body: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":         "access-1",
			"refresh_token": "refresh-1",
			"user": map[string]string{
				"id":       "user-1",
				"username": "alice",
				"role":     "user",
			},
		})
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	out := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetArgs([]string{
		"--config", configPath,
		"login",
		"--server", server.URL,
		"--username", "alice",
		"--password-stdin",
	})
	cmd.SetIn(bytes.NewBufferString("secret\n"))
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := shared.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Auth.ServerURL != server.URL || cfg.Auth.AccessToken != "access-1" || cfg.Auth.RefreshToken != "refresh-1" {
		t.Fatalf("unexpected saved auth config: %#v", cfg.Auth)
	}
	if cfg.Auth.UserID != "user-1" || cfg.Auth.Username != "alice" {
		t.Fatalf("unexpected saved user info: %#v", cfg.Auth)
	}
	if got := strings.TrimSpace(out.String()); got != "alice\tuser-1\t"+server.URL {
		t.Fatalf("unexpected login output: %q", got)
	}
}

func TestWhoamiCommandRefreshesExpiredAccessToken(t *testing.T) {
	var meCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/me":
			meCalls++
			if meCalls == 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer access-2" {
				t.Fatalf("unexpected whoami auth header after refresh: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]string{
					"id":       "user-1",
					"username": "alice",
					"role":     "user",
				},
			})
		case "/api/auth/refresh":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode refresh body: %v", err)
			}
			if body["refresh_token"] != "refresh-1" {
				t.Fatalf("unexpected refresh body: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":         "access-2",
				"refresh_token": "refresh-2",
				"user": map[string]string{
					"id":       "user-1",
					"username": "alice",
					"role":     "user",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	initial := shared.DefaultConfig()
	initial.ConfigPath = configPath
	initial.Auth = shared.AuthConfig{
		ServerURL:    server.URL,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		UserID:       "user-1",
		Username:     "alice",
	}
	if err := shared.SaveConfig(configPath, initial); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	out := &bytes.Buffer{}
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "whoami"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(out)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := shared.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Auth.AccessToken != "access-2" || cfg.Auth.RefreshToken != "refresh-2" {
		t.Fatalf("expected refreshed tokens to be persisted, got %#v", cfg.Auth)
	}
	if got := strings.TrimSpace(out.String()); got != "alice\tuser-1\t"+server.URL {
		t.Fatalf("unexpected whoami output: %q", got)
	}
}

func TestLogoutCommandClearsTokensButKeepsServerURL(t *testing.T) {
	var logoutBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/logout" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&logoutBody); err != nil {
			t.Fatalf("decode logout body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	initial := shared.DefaultConfig()
	initial.ConfigPath = configPath
	initial.Auth = shared.AuthConfig{
		ServerURL:    server.URL,
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		UserID:       "user-1",
		Username:     "alice",
	}
	if err := shared.SaveConfig(configPath, initial); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "logout"})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	cfg, err := shared.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Auth.ServerURL != server.URL {
		t.Fatalf("expected server url to remain configured, got %#v", cfg.Auth)
	}
	if cfg.Auth.AccessToken != "" || cfg.Auth.RefreshToken != "" || cfg.Auth.UserID != "" || cfg.Auth.Username != "" {
		t.Fatalf("expected logout to clear stored credentials, got %#v", cfg.Auth)
	}
	if logoutBody["refresh_token"] != "refresh-1" {
		t.Fatalf("unexpected logout body: %#v", logoutBody)
	}
}
