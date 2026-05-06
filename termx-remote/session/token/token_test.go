package token_test

import (
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
}
