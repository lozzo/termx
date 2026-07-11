package remoteauth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPairingBundleRoundTripBindsDaemonIdentityAndScope(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := NewIdentity("device-pairing", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 11, 15, 0, 0, 0, time.UTC)
	bundle, err := IssuePairingBundle(identity, PairingIssueOptions{
		Label: "Lab daemon", Scope: Scope{AllowDaemon: true}, Lifetime: time.Hour, Now: now,
		Random: bytes.NewReader(bytes.Repeat([]byte{0x31}, 18)),
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := EncodePairingBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	parsed, claims, err := ParsePairingBundle(payload, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.DeviceID != identity.DeviceID || parsed.DeviceFingerprint != identity.Fingerprint || parsed.Label != "Lab daemon" || !claims.Scope.AllowDaemon || claims.IssuerDeviceID != identity.DeviceID {
		t.Fatalf("pairing round trip lost identity/scope: bundle=%#v claims=%#v", parsed, claims)
	}
}

func TestPairingBundleRejectsUnknownFieldsAndIdentityMismatch(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	identity, _ := NewIdentity("device-pairing", privateKey)
	now := time.Now().UTC()
	bundle, err := IssuePairingBundle(identity, PairingIssueOptions{Scope: Scope{TerminalID: "term-1"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := EncodePairingBundle(bundle)
	unknown := strings.Replace(string(payload), "\n}", ",\n  \"hub_url\": \"https://caller.invalid\"\n}", 1)
	if _, _, err := ParsePairingBundle([]byte(unknown), now); err == nil {
		t.Fatal("unknown caller-selected Hub field was accepted")
	}
	bundle.DeviceID = "device-other"
	payload, _ = json.Marshal(bundle)
	if _, _, err := ParsePairingBundle(payload, now); err == nil {
		t.Fatal("pairing device mismatch was accepted")
	}
}
