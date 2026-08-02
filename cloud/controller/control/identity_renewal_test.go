package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestIdentityRenewalMustMatchControlStreamPeerCertificate(t *testing.T) {
	peerFingerprint := sha256.Sum256([]byte("authenticated peer certificate"))
	wrongFingerprint := sha256.Sum256([]byte("different certificate"))
	called := 0
	service := &Service{config: Config{RenewIdentityCertificate: func(_ context.Context, edgeID string, request *cloudv1.EdgeIdentityRenewRequest) (*cloudv1.EdgeIdentityRenewResponse, error) {
		called++
		if edgeID != "edge-renewal" || request.GetRequestId() != "request-1" {
			t.Fatalf("edge=%q request=%q", edgeID, request.GetRequestId())
		}
		return &cloudv1.EdgeIdentityRenewResponse{RequestId: request.GetRequestId(), CertificateSha256: bytes.Repeat([]byte{3}, sha256.Size)}, nil
	}}}
	event := &cloudv1.EdgeEvent{
		SenderId: "edge-renewal",
		Payload: &cloudv1.EdgeEvent_IdentityRenew{IdentityRenew: &cloudv1.EdgeIdentityRenewRequest{
			RequestId: "request-1", CsrPem: []byte("csr"), CurrentCertificateSha256: wrongFingerprint[:],
		}},
	}
	if _, err := service.applyEvent(context.Background(), event, peerFingerprint[:]); err == nil {
		t.Fatal("renewal using a different certificate fingerprint was accepted")
	}
	if called != 0 {
		t.Fatalf("signer called for mismatched peer fingerprint: %d", called)
	}
	event.GetIdentityRenew().CurrentCertificateSha256 = peerFingerprint[:]
	response, err := service.applyEvent(context.Background(), event, peerFingerprint[:])
	if err != nil {
		t.Fatal(err)
	}
	wrapped, ok := response.(*cloudv1.ControllerCommand_IdentityRenew)
	if !ok || wrapped.IdentityRenew.GetRequestId() != "request-1" || called != 1 {
		t.Fatalf("response=%T called=%d", response, called)
	}
}
