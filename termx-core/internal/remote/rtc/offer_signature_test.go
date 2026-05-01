package rtc

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
)

func TestOfferSignatureVerifiesCanonicalMessageAndRejectsReplay(t *testing.T) {
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	fields := OfferSignatureFields{
		TicketID:   "",
		MachineID:  "mach_local",
		TerminalID: "term_1",
		SDP:        "v=0\r\ns=termx\r\n",
		Nonce:      "nonce-1",
		Timestamp:  now,
	}
	message := CanonicalOfferSignatureMessage(fields)
	rawMessage := string(message)
	for _, want := range []string{
		"termx-webrtc-offer-v1:",
		"ticket_id:",
		"machine_id:mach_local",
		"terminal_id:term_1",
		"sha256(sdp):",
		"nonce:nonce-1",
		"timestamp:1777631400",
	} {
		if !strings.Contains(rawMessage, want) {
			t.Fatalf("expected canonical message to contain %q, got %q", want, rawMessage)
		}
	}
	if strings.Contains(rawMessage, fields.SDP) {
		t.Fatalf("canonical message must hash SDP instead of embedding it: %q", rawMessage)
	}
	signature := OfferSignature{
		Algorithm: "ed25519",
		Nonce:     fields.Nonce,
		Timestamp: fields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, message)),
	}
	replay := cert.NewReplayWindow(5 * time.Minute)
	if err := VerifyOfferSignature(signature, fields, appPublic, replay, now); err != nil {
		t.Fatalf("VerifyOfferSignature returned error: %v", err)
	}
	if err := VerifyOfferSignature(signature, fields, appPublic, replay, now); err == nil {
		t.Fatal("expected replayed nonce to be rejected")
	}
}

func TestOfferSignatureRejectsTamperedSDPAndUnsupportedAlgorithm(t *testing.T) {
	appPublic, appPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	fields := OfferSignatureFields{
		MachineID:  "mach_local",
		TerminalID: "term_1",
		SDP:        "v=0\r\ns=termx\r\n",
		Nonce:      "nonce-1",
		Timestamp:  now,
	}
	signature := OfferSignature{
		Algorithm: "ed25519",
		Nonce:     fields.Nonce,
		Timestamp: fields.Timestamp.Unix(),
		Value:     base64.StdEncoding.EncodeToString(ed25519.Sign(appPrivate, CanonicalOfferSignatureMessage(fields))),
	}

	tampered := fields
	tampered.SDP = "v=0\r\ns=tampered\r\n"
	if err := VerifyOfferSignature(signature, tampered, appPublic, cert.NewReplayWindow(5*time.Minute), now); err == nil {
		t.Fatal("expected tampered SDP to be rejected")
	}

	signature.Algorithm = "rsa"
	if err := VerifyOfferSignature(signature, fields, appPublic, cert.NewReplayWindow(5*time.Minute), now); err == nil {
		t.Fatal("expected unsupported signature algorithm to be rejected")
	}
}
