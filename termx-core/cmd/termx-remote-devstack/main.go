package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lozzow/termx/termx-core"
)

type tokenResponse struct {
	RawToken string `json:"rawToken"`
}

type devicesResponse struct {
	Devices []struct {
		ID string `json:"id"`
	} `json:"devices"`
}

type terminalsResponse struct {
	Terminals []struct {
		ID string `json:"id"`
	} `json:"terminals"`
}

func main() {
	var controlURL string
	var hubURL string
	var email string
	var password string
	var terminalName string
	var dataDir string

	flag.StringVar(&controlURL, "control-url", "http://127.0.0.1:12306", "control plane base URL")
	flag.StringVar(&hubURL, "hub-url", "http://127.0.0.1:8447", "hub base URL")
	flag.StringVar(&email, "email", "demo@example.com", "control login email")
	flag.StringVar(&password, "password", "demo1234", "control login password")
	flag.StringVar(&terminalName, "terminal-name", "remote-ui-terminal", "terminal name to create")
	flag.StringVar(&dataDir, "data-dir", "", "persistent remote runtime data directory; defaults to a temporary directory")
	flag.Parse()

	if err := run(controlURL, hubURL, email, password, terminalName, dataDir); err != nil {
		log.Fatal(err)
	}
}

func run(controlURL, hubURL, email, password, terminalName, dataDir string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	if err := postJSON(httpClient, controlURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, nil); err != nil {
		return err
	}

	var tokenResp tokenResponse
	if err := postJSON(httpClient, controlURL+"/api/tokens", map[string]string{
		"name": "remote-devstack",
	}, &tokenResp); err != nil {
		return err
	}

	srv := termx.NewServer(termx.WithRemoteConfig(termx.RemoteConfig{
		Enabled:     true,
		ControlURL:  controlURL,
		HubURL:      hubURL,
		AccessToken: tokenResp.RawToken,
		DataDir:     mustDataDir(dataDir),
		DeviceName:  "remote-ui-device",
	}))

	ctx := context.Background()
	created, err := srv.Create(ctx, termx.CreateOptions{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    terminalName,
		Size:    termx.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		return err
	}

	status := srv.RemoteStatus()
	if err := waitForControlInventory(httpClient, controlURL, status.DeviceID, created.ID); err != nil {
		return err
	}
	if err := waitForHubInventory(hubURL, status.DeviceID); err != nil {
		return err
	}

	log.Printf("remote devstack ready device=%s terminal=%s", status.DeviceID, created.ID)
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()
	return nil
}

func mustDataDir(dataDir string) string {
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			panic(err)
		}
		return dataDir
	}
	dir, err := os.MkdirTemp("", "termx-remote-devstack-*")
	if err != nil {
		panic(err)
	}
	return dir
}

func waitForControlInventory(client *http.Client, controlURL, deviceID, terminalID string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var devices devicesResponse
		var terminals terminalsResponse
		if err := getJSON(client, controlURL+"/api/devices", &devices); err == nil {
			if err := getJSON(client, controlURL+"/api/terminals", &terminals); err == nil {
				if containsDevice(devices, deviceID) && containsTerminal(terminals, terminalID) {
					return nil
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return os.ErrDeadlineExceeded
}

func waitForHubInventory(hubURL, deviceID string) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(hubURL + "/api/v1/agents")
		if err == nil {
			var payload struct {
				Agents []struct {
					DeviceID string `json:"device_id"`
				} `json:"agents"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
				for _, agent := range payload.Agents {
					if agent.DeviceID == deviceID {
						closeBody(resp)
						return nil
					}
				}
			}
			closeBody(resp)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return os.ErrDeadlineExceeded
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func postJSON(client *http.Client, url string, body any, out any) error {
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func getJSON(client *http.Client, url string, out any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func containsDevice(resp devicesResponse, id string) bool {
	for _, device := range resp.Devices {
		if device.ID == id {
			return true
		}
	}
	return false
}

func containsTerminal(resp terminalsResponse, id string) bool {
	for _, terminal := range resp.Terminals {
		if terminal.ID == id {
			return true
		}
	}
	return false
}
