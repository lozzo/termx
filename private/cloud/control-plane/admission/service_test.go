package admission_test

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/admission"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

type sessionSource struct {
	session domain.ManagedSession
}

func (source sessionSource) ManagedSession(accountID, sessionID string, now time.Time) (domain.ManagedSession, error) {
	if source.session.AccountID != accountID || source.session.ID != sessionID || !now.Before(source.session.ExpiresAt) {
		return domain.ManagedSession{}, errors.New("managed session not found")
	}
	return source.session, nil
}

func TestServiceDerivesHubAndPrincipalFromManagedSession(t *testing.T) {
	now := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	privateKey := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	signer, err := servicecredential.NewSigner("cp-key", privateKey, now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := servicecredential.NewHubAdmissionIssuer("control-plane.test", signer)
	if err != nil {
		t.Fatal(err)
	}
	session := domain.ManagedSession{
		ID: "managed-1", AccountID: "account-1", ClientDeviceID: "client-1", TargetDeviceID: "daemon-1",
		Hub: domain.HubAssignment{HubID: "hub-eu-1", Region: "eu-west"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	service, err := admission.NewService(sessionSource{session: session}, issuer)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := service.Issue(admission.Command{
		TicketID: "ticket-1", AccountID: session.AccountID, ManagedSessionID: session.ID,
		PrincipalKind: servicecredential.PrincipalClient,
		Operations:    []servicecredential.HubOperation{servicecredential.HubOperationOffer, servicecredential.HubOperationCandidate},
		TTL:           time.Minute,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	ring, _ := servicecredential.NewKeyRing(signer.PublicKey())
	claims, err := servicecredential.VerifyHubAdmission(ring, ticket.Bytes(), servicecredential.HubAdmissionExpectation{
		Issuer: "control-plane.test", AudienceHubID: session.Hub.HubID, PrincipalKind: servicecredential.PrincipalClient,
		AccountID: session.AccountID, DeviceID: session.ClientDeviceID, ManagedSessionID: session.ID,
		TargetDeviceID: session.TargetDeviceID, Operation: servicecredential.HubOperationOffer,
	}, now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claims.DeviceID != session.ClientDeviceID || claims.TargetDeviceID != session.TargetDeviceID {
		t.Fatalf("principal binding = %#v", claims)
	}

	_, err = service.Issue(admission.Command{
		TicketID: "ticket-2", AccountID: session.AccountID, ManagedSessionID: session.ID,
		PrincipalKind: servicecredential.PrincipalDaemon,
		Operations:    []servicecredential.HubOperation{servicecredential.HubOperationOffer},
		TTL:           time.Minute,
	}, now)
	if !errors.Is(err, servicecredential.ErrCredentialBinding) {
		t.Fatalf("daemon offer error = %v", err)
	}
}
