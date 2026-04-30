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

type DeviceRegistrationTerminal struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command []string `json:"command,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
	State   string   `json:"state,omitempty"`
}

type DeviceRegistrationRequest struct {
	DeviceID    string                       `json:"deviceId"`
	DisplayName string                       `json:"displayName"`
	Hostname    string                       `json:"hostname"`
	Platform    string                       `json:"platform"`
	State       string                       `json:"state,omitempty"`
	HubID       string                       `json:"hubId,omitempty"`
	Labels      []string                     `json:"labels,omitempty"`
	Terminals   []DeviceRegistrationTerminal `json:"terminals,omitempty"`
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

func doJSON(ctx context.Context, method, url string, input any, out any, headers map[string]string) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode json request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build json request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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
