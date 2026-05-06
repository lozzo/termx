package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func TestRemoteLoginParentDefaultsToBrowserLogin(t *testing.T) {
	oldClient := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLoginHTTPClient = oldClient
		remoteAuthStorePath = oldStore
	})
	authStore := filepath.Join(t.TempDir(), "remote-auth.json")
	remoteAuthStorePath = func(configPath string) (string, error) { return authStore, nil }
	var createdForControl string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		createBrowserLoginFunc: func(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
			createdForControl = controlURL
			return remoteBrowserLoginResult{
				BrowserLoginCode:        "browser-parent",
				VerificationURIComplete: "https://control.example.test/device?user_code=PARENT",
				ExpiresAt:               time.Now().Add(time.Minute),
				Interval:                time.Nanosecond,
			}, nil
		},
		pollBrowserLoginFunc: func(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
			return remoteLoginAuthResult{
				AccessToken:  "parent-access",
				RefreshToken: "parent-refresh",
				User:         remoteLoginUser{Email: "parent@example.com"},
			}, true, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "remote", "login", "--server", "https://control.example.test", "--no-browser", "--timeout", "100ms"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("parent login returned error: %v", err)
	}
	if createdForControl != "https://control.example.test" {
		t.Fatalf("expected parent login to start browser flow for control URL, got %q", createdForControl)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load parent login config: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "parent-access" {
		t.Fatalf("unexpected parent login config: %#v", cfg)
	}
}

func TestRemoteLoginTokenPersistsBootstrapOutsideConfigFile(t *testing.T) {
	oldClient := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLoginHTTPClient = oldClient
		remoteAuthStorePath = oldStore
	})
	remoteAuthStorePath = func(configPath string) (string, error) {
		return filepath.Join(t.TempDir(), "remote-auth.json"), nil
	}
	var validatedToken string
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		meFunc: func(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
			validatedToken = token
			return remoteLoginUser{Email: "cli-token@example.com"}, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("access-secret\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	cmd.SetArgs([]string{"--config", configPath, "remote", "login", "token", "--server", "https://control.example.test", "--token-file", tokenPath})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if validatedToken != "access-secret" {
		t.Fatalf("expected token to be validated through control, got %q", validatedToken)
	}
	if data, err := os.ReadFile(configPath); err == nil && strings.Contains(string(data), "access-secret") {
		t.Fatal("raw connection key was written to termx config")
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("remoteConfigFromFileAndEnv returned error: %v", err)
	}
	if !cfg.Enabled || cfg.ControlURL != "https://control.example.test" || cfg.AccessToken != "access-secret" ||
		cfg.HubURL != "" {
		t.Fatalf("unexpected loaded remote config after login: %#v", cfg)
	}
}

func TestRemoteLoginPasswordAndBrowserPersistKeys(t *testing.T) {
	oldClient := remoteLoginHTTPClient
	oldStore := remoteAuthStorePath
	t.Cleanup(func() {
		remoteLoginHTTPClient = oldClient
		remoteAuthStorePath = oldStore
	})
	authStore := filepath.Join(t.TempDir(), "remote-auth.json")
	remoteAuthStorePath = func(configPath string) (string, error) { return authStore, nil }
	var loginEmail string
	var pollCalls int
	remoteLoginHTTPClient = remoteLoginHTTPClientFunc{
		loginFunc: func(ctx context.Context, controlURL string, email string, password string) (remoteLoginAuthResult, error) {
			loginEmail = email
			if password != "valid password" {
				t.Fatalf("unexpected password %q", password)
			}
			return remoteLoginAuthResult{AccessToken: "password-access", RefreshToken: "password-refresh", User: remoteLoginUser{Email: email}}, nil
		},
		createBrowserLoginFunc: func(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
			return remoteBrowserLoginResult{
				BrowserLoginCode:        "browser-1",
				UserCode:                "USER-CODE",
				VerificationURIComplete: "https://control.example.test/device?user_code=USER-CODE",
				ExpiresAt:               time.Now().Add(time.Minute),
				Interval:                time.Nanosecond,
			}, nil
		},
		pollBrowserLoginFunc: func(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
			pollCalls++
			if pollCalls == 1 {
				return remoteLoginAuthResult{}, false, nil
			}
			return remoteLoginAuthResult{AccessToken: "device-access", RefreshToken: "device-refresh", User: remoteLoginUser{Email: "device@example.com"}}, true, nil
		},
	}

	configPath := filepath.Join(t.TempDir(), "termx.yaml")
	cmd := newRootCmd()
	t.Setenv("TERMX_TEST_PASSWORD", "valid password")
	cmd.SetArgs([]string{"--config", configPath, "remote", "login", "password", "--server", "https://control.example.test", "--email", "cli@example.com", "--password-env", "TERMX_TEST_PASSWORD"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("password login returned error: %v", err)
	}
	if loginEmail != "cli@example.com" {
		t.Fatalf("unexpected login email %q", loginEmail)
	}
	cfg, err := remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load password login config: %v", err)
	}
	if cfg.AccessToken != "password-access" {
		t.Fatalf("expected password connection key in auth store, got %#v", cfg)
	}

	cmd = newRootCmd()
	cmd.SetArgs([]string{"--config", configPath, "remote", "login", "browser", "--server", "https://control.example.test", "--timeout", "100ms"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("browser login returned error: %v", err)
	}
	cfg, err = remoteConfigFromFileAndEnv(configPath)
	if err != nil {
		t.Fatalf("load browser config: %v", err)
	}
	if cfg.AccessToken != "device-access" {
		t.Fatalf("expected browser connection key in auth store, got %#v", cfg)
	}
	var stored struct {
		RefreshToken string `json:"refresh_token"`
	}
	if data, err := os.ReadFile(authStore); err != nil {
		t.Fatalf("read auth store: %v", err)
	} else if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("decode auth store: %v", err)
	}
	if stored.RefreshToken != "device-refresh" {
		t.Fatalf("expected refresh token to rotate in auth store, got %q", stored.RefreshToken)
	}
}

func TestRemoteLoginDoesNotPersistMachinePrivateKey(t *testing.T) {
	record := remoteAuthRecord{
		ControlURL:        "https://control.example.test",
		AccessToken:       "access-secret",
		RefreshToken:      "refresh-secret",
		MachinePrivateKey: "must-not-persist",
	}
	path := filepath.Join(t.TempDir(), "remote-auth.json")
	if err := saveRemoteAuthRecord(path, record); err == nil {
		t.Fatal("expected private-key-shaped auth record to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("private key rejection still created auth store: %v", err)
	}
}

type remoteLoginHTTPClientFunc struct {
	meFunc                 func(context.Context, string, string) (remoteLoginUser, error)
	loginFunc              func(context.Context, string, string, string) (remoteLoginAuthResult, error)
	createBrowserLoginFunc func(context.Context, string, string) (remoteBrowserLoginResult, error)
	pollBrowserLoginFunc   func(context.Context, string, string) (remoteLoginAuthResult, bool, error)
}

func (f remoteLoginHTTPClientFunc) Me(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
	if f.meFunc == nil {
		return remoteLoginUser{}, nil
	}
	return f.meFunc(ctx, controlURL, token)
}

func (f remoteLoginHTTPClientFunc) Login(ctx context.Context, controlURL string, email string, password string) (remoteLoginAuthResult, error) {
	if f.loginFunc == nil {
		return remoteLoginAuthResult{}, nil
	}
	return f.loginFunc(ctx, controlURL, email, password)
}

func (f remoteLoginHTTPClientFunc) CreateBrowserLogin(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
	if f.createBrowserLoginFunc == nil {
		return remoteBrowserLoginResult{}, nil
	}
	return f.createBrowserLoginFunc(ctx, controlURL, clientName)
}

func (f remoteLoginHTTPClientFunc) PollBrowserLogin(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
	if f.pollBrowserLoginFunc == nil {
		return remoteLoginAuthResult{}, false, nil
	}
	return f.pollBrowserLoginFunc(ctx, controlURL, browserLoginCode)
}

var _ = remoteprotocol.Config{}
