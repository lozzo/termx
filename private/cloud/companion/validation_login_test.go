package companion

import (
	"testing"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

func TestPublicHTTPLoginURLRequiresExplicitStagingProfile(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	flow := &cloudpb.LoginFlow{FlowId: "flow", VerificationUri: "http://114.66.58.243:41100/device?code=ABCD", UserCode: "ABCD", ExpiresAtUnix: uint64(now.Add(time.Minute).Unix()), PollIntervalMillis: 1000}
	if err := validateLoginFlow(flow, now, false); !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL) {
		t.Fatalf("default public HTTP login URL = %v", err)
	}
	if err := validateLoginFlow(flow, now, true); err != nil {
		t.Fatalf("explicit staging public HTTP login URL = %v", err)
	}
}

func TestAuthorizationRevokedIsAStableCloudErrorCode(t *testing.T) {
	if !validCloudErrorCode(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_AUTHORIZATION_REVOKED, false) {
		t.Fatal("authorization revoked cloud error was rejected")
	}
}
