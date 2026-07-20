package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestAdapterConsumesProtoDeviceEnrollmentSession(t *testing.T) {
	now := time.Date(2026, 7, 20, 16, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != ControlCompleteEnrollmentPath || request.Header.Get("Content-Type") != ProtobufMediaType {
			http.Error(writer, "unexpected request", http.StatusBadRequest)
			return
		}
		response := &cloudpb.DeviceEnrollmentServiceSession{
			Session:           &cloudpb.CloudSessionSummary{AccountId: "account-1", AccountLabel: "Alice", DeviceId: "daemon-1", ExpiresAtUnix: uint64(now.Add(time.Hour).Unix())},
			AccessToken:       []byte("private-device-access-token"),
			ControlEnrollment: &cloudpb.DaemonControlEnrollment{AccountId: "account-1", DaemonDeviceId: "daemon-1", AuthEpoch: 3, EnrolledAtUnixMillis: now.UnixMilli(), VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: "control-1", PublicKey: make([]byte, 32), NotBeforeUnixMillis: now.Add(-time.Hour).UnixMilli(), NotAfterUnixMillis: now.Add(time.Hour).UnixMilli()}}},
		}
		payload, _ := proto.Marshal(response)
		writer.Header().Set("Content-Type", ProtobufMediaType)
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	adapter, err := New(Config{ControlPlaneURL: server.URL, HubURL: server.URL, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.CompleteDeviceEnrollment(context.Background(), &cloudpb.CompleteDeviceEnrollmentRequest{FlowId: "flow-1", Proof: &cloudpb.DeviceProof{DeviceId: "daemon-1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Session.Destroy()
	if result.Session.Metadata().Kind != session.KindDevice || result.Session.Metadata().DeviceID != "daemon-1" || result.ControlEnrollment.GetAuthEpoch() != 3 {
		t.Fatalf("device enrollment result = (%v, %v)", result.Session.Metadata(), result.ControlEnrollment)
	}
	authorization := result.Session.Authorization()
	defer authorization.Destroy()
	if string(authorization.Bytes()) != "private-device-access-token" {
		t.Fatal("private device token was not stored in the Companion session")
	}
}
