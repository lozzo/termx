package postgres

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func TestReconcileBindingKeySetPersistsStableMonotonicRevision(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	firstDigest := bytes.Repeat([]byte{0x71}, 32)
	secondDigest := bytes.Repeat([]byte{0x72}, 32)
	first, err := database.ReconcileBindingKeySet(ctx, firstDigest, now)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := database.ReconcileBindingKeySet(ctx, firstDigest, now.Add(time.Minute))
	if err != nil || restarted != first {
		t.Fatalf("same digest revision=%d first=%d err=%v", restarted, first, err)
	}
	rotated, err := database.ReconcileBindingKeySet(ctx, secondDigest, now.Add(2*time.Minute))
	if err != nil || rotated != first+1 {
		t.Fatalf("rotated revision=%d want=%d err=%v", rotated, first+1, err)
	}
}
