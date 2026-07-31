package certificate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFileSecretStorePutSyncFailureUsesDurableDeleteOrder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	realRename, realRemove := store.rename, store.remove
	operations := make([]string, 0, 8)
	store.rename = func(from, to string) error {
		operations = append(operations, "rename:"+secretPathKind(from)+"->"+secretPathKind(to))
		return realRename(from, to)
	}
	store.remove = func(path string) error {
		operations = append(operations, "remove:"+secretPathKind(path))
		return realRemove(path)
	}
	wantErr := errors.New("publish root sync failed")
	syncCalls := 0
	store.syncDirectory = func(path string) error {
		if path != root {
			t.Fatalf("sync path = %q, want root", path)
		}
		syncCalls++
		operations = append(operations, "sync:root")
		if syncCalls == 1 {
			return wantErr
		}
		return nil
	}

	if _, err := store.Put([]byte("certificate"), []byte("private key")); !errors.Is(err, wantErr) {
		t.Fatalf("Put error = %v, want %v", err, wantErr)
	}
	want := []string{
		"rename:pending->live", "sync:root", "rename:live->tombstone", "sync:root",
		"remove:certificate", "remove:key", "remove:tombstone", "sync:root",
	}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %v, want %v", operations, want)
	}
}

func TestFileSecretStorePutJoinsPublishAndCleanupSyncFailures(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	publishErr := errors.New("publish sync failed")
	cleanupErr := errors.New("tombstone sync failed")
	syncCalls := 0
	store.syncDirectory = func(string) error {
		syncCalls++
		if syncCalls == 1 {
			return publishErr
		}
		return cleanupErr
	}
	if _, err := store.Put([]byte("certificate"), []byte("private key")); !errors.Is(err, publishErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Put error = %v, want joined publish and cleanup sync failures", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), deletePrefix) {
		t.Fatalf("failed durable cleanup state = %v, want one tombstone", entries)
	}
	store.syncDirectory = func(string) error { return nil }
	reference := strings.TrimPrefix(entries[0].Name(), deletePrefix)
	if err := store.Delete(reference); err != nil {
		t.Fatalf("retry tombstone cleanup: %v", err)
	}
}

func TestFileSecretStoreDeleteRetriesInterruptedTombstone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := store.Put([]byte("certificate"), []byte("private key"))
	if err != nil {
		t.Fatal(err)
	}
	realRemove := store.remove
	wantErr := errors.New("private key removal interrupted")
	failed := false
	store.remove = func(path string) error {
		if !failed && filepath.Base(path) == privateKeyFile {
			failed = true
			return wantErr
		}
		return realRemove(path)
	}
	if err := store.Delete(reference); !errors.Is(err, wantErr) {
		t.Fatalf("Delete error = %v, want %v", err, wantErr)
	}
	if _, err := os.Lstat(filepath.Join(root, reference)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("live directory exists after tombstone rename: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, deletePrefix+reference)); err != nil {
		t.Fatalf("retryable tombstone missing: %v", err)
	}

	store.remove = realRemove
	if err := store.Delete(reference); err != nil {
		t.Fatalf("retry interrupted Delete: %v", err)
	}
	if err := store.Delete(reference); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, deletePrefix+reference)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tombstone remains after retry: %v", err)
	}
}

func TestFileSecretStoreDeleteRetriesFinalRootSyncFromNeitherState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := store.Put([]byte("certificate"), []byte("private key"))
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("final deletion sync failed")
	syncCalls := 0
	store.syncDirectory = func(string) error {
		syncCalls++
		if syncCalls == 2 {
			return wantErr
		}
		return nil
	}
	if err := store.Delete(reference); !errors.Is(err, wantErr) {
		t.Fatalf("Delete error = %v, want final sync failure", err)
	}
	if _, err := os.Lstat(filepath.Join(root, deletePrefix+reference)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed tombstone unexpectedly remains: %v", err)
	}
	if err := store.Delete(reference); err != nil {
		t.Fatalf("retry from neither state: %v", err)
	}
	if syncCalls != 3 {
		t.Fatalf("root sync calls = %d, want tombstone, failed deletion, retry", syncCalls)
	}
}

func TestFileSecretStoreReconcileRetriesFinalRootSyncAfterCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put([]byte("certificate"), []byte("private key"))
	if err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("orphan deletion sync failed")
	syncCalls := 0
	store.syncDirectory = func(string) error {
		syncCalls++
		if syncCalls == 2 {
			return wantErr
		}
		return nil
	}
	if err := store.Reconcile(nil); !errors.Is(err, wantErr) {
		t.Fatalf("Reconcile error = %v, want final sync failure", err)
	}
	if _, err := os.Lstat(filepath.Join(root, orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan unexpectedly remains after failed final sync: %v", err)
	}
	if err := store.Reconcile(nil); err != nil {
		t.Fatalf("retry Reconcile from empty inventory: %v", err)
	}
	if syncCalls != 3 {
		t.Fatalf("root sync calls = %d, want tombstone, failed deletion, retry", syncCalls)
	}
}

func TestFileSecretStoreReconcileRestoresAndCleansManagedState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Put([]byte("active cert"), []byte("active key"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := store.Put([]byte("restored cert"), []byte("restored key"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put([]byte("orphan cert"), []byte("orphan key"))
	if err != nil {
		t.Fatal(err)
	}
	staleTombstone, err := store.Put([]byte("stale cert"), []byte("stale key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, restored), filepath.Join(root, deletePrefix+restored)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, staleTombstone), filepath.Join(root, deletePrefix+staleTombstone)); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(root, pendingPrefix+"interrupted")
	if err := os.Mkdir(pending, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pending, certificateFile), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Reconcile([]string{active, restored}); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{active, restored} {
		if _, _, err := store.Read(reference); err != nil {
			t.Fatalf("read active reference after reconcile: %v", err)
		}
	}
	for _, path := range []string{
		filepath.Join(root, orphan),
		filepath.Join(root, deletePrefix+staleTombstone),
		pending,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("non-active managed path remains: %s: %v", filepath.Base(path), err)
		}
	}
}

func TestFileSecretStoreReconcileSyncsRestoredTruthBeforeCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.Put([]byte("active cert"), []byte("active key"))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put([]byte("orphan cert"), []byte("orphan key"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, active), filepath.Join(root, deletePrefix+active)); err != nil {
		t.Fatal(err)
	}
	realRename, realRemove, realSync := store.rename, store.remove, store.syncDirectory
	operations := make([]string, 0, 8)
	store.rename = func(from, to string) error {
		operations = append(operations, "rename:"+secretPathKind(from)+"->"+secretPathKind(to))
		return realRename(from, to)
	}
	store.remove = func(path string) error {
		operations = append(operations, "remove:"+secretPathKind(path))
		return realRemove(path)
	}
	store.syncDirectory = func(path string) error {
		operations = append(operations, "sync:root")
		return realSync(path)
	}
	if err := store.Reconcile([]string{active}); err != nil {
		t.Fatal(err)
	}
	if len(operations) < 2 || operations[0] != "rename:tombstone->live" || operations[1] != "sync:root" {
		t.Fatalf("restore did not sync before cleanup: %v", operations)
	}
	if _, err := os.Lstat(filepath.Join(root, orphan)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan remains after reconcile: %v", err)
	}
}

func TestFileSecretStoreReconcileFailsClosedBeforeCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := store.Put([]byte("orphan cert"), []byte("orphan key"))
	if err != nil {
		t.Fatal(err)
	}
	missing := uuid.NewString()
	if err := store.Reconcile([]string{missing}); err == nil {
		t.Fatal("missing active secret was accepted")
	}
	if _, _, err := store.Read(orphan); err != nil {
		t.Fatalf("orphan was cleaned before active truth validation completed: %v", err)
	}
}

func TestFileSecretStoreReconcileRejectsIncompleteActiveTombstone(t *testing.T) {
	root := filepath.Join(t.TempDir(), "certificates")
	store, err := NewFileSecretStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reference := uuid.NewString()
	tombstone := filepath.Join(root, deletePrefix+reference)
	if err := os.Mkdir(tombstone, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tombstone, certificateFile), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile([]string{reference}); err == nil {
		t.Fatal("incomplete active tombstone was restored")
	}
	if _, err := os.Lstat(tombstone); err != nil {
		t.Fatalf("incomplete active tombstone was modified: %v", err)
	}
}

func TestFileSecretStoreReconcileRejectsUnsafeEntries(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "certificates")
		store, err := NewFileSecretStore(root)
		if err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		path := filepath.Join(root, uuid.NewString())
		if err := os.Symlink(outside, path); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := store.Reconcile(nil); err == nil {
			t.Fatal("symlink secret entry was accepted")
		}
		if _, err := os.Stat(outside); err != nil {
			t.Fatalf("outside symlink target was changed: %v", err)
		}
	})

	t.Run("unmanaged file", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "certificates")
		store, err := NewFileSecretStore(root)
		if err != nil {
			t.Fatal(err)
		}
		reference, err := store.Put([]byte("certificate"), []byte("private key"))
		if err != nil {
			t.Fatal(err)
		}
		unexpected := filepath.Join(root, reference, "unexpected")
		if err := os.WriteFile(unexpected, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.Delete(reference); err == nil {
			t.Fatal("Delete accepted an unmanaged secret directory")
		}
		if payload, err := os.ReadFile(unexpected); err != nil || string(payload) != "keep" {
			t.Fatalf("unmanaged file was changed: payload=%q err=%v", payload, err)
		}
	})
}

func secretPathKind(path string) string {
	base := filepath.Base(path)
	switch {
	case base == certificateFile:
		return "certificate"
	case base == privateKeyFile:
		return "key"
	case strings.HasPrefix(base, pendingPrefix):
		return "pending"
	case strings.HasPrefix(base, deletePrefix):
		return "tombstone"
	case uuid.Validate(base) == nil:
		return "live"
	default:
		return base
	}
}
