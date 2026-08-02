package enrollment

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	"github.com/anytty/anytty/cloud/controller/control"
	"github.com/anytty/anytty/cloud/controller/directory"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestDaemonManagementDerivesAccountForEnrollmentAndStateChange(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	enrollments := &managementEnrollmentStoreFake{}
	persistent := &managementStoreFake{daemon: Daemon{ID: "daemon-a", AccountID: "11111111-1111-1111-1111-111111111111", DisplayName: "开发 Mac", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE, StateRevision: 4}}
	runtimeDirectory, err := directory.New(directory.Config{MailboxSize: 8, GracePeriod: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimeDirectory.Close)
	enrollmentService := &Service{config: Config{Store: enrollments, EnrollmentTTL: 10 * time.Minute, Now: func() time.Time { return now }}}
	if _, err := enrollmentService.CreateEnrollment(context.Background(), &cloudv1.CreateDaemonEnrollmentRequest{AccountName: "残缺账号", DaemonName: "daemon"}, "anytty cloud enroll"); err == nil {
		t.Fatal("enrollment without an existing account ID was accepted")
	}
	service, err := NewManagementService(ManagementConfig{Enrollment: enrollmentService, Store: persistent, Directory: runtimeDirectory, Control: &control.Service{}, CommandPrefix: "anytty cloud enroll", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := account.ContextWithIdentity(context.Background(), account.Identity{Account: &cloudv1.AccountProfile{AccountId: "11111111-1111-1111-1111-111111111111", DisplayName: "账号 A"}, RefreshID: "refresh-a"})

	created, err := service.CreateMyEnrollment(ctx, &cloudv1.CreateMyDaemonEnrollmentRequest{DaemonName: "  开发 Mac  "})
	if err != nil {
		t.Fatal(err)
	}
	if enrollments.accountID != "11111111-1111-1111-1111-111111111111" || enrollments.accountName != "账号 A" || enrollments.daemonName != "开发 Mac" {
		t.Fatalf("enrollment account=%q name=%q daemon=%q", enrollments.accountID, enrollments.accountName, enrollments.daemonName)
	}
	if created.GetEnrollCommand() == "" {
		t.Fatal("enrollment command is empty")
	}

	changed, err := service.ChangeMyDaemonState(ctx, &cloudv1.ChangeMyDaemonStateRequest{DaemonId: "daemon-a", TargetState: cloudv1.DaemonState_DAEMON_STATE_BLOCKED, ExpectedStateRevision: 4, Reason: "  暂停服务  "})
	if err != nil {
		t.Fatal(err)
	}
	if persistent.accountID != "11111111-1111-1111-1111-111111111111" || persistent.reason != "暂停服务" || changed.GetDaemon().GetState() != cloudv1.DaemonState_DAEMON_STATE_BLOCKED {
		t.Fatalf("change account=%q reason=%q response=%+v", persistent.accountID, persistent.reason, changed)
	}

	other := account.ContextWithIdentity(context.Background(), account.Identity{Account: &cloudv1.AccountProfile{AccountId: "22222222-2222-2222-2222-222222222222"}, RefreshID: "refresh-b"})
	if _, err := service.ChangeMyDaemonState(other, &cloudv1.ChangeMyDaemonStateRequest{DaemonId: "daemon-a", TargetState: cloudv1.DaemonState_DAEMON_STATE_DELETED, ExpectedStateRevision: 5, Reason: "越权"}); !errors.Is(err, errManagementOwnership) {
		t.Fatalf("cross-account change error=%v", err)
	}
}

var errManagementOwnership = errors.New("daemon does not belong to account")

type managementEnrollmentStoreFake struct{ accountID, accountName, daemonName string }

func (store *managementEnrollmentStoreFake) CreateDaemonEnrollment(_ context.Context, accountID, accountName, daemonName string, _ []byte, _ time.Time, _ time.Time) (string, error) {
	store.accountID, store.accountName, store.daemonName = accountID, accountName, daemonName
	return accountID, nil
}
func (*managementEnrollmentStoreFake) GetDaemonEnrollmentAccount(context.Context, []byte, time.Time) (string, error) {
	return "", errors.New("unused")
}
func (*managementEnrollmentStoreFake) ConsumeDaemonEnrollment(context.Context, []byte, string, string, ed25519.PublicKey, time.Time) (Daemon, error) {
	return Daemon{}, errors.New("unused")
}
func (*managementEnrollmentStoreFake) GetDaemon(context.Context, string) (Daemon, error) {
	return Daemon{}, errors.New("unused")
}
func (*managementEnrollmentStoreFake) ListDaemons(context.Context) ([]Daemon, error) { return nil, nil }

type managementStoreFake struct {
	daemon            Daemon
	accountID, reason string
}

func (store *managementStoreFake) ListDaemonsByAccount(_ context.Context, accountID string) ([]Daemon, error) {
	if accountID != store.daemon.AccountID {
		return nil, nil
	}
	return []Daemon{store.daemon}, nil
}
func (store *managementStoreFake) ChangeDaemonState(_ context.Context, accountID, daemonID string, target cloudv1.DaemonState, expectedRevision uint64, reason string, now time.Time) (Daemon, error) {
	store.accountID, store.reason = accountID, reason
	if accountID != store.daemon.AccountID || daemonID != store.daemon.ID {
		return Daemon{}, errManagementOwnership
	}
	if expectedRevision != store.daemon.StateRevision {
		return Daemon{}, errors.New("revision conflict")
	}
	store.daemon.State = target
	store.daemon.StateRevision++
	store.daemon.UpdatedAt = now
	return store.daemon, nil
}
