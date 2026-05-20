package token_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/session/token"
)

var secret = make([]byte, 32)

func baseClaims() token.Claims {
	now := time.Now()
	return token.Claims{
		SessionID:    "sid1",
		MachineID:    "mid1",
		Capabilities: []string{"terminal"},
		IssuedAt:     now.Unix(),
		ExpiresAt:    now.Add(time.Hour).Unix(),
	}
}

func TestIssueAndVerify(t *testing.T) {
	tok, err := token.Issue(secret, baseClaims())
	if err != nil {
		t.Fatal(err)
	}
	got, err := token.Verify(tok, secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got.MachineID != "mid1" {
		t.Fatalf("machine_id: %s", got.MachineID)
	}
}

func TestIssueRejectsShortSecret(t *testing.T) {
	if _, err := token.Issue(make([]byte, 31), baseClaims()); err == nil {
		t.Fatal("expected short secret error")
	}
}

func TestVerifyExpired(t *testing.T) {
	c := baseClaims()
	c.ExpiresAt = time.Now().Add(-time.Hour).Unix()
	tok, err := token.Issue(secret, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := token.Verify(tok, secret, time.Now()); err == nil {
		t.Fatal("expected expired error")
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	tok, err := token.Issue(secret, baseClaims())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(tok, ".", 2)
	if _, err := token.Verify("dGFtcGVyZWQ."+parts[1], secret, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	tok, err := token.Issue(secret, baseClaims())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(tok, ".", 2)
	mac, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	mac[0] ^= 0xff
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(mac)
	if _, err := token.Verify(tampered, secret, time.Now()); err == nil {
		t.Fatal("expected signature error")
	}
}

func TestVerifyRejectsSignedDamagedPayload(t *testing.T) {
	damagedPayload := base64.RawURLEncoding.EncodeToString([]byte("{"))
	tok := damagedPayload + "." + signPayloadForTest(secret, damagedPayload)
	if _, err := token.Verify(tok, secret, time.Now()); err == nil {
		t.Fatal("expected payload decode error")
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	tok, err := token.Issue(secret, baseClaims())
	if err != nil {
		t.Fatal(err)
	}
	wrong := make([]byte, 32)
	wrong[0] = 0xff
	if _, err := token.Verify(tok, wrong, time.Now()); err == nil {
		t.Fatal("expected error")
	}
}

func TestCapabilitiesSorted(t *testing.T) {
	c := baseClaims()
	c.Capabilities = []string{"terminal_management", "file_manager", "terminal"}
	c.Paths = []string{"local", "hub"}
	tok, err := token.Issue(secret, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := token.Verify(tok, secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got.Capabilities); i++ {
		if got.Capabilities[i] < got.Capabilities[i-1] {
			t.Fatal("capabilities not sorted")
		}
	}
	for i := 1; i < len(got.Paths); i++ {
		if got.Paths[i] < got.Paths[i-1] {
			t.Fatal("paths not sorted")
		}
	}
}

func TestAnswerProofKeySealedIntoClaims(t *testing.T) {
	c := baseClaims()
	sealed, err := token.SealAnswerProofKey(secret, c, "answer-proof-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.AnswerProofKey = sealed

	tok, err := token.Issue(secret, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := token.Verify(tok, secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := token.OpenAnswerProofKey(secret, got)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "answer-proof-secret" {
		t.Fatalf("proof secret = %q", opened)
	}
}

func TestAnswerProofKeyAADBindsSessionAndMachine(t *testing.T) {
	c := baseClaims()
	sealed, err := token.SealAnswerProofKey(secret, c, "answer-proof-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.AnswerProofKey = sealed
	c.MachineID = "other-machine"
	if _, err := token.OpenAnswerProofKey(secret, c); err == nil {
		t.Fatal("expected answer proof key open to fail after machine change")
	}

	c = baseClaims()
	sealed, err = token.SealAnswerProofKey(secret, c, "answer-proof-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.AnswerProofKey = sealed
	c.SessionID = "other-session"
	if _, err := token.OpenAnswerProofKey(secret, c); err == nil {
		t.Fatal("expected answer proof key open to fail after session change")
	}
}

func TestComputeAnswerProof(t *testing.T) {
	c := baseClaims()
	sealed, err := token.SealAnswerProofKey(secret, c, "answer-proof-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.AnswerProofKey = sealed

	got, err := token.ComputeAnswerProof(secret, c, "rtc-session-1", "challenge-1")
	if err != nil {
		t.Fatal(err)
	}
	want := answerProofForTest("answer-proof-secret", c.SessionID, "rtc-session-1", "challenge-1")
	if got != want {
		t.Fatalf("answer proof = %q, want %q", got, want)
	}
}

func TestComputeAnswerProofRejectsMissingChallenge(t *testing.T) {
	c := baseClaims()
	sealed, err := token.SealAnswerProofKey(secret, c, "answer-proof-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.AnswerProofKey = sealed
	if _, err := token.ComputeAnswerProof(secret, c, "rtc-session-1", " "); err == nil {
		t.Fatal("expected missing challenge error")
	}
}

func TestComputeAnswerProofRejectsInvalidProofKey(t *testing.T) {
	c := baseClaims()
	if _, err := token.ComputeAnswerProof(secret, c, "rtc-session-1", "challenge-1"); err == nil {
		t.Fatal("expected missing proof key error")
	}
}

func signPayloadForTest(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("termx-session-v1:"))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func answerProofForTest(proofKey string, pairSessionID string, rtcSessionID string, challenge string) string {
	mac := hmac.New(sha256.New, []byte(proofKey))
	mac.Write([]byte("termx-answer-proof-v1:"))
	mac.Write([]byte(strings.TrimSpace(pairSessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.TrimSpace(rtcSessionID)))
	mac.Write([]byte(":"))
	mac.Write([]byte(strings.TrimSpace(challenge)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
