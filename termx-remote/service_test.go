package remote

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lozzow/termx/internal/protocol"
	"github.com/lozzow/termx/termx-remote/pairing"
	remoteprotocol "github.com/lozzow/termx/termx-remote/protocol"
	"github.com/lozzow/termx/termx-remote/protocol/runtimepb"
	remotertc "github.com/lozzow/termx/termx-remote/session/rtc"
	"github.com/lozzow/termx/termx-shared/transport"
	"google.golang.org/protobuf/proto"
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
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalCreateRequest{Command: []string{"/bin/zsh", "-l"}, Name: "ops shell"}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalInventoryItem
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal management response: %v", err)
	}
	if payload.GetTerminalId() != "terminal-created" {
		t.Fatalf("expected terminal_id in create response, got %s", string(body))
	}
	if daemon.createName != "ops shell" {
		t.Fatalf("create request did not reach daemon, got name %q", daemon.createName)
	}
	if got := daemon.createCommand; len(got) != 2 || got[0] != "/bin/zsh" || got[1] != "-l" {
		t.Fatalf("create command = %#v", got)
	}
}

func TestTerminalManagementCreateDefaultsCommandAndDir(t *testing.T) {
	daemon := &terminalManagementDaemonStub{}
	router := terminalManagementRouter{daemon: daemon}
	t.Setenv("SHELL", "/bin/bash")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "create",
		Path:   "create",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalCreateRequest{Name: "default shell"}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	if got := daemon.createCommand; len(got) != 1 || got[0] != "/bin/bash" {
		t.Fatalf("default create command = %#v", got)
	}
	if daemon.createDir != wd {
		t.Fatalf("default create dir = %q, want %q", daemon.createDir, wd)
	}
}

func TestTerminalManagementListPreservesDistinctTerminalIDs(t *testing.T) {
	daemon := &terminalManagementDaemonStub{
		list: []protocol.TerminalInfo{
			{ID: "1", Name: "one", Command: []string{"/bin/bash"}, State: "running", Size: protocol.Size{Cols: 80, Rows: 24}},
			{ID: "3", Name: "three", Command: []string{"/bin/bash"}, State: "running", Size: protocol.Size{Cols: 80, Rows: 24}},
		},
	}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "list",
		Path:   "list",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.Empty{}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalListResponse
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal management list response: %v", err)
	}
	if len(payload.GetTerminals()) != 2 || payload.GetTerminals()[0].GetTerminalId() != "1" || payload.GetTerminals()[1].GetTerminalId() != "3" {
		t.Fatalf("expected distinct terminal IDs [1 3], got %#v body=%s", payload.GetTerminals(), string(body))
	}
}

func TestTerminalManagementListIncludesTerminalMetadataTags(t *testing.T) {
	daemon := &terminalManagementDaemonStub{
		list: []protocol.TerminalInfo{{
			ID:      "1",
			Name:    "one",
			Command: []string{"/bin/bash"},
			State:   "running",
			Size:    protocol.Size{Cols: 80, Rows: 24},
			Tags: map[string]string{
				"termx.cwd":         "/srv/app",
				"termx.environment": "prod",
				"termx.size_lock":   "lock",
			},
			ResizeOwnership: &protocol.ResizeOwnership{
				OwnerSurfaceID: "app:machine-local:terminal:1",
			},
			ResizeOwnerAttachmentCount: 1,
		}},
	}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "list",
		Path:   "list",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.Empty{}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalListResponse
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal management list response: %v", err)
	}
	terms := payload.GetTerminals()
	if len(terms) != 1 ||
		terms[0].GetCwd() != "/srv/app" ||
		terms[0].GetEnvironment() != "prod" ||
		!terms[0].GetSizeLocked() ||
		terms[0].GetSizeLockMode() != "lock" ||
		terms[0].GetResizeOwnership().GetOwnerSurfaceId() != "app:machine-local:terminal:1" ||
		terms[0].GetResizeOwnerAttachmentCount() != 1 {
		t.Fatalf("expected terminal metadata in list response, got %#v body=%s", terms, string(body))
	}
}

func TestTerminalManagementGetDirectoryPrefersProcessCWD(t *testing.T) {
	daemon := &terminalManagementDaemonStub{
		get: &protocol.TerminalInfo{
			ID:      "terminal-1",
			State:   "running",
			LiveCWD: "/srv/process",
			CWD:     "/srv/reported",
			Tags:    map[string]string{"termx.cwd": "/srv/metadata"},
		},
	}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "get_directory",
		Path:   "get_directory",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalDirectoryRequest{TerminalId: "terminal-1"}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalDirectoryResponse
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal directory response: %v", err)
	}
	if payload.GetPath() != "/srv/process" || payload.GetSource() != "process" {
		t.Fatalf("expected process cwd, got %#v body=%s", payload, string(body))
	}
}

func TestTerminalManagementGetDirectoryFallsBackToReportedCWD(t *testing.T) {
	daemon := &terminalManagementDaemonStub{
		get: &protocol.TerminalInfo{
			ID:    "terminal-1",
			State: "running",
			CWD:   "/srv/reported",
			Tags:  map[string]string{"termx.cwd": "/srv/metadata"},
		},
	}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "get_directory",
		Path:   "get_directory",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalDirectoryRequest{TerminalId: "terminal-1"}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalDirectoryResponse
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal directory response: %v", err)
	}
	if payload.GetPath() != "/srv/reported" || payload.GetSource() != "reported" {
		t.Fatalf("expected reported cwd, got %#v body=%s", payload, string(body))
	}
}

func TestTerminalManagementGetDirectoryFallsBackToMetadataCWD(t *testing.T) {
	daemon := &terminalManagementDaemonStub{
		get: &protocol.TerminalInfo{
			ID:    "terminal-1",
			State: "running",
			Tags:  map[string]string{"termx.cwd": "/srv/metadata"},
		},
	}
	router := terminalManagementRouter{daemon: daemon}

	status, body, errMsg := router.RouteTerminalManagementRequest(context.Background(), remotertc.TerminalManagementRequest{
		Method: "get_directory",
		Path:   "get_directory",
		Body:   mustMarshalRuntimeProto(t, &runtimepb.TerminalDirectoryRequest{TerminalId: "terminal-1"}),
	})
	if errMsg != "" {
		t.Fatalf("RouteTerminalManagementRequest returned error: %s", errMsg)
	}
	if status != http.StatusOK {
		t.Fatalf("RouteTerminalManagementRequest status = %d, body = %s", status, string(body))
	}
	var payload runtimepb.TerminalDirectoryResponse
	if err := proto.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode terminal directory response: %v", err)
	}
	if payload.GetPath() != "/srv/metadata" || payload.GetSource() != "metadata" {
		t.Fatalf("expected metadata cwd, got %#v body=%s", payload, string(body))
	}
}

func mustMarshalRuntimeProto(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal runtime proto: %v", err)
	}
	return data
}

func pairClaimRequestForTest(session remoteprotocol.PairStartResult) pairing.ClaimRequest {
	return pairing.ClaimRequest{
		PairSessionID:         session.PairSessionID,
		PairSecret:            session.PairSecret,
		RequestedCapabilities: []string{"terminal"},
	}
}

type terminalManagementDaemonStub struct {
	createName    string
	createCommand []string
	createDir     string
	list          []protocol.TerminalInfo
	get           *protocol.TerminalInfo
}

func (d *terminalManagementDaemonStub) Create(_ context.Context, params protocol.CreateParams) (*protocol.CreateResult, error) {
	d.createName = params.Name
	d.createCommand = append([]string(nil), params.Command...)
	d.createDir = params.Dir
	return &protocol.CreateResult{TerminalID: "terminal-created", State: "running"}, nil
}

func (d *terminalManagementDaemonStub) Get(_ context.Context, terminalID string) (*protocol.TerminalInfo, error) {
	if d.get != nil {
		info := *d.get
		if info.ID == "" {
			info.ID = terminalID
		}
		return &info, nil
	}
	return &protocol.TerminalInfo{
		ID:      terminalID,
		Name:    d.createName,
		Command: []string{"/bin/zsh", "-l"},
		State:   "running",
		Size:    protocol.Size{Cols: 120, Rows: 40},
	}, nil
}

func (d *terminalManagementDaemonStub) List(context.Context) (*protocol.ListResult, error) {
	return &protocol.ListResult{Terminals: append([]protocol.TerminalInfo(nil), d.list...)}, nil
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
