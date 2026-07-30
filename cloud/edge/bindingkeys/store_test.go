package bindingkeys

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestOpenMissingAndExpiredCacheIsUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "binding.pb")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if store.Usable(now) {
		t.Fatal("missing cache is usable")
	}
	expired := bundle(3, now.Add(-2*time.Hour), time.Hour, "key-a", make([]byte, ed25519.PublicKeySize))
	writeBundle(t, path, expired, 0o600)
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Usable(now) {
		t.Fatal("expired cache is usable")
	}
	if got := reopened.Bundle().GetRevision(); got != 3 {
		t.Fatalf("expired cache revision=%d want=3", got)
	}
}

func TestOpenRejectsUnsafeOrCorruptCache(t *testing.T) {
	now := time.Now().UTC()
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(bundle(1, now, time.Hour, "key-a", make([]byte, ed25519.PublicKeySize)))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*testing.T, string){
		"directory": func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, path string) {
			target := path + ".target"
			writeBytes(t, target, payload, 0o600)
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		},
		"corrupt":   func(t *testing.T, path string) { writeBytes(t, path, []byte("not-protobuf"), 0o600) },
		"truncated": func(t *testing.T, path string) { writeBytes(t, path, payload[:len(payload)/2], 0o600) },
	}
	if runtime.GOOS != "windows" {
		tests["group readable"] = func(t *testing.T, path string) { writeBytes(t, path, payload, 0o640) }
		tests["other readable"] = func(t *testing.T, path string) { writeBytes(t, path, payload, 0o604) }
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "binding.pb")
			setup(t, path)
			if _, err := Open(path); err == nil {
				t.Fatal("unsafe or corrupt cache was accepted")
			}
		})
	}
}

func TestReadBundleValidatesAndReadsTheOpenedDescriptor(t *testing.T) {
	now := time.Now().UTC()
	directory := t.TempDir()
	path := filepath.Join(directory, "binding.pb")
	originalPath := filepath.Join(directory, "binding.original.pb")
	replacementPath := filepath.Join(directory, "binding.replacement.pb")
	writeBundle(t, path, bundle(1, now, time.Hour, "key-a", make([]byte, ed25519.PublicKeySize)), 0o600)
	writeBundle(t, replacementPath, bundle(2, now, time.Hour, "key-b", append(make([]byte, ed25519.PublicKeySize-1), 1)), 0o600)

	loaded, err := readBundleWith(path, func(openPath string) (*os.File, error) {
		file, openErr := openBundleFileNoFollow(openPath)
		if openErr != nil {
			return nil, openErr
		}
		if renameErr := os.Rename(path, originalPath); renameErr != nil {
			_ = file.Close()
			return nil, renameErr
		}
		if symlinkErr := os.Symlink(replacementPath, path); symlinkErr != nil {
			_ = file.Close()
			return nil, symlinkErr
		}
		return file, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GetRevision() != 1 {
		t.Fatalf("loaded replacement revision=%d want opened revision=1", loaded.GetRevision())
	}
	if _, err := Open(path); err == nil {
		t.Fatal("replacement symlink was accepted on a new open")
	}
}

func TestUpdateEnforcesRevisionReplayAndKeyChangeRules(t *testing.T) {
	now := time.Now().UTC()
	store, err := Open(filepath.Join(t.TempDir(), "binding.pb"))
	if err != nil {
		t.Fatal(err)
	}
	keyA := make([]byte, ed25519.PublicKeySize)
	keyB := append(make([]byte, ed25519.PublicKeySize-1), 1)
	first := bundle(5, now, time.Hour, "key-a", keyA)
	if err := store.Update(first); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("published cache info=%v err=%v", info, err)
	}
	if err := store.Update(proto.Clone(first).(*cloudv1.KeyBundle)); err != nil {
		t.Fatalf("identical replay: %v", err)
	}
	extended := proto.Clone(first).(*cloudv1.KeyBundle)
	extended.ExpiresAt = timestamppb.New(now.Add(2 * time.Hour))
	if err := store.Update(extended); err != nil {
		t.Fatalf("same revision expiry extension: %v", err)
	}
	rollback := proto.Clone(first).(*cloudv1.KeyBundle)
	rollback.Revision = 4
	if err := store.Update(rollback); err == nil {
		t.Fatal("revision rollback was accepted")
	}
	expiryRollback := proto.Clone(first).(*cloudv1.KeyBundle)
	if err := store.Update(expiryRollback); err == nil {
		t.Fatal("same revision expiry rollback was accepted")
	}
	changed := bundle(5, now, 2*time.Hour, "key-b", keyB)
	if err := store.Update(changed); err == nil {
		t.Fatal("key change without revision increment was accepted")
	}
	changed.Revision = 6
	if err := store.Update(changed); err != nil {
		t.Fatalf("revisioned key change: %v", err)
	}
}

func TestPreRenamePublishFailuresKeepOldSnapshotAndPermitRetry(t *testing.T) {
	now := time.Now().UTC()
	first := bundle(1, now, time.Hour, "key-a", make([]byte, ed25519.PublicKeySize))
	second := bundle(2, now, time.Hour, "key-b", append(make([]byte, ed25519.PublicKeySize-1), 1))
	tests := map[string]func(*Store) func(){
		"write": func(store *Store) func() {
			original := store.writeFile
			store.writeFile = func(*os.File, []byte) (int, error) { return 0, errors.New("injected write failure") }
			return func() { store.writeFile = original }
		},
		"short write": func(store *Store) func() {
			original := store.writeFile
			store.writeFile = func(_ *os.File, payload []byte) (int, error) { return len(payload) - 1, nil }
			return func() { store.writeFile = original }
		},
		"file fsync": func(store *Store) func() {
			original := store.syncFile
			store.syncFile = func(*os.File) error { return errors.New("injected file sync failure") }
			return func() { store.syncFile = original }
		},
		"rename": func(store *Store) func() {
			original := store.rename
			store.rename = func(string, string) error { return errors.New("injected rename failure") }
			return func() { store.rename = original }
		},
	}
	for name, inject := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "binding.pb")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Update(first); err != nil {
				t.Fatal(err)
			}
			restore := inject(store)
			err = store.Update(second)
			restore()
			if err == nil || store.Bundle().GetRevision() != 1 || readRevision(t, path) != 1 {
				t.Fatalf("failure err=%v memory=%d disk=%d", err, store.Bundle().GetRevision(), readRevision(t, path))
			}
			if err := store.Update(second); err != nil {
				t.Fatalf("retry after failure: %v", err)
			}
			reopened, err := Open(path)
			if err != nil || reopened.Bundle().GetRevision() != 2 {
				t.Fatalf("restart after retry revision=%v err=%v", reopened.Bundle(), err)
			}
		})
	}
}

func TestDirectorySyncFailureIsFatalUntilRestart(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "binding.pb")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := bundle(1, now, time.Hour, "key-a", make([]byte, ed25519.PublicKeySize))
	second := bundle(2, now, time.Hour, "key-b", append(make([]byte, ed25519.PublicKeySize-1), 1))
	third := bundle(3, now, time.Hour, "key-c", append(make([]byte, ed25519.PublicKeySize-1), 2))
	if err := store.Update(first); err != nil {
		t.Fatal(err)
	}
	store.syncDirectory = func(string) error { return errors.New("injected directory sync failure") }
	if err := store.Update(second); !errors.Is(err, ErrDurabilityUncertain) {
		t.Fatalf("directory sync failure err=%v", err)
	}
	if store.Bundle() != nil || store.Usable(now) {
		t.Fatal("durability-uncertain store retained a published or usable snapshot")
	}
	if _, err := store.VerificationKeys(now); !errors.Is(err, ErrDurabilityUncertain) {
		t.Fatalf("admission error=%v want durability uncertain", err)
	}
	if err := store.Update(third); !errors.Is(err, ErrDurabilityUncertain) {
		t.Fatalf("subsequent update error=%v want durability uncertain", err)
	}
	if readRevision(t, path) != 2 {
		t.Fatal("fatal store overwrote the visible revision 2")
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Bundle().GetRevision() != 2 || !reopened.Usable(now) {
		t.Fatalf("restart did not recover visible revision 2: %v", reopened.Bundle())
	}
	if err := reopened.Update(third); err != nil {
		t.Fatalf("update after restart: %v", err)
	}
}

func TestConcurrentUpdateAndAdmissionObserveCompleteKeysets(t *testing.T) {
	now := time.Now().UTC()
	publicA, privateA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicB, privateB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "binding.pb"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(bundle(1, now.Add(-time.Minute), time.Hour, "key-a", publicA)); err != nil {
		t.Fatal(err)
	}
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: uuid.NewString(), DaemonId: uuid.NewString(), AccountId: uuid.NewString(), EdgeId: "edge-concurrent", DeviceId: "device-concurrent",
		DevicePublicKey: make([]byte, ed25519.PublicKeySize), Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING},
		IssuedAt: timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(now.Add(time.Hour)), Revision: 1, EdgeLocatorSha256: make([]byte, sha256.Size),
	}
	signedA, err := ticket.SignDaemonBinding("key-a", privateA, claims)
	if err != nil {
		t.Fatal(err)
	}
	signedB, err := ticket.SignDaemonBinding("key-b", privateB, claims)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsOut := make(chan error, 8)
	var readers sync.WaitGroup
	for range 8 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range 500 {
				keys, err := store.VerificationKeys(now)
				if err != nil {
					errorsOut <- err
					return
				}
				_, errA := ticket.VerifyDaemonBinding(signedA, keys, "edge-concurrent", now, 0)
				_, errB := ticket.VerifyDaemonBinding(signedB, keys, "edge-concurrent", now, 0)
				if (errA == nil) == (errB == nil) {
					errorsOut <- errors.New("admission observed a partial or mixed keyset")
					return
				}
			}
		}()
	}
	close(start)
	for revision := uint64(2); revision <= 16; revision++ {
		id, key := "key-a", publicA
		if revision%2 == 0 {
			id, key = "key-b", publicB
		}
		if err := store.Update(bundle(revision, now.Add(-time.Minute), time.Hour, id, key)); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
}

func bundle(revision uint64, issued time.Time, ttl time.Duration, keyID string, publicKey []byte) *cloudv1.KeyBundle {
	return &cloudv1.KeyBundle{
		Revision: revision, IssuedAt: timestamppb.New(issued), ExpiresAt: timestamppb.New(issued.Add(ttl)),
		Keys: []*cloudv1.VerificationKey{{KeyId: keyID, Algorithm: "Ed25519", PublicKey: append([]byte(nil), publicKey...)}},
	}
}

func writeBundle(t *testing.T, path string, bundle *cloudv1.KeyBundle, mode os.FileMode) {
	t.Helper()
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, path, payload, mode)
}

func writeBytes(t *testing.T, path string, payload []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, payload, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readRevision(t *testing.T, path string) uint64 {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return store.Bundle().GetRevision()
}
