package operator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/account"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestAccountManagementAuthorizationMatrix(t *testing.T) {
	now := time.Unix(4_000, 0).UTC()
	profile := &cloudv1.AccountProfile{AccountId: "actor"}
	tests := []struct {
		name    string
		ctx     context.Context
		wantErr error
	}{
		{name: "no identity", ctx: context.Background(), wantErr: account.ErrUnauthenticated},
		{name: "user", ctx: permissionContext(profile, cloudv1.AccountRole_ACCOUNT_ROLE_USER, now.Add(time.Minute)), wantErr: account.ErrForbidden},
		{name: "operator", ctx: permissionContext(profile, cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR, now.Add(time.Minute)), wantErr: account.ErrForbidden},
		{name: "admin recent auth expired", ctx: permissionContext(profile, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, now), wantErr: account.ErrRecentAuthenticationRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := requireAdmin(test.ctx, true, now); !errors.Is(err, test.wantErr) {
				t.Fatalf("requireAdmin error = %v, want %v", err, test.wantErr)
			}
		})
	}
	if _, err := requireAdmin(permissionContext(profile, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, now.Add(time.Minute)), true, now); err != nil {
		t.Fatalf("recent admin rejected: %v", err)
	}
}

func TestOperatorCannotElevateAndSelfConflictsFailBeforeStore(t *testing.T) {
	now := time.Unix(5_000, 0).UTC()
	store := &permissionStoreFake{}
	service := &Service{config: Config{Store: store, Now: func() time.Time { return now }}}
	operatorContext := permissionContext(&cloudv1.AccountProfile{AccountId: "operator"}, cloudv1.AccountRole_ACCOUNT_ROLE_OPERATOR, now.Add(time.Minute))
	if _, err := service.SetAccountRole(operatorContext, &cloudv1.SetAccountRoleRequest{AccountId: "target", Role: cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, Enabled: true, Reason: "elevation"}); !errors.Is(err, account.ErrForbidden) {
		t.Fatalf("operator elevation error = %v", err)
	}
	adminContext := permissionContext(&cloudv1.AccountProfile{AccountId: "admin"}, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, now.Add(time.Minute))
	if _, err := service.SetAccountRole(adminContext, &cloudv1.SetAccountRoleRequest{AccountId: "admin", Role: cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, Enabled: false, Reason: "self removal"}); !errors.Is(err, account.ErrAccountConflict) {
		t.Fatalf("self admin removal error = %v", err)
	}
	if _, err := service.SetAccountState(adminContext, &cloudv1.SetAccountStateRequest{AccountId: "admin", State: cloudv1.AccountState_ACCOUNT_STATE_DISABLED, ExpectedRevision: 1, Reason: "self disable"}); !errors.Is(err, account.ErrAccountConflict) {
		t.Fatalf("self disable error = %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("store mutation calls = %d, want 0", store.calls)
	}
}

func TestUnknownAccountMutationEnumsFailClosed(t *testing.T) {
	now := time.Unix(6_000, 0).UTC()
	store := &permissionStoreFake{}
	service := &Service{config: Config{Store: store, Now: func() time.Time { return now }}}
	ctx := permissionContext(&cloudv1.AccountProfile{AccountId: "admin"}, cloudv1.AccountRole_ACCOUNT_ROLE_ADMIN, now.Add(time.Minute))
	if _, err := service.SetAccountState(ctx, &cloudv1.SetAccountStateRequest{AccountId: "target", State: cloudv1.AccountState(99), ExpectedRevision: 1, Reason: "unknown"}); !errors.Is(err, account.ErrInvalidArgument) {
		t.Fatalf("unknown state error = %v", err)
	}
	if _, err := service.SetAccountRole(ctx, &cloudv1.SetAccountRoleRequest{AccountId: "target", Role: cloudv1.AccountRole(99), Enabled: true, Reason: "unknown"}); !errors.Is(err, account.ErrInvalidArgument) {
		t.Fatalf("unknown role error = %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("store mutation calls = %d, want 0", store.calls)
	}
}

func permissionContext(profile *cloudv1.AccountProfile, role cloudv1.AccountRole, recent time.Time) context.Context {
	return account.ContextWithIdentity(context.Background(), account.Identity{Account: profile, Roles: []cloudv1.AccountRole{role}, RefreshID: "refresh", RecentAuthExpiresAt: recent})
}

type permissionStoreFake struct {
	Store
	calls int
}

func (store *permissionStoreFake) SetAccountRole(context.Context, *cloudv1.SetAccountRoleRequest, string, time.Time) ([]cloudv1.AccountRole, error) {
	store.calls++
	return nil, nil
}

func (store *permissionStoreFake) SetAccountState(context.Context, *cloudv1.SetAccountStateRequest, string, time.Time) (*cloudv1.AccountProfile, error) {
	store.calls++
	return nil, nil
}
