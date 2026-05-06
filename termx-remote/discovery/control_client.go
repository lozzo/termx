package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type DeviceRegistrationRequest struct {
	DeviceID         string   `json:"deviceId"`
	MachinePublicKey string   `json:"machinePublicKey,omitempty"`
	DisplayName      string   `json:"displayName"`
	Hostname         string   `json:"hostname"`
	Platform         string   `json:"platform"`
	State            string   `json:"state,omitempty"`
	HubID            string   `json:"hubId,omitempty"`
	Labels           []string `json:"labels,omitempty"`
}

func RegisterDevice(ctx context.Context, baseURL, token string, payload DeviceRegistrationRequest) error {
	if err := doJSON(
		ctx,
		http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/api/devices/register",
		payload,
		nil,
		map[string]string{"Authorization": "Bearer " + token},
	); err != nil {
		return fmt.Errorf("register device in control: %w", err)
	}
	return nil
}

type Hub struct {
	ID        string `json:"id"`
	Region    string `json:"region"`
	HTTPURL   string `json:"http_url"`
	Status    string `json:"status"`
	Capacity  int    `json:"capacity"`
	Weight    int    `json:"weight"`
	Health    string `json:"health,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func DiscoverHubs(ctx context.Context, baseURL, token string) ([]Hub, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("control URL is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("access token is required")
	}
	var out struct {
		Hubs []Hub `json:"hubs"`
	}
	if err := doJSON(
		ctx,
		http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/api/v1/hubs",
		nil,
		&out,
		map[string]string{"Authorization": "Bearer " + token},
	); err != nil {
		return nil, fmt.Errorf("discover hubs: %w", err)
	}
	return out.Hubs, nil
}

type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if strings.TrimSpace(e.Body) == "" {
		return fmt.Sprintf("request failed: %d", e.StatusCode)
	}
	return fmt.Sprintf("request failed: %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

func IsHTTPStatus(err error, status int) bool {
	var statusErr *HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == status
}

func IsForcedOffline(err error) bool {
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	if statusErr.StatusCode != http.StatusUnauthorized && statusErr.StatusCode != http.StatusForbidden {
		return false
	}
	body := strings.ToLower(strings.TrimSpace(statusErr.Body))
	return strings.Contains(body, "forced offline") || strings.Contains(body, "forced_offline")
}

func doJSON(ctx context.Context, method, url string, input any, out any, headers map[string]string) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode json request: %w", err)
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("build json request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform json request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &HTTPStatusError{StatusCode: resp.StatusCode}
	}
	if resp.StatusCode >= 400 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(payload)),
		}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
