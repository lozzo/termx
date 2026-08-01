package control

import (
	"context"
	"errors"
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestReconcileDesiredCertificateRepairsMatchingHello(t *testing.T) {
	desired := &cloudv1.EdgeCertificateBundle{TargetEdgeId: "edge-1", CertificateProfileId: "profile-1", Revision: 3}
	callbackCount := 0
	service := &Service{config: Config{
		DesiredCertificate: func(context.Context, string) (*cloudv1.EdgeCertificateBundle, error) { return desired, nil },
		CertificateApplied: func(_ context.Context, edgeID string, applied *cloudv1.CertificateApplied) error {
			callbackCount++
			if edgeID != "edge-1" || !applied.GetApplied() || applied.GetCertificateProfileId() != "profile-1" || applied.GetRevision() != 3 {
				t.Fatalf("unexpected reconciliation: edge=%s applied=%+v", edgeID, applied)
			}
			return nil
		},
	}}
	bundle, err := service.reconcileDesiredCertificate(context.Background(), "edge-1", &cloudv1.EdgeHello{CertificateProfileId: "profile-1", CertificateVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if bundle != nil || callbackCount != 1 {
		t.Fatalf("bundle=%+v callbackCount=%d want nil/1", bundle, callbackCount)
	}

	reconcileErr := errors.New("persist applied")
	service.config.CertificateApplied = func(context.Context, string, *cloudv1.CertificateApplied) error { return reconcileErr }
	if _, err := service.reconcileDesiredCertificate(context.Background(), "edge-1", &cloudv1.EdgeHello{CertificateProfileId: "profile-1", CertificateVersion: 3}); !errors.Is(err, reconcileErr) {
		t.Fatalf("reconciliation error=%v want %v", err, reconcileErr)
	}
}

func TestReconcileDesiredCertificateReturnsMismatchedDesiredBundle(t *testing.T) {
	desired := &cloudv1.EdgeCertificateBundle{TargetEdgeId: "edge-1", CertificateProfileId: "profile-2", Revision: 1}
	service := &Service{config: Config{
		DesiredCertificate: func(context.Context, string) (*cloudv1.EdgeCertificateBundle, error) { return desired, nil },
		CertificateApplied: func(context.Context, string, *cloudv1.CertificateApplied) error {
			t.Fatal("mismatched Hello must not be recorded as applied")
			return nil
		},
	}}
	bundle, err := service.reconcileDesiredCertificate(context.Background(), "edge-1", &cloudv1.EdgeHello{CertificateProfileId: "profile-1", CertificateVersion: 7})
	if err != nil {
		t.Fatal(err)
	}
	if bundle != desired {
		t.Fatalf("bundle=%+v want desired bundle", bundle)
	}
}

func TestRefreshCertificateResolvesLatestDesiredAtWriterSequence(t *testing.T) {
	desired := &cloudv1.EdgeCertificateBundle{
		TargetEdgeId: "edge-1", CertificateProfileId: "profile-1", Revision: 1,
		CertificateChainPem: []byte("revision-1-certificate"), PrivateKeyPem: []byte("revision-1-key"),
	}
	outbound := make(chan externalCommand, 1)
	generation := &connectionGeneration{external: outbound, invalidated: make(chan struct{})}
	service := &Service{
		config:      Config{DesiredCertificate: func(context.Context, string) (*cloudv1.EdgeCertificateBundle, error) { return desired, nil }},
		connections: map[string]*connectionGeneration{"connection-1": generation}, edgeConnections: map[string]string{"edge-1": "connection-1"},
	}
	refreshResult := make(chan error, 1)
	go func() { refreshResult <- service.RefreshCertificate(context.Background(), "edge-1") }()
	request := <-outbound
	desired = &cloudv1.EdgeCertificateBundle{
		TargetEdgeId: "edge-1", CertificateProfileId: "profile-2", Revision: 7,
		CertificateChainPem: []byte("revision-7-certificate"), PrivateKeyPem: []byte("revision-7-key"),
	}
	payload, shouldSend, err := service.resolveExternalCommand(context.Background(), "edge-1", request)
	if err != nil {
		t.Fatal(err)
	}
	certificatePayload, ok := payload.(*cloudv1.ControllerCommand_CertificateBundle)
	if !shouldSend || !ok || certificatePayload.CertificateBundle != desired {
		t.Fatalf("resolved payload=%+v shouldSend=%v want latest desired", payload, shouldSend)
	}
	request.result <- nil
	if err := <-refreshResult; err != nil {
		t.Fatal(err)
	}
}
