package webrtc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anytty/anytty/internal/protocol/directsignal"
	"github.com/anytty/anytty/proto/remoteauthpb"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/anytty/anytty/shared/transport"
	pion "github.com/pion/webrtc/v4"
)

func TestDirectServerPreAuthGlobalLimitRejectsBeforeWorkerAndReusesSlot(t *testing.T) {
	harness := newDirectServerHarness(t)
	var workers atomic.Int32
	harness.server.beforeConnectionWorkerRun = func() { workers.Add(1) }
	harness.start(t)

	clients := make([]net.Conn, 0, directSignalingPreAuthLimit+1)
	for index := 0; index < directSignalingPreAuthLimit; index++ {
		address := net.Addr(directTestAddress(fmt.Sprintf("[2001:db8::%x]:1234", index+1)))
		if index < directSignalingPreAuthPerIPLimit {
			if index%2 == 0 {
				address = directTestAddress(fmt.Sprintf("192.0.2.10:%d", 1200+index))
			} else {
				address = directTestAddress(fmt.Sprintf("[::ffff:192.0.2.10]:%d", 1200+index))
			}
		}
		_, client := harness.acceptPipe(t, address)
		clients = append(clients, client)
	}
	waitDirectServerState(t, harness.server, directSignalingPreAuthLimit, directSignalingPreAuthPerIPLimit)
	waitAtomicInt32(t, &workers, directSignalingPreAuthLimit)

	rejectedServer, rejectedClient := harness.acceptPipe(t, directTestAddress("[2001:db8::ffff]:1234"))
	response := readDirectResponse(t, rejectedClient)
	assertDirectErrorCode(t, response, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED)
	if rejectedServer.reads.Load() != 0 {
		t.Fatalf("overloaded connection entered request reader %d times", rejectedServer.reads.Load())
	}
	if got := workers.Load(); got != directSignalingPreAuthLimit {
		t.Fatalf("workers started = %d, want %d", got, directSignalingPreAuthLimit)
	}

	_ = clients[0].Close()
	waitDirectServerState(t, harness.server, directSignalingPreAuthLimit-1, directSignalingPreAuthPerIPLimit-1)
	_, replacement := harness.acceptPipe(t, directTestAddress("[2001:db8::fffe]:1234"))
	clients = append(clients, replacement)
	waitDirectServerState(t, harness.server, directSignalingPreAuthLimit, directSignalingPreAuthPerIPLimit-1)
	waitAtomicInt32(t, &workers, directSignalingPreAuthLimit+1)

	harness.stop(t)
	assertDirectServerSlotsEmpty(t, harness.server)
	for _, connection := range clients {
		_ = connection.Close()
	}
}

func TestDirectServerPreAuthNormalizesSourceIPAndRejectsNinth(t *testing.T) {
	harness := newDirectServerHarness(t)
	var workers atomic.Int32
	harness.server.beforeConnectionWorkerRun = func() { workers.Add(1) }
	harness.start(t)

	clients := make([]net.Conn, 0, directSignalingPreAuthPerIPLimit+1)
	for index := 0; index < directSignalingPreAuthPerIPLimit; index++ {
		var address net.Addr = directTestAddress(fmt.Sprintf("192.0.2.44:%d", 2000+index))
		if index%2 != 0 {
			address = directTestAddress(fmt.Sprintf("[::ffff:192.0.2.44]:%d", 2000+index))
		}
		_, client := harness.acceptPipe(t, address)
		clients = append(clients, client)
	}
	waitDirectServerState(t, harness.server, directSignalingPreAuthPerIPLimit, directSignalingPreAuthPerIPLimit)

	rejectedServer, rejectedClient := harness.acceptPipe(t, &net.TCPAddr{IP: net.ParseIP("192.0.2.44"), Port: 3000})
	assertDirectErrorCode(t, readDirectResponse(t, rejectedClient), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED)
	if rejectedServer.reads.Load() != 0 || workers.Load() != directSignalingPreAuthPerIPLimit {
		t.Fatalf("ninth source-IP connection reads=%d workers=%d", rejectedServer.reads.Load(), workers.Load())
	}

	_ = clients[0].Close()
	waitDirectServerState(t, harness.server, directSignalingPreAuthPerIPLimit-1, directSignalingPreAuthPerIPLimit-1)
	_, replacement := harness.acceptPipe(t, directTestAddress("[::ffff:192.0.2.44]:3001"))
	clients = append(clients, replacement)
	waitDirectServerState(t, harness.server, directSignalingPreAuthPerIPLimit, directSignalingPreAuthPerIPLimit)
	waitAtomicInt32(t, &workers, directSignalingPreAuthPerIPLimit+1)

	harness.stop(t)
	assertDirectServerSlotsEmpty(t, harness.server)
	for _, connection := range clients {
		_ = connection.Close()
	}
}

func TestDirectServerFirstRequestTimeoutUsesShortTestHook(t *testing.T) {
	harness := newDirectServerHarness(t)
	harness.server.firstRequestLimit = 25 * time.Millisecond
	harness.start(t)

	started := time.Now()
	_, client := harness.acceptPipe(t, directTestAddress("192.0.2.50:4000"))
	assertDirectErrorCode(t, readDirectResponse(t, client), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("first request timeout took %s", elapsed)
	}
	waitDirectServerState(t, harness.server, 0, 0)
}

func TestDirectServerFirstRequestDeadlineUsesEarlierContextDeadline(t *testing.T) {
	requestDeadline := time.Now().Add(directSignalingFirstRequestLimit)
	contextDeadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), contextDeadline)
	defer cancel()
	if got := earlierDeadline(requestDeadline, ctx); !got.Equal(contextDeadline) {
		t.Fatalf("first request deadline = %s, want earlier context deadline %s", got, contextDeadline)
	}
}

func TestDirectServerPeerLimitRejectsImmediatelyAndReleasesPreAuth(t *testing.T) {
	harness := newDirectServerHarness(t)
	for index := 0; index < directSignalingPeerLimit; index++ {
		harness.server.peerSlots <- struct{}{}
	}
	harness.start(t)

	started := time.Now()
	response := harness.exchange(t, directTestAddress("192.0.2.60:5000"), harness.request("peer-full"))
	assertDirectErrorCode(t, response, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("full peer admission waited %s", elapsed)
	}
	waitDirectServerState(t, harness.server, 0, 0)
	if got := len(harness.server.peerSlots); got != directSignalingPeerLimit {
		t.Fatalf("peer slots = %d, want %d", got, directSignalingPeerLimit)
	}
	for index := 0; index < directSignalingPeerLimit; index++ {
		<-harness.server.peerSlots
	}
}

func TestDirectServerAnswerFailuresReleasePeerExactlyOnce(t *testing.T) {
	harness := newDirectServerHarness(t)
	harness.server.peerConnections = func(pion.Configuration) (*pion.PeerConnection, error) {
		return nil, errors.New("injected peer creation failure")
	}
	harness.start(t)

	for index := 0; index < 64; index++ {
		response := harness.exchange(t, directTestAddress(fmt.Sprintf("[2001:db8:1::%x]:6000", index+1)), harness.request(fmt.Sprintf("factory-failure-%d", index)))
		assertDirectErrorCode(t, response, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL)
		if got := len(harness.server.peerSlots); got != 0 {
			t.Fatalf("peer slots after failure %d = %d", index, got)
		}
	}

	harness.server.peerConnections = pion.NewPeerConnection
	response := harness.exchange(t, directTestAddress("192.0.2.61:6001"), harness.request("invalid-sdp-close-callback"))
	assertDirectErrorCode(t, response, remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_INTERNAL)
	if got := len(harness.server.peerSlots); got != 0 {
		t.Fatalf("peer slots after callback and caller release = %d", got)
	}
	waitDirectServerState(t, harness.server, 0, 0)
}

func TestDirectServerAdmissionRejectionsReleasePreAuth(t *testing.T) {
	harness := newDirectServerHarness(t)
	for index := 0; index < directSignalingPeerLimit; index++ {
		harness.server.peerSlots <- struct{}{}
	}
	harness.start(t)

	protocol := harness.request("protocol")
	protocol.OfferSdp = ""
	assertDirectErrorCode(t, harness.exchange(t, directTestAddress("192.0.2.70:7000"), protocol), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_PROTOCOL)

	mismatch := harness.request("identity-mismatch")
	mismatch.ExpectedDeviceFingerprint = "sha256:mismatch"
	assertDirectErrorCode(t, harness.exchange(t, directTestAddress("192.0.2.70:7001"), mismatch), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_IDENTITY_MISMATCH)

	replay := harness.request("replay")
	assertDirectErrorCode(t, harness.exchange(t, directTestAddress("192.0.2.70:7002"), replay), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED)
	assertDirectErrorCode(t, harness.exchange(t, directTestAddress("192.0.2.70:7003"), replay), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_REPLAYED)
	waitDirectServerState(t, harness.server, 0, 0)

	for index := 0; index < directSignalingPeerLimit; index++ {
		<-harness.server.peerSlots
	}
}

func TestDirectServerCloseBetweenAcquireAndTrackReleasesWithoutWorker(t *testing.T) {
	harness := newDirectServerHarness(t)
	acquired := make(chan struct{})
	resume := make(chan struct{})
	var acquireOnce sync.Once
	var workers atomic.Int32
	harness.server.afterPreAuthAcquire = func() {
		acquireOnce.Do(func() { close(acquired) })
		<-resume
	}
	harness.server.beforeConnectionWorkerRun = func() { workers.Add(1) }
	harness.start(t)

	serverConnection, client := harness.acceptPipe(t, directTestAddress("192.0.2.80:8000"))
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("pre-auth acquisition did not reach interleaving hook")
	}
	if err := harness.server.Close(); err != nil {
		t.Fatal(err)
	}
	close(resume)
	assertDirectErrorCode(t, readDirectResponse(t, client), remoteauthpb.DirectSignalingErrorCode_DIRECT_SIGNALING_ERROR_CODE_OVERLOADED)
	harness.stop(t)
	assertDirectServerSlotsEmpty(t, harness.server)
	if workers.Load() != 0 || serverConnection.reads.Load() != 0 {
		t.Fatalf("close interleaving started workers=%d reads=%d", workers.Load(), serverConnection.reads.Load())
	}
}

type directServerHarness struct {
	server   *DirectServer
	listener *directTestListener
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan error
	now      time.Time
	stopOnce sync.Once
}

func newDirectServerHarness(t *testing.T) *directServerHarness {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity("direct-server-test", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	listener := newDirectTestListener()
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	harness := &directServerHarness{
		listener: listener, ctx: ctx, cancel: cancel, done: make(chan error, 1), now: now,
		server: &DirectServer{
			identity: identity, handler: directTestHandler{}, signalingListener: listener, iceMux: &directTestTCPMux{},
			peerConnections: func(pion.Configuration) (*pion.PeerConnection, error) { return nil, errors.New("unused peer factory") },
			now:             func() time.Time { return now }, firstRequestLimit: directSignalingFirstRequestLimit,
			consumed: make(map[string]time.Time), conns: make(map[net.Conn]struct{}), preAuthByIP: make(map[string]int),
			peerSlots: make(chan struct{}, directSignalingPeerLimit),
		},
	}
	t.Cleanup(func() { harness.stop(t) })
	return harness
}

func (harness *directServerHarness) start(t *testing.T) {
	t.Helper()
	go func() { harness.done <- harness.server.Serve(harness.ctx) }()
}

func (harness *directServerHarness) stop(t *testing.T) {
	t.Helper()
	harness.stopOnce.Do(func() {
		harness.cancel()
		if err := harness.server.Close(); err != nil {
			t.Errorf("close direct server: %v", err)
		}
		select {
		case err := <-harness.done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("serve direct server: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("direct server did not stop")
		}
	})
}

func (harness *directServerHarness) acceptPipe(t *testing.T, remoteAddress net.Addr) (*directTestConn, net.Conn) {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	connection := &directTestConn{Conn: serverSide, remoteAddress: remoteAddress}
	select {
	case harness.listener.connections <- connection:
	case <-time.After(time.Second):
		t.Fatal("direct test listener did not accept connection")
	}
	return connection, clientSide
}

func (harness *directServerHarness) exchange(t *testing.T, remoteAddress net.Addr, request *remoteauthpb.DirectSignalingRequestV1) *remoteauthpb.DirectSignalingResponseV1 {
	t.Helper()
	_, client := harness.acceptPipe(t, remoteAddress)
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := directsignal.WriteMessage(client, request); err != nil {
		t.Fatal(err)
	}
	response := &remoteauthpb.DirectSignalingResponseV1{}
	if err := directsignal.ReadMessage(client, response); err != nil {
		t.Fatal(err)
	}
	return response
}

func (harness *directServerHarness) request(id string) *remoteauthpb.DirectSignalingRequestV1 {
	return &remoteauthpb.DirectSignalingRequestV1{
		SchemaVersion: remoteauth.DirectSignalingSchemaVersion, RequestId: id,
		ExpectedDeviceId: harness.server.identity.DeviceID, ExpectedDeviceFingerprint: harness.server.identity.Fingerprint,
		OfferSdp: "invalid-sdp-for-admission-only", IssuedAtUnixNano: harness.now.UnixNano(),
		ExpiresAtUnixNano: harness.now.Add(remoteauth.DirectSignalingMaxTTL).UnixNano(),
	}
}

func readDirectResponse(t *testing.T, connection net.Conn) *remoteauthpb.DirectSignalingResponseV1 {
	t.Helper()
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response := &remoteauthpb.DirectSignalingResponseV1{}
	if err := directsignal.ReadMessage(connection, response); err != nil {
		t.Fatal(err)
	}
	return response
}

func assertDirectErrorCode(t *testing.T, response *remoteauthpb.DirectSignalingResponseV1, want remoteauthpb.DirectSignalingErrorCode) {
	t.Helper()
	if response.GetError() == nil || response.GetError().GetCode() != want {
		t.Fatalf("direct signaling response = %#v, want error code %s", response, want)
	}
}

func waitDirectServerState(t *testing.T, server *DirectServer, total, perIPMax int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		gotTotal := server.preAuthTotal
		gotPerIPMax := 0
		for _, count := range server.preAuthByIP {
			if count > gotPerIPMax {
				gotPerIPMax = count
			}
		}
		server.mu.Unlock()
		if gotTotal == total && gotPerIPMax == perIPMax {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pre-auth state total=%d max-per-IP=%d, want %d/%d", gotTotal, gotPerIPMax, total, perIPMax)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitAtomicInt32(t *testing.T, value *atomic.Int32, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for value.Load() != int32(want) {
		if time.Now().After(deadline) {
			t.Fatalf("atomic value = %d, want %d", value.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

func assertDirectServerSlotsEmpty(t *testing.T, server *DirectServer) {
	t.Helper()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.preAuthTotal != 0 || len(server.preAuthByIP) != 0 || len(server.peerSlots) != 0 || len(server.conns) != 0 {
		t.Fatalf("server retained pre-auth=%d per-IP=%v peers=%d conns=%d", server.preAuthTotal, server.preAuthByIP, len(server.peerSlots), len(server.conns))
	}
}

type directTestHandler struct{}

func (directTestHandler) ServeDataChannel(context.Context, transport.Transport, string) error {
	return nil
}

type directTestListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newDirectTestListener() *directTestListener {
	return &directTestListener{connections: make(chan net.Conn), closed: make(chan struct{})}
}

func (listener *directTestListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *directTestListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.closed) })
	return nil
}

func (listener *directTestListener) Addr() net.Addr { return directTestAddress("127.0.0.1:0") }

type directTestTCPMux struct{}

func (*directTestTCPMux) Close() error { return nil }
func (*directTestTCPMux) GetConnByUfrag(string, bool, net.IP) (net.PacketConn, error) {
	return nil, net.ErrClosed
}
func (*directTestTCPMux) RemoveConnByUfrag(string) {}

type directTestConn struct {
	net.Conn
	remoteAddress net.Addr
	reads         atomic.Int32
}

func (connection *directTestConn) Read(buffer []byte) (int, error) {
	connection.reads.Add(1)
	return connection.Conn.Read(buffer)
}

func (connection *directTestConn) RemoteAddr() net.Addr { return connection.remoteAddress }

type directTestAddress string

func (address directTestAddress) Network() string { return "tcp" }
func (address directTestAddress) String() string  { return string(address) }
