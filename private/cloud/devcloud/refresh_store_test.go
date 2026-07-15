package devcloud

import (
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/session"
)

func TestRefreshStoreRejectsReplayExpiryAndDeviceRevocation(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	store, err := openRefreshStore(filepath.Join(t.TempDir(), "refresh.json"), rand.Reader, now)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.Issue(refreshRecord{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Rotate(first, session.KindDevice, now.Add(refreshSessionTTL+time.Second)); err == nil {
		t.Fatal("expired refresh secret was accepted")
	}
	second, _, err := store.Issue(refreshRecord{Kind: session.KindDevice, AccountID: "account-1", DeviceID: "daemon-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeDevice("daemon-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Rotate(second, session.KindDevice, now); err == nil {
		t.Fatal("revoked device refresh secret was accepted")
	}
	client, _, err := store.Issue(refreshRecord{Kind: session.KindAccount, AccountID: "account-1", DeviceID: "client-1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Rotate(client, session.KindAccount, now); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.Rotate(client, session.KindAccount, now); err == nil {
		t.Fatal("replayed refresh secret was accepted")
	}
}
