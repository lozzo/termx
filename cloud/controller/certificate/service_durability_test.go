package certificate

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/google/uuid"
)

func TestUploadProfileReadsExistingProfileBeforePut(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	wantErr := errors.New("profile read failed")
	putCalls := 0
	store := &serviceStoreStub{getProfile: func(context.Context, string) (Profile, error) {
		return Profile{}, wantErr
	}}
	secrets := &secretStoreStub{put: func([]byte, []byte) (string, error) {
		putCalls++
		return uuid.NewString(), nil
	}}
	service := newDurabilityTestService(t, store, secrets, now, nil)
	_, err := service.UploadProfile(context.Background(), &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: "profile-1", Name: "edge", ExpectedRevision: 1,
		CertificateChainPem: certificatePEM, PrivateKeyPem: privateKeyPEM,
	}, "operator")
	if !errors.Is(err, wantErr) {
		t.Fatalf("UploadProfile error = %v, want %v", err, wantErr)
	}
	if putCalls != 0 {
		t.Fatalf("Put calls = %d, want zero before profile Get succeeds", putCalls)
	}
}

func TestUploadProfileResolvesAmbiguousCommittedMutationFromDatabaseTruth(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	newRef := uuid.NewString()
	mutationErr := errors.New("commit response lost")
	var committed Profile
	store := &serviceStoreStub{
		getProfile: func(context.Context, string) (Profile, error) {
			return Profile{ID: "profile-1", Revision: 1, SecretRef: uuid.NewString(), CreatedAt: now.Add(-time.Hour)}, nil
		},
		replaceProfile: func(_ context.Context, _ uint64, profile Profile, _ string) (string, []Binding, error) {
			committed = profile
			committed.Bindings = []Binding{{EdgeID: "edge-1", ProfileID: profile.ID, DesiredRevision: profile.Revision}}
			return "", nil, mutationErr
		},
		listProfiles: func(context.Context) ([]Profile, error) {
			return []Profile{committed}, nil
		},
	}
	var reconciled []string
	secrets := &secretStoreStub{
		put: func([]byte, []byte) (string, error) { return newRef, nil },
		reconcile: func(references []string) error {
			reconciled = append([]string(nil), references...)
			return nil
		},
	}
	service := newDurabilityTestService(t, store, secrets, now, nil)
	response, err := service.UploadProfile(context.Background(), &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: "profile-1", Name: "edge", ExpectedRevision: 1,
		CertificateChainPem: certificatePEM, PrivateKeyPem: privateKeyPEM,
	}, "operator")
	if err != nil {
		t.Fatalf("ambiguous committed upload returned an error: %v", err)
	}
	if response.GetProfile().GetRevision() != 2 {
		t.Fatalf("response revision = %d, want 2", response.GetProfile().GetRevision())
	}
	if len(reconciled) != 1 || reconciled[0] != newRef {
		t.Fatalf("reconciled refs = %v, want active new ref", reconciled)
	}
}

func TestUploadProfileMutationErrorReconcilesOnlyWhenDatabaseTruthIsReadable(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	mutationErr := errors.New("mutation failed")
	truthErr := errors.New("truth unavailable")
	newRef := uuid.NewString()
	oldRef := uuid.NewString()

	t.Run("readable truth", func(t *testing.T) {
		var reconciled []string
		store := &serviceStoreStub{
			getProfile: func(context.Context, string) (Profile, error) {
				return Profile{ID: "profile-1", Revision: 1, SecretRef: oldRef, CreatedAt: now.Add(-time.Hour)}, nil
			},
			replaceProfile: func(context.Context, uint64, Profile, string) (string, []Binding, error) {
				return "", nil, mutationErr
			},
			listProfiles: func(context.Context) ([]Profile, error) {
				return []Profile{{ID: "profile-1", Revision: 1, SecretRef: oldRef}}, nil
			},
		}
		secrets := &secretStoreStub{
			put: func([]byte, []byte) (string, error) { return newRef, nil },
			reconcile: func(references []string) error {
				reconciled = append([]string(nil), references...)
				return nil
			},
		}
		service := newDurabilityTestService(t, store, secrets, now, nil)
		_, err := service.UploadProfile(context.Background(), replacementRequest(certificatePEM, privateKeyPEM), "operator")
		if !errors.Is(err, mutationErr) {
			t.Fatalf("UploadProfile error = %v, want mutation error", err)
		}
		if len(reconciled) != 1 || reconciled[0] != oldRef {
			t.Fatalf("reconciled refs = %v, want DB active old ref", reconciled)
		}
	})

	t.Run("unreadable truth preserves files", func(t *testing.T) {
		reconcileCalls, deleteCalls := 0, 0
		store := &serviceStoreStub{
			getProfile: func(context.Context, string) (Profile, error) {
				return Profile{ID: "profile-1", Revision: 1, SecretRef: oldRef, CreatedAt: now.Add(-time.Hour)}, nil
			},
			replaceProfile: func(context.Context, uint64, Profile, string) (string, []Binding, error) {
				return "", nil, mutationErr
			},
			listProfiles: func(context.Context) ([]Profile, error) { return nil, truthErr },
		}
		secrets := &secretStoreStub{
			put:       func([]byte, []byte) (string, error) { return newRef, nil },
			reconcile: func([]string) error { reconcileCalls++; return nil },
			delete:    func(string) error { deleteCalls++; return nil },
		}
		service := newDurabilityTestService(t, store, secrets, now, nil)
		_, err := service.UploadProfile(context.Background(), replacementRequest(certificatePEM, privateKeyPEM), "operator")
		if !errors.Is(err, mutationErr) || !errors.Is(err, truthErr) {
			t.Fatalf("UploadProfile error = %v, want mutation and truth errors", err)
		}
		if reconcileCalls != 0 || deleteCalls != 0 {
			t.Fatalf("unreadable truth triggered cleanup: reconcile=%d delete=%d", reconcileCalls, deleteCalls)
		}
	})
}

func TestUploadProfilePostCommitCleanupFailureReturnsSuccessAndRetries(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	newRef, oldRef := uuid.NewString(), uuid.NewString()
	current := Profile{ID: "profile-1", Revision: 1, SecretRef: oldRef, CreatedAt: now.Add(-time.Hour)}
	var committed Profile
	store := &serviceStoreStub{
		getProfile: func(context.Context, string) (Profile, error) { return current, nil },
		replaceProfile: func(_ context.Context, _ uint64, profile Profile, _ string) (string, []Binding, error) {
			committed = profile
			return oldRef, nil, nil
		},
		listProfiles: func(context.Context) ([]Profile, error) { return []Profile{committed}, nil },
	}
	reconcileCalls := 0
	secrets := &secretStoreStub{
		put: func([]byte, []byte) (string, error) { return newRef, nil },
		reconcile: func([]string) error {
			reconcileCalls++
			if reconcileCalls == 1 {
				return errors.New("sensitive-internal-reference")
			}
			return nil
		},
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	service := newDurabilityTestService(t, store, secrets, now, logger)
	response, err := service.UploadProfile(context.Background(), replacementRequest(certificatePEM, privateKeyPEM), "operator")
	if err != nil || response == nil {
		t.Fatalf("committed upload response=%v error=%v", response, err)
	}
	if strings.Contains(logs.String(), "sensitive-internal-reference") || strings.Contains(logs.String(), newRef) || strings.Contains(logs.String(), oldRef) {
		t.Fatalf("cleanup log leaked internal details: %s", logs.String())
	}
	if err := service.ReconcileSecrets(context.Background()); err != nil {
		t.Fatalf("next reconciliation retry: %v", err)
	}
	if reconcileCalls != 2 {
		t.Fatalf("reconcile calls = %d, want immediate and retry", reconcileCalls)
	}
}

func TestUploadProfileDispatchesOutsideSecretStateLock(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	oldRef, newRef := uuid.NewString(), uuid.NewString()
	current := Profile{ID: "profile-1", Revision: 1, SecretRef: oldRef, CreatedAt: now.Add(-time.Hour)}
	var committed Profile
	store := &serviceStoreStub{
		getProfile: func(context.Context, string) (Profile, error) { return current, nil },
		replaceProfile: func(_ context.Context, _ uint64, profile Profile, _ string) (string, []Binding, error) {
			committed = profile
			return oldRef, []Binding{{EdgeID: "edge-1", ProfileID: profile.ID, DesiredRevision: profile.Revision}}, nil
		},
		listProfiles: func(context.Context) ([]Profile, error) { return []Profile{committed}, nil },
	}
	secrets := &secretStoreStub{
		put:       func([]byte, []byte) (string, error) { return newRef, nil },
		reconcile: func([]string) error { return nil },
	}
	var service *Service
	service, err := New(Config{
		Store: store, Secrets: secrets, Edges: &edgeconfig.Service{},
		Dispatcher: DispatcherFunc(func(ctx context.Context, _ string) error {
			return service.ReconcileSecrets(ctx)
		}),
		Online: func(context.Context, string) (bool, error) { return false, nil },
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.UploadProfile(context.Background(), replacementRequest(certificatePEM, privateKeyPEM), "operator")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher deadlocked re-entering ReconcileSecrets")
	}
}

func TestBundleForEdgeHoldsReadLockThroughSecretRead(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	certificatePEM, privateKeyPEM := testPair(t, now, "edge.example.com")
	oldRef, newRef := uuid.NewString(), uuid.NewString()
	current := Profile{ID: "profile-1", Name: "edge", Revision: 1, SecretRef: oldRef, CreatedAt: now.Add(-time.Hour)}
	var committed Profile
	store := &serviceStoreStub{
		getBinding: func(context.Context, string) (Binding, bool, error) {
			return Binding{EdgeID: "edge-1", ProfileID: current.ID, DesiredRevision: current.Revision}, true, nil
		},
		getProfile: func(context.Context, string) (Profile, error) { return current, nil },
		replaceProfile: func(_ context.Context, _ uint64, profile Profile, _ string) (string, []Binding, error) {
			committed = profile
			return oldRef, nil, nil
		},
		listProfiles: func(context.Context) ([]Profile, error) { return []Profile{committed}, nil },
	}
	readStarted := make(chan struct{})
	releaseRead := make(chan struct{})
	putStarted := make(chan struct{})
	var readOnce, putOnce sync.Once
	secrets := &secretStoreStub{
		read: func(string) ([]byte, []byte, error) {
			readOnce.Do(func() { close(readStarted) })
			<-releaseRead
			return []byte("old certificate"), []byte("old private key"), nil
		},
		put: func([]byte, []byte) (string, error) {
			putOnce.Do(func() { close(putStarted) })
			return newRef, nil
		},
		reconcile: func([]string) error { return nil },
	}
	service := newDurabilityTestService(t, store, secrets, now, nil)
	bundleResult := make(chan *cloudv1.EdgeCertificateBundle, 1)
	bundleErr := make(chan error, 1)
	go func() {
		bundle, err := service.BundleForEdge(context.Background(), "edge-1")
		bundleResult <- bundle
		bundleErr <- err
	}()
	<-readStarted
	uploadDone := make(chan error, 1)
	go func() {
		_, err := service.UploadProfile(context.Background(), replacementRequest(certificatePEM, privateKeyPEM), "operator")
		uploadDone <- err
	}()
	select {
	case <-putStarted:
		t.Fatal("Upload entered Put while Bundle still held the old secret read")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseRead)
	if err := <-bundleErr; err != nil {
		t.Fatalf("BundleForEdge: %v", err)
	}
	bundle := <-bundleResult
	if string(bundle.GetCertificateChainPem()) != "old certificate" {
		t.Fatalf("bundle certificate = %q", bundle.GetCertificateChainPem())
	}
	if err := <-uploadDone; err != nil {
		t.Fatalf("UploadProfile after Bundle: %v", err)
	}
}

func TestReconcileSecretsFailsClosedWhenStartupActiveSecretIsMissing(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	store := &serviceStoreStub{listProfiles: func(context.Context) ([]Profile, error) {
		return []Profile{{ID: "profile-1", SecretRef: uuid.NewString()}}, nil
	}}
	fileStore, err := NewFileSecretStore(filepath.Join(physicalTempDir(t), "certificates"))
	if err != nil {
		t.Fatal(err)
	}
	service := newDurabilityTestService(t, store, fileStore, now, nil)
	if err := service.ReconcileSecrets(context.Background()); err == nil {
		t.Fatal("startup reconciliation accepted a missing active secret")
	}
}

func replacementRequest(certificatePEM, privateKeyPEM []byte) *cloudv1.UploadCertificateProfileRequest {
	return &cloudv1.UploadCertificateProfileRequest{
		CertificateProfileId: "profile-1", Name: "edge", ExpectedRevision: 1,
		CertificateChainPem: certificatePEM, PrivateKeyPem: privateKeyPEM,
	}
}

func newDurabilityTestService(t *testing.T, store Store, secrets SecretStore, now time.Time, logger *slog.Logger) *Service {
	t.Helper()
	service, err := New(Config{
		Store: store, Secrets: secrets, Edges: &edgeconfig.Service{},
		Dispatcher: DispatcherFunc(func(context.Context, string) error { return nil }),
		Online:     func(context.Context, string) (bool, error) { return false, nil },
		Now:        func() time.Time { return now }, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

type secretStoreStub struct {
	put       func([]byte, []byte) (string, error)
	read      func(string) ([]byte, []byte, error)
	delete    func(string) error
	reconcile func([]string) error
}

func (store *secretStoreStub) Put(certificatePEM, privateKeyPEM []byte) (string, error) {
	if store.put == nil {
		return "", errors.New("unexpected secret Put")
	}
	return store.put(certificatePEM, privateKeyPEM)
}

func (store *secretStoreStub) Read(reference string) ([]byte, []byte, error) {
	if store.read == nil {
		return nil, nil, errors.New("unexpected secret Read")
	}
	return store.read(reference)
}

func (store *secretStoreStub) Delete(reference string) error {
	if store.delete == nil {
		return errors.New("unexpected secret Delete")
	}
	return store.delete(reference)
}

func (store *secretStoreStub) Reconcile(references []string) error {
	if store.reconcile == nil {
		return errors.New("unexpected secret Reconcile")
	}
	return store.reconcile(references)
}

type serviceStoreStub struct {
	listProfiles   func(context.Context) ([]Profile, error)
	getProfile     func(context.Context, string) (Profile, error)
	createProfile  func(context.Context, Profile, string) error
	replaceProfile func(context.Context, uint64, Profile, string) (string, []Binding, error)
	getBinding     func(context.Context, string) (Binding, bool, error)
}

func (store *serviceStoreStub) ListCertificateProfiles(ctx context.Context) ([]Profile, error) {
	if store.listProfiles == nil {
		return nil, errors.New("unexpected profile List")
	}
	return store.listProfiles(ctx)
}

func (store *serviceStoreStub) GetCertificateProfile(ctx context.Context, id string) (Profile, error) {
	if store.getProfile == nil {
		return Profile{}, errors.New("unexpected profile Get")
	}
	return store.getProfile(ctx, id)
}

func (store *serviceStoreStub) CreateCertificateProfile(ctx context.Context, profile Profile, actorID string) error {
	if store.createProfile == nil {
		return errors.New("unexpected profile Create")
	}
	return store.createProfile(ctx, profile, actorID)
}

func (store *serviceStoreStub) ReplaceCertificateProfile(ctx context.Context, revision uint64, profile Profile, actorID string) (string, []Binding, error) {
	if store.replaceProfile == nil {
		return "", nil, errors.New("unexpected profile Replace")
	}
	return store.replaceProfile(ctx, revision, profile, actorID)
}

func (store *serviceStoreStub) GetCertificateBinding(ctx context.Context, edgeID string) (Binding, bool, error) {
	if store.getBinding == nil {
		return Binding{}, false, errors.New("unexpected binding Get")
	}
	return store.getBinding(ctx, edgeID)
}

func (*serviceStoreStub) BindCertificateProfile(context.Context, edgeconfig.Edge, Profile, uint64, string, time.Time) (Binding, error) {
	return Binding{}, errors.New("unexpected profile Bind")
}

func (*serviceStoreStub) UnbindCertificateProfile(context.Context, string, uint64, string, time.Time) (Binding, error) {
	return Binding{}, errors.New("unexpected profile Unbind")
}

func (*serviceStoreStub) RecordCertificateApplied(context.Context, string, *cloudv1.CertificateApplied, time.Time) error {
	return errors.New("unexpected applied record")
}
