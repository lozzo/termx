package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	CreateBrowserLogin(context.Context, string, string) (remoteBrowserLoginResult, error)
	PollBrowserLogin(context.Context, string, string) (remoteLoginAuthResult, bool, error)
}

type remoteLoginUser struct {
	Email string `json:"email"`
}

type remoteLoginAuthResult struct {
	User         remoteLoginUser `json:"user"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
}

type remoteBrowserLoginResult struct {
	BrowserLoginCode        string
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
	var serverURL string
	var controlURL string
	var timeout time.Duration
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login this computer to Web Control",
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = firstNonEmpty(serverURL, controlURL)
			if controlURL == "" {
				return fmt.Errorf("--server is required")
			}
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			created, err := remoteLoginHTTPClient.CreateBrowserLogin(cmd.Context(), controlURL, "termx cli")
			if err != nil {
				return err
			}
			auth, err := completeRemoteBrowserLogin(cmd, controlURL, created, timeout, !noBrowser, cmd.OutOrStdout())
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
	cmd.Flags().StringVar(&serverURL, "server", "", "Web Control URL")
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "browser login timeout")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the login URL without opening a browser")
	_ = cmd.Flags().MarkHidden("control-url")
	cmd.AddCommand(remoteLoginTokenCommand(configPath))
	cmd.AddCommand(remoteLoginPasswordCommand(configPath))
	cmd.AddCommand(remoteLoginBrowserCommand(configPath))
	return cmd
}

func remoteLoginTokenCommand(configPath *string) *cobra.Command {
	var serverURL string
	var controlURL string
	var token string
	var tokenEnv string
	var tokenFile string
	cmd := &cobra.Command{
		Use:    "token",
		Short:  "Login with an existing Web Control connection key",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = firstNonEmpty(serverURL, controlURL)
			resolvedToken, err := resolveSecretFlag(token, tokenEnv, tokenFile)
			if err != nil {
				return err
			}
			token = strings.TrimSpace(resolvedToken)
			if controlURL == "" || token == "" {
				return fmt.Errorf("--server and connection key source are required")
			}
			if _, err := remoteLoginHTTPClient.Me(cmd.Context(), controlURL, token); err != nil {
				return err
			}
			return persistRemoteLogin(cmd, *configPath, remoteAuthRecord{ControlURL: controlURL, AccessToken: token})
		},
	}
	cmd.Flags().StringVar(&serverURL, "server", "", "Web Control URL")
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().StringVar(&token, "token", "", "Web Control connection key")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable containing the Web Control connection key")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "file containing the Web Control connection key")
	_ = cmd.Flags().MarkHidden("control-url")
	return cmd
}

func remoteLoginPasswordCommand(configPath *string) *cobra.Command {
	var serverURL string
	var controlURL string
	var email string
	var password string
	var passwordEnv string
	var passwordFile string
	cmd := &cobra.Command{
		Use:    "password",
		Short:  "Login with Web Control email and password",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = firstNonEmpty(serverURL, controlURL)
			email = strings.TrimSpace(email)
			resolvedPassword, err := resolveSecretFlag(password, passwordEnv, passwordFile)
			if err != nil {
				return err
			}
			password = resolvedPassword
			if controlURL == "" || email == "" || password == "" {
				return fmt.Errorf("--server, email, and password source are required")
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
	cmd.Flags().StringVar(&serverURL, "server", "", "Web Control URL")
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().StringVar(&email, "email", "", "Web Control email")
	cmd.Flags().StringVar(&password, "password", "", "Web Control password")
	cmd.Flags().StringVar(&passwordEnv, "password-env", "", "environment variable containing the Web Control password")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "file containing the Web Control password")
	_ = cmd.Flags().MarkHidden("control-url")
	return cmd
}

func remoteLoginBrowserCommand(configPath *string) *cobra.Command {
	var serverURL string
	var controlURL string
	var timeout time.Duration
	var noBrowser bool
	cmd := &cobra.Command{
		Use:     "browser",
		Aliases: []string{"device-code"},
		Short:   "Login in the browser",
		RunE: func(cmd *cobra.Command, args []string) error {
			controlURL = firstNonEmpty(serverURL, controlURL)
			if controlURL == "" {
				return fmt.Errorf("--server is required")
			}
			if timeout <= 0 {
				timeout = 5 * time.Minute
			}
			created, err := remoteLoginHTTPClient.CreateBrowserLogin(cmd.Context(), controlURL, "termx cli")
			if err != nil {
				return err
			}
			if auth, err := completeRemoteBrowserLogin(cmd, controlURL, created, timeout, !noBrowser, cmd.OutOrStdout()); err != nil {
				return err
			} else {
				return persistRemoteLogin(cmd, *configPath, remoteAuthRecord{
					ControlURL:   controlURL,
					AccessToken:  auth.AccessToken,
					RefreshToken: auth.RefreshToken,
				})
			}
		},
	}
	cmd.Flags().StringVar(&serverURL, "server", "", "Web Control URL")
	cmd.Flags().StringVar(&controlURL, "control-url", "", "Web Control URL")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "browser login timeout")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the login URL without opening a browser")
	_ = cmd.Flags().MarkHidden("control-url")
	return cmd
}

func completeRemoteBrowserLogin(cmd *cobra.Command, controlURL string, created remoteBrowserLoginResult, timeout time.Duration, launchBrowser bool, progress io.Writer) (remoteLoginAuthResult, error) {
	if created.BrowserLoginCode == "" {
		return remoteLoginAuthResult{}, fmt.Errorf("control did not return a browser login code")
	}
	if progress == nil {
		progress = cmd.OutOrStdout()
	}
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if created.VerificationURIComplete != "" {
		fmt.Fprintf(progress, "login_url:\t%s\n", created.VerificationURIComplete)
		if launchBrowser {
			if err := openBrowser(created.VerificationURIComplete); err != nil {
				fmt.Fprintf(progress, "browser_open:\tfailed: %v\n", err)
			} else {
				fmt.Fprintln(progress, "browser_open:\ttrue")
			}
		}
	} else if created.UserCode != "" {
		fmt.Fprintf(progress, "login_code:\t%s\n", created.UserCode)
	}
	fmt.Fprintln(progress, "waiting:\tfinish login in the browser")

	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	interval := created.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		auth, ok, err := remoteLoginHTTPClient.PollBrowserLogin(ctx, controlURL, created.BrowserLoginCode)
		if err != nil {
			return remoteLoginAuthResult{}, err
		}
		if ok {
			return auth, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return remoteLoginAuthResult{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func persistRemoteLogin(cmd *cobra.Command, configPath string, record remoteAuthRecord) error {
	path, err := remoteAuthStorePath(configPath)
	if err != nil {
		return err
	}
	if err := saveRemoteAuthRecord(path, record); err != nil {
		return err
	}
	if err := ensureRemoteConfigBootstrap(configPath, record.ControlURL, record.HubURL, path, "online"); err != nil {
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

func (controlPlaneLoginClient) CreateBrowserLogin(ctx context.Context, controlURL string, clientName string) (remoteBrowserLoginResult, error) {
	var out struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := controlJSON(ctx, http.MethodPost, strings.TrimRight(controlURL, "/")+"/api/v1/auth/browser", map[string]string{
		"client_name":      clientName,
		"verification_uri": strings.TrimRight(controlURL, "/") + "/device",
	}, &out, ""); err != nil {
		return remoteBrowserLoginResult{}, err
	}
	return remoteBrowserLoginResult{
		BrowserLoginCode:        out.DeviceCode,
		UserCode:                out.UserCode,
		VerificationURIComplete: out.VerificationURIComplete,
		ExpiresAt:               time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
		Interval:                time.Duration(out.Interval) * time.Second,
	}, nil
}

func (controlPlaneLoginClient) PollBrowserLogin(ctx context.Context, controlURL string, browserLoginCode string) (remoteLoginAuthResult, bool, error) {
	var out remoteLoginAuthResult
	err := controlJSON(ctx, http.MethodPost, strings.TrimRight(controlURL, "/")+"/api/v1/auth/browser/token", map[string]string{
		"device_code": browserLoginCode,
	}, &out, "")
	if err == nil {
		return out, true, nil
	}
	if errors.Is(err, errAuthorizationPending) {
		return remoteLoginAuthResult{}, false, nil
	}
	return remoteLoginAuthResult{}, false, err
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
		return errors.New("control url and connection key are required")
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

func ensureRemoteConfigBootstrap(configPath string, controlURL string, hubURL string, authStorePath string, mode string) error {
	if strings.TrimSpace(configPath) == "" {
		configPath = shared.DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	content := "remote:\n  enabled: true\n"
	if strings.TrimSpace(mode) != "" {
		content += fmt.Sprintf("  mode: %s\n", strings.TrimSpace(mode))
	}
	if strings.TrimSpace(controlURL) != "" {
		content += fmt.Sprintf("  control_url: %s\n", strings.TrimSpace(controlURL))
	}
	if strings.TrimSpace(hubURL) != "" {
		content += fmt.Sprintf("  hub_urls: [%s]\n", strings.TrimSpace(hubURL))
	}
	if strings.TrimSpace(authStorePath) != "" {
		content += fmt.Sprintf("  auth_store: %s\n", authStorePath)
	}
	return os.WriteFile(configPath, []byte(content), 0o600)
}
