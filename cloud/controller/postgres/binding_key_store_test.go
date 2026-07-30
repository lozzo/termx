package postgres

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/bindingkeys"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

func TestBindingKeySetPersistsMetadataAndRejectsHistoricalReplay(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := openMigratedBindingKeyDatabase(t, ctx, databaseURL)
	truncateBindingKeyTables(t, ctx, database)

	now := time.Date(2026, 7, 30, 12, 0, 0, 123456000, time.UTC)
	firstDigest := bytes.Repeat([]byte{0x71}, 32)
	secondDigest := bytes.Repeat([]byte{0x72}, 32)
	first, err := database.ReconcileBindingKeySet(ctx, firstDigest, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	database = openMigratedBindingKeyDatabase(t, ctx, databaseURL)
	defer database.Close()
	restarted, err := database.ReconcileBindingKeySet(ctx, firstDigest, now.Add(time.Minute), time.Hour)
	if err != nil || restarted.Revision != first.Revision || !restarted.IssuedAt.Equal(first.IssuedAt) || !restarted.ExpiresAt.Equal(first.ExpiresAt) {
		t.Fatalf("restart metadata=%+v first=%+v err=%v", restarted, first, err)
	}
	rotated, err := database.ReconcileBindingKeySet(ctx, secondDigest, now.Add(2*time.Minute), time.Hour)
	if err != nil || rotated.Revision != first.Revision+1 {
		t.Fatalf("rotated metadata=%+v want revision=%d err=%v", rotated, first.Revision+1, err)
	}
	if _, err := database.ReconcileBindingKeySet(ctx, firstDigest, now.Add(3*time.Minute), time.Hour); !errors.Is(err, bindingkeys.ErrKeySetReplay) {
		t.Fatalf("A->B->A error=%v want replay", err)
	}
	var historyCount int
	if err := database.pool.QueryRow(ctx, `SELECT count(*) FROM binding_keyset_history WHERE purpose=$1`, bindingKeySetPurpose).Scan(&historyCount); err != nil || historyCount != 2 {
		t.Fatalf("history count=%d err=%v", historyCount, err)
	}
}

func TestBindingKeySetConcurrentControllersShareDatabaseMetadata(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := openMigratedBindingKeyDatabase(t, ctx, databaseURL)
	defer database.Close()
	truncateBindingKeyTables(t, ctx, database)

	now := time.Date(2026, 7, 30, 12, 0, 0, 654321000, time.UTC)
	digest := bytes.Repeat([]byte{0x81}, 32)
	results := make([]bindingkeys.Metadata, 20)
	errorsOut := make([]error, len(results))
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index], errorsOut[index] = database.ReconcileBindingKeySet(ctx, digest, now.Add(time.Duration(index)*time.Millisecond), time.Hour)
		}()
	}
	group.Wait()
	for index := range results {
		if errorsOut[index] != nil {
			t.Fatalf("controller %d: %v", index, errorsOut[index])
		}
		if results[index].Revision != results[0].Revision || !results[index].IssuedAt.Equal(results[0].IssuedAt) || !results[index].ExpiresAt.Equal(results[0].ExpiresAt) {
			t.Fatalf("controller %d metadata=%+v first=%+v", index, results[index], results[0])
		}
	}

	refreshedAt := results[0].IssuedAt.Add(30 * time.Minute)
	refreshed, err := database.ReconcileBindingKeySet(ctx, digest, refreshedAt, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	sameRevision, err := database.ReconcileBindingKeySet(ctx, digest, refreshedAt.Add(time.Second), time.Hour)
	if err != nil || sameRevision.Revision != refreshed.Revision || !sameRevision.IssuedAt.Equal(refreshed.IssuedAt) || !sameRevision.ExpiresAt.Equal(refreshed.ExpiresAt) {
		t.Fatalf("same revision expiry metadata differs: refreshed=%+v next=%+v err=%v", refreshed, sameRevision, err)
	}
	shortTTLRefresh, err := database.ReconcileBindingKeySet(ctx, digest, refreshed.IssuedAt.Add(30*time.Minute), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if shortTTLRefresh.Revision != refreshed.Revision || shortTTLRefresh.ExpiresAt.Before(refreshed.ExpiresAt) {
		t.Fatalf("shorter TTL moved same revision expiry backward: refreshed=%+v short=%+v", refreshed, shortTTLRefresh)
	}
}

func TestBindingKeyOwnersRejectStaleInstanceBeforePublish(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := openMigratedBindingKeyDatabase(t, ctx, databaseURL)
	defer database.Close()
	truncateBindingKeyTables(t, ctx, database)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	keyA := &cloudv1.VerificationKey{KeyId: "key-a", Algorithm: "Ed25519", PublicKey: make([]byte, ed25519.PublicKeySize)}
	first, err := bindingkeys.New(ctx, bindingkeys.Config{Store: database, Keys: []*cloudv1.VerificationKey{keyA}, TTL: time.Hour, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	second, err := bindingkeys.New(ctx, bindingkeys.Config{Store: database, Keys: []*cloudv1.VerificationKey{keyA}, TTL: time.Hour, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	firstBundle, err := first.Bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondBundle, err := second.Bundle(ctx)
	if err != nil || !proto.Equal(firstBundle, secondBundle) {
		t.Fatalf("same digest owners differ: first=%v second=%v err=%v", firstBundle, secondBundle, err)
	}
	keyB := &cloudv1.VerificationKey{KeyId: "key-b", Algorithm: "Ed25519", PublicKey: append(make([]byte, ed25519.PublicKeySize-1), 1)}
	rotated, err := bindingkeys.New(ctx, bindingkeys.Config{Store: database, Keys: []*cloudv1.VerificationKey{keyB}, TTL: time.Hour, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	rotatedBundle, err := rotated.Bundle(ctx)
	if err != nil || rotatedBundle.GetRevision() != firstBundle.GetRevision()+1 {
		t.Fatalf("rotated bundle=%v err=%v", rotatedBundle, err)
	}
	if _, err := first.Bundle(ctx); !errors.Is(err, bindingkeys.ErrKeySetReplay) {
		t.Fatalf("stale owner publish error=%v want replay", err)
	}
}

func TestBindingKeySetConcurrentRotationsRejectEarlierPublisher(t *testing.T) {
	databaseURL := os.Getenv("ANYTTY_CLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("ANYTTY_CLOUD_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := openMigratedBindingKeyDatabase(t, ctx, databaseURL)
	defer database.Close()
	truncateBindingKeyTables(t, ctx, database)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	initialDigest := bytes.Repeat([]byte{0x91}, 32)
	if _, err := database.ReconcileBindingKeySet(ctx, initialDigest, now, time.Hour); err != nil {
		t.Fatal(err)
	}
	digests := [][]byte{bytes.Repeat([]byte{0x92}, 32), bytes.Repeat([]byte{0x93}, 32)}
	results := make([]bindingkeys.Metadata, len(digests))
	errorsOut := make([]error, len(digests))
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range digests {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results[index], errorsOut[index] = database.ReconcileBindingKeySet(ctx, digests[index], now.Add(time.Minute), time.Hour)
		}()
	}
	close(start)
	group.Wait()
	for index, err := range errorsOut {
		if err != nil {
			t.Fatalf("rotation %d: %v", index, err)
		}
	}
	if results[0].Revision == results[1].Revision || (results[0].Revision != 2 && results[0].Revision != 3) || (results[1].Revision != 2 && results[1].Revision != 3) {
		t.Fatalf("concurrent revisions=%d,%d want 2,3", results[0].Revision, results[1].Revision)
	}
	earlier := 0
	if results[1].Revision < results[0].Revision {
		earlier = 1
	}
	if _, err := database.ReconcileBindingKeySet(ctx, digests[earlier], now.Add(2*time.Minute), time.Hour); !errors.Is(err, bindingkeys.ErrKeySetReplay) {
		t.Fatalf("earlier concurrent publisher error=%v want replay", err)
	}
}

func openMigratedBindingKeyDatabase(t *testing.T, ctx context.Context, databaseURL string) *Database {
	t.Helper()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		t.Fatal(err)
	}
	return database
}

func truncateBindingKeyTables(t *testing.T, ctx context.Context, database *Database) {
	t.Helper()
	if _, err := database.pool.Exec(ctx, `TRUNCATE binding_keysets, binding_keyset_history`); err != nil {
		t.Fatal(err)
	}
}
