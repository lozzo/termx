package edgeconfig_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/edgeconfig"
)

func TestServiceSignsVersionsAndConsumesTwoStageClaimOnce(t *testing.T) {
	_, signingKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0).UTC()
	store := &memoryStore{}
	service, err := edgeconfig.NewService(edgeconfig.Config{Store: store, SigningKey: signingKey, SigningKeyID: "config-key-1", ClaimTTL: 10 * time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	edge, installToken, expiresAt, err := service.CreateEdge(context.Background(), edgeconfig.CreateInput{Name: "上海边缘一号", Region: "cn-east", Capacity: 2000, PublicEndpoint: "edge.example.com:41102"})
	if err != nil {
		t.Fatalf("create Edge: %v", err)
	}
	if installToken == "" || len(store.installDigest) != 32 || expiresAt != now.Add(10*time.Minute) || edge.SignedConfig.GetKeyId() != "config-key-1" {
		t.Fatalf("create result token=%q digest=%x expires=%v signed=%v", installToken, store.installDigest, expiresAt, edge.SignedConfig)
	}
	verified, err := edgeconfig.VerifySignedConfig(edge.SignedConfig, "config-key-1", signingKey.Public().(ed25519.PublicKey))
	if err != nil || verified.GetEdgeId() != edge.ID || verified.GetPublicEndpoint() != "edge.example.com:41102" {
		t.Fatalf("verify desired config=%v err=%v", verified, err)
	}
	claimed, bootstrapToken, _, err := service.ConsumeInstallClaim(context.Background(), installToken)
	if err != nil || claimed.ID != edge.ID || bootstrapToken == "" {
		t.Fatalf("consume install claim edge=%+v token=%q err=%v", claimed, bootstrapToken, err)
	}
	if _, _, _, err := service.ConsumeInstallClaim(context.Background(), installToken); !errors.Is(err, edgeconfig.ErrClaimInvalid) {
		t.Fatalf("reused install claim error=%v", err)
	}
	csrDigest := bytes.Repeat([]byte{7}, 32)
	if _, err := service.ConsumeBootstrapClaim(context.Background(), bootstrapToken, edge.ID, csrDigest); err != nil {
		t.Fatalf("consume bootstrap: %v", err)
	}
	if _, err := service.ConsumeBootstrapClaim(context.Background(), bootstrapToken, edge.ID, csrDigest); !errors.Is(err, edgeconfig.ErrClaimInvalid) {
		t.Fatalf("reused bootstrap error=%v", err)
	}

	updated, err := service.UpdateEdge(context.Background(), edgeconfig.UpdateInput{EdgeID: edge.ID, ExpectedRevision: 1, Name: edge.Name, Region: "cn-south", Capacity: 3000, PublicEndpoint: edge.PublicEndpoint, Enabled: true})
	if err != nil || updated.ConfigVersion != 2 || updated.Revision != 2 {
		t.Fatalf("update Edge=%+v err=%v", updated, err)
	}
	if _, err := service.UpdateEdge(context.Background(), edgeconfig.UpdateInput{EdgeID: edge.ID, ExpectedRevision: 1, Name: edge.Name, Region: "cn-north", Capacity: 1, PublicEndpoint: edge.PublicEndpoint, Enabled: true}); !errors.Is(err, edgeconfig.ErrRevisionConflict) {
		t.Fatalf("stale update error=%v", err)
	}
	deleteInput := edgeconfig.DeleteInput{EdgeID: edge.ID, ExpectedRevision: updated.Revision, ActorID: "admin", Reason: "retire deployment"}
	if err := service.DeleteEdge(context.Background(), deleteInput); !errors.Is(err, edgeconfig.ErrEdgeEnabled) {
		t.Fatalf("enabled deletion error=%v", err)
	}
	disabled, err := service.UpdateEdge(context.Background(), edgeconfig.UpdateInput{EdgeID: edge.ID, ExpectedRevision: updated.Revision, Name: edge.Name, Region: updated.Region, Capacity: updated.Capacity, PublicEndpoint: edge.PublicEndpoint, Enabled: false})
	if err != nil {
		t.Fatalf("disable Edge: %v", err)
	}
	if err := service.DeleteEdge(context.Background(), deleteInput); !errors.Is(err, edgeconfig.ErrRevisionConflict) {
		t.Fatalf("stale deletion error=%v", err)
	}
	deleteInput.ExpectedRevision = disabled.Revision
	if err := service.DeleteEdge(context.Background(), deleteInput); err != nil {
		t.Fatalf("delete disabled Edge: %v", err)
	}
	if values, err := service.ListEdges(context.Background()); err != nil || len(values) != 0 {
		t.Fatalf("deleted Edge remains: values=%v err=%v", values, err)
	}
}

func TestServiceAcceptsIPAddressPublicEndpoint(t *testing.T) {
	_, signingKey, _ := ed25519.GenerateKey(rand.Reader)
	service, err := edgeconfig.NewService(edgeconfig.Config{
		Store: &memoryStore{}, SigningKey: signingKey, SigningKeyID: "config-key-ip", ClaimTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	edge, _, _, err := service.CreateEdge(context.Background(), edgeconfig.CreateInput{
		Name: "CN1 Edge", Region: "cn-east", Capacity: 1000, PublicEndpoint: "114.66.58.243:41102",
	})
	if err != nil {
		t.Fatalf("create Edge with IP endpoint: %v", err)
	}
	if edge.PublicEndpoint != "114.66.58.243:41102" {
		t.Fatalf("public endpoint = %q", edge.PublicEndpoint)
	}
}

func TestIdentityRecoveryClaimIsShortLivedAndOneTime(t *testing.T) {
	_, signingKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{edge: edgeconfig.Edge{ID: "edge-recovery", Enabled: true}}
	service, err := edgeconfig.NewService(edgeconfig.Config{
		Store: store, SigningKey: signingKey, SigningKeyID: "config-key-recovery", ClaimTTL: 10 * time.Minute, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	token, expiresAt, err := service.CreateIdentityRecoveryClaim(context.Background(), store.edge.ID, "admin-account", "expired identity certificate")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || expiresAt != now.Add(10*time.Minute) || len(store.recoveryDigest) != 32 {
		t.Fatalf("token=%q expires=%v digest=%x", token, expiresAt, store.recoveryDigest)
	}
	csrDigest := bytes.Repeat([]byte{9}, 32)
	if _, err := service.ConsumeIdentityRecoveryClaim(context.Background(), token, store.edge.ID, csrDigest); err != nil {
		t.Fatalf("consume recovery claim: %v", err)
	}
	if !bytes.Equal(store.recoveryCSR, csrDigest) {
		t.Fatalf("recovery CSR digest = %x", store.recoveryCSR)
	}
	if _, err := service.ConsumeIdentityRecoveryClaim(context.Background(), token, store.edge.ID, csrDigest); !errors.Is(err, edgeconfig.ErrClaimInvalid) {
		t.Fatalf("reused recovery claim error = %v", err)
	}
}

type memoryStore struct {
	mu              sync.Mutex
	edge            edgeconfig.Edge
	installDigest   []byte
	installConsumed bool
	bootstrapDigest []byte
	bootstrapUsed   bool
	recoveryDigest  []byte
	recoveryCSR     []byte
	recoveryUsed    bool
}

func (store *memoryStore) ListEdges(context.Context) ([]edgeconfig.Edge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.edge.ID == "" {
		return nil, nil
	}
	return []edgeconfig.Edge{store.edge}, nil
}

func (store *memoryStore) GetEdge(_ context.Context, edgeID string) (edgeconfig.Edge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.edge.ID != edgeID {
		return edgeconfig.Edge{}, errors.New("not found")
	}
	return store.edge, nil
}

func (store *memoryStore) CreateEdge(_ context.Context, edge edgeconfig.Edge, digest []byte, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.edge, store.installDigest = edge, append([]byte(nil), digest...)
	return nil
}

func (store *memoryStore) UpdateEdge(_ context.Context, input edgeconfig.UpdateInput, updated edgeconfig.Edge) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.edge.Revision != input.ExpectedRevision {
		return edgeconfig.ErrRevisionConflict
	}
	store.edge = updated
	return nil
}

func (store *memoryStore) DeleteEdge(_ context.Context, input edgeconfig.DeleteInput) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.edge.ID == "" {
		return edgeconfig.ErrEdgeNotFound
	}
	if store.edge.Revision != input.ExpectedRevision {
		return edgeconfig.ErrRevisionConflict
	}
	if store.edge.Enabled {
		return edgeconfig.ErrEdgeEnabled
	}
	store.edge = edgeconfig.Edge{}
	return nil
}

func (store *memoryStore) ConsumeInstallClaim(_ context.Context, installDigest, bootstrapDigest []byte, _ time.Time) (edgeconfig.Edge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.installConsumed || !bytes.Equal(store.installDigest, installDigest) {
		return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
	}
	store.installConsumed = true
	store.bootstrapDigest = append([]byte(nil), bootstrapDigest...)
	return store.edge, nil
}

func (store *memoryStore) ConsumeBootstrapClaim(_ context.Context, digest []byte, edgeID string, _ []byte) (edgeconfig.Edge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.bootstrapUsed || edgeID != store.edge.ID || !bytes.Equal(store.bootstrapDigest, digest) {
		return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
	}
	store.bootstrapUsed = true
	return store.edge, nil
}

func (store *memoryStore) CreateIdentityRecoveryClaim(_ context.Context, edgeID string, digest []byte, _ time.Time, actorID, reason string, _ time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.edge.ID != edgeID || actorID == "" || reason == "" {
		return edgeconfig.ErrEdgeNotFound
	}
	store.recoveryDigest = append([]byte(nil), digest...)
	store.recoveryUsed = false
	return nil
}

func (store *memoryStore) ConsumeIdentityRecoveryClaim(_ context.Context, digest []byte, edgeID string, csrDigest []byte, _ time.Time) (edgeconfig.Edge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.recoveryUsed || store.edge.ID != edgeID || !bytes.Equal(store.recoveryDigest, digest) {
		return edgeconfig.Edge{}, edgeconfig.ErrClaimInvalid
	}
	store.recoveryUsed = true
	store.recoveryCSR = append([]byte(nil), csrDigest...)
	return store.edge, nil
}
