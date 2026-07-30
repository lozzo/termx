package postgres

import (
	"testing"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

func TestUnknownPersistedAccountStateAndRoleFailClosed(t *testing.T) {
	if state, err := parseAccountState("future-state"); err == nil || state != cloudv1.AccountState_ACCOUNT_STATE_UNSPECIFIED {
		t.Fatalf("unknown state = %v, err=%v", state, err)
	}
	if role, err := parseAccountRole("future-role"); err == nil || role != cloudv1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED {
		t.Fatalf("unknown role = %v, err=%v", role, err)
	}
	if _, err := accountStateName(cloudv1.AccountState(99)); err == nil {
		t.Fatal("unknown proto state was mapped")
	}
	if _, err := accountRoleName(cloudv1.AccountRole(99)); err == nil {
		t.Fatal("unknown proto role was mapped")
	}
}
