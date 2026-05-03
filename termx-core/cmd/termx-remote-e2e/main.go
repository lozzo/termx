package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core"
	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	remotecert "github.com/lozzow/termx/termx-core/internal/remote/cert"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/pion/webrtc/v4"
)

type authResponse struct {
	AccessToken string `json:"access_token"`
}

type deviceRecord struct {
	ID string `json:"id"`
}

type devicesResponse struct {
	Devices []deviceRecord `json:"devices"`
}

type terminalRecord struct {
	ID        string `json:"id"`
	MachineID string `json:"machine_id"`
	State     string `json:"state"`
}

type terminalsResponse struct {
	Terminals []terminalRecord `json:"terminals"`
}

type managedTicketResponse struct {
	ID         string    `json:"id"`
	MachineID  string    `json:"machine_id"`
	TerminalID string    `json:"terminal_id"`
	Path       string    `json:"path"`
	AllowRelay bool      `json:"allow_relay"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type pairHTTPResponse struct {
	MachineID        string                            `json:"machine_id"`
	AppCertificate   remotecert.AppCertificateEnvelope `json:"app_certificate"`
	MachinePublicKey string                            `json:"machine_public_key"`
	ExpiresAt        time.Time                         `json:"expires_at"`
}

type managedSessionRequestInput struct {
	TicketID       string
	MachineID      string
	TerminalID     string
	SessionID      string
	SDP            string
	ICECandidates  []string
	AppCertificate json.RawMessage
	AppPrivateKey  ed25519.PrivateKey
	Nonce          string
	Now            time.Time
}

type managedSessionRequest struct {
	ConnectTicket  string                   `json:"connect_ticket"`
	MachineID      string                   `json:"machine_id"`
	TerminalID     string                   `json:"terminal_id"`
	AppCertificate json.RawMessage          `json:"app_certificate"`
	Offer          managedSessionOffer      `json:"offer"`
	Signature      remotertc.OfferSignature `json:"signature"`
}

type managedSessionOffer struct {
	SessionID     string   `json:"session_id"`
	SDP           string   `json:"sdp"`
	ICECandidates []string `json:"ice_candidates"`
}

func main() {
	var controlURL string
	var hubURL string
	var email string
	var password string
	var pairURL string
	var pairSessionID string
	var pairSecret string
	var machineID string
	var terminalID string
	var stunURL string

	flag.StringVar(&controlURL, "control-url", "http://127.0.0.1:12306", "control plane base URL")
	flag.StringVar(&hubURL, "hub-url", "http://127.0.0.1:8447", "hub base URL")
	flag.StringVar(&email, "email", "demo@example.com", "control login/register email")
	flag.StringVar(&password, "password", "demo1234", "control login/register password")
	flag.StringVar(&pairURL, "pair-url", "", "daemon local pair URL, usually forwarded to http://127.0.0.1:18888/api/local/pair")
	flag.StringVar(&pairSessionID, "pair-session-id", "", "pair session id from `termx remote pair --json`")
	flag.StringVar(&pairSecret, "pair-secret", "", "pair secret from `termx remote pair --json`")
	flag.StringVar(&machineID, "machine-id", "", "optional target machine id; defaults to first registered terminal machine")
	flag.StringVar(&terminalID, "terminal-id", "", "optional target terminal id; defaults to first registered terminal")
	flag.StringVar(&stunURL, "stun-url", "", "optional STUN URL for the local offerer, for example stun:stun.l.google.com:19302")
	flag.Parse()

	if err := run(smokeConfig{
		ControlURL:    controlURL,
		HubURL:        hubURL,
		Email:         email,
		Password:      password,
		PairURL:       pairURL,
		PairSessionID: pairSessionID,
		PairSecret:    pairSecret,
		MachineID:     machineID,
		TerminalID:    terminalID,
		STUNURL:       stunURL,
	}); err != nil {
		log.Fatal(err)
	}
	log.Println("remote managed smoke passed")
}

type smokeConfig struct {
	ControlURL    string
	HubURL        string
	Email         string
	Password      string
	PairURL       string
	PairSessionID string
	PairSecret    string
	MachineID     string
	TerminalID    string
	STUNURL       string
}

func run(cfg smokeConfig) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Jar: jar, Timeout: 20 * time.Second}

	auth, err := registerOrLogin(httpClient, cfg.ControlURL, cfg.Email, cfg.Password)
	if err != nil {
		return err
	}
	if strings.TrimSpace(auth.AccessToken) == "" {
		return fmt.Errorf("control returned empty access token")
	}

	machineID, terminalID, err := waitForControlInventory(httpClient, cfg.ControlURL, auth.AccessToken, cfg.MachineID, cfg.TerminalID)
	if err != nil {
		return err
	}
	appCert, appPrivate, err := claimPairCertificate(httpClient, cfg.PairURL, cfg.PairSessionID, cfg.PairSecret)
	if err != nil {
		return err
	}
	if appCert.Payload.MachineID != machineID {
		return fmt.Errorf("pair certificate machine mismatch: %s != %s", appCert.Payload.MachineID, machineID)
	}
	certJSON, err := json.Marshal(appCert)
	if err != nil {
		return err
	}

	ticket, err := createManagedTicket(httpClient, cfg.ControlURL, auth.AccessToken, machineID, terminalID)
	if err != nil {
		return err
	}
	if ticket.Path != "managed" || ticket.AllowRelay {
		return fmt.Errorf("unexpected ticket policy path=%q allow_relay=%v", ticket.Path, ticket.AllowRelay)
	}

	return smokeTerminalAttach(openChannelConfig{
		HubURL:         cfg.HubURL,
		Ticket:         ticket,
		AppCertificate: certJSON,
		AppPrivateKey:  appPrivate,
		STUNURL:        cfg.STUNURL,
	})
}

func registerOrLogin(client *http.Client, controlURL, email, password string) (authResponse, error) {
	var auth authResponse
	status, body, err := postJSONStatus(client, controlURL+"/api/v1/auth/register", map[string]string{
		"email":    email,
		"password": password,
	}, &auth, "")
	if err == nil && status == http.StatusCreated {
		return auth, nil
	}
	auth = authResponse{}
	status, body, err = postJSONStatus(client, controlURL+"/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, &auth, "")
	if err != nil {
		return authResponse{}, fmt.Errorf("register/login control failed: status=%d body=%s err=%w", status, body, err)
	}
	return auth, nil
}

func waitForControlInventory(client *http.Client, controlURL, accessToken, wantedMachineID, wantedTerminalID string) (string, string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var devices devicesResponse
		var terminals terminalsResponse
		devicesErr := getJSONAuth(client, controlURL+"/api/devices", &devices, accessToken)
		terminalsErr := getJSONAuth(client, controlURL+"/api/terminals", &terminals, accessToken)
		if devicesErr == nil && terminalsErr == nil {
			if machineID, terminalID, ok := selectTarget(devices, terminals, wantedMachineID, wantedTerminalID); ok {
				return machineID, terminalID, nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", "", fmt.Errorf("timed out waiting for control inventory machine=%q terminal=%q", wantedMachineID, wantedTerminalID)
}

func selectTarget(devices devicesResponse, terminals terminalsResponse, wantedMachineID, wantedTerminalID string) (string, string, bool) {
	deviceIDs := make(map[string]struct{}, len(devices.Devices))
	for _, device := range devices.Devices {
		if strings.TrimSpace(device.ID) != "" {
			deviceIDs[device.ID] = struct{}{}
		}
	}
	for _, terminal := range terminals.Terminals {
		if wantedTerminalID != "" && terminal.ID != wantedTerminalID {
			continue
		}
		if wantedMachineID != "" && terminal.MachineID != wantedMachineID {
			continue
		}
		if _, ok := deviceIDs[terminal.MachineID]; !ok {
			continue
		}
		if terminal.ID != "" && terminal.MachineID != "" {
			return terminal.MachineID, terminal.ID, true
		}
	}
	return "", "", false
}

func claimPairCertificate(client *http.Client, pairURL, pairSessionID, pairSecret string) (remotecert.AppCertificateEnvelope, ed25519.PrivateKey, error) {
	if strings.TrimSpace(pairURL) == "" || strings.TrimSpace(pairSessionID) == "" || strings.TrimSpace(pairSecret) == "" {
		return remotecert.AppCertificateEnvelope{}, nil, fmt.Errorf("pair-url, pair-session-id, and pair-secret are required")
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return remotecert.AppCertificateEnvelope{}, nil, err
	}
	var pair pairHTTPResponse
	_, body, err := postJSONStatus(client, pairURL, map[string]any{
		"pair_session_id":        pairSessionID,
		"pair_secret":            pairSecret,
		"app_device_id":          randomToken("app_"),
		"app_name":               "termx-remote-e2e",
		"app_public_key":         base64.StdEncoding.EncodeToString(appPublic),
		"requested_capabilities": []string{"terminal", "file_manager", "terminal_management"},
	}, &pair, "")
	if err != nil {
		return remotecert.AppCertificateEnvelope{}, nil, fmt.Errorf("claim pair certificate failed: body=%s err=%w", body, err)
	}
	if pair.AppCertificate.Payload.MachineID == "" {
		return remotecert.AppCertificateEnvelope{}, nil, fmt.Errorf("pair response missing app certificate")
	}
	return pair.AppCertificate, appPrivate, nil
}

func createManagedTicket(client *http.Client, controlURL, accessToken, machineID, terminalID string) (managedTicketResponse, error) {
	var ticket managedTicketResponse
	_, body, err := postJSONStatus(client, controlURL+"/api/v1/managed/connect-tickets", map[string]any{
		"machine_id":  machineID,
		"terminal_id": terminalID,
		"ttl_seconds": 120,
	}, &ticket, accessToken)
	if err != nil {
		return managedTicketResponse{}, fmt.Errorf("create managed ticket failed: body=%s err=%w", body, err)
	}
	if ticket.ID == "" {
		return managedTicketResponse{}, fmt.Errorf("control returned empty managed ticket id")
	}
	return ticket, nil
}

func buildManagedSessionRequest(input managedSessionRequestInput) (managedSessionRequest, error) {
	if len(input.AppPrivateKey) != ed25519.PrivateKeySize {
		return managedSessionRequest{}, fmt.Errorf("app private key has size %d, want %d", len(input.AppPrivateKey), ed25519.PrivateKeySize)
	}
	if len(input.AppCertificate) == 0 {
		return managedSessionRequest{}, fmt.Errorf("app certificate is required")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nonce := strings.TrimSpace(input.Nonce)
	if nonce == "" {
		nonce = randomToken("nonce_")
	}
	fields := remotertc.OfferSignatureFields{
		TicketID:   input.TicketID,
		MachineID:  input.MachineID,
		TerminalID: input.TerminalID,
		SDP:        input.SDP,
		Candidates: input.ICECandidates,
		Nonce:      nonce,
		Timestamp:  now,
	}
	signature := ed25519.Sign(input.AppPrivateKey, remotertc.CanonicalOfferSignatureMessage(fields))
	return managedSessionRequest{
		ConnectTicket:  input.TicketID,
		MachineID:      input.MachineID,
		TerminalID:     input.TerminalID,
		AppCertificate: append(json.RawMessage(nil), input.AppCertificate...),
		Offer: managedSessionOffer{
			SessionID:     input.SessionID,
			SDP:           input.SDP,
			ICECandidates: append([]string(nil), input.ICECandidates...),
		},
		Signature: remotertc.OfferSignature{
			Algorithm: "ed25519",
			Nonce:     nonce,
			Timestamp: now.UTC().Unix(),
			Value:     base64.StdEncoding.EncodeToString(signature),
		},
	}, nil
}

type openChannelConfig struct {
	HubURL         string
	Ticket         managedTicketResponse
	AppCertificate json.RawMessage
	AppPrivateKey  ed25519.PrivateKey
	STUNURL        string
}

func smokeTerminalAttach(cfg openChannelConfig) error {
	label := "terminal:" + cfg.Ticket.TerminalID
	offerPC, dc, answerSDP, err := openLabeledChannel(cfg, label)
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
	if _, err := client.Snapshot(ctx, cfg.Ticket.TerminalID, 0, 0); err != nil {
		return fmt.Errorf("snapshot over terminal channel: %w", err)
	}
	attach, err := client.Attach(ctx, cfg.Ticket.TerminalID, string(termx.ModeCollaborator))
	if err != nil {
		return fmt.Errorf("attach terminal: %w", err)
	}
	stream, stop := client.Stream(attach.Channel)
	defer stop()
	if err := client.Input(ctx, attach.Channel, []byte("echo remote-stack-smoke\n")); err != nil {
		return fmt.Errorf("send input: %w", err)
	}
	return waitForStreamContains(stream, "remote-stack-smoke", 10*time.Second)
}

func openLabeledChannel(cfg openChannelConfig, label string) (*webrtc.PeerConnection, *webrtc.DataChannel, string, error) {
	webrtcCfg := webrtc.Configuration{}
	if strings.TrimSpace(cfg.STUNURL) != "" {
		webrtcCfg.ICEServers = []webrtc.ICEServer{{URLs: []string{strings.TrimSpace(cfg.STUNURL)}}}
	}
	offerPC, err := webrtc.NewPeerConnection(webrtcCfg)
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
	waitGathering(offerPC, 8*time.Second)
	localDescription := offerPC.LocalDescription()
	if localDescription == nil || localDescription.SDP == "" {
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("local offer SDP is empty")
	}

	sessionID := cfg.Ticket.ID + "-" + strings.ReplaceAll(label, ":", "-")
	requestBody, err := buildManagedSessionRequest(managedSessionRequestInput{
		TicketID:       cfg.Ticket.ID,
		MachineID:      cfg.Ticket.MachineID,
		TerminalID:     cfg.Ticket.TerminalID,
		SessionID:      sessionID,
		SDP:            localDescription.SDP,
		ICECandidates:  []string{},
		AppCertificate: cfg.AppCertificate,
		AppPrivateKey:  cfg.AppPrivateKey,
	})
	if err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	var sessionResp struct {
		SessionID string `json:"session_id"`
		Pending   bool   `json:"pending"`
		Answer    struct {
			SDP           string   `json:"sdp"`
			ICECandidates []string `json:"ice_candidates"`
		} `json:"answer"`
	}
	if _, body, err := postJSONStatus(http.DefaultClient, strings.TrimRight(cfg.HubURL, "/")+"/api/v1/sessions", requestBody, &sessionResp, ""); err != nil {
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("submit managed offer failed: body=%s err=%w", body, err)
	}
	if sessionResp.Pending {
		if err := pollManagedAnswer(strings.TrimRight(cfg.HubURL, "/"), cfg.Ticket, sessionID, &sessionResp); err != nil {
			offerPC.Close()
			return nil, nil, "", err
		}
	}
	if sessionResp.Answer.SDP == "" {
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("hub returned empty answer SDP")
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sessionResp.Answer.SDP}); err != nil {
		offerPC.Close()
		return nil, nil, "", err
	}
	select {
	case <-openCh:
	case <-time.After(15 * time.Second):
		offerPC.Close()
		return nil, nil, "", fmt.Errorf("timed out waiting for %s data channel open", label)
	}
	return offerPC, dc, sessionResp.Answer.SDP, nil
}

func pollManagedAnswer(hubURL string, ticket managedTicketResponse, sessionID string, out any) error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_, _, err := postJSONStatus(http.DefaultClient, hubURL+"/api/v1/sessions/"+sessionID+"/answer", map[string]string{
			"connect_ticket": ticket.ID,
			"machine_id":     ticket.MachineID,
		}, out, "")
		if err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out polling managed answer")
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

func postJSONStatus(client *http.Client, url string, body any, out any, bearer string) (int, string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	rawBody, readErr := io.ReadAll(resp.Body)
	bodyText := string(rawBody)
	if readErr != nil {
		return resp.StatusCode, bodyText, readErr
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, bodyText, fmt.Errorf("http %d", resp.StatusCode)
	}
	if out != nil && len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, out); err != nil {
			return resp.StatusCode, bodyText, err
		}
	}
	return resp.StatusCode, bodyText, nil
}

func getJSONAuth(client *http.Client, url string, out any, bearer string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearer))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d: %s", resp.StatusCode, string(raw))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func randomToken(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf[:])
}

func minimalSDP(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "offer"
	}
	return strings.Join([]string{
		"v=0",
		"o=- 0 0 IN IP4 127.0.0.1",
		"s=" + sessionID,
		"t=0 0",
		"m=application 9 UDP/DTLS/SCTP webrtc-datachannel",
		"a=mid:data",
		"a=sctp-port:5000",
		"",
	}, "\r\n")
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
