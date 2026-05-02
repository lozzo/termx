package termx

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/bridge"
	remotecert "github.com/lozzow/termx/termx-core/internal/remote/cert"
	remoteidentity "github.com/lozzow/termx/termx-core/internal/remote/identity"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/pion/webrtc/v4"
)

func TestE2ERemoteLocalWebHandlerStatusTerminalsAndPair(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-web-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-web-shell",
		Size:    Size{Cols: 100, Rows: 30},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}
	handler := srv.LocalWebHandler(LocalWebOptions{
		HTTPURL:       "http://127.0.0.1:7342",
		LocalPairURL:  "http://127.0.0.1:7342/api/local/pair",
		ICETCPEnabled: true,
		ICETCPPort:    7342,
		Assets: NewLocalWebStaticAssets(map[string]string{
			"index.html": "<!doctype html><title>TermX Local</title>",
		}),
	})

	status := localWebJSON(t, handler, http.MethodGet, "/api/local/status", nil)
	if status["machine_id"] == "" {
		t.Fatalf("expected machine_id in local status: %#v", status)
	}
	if status["device_id"] != nil {
		t.Fatalf("local status must not expose device_id: %#v", status)
	}
	if raw, _ := json.Marshal(status); strings.Contains(strings.ToLower(string(raw)), "turn:") {
		t.Fatalf("local status must not include TURN credentials: %s", raw)
	}

	terminals := localWebJSON(t, handler, http.MethodGet, "/api/local/terminals", nil)
	terminalList, ok := terminals["terminals"].([]any)
	if !ok || len(terminalList) != 1 {
		t.Fatalf("expected one local terminal, got %#v", terminals)
	}
	first, ok := terminalList[0].(map[string]any)
	if !ok {
		t.Fatalf("expected terminal object, got %#v", terminalList[0])
	}
	if first["terminal_id"] != term.ID {
		t.Fatalf("expected terminal_id %q, got %#v", term.ID, first)
	}
	if first["pane_id"] != nil || first["workspace_id"] != nil || first["tab_id"] != nil {
		t.Fatalf("local terminal response must not expose workspace/tab/pane: %#v", first)
	}

	session, err := srv.RemotePairStart(PairStartOptions{
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairBody := []byte(`{
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-local-web",
		"app_name":"Local Web",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)
	pair := localWebJSON(t, handler, http.MethodPost, "/api/local/pair", pairBody)
	if pair["machine_id"] != session.MachineID {
		t.Fatalf("expected pair machine_id %q, got %#v", session.MachineID, pair)
	}
	if pair["machine_public_key_fingerprint"] != session.MachinePublicKeyFingerprint {
		t.Fatalf("expected pair machine fingerprint %q, got %#v", session.MachinePublicKeyFingerprint, pair)
	}
	if pair["expires_at"] == "" {
		t.Fatalf("expected pair expiration in response: %#v", pair)
	}
	if pair["machine_private_key"] != nil {
		t.Fatalf("local pair must not expose machine private key: %#v", pair)
	}
	if pair["app_certificate"] == nil {
		t.Fatalf("expected app_certificate in pair response: %#v", pair)
	}
}

func TestLocalRTCAnswerSeparatesTerminalManagementCapabilityFromFileManager(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-web-policy-machine",
	}))
	defer srv.Shutdown(context.Background())

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	defer func() { _, _ = srv.RemoteLocalDisable(context.Background()) }()

	session, appCert, appPrivate := claimLocalRTCAppCertificateHTTP(t, srv, status.LocalPairURL, []string{"terminal_management"})
	offerSDP := newLocalOfferSDP(t, "api")
	offer := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-management-only", session.MachineID, "", offerSDP, "nonce-management-only", time.Now().UTC())
	resp, err := http.Post(status.HTTPURL+"/api/local/rtc/offer", "application/json", bytes.NewReader(offer))
	if err != nil {
		t.Fatalf("POST local rtc offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected terminal management only certificate to create machine api session, got %d: %s", resp.StatusCode, body)
	}
	var rtcResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rtcResp); err != nil {
		t.Fatalf("decode rtc response: %v", err)
	}
	if got := rtcResp["data_channels"].([]any); len(got) != 1 || got[0] != "api" {
		t.Fatalf("expected api-only data channels for management-only session, got %#v", rtcResp["data_channels"])
	}

	fileOnlySession, fileOnlyCert, fileOnlyPrivate := claimLocalRTCAppCertificateHTTP(t, srv, status.LocalPairURL, []string{"file_manager"})
	fileOnlySDP := newLocalOfferSDP(t, "api")
	fileOnlyOffer := signedLocalRTCOfferBody(t, fileOnlyCert, fileOnlyPrivate, "rtc-file-only", fileOnlySession.MachineID, "", fileOnlySDP, "nonce-file-only", time.Now().UTC())
	resp, err = http.Post(status.HTTPURL+"/api/local/rtc/offer", "application/json", bytes.NewReader(fileOnlyOffer))
	if err != nil {
		t.Fatalf("POST file-only local rtc offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("file_manager-only certificate must not authorize machine terminal management api session")
	}
}

func TestE2ERemoteLocalWebHandlerUpdatesTerminalWithoutExistingTags(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-web-update-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-web-update-shell",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}

	handler := srv.LocalWebHandler(LocalWebOptions{
		HTTPURL:      "http://127.0.0.1:7342",
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		Assets: NewLocalWebStaticAssets(map[string]string{
			"index.html": "<!doctype html><title>TermX Local</title>",
		}),
	})

	body := []byte(`{
		"name":"renamed-shell",
		"cwd":"/tmp",
		"environment":"dev",
		"size_lock_mode":"off"
	}`)
	updated := localWebJSON(t, handler, http.MethodPatch, "/api/local/terminals/"+term.ID, body)
	if updated["name"] != "renamed-shell" {
		t.Fatalf("expected renamed terminal, got %#v", updated)
	}
	if updated["cwd"] != "/tmp" {
		t.Fatalf("expected cwd /tmp, got %#v", updated)
	}
	if updated["environment"] != "dev" {
		t.Fatalf("expected environment dev, got %#v", updated)
	}

	info, err := srv.Get(context.Background(), term.ID)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if info.Name != "renamed-shell" {
		t.Fatalf("expected persisted name renamed-shell, got %#v", info)
	}
	if info.Tags["termx.cwd"] != "/tmp" {
		t.Fatalf("expected persisted cwd tag, got %#v", info.Tags)
	}
	if info.Tags["termx.environment"] != "dev" {
		t.Fatalf("expected persisted environment tag, got %#v", info.Tags)
	}
}

func TestE2ERemoteLocalEnableStatusAndDisable(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-management-test",
	}))
	defer srv.Shutdown(context.Background())

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	if !status.Enabled || status.HTTPURL == "" || status.LocalPairURL == "" || !status.ICETCPEnabled || status.ICETCPPort == 0 {
		t.Fatalf("unexpected local enable status: %#v", status)
	}
	if strings.Contains(strings.ToLower(status.HTTPURL), "turn") || strings.Contains(strings.ToLower(status.LocalPairURL), "turn") {
		t.Fatalf("local status must not expose TURN credentials: %#v", status)
	}

	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(status.HTTPURL + "/api/local/status")
	if err != nil {
		t.Fatalf("GET local status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected local status 200, got %d: %s", resp.StatusCode, string(body))
	}

	current := srv.RemoteLocalStatus()
	if current.HTTPURL != status.HTTPURL || current.LocalPairURL != status.LocalPairURL {
		t.Fatalf("unexpected current local status: %#v", current)
	}
	disabled, err := srv.RemoteLocalDisable(context.Background())
	if err != nil {
		t.Fatalf("RemoteLocalDisable returned error: %v", err)
	}
	if disabled.Enabled || disabled.HTTPURL != "" || disabled.ICETCPEnabled {
		t.Fatalf("expected disabled local status, got %#v", disabled)
	}
	if _, err := client.Get(status.HTTPURL + "/api/local/status"); err == nil {
		t.Fatal("expected local web to stop after disable")
	}
}

func TestRemoteLocalEnableFailureKeepsExistingRuntime(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-management-test",
	}))
	defer srv.Shutdown(context.Background())

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}

	blocker, err := StartLocalICETCPMux(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartLocalICETCPMux blocker returned error: %v", err)
	}
	defer blocker.Close()
	blockerEndpoint := blocker.Endpoint()
	blockedAddr := blockerEndpoint.Host + ":" + blockerEndpoint.PortString()
	if _, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   blockedAddr,
	}); err == nil {
		t.Fatal("expected reconfigure to fail on already-bound ICE TCP address")
	}

	current := srv.RemoteLocalStatus()
	if !current.Enabled || current.HTTPURL != status.HTTPURL || current.ICETCPAddr != status.ICETCPAddr {
		t.Fatalf("failed reconfigure should keep existing runtime, got %#v want %#v", current, status)
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(status.HTTPURL + "/api/local/status")
	if err != nil {
		t.Fatalf("existing local web stopped after failed reconfigure: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected existing local web 200 after failed reconfigure, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestE2ERemoteLocalDisableClosesActiveRTCSessions(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-disable-rtc-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-disable-rtc-shell",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	session, appCert, appPrivate := claimLocalRTCAppCertificateHTTP(t, srv, status.LocalPairURL, []string{"terminal", "file_manager"})

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()
	terminalDC, err := offerPC.CreateDataChannel("terminal:"+term.ID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel terminal returned error: %v", err)
	}
	terminalOpen := make(chan struct{})
	terminalClosed := make(chan struct{})
	terminalDC.OnOpen(func() {
		select {
		case <-terminalOpen:
		default:
			close(terminalOpen)
		}
	})
	terminalDC.OnClose(func() {
		select {
		case <-terminalClosed:
		default:
			close(terminalClosed)
		}
	})
	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	body := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-disable", session.MachineID, term.ID, offerPC.LocalDescription().SDP, "nonce-disable", time.Now().UTC())
	var rtcResp map[string]any
	localWebHTTPDecode(t, status.HTTPURL+"/api/local/rtc/offer", body, &rtcResp)
	answer := rtcResp["answer"].(map[string]any)
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer["sdp"].(string),
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}
	select {
	case <-terminalOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for local RTC terminal channel to open")
	}
	if _, err := srv.RemoteLocalDisable(context.Background()); err != nil {
		t.Fatalf("RemoteLocalDisable returned error: %v", err)
	}
	select {
	case <-terminalClosed:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for local RTC terminal channel to close after disable")
	}
}

func TestE2ERemoteLocalWebHandlerAnswersAuthenticatedRTCOffer(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-rtc-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-rtc-shell",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}
	otherTerm, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-rtc-other-shell",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("Create second terminal returned error: %v", err)
	}

	iceMux, err := StartLocalICETCPMux(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("StartLocalICETCPMux returned error: %v", err)
	}
	defer iceMux.Close()
	handler := srv.LocalWebHandler(LocalWebOptions{
		HTTPURL:      "http://127.0.0.1:7342",
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		ICETCPMux:    iceMux,
	})

	session, err := srv.RemotePairStart(PairStartOptions{
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairBody := []byte(`{
		"pair_session_id":"` + session.PairSessionID + `",
		"pair_secret":"` + session.PairSecret + `",
		"app_device_id":"app-local-rtc",
		"app_name":"Local RTC Test",
		"app_public_key":"` + base64.StdEncoding.EncodeToString(appPublic) + `",
		"requested_capabilities":["terminal","file_manager"]
	}`)
	var pair struct {
		AppCertificate remotecert.AppCertificateEnvelope `json:"app_certificate"`
	}
	localWebDecode(t, handler, http.MethodPost, "/api/local/pair", pairBody, &pair)

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()
	if _, err := offerPC.CreateDataChannel("api", nil); err != nil {
		t.Fatalf("CreateDataChannel returned error: %v", err)
	}
	terminalDC, err := offerPC.CreateDataChannel("terminal:"+term.ID, nil)
	if err != nil {
		t.Fatalf("CreateDataChannel terminal returned error: %v", err)
	}
	terminalOpen := make(chan struct{})
	terminalDC.OnOpen(func() {
		select {
		case <-terminalOpen:
		default:
			close(terminalOpen)
		}
	})
	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	now := time.Now().UTC()
	fields := remotertc.OfferSignatureFields{
		MachineID:  session.MachineID,
		TerminalID: term.ID,
		SDP:        offerPC.LocalDescription().SDP,
		Nonce:      "nonce-local-rtc",
		Timestamp:  now,
	}
	signature := remotertc.OfferSignature{
		Algorithm: "ed25519",
		Nonce:     fields.Nonce,
		Timestamp: fields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, remotertc.CanonicalOfferSignatureMessage(fields))),
	}
	bodyBytes, err := json.Marshal(map[string]any{
		"app_certificate": pair.AppCertificate,
		"offer": map[string]any{
			"session_id":     "rtc-local-1",
			"machine_id":     session.MachineID,
			"terminal_id":    term.ID,
			"sdp":            offerPC.LocalDescription().SDP,
			"ice_candidates": []string{},
		},
		"signature": signature,
		"client": map[string]string{
			"type":    "browser",
			"purpose": "runtime",
		},
	})
	if err != nil {
		t.Fatalf("Marshal RTC body returned error: %v", err)
	}

	rtcResp := localWebJSON(t, handler, http.MethodPost, "/api/local/rtc/offer", bodyBytes)
	if rtcResp["ice_tcp_enabled"] != true {
		t.Fatalf("expected ICE TCP enabled in RTC response: %#v", rtcResp)
	}
	answer, ok := rtcResp["answer"].(map[string]any)
	if !ok || answer["sdp"] == "" {
		t.Fatalf("expected RTC answer SDP, got %#v", rtcResp)
	}
	raw, _ := json.Marshal(rtcResp)
	if strings.Contains(strings.ToLower(string(raw)), "turn:") || strings.Contains(strings.ToLower(string(raw)), "turns:") {
		t.Fatalf("local RTC response must not expose TURN credentials: %s", raw)
	}
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer["sdp"].(string),
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}
	select {
	case <-terminalOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for signed terminal data channel to open")
	}
	clientTransport := bridge.NewDataChannelTransport(terminalDC)
	defer clientTransport.Close()
	client := protocol.NewClient(clientTransport)
	defer client.Close()
	clientCtx, cancelClient := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClient()
	if err := client.Hello(clientCtx, protocol.Hello{Version: protocol.Version, Client: "local-rtc-test"}); err != nil {
		t.Fatalf("Hello over signed local RTC terminal channel returned error: %v", err)
	}
	if _, err := client.Snapshot(clientCtx, term.ID, 0, 0); err != nil {
		t.Fatalf("Snapshot for signed terminal returned error: %v", err)
	}
	if _, err := client.Snapshot(clientCtx, otherTerm.ID, 0, 0); err == nil {
		t.Fatalf("signed local RTC terminal channel for %q must not access other terminal %q", term.ID, otherTerm.ID)
	}
	if _, err := client.Events(clientCtx, protocol.EventsParams{SessionID: "main"}); err == nil {
		t.Fatalf("signed local RTC terminal channel for %q must not subscribe to session-only events", term.ID)
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/api/local/rtc/offer", bytes.NewReader(bodyBytes))
	replayRec := httptest.NewRecorder()
	handler.ServeHTTP(replayRec, replayReq)
	if replayRec.Code != http.StatusBadRequest {
		t.Fatalf("expected replayed RTC offer to be rejected, got %d body=%q", replayRec.Code, replayRec.Body.String())
	}
}

func TestE2ERemoteLocalWebHandlerAnswersMachineInventoryEventsOffer(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-rtc-events-machine",
	}))
	defer srv.Shutdown(context.Background())

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	session, appCert, appPrivate := claimLocalRTCAppCertificateHTTP(t, srv, status.LocalPairURL, []string{"terminal", "file_manager"})

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()
	eventsDC, err := offerPC.CreateDataChannel("events", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(events) returned error: %v", err)
	}
	eventsOpen := make(chan struct{})
	eventsDC.OnOpen(func() {
		select {
		case <-eventsOpen:
		default:
			close(eventsOpen)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	body := signedLocalRTCOfferBodyWithPurpose(t, appCert, appPrivate, "rtc-events", session.MachineID, "", offerPC.LocalDescription().SDP, "nonce-events", time.Now().UTC(), "inventory_events")
	var rtcResp map[string]any
	localWebHTTPDecode(t, status.HTTPURL+"/api/local/rtc/offer", body, &rtcResp)
	if got := rtcResp["data_channels"].([]any); len(got) != 1 || got[0] != "events" {
		t.Fatalf("expected events-only data channels, got %#v", rtcResp["data_channels"])
	}
	answer := rtcResp["answer"].(map[string]any)
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer["sdp"].(string),
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}
	select {
	case <-eventsOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for machine events data channel to open")
	}

	clientTransport := bridge.NewDataChannelTransport(eventsDC)
	defer clientTransport.Close()
	client := protocol.NewClient(clientTransport)
	defer client.Close()
	clientCtx, cancelClient := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClient()
	if err := client.Hello(clientCtx, protocol.Hello{Version: protocol.Version, Client: "local-rtc-events-test"}); err != nil {
		t.Fatalf("Hello over machine events channel returned error: %v", err)
	}
	clientEvents, err := client.Events(clientCtx, protocol.EventsParams{Types: []protocol.EventType{
		protocol.EventTerminalCreated,
		protocol.EventTerminalRemoved,
		protocol.EventTerminalMetadataChanged,
	}})
	if err != nil {
		t.Fatalf("Events subscribe over machine events channel returned error: %v", err)
	}
	serverEventsCtx, cancelServerEvents := context.WithCancel(context.Background())
	defer cancelServerEvents()
	serverEvents := srv.Events(serverEventsCtx, WithTypeFilter(EventTerminalCreated))
	created, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "events-created-terminal",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}
	select {
	case evt := <-serverEvents:
		if evt.Type != EventTerminalCreated || evt.TerminalID != created.ID {
			t.Fatalf("unexpected server event bus event: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server event bus create event")
	}

	select {
	case evt := <-clientEvents:
		if evt.Type != protocol.EventTerminalCreated || evt.TerminalID != created.ID {
			t.Fatalf("unexpected machine inventory event: %#v", evt)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for machine inventory create event")
	}
}

func TestE2ERemoteLocalWebHandlerPushesMetadataChangeOverMachineInventoryEvents(t *testing.T) {
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    t.TempDir(),
		DeviceName: "local-rtc-events-metadata-machine",
	}))
	defer srv.Shutdown(context.Background())

	status, err := srv.RemoteLocalEnable(context.Background(), RemoteLocalOptions{
		LocalWebAddr: "127.0.0.1:0",
		ICETCPAddr:   "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("RemoteLocalEnable returned error: %v", err)
	}
	session, appCert, appPrivate := claimLocalRTCAppCertificateHTTP(t, srv, status.LocalPairURL, []string{"terminal", "file_manager"})

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer offerPC.Close()
	eventsDC, err := offerPC.CreateDataChannel("events", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel(events) returned error: %v", err)
	}
	eventsOpen := make(chan struct{})
	eventsDC.OnOpen(func() {
		select {
		case <-eventsOpen:
		default:
			close(eventsOpen)
		}
	})

	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := offerPC.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, offerPC, 5*time.Second)

	body := signedLocalRTCOfferBodyWithPurpose(t, appCert, appPrivate, "rtc-events-metadata", session.MachineID, "", offerPC.LocalDescription().SDP, "nonce-events-metadata", time.Now().UTC(), "inventory_events")
	var rtcResp map[string]any
	localWebHTTPDecode(t, status.HTTPURL+"/api/local/rtc/offer", body, &rtcResp)
	answer := rtcResp["answer"].(map[string]any)
	if err := offerPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer["sdp"].(string),
	}); err != nil {
		t.Fatalf("SetRemoteDescription returned error: %v", err)
	}
	select {
	case <-eventsOpen:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for machine events data channel to open")
	}

	clientTransport := bridge.NewDataChannelTransport(eventsDC)
	defer clientTransport.Close()
	client := protocol.NewClient(clientTransport)
	defer client.Close()
	clientCtx, cancelClient := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelClient()
	if err := client.Hello(clientCtx, protocol.Hello{Version: protocol.Version, Client: "local-rtc-events-metadata-test"}); err != nil {
		t.Fatalf("Hello over machine events channel returned error: %v", err)
	}
	clientEvents, err := client.Events(clientCtx, protocol.EventsParams{Types: []protocol.EventType{
		protocol.EventTerminalMetadataChanged,
	}})
	if err != nil {
		t.Fatalf("Events subscribe over machine events channel returned error: %v", err)
	}
	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "events-metadata-terminal",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}
	if err := srv.SetTags(context.Background(), term.ID, map[string]string{"termx.environment": "prod"}); err != nil {
		t.Fatalf("SetTags returned error: %v", err)
	}

	for {
		select {
		case evt := <-clientEvents:
			if evt.Type == protocol.EventTerminalMetadataChanged && evt.TerminalID == term.ID {
				return
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for machine inventory metadata event")
		}
	}
}

func TestE2ERemoteLocalWebHandlerRejectsInvalidRTCOfferAuth(t *testing.T) {
	dataDir := t.TempDir()
	srv := NewServer(WithRemoteConfig(RemoteConfig{
		Enabled:    true,
		DataDir:    dataDir,
		DeviceName: "local-rtc-auth-machine",
	}))
	defer srv.Shutdown(context.Background())

	term, err := srv.Create(context.Background(), CreateOptions{
		Command: []string{"sh", "-c", "sleep 5"},
		Name:    "local-rtc-auth-shell",
		Size:    Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("pty not permitted in this environment: %v", err)
		}
		t.Fatalf("Create returned error: %v", err)
	}
	handler := srv.LocalWebHandler(LocalWebOptions{
		HTTPURL:      "http://127.0.0.1:7342",
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
	})

	t.Run("nonexistent_terminal", func(t *testing.T) {
		session, appCert, appPrivate := claimLocalRTCAppCertificate(t, srv, handler, []string{"terminal", "file_manager"})
		sdp := newLocalOfferSDP(t, "api")
		body := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-missing-terminal", session.MachineID, "missing-terminal", sdp, "nonce-missing-terminal", time.Now().UTC())
		localWebExpectStatus(t, handler, http.MethodPost, "/api/local/rtc/offer", body, http.StatusBadRequest)
	})

	t.Run("stale_signature_timestamp", func(t *testing.T) {
		session, appCert, appPrivate := claimLocalRTCAppCertificate(t, srv, handler, []string{"terminal", "file_manager"})
		sdp := newLocalOfferSDP(t, "api")
		body := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-stale", session.MachineID, term.ID, sdp, "nonce-stale", time.Now().UTC().Add(-10*time.Minute))
		localWebExpectStatus(t, handler, http.MethodPost, "/api/local/rtc/offer", body, http.StatusBadRequest)
	})

	t.Run("terminal_only_certificate", func(t *testing.T) {
		session, appCert, appPrivate := claimLocalRTCAppCertificate(t, srv, handler, []string{"terminal"})
		sdp := newLocalOfferSDP(t, "api")
		body := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-terminal-only", session.MachineID, term.ID, sdp, "nonce-terminal-only", time.Now().UTC())
		localWebExpectStatus(t, handler, http.MethodPost, "/api/local/rtc/offer", body, http.StatusBadRequest)
	})

	t.Run("wrong_local_machine_id", func(t *testing.T) {
		appPublic, appPrivate, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatalf("GenerateKey returned error: %v", err)
		}
		machineKey, err := remoteidentity.LoadOrCreateMachineKey(dataDir)
		if err != nil {
			t.Fatalf("LoadOrCreateMachineKey returned error: %v", err)
		}
		now := time.Now().UTC()
		appCert, err := remotecert.SignAppCertificate(remotecert.AppCertificatePayload{
			Version:      1,
			CertID:       "cert-wrong-machine",
			MachineID:    "wrong-local-machine",
			AppDeviceID:  "app-wrong-machine",
			AppPublicKey: base64.StdEncoding.EncodeToString(appPublic),
			AppName:      "Wrong Machine Test",
			Capabilities: []string{
				"terminal",
				"file_manager",
			},
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Hour),
		}, machineKey)
		if err != nil {
			t.Fatalf("SignAppCertificate returned error: %v", err)
		}
		sdp := newLocalOfferSDP(t, "api")
		body := signedLocalRTCOfferBody(t, appCert, appPrivate, "rtc-wrong-machine", "wrong-local-machine", term.ID, sdp, "nonce-wrong-machine", now)
		localWebExpectStatus(t, handler, http.MethodPost, "/api/local/rtc/offer", body, http.StatusBadRequest)
	})
}

func claimLocalRTCAppCertificate(t *testing.T, srv *Server, handler http.Handler, capabilities []string) (protocol.PairStartResult, remotecert.AppCertificateEnvelope, ed25519.PrivateKey) {
	t.Helper()
	session, err := srv.RemotePairStart(PairStartOptions{
		LocalPairURL: "http://127.0.0.1:7342/api/local/pair",
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairBody, err := json.Marshal(map[string]any{
		"pair_session_id":        session.PairSessionID,
		"pair_secret":            session.PairSecret,
		"app_device_id":          "app-local-rtc",
		"app_name":               "Local RTC Test",
		"app_public_key":         base64.StdEncoding.EncodeToString(appPublic),
		"requested_capabilities": capabilities,
	})
	if err != nil {
		t.Fatalf("Marshal pair body returned error: %v", err)
	}
	var pair struct {
		AppCertificate remotecert.AppCertificateEnvelope `json:"app_certificate"`
	}
	localWebDecode(t, handler, http.MethodPost, "/api/local/pair", pairBody, &pair)
	return session, pair.AppCertificate, appPrivate
}

func claimLocalRTCAppCertificateHTTP(t *testing.T, srv *Server, localPairURL string, capabilities []string) (protocol.PairStartResult, remotecert.AppCertificateEnvelope, ed25519.PrivateKey) {
	t.Helper()
	session, err := srv.RemotePairStart(PairStartOptions{
		LocalPairURL: localPairURL,
		TTL:          time.Minute,
	})
	if err != nil {
		t.Fatalf("RemotePairStart returned error: %v", err)
	}
	appPublic, appPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	pairBody, err := json.Marshal(map[string]any{
		"pair_session_id":        session.PairSessionID,
		"pair_secret":            session.PairSecret,
		"app_device_id":          "app-local-rtc",
		"app_name":               "Local RTC Test",
		"app_public_key":         base64.StdEncoding.EncodeToString(appPublic),
		"requested_capabilities": capabilities,
	})
	if err != nil {
		t.Fatalf("Marshal pair body returned error: %v", err)
	}
	var pair struct {
		AppCertificate remotecert.AppCertificateEnvelope `json:"app_certificate"`
	}
	localWebHTTPDecode(t, localPairURL, pairBody, &pair)
	return session, pair.AppCertificate, appPrivate
}

func newLocalOfferSDP(t *testing.T, labels ...string) string {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection returned error: %v", err)
	}
	defer pc.Close()
	for _, label := range labels {
		if _, err := pc.CreateDataChannel(label, nil); err != nil {
			t.Fatalf("CreateDataChannel %q returned error: %v", label, err)
		}
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer returned error: %v", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription returned error: %v", err)
	}
	waitPeerICE(t, pc, 5*time.Second)
	return pc.LocalDescription().SDP
}

func signedLocalRTCOfferBody(
	t *testing.T,
	appCert remotecert.AppCertificateEnvelope,
	appPrivate ed25519.PrivateKey,
	sessionID string,
	machineID string,
	terminalID string,
	sdp string,
	nonce string,
	timestamp time.Time,
) []byte {
	return signedLocalRTCOfferBodyWithPurpose(t, appCert, appPrivate, sessionID, machineID, terminalID, sdp, nonce, timestamp, "runtime")
}

func signedLocalRTCOfferBodyWithPurpose(
	t *testing.T,
	appCert remotecert.AppCertificateEnvelope,
	appPrivate ed25519.PrivateKey,
	sessionID string,
	machineID string,
	terminalID string,
	sdp string,
	nonce string,
	timestamp time.Time,
	purpose string,
) []byte {
	t.Helper()
	fields := remotertc.OfferSignatureFields{
		MachineID:  machineID,
		TerminalID: terminalID,
		SDP:        sdp,
		Nonce:      nonce,
		Timestamp:  timestamp,
	}
	signature := remotertc.OfferSignature{
		Algorithm: "ed25519",
		Nonce:     fields.Nonce,
		Timestamp: fields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, remotertc.CanonicalOfferSignatureMessage(fields))),
	}
	bodyBytes, err := json.Marshal(map[string]any{
		"app_certificate": appCert,
		"offer": map[string]any{
			"session_id":     sessionID,
			"machine_id":     machineID,
			"terminal_id":    terminalID,
			"sdp":            sdp,
			"ice_candidates": []string{},
		},
		"signature": signature,
		"client": map[string]string{
			"type":    "browser",
			"purpose": purpose,
		},
	})
	if err != nil {
		t.Fatalf("Marshal RTC body returned error: %v", err)
	}
	return bodyBytes
}

func localWebExpectStatus(t *testing.T, handler http.Handler, method, path string, body []byte, want int) string {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s expected status %d, got %d body=%q", method, path, want, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func localWebJSON(t *testing.T, handler http.Handler, method, path string, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	localWebDecode(t, handler, method, path, body, &out)
	return out
}

func localWebDecode(t *testing.T, handler http.Handler, method, path string, body []byte, out any) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s expected 200, got %d body=%q", method, path, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("decode %s %s response: %v\n%s", method, path, err, rec.Body.String())
	}
}

func localWebHTTPDecode(t *testing.T, rawURL string, body []byte, out any) {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(rawURL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s returned error: %v", rawURL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", rawURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s expected 200, got %d body=%q", rawURL, resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		t.Fatalf("decode %s response: %v\n%s", rawURL, err, string(respBody))
	}
}
