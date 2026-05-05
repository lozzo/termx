package rtc

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/cert"
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
		Candidates: []string{"candidate:host-a", " candidate:host-b "},
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
		"sha256(candidates):",
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

func TestCanonicalOfferSignatureMessageMatchesRemoteUIContract(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	message := string(CanonicalOfferSignatureMessage(OfferSignatureFields{
		MachineID:  "machine-local",
		TerminalID: "terminal-1",
		SDP:        "v=0\r\ns=termx\r\n",
		Candidates: []string{"candidate:host-a", "candidate:host-b"},
		Nonce:      "nonce-1",
		Timestamp:  now,
	}))

	want := strings.Join([]string{
		"termx-webrtc-offer-v1:",
		"ticket_id:",
		"machine_id:machine-local",
		"terminal_id:terminal-1",
		"sha256(sdp):dd33fcfb47f1bcefb7e8f57c03aa4778c5f7e2490f14259f9b892c05d0aa0158",
		"sha256(candidates):a00196786082bed059a8712b03f5355783521d39a724846935ae8deaa9a6cd96",
		"nonce:nonce-1",
		"timestamp:1777631400",
	}, "\n")
	if message != want {
		t.Fatalf("canonical offer mismatch\nwant: %q\n got: %q", want, message)
	}
}

func TestCanonicalOfferSignatureMessageMatchesRemoteUIJSONEscapingContract(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	message := string(CanonicalOfferSignatureMessage(OfferSignatureFields{
		MachineID:  "machine-local",
		TerminalID: "terminal-1",
		SDP:        "v=0\r\ns=termx\r\n",
		Candidates: []string{"candidate:<host>&"},
		Nonce:      "nonce-1",
		Timestamp:  now,
	}))

	if !strings.Contains(message, "sha256(candidates):8cd5bdbbbe5fc4e653636ccd241028b014e95153072e56d6ffa6aff2bef9ef0e") {
		t.Fatalf("candidate JSON escaping hash does not match remote-ui contract: %q", message)
	}
}

func TestCanonicalOfferSignatureMessagePreservesCandidateListBoundaries(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 30, 0, 0, time.UTC)
	oneCandidate := string(CanonicalOfferSignatureMessage(OfferSignatureFields{
		MachineID:  "machine-local",
		TerminalID: "terminal-1",
		SDP:        "v=0\r\ns=termx\r\n",
		Candidates: []string{"candidate:a\ncandidate:b"},
		Nonce:      "nonce-1",
		Timestamp:  now,
	}))
	twoCandidates := string(CanonicalOfferSignatureMessage(OfferSignatureFields{
		MachineID:  "machine-local",
		TerminalID: "terminal-1",
		SDP:        "v=0\r\ns=termx\r\n",
		Candidates: []string{"candidate:a", "candidate:b"},
		Nonce:      "nonce-1",
		Timestamp:  now,
	}))

	if oneCandidate == twoCandidates {
		t.Fatalf("candidate canonicalization collapsed list boundaries: %q", oneCandidate)
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
		Candidates: []string{"candidate:host-a"},
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
	tampered = fields
	tampered.Candidates = []string{"candidate:host-a", "candidate:tampered"}
	if err := VerifyOfferSignature(signature, tampered, appPublic, cert.NewReplayWindow(5*time.Minute), now); err == nil {
		t.Fatal("expected tampered ICE candidates to be rejected")
	}

	signature.Algorithm = "rsa"
	if err := VerifyOfferSignature(signature, fields, appPublic, cert.NewReplayWindow(5*time.Minute), now); err == nil {
		t.Fatal("expected unsupported signature algorithm to be rejected")
	}
}
