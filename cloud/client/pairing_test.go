package client

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/anytty/anytty/proto/remoteauthpb"
	"github.com/anytty/anytty/shared/remoteauth"
)

func TestPairingRouteBuildsOnlineAdmissionWithoutGrantOrFullCA(t *testing.T) {
	claim := bytes.Repeat([]byte{0x41}, 16)
	offer := &remoteauthpb.PairingClaimOffer{
		SchemaVersion: remoteauth.PairingClaimOfferVersion, Claim: claim, DeviceId: "device-1", DevicePublicKey: bytes.Repeat([]byte{0x42}, ed25519.PublicKeySize),
		ExpiresAtUnixNano: time.Now().Add(time.Minute).UnixNano(),
		Routes: []*remoteauthpb.PairingRouteSeed{{RouteId: "cloud", Route: &remoteauthpb.PairingRouteSeed_ManagedWebrtc{ManagedWebrtc: &remoteauthpb.PairingManagedRouteSeed{
			DaemonId: "daemon-1", EdgeId: "edge-1", PublicEndpoint: "edge.example:443", ServerName: "edge.example", CaCertificateDerSha256: bytes.Repeat([]byte{0x43}, sha256.Size),
		}}}},
	}
	payload, err := remoteauth.EncodePairingClaimOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewClient(Config{ControllerAddress: "controller.example:443", ControllerServerName: "controller.example"})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := network.PairingRoute(payload)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(claim)
	if resolution.routeGrant != nil || resolution.locator != nil || resolution.pairingBootstrap == nil || resolution.pairingAdmission == nil ||
		resolution.pairingAdmission.GetDaemonId() != "daemon-1" || resolution.pairingAdmission.GetDeviceId() != "device-1" || !bytes.Equal(resolution.pairingAdmission.GetPairingClaimSha256(), wantDigest[:]) {
		t.Fatalf("pairing resolution = %#v", resolution)
	}
}
