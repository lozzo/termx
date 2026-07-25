package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestNetworkRouteUnavailableRemainsRetryable(t *testing.T) {
	err := retryableRouteUnavailable("network unavailable")
	var cloudErr *cloudcompanion.Error
	if !errors.As(err, &cloudErr) || cloudErr.Code != cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE || !cloudErr.Retryable {
		t.Fatalf("route unavailable error = %#v", err)
	}
}

func TestAdapterRejectsNonLoopbackAndUnexpectedMediaType(t *testing.T) {
	if _, err := New(Config{ControlPlaneURL: "http://cloud.example.test"}); err == nil {
		t.Fatal("adapter accepted non-loopback Control Plane")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("not protobuf"))
	}))
	defer server.Close()
	adapter, err := New(Config{ControlPlaneURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.BeginLogin(context.Background(), &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("unexpected media type error = %v", err)
	}
}

func TestAdapterDoesNotForwardAdmissionAcrossRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	adapter, err := New(Config{ControlPlaneURL: redirect.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.BeginLogin(context.Background(), &cloudpb.BeginLoginRequest{Method: cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("redirect error = %v", err)
	}
	if redirected.Load() {
		t.Fatal("adapter followed cloud redirect")
	}
}

func TestStreamFrameRoundTripAndLengthLimit(t *testing.T) {
	original := &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{PresenceSessionId: "presence-1", HeartbeatSeconds: 30}}}
	var buffer bytes.Buffer
	if err := WriteFrame(&buffer, original); err != nil {
		t.Fatal(err)
	}
	decoded := &cloudpb.PresenceEvent{}
	if err := ReadFrame(&buffer, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetReady().GetPresenceSessionId() != original.GetReady().GetPresenceSessionId() {
		t.Fatalf("decoded frame = %v", decoded)
	}
	var oversized bytes.Buffer
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], maxBodyBytes+1)
	_, _ = oversized.Write(header[:])
	if err := ReadFrame(&oversized, &cloudpb.PresenceEvent{}); err == nil {
		t.Fatal("stream accepted oversized frame")
	}
}
