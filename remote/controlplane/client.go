package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email,omitempty"`
	Role     string `json:"role,omitempty"`
}

type LoginResult struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
}

type RefreshResult struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("control plane request failed with status %d", e.StatusCode)
	}
	return fmt.Sprintf("control plane request failed with status %d: %s", e.StatusCode, e.Message)
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		baseURL:    normalizeBaseURL(baseURL),
		httpClient: httpClient,
	}
}

func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

func (c *Client) Login(ctx context.Context, username, password string) (*LoginResult, error) {
	var out LoginResult
	err := c.doJSON(ctx, http.MethodPost, "/api/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, "", true, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	var out RefreshResult
	err := c.doJSON(ctx, http.MethodPost, "/api/auth/refresh", map[string]string{
		"refresh_token": refreshToken,
	}, "", true, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/auth/logout", map[string]string{
		"refresh_token": refreshToken,
	}, "", true, nil)
}

func (c *Client) Me(ctx context.Context, accessToken string) (*User, error) {
	var payload struct {
		User User `json:"user"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/api/auth/me", nil, accessToken, false, &payload)
	if err != nil {
		return nil, err
	}
	return &payload.User, nil
}

func IsUnauthorized(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusUnauthorized
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, accessToken string, appClient bool, out any) error {
	if c == nil || c.baseURL == "" {
		return fmt.Errorf("control plane base url is required")
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if appClient {
		req.Header.Set("X-Client-Type", "app")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &payload)
		msg := strings.TrimSpace(payload.Message)
		if msg == "" {
			msg = strings.TrimSpace(payload.Error)
		}
		if msg == "" {
			msg = strings.TrimSpace(string(data))
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}

	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
