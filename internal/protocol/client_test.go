package protocol

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-proto/wire"

	"github.com/lozzow/termx/termx-shared/transport/memory"
)

var errConcurrentSend = errors.New("concurrent send")

type legacyRemotePairStartParams struct {
	localPairURL string
	ttlSeconds   int
}

func (p legacyRemotePairStartParams) GetLocalPairURL() string { return p.localPairURL }
func (p legacyRemotePairStartParams) GetTTLSeconds() int      { return p.ttlSeconds }

func TestClientBoundaryDoesNotExposeRemoteRPCMethods(t *testing.T) {
	want := []string{
		"Attach",
		"AttachWithOptions",
		"Call",
		"Close",
		"Create",
		"Detach",
		"EnsureResize",
		"Events",
		"Hello",
		"Input",
		"InputWithOptions",
		"Kill",
		"List",
		"LockResize",
		"Remove",
		"Resize",
		"ResizeRequest",
		"Restart",
		"SetMetadata",
		"SetTags",
		"Snapshot",
		"SnapshotCompact",
		"StorageDelete",
		"StorageGet",
		"StorageList",
		"StoragePut",
		"Stream",
		"StreamReady",
		"UnlockResize",
		"WorkbenchApply",
		"WorkbenchGet",
	}
	clientType := reflect.TypeOf((*Client)(nil))
	got := make([]string, 0, clientType.NumMethod())
	for i := 0; i < clientType.NumMethod(); i++ {
		got = append(got, clientType.Method(i).Name)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("protocol.Client public methods changed:\n got: %v\nwant: %v", got, want)
	}
}

func TestRemoteProtocolTypedParams(t *testing.T) {
	pairParams := RemotePairStartParams{
		LocalPairURL:   "http://127.0.0.1:18888/pair",
		TTLSeconds:     120,
		AuthTTLSeconds: 30,
	}
	payload, err := EncodeMethodParams("remote.pair.start", pairParams)
	if err != nil {
		t.Fatalf("encode remote pair start params: %v", err)
	}
	decoded, err := DecodeMethodParams("remote.pair.start", payload)
	if err != nil {
		t.Fatalf("decode remote pair start params: %v", err)
	}
	gotPairParams, ok := decoded.(RemotePairStartParams)
	if !ok {
		t.Fatalf("unexpected decoded params type: %T", decoded)
	}
	if !reflect.DeepEqual(gotPairParams, pairParams) {
		t.Fatalf("remote pair start params mismatch:\n got: %#v\nwant: %#v", gotPairParams, pairParams)
	}

	localParams := RemoteLocalEnableParams{
		LocalWebAddr: "127.0.0.1:18080",
		ICETCPAddr:   "127.0.0.1:19090",
		HubURLs:      []string{"https://hub-a.example", "https://hub-b.example"},
		ControlURL:   "https://control.example",
		AccessToken:  "token",
		Region:       "local",
	}
	payload, err = EncodeMethodParams("remote.local.enable", &localParams)
	if err != nil {
		t.Fatalf("encode remote local enable params: %v", err)
	}
	decoded, err = DecodeMethodParams("remote.local.enable", payload)
	if err != nil {
		t.Fatalf("decode remote local enable params: %v", err)
	}
	gotLocalParams, ok := decoded.(RemoteLocalEnableParams)
	if !ok {
		t.Fatalf("unexpected decoded local params type: %T", decoded)
	}
	if !reflect.DeepEqual(gotLocalParams, localParams) {
		t.Fatalf("remote local enable params mismatch:\n got: %#v\nwant: %#v", gotLocalParams, localParams)
	}
}

func TestRemoteProtocolRejectsLegacyGetterParams(t *testing.T) {
	_, err := EncodeMethodParams("remote.pair.start", legacyRemotePairStartParams{
		localPairURL: "http://127.0.0.1:18888/pair",
		ttlSeconds:   120,
	})
	if err == nil {
		t.Fatal("expected legacy remote pair params to be rejected")
	}
	if !strings.Contains(err.Error(), "protocol.RemotePairStartParams") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoteProtocolTypedResults(t *testing.T) {
	updatedAt := time.Date(2026, 6, 22, 1, 2, 3, 4, time.UTC)
	expiresAt := time.Date(2026, 6, 22, 2, 3, 4, 5, time.UTC)

	status := RemoteStatus{
		State:         "running",
		Detail:        "ready",
		DeviceID:      "device-1",
		DeviceName:    "devbox",
		ControlURL:    "https://control.example",
		HubURL:        "https://hub.example",
		HubURLs:       []string{"https://hub-a.example", "https://hub-b.example"},
		DataDir:       "/tmp/termx",
		Mode:          "local",
		AllowLAN:      true,
		TerminalCount: 3,
		UpdatedAt:     updatedAt,
	}
	payload, err := EncodeMethodResult("remote.status", status)
	if err != nil {
		t.Fatalf("encode remote status: %v", err)
	}
	statusPayload := payload
	var gotStatus RemoteStatus
	if err := DecodeMethodResult("remote.status", payload, &gotStatus); err != nil {
		t.Fatalf("decode remote status: %v", err)
	}
	if !reflect.DeepEqual(gotStatus, status) {
		t.Fatalf("remote status mismatch:\n got: %#v\nwant: %#v", gotStatus, status)
	}

	pairResult := RemotePairStartResult{
		Type:              "local",
		MachineID:         "machine-1",
		MachineName:       "devbox",
		LocalPairURL:      "http://127.0.0.1:18080/pair",
		PairSessionID:     "session-1",
		PairSecret:        "pair-secret",
		AnswerProofSecret: "answer-secret",
		ExpiresAt:         expiresAt,
	}
	payload, err = EncodeMethodResult("remote.pair.start", &pairResult)
	if err != nil {
		t.Fatalf("encode remote pair start result: %v", err)
	}
	var gotPairResult RemotePairStartResult
	if err := DecodeMethodResult("remote.pair.start", payload, &gotPairResult); err != nil {
		t.Fatalf("decode remote pair start result: %v", err)
	}
	if !reflect.DeepEqual(gotPairResult, pairResult) {
		t.Fatalf("remote pair start result mismatch:\n got: %#v\nwant: %#v", gotPairResult, pairResult)
	}

	localStatus := RemoteLocalStatus{
		Enabled:       true,
		HTTPURL:       "http://127.0.0.1:18080",
		LocalWebAddr:  "127.0.0.1:18080",
		LocalPairURL:  "http://127.0.0.1:18080/pair",
		ICETCPEnabled: true,
		ICETCPAddr:    "127.0.0.1:19090",
		ICETCPPort:    19090,
		UpdatedAt:     updatedAt,
	}
	payload, err = EncodeMethodResult("remote.local.status", localStatus)
	if err != nil {
		t.Fatalf("encode remote local status: %v", err)
	}
	var gotLocalStatus RemoteLocalStatus
	if err := DecodeMethodResult("remote.local.status", payload, &gotLocalStatus); err != nil {
		t.Fatalf("decode remote local status: %v", err)
	}
	if !reflect.DeepEqual(gotLocalStatus, localStatus) {
		t.Fatalf("remote local status mismatch:\n got: %#v\nwant: %#v", gotLocalStatus, localStatus)
	}

	var wrongTarget struct{ State string }
	if err := DecodeMethodResult("remote.status", statusPayload, &wrongTarget); err == nil {
		t.Fatal("expected remote result decode to reject arbitrary struct target")
	}
}

func TestClientRequestStreamAndProtocolError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runFakeProtocolServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	created, err := client.Create(ctx, CreateParams{
		Command: []string{"bash", "--noprofile", "--norc"},
		Name:    "demo",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if created.TerminalID != "term-1" || created.State != "running" {
		t.Fatalf("unexpected create result: %#v", created)
	}

	list, err := client.List(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list.Terminals) != 1 || list.Terminals[0].ID != "term-1" {
		t.Fatalf("unexpected list result: %#v", list)
	}

	if err := client.SetTags(ctx, "term-1", map[string]string{"role": "shell"}); err != nil {
		t.Fatalf("set tags failed: %v", err)
	}
	if err := client.SetMetadata(ctx, "term-1", "dev-shell", map[string]string{"role": "shell", "team": "infra"}); err != nil {
		t.Fatalf("set metadata failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	if attach.Channel != 7 {
		t.Fatalf("unexpected channel: %#v", attach)
	}

	stream, stop := client.Stream(attach.Channel)
	defer stop()

	if err := client.Input(ctx, attach.Channel, []byte("echo hi\n")); err != nil {
		t.Fatalf("input failed: %v", err)
	}

	select {
	case msg := <-stream:
		if msg.Type != wire.TypeScreenUpdate || string(msg.Payload) != "stream-data" {
			t.Fatalf("unexpected stream frame: %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for stream output")
	}

	if err := client.Resize(ctx, attach.Channel, 100, 40); err != nil {
		t.Fatalf("resize failed: %v", err)
	}

	snap, err := client.Snapshot(ctx, "term-1", 0, 50)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if snap.TerminalID != "term-1" || len(snap.Screen.Cells) != 1 {
		t.Fatalf("unexpected snapshot result: %#v", snap)
	}

	compactSnap, err := client.SnapshotCompact(ctx, "term-1", 0, 50)
	if err != nil {
		t.Fatalf("compact snapshot failed: %v", err)
	}
	if compactSnap.TerminalID != "term-1" || len(compactSnap.ScreenRows) != 1 || compactSnap.ScreenRows[0].Text != "hi" {
		t.Fatalf("unexpected compact snapshot result: %#v", compactSnap)
	}

	err = client.Kill(ctx, "missing")
	if err == nil || !strings.Contains(err.Error(), "protocol error 404") {
		t.Fatalf("expected protocol error 404, got %v", err)
	}

	if _, err := client.List(ctx); err == nil {
		t.Fatal("expected list after server close to fail")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientInputWithOptionsUsesAckedRequest(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	defer serverTransport.Close()
	client := NewClient(clientTransport)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.InputWithOptions(ctx, InputParams{
			TerminalID: "term-1",
			Channel:    7,
			SurfaceID:  "surface-1",
			ViewID:     "view-1",
			Data:       []byte("ls\n"),
		})
	}()

	req, err := expectRequest(serverTransport, "input")
	if err != nil {
		t.Fatal(err)
	}
	params, err := requestParams[InputParams](req)
	if err != nil {
		t.Fatal(err)
	}
	if params.TerminalID != "term-1" || params.Channel != 7 || params.SurfaceID != "surface-1" || params.ViewID != "view-1" || string(params.Data) != "ls\n" {
		t.Fatalf("unexpected input params %#v", params)
	}
	if err := sendMethodResponse(serverTransport, req, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("input request failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClientInputWithOptionsReturnsServerError(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	defer serverTransport.Close()
	client := NewClient(clientTransport)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.InputWithOptions(ctx, InputParams{TerminalID: "term-1", Channel: 99, Data: []byte("x")})
	}()

	req, err := expectRequest(serverTransport, "input")
	if err != nil {
		t.Fatal(err)
	}
	if err := sendError(serverTransport, req.ID, 404, "terminal not found"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "terminal not found") {
			t.Fatalf("expected protocol error, got %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestClientEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runFakeEventServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	events, err := client.Events(ctx, EventsParams{
		TerminalID: "term-1",
		Types:      []EventType{EventTerminalRemoved},
	})
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	select {
	case evt, ok := <-events:
		if !ok {
			t.Fatal("expected event channel to stay open")
		}
		if evt.Type != EventTerminalRemoved || evt.TerminalID != "term-1" {
			t.Fatalf("unexpected event: %#v", evt)
		}
		if evt.Removed == nil || evt.Removed.Reason != "expired" {
			t.Fatalf("unexpected removed payload: %#v", evt)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

type eventsResult struct {
	events <-chan Event
	err    error
}

func TestClientEventsFanOutPerSubscription(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	client := NewClient(clientTransport)
	defer client.Close()

	helloDone := make(chan error, 1)
	go func() {
		helloDone <- client.Hello(ctx, Hello{Version: wire.Version, Client: "test"})
	}()
	if channel, typ, _, err := recvFrame(serverTransport); err != nil || channel != 0 || typ != wire.TypeHello {
		t.Fatalf("expected client hello, channel=%d type=%d err=%v", channel, typ, err)
	}
	if err := respondHello(serverTransport); err != nil {
		t.Fatalf("respond hello: %v", err)
	}
	if err := <-helloDone; err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	termOneDone := make(chan eventsResult, 1)
	go func() {
		events, err := client.Events(ctx, EventsParams{TerminalID: "term-1", Types: []EventType{EventTerminalStateChanged}})
		termOneDone <- eventsResult{events: events, err: err}
	}()
	req, err := expectRequest(serverTransport, "events")
	if err != nil {
		t.Fatalf("expect first events request: %v", err)
	}
	params, err := requestParams[EventsParams](req)
	if err != nil {
		t.Fatalf("decode first events params: %v", err)
	}
	if params.TerminalID != "" || len(params.Types) != 0 {
		t.Fatalf("client should open one broad daemon events stream, got %#v", params)
	}
	if err := sendMethodResponse(serverTransport, req, nil); err != nil {
		t.Fatalf("respond first events request: %v", err)
	}
	termOneResult := <-termOneDone
	if termOneResult.err != nil {
		t.Fatalf("events term-1: %v", termOneResult.err)
	}
	termOneEvents := termOneResult.events

	termTwoDone := make(chan eventsResult, 1)
	go func() {
		events, err := client.Events(ctx, EventsParams{TerminalID: "term-2", Types: []EventType{EventTerminalStateChanged}})
		termTwoDone <- eventsResult{events: events, err: err}
	}()
	termTwoResult := <-termTwoDone
	if termTwoResult.err != nil {
		t.Fatalf("events term-2: %v", termTwoResult.err)
	}
	termTwoEvents := termTwoResult.events

	sendProtocolEvent := func(event Event) {
		t.Helper()
		payload, err := EncodeEventPayload(event)
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		if err := sendFrame(serverTransport, 0, wire.TypeEvent, payload); err != nil {
			t.Fatalf("send event: %v", err)
		}
	}
	sendProtocolEvent(Event{Type: EventTerminalStateChanged, TerminalID: "term-1"})
	if got := assertProtocolEvent(t, termOneEvents); got.TerminalID != "term-1" {
		t.Fatalf("term-1 subscriber got wrong event %#v", got)
	}
	assertNoProtocolEvent(t, termTwoEvents)

	sendProtocolEvent(Event{Type: EventTerminalStateChanged, TerminalID: "term-2"})
	if got := assertProtocolEvent(t, termTwoEvents); got.TerminalID != "term-2" {
		t.Fatalf("term-2 subscriber got wrong event %#v", got)
	}
	assertNoProtocolEvent(t, termOneEvents)
}

func TestClientEventsFanOutRespectsStorageFilters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	client := NewClient(clientTransport)
	defer client.Close()

	helloDone := make(chan error, 1)
	go func() {
		helloDone <- client.Hello(ctx, Hello{Version: wire.Version, Client: "test"})
	}()
	if channel, typ, _, err := recvFrame(serverTransport); err != nil || channel != 0 || typ != wire.TypeHello {
		t.Fatalf("expected client hello, channel=%d type=%d err=%v", channel, typ, err)
	}
	if err := respondHello(serverTransport); err != nil {
		t.Fatalf("respond hello: %v", err)
	}
	if err := <-helloDone; err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	storageDone := make(chan eventsResult, 1)
	go func() {
		events, err := client.Events(ctx, EventsParams{
			Types:            []EventType{EventStorageChanged},
			StorageAppID:     "termx-tui-v3",
			StorageScope:     StorageScopePublic,
			StorageOwnerID:   "workspace-main",
			StorageKeyPrefix: "workbench/",
		})
		storageDone <- eventsResult{events: events, err: err}
	}()
	req, err := expectRequest(serverTransport, "events")
	if err != nil {
		t.Fatalf("expect events request: %v", err)
	}
	if err := sendMethodResponse(serverTransport, req, nil); err != nil {
		t.Fatalf("respond events request: %v", err)
	}
	storageResult := <-storageDone
	if storageResult.err != nil {
		t.Fatalf("events storage: %v", storageResult.err)
	}

	sendProtocolEvent := func(event Event) {
		t.Helper()
		payload, err := EncodeEventPayload(event)
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		if err := sendFrame(serverTransport, 0, wire.TypeEvent, payload); err != nil {
			t.Fatalf("send event: %v", err)
		}
	}
	sendProtocolEvent(Event{Type: EventTerminalStateChanged, TerminalID: "term-1"})
	assertNoProtocolEvent(t, storageResult.events)

	sendProtocolEvent(Event{Type: EventStorageChanged, Storage: &StorageChangedData{
		AppID:   "termx-tui-v3",
		Scope:   StorageScopePublic,
		OwnerID: "workspace-main",
		Key:     "workbench/root",
		Version: 3,
	}})
	if got := assertProtocolEvent(t, storageResult.events); got.Storage == nil || got.Storage.Key != "workbench/root" {
		t.Fatalf("storage subscriber got wrong event %#v", got)
	}
}

func assertProtocolEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("expected event channel to stay open")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol event")
	}
	return Event{}
}

func assertNoProtocolEvent(t *testing.T, events <-chan Event) {
	t.Helper()
	select {
	case event, ok := <-events:
		if ok {
			t.Fatalf("unexpected protocol event %#v", event)
		}
		t.Fatal("event channel closed unexpectedly")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestClientAttachBuffersFramesThatArriveBeforeStreamRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runBufferedAttachServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "collaborator")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	stream, stop := client.Stream(attach.Channel)
	defer stop()

	select {
	case msg := <-stream:
		if msg.Type != wire.TypeScreenUpdate || string(msg.Payload) != "early-output" {
			t.Fatalf("unexpected buffered stream frame: %#v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for buffered screen update")
	}

	select {
	case msg := <-stream:
		if msg.Type != wire.TypeClosed {
			t.Fatalf("expected closed frame, got %#v", msg)
		}
		code, err := wire.DecodeClosedPayload(msg.Payload)
		if err != nil {
			t.Fatalf("decode closed payload failed: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for buffered closed frame")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientStreamCancelDropsLateFramesAndKeepsReadLoopAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runLateFrameAfterCancelServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	attach, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}
	_, stop := client.Stream(attach.Channel)
	stop()

	stream, stop2 := client.Stream(attach.Channel)
	defer stop2()

	select {
	case frame := <-stream:
		t.Fatalf("expected late frame to be dropped after cancel, got %#v", frame)
	case <-time.After(200 * time.Millisecond):
	}

	if _, err := client.List(ctx); err != nil {
		t.Fatalf("expected client to stay usable after late frame, got %v", err)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientStreamCancelKeepsEarlyFramesWhenSameChannelIsReattached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := memory.NewPair()
	defer clientTransport.Close()
	defer serverTransport.Close()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- runReusedChannelAttachServer(serverTransport)
	}()

	client := NewClient(clientTransport)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	first, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("first attach failed: %v", err)
	}
	_, stop := client.Stream(first.Channel)
	stop()

	second, err := client.Attach(ctx, "term-1", "observer")
	if err != nil {
		t.Fatalf("second attach failed: %v", err)
	}
	stream, stop2 := client.Stream(second.Channel)
	defer stop2()

	select {
	case frame := <-stream:
		if frame.Type != wire.TypeScreenUpdate || string(frame.Payload) != "replayed-after-reattach" {
			t.Fatalf("unexpected replayed frame: %#v", frame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for replayed early frame on reused channel")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("fake server failed: %v", err)
	}
}

func TestClientCloseWaitsForReadLoopAndUnblocksPendingRequest(t *testing.T) {
	clientTransport, serverTransport := memory.NewPair()
	defer serverTransport.Close()

	client := NewClient(clientTransport)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.List(context.Background())
		errCh <- err
	}()

	frameReceived := make(chan struct{})
	go func() {
		_, _ = serverTransport.Recv()
		close(frameReceived)
	}()

	select {
	case <-frameReceived:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for request frame")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	select {
	case <-client.done:
	default:
		t.Fatal("expected Close to wait for readLoop shutdown")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("expected EOF from pending request, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pending request to fail")
	}
}

func TestClientSerializesConcurrentSends(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := newConcurrentUnsafeTransport()
	client := NewClient(tr)
	defer client.Close()

	if err := client.Hello(ctx, Hello{Version: wire.Version, Client: "test"}); err != nil {
		t.Fatalf("hello failed: %v", err)
	}

	const workers = 8
	start := make(chan struct{})
	errCh := make(chan error, workers)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := client.List(ctx)
			errCh <- err
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("expected concurrent lists to succeed, got %v", err)
		}
	}
}

func TestClientStreamPreservesQueuedFullReplaceScreenUpdates(t *testing.T) {
	stream := newClientStreamWithConfig(4, 0)
	defer stream.close()

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "a")})
	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 0
	})

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "b")})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "c")})

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.queue) != 2 {
		t.Fatalf("expected full-replace screen updates to remain sequence-addressable, got %d", len(stream.queue))
	}
	if !testScreenUpdatePayloadContainsText(t, stream.queue[0].Payload, "b") || !testScreenUpdatePayloadContainsText(t, stream.queue[1].Payload, "c") {
		t.Fatalf("unexpected queued frames: %#v", stream.queue)
	}
}

func TestClientStreamDoesNotCoalesceQueuedDeltaScreenUpdates(t *testing.T) {
	stream := newClientStreamWithConfig(4, 0)
	defer stream.close()

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testDeltaScreenUpdatePayload(t, "a")})
	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 0
	})

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testDeltaScreenUpdatePayload(t, "b")})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testDeltaScreenUpdatePayload(t, "c")})

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.queue) != 2 {
		t.Fatalf("expected queued delta screen updates to be preserved, got %d", len(stream.queue))
	}
	if !testScreenUpdatePayloadContainsText(t, stream.queue[0].Payload, "b") || !testScreenUpdatePayloadContainsText(t, stream.queue[1].Payload, "c") {
		t.Fatalf("unexpected queued delta frames: %#v", stream.queue)
	}
}

func TestClientStreamKeepsControlFrameBetweenScreenUpdates(t *testing.T) {
	stream := newClientStreamWithConfig(4, 0)
	defer stream.close()

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "a")})
	stream.send(StreamFrame{Type: wire.TypeResize, Payload: []byte("resize")})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "b")})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: testFullReplaceScreenUpdatePayload(t, "c")})

	frames := []StreamFrame{
		waitClientStreamFrame(t, stream),
		waitClientStreamFrame(t, stream),
		waitClientStreamFrame(t, stream),
		waitClientStreamFrame(t, stream),
	}
	if frames[0].Type != wire.TypeScreenUpdate || !testScreenUpdatePayloadContainsText(t, frames[0].Payload, "a") {
		t.Fatalf("unexpected first queued frame: %#v", frames[0])
	}
	if frames[1].Type != wire.TypeResize {
		t.Fatalf("unexpected control frame: %#v", frames[1])
	}
	if frames[2].Type != wire.TypeScreenUpdate || !testScreenUpdatePayloadContainsText(t, frames[2].Payload, "b") {
		t.Fatalf("unexpected second screen frame: %#v", frames[2])
	}
	if frames[3].Type != wire.TypeScreenUpdate || !testScreenUpdatePayloadContainsText(t, frames[3].Payload, "c") {
		t.Fatalf("unexpected final screen frame: %#v", frames[3])
	}
}

func TestClientStreamOverflowQueuesSyncLostInsteadOfSilentDrop(t *testing.T) {
	stream := newClientStreamWithConfig(2, 0)
	defer stream.close()

	payload := bytes.Repeat([]byte("x"), wire.MaxFrameSize/2+1)
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})
	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 0
	})

	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})
	stream.send(StreamFrame{Type: wire.TypeResize, Payload: wire.EncodeResizePayload(80, 24)})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})
	stream.send(StreamFrame{Type: wire.TypeResize, Payload: wire.EncodeResizePayload(80, 25)})

	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) == 3 && stream.pendingDroppedBytes == 0
	})

	stream.mu.Lock()
	if len(stream.queue) != 3 {
		t.Fatalf("expected sync-lost frame after overflow flush, got %d frames", len(stream.queue))
	}
	frame := stream.queue[2]
	if frame.Type != wire.TypeSyncLost {
		t.Fatalf("expected sync-lost frame after overflow, got %#v", frame)
	}
	dropped, err := wire.DecodeSyncLostPayload(frame.Payload)
	if err != nil {
		t.Fatalf("decode sync-lost payload: %v", err)
	}
	if dropped == 0 {
		t.Fatalf("expected non-zero dropped byte count, got %d", dropped)
	}
	stream.mu.Unlock()
}

func TestClientStreamOverflowFlushesSyncLostWithoutWaitingForNextFrame(t *testing.T) {
	stream := newClientStreamWithConfig(1, 0)
	defer stream.close()

	payload := bytes.Repeat([]byte("x"), 128)
	stream.send(StreamFrame{Type: wire.TypeResize, Payload: wire.EncodeResizePayload(80, 24)})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})

	waitForClientStreamState(t, stream, func() bool {
		return len(stream.queue) >= 1 && stream.queue[len(stream.queue)-1].Type == wire.TypeSyncLost && stream.pendingDroppedBytes == 0
	})

	stream.mu.Lock()
	defer stream.mu.Unlock()
	frame := stream.queue[len(stream.queue)-1]
	if frame.Type != wire.TypeSyncLost {
		t.Fatalf("expected dropped screen frame to queue sync-lost immediately, got %#v", frame)
	}
	dropped, err := wire.DecodeSyncLostPayload(frame.Payload)
	if err != nil {
		t.Fatalf("decode sync-lost payload: %v", err)
	}
	if dropped != uint64(len(payload)) {
		t.Fatalf("expected dropped byte count %d, got %d", len(payload), dropped)
	}
}

func TestClientStreamOverflowSyncLostAcksDroppedScreenSequence(t *testing.T) {
	stream := newClientStreamWithConfig(1, 1)
	defer stream.close()

	payload := bytes.Repeat([]byte("x"), 128)
	stream.send(StreamFrame{Type: wire.TypeResize, Payload: wire.EncodeResizePayload(80, 24)})
	stream.send(StreamFrame{Type: wire.TypeScreenUpdate, Payload: payload})

	frame := waitClientStreamFrame(t, stream)
	if frame.Type != wire.TypeResize {
		t.Fatalf("expected first delivered frame to remain resize, got %#v", frame)
	}
	frame = waitClientStreamFrame(t, stream)
	if frame.Type != wire.TypeSyncLost {
		t.Fatalf("expected sync-lost after dropped screen frame, got %#v", frame)
	}
	if frame.ScreenSequence != 1 {
		t.Fatalf("expected sync-lost to carry dropped screen sequence 1, got %d", frame.ScreenSequence)
	}
}

func testFullReplaceScreenUpdatePayload(t *testing.T, text string) []byte {
	t.Helper()
	payload, err := EncodeScreenUpdatePayload(ScreenUpdate{
		FullReplace: true,
		Size:        Size{Cols: 8, Rows: 1},
		Screen: ScreenData{
			Cells: [][]Cell{{{Content: text, Width: 1}}},
		},
		Cursor: CursorState{Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode full screen update: %v", err)
	}
	return payload
}

func testDeltaScreenUpdatePayload(t *testing.T, text string) []byte {
	t.Helper()
	payload, err := EncodeScreenUpdatePayload(ScreenUpdate{
		Size: Size{Cols: 8, Rows: 1},
		Ops: []ScreenOp{{
			Code:  ScreenOpWriteSpan,
			Row:   0,
			Col:   0,
			Cells: []Cell{{Content: text, Width: 1}},
		}},
		Cursor: CursorState{Visible: true},
		Modes:  TerminalModes{AutoWrap: true},
	})
	if err != nil {
		t.Fatalf("encode delta screen update: %v", err)
	}
	return payload
}

func testScreenUpdatePayloadContainsText(t *testing.T, payload []byte, text string) bool {
	t.Helper()
	update, err := DecodeScreenUpdatePayload(payload)
	if err != nil {
		t.Fatalf("decode screen update: %v", err)
	}
	for _, row := range update.Screen.Cells {
		for _, cell := range row {
			if cell.Content == text {
				return true
			}
		}
	}
	for _, op := range update.Ops {
		for _, cell := range op.Cells {
			if cell.Content == text {
				return true
			}
		}
	}
	return false
}

func runFakeProtocolServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "create")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, CreateResult{TerminalID: "term-1", State: "running"}); err != nil {
		return err
	}

	req, err = expectRequest(tr, "list")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, ListResult{
		Terminals: []TerminalInfo{{
			ID:    "term-1",
			Name:  "demo",
			State: "running",
		}},
	}); err != nil {
		return err
	}

	req, err = expectRequest(tr, "set_tags")
	if err != nil {
		return err
	}
	setTags, err := requestParams[SetTagsParams](req)
	if err != nil {
		return err
	}
	if setTags.TerminalID != "term-1" || setTags.Tags["role"] != "shell" {
		return fmt.Errorf("unexpected set_tags params: %#v", setTags)
	}
	if err := sendMethodResponse(tr, req, nil); err != nil {
		return err
	}

	req, err = expectRequest(tr, "set_metadata")
	if err != nil {
		return err
	}
	setMetadata, err := requestParams[SetMetadataParams](req)
	if err != nil {
		return err
	}
	if setMetadata.TerminalID != "term-1" || setMetadata.Name != "dev-shell" || setMetadata.Tags["team"] != "infra" {
		return fmt.Errorf("unexpected set_metadata params: %#v", setMetadata)
	}
	if err := sendMethodResponse(tr, req, nil); err != nil {
		return err
	}

	req, err = expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, AttachResult{Mode: "collaborator", Channel: 7}); err != nil {
		return err
	}

	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 7 || typ != wire.TypeInput || string(payload) != "echo hi\n" {
		return fmt.Errorf("unexpected input frame: channel=%d type=%d payload=%q", channel, typ, string(payload))
	}
	if err := sendFrame(tr, 7, wire.TypeScreenUpdate, []byte("stream-data")); err != nil {
		return err
	}

	channel, typ, payload, err = recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 7 || typ != wire.TypeResize {
		return fmt.Errorf("unexpected resize frame: channel=%d type=%d", channel, typ)
	}
	cols, rows, err := wire.DecodeResizePayload(payload)
	if err != nil {
		return err
	}
	if cols != 100 || rows != 40 {
		return fmt.Errorf("unexpected resize payload: %dx%d", cols, rows)
	}

	req, err = expectRequest(tr, "snapshot")
	if err != nil {
		return err
	}
	snapshotResult, err := EncodeSnapshotPayload(&Snapshot{
		TerminalID: "term-1",
		Size:       Size{Cols: 80, Rows: 24},
		Screen: ScreenData{Cells: [][]Cell{
			{{Content: "h", Width: 1}, {Content: "i", Width: 1}},
		}},
		Cursor:    CursorState{Row: 0, Col: 2, Visible: true, Shape: "block"},
		Modes:     TerminalModes{AutoWrap: true},
		Timestamp: time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return err
	}
	if err := sendBinaryResponse(tr, req.ID, snapshotResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "snapshot")
	if err != nil {
		return err
	}
	if err := sendBinaryResponse(tr, req.ID, snapshotResult); err != nil {
		return err
	}

	req, err = expectRequest(tr, "kill")
	if err != nil {
		return err
	}
	if err := sendError(tr, req.ID, 404, "missing"); err != nil {
		return err
	}

	return tr.Close()
}

func waitForClientStreamState(t *testing.T, stream *clientStream, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stream.mu.Lock()
		ok := cond()
		stream.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	t.Fatalf("timed out waiting for client stream state: queued=%d dropped=%d", len(stream.queue), stream.pendingDroppedBytes)
}

func waitClientStreamFrame(t *testing.T, stream *clientStream) StreamFrame {
	t.Helper()
	select {
	case frame, ok := <-stream.channel():
		if !ok {
			t.Fatal("client stream closed before frame")
		}
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for client stream frame")
	}
	return StreamFrame{}
}

type concurrentUnsafeTransport struct {
	inFlight atomic.Int32
	recvCh   chan []byte
	done     chan struct{}
	once     sync.Once
}

func newConcurrentUnsafeTransport() *concurrentUnsafeTransport {
	return &concurrentUnsafeTransport{
		recvCh: make(chan []byte, 32),
		done:   make(chan struct{}),
	}
}

func (t *concurrentUnsafeTransport) Send(frame []byte) error {
	select {
	case <-t.done:
		return io.EOF
	default:
	}
	if !t.inFlight.CompareAndSwap(0, 1) {
		return errConcurrentSend
	}
	defer t.inFlight.Store(0)

	time.Sleep(20 * time.Millisecond)

	channel, typ, payload, err := wire.DecodeFrame(frame)
	if err != nil {
		return err
	}
	if channel != 0 {
		return fmt.Errorf("unexpected non-control channel %d", channel)
	}

	switch typ {
	case wire.TypeHello:
		resp, err := EncodeHelloPayload(Hello{Version: wire.Version, Server: "test"})
		if err != nil {
			return err
		}
		reply, err := wire.EncodeFrame(0, wire.TypeHello, resp)
		if err != nil {
			return err
		}
		t.recvCh <- reply
		return nil
	case wire.TypeRequest:
		req, err := DecodeRequestPayload(payload)
		if err != nil {
			return err
		}
		if req.Method != "list" {
			return fmt.Errorf("unexpected method %q", req.Method)
		}
		result, err := EncodeMethodResult(req.Method, ListResult{})
		if err != nil {
			return err
		}
		replyPayload, err := EncodeResponsePayload(Response{ID: req.ID, Result: result})
		if err != nil {
			return err
		}
		reply, err := wire.EncodeFrame(0, wire.TypeResponse, replyPayload)
		if err != nil {
			return err
		}
		t.recvCh <- reply
		return nil
	default:
		return fmt.Errorf("unexpected frame type %d", typ)
	}
}

func (t *concurrentUnsafeTransport) Recv() ([]byte, error) {
	select {
	case <-t.done:
		return nil, io.EOF
	case frame, ok := <-t.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	}
}

func (t *concurrentUnsafeTransport) Close() error {
	t.once.Do(func() {
		close(t.done)
		close(t.recvCh)
	})
	return nil
}

func (t *concurrentUnsafeTransport) Done() <-chan struct{} {
	return t.done
}

func runFakeEventServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "events")
	if err != nil {
		return err
	}
	params, err := requestParams[EventsParams](req)
	if err != nil {
		return err
	}
	if params.TerminalID != "" || len(params.Types) != 0 {
		return fmt.Errorf("client should open one broad events stream, got: %#v", params)
	}

	if err := sendMethodResponse(tr, req, nil); err != nil {
		return err
	}

	payload, err := EncodeEventPayload(Event{
		Type:       EventTerminalRemoved,
		TerminalID: "term-1",
		Timestamp:  time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC),
		Removed:    &TerminalRemovedData{Reason: "expired"},
	})
	if err != nil {
		return err
	}
	if err := sendFrame(tr, 0, wire.TypeEvent, payload); err != nil {
		return err
	}
	return tr.Close()
}

func runBufferedAttachServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendFrame(tr, 7, wire.TypeScreenUpdate, []byte("early-output")); err != nil {
		return err
	}
	if err := sendFrame(tr, 7, wire.TypeClosed, wire.EncodeClosedPayload(0)); err != nil {
		return err
	}
	return sendMethodResponse(tr, req, AttachResult{Mode: "collaborator", Channel: 7})
}

func runLateFrameAfterCancelServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, AttachResult{Mode: "observer", Channel: 7}); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	if err := sendFrame(tr, 7, wire.TypeScreenUpdate, []byte("late-output")); err != nil {
		return err
	}

	req, err = expectRequest(tr, "list")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, ListResult{
		Terminals: []TerminalInfo{{ID: "term-1", Name: "demo", State: "running"}},
	}); err != nil {
		return err
	}
	return nil
}

func runReusedChannelAttachServer(tr *memory.Transport) error {
	if err := expectHello(tr); err != nil {
		return err
	}
	if err := respondHello(tr); err != nil {
		return err
	}

	req, err := expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendMethodResponse(tr, req, AttachResult{Mode: "observer", Channel: 7}); err != nil {
		return err
	}

	req, err = expectRequest(tr, "attach")
	if err != nil {
		return err
	}
	if err := sendFrame(tr, 7, wire.TypeScreenUpdate, []byte("replayed-after-reattach")); err != nil {
		return err
	}
	return sendMethodResponse(tr, req, AttachResult{Mode: "observer", Channel: 7})
}

func expectHello(tr *memory.Transport) error {
	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return err
	}
	if channel != 0 || typ != wire.TypeHello {
		return fmt.Errorf("unexpected hello frame: channel=%d type=%d", channel, typ)
	}
	_, err = DecodeHelloPayload(payload)
	return err
}

func respondHello(tr *memory.Transport) error {
	payload, _ := EncodeHelloPayload(Hello{Version: wire.Version, Server: "fake"})
	return sendFrame(tr, 0, wire.TypeHello, payload)
}

func expectRequest(tr *memory.Transport, method string) (Request, error) {
	channel, typ, payload, err := recvFrame(tr)
	if err != nil {
		return Request{}, err
	}
	if channel != 0 || typ != wire.TypeRequest {
		return Request{}, fmt.Errorf("unexpected request frame: channel=%d type=%d", channel, typ)
	}
	req, err := DecodeRequestPayload(payload)
	if err != nil {
		return Request{}, err
	}
	if req.Method != method {
		return Request{}, fmt.Errorf("unexpected method: %s", req.Method)
	}
	return req, nil
}

func sendResponse(tr *memory.Transport, id uint64, result []byte) error {
	payload, _ := EncodeResponsePayload(Response{ID: id, Result: result})
	return sendFrame(tr, 0, wire.TypeResponse, payload)
}

func sendMethodResponse(tr *memory.Transport, req Request, result any) error {
	payload, err := EncodeMethodResult(req.Method, result)
	if err != nil {
		return err
	}
	return sendResponse(tr, req.ID, payload)
}

func requestParams[T any](req Request) (T, error) {
	var zero T
	decoded, err := DecodeMethodParams(req.Method, req.Params)
	if err != nil {
		return zero, err
	}
	params, ok := decoded.(T)
	if !ok {
		return zero, fmt.Errorf("decoded params for %s as %T", req.Method, decoded)
	}
	return params, nil
}

func sendBinaryResponse(tr *memory.Transport, id uint64, result []byte) error {
	payload, err := EncodeBinaryResponsePayload(id, result)
	if err != nil {
		return err
	}
	return sendFrame(tr, 0, wire.TypeResponseBinary, payload)
}

func sendError(tr *memory.Transport, id uint64, code int, message string) error {
	payload, _ := EncodeErrorPayload(ErrorMessage{
		ID: id,
		Error: ProtocolError{
			Code:    code,
			Message: message,
		},
	})
	return sendFrame(tr, 0, wire.TypeError, payload)
}

func sendFrame(tr *memory.Transport, channel uint16, typ uint8, payload []byte) error {
	frame, err := wire.EncodeFrame(channel, typ, payload)
	if err != nil {
		return err
	}
	return tr.Send(frame)
}

func recvFrame(tr *memory.Transport) (uint16, uint8, []byte, error) {
	frame, err := tr.Recv()
	if err != nil {
		return 0, 0, nil, err
	}
	return wire.DecodeFrame(frame)
}
