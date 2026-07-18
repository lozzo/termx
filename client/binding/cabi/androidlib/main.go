// Package main 生成 Android 使用的 termx Client Engine c-shared library。
// 稳定 C ABI 只传递 serialized Proto 与 opaque handle；Android spike 的 in-process daemon 仅用于证明真实 Pion/auth/Hello/API 链路。
package main

/*
#include <stdlib.h>
#include <stddef.h>
#include <stdint.h>

typedef uint64_t termx_handle_t;
typedef enum termx_status_v1 {
  TERMX_STATUS_OK = 0,
  TERMX_STATUS_INVALID_ARGUMENT = 1,
  TERMX_STATUS_INVALID_HANDLE = 2,
  TERMX_STATUS_CLOSED = 3,
  TERMX_STATUS_CAPACITY = 4,
  TERMX_STATUS_INTERNAL = 5
} termx_status_v1;
typedef struct termx_buffer_v1 {
  termx_handle_t buffer_handle;
  const uint8_t *data;
  size_t length;
} termx_buffer_v1;
*/
import "C"

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	apilayer "github.com/lozzow/termx/api_layer"
	"github.com/lozzow/termx/client/adapter/managed"
	pionadapter "github.com/lozzow/termx/client/adapter/managed/pion"
	"github.com/lozzow/termx/client/binding"
	"github.com/lozzow/termx/client/endpoint"
	clientruntime "github.com/lozzow/termx/client/runtime"
	core "github.com/lozzow/termx/core"
	"github.com/lozzow/termx/proto/bindingpb"
	"github.com/lozzow/termx/proto/cloudpb"
	remotev2daemon "github.com/lozzow/termx/remote/daemon"
	remotev2webrtc "github.com/lozzow/termx/remote/webrtc"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/remoteauth"
)

var androidLibrary = struct {
	sync.Mutex
	registry   *binding.Registry
	runtimeDir string
	hosts      map[uint64]*androidSpikeHost
	buffers    map[uint64]unsafe.Pointer
	nextBuffer uint64
}{registry: binding.NewRegistry(), hosts: make(map[uint64]*androidSpikeHost), buffers: make(map[uint64]unsafe.Pointer)}

// androidSpikeHost 是 PA005N1 的 Android 进程内纵向 harness。
// 它复用生产 Client Engine、Pion、remote auth、protocol 与 API Layer；PA005N2 会把 Cloud/credential 注入替换为真实平台适配。
type androidSpikeHost struct {
	ctx        context.Context
	cancel     context.CancelFunc
	server     *core.Server
	answerer   remotev2webrtc.Answerer
	identity   remoteauth.Identity
	credential remoteauth.ClientAccessCredential
	store      *remoteauth.AccessStore
	now        time.Time
	generation atomic.Uint64
	closeOnce  sync.Once
}

func newAndroidSpikeHost(runtimeDir string) (*androidSpikeHost, error) {
	if runtimeDir == "" {
		return nil, fmt.Errorf("android runtime directory is required")
	}
	stateDir := filepath.Join(runtimeDir, fmt.Sprintf("termx-go-client-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create android client state directory: %w", err)
	}
	_, daemonPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	identity, err := remoteauth.NewIdentity("android-spike-daemon", daemonPrivateKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	clientIdentity, err := remoteauth.GenerateClientAccessIdentity("android-spike", rand.Reader)
	if err != nil {
		return nil, err
	}
	store, err := remoteauth.LoadAccessStore(stateDir, identity, remoteauth.AccessStoreOptions{Now: func() time.Time { return now }})
	if err != nil {
		return nil, err
	}
	bundle, _, err := store.IssuePairingBundle(remoteauth.PairingIssueOptions{
		Scope: remoteauth.Scope{AllowDaemon: true}, TicketTTL: time.Hour, GrantLifetime: time.Hour, Now: now,
	})
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	payload, err := remoteauth.EncodePairingBundle(bundle)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	exchanged, err := store.RedeemPairingBundle(payload, clientIdentity.PublicKey, "android-go-client", now)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	server := core.NewServer(core.WithApplicationExecutorFactory(apilayer.CoreApplicationExecutorFactory))
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	host := &androidSpikeHost{
		ctx: daemonCtx, cancel: daemonCancel, server: server, identity: identity, store: store, now: now,
		credential: remoteauth.ClientAccessCredential{
			Version: 1, EndpointID: "android-spike", Identity: clientIdentity,
			CapabilityGrant: exchanged.Grant, UpdatedAt: now,
		},
	}
	host.answerer = remotev2webrtc.Answerer{Handler: remotev2daemon.SessionAcceptor{
		Core: server, Identity: identity, AccessStore: store, Now: func() time.Time { return now },
	}}
	return host, nil
}

// OpenSession 建立 PA005N1 的真实 managed WebRTC session。
// endpoint_id=cancel 会只等待 context，用于从 JNI 证明 operation cancel；其他 endpoint 必须精确为 android-spike。
func (host *androidSpikeHost) OpenSession(ctx context.Context, request *bindingpb.OpenSessionRequest) (clientruntime.ApplicationReadySession, error) {
	if request.GetEndpointId() == "cancel" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if request.GetEndpointId() != "android-spike" {
		return nil, fmt.Errorf("unsupported Android spike endpoint %q", request.GetEndpointId())
	}
	target := endpoint.Endpoint{
		ID:             "android-spike",
		DaemonIdentity: endpoint.DaemonIdentity{DeviceID: host.identity.DeviceID, DeviceFingerprint: host.identity.Fingerprint},
		Routes: map[endpoint.RouteID]endpoint.AccessRoute{
			"webrtc": {
				ID: "webrtc", Kind: endpoint.RouteManagedWebRTC, Enabled: true,
				Source: endpoint.SourceCloud, PolicySource: endpoint.SourceUser,
				CredentialRef: "credential:android-spike", TargetDeviceID: host.identity.DeviceID,
				AccountProfile: "default", RelayMode: endpoint.RelayDirect,
			},
		},
	}
	generation := clientruntime.SessionGeneration(host.generation.Add(1))
	attempt, err := clientruntime.NewAttemptRequest(target, "webrtc", generation, clientruntime.ConnectIntentInteractive)
	if err != nil {
		return nil, err
	}
	dialer := &managed.Dialer{
		Cloud: androidSpikeCompanion(host.ctx, host.answerer), Peers: pionadapter.Factory{}, ClientName: "android-go-client",
		Authorization: managed.CapabilityAuthorizer{
			Credentials: androidSpikeCredentialSource{credential: host.credential}, Now: func() time.Time { return host.now },
		},
		Now: func() time.Time { return host.now },
	}
	ready, err := dialer.Dial(ctx, attempt)
	if err != nil {
		return nil, err
	}
	application, ok := ready.(clientruntime.ApplicationReadySession)
	if !ok {
		_ = ready.Close()
		return nil, fmt.Errorf("Android managed route returned no application session")
	}
	return application, nil
}

func (host *androidSpikeHost) close() error {
	var err error
	host.closeOnce.Do(func() {
		host.cancel()
		err = host.store.Close()
	})
	return err
}

type androidSpikeCredentialSource struct {
	credential remoteauth.ClientAccessCredential
}

func (source androidSpikeCredentialSource) ResolveClientCredential(_ context.Context, endpointID, credentialRef string) (remoteauth.ClientAccessCredential, error) {
	if endpointID != "android-spike" || credentialRef != "credential:android-spike" {
		return remoteauth.ClientAccessCredential{}, fmt.Errorf("Android spike credential reference is invalid")
	}
	return source.credential, nil
}

func androidSpikeCompanion(daemonCtx context.Context, answerer remotev2webrtc.Answerer) *cloudcompanion.FakeClient {
	return &cloudcompanion.FakeClient{
		ResolveEndpointFunc: func(_ context.Context, request *cloudpb.ResolveEndpointRequest) (*cloudpb.ResolvedEndpoint, error) {
			return &cloudpb.ResolvedEndpoint{
				EndpointId: request.GetEndpointId(), TargetDeviceId: request.GetTargetDeviceId(), ManagedSessionId: "android-managed-1",
			}, nil
		},
		CreateSignalingSessionFunc: func(_ context.Context, request *cloudpb.CreateSignalingSessionRequest) (cloudcompanion.SignalingStream, error) {
			answer, err := answerer.Answer(daemonCtx, &cloudpb.SignalingOffer{
				SignalingSessionId: "android-signal-1", ManagedSessionId: request.GetManagedSessionId(), Sdp: request.GetOfferSdp(),
			}, nil)
			if err != nil {
				return nil, err
			}
			stream := cloudcompanion.NewFakeSignalingStream(1)
			if err := stream.Push(&cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: answer}}); err != nil {
				return nil, err
			}
			return stream, nil
		},
	}
}

//export termx_android_spike_set_runtime_dir
func termx_android_spike_set_runtime_dir(value *C.char) C.termx_status_v1 {
	if value == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	androidLibrary.runtimeDir = C.GoString(value)
	androidLibrary.Unlock()
	return C.TERMX_STATUS_OK
}

//export termx_client_abi_version
func termx_client_abi_version() C.uint32_t { return C.uint32_t(binding.ABIVersion) }

//export termx_engine_create
func termx_engine_create(out *C.termx_handle_t) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	androidLibrary.Lock()
	runtimeDir := androidLibrary.runtimeDir
	androidLibrary.Unlock()
	host, err := newAndroidSpikeHost(runtimeDir)
	if err != nil {
		return status(err)
	}
	handle, err := androidLibrary.registry.CreateEngine(host)
	if err != nil {
		_ = host.close()
		return status(err)
	}
	androidLibrary.Lock()
	androidLibrary.hosts[handle] = host
	androidLibrary.Unlock()
	*out = C.termx_handle_t(handle)
	return C.TERMX_STATUS_OK
}

//export termx_engine_open_session
func termx_engine_open_session(engine C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.OpenSession(uint64(engine), payload)
	})
}

//export termx_engine_execute
func termx_engine_execute(engine, session C.termx_handle_t, data *C.uint8_t, length C.size_t, out *C.termx_handle_t) C.termx_status_v1 {
	return operation(data, length, out, func(payload []byte) (uint64, error) {
		return androidLibrary.registry.Execute(uint64(engine), uint64(session), payload)
	})
}

//export termx_engine_next_event
func termx_engine_next_event(engine C.termx_handle_t, timeoutMillis C.uint32_t, out *C.termx_buffer_v1) C.termx_status_v1 {
	if out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	ctx := context.Background()
	cancel := func() {}
	if timeoutMillis > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMillis)*time.Millisecond)
	}
	defer cancel()
	payload, err := androidLibrary.registry.NextEvent(ctx, uint64(engine))
	if err != nil {
		return status(err)
	}
	pointer := C.malloc(C.size_t(len(payload)))
	if pointer == nil {
		return C.TERMX_STATUS_CAPACITY
	}
	copy(unsafe.Slice((*byte)(pointer), len(payload)), payload)
	androidLibrary.Lock()
	androidLibrary.nextBuffer++
	handle := androidLibrary.nextBuffer
	if handle == 0 {
		androidLibrary.Unlock()
		C.free(pointer)
		return C.TERMX_STATUS_CAPACITY
	}
	androidLibrary.buffers[handle] = pointer
	androidLibrary.Unlock()
	out.buffer_handle = C.termx_handle_t(handle)
	out.data = (*C.uint8_t)(pointer)
	out.length = C.size_t(len(payload))
	return C.TERMX_STATUS_OK
}

//export termx_engine_cancel
func termx_engine_cancel(engine, operationHandle C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.Cancel(uint64(engine), uint64(operationHandle)))
}

//export termx_engine_close_session
func termx_engine_close_session(engine, session C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.CloseSession(uint64(engine), uint64(session)))
}

//export termx_engine_release
func termx_engine_release(engine, handle C.termx_handle_t) C.termx_status_v1 {
	return status(androidLibrary.registry.Release(uint64(engine), uint64(handle)))
}

//export termx_engine_close
func termx_engine_close(engine C.termx_handle_t) C.termx_status_v1 {
	handle := uint64(engine)
	err := androidLibrary.registry.CloseEngine(handle)
	androidLibrary.Lock()
	host := androidLibrary.hosts[handle]
	delete(androidLibrary.hosts, handle)
	androidLibrary.Unlock()
	if host != nil {
		if closeErr := host.close(); err == nil {
			err = closeErr
		}
	}
	return status(err)
}

//export termx_buffer_free
func termx_buffer_free(buffer C.termx_handle_t) C.termx_status_v1 {
	handle := uint64(buffer)
	androidLibrary.Lock()
	pointer := androidLibrary.buffers[handle]
	if pointer != nil {
		delete(androidLibrary.buffers, handle)
	}
	androidLibrary.Unlock()
	if pointer == nil {
		return C.TERMX_STATUS_INVALID_HANDLE
	}
	C.free(pointer)
	return C.TERMX_STATUS_OK
}

func operation(data *C.uint8_t, length C.size_t, out *C.termx_handle_t, invoke func([]byte) (uint64, error)) C.termx_status_v1 {
	if data == nil || length == 0 || uint64(length) > uint64(binding.MaxPayloadBytes) || out == nil {
		return C.TERMX_STATUS_INVALID_ARGUMENT
	}
	payload := C.GoBytes(unsafe.Pointer(data), C.int(length))
	handle, err := invoke(payload)
	if err != nil {
		return status(err)
	}
	*out = C.termx_handle_t(handle)
	return C.TERMX_STATUS_OK
}

func status(err error) C.termx_status_v1 {
	if err == nil {
		return C.TERMX_STATUS_OK
	}
	switch {
	case errors.Is(err, binding.ErrInvalidHandle):
		return C.TERMX_STATUS_INVALID_HANDLE
	case errors.Is(err, binding.ErrClosed):
		return C.TERMX_STATUS_CLOSED
	case errors.Is(err, binding.ErrHandleActive):
		return C.TERMX_STATUS_INVALID_ARGUMENT
	default:
		return C.TERMX_STATUS_INTERNAL
	}
}

func main() {}
