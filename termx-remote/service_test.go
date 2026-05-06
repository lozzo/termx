package remote

import (
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
)

func TestLocalStatusDisabledByDefault(t *testing.T) {
	service := NewService(remoteprotocol.Config{}, nil)

	status := service.LocalStatus()
	if status.Enabled {
		t.Fatalf("LocalStatus enabled by default: %+v", status)
	}
}

func TestPairStartUsesConfiguredTokenTTL(t *testing.T) {
	service := NewService(remoteprotocol.Config{
		Enabled:         true,
		DataDir:         t.TempDir(),
		DeviceName:      "token-ttl-device",
		TokenTTLSeconds: int((2 * time.Hour).Seconds()),
	}, nil)

	session, err := service.PairStart(remoteprotocol.PairStartParams{TTLSeconds: int(time.Minute.Seconds())})
	if err != nil {
		t.Fatalf("PairStart returned error: %v", err)
	}
	resp, err := service.pairClaim(t.Context(), pairClaimRequestForTest(session))
	if err != nil {
		t.Fatalf("pairClaim returned error: %v", err)
	}
	if got := resp.ExpiresAt.Sub(time.Now().UTC()); got < time.Hour || got > 3*time.Hour {
		t.Fatalf("expected token ttl around two hours, got expiry %s", resp.ExpiresAt)
	}
}

func pairClaimRequestForTest(session remoteprotocol.PairStartResult) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		RequestedCapabilities: []string{"terminal"},
	}
}
