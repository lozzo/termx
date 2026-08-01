package directoryapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBeginClientRouteVerifiesGrantBeforeReturningDaemonLifecycle(t *testing.T) {
	now := time.Unix(10_000, 0).UTC()
	daemonIdentity := newRouteIdentity(t, "daemon-device")
	attackerIdentity := newRouteIdentity(t, "attacker-device")
	clientIdentity := newRouteIdentity(t, "client-device")
	daemonID := uuid.NewString()
	claims := &cloudv1.CloudRouteGrantClaims{
		GrantId: uuid.NewString(), DaemonId: daemonID, ClientPublicKey: clientIdentity.PublicKey,
		Product: cloudv1.ClientProduct_CLIENT_PRODUCT_TUI, IssuedAt: timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(now.Add(time.Hour)),
	}
	validGrant, err := ticket.SignCloudRouteGrant(daemonIdentity, claims)
	if err != nil {
		t.Fatal(err)
	}
	forgedGrant, err := ticket.SignCloudRouteGrant(attackerIdentity, claims)
	if err != nil {
		t.Fatal(err)
	}
	unknownClaims := proto.Clone(claims).(*cloudv1.CloudRouteGrantClaims)
	unknownClaims.DaemonId = uuid.NewString()
	unknownGrant, err := ticket.SignCloudRouteGrant(attackerIdentity, unknownClaims)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		state     cloudv1.DaemonState
		validCode codes.Code
	}{
		{name: "blocked", state: cloudv1.DaemonState_DAEMON_STATE_BLOCKED, validCode: codes.PermissionDenied},
		{name: "deleted", state: cloudv1.DaemonState_DAEMON_STATE_DELETED, validCode: codes.NotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtimeDirectory, err := directory.New(directory.Config{MailboxSize: 8, GracePeriod: 0})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(runtimeDirectory.Close)
			store := routeEnrollmentStore{daemon: enrollment.Daemon{
				ID: daemonID, AccountID: uuid.NewString(), DeviceID: daemonIdentity.DeviceID,
				DeviceFingerprint: daemonIdentity.Fingerprint, DevicePublicKey: daemonIdentity.PublicKey,
				State: test.state, StateRevision: 2,
			}}
			service, err := NewService(Config{
				Store: store, Directory: runtimeDirectory, Edges: new(edgeconfig.Service), Entitlement: routeEntitlement{},
				EdgeCACertificate: []byte("test-ca"), ChallengeTTL: time.Minute, Now: func() time.Time { return now },
			})
			if err != nil {
				t.Fatal(err)
			}

			if _, err := service.BeginClientRoute(context.Background(), &cloudv1.BeginClientRouteRequest{CloudRouteGrant: forgedGrant}); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("forged grant exposed %s daemon state: %v", test.name, err)
			}
			if _, err := service.BeginClientRoute(context.Background(), &cloudv1.BeginClientRouteRequest{CloudRouteGrant: unknownGrant}); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("unknown daemon grant code=%v err=%v", status.Code(err), err)
			}
			if _, err := service.BeginClientRoute(context.Background(), &cloudv1.BeginClientRouteRequest{CloudRouteGrant: validGrant}); status.Code(err) != test.validCode {
				t.Fatalf("verified grant state code=%v err=%v", status.Code(err), err)
			}
		})
	}
}

func newRouteIdentity(t *testing.T, deviceID string) remoteauth.Identity {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := remoteauth.NewIdentity(deviceID, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

type routeEnrollmentStore struct{ daemon enrollment.Daemon }

func (routeEnrollmentStore) CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error) {
	return "", errors.New("unused")
}
func (routeEnrollmentStore) GetDaemonEnrollmentAccount(context.Context, []byte, time.Time) (string, error) {
	return "", errors.New("unused")
}
func (routeEnrollmentStore) ConsumeDaemonEnrollment(context.Context, []byte, string, string, ed25519.PublicKey, time.Time) (enrollment.Daemon, error) {
	return enrollment.Daemon{}, errors.New("unused")
}
func (store routeEnrollmentStore) GetDaemon(_ context.Context, daemonID string) (enrollment.Daemon, error) {
	if daemonID != store.daemon.ID {
		return enrollment.Daemon{}, enrollment.ErrDaemonUnavailable
	}
	return store.daemon, nil
}
func (store routeEnrollmentStore) ListDaemons(context.Context) ([]enrollment.Daemon, error) {
	return []enrollment.Daemon{store.daemon}, nil
}

type routeEntitlement struct{}

func (routeEntitlement) EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error) {
	return &cloudv1.EffectiveEntitlement{State: cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE, Capability: &cloudv1.CloudCapability{ManagedP2PEnabled: true}}, nil
}
