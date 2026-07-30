// Package bindingkeys persists and atomically publishes the Edge binding verification key bundle.
package bindingkeys

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/filepublish"
	"google.golang.org/protobuf/proto"
)

const maxBundleBytes = 1 << 20

var ErrUnavailable = errors.New("binding verification keys are unavailable")

// ErrDurabilityUncertain means rename made a new bundle visible but directory durability could not be established.
// The store stays fail-closed until a restart reloads the visible file.
var ErrDurabilityUncertain = errors.New("binding key bundle durability is uncertain; restart required")

type snapshot struct {
	bundle              *cloudv1.KeyBundle
	keys                ticket.KeySet
	durabilityUncertain bool
}

// Store serializes receive rules and publishes a complete immutable snapshot only after durable file publication.
type Store struct {
	path string
	mu   sync.RWMutex
	data atomic.Pointer[snapshot]

	rename        func(string, string) error
	syncDirectory func(string) error
	syncFile      func(*os.File) error
	writeFile     func(*os.File, []byte) (int, error)
}

// Open loads one explicitly configured protobuf cache. A missing cache is a valid unavailable state.
func Open(path string) (*Store, error) {
	path = filepath.Clean(path)
	if path == "." || !filepath.IsAbs(path) {
		return nil, errors.New("binding key bundle cache requires an absolute file path")
	}
	store := &Store{
		path: path, rename: filepublish.Rename, syncDirectory: filepublish.SyncDirectory,
		syncFile:  func(file *os.File) error { return file.Sync() },
		writeFile: func(file *os.File, payload []byte) (int, error) { return file.Write(payload) },
	}
	bundle, err := readBundle(path)
	if err != nil {
		return nil, err
	}
	if bundle != nil {
		canonical, keys, err := ticket.CanonicalKeyBundle(bundle)
		if err != nil {
			return nil, fmt.Errorf("validate binding key bundle cache: %w", err)
		}
		store.data.Store(&snapshot{bundle: canonical, keys: keys})
	}
	return store, nil
}

// Update applies rollback/replay/key-change rules, durably replaces the cache, then publishes memory.
func (store *Store) Update(bundle *cloudv1.KeyBundle) error {
	canonical, keys, err := ticket.CanonicalKeyBundle(bundle)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	current := store.data.Load()
	if current != nil && current.durabilityUncertain {
		return ErrDurabilityUncertain
	}
	if current != nil {
		switch {
		case canonical.GetRevision() < current.bundle.GetRevision():
			return errors.New("binding key bundle revision rollback")
		case canonical.GetRevision() == current.bundle.GetRevision() && !ticket.SameKeySet(canonical, current.bundle):
			return errors.New("binding key bundle changed keys without increasing revision")
		case canonical.GetRevision() == current.bundle.GetRevision() && canonical.GetExpiresAt().AsTime().Before(current.bundle.GetExpiresAt().AsTime()):
			return errors.New("binding key bundle expiry rollback")
		}
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(canonical)
	if err != nil {
		return err
	}
	if err := store.publish(payload); err != nil {
		return err
	}
	store.data.Store(&snapshot{bundle: canonical, keys: keys})
	return nil
}

// VerificationKeys returns one complete keyset snapshot only while its bundle is effective.
func (store *Store) VerificationKeys(now time.Time) (ticket.KeySet, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	current := store.data.Load()
	if current != nil && current.durabilityUncertain {
		return nil, ErrDurabilityUncertain
	}
	if current == nil || !ticket.KeyBundleUsableAt(current.bundle, now) {
		return nil, ErrUnavailable
	}
	result := make(ticket.KeySet, len(current.keys))
	for id, publicKey := range current.keys {
		result[id] = append([]byte(nil), publicKey...)
	}
	return result, nil
}

// Usable reports whether AgentGateway admission may use the current snapshot at now.
func (store *Store) Usable(now time.Time) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()
	current := store.data.Load()
	return current != nil && !current.durabilityUncertain && ticket.KeyBundleUsableAt(current.bundle, now)
}

// Bundle returns a defensive copy for health expiry scheduling and tests.
func (store *Store) Bundle() *cloudv1.KeyBundle {
	store.mu.RLock()
	defer store.mu.RUnlock()
	current := store.data.Load()
	if current == nil || current.durabilityUncertain {
		return nil
	}
	return proto.Clone(current.bundle).(*cloudv1.KeyBundle)
}

func (store *Store) publish(payload []byte) error {
	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := validateExistingTarget(store.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(store.path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	written, err := store.writeFile(temporary, payload)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if written != len(payload) {
		_ = temporary.Close()
		return io.ErrShortWrite
	}
	if err := store.syncFile(temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := store.rename(temporaryPath, store.path); err != nil {
		return err
	}
	if err := store.syncDirectory(directory); err != nil {
		// Update holds the write lock across rename and this transition, so no
		// admission can observe the old snapshot after an uncertain commit.
		store.data.Store(&snapshot{durabilityUncertain: true})
		return fmt.Errorf("%w: %v", ErrDurabilityUncertain, err)
	}
	return nil
}

func readBundle(path string) (*cloudv1.KeyBundle, error) {
	return readBundleWith(path, openBundleFileNoFollow)
}

func readBundleWith(path string, openFile func(string) (*os.File, error)) (*cloudv1.KeyBundle, error) {
	file, err := openFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	if err := validateOpenedBundleFile(file); err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(io.LimitReader(file, maxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > maxBundleBytes {
		return nil, errors.New("binding key bundle cache size is invalid")
	}
	bundle := &cloudv1.KeyBundle{}
	if err := proto.Unmarshal(payload, bundle); err != nil {
		return nil, errors.New("binding key bundle cache is corrupt")
	}
	return bundle, nil
}

func validateExistingTarget(path string) error {
	file, err := openBundleFileNoFollow(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return validateOpenedBundleFile(file)
}
