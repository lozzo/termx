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
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/pion/webrtc/v4"
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

type ticketResponse struct {
	Ticket struct {
		TicketID      string `json:"ticketId"`
		DeviceID      string `json:"deviceId"`
		TerminalID    string `json:"terminalId"`
		HubBaseURL    string `json:"hubBaseUrl"`
		SignalingPath string `json:"signalingPath"`
		RTCConfigPath string `json:"rtcConfigPath"`
		AllowRelay    bool   `json:"allowRelay"`
	} `json:"ticket"`
}

type apiResponse struct {
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func main() {
	var controlURL string
	var hubURL string
	var email string
	var password string

	flag.StringVar(&controlURL, "control-url", "http://127.0.0.1:12306", "control plane base URL")
	flag.StringVar(&hubURL, "hub-url", "http://127.0.0.1:8447", "hub base URL")
	flag.StringVar(&email, "email", "demo@example.com", "control login email")
	flag.StringVar(&password, "password", "demo1234", "control login password")
	flag.Parse()

	if err := run(controlURL, hubURL, email, password); err != nil {
		log.Fatal(err)
	}
	log.Println("remote stack smoke passed")
}

func run(controlURL, hubURL, email, password string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Jar: jar, Timeout: 20 * time.Second}

	if err := postJSON(httpClient, controlURL+"/api/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, nil); err != nil {
		return fmt.Errorf("login control: %w", err)
	}

	var tokenResp tokenResponse
	if err := postJSON(httpClient, controlURL+"/api/tokens", map[string]string{
		"name": "remote-e2e",
	}, &tokenResp); err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	if tokenResp.RawToken == "" {
		return fmt.Errorf("control returned empty raw token")
	}

	srv := termx.NewServer(termx.WithRemoteConfig(termx.RemoteConfig{
		Enabled:     true,
		ControlURL:  controlURL,
		HubURL:      hubURL,
		AccessToken: tokenResp.RawToken,
		DataDir:     mustMkdirTemp(),
		DeviceName:  "remote-e2e-device",
	}))

	ctx := context.Background()
	created, err := srv.Create(ctx, termx.CreateOptions{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "remote-e2e-terminal",
		Size:    termx.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		return fmt.Errorf("create terminal: %w", err)
	}

	status := srv.RemoteStatus()
	if status.DeviceID == "" {
		return fmt.Errorf("remote runtime did not expose device id")
	}

	if err := waitForControlInventory(httpClient, controlURL, status.DeviceID, created.ID); err != nil {
		return err
	}
	if err := waitForHubInventory(hubURL, status.DeviceID); err != nil {
		return err
	}

	var ticketResp ticketResponse
	if err := postJSON(httpClient, controlURL+"/api/connect-ticket", map[string]string{
		"deviceId":   status.DeviceID,
		"terminalId": created.ID,
	}, &ticketResp); err != nil {
		return fmt.Errorf("request connect ticket: %w", err)
	}

	if err := smokeTerminalAttach(ticketResp); err != nil {
		return err
	}
	if err := smokeFileAPI(ticketResp); err != nil {
		return err
	}
	return nil
}

func mustMkdirTemp() string {
	dir, err := os.MkdirTemp("", "termx-remote-e2e-*")
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
	return fmt.Errorf("timed out waiting for control inventory")
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
				_ = resp.Body.Close()
				for _, agent := range payload.Agents {
					if agent.DeviceID == deviceID {
						return nil
					}
				}
			} else {
				_ = resp.Body.Close()
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for hub inventory")
}

func smokeTerminalAttach(ticket ticketResponse) error {
	offerPC, dc, answerSDP, err := openLabeledChannel(ticket, "terminal:"+ticket.Ticket.TerminalID)
	if err != nil {
		return fmt.Errorf("open terminal channel: %w", err)
	}
	defer offerPC.Close()
	defer dc.Close()

	clientTransport := bridge.NewDataChannelTransport(dc)
	defer clientTransport.Close()
	client := protocol.NewClient(clientTransport)
	defer client.Close()

	ctx := context.Background()
	if err := client.Hello(ctx, protocol.Hello{Version: protocol.Version, Client: "remote-e2e"}); err != nil {
		return fmt.Errorf("protocol hello: %w", err)
	}
	if answerSDP == "" {
		return fmt.Errorf("empty answer sdp")
	}
	if _, err := client.Snapshot(ctx, ticket.Ticket.TerminalID, 0, 0); err != nil {
		return fmt.Errorf("snapshot over terminal channel: %w", err)
	}
	attach, err := client.Attach(ctx, ticket.Ticket.TerminalID, string(termx.ModeCollaborator))
	if err != nil {
		return fmt.Errorf("attach terminal: %w", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()
	if err := client.Input(ctx, attach.Channel, []byte("echo remote-stack-smoke\n")); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	if err := waitForStreamContains(stream, "remote-stack-smoke", 8*time.Second); err != nil {
		return err
	}
	return nil
}

func smokeFileAPI(ticket ticketResponse) error {
	offerPC, apiDC, _, err := openLabeledChannel(ticket, "api")
	if err != nil {
		return fmt.Errorf("open api channel: %w", err)
	}
	defer offerPC.Close()
	defer apiDC.Close()

	tempDir, err := os.MkdirTemp("", "termx-fileapi-*")
	if err != nil {
		return err
	}
	sourcePath := tempDir + "/source.txt"
	uploadPath := tempDir + "/upload.txt"
	if err := os.WriteFile(sourcePath, []byte("file api smoke\n"), 0o644); err != nil {
		return err
	}

	listResp, err := sendAPIRequest(apiDC, apiRequest{ID: "list", Method: "POST", Path: "/files/list", Body: map[string]any{"path": tempDir}})
	if err != nil || listResp.Status != 200 {
		return fmt.Errorf("list api failed: %v status=%d", err, listResp.Status)
	}

	previewResp, err := sendAPIRequest(apiDC, apiRequest{ID: "preview", Method: "POST", Path: "/files/preview", Body: map[string]any{"path": sourcePath}})
	if err != nil || previewResp.Status != 200 {
		return fmt.Errorf("preview api failed: %v status=%d", err, previewResp.Status)
	}

	downloadInit, err := sendAPIRequest(apiDC, apiRequest{ID: "download-init", Method: "POST", Path: "/files/download/init", Body: map[string]any{"path": sourcePath}})
	if err != nil || downloadInit.Status != 200 {
		return fmt.Errorf("download init failed: %v status=%d", err, downloadInit.Status)
	}
	var dl struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(downloadInit.Body, &dl); err != nil || dl.TransferID == "" {
		return fmt.Errorf("decode download init: %w", err)
	}

	downloadPC, fileDownloadDC, _, err := openLabeledChannel(ticket, "file:"+dl.TransferID)
	if err != nil {
		return err
	}
	downloaded, err := readFileDownload(fileDownloadDC)
	downloadPC.Close()
	if err != nil {
		return err
	}
	if string(downloaded) != "file api smoke\n" {
		return fmt.Errorf("unexpected downloaded content: %q", string(downloaded))
	}

	uploadInit, err := sendAPIRequest(apiDC, apiRequest{ID: "upload-init", Method: "POST", Path: "/files/upload/init", Body: map[string]any{"path": uploadPath, "size": 17}})
	if err != nil || uploadInit.Status != 200 {
		return fmt.Errorf("upload init failed: %v status=%d", err, uploadInit.Status)
	}
	var ul struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.Unmarshal(uploadInit.Body, &ul); err != nil || ul.TransferID == "" {
		return fmt.Errorf("decode upload init: %w", err)
	}

	uploadPC, fileUploadDC, _, err := openLabeledChannel(ticket, "file:"+ul.TransferID)
	if err != nil {
		return err
	}
	if err := sendFileUpload(fileUploadDC, []byte("upload smoke ok\n")); err != nil {
		uploadPC.Close()
		return err
	}
	uploadPC.Close()

	uploadComplete, err := sendAPIRequest(apiDC, apiRequest{ID: "upload-complete", Method: "POST", Path: "/files/upload/complete", Body: map[string]any{"transfer_id": ul.TransferID}})
	if err != nil || uploadComplete.Status != 200 {
		return fmt.Errorf("upload complete failed: %v status=%d", err, uploadComplete.Status)
	}
	uploaded, err := os.ReadFile(uploadPath)
	if err != nil {
		return err
	}
	if string(uploaded) != "upload smoke ok\n" {
		return fmt.Errorf("unexpected uploaded content: %q", string(uploaded))
	}
	return nil
}

func openLabeledChannel(ticket ticketResponse, label string) (*webrtc.PeerConnection, *webrtc.DataChannel, string, error) {
	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, nil, "", err
	}
	dc, err := offerPC.CreateDataChannel(label, nil)
	if err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}

	openCh := make(chan struct{})
	dc.OnOpen(func() {
		select {
		case <-openCh:
		default:
			close(openCh)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	waitGathering(offerPC, 5*time.Second)

	reqBody, _ := json.Marshal(map[string]any{
		"session_id":           ticket.Ticket.TicketID + "-" + strings.ReplaceAll(label, ":", "-"),
		"ticket_id":            ticket.Ticket.TicketID,
		"device_id":            ticket.Ticket.DeviceID,
		"terminal_id":          ticket.Ticket.TerminalID,
		"sdp":                  offerPC.LocalDescription().SDP,
		"ice_candidates":       []string{},
		"allow_relay":          ticket.Ticket.AllowRelay,
		"allow_relay_transfer": false,
	})
	resp, err := http.Post(ticket.Ticket.HubBaseURL+ticket.Ticket.SignalingPath, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("hub offer failed with %d", resp.StatusCode)
	}
	var answer struct {
		SDP           string   `json:"sdp"`
		ICECandidates []string `json:"ice_candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answer.SDP}); err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	select {
	case <-openCh:
	case <-time.After(10 * time.Second):
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("timed out waiting for %s data channel open", label)
	}
	return offerPC, dc, answer.SDP, nil
}

func waitGathering(pc *webrtc.PeerConnection, timeout time.Duration) {
	if pc.ICEGatheringState() == webrtc.ICEGatheringStateComplete {
		return
	}
	done := make(chan struct{})
	pc.OnICEGatheringStateChange(func(state webrtc.ICEGatheringState) {
		if state == webrtc.ICEGatheringStateComplete {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

type apiRequest struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Path   string      `json:"path"`
	Body   interface{} `json:"body"`
}

func sendAPIRequest(dc *webrtc.DataChannel, payload apiRequest) (apiResponse, error) {
	data, _ := json.Marshal(payload)
	resultCh := make(chan apiResponse, 1)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if resp, ok := decodeChunkedAPIResponse(msg.Data); ok {
			resultCh <- resp
		}
	})
	if err := dc.Send(data); err != nil {
		return apiResponse{}, err
	}
	select {
	case resp := <-resultCh:
		return resp, nil
	case <-time.After(10 * time.Second):
		return apiResponse{}, fmt.Errorf("timed out waiting for api response")
	}
}

func decodeChunkedAPIResponse(data []byte) (apiResponse, bool) {
	if len(data) < 4 || data[0] != 0xc0 {
		return apiResponse{}, false
	}
	idLen := int(data[2])
	if len(data) < 3+idLen {
		return apiResponse{}, false
	}
	var out apiResponse
	if err := json.Unmarshal(data[3+idLen:], &out); err != nil {
		return apiResponse{}, false
	}
	return out, true
}

func readFileDownload(dc *webrtc.DataChannel) ([]byte, error) {
	done := make(chan []byte, 1)
	var chunks [][]byte
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) == 0 {
			return
		}
		switch msg.Data[0] {
		case 0x01:
			chunks = append(chunks, append([]byte(nil), msg.Data[5:]...))
		case 0x02:
			var merged []byte
			for _, chunk := range chunks {
				merged = append(merged, chunk...)
			}
			done <- merged
		}
	})
	select {
	case data := <-done:
		return data, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("timed out waiting for file download")
	}
}

func sendFileUpload(dc *webrtc.DataChannel, payload []byte) error {
	ackCh := make(chan struct{}, 1)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if len(msg.Data) > 0 && msg.Data[0] == 0x02 {
			select {
			case ackCh <- struct{}{}:
			default:
			}
		}
	})
	frame := make([]byte, 5+len(payload))
	frame[0] = 0x01
	copy(frame[5:], payload)
	if err := dc.Send(frame); err != nil {
		return err
	}
	complete := make([]byte, 5)
	complete[0] = 0x02
	if err := dc.Send(complete); err != nil {
		return err
	}
	select {
	case <-ackCh:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timed out waiting for upload ack")
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

func containsDevice(resp devicesResponse, deviceID string) bool {
	for _, device := range resp.Devices {
		if device.ID == deviceID {
			return true
		}
	}
	return false
}

func containsTerminal(resp terminalsResponse, terminalID string) bool {
	for _, terminal := range resp.Terminals {
		if terminal.ID == terminalID {
			return true
		}
	}
	return false
}

type testingTAdapter struct {
	fatalf func(format string, args ...any)
}

func waitForStreamContains(stream <-chan protocol.StreamFrame, needle string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("timed out waiting for terminal output %q", needle)
		case frame, ok := <-stream:
			if !ok {
				return fmt.Errorf("stream closed while waiting for terminal output %q", needle)
			}
			if frame.Type == protocol.TypeOutput && strings.Contains(string(frame.Payload), needle) {
				return nil
			}
		}
	}
}
