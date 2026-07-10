package main

import (
	"context"
	"testing"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"github.com/lozzow/termx/termx-shared/cloudcompanion"
	"github.com/lozzow/termx/termx-shared/connection"
)

func TestV3ManagedEndpointFailsClosedWhenCompanionIsUnavailable(t *testing.T) {
	dialer := v3ManagedCloudEndpointDialer()
	_, err := dialer(context.Background(), connection.Config{
		ID: "lab", Transport: connection.TransportHubP2P, HubDeviceID: "device-1",
	})
	if !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		t.Fatalf("dial error = %v, want COMPANION_MISSING", err)
	}
}
