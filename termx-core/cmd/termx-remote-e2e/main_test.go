package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	remotecert "github.com/lozzow/termx/termx-core/internal/remote/cert"
	remotertc "github.com/lozzow/termx/termx-core/internal/remote/rtc"
	"github.com/pion/webrtc/v4"
)

func TestBuildManagedSessionRequestSignsTicketBoundOffer(t *testing.T) {
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	cert := remotecert.AppCertificateEnvelope{
		Payload: remotecert.AppCertificatePayload{
			Version:      1,
			MachineID:    "machine-1",
			AppPublicKey: base64.StdEncoding.EncodeToString(appPublic),
		},
		Signature: "cert-signature",
	}
	certJSON, err := json.Marshal(cert)
	if err != nil {
		t.Fatalf("marshal cert: %v", err)
	}
	now := time.Date(2026, 5, 3, 12, 10, 0, 0, time.UTC)
	req, err := buildManagedSessionRequest(managedSessionRequestInput{
		TicketID:       "ticket-1",
		MachineID:      "machine-1",
		TerminalID:     "terminal-1",
		SessionID:      "rtc-session-1",
		SDP:            minimalSDP("offer"),
		ICECandidates:  []string{"candidate:1 1 udp 1 192.0.2.1 1 typ host"},
		AppCertificate: certJSON,
		AppPrivateKey:  appPrivate,
		Nonce:          "nonce-1",
		Now:            now,
	})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	raw := string(payload)
	for _, want := range []string{`"connect_ticket":"ticket-1"`, `"machine_id":"machine-1"`, `"terminal_id":"terminal-1"`, `"app_certificate"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("request missing %s: %s", want, raw)
		}
	}
	for _, forbidden := range []string{"terminal_data", "file_data", `"path":"relay"`, "http_runtime", "websocket"} {
		if strings.Contains(strings.ToLower(raw), forbidden) {
			t.Fatalf("request leaked runtime/relay field %q: %s", forbidden, raw)
		}
	}
	if err := remotertc.VerifyOfferSignature(remotertc.OfferSignature{
		Algorithm: req.Signature.Algorithm,
		Nonce:     req.Signature.Nonce,
		Timestamp: req.Signature.Timestamp,
		Value:     req.Signature.Value,
	}, remotertc.OfferSignatureFields{
		TicketID:   "ticket-1",
		MachineID:  "machine-1",
		TerminalID: "terminal-1",
		SDP:        minimalSDP("offer"),
		Candidates: []string{"candidate:1 1 udp 1 192.0.2.1 1 typ host"},
	}, appPublic, nil, now); err != nil {
		t.Fatalf("request signature did not verify: %v", err)
	}
}

func TestApplyRemoteICEServersAcceptsManagedTurnConfig(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("new peer connection: %v", err)
	}
	defer pc.Close()

	if err := applyRemoteICEServers(pc, []hubICEServer{
		{URLs: []string{"stun:hub.termx.test:3478"}},
		{URLs: []string{"turn:hub.termx.test:3478?transport=udp"}, Username: "lease:1770000000", Credential: "credential"},
	}); err != nil {
		t.Fatalf("apply ice servers: %v", err)
	}
	got := pc.GetConfiguration().ICEServers
	if len(got) != 2 || got[1].Username != "lease:1770000000" || got[1].Credential != "credential" {
		t.Fatalf("peer connection ICE servers = %+v", got)
	}
}
