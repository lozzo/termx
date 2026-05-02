package machines_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestBootstrapClaimAndListDoNotStorePrivateKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-bootstrap-test")
	ownerID := registerUser(t, ctx, db, "owner-bootstrap@example.com")
	svc := machines.NewService(machines.Config{DB: db, Clock: fixedClock(time.Date(2026, 5, 3, 5, 5, 0, 0, time.UTC))})

	if _, err := svc.Bootstrap(ctx, machines.BootstrapInput{
		MachinePublicKey:  "machine-public-key",
		DisplayName:       "Dev Machine",
		Hostname:          "dev.local",
		Platform:          "darwin/arm64",
		MachinePrivateKey: "should-never-be-stored",
	}); err == nil {
		t.Fatal("bootstrap accepted uploaded machine private key")
	}
	bootstrap, err := svc.Bootstrap(ctx, machines.BootstrapInput{
		MachinePublicKey: "machine-public-key",
		DisplayName:      "Dev Machine",
		Hostname:         "dev.local",
		Platform:         "darwin/arm64",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if bootstrap.Machine.ID == "" {
		t.Fatal("bootstrap did not assign machine id")
	}
	if bootstrap.ClaimToken == "" {
		t.Fatal("bootstrap did not return claim token")
	}
	if bootstrap.Machine.OwnerUserID != "" {
		t.Fatalf("bootstrap owner = %q, want unclaimed", bootstrap.Machine.OwnerUserID)
	}
	if containsPrivateMaterial(t, bootstrap) {
		t.Fatalf("bootstrap response leaked private material: %+v", bootstrap)
	}
	assertNoPrivateColumns(t, db, "machines")

	claimed, err := svc.Claim(ctx, machines.ClaimInput{
		UserID:     ownerID,
		MachineID:  bootstrap.Machine.ID,
		ClaimToken: bootstrap.ClaimToken,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.OwnerUserID != ownerID {
		t.Fatalf("claimed owner = %q", claimed.OwnerUserID)
	}

	listed, err := svc.ListMachines(ctx, ownerID)
	if err != nil {
		t.Fatalf("list machines: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != bootstrap.Machine.ID {
		t.Fatalf("listed machines = %+v", listed)
	}
	if containsPrivateMaterial(t, listed) {
		t.Fatalf("machine list leaked private material: %+v", listed)
	}
}

func TestOwnershipAndClaimBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-owner-test")
	ownerID := registerUser(t, ctx, db, "owner-boundary@example.com")
	otherID := registerUser(t, ctx, db, "other-boundary@example.com")
	svc := machines.NewService(machines.Config{DB: db, Clock: fixedClock(time.Date(2026, 5, 3, 5, 6, 0, 0, time.UTC))})

	bootstrap, err := svc.Bootstrap(ctx, machines.BootstrapInput{
		MachinePublicKey: "owner-machine-public-key",
		DisplayName:      "Owned Machine",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if _, err := svc.Claim(ctx, machines.ClaimInput{UserID: ownerID, MachineID: bootstrap.Machine.ID}); err == nil {
		t.Fatal("claim without claim token succeeded")
	}
	if _, err := svc.Claim(ctx, machines.ClaimInput{UserID: ownerID, MachineID: bootstrap.Machine.ID, ClaimToken: "wrong"}); err == nil {
		t.Fatal("claim with wrong token succeeded")
	}
	if _, err := svc.Claim(ctx, machines.ClaimInput{UserID: ownerID, MachineID: bootstrap.Machine.ID, ClaimToken: bootstrap.ClaimToken}); err != nil {
		t.Fatalf("claim owner: %v", err)
	}
	if _, err := svc.Claim(ctx, machines.ClaimInput{UserID: otherID, MachineID: bootstrap.Machine.ID, ClaimToken: bootstrap.ClaimToken}); err == nil {
		t.Fatal("second user claimed an owned machine")
	}
	if _, err := svc.GetMachine(ctx, otherID, bootstrap.Machine.ID); err == nil {
		t.Fatal("other user read owned machine")
	}
	if _, err := svc.GetMachine(ctx, ownerID, bootstrap.Machine.ID); err != nil {
		t.Fatalf("owner could not read machine: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, machines.BootstrapInput{
		MachineID:        bootstrap.Machine.ID,
		MachinePublicKey: "attacker-public-key",
		DisplayName:      "mutated",
	}); err == nil {
		t.Fatal("bootstrap mutated an owned machine")
	}
}

func TestAppCertificateMetadataRevocationAndValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := openTestDB(t, ctx, "termx-machines-certs-test")
	ownerID := registerUser(t, ctx, db, "owner-certs@example.com")
	otherID := registerUser(t, ctx, db, "other-certs@example.com")
	clock := &mutableClock{value: time.Date(2026, 5, 3, 5, 7, 0, 0, time.UTC)}
	svc := machines.NewService(machines.Config{DB: db, Clock: clock})

	machinePub, machinePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate machine key: %v", err)
	}
	appPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate app key: %v", err)
	}
	bootstrap, err := svc.Bootstrap(ctx, machines.BootstrapInput{
		MachinePublicKey: base64.RawURLEncoding.EncodeToString(machinePub),
		DisplayName:      "Cert Machine",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	claimed, err := svc.Claim(ctx, machines.ClaimInput{UserID: ownerID, MachineID: bootstrap.Machine.ID, ClaimToken: bootstrap.ClaimToken})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	payload := signedPayload(t, machinePriv, certificatePayload{
		CertID:       "cert_test",
		MachineID:    claimed.ID,
		AppPublicKey: base64.RawURLEncoding.EncodeToString(appPub),
		ExpiresAt:    clock.Now().Add(time.Hour),
	})
	if _, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Alice Phone",
		CertificatePayload:   payload.Body,
		CertificateSignature: payload.Signature,
		AppPrivateKey:        "must-not-store",
		ExpiresAt:            clock.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("register app certificate accepted uploaded app private key")
	}
	privatePayload := signedPayload(t, machinePriv, certificatePayload{
		CertID:       "private_cert",
		MachineID:    claimed.ID,
		AppPublicKey: base64.RawURLEncoding.EncodeToString(appPub),
		ExpiresAt:    clock.Now().Add(time.Hour),
		Extra: map[string]any{
			"d":   "private-exponent",
			"pem": "-----BEGIN PRIVATE KEY-----secret-----END PRIVATE KEY-----",
		},
	})
	if _, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Alice Phone",
		CertificatePayload:   privatePayload.Body,
		CertificateSignature: privatePayload.Signature,
		ExpiresAt:            clock.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("private-key-shaped certificate metadata was accepted")
	}
	cert, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Alice Phone",
		CertificatePayload:   payload.Body,
		CertificateSignature: payload.Signature,
		ExpiresAt:            clock.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("register app certificate: %v", err)
	}
	if cert.ID == "" || cert.AppDeviceID == "" {
		t.Fatalf("cert missing ids: %+v", cert)
	}
	if containsPrivateMaterial(t, cert) {
		t.Fatalf("certificate response leaked private material: %+v", cert)
	}
	assertNoPrivateColumns(t, db, "app_devices")
	assertNoPrivateColumns(t, db, "app_certificates")

	mismatchPayload := signedPayload(t, machinePriv, certificatePayload{
		CertID:       "mismatch",
		MachineID:    "other_machine",
		AppPublicKey: base64.RawURLEncoding.EncodeToString(appPub),
		ExpiresAt:    clock.Now().Add(time.Hour),
	})
	if _, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Alice Phone",
		CertificatePayload:   mismatchPayload.Body,
		CertificateSignature: mismatchPayload.Signature,
		ExpiresAt:            clock.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("mismatched certificate payload machine_id was accepted")
	}
	if _, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Alice Phone",
		CertificatePayload:   payload.Body,
		CertificateSignature: base64.RawURLEncoding.EncodeToString([]byte("invalid-signature")),
		ExpiresAt:            clock.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("invalid certificate signature was accepted")
	}

	if _, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               otherID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(appPub),
		AppDisplayName:       "Other Phone",
		CertificatePayload:   payload.Body,
		CertificateSignature: payload.Signature,
		ExpiresAt:            clock.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("other user registered certificate for owned machine")
	}

	certs, err := svc.ListAppCertificates(ctx, ownerID, claimed.ID)
	if err != nil {
		t.Fatalf("list certs: %v", err)
	}
	if len(certs) != 1 || certs[0].ID != cert.ID {
		t.Fatalf("listed certs = %+v", certs)
	}
	if containsPrivateMaterial(t, certs) {
		t.Fatalf("certificate list leaked private material: %+v", certs)
	}

	if err := svc.ValidateAppCertificate(ctx, ownerID, claimed.ID, cert.ID); err != nil {
		t.Fatalf("validate fresh cert: %v", err)
	}
	if err := svc.RevokeAppCertificate(ctx, ownerID, claimed.ID, cert.ID); err != nil {
		t.Fatalf("revoke cert: %v", err)
	}
	if err := svc.ValidateAppCertificate(ctx, ownerID, claimed.ID, cert.ID); err == nil {
		t.Fatal("revoked certificate validated")
	}

	expiredAppPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate expired app key: %v", err)
	}
	expiredPayload := signedPayload(t, machinePriv, certificatePayload{
		CertID:       "expired",
		MachineID:    claimed.ID,
		AppPublicKey: base64.RawURLEncoding.EncodeToString(expiredAppPub),
		ExpiresAt:    clock.Now().Add(time.Minute),
	})
	expired, err := svc.RegisterAppCertificate(ctx, machines.RegisterAppCertificateInput{
		UserID:               ownerID,
		MachineID:            claimed.ID,
		AppPublicKey:         base64.RawURLEncoding.EncodeToString(expiredAppPub),
		AppDisplayName:       "Expired Phone",
		CertificatePayload:   expiredPayload.Body,
		CertificateSignature: expiredPayload.Signature,
		ExpiresAt:            clock.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("register expired candidate: %v", err)
	}
	clock.value = clock.value.Add(2 * time.Minute)
	if err := svc.ValidateAppCertificate(ctx, ownerID, claimed.ID, expired.ID); err == nil {
		t.Fatal("expired certificate validated")
	}
}

type signedCertificatePayload struct {
	Body      string
	Signature string
}

type certificatePayload struct {
	CertID       string
	MachineID    string
	AppPublicKey string
	ExpiresAt    time.Time
	Extra        map[string]any
}

func signedPayload(t *testing.T, machinePrivate ed25519.PrivateKey, payload certificatePayload) signedCertificatePayload {
	t.Helper()
	body := map[string]any{
		"cert_id":        payload.CertID,
		"machine_id":     payload.MachineID,
		"app_public_key": payload.AppPublicKey,
		"expires_at":     payload.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	for key, value := range payload.Extra {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signature := ed25519.Sign(machinePrivate, encoded)
	return signedCertificatePayload{
		Body:      string(encoded),
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}
}

func registerUser(t *testing.T, ctx context.Context, db *sql.DB, email string) string {
	t.Helper()
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 5, 4, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-3-machine-test-secret")),
	})
	auth, err := accounts.Register(ctx, account.RegisterInput{Email: email, Password: "valid password"})
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	return auth.User.ID
}

func openTestDB(t *testing.T, ctx context.Context, name string) *sql.DB {
	t.Helper()
	db, err := store.OpenSQLite(ctx, "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func containsPrivateMaterial(t *testing.T, value any) bool {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	text := strings.ToLower(string(payload))
	return strings.Contains(text, "private_key") ||
		strings.Contains(text, "privatekey") ||
		strings.Contains(text, "must-not-store") ||
		strings.Contains(text, "should-never-be-stored")
}

func assertNoPrivateColumns(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("table info %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "private") || strings.Contains(lower, "secret") {
			t.Fatalf("table %s contains private/secret column %s", table, name)
		}
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}
