package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/tuiv2/shared" //nolint:typecheck
	"github.com/spf13/cobra"
)

type remoteLoginClient interface {
	Me(context.Context, string, string) (remoteLoginUser, error)
	Login(context.Context, string, string, string) (remoteLoginAuthResult, error)
	CreateDeviceCode(context.Context, string, string) (remoteDeviceCodeResult, error)
	PollDeviceCode(context.Context, string, string) (remoteLoginAuthResult, bool, error)
	DiscoverHubs(context.Context, string, string) ([]remoteLoginHub, error)
}

type remoteLoginUser struct {
	Email string `json:"email"`
}

type remoteLoginAuthResult struct {
	User         remoteLoginUser `json:"user"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
}

type remoteLoginHub struct {
	ID      string `json:"id"`
	HTTPURL string `json:"http_url"`
}

type remoteDeviceCodeResult struct {
	DeviceCode              string
	UserCode                string
	VerificationURIComplete string
	ExpiresAt               time.Time
	Interval                time.Duration
}

type remoteAuthRecord struct {
	ControlURL        string `json:"control_url"`
	HubURL            string `json:"hub_url,omitempty"`
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	MachinePrivateKey string `json:"machine_private_key,omitempty"`
	SavedAt           string `json:"saved_at"`
}

var (
	remoteLoginHTTPClient remoteLoginClient = controlPlaneLoginClient{}
	remoteAuthStorePath                     = defaultRemoteAuthStorePath
)

func remoteLoginCommand(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login this daemon to Web Control",
	}
	cmd.AddCommand(remoteLoginTokenCommand(configPath))
	cmd.AddCommand(remoteLoginPasswordCommand(configPath))
	cmd.AddCommand(remoteLoginDeviceCodeCommand(configPath))
	return cmd
}

func remoteLoginTokenCommand(configPath *string) *cobra.Command {
	var controlURL string
	var token string
	var tokenEnv string
	var tokenFile string
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Login with an existing Web Control access token",
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = strings.TrimSpace(controlURL)
			resolvedToken, err := resolveSecretFlag(token, tokenEnv, tokenFile)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(resolvedToken)
			if controlURL == "" || token == "" {
				return fmt.Errorf("control-url and token source are required")
			}
			if _, err := remoteLoginHTTPClient.Me(cmd.Context(), controlURL, token); err != nil {
				return err
			}
			return persistRemoteLogin(cmd, *configPath, remoteAuthRecord{ControlURL: controlURL, AccessToken: token})
		},
	}
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().StringVar(&token, "token", "", "Web Control access token")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable containing the Web Control access token")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "file containing the Web Control access token")
	return cmd
}

func remoteLoginPasswordCommand(configPath *string) *cobra.Command {
	var controlURL string
	var email string
	var password string
	var passwordEnv string
	var passwordFile string
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Login with Web Control email and password",
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = strings.TrimSpace(controlURL)
			email = strings.TrimSpace(email)
			resolvedPassword, err := resolveSecretFlag(password, passwordEnv, passwordFile)
			if err != nil {
				return err
			}
			password = resolvedPassword
			if controlURL == "" || email == "" || password == "" {
				return fmt.Errorf("control-url, email, and password source are required")
			}
			auth, err := remoteLoginHTTPClient.Login(cmd.Context(), controlURL, email, password)
			if err != nil {
				return err
			}
			return persistRemoteLogin(cmd, *configPath, remoteAuthRecord{
				ControlURL:   controlURL,
				AccessToken:  auth.AccessToken,
				RefreshToken: auth.RefreshToken,
			})
		},
	}
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().StringVar(&email, "email", "", "Web Control email")
	cmd.Flags().StringVar(&password, "password", "", "Web Control password")
	cmd.Flags().StringVar(&passwordEnv, "password-env", "", "environment variable containing the Web Control password")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "file containing the Web Control password")
	return cmd
}

func remoteLoginDeviceCodeCommand(configPath *string) *cobra.Command {
	var controlURL string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "device-code",
		Short: "Login with a Web Control device code",
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = strings.TrimSpace(controlURL)
			if controlURL == "" {
				return fmt.Errorf("control-url is required")
			}
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			created, err := remoteLoginHTTPClient.CreateDeviceCode(cmd.Context(), controlURL, "termx cli")
			if err != nil {
				return err
			}
			if created.VerificationURIComplete != "" {
				fmt.Fprintln(cmd.OutOrStdout(), created.VerificationURIComplete)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			interval := created.Interval
			if interval <= 0 {
				interval = 5 * time.Second
			}
			for {
				auth, ok, err := remoteLoginHTTPClient.PollDeviceCode(ctx, controlURL, created.DeviceCode)
				if err != nil {
					return err
				}
				if ok {
					return persistRemoteLogin(cmd, *configPath, remoteAuthRecord{
						ControlURL:   controlURL,
						AccessToken:  auth.AccessToken,
						RefreshToken: auth.RefreshToken,
					})
				}
				timer := time.NewTimer(interval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return ctx.Err()
				case <-timer.C:
				}
			}
		},
	}
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "device-code polling timeout")
	return cmd
}

func persistRemoteLogin(cmd *cobra.Command, configPath string, record remoteAuthRecord) error {
	hubs, err := remoteLoginHTTPClient.DiscoverHubs(cmd.Context(), record.ControlURL, record.AccessToken)
	if err == nil {
		for _, hub := range hubs {
			if strings.TrimSpace(hub.HTTPURL) != "" {
				record.HubURL = strings.TrimSpace(hub.HTTPURL)
				break
			}
		}
	}
	path, err := remoteAuthStorePath(configPath)
	if err != nil {
		return err
	}
	if err := saveRemoteAuthRecord(path, record); err != nil {
		return err
	}
	if err := ensureRemoteConfigBootstrap(configPath, record.ControlURL, record.HubURL, path); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "remote login saved")
	return nil
}

func resolveSecretFlag(raw string, envName string, filePath string) (string, error) {
	sources := 0
	if strings.TrimSpace(raw) != "" {
		sources++
	}
	if strings.TrimSpace(envName) != "" {
		sources++
	}
	if strings.TrimSpace(filePath) != "" {
		sources++
	}
	if sources > 1 {
		return "", fmt.Errorf("only one secret source may be set")
	}
	if strings.TrimSpace(envName) != "" {
		return strings.TrimSpace(os.Getenv(strings.TrimSpace(envName))), nil
	}
	if strings.TrimSpace(filePath) != "" {
		data, err := os.ReadFile(strings.TrimSpace(filePath))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return strings.TrimSpace(raw), nil
}

type controlPlaneLoginClient struct{}

func (controlPlaneLoginClient) Me(ctx context.Context, controlURL string, token string) (remoteLoginUser, error) {
	var out struct {
		User remoteLoginUser `json:"user"`
	}
	if err := controlJSON(ctx, http.MethodGet, strings.TrimRight(controlURL, "/")+"/api/v1/auth/me", nil, &out, token); err != nil {
		return remoteLoginUser{}, err
	}
	return out.User, nil
}

func (controlPlaneLoginClient) Login(ctx context.Context, controlURL string, email string, password string) (remoteLoginAuthResult, error) {
	var out remoteLoginAuthResult
	if err := controlJSON(ctx, http.MethodPost, strings.TrimRight(controlURL, "/")+"/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, &out, ""); err != nil {
		return remoteLoginAuthResult{}, err
	}
	return out, nil
}

func (controlPlaneLoginClient) CreateDeviceCode(ctx context.Context, controlURL string, clientName string) (remoteDeviceCodeResult, error) {
	var out struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := controlJSON(ctx, http.MethodPost, strings.TrimRight(controlURL, "/")+"/api/v1/auth/device-code", map[string]string{
		"client_name":      clientName,
		"verification_uri": strings.TrimRight(controlURL, "/") + "/device",
	}, &out, ""); err != nil {
		return remoteDeviceCodeResult{}, err
	}
	return remoteDeviceCodeResult{
		DeviceCode:              out.DeviceCode,
		UserCode:                out.UserCode,
		VerificationURIComplete: out.VerificationURIComplete,
		ExpiresAt:               time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
		Interval:                time.Duration(out.Interval) * time.Second,
	}, nil
}

func (controlPlaneLoginClient) PollDeviceCode(ctx context.Context, controlURL string, deviceCode string) (remoteLoginAuthResult, bool, error) {
	var out remoteLoginAuthResult
	err := controlJSON(ctx, http.MethodPost, strings.TrimRight(controlURL, "/")+"/api/v1/auth/device-code/token", map[string]string{
		"device_code": deviceCode,
	}, &out, "")
	if err == nil {
		return out, true, nil
	}
	if errors.Is(err, errAuthorizationPending) {
		return remoteLoginAuthResult{}, false, nil
	}
	return remoteLoginAuthResult{}, false, err
}

func (controlPlaneLoginClient) DiscoverHubs(ctx context.Context, controlURL string, token string) ([]remoteLoginHub, error) {
	var out struct {
		Hubs []remoteLoginHub `json:"hubs"`
	}
	if err := controlJSON(ctx, http.MethodGet, strings.TrimRight(controlURL, "/")+"/api/v1/hubs", nil, &out, token); err != nil {
		return nil, err
	}
	return out.Hubs, nil
}

var errAuthorizationPending = errors.New("authorization pending")

func controlJSON(ctx context.Context, method string, url string, body any, out any, bearer string) error {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		if payload.Error.Code == "authorization_pending" {
			return errAuthorizationPending
		}
		if payload.Error.Message != "" {
			return fmt.Errorf("%s", payload.Error.Message)
		}
		return fmt.Errorf("control request failed: %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func defaultRemoteAuthStorePath(configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return filepath.Join(filepath.Dir(configPath), "remote-auth.json"), nil
	}
	return filepath.Join(shared.StateDir(), "remote-auth.json"), nil
}

func saveRemoteAuthRecord(path string, record remoteAuthRecord) error {
	if strings.TrimSpace(record.MachinePrivateKey) != "" {
		return errors.New("machine private key must not be persisted")
	}
	record.ControlURL = strings.TrimSpace(record.ControlURL)
	record.HubURL = strings.TrimSpace(record.HubURL)
	record.AccessToken = strings.TrimSpace(record.AccessToken)
	record.RefreshToken = strings.TrimSpace(record.RefreshToken)
	if record.ControlURL == "" || record.AccessToken == "" {
		return errors.New("control url and access token are required")
	}
	if record.SavedAt == "" {
		record.SavedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func loadRemoteAuthRecord(path string) (remoteAuthRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return remoteAuthRecord{}, err
	}
	var record remoteAuthRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return remoteAuthRecord{}, err
	}
	if strings.TrimSpace(record.MachinePrivateKey) != "" {
		return remoteAuthRecord{}, errors.New("remote auth store contains forbidden machine private key")
	}
	return record, nil
}

func ensureRemoteConfigBootstrap(configPath string, controlURL string, hubURL string, authStorePath string) error {
	if strings.TrimSpace(configPath) == "" {
		configPath = shared.DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("remote:\n  enabled: true\n  controlURL: %s\n", strings.TrimSpace(controlURL))
	if strings.TrimSpace(hubURL) != "" {
		content += fmt.Sprintf("  hubURL: %s\n", strings.TrimSpace(hubURL))
	}
	content += fmt.Sprintf("  authStore: %s\n", authStorePath)
	return os.WriteFile(configPath, []byte(content), 0o600)
}
