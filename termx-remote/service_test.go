package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-core/protocol"
	"github.com/lozzow/termx/termx-core/transport"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
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
	if session.AnswerProofSecret == "" {
		t.Fatal("PairStart did not return answer proof secret")
	}
	resp, err := service.pairClaim(t.Context(), pairClaimRequestForTest(session))
	if err != nil {
		t.Fatalf("pairClaim returned error: %v", err)
	}
	if got := resp.ExpiresAt.Sub(time.Now().UTC()); got < time.Hour || got > 3*time.Hour {
		t.Fatalf("expected token ttl around two hours, got expiry %s", resp.ExpiresAt)
	}
}

func TestTerminalManagementCreateMarshalsProtocolTerminalID(t *testing.T) {
	daemon := &terminalManagementDaemonStub{}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "create",
		Path:   "create",
		Body:   json.RawMessage(`{"command":["/bin/zsh","-l"],"name":"ops shell"}`),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal management response: %v", err)
	}
	if payload["terminal_id"] != "terminal-created" {
		t.Fatalf("expected terminal_id in create response, got %s", string(body))
	}
	if _, ok := payload["ID"]; ok {
		t.Fatalf("create response must not use Go field name ID: %s", string(body))
	}
	if daemon.createName != "ops shell" {
		t.Fatalf("create request did not reach daemon, got name %q", daemon.createName)
	}
}

func pairClaimRequestForTest(session remoteprotocol.PairStartResult) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		RequestedCapabilities: []string{"terminal"},
	}
}

type terminalManagementDaemonStub struct {
	createName string
}

func (d *terminalManagementDaemonStub) Create(_ context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	d.createName = params.Name
	return &protocol.CreateResult{TerminalID: "terminal-created", State: "running"}, nil
}

func (d *terminalManagementDaemonStub) Get(_ context.Context, terminalID string) (*protocol.TerminalInfo, error) {
	return &protocol.TerminalInfo{
		ID:      terminalID,
		Name:    d.createName,
		Command: []string{"/bin/zsh", "-l"},
		State:   "running",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	}, nil
}

func (d *terminalManagementDaemonStub) List(context.Context) (*protocol.ListResult, error) {
	return &protocol.ListResult{}, nil
}

func (d *terminalManagementDaemonStub) SetMetadata(context.Context, string, string, map[string]string) error {
	return nil
}

func (d *terminalManagementDaemonStub) Remove(context.Context, string) error {
	return nil
}

func (d *terminalManagementDaemonStub) Events(context.Context, protocol.EventsParams) (<-chan protocol.Event, error) {
	ch := make(chan protocol.Event)
	close(ch)
	return ch, nil
}

func (d *terminalManagementDaemonStub) ServeTransport(context.Context, transport.Transport, string) error {
	return nil
}
