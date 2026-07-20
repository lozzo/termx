package webcontroller_test

import (
	"path/filepath"
	"testing"
	"time"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

func TestUserCenterScopesNodeMutationToOwningAccount(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	store := webcontroller.NewUserCenterStore(func() time.Time { return now })
	t.Cleanup(func() { _ = store.Close() })
	_, nodes, _, _, err := store.Snapshot("account-dev-local")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("Snapshot() = %d nodes, %v", len(nodes), err)
	}
	if _, err := store.RevokeNode("another-account", nodes[0].ID); err == nil {
		t.Fatal("RevokeNode() allowed a foreign account")
	}
	if _, err := store.RevokeNode("account-dev-local", nodes[0].ID); err != nil {
		t.Fatalf("RevokeNode() = %v", err)
	}
}

func TestUserCenterDoesNotPersistDaemonOnlineTruth(t *testing.T) {
	store := webcontroller.NewUserCenterStore(time.Now)
	defer store.Close()
	if err := store.UpsertCloudDevice("account-dev-local", "daemon-online", "Build daemon", "daemon"); err != nil {
		t.Fatal(err)
	}
	_, nodes, _, _, err := store.Snapshot("account-dev-local")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.ID == "daemon-online" {
			if node.Online {
				t.Fatal("UserCenter persisted daemon online truth")
			}
			return
		}
	}
	t.Fatal("upserted daemon was not found")
}

func TestReferralRewardsArePaymentBoundIdempotentAndPersistent(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "accounts.db")
	store, err := webcontroller.OpenUserCenterStore(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	_, _, referrerProgram, _, _ := store.Snapshot("account-dev-local")
	referred, err := store.RegisterPasswordAccount("new@example.com", "a-secure-password", referrerProgram.Code)
	if err != nil {
		t.Fatalf("RegisterPasswordAccount() = %v", err)
	}
	if _, err := store.ApplyReferralPayment("order-1", referred.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyReferralPayment("order-1", referred.AccountID); err != nil {
		t.Fatal(err)
	}
	_, _, inviter, _, _ := store.Snapshot("account-dev-local")
	_, _, invitee, _, _ := store.Snapshot(referred.AccountID)
	if inviter.RewardDays != 15 || len(inviter.Rewards) != 1 {
		t.Fatalf("inviter rewards = %#v", inviter)
	}
	if invitee.RewardDays != 7 || len(invitee.Rewards) != 1 {
		t.Fatalf("invitee rewards = %#v", invitee)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := webcontroller.OpenUserCenterStore(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	_, _, persisted, _, err := reopened.Snapshot("account-dev-local")
	if err != nil || persisted.RewardDays != 15 {
		t.Fatalf("reopened rewards = %#v, %v", persisted, err)
	}
}

func TestPasswordIdentityCanLoginAndChangePassword(t *testing.T) {
	store := webcontroller.NewUserCenterStore(time.Now)
	defer store.Close()
	profile, err := store.RegisterPasswordAccount("owner@example.com", "first-password", "")
	if err != nil || !profile.PasswordConfigured {
		t.Fatalf("register = %#v, %v", profile, err)
	}
	if _, err := store.AuthenticatePassword(profile.Email, "first-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangePassword(profile.AccountID, "wrong", "second-password"); err == nil {
		t.Fatal("wrong current password accepted")
	}
	if err := store.ChangePassword(profile.AccountID, "first-password", "second-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticatePassword(profile.Email, "first-password"); err == nil {
		t.Fatal("old password still accepted")
	}
	if _, err := store.AuthenticatePassword(profile.Email, "second-password"); err != nil {
		t.Fatal(err)
	}
}
