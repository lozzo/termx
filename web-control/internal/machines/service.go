package machines

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Config struct {
	DB    *sql.DB
	Clock Clock
}

type Service struct {
	db    *sql.DB
	clock Clock
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	return &Service{db: cfg.DB, clock: clock}
}

func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (BootstrapResult, error) {
	publicKey := strings.TrimSpace(in.MachinePublicKey)
	if publicKey == "" {
		return BootstrapResult{}, errors.New("machine public key is required")
	}
	if strings.TrimSpace(in.MachinePrivateKey) != "" {
		return BootstrapResult{}, errors.New("machine private key must not be uploaded")
	}
	id := strings.TrimSpace(in.MachineID)
	if id == "" {
		id = randomID("mach")
	}
	machine := Machine{
		ID:               id,
		MachinePublicKey: publicKey,
		DisplayName:      nonEmpty(in.DisplayName, "Unnamed machine"),
		Hostname:         strings.TrimSpace(in.Hostname),
		Platform:         strings.TrimSpace(in.Platform),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BootstrapResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if in.MachineID != "" {
		existing, err := loadMachineTx(ctx, tx, machine.ID)
		if err == nil && existing.OwnerUserID != "" {
			return BootstrapResult{}, errors.New("claimed machine cannot be bootstrapped without owner authentication")
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return BootstrapResult{}, err
		}
	}
	claimToken := randomID("claim")
	claimTokenHash := hashToken(claimToken)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO machines(id, machine_public_key, claim_token_hash, display_name, hostname, platform, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			machine_public_key = excluded.machine_public_key,
			claim_token_hash = excluded.claim_token_hash,
			display_name = excluded.display_name,
			hostname = excluded.hostname,
			platform = excluded.platform,
			last_seen_at = excluded.last_seen_at
		WHERE machines.owner_user_id IS NULL
	`, machine.ID, machine.MachinePublicKey, claimTokenHash, machine.DisplayName, machine.Hostname, machine.Platform, formatTime(s.clock.Now())); err != nil {
		return BootstrapResult{}, fmt.Errorf("bootstrap machine: %w", err)
	}
	if in.MachineID != "" {
		var owner sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT owner_user_id FROM machines WHERE id = ?`, machine.ID).Scan(&owner); err != nil {
			return BootstrapResult{}, err
		}
		if owner.Valid && owner.String != "" {
			return BootstrapResult{}, errors.New("claimed machine cannot be bootstrapped without owner authentication")
		}
	}
	if err := tx.Commit(); err != nil {
		return BootstrapResult{}, err
	}
	tx = nil
	now := s.clock.Now()
	machine.LastSeenAt = &now
	return BootstrapResult{Machine: machine, ClaimToken: claimToken}, nil
}

func (s *Service) Claim(ctx context.Context, in ClaimInput) (Machine, error) {
	userID := strings.TrimSpace(in.UserID)
	machineID := strings.TrimSpace(in.MachineID)
	if userID == "" || machineID == "" {
		return Machine{}, errors.New("user id and machine id are required")
	}
	claimToken := strings.TrimSpace(in.ClaimToken)
	if claimToken == "" {
		return Machine{}, errors.New("claim token is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Machine{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	machine, err := loadMachineTx(ctx, tx, machineID)
	if err != nil {
		return Machine{}, err
	}
	if machine.OwnerUserID != "" && machine.OwnerUserID != userID {
		return Machine{}, errors.New("machine is owned by another user")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE machines
		SET owner_user_id = ?, claim_token_hash = ''
		WHERE id = ? AND owner_user_id IS NULL AND claim_token_hash = ?
	`, userID, machineID, hashToken(claimToken))
	if err != nil {
		return Machine{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Machine{}, err
	}
	if rows != 1 {
		return Machine{}, errors.New("invalid claim token")
	}
	machine.OwnerUserID = userID
	if err := tx.Commit(); err != nil {
		return Machine{}, err
	}
	tx = nil
	return machine, nil
}

func (s *Service) ListMachines(ctx context.Context, userID string) ([]Machine, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_user_id, machine_public_key, display_name, hostname, platform, last_seen_at
		FROM machines
		WHERE owner_user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Machine
	for rows.Next() {
		machine, err := scanMachine(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, machine)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) GetMachine(ctx context.Context, userID string, machineID string) (Machine, error) {
	machine, err := s.loadOwnedMachine(ctx, userID, machineID)
	if err != nil {
		return Machine{}, err
	}
	return machine, nil
}

func (s *Service) RegisterAppCertificate(ctx context.Context, in RegisterAppCertificateInput) (AppCertificate, error) {
	machine, err := s.loadOwnedMachine(ctx, in.UserID, in.MachineID)
	if err != nil {
		return AppCertificate{}, err
	}
	if strings.TrimSpace(in.AppPrivateKey) != "" {
		return AppCertificate{}, errors.New("app private key must not be uploaded")
	}
	appPublicKey := strings.TrimSpace(in.AppPublicKey)
	if appPublicKey == "" {
		return AppCertificate{}, errors.New("app public key is required")
	}
	if in.ExpiresAt.IsZero() {
		return AppCertificate{}, errors.New("certificate expiry is required")
	}
	payload, err := validateCertificatePayload(machine.MachinePublicKey, in.MachineID, appPublicKey, in.ExpiresAt, in.CertificatePayload, in.CertificateSignature)
	if err != nil {
		return AppCertificate{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AppCertificate{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	appDeviceID := randomID("appdev")
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_devices(id, user_id, app_public_key, display_name)
		VALUES (?, ?, ?, ?)
	`, appDeviceID, in.UserID, appPublicKey, nonEmpty(in.AppDisplayName, "Unnamed app")); err != nil {
		return AppCertificate{}, err
	}
	cert := AppCertificate{
		ID:                   randomID("cert"),
		MachineID:            in.MachineID,
		AppDeviceID:          appDeviceID,
		AppPublicKey:         appPublicKey,
		AppDisplayName:       nonEmpty(in.AppDisplayName, "Unnamed app"),
		CertificatePayload:   payload,
		CertificateSignature: strings.TrimSpace(in.CertificateSignature),
		ExpiresAt:            in.ExpiresAt.UTC(),
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO app_certificates(id, machine_id, app_device_id, certificate_payload, certificate_signature, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, cert.ID, cert.MachineID, cert.AppDeviceID, cert.CertificatePayload, cert.CertificateSignature, formatTime(cert.ExpiresAt)); err != nil {
		return AppCertificate{}, err
	}
	if err := tx.Commit(); err != nil {
		return AppCertificate{}, err
	}
	tx = nil
	return cert, nil
}

func (s *Service) ListAppCertificates(ctx context.Context, userID string, machineID string) ([]AppCertificate, error) {
	if _, err := s.loadOwnedMachine(ctx, userID, machineID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.machine_id, c.app_device_id, d.app_public_key, d.display_name, c.certificate_payload, c.certificate_signature, c.revoked_at, c.expires_at
		FROM app_certificates c
		JOIN app_devices d ON d.id = c.app_device_id
		WHERE c.machine_id = ?
		ORDER BY c.created_at DESC
	`, machineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AppCertificate
	for rows.Next() {
		cert, err := scanAppCertificate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, cert)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) RevokeAppCertificate(ctx context.Context, userID string, machineID string, certID string) error {
	if _, err := s.loadOwnedMachine(ctx, userID, machineID); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE app_certificates
		SET revoked_at = ?
		WHERE id = ? AND machine_id = ? AND revoked_at IS NULL
	`, formatTime(s.clock.Now()), certID, machineID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errors.New("app certificate not found or already revoked")
	}
	return nil
}

func (s *Service) ValidateAppCertificate(ctx context.Context, userID string, machineID string, certID string) error {
	if _, err := s.loadOwnedMachine(ctx, userID, machineID); err != nil {
		return err
	}
	cert, err := s.loadCertificate(ctx, machineID, certID)
	if err != nil {
		return err
	}
	if cert.RevokedAt != nil {
		return errors.New("app certificate is revoked")
	}
	if !s.clock.Now().Before(cert.ExpiresAt) {
		return errors.New("app certificate is expired")
	}
	return nil
}

func (s *Service) loadOwnedMachine(ctx context.Context, userID string, machineID string) (Machine, error) {
	userID = strings.TrimSpace(userID)
	machineID = strings.TrimSpace(machineID)
	if userID == "" || machineID == "" {
		return Machine{}, errors.New("user id and machine id are required")
	}
	machine, err := loadMachine(ctx, s.db, machineID)
	if err != nil {
		return Machine{}, err
	}
	if machine.OwnerUserID != userID {
		return Machine{}, errors.New("machine not found")
	}
	return machine, nil
}

func (s *Service) loadCertificate(ctx context.Context, machineID string, certID string) (AppCertificate, error) {
	var row singleRow = s.db.QueryRowContext(ctx, `
		SELECT c.id, c.machine_id, c.app_device_id, d.app_public_key, d.display_name, c.certificate_payload, c.certificate_signature, c.revoked_at, c.expires_at
		FROM app_certificates c
		JOIN app_devices d ON d.id = c.app_device_id
		WHERE c.id = ? AND c.machine_id = ?
	`, certID, machineID)
	return scanAppCertificate(row)
}

type singleRow interface {
	Scan(dest ...any) error
}

type machineScanner interface {
	Scan(dest ...any) error
}

func loadMachine(ctx context.Context, db *sql.DB, id string) (Machine, error) {
	var row machineScanner = db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, machine_public_key, display_name, hostname, platform, last_seen_at
		FROM machines
		WHERE id = ?
	`, id)
	return scanMachine(row)
}

func loadMachineTx(ctx context.Context, tx *sql.Tx, id string) (Machine, error) {
	var row machineScanner = tx.QueryRowContext(ctx, `
		SELECT id, owner_user_id, machine_public_key, display_name, hostname, platform, last_seen_at
		FROM machines
		WHERE id = ?
	`, id)
	return scanMachine(row)
}

func scanMachine(row machineScanner) (Machine, error) {
	var machine Machine
	var owner sql.NullString
	var lastSeen sql.NullString
	err := row.Scan(&machine.ID, &owner, &machine.MachinePublicKey, &machine.DisplayName, &machine.Hostname, &machine.Platform, &lastSeen)
	if err != nil {
		return Machine{}, err
	}
	if owner.Valid {
		machine.OwnerUserID = owner.String
	}
	if lastSeen.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, lastSeen.String); err == nil {
			machine.LastSeenAt = &parsed
		}
	}
	return machine, nil
}

func scanAppCertificate(row singleRow) (AppCertificate, error) {
	var cert AppCertificate
	var revoked sql.NullString
	var expiresAt string
	if err := row.Scan(&cert.ID, &cert.MachineID, &cert.AppDeviceID, &cert.AppPublicKey, &cert.AppDisplayName, &cert.CertificatePayload, &cert.CertificateSignature, &revoked, &expiresAt); err != nil {
		return AppCertificate{}, err
	}
	if revoked.Valid {
		if parsed, err := time.Parse(time.RFC3339Nano, revoked.String); err == nil {
			cert.RevokedAt = &parsed
		}
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return AppCertificate{}, err
	}
	cert.ExpiresAt = parsedExpiry
	return cert, nil
}

func validateCertificatePayload(machinePublicKey string, machineID string, appPublicKey string, expiresAt time.Time, payload string, signature string) (string, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", errors.New("certificate payload is required")
	}
	if strings.TrimSpace(signature) == "" {
		return "", errors.New("certificate signature is required")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return "", errors.New("certificate payload must be JSON")
	}
	if containsPrivateShape(decoded) {
		return "", errors.New("certificate payload contains private key material")
	}
	payloadMachineID, _ := decoded["machine_id"].(string)
	payloadAppPublicKey, _ := decoded["app_public_key"].(string)
	payloadExpiresAt, _ := decoded["expires_at"].(string)
	if payloadMachineID != machineID {
		return "", errors.New("certificate payload machine mismatch")
	}
	if payloadAppPublicKey != appPublicKey {
		return "", errors.New("certificate payload app key mismatch")
	}
	parsedExpiry, err := time.Parse(time.RFC3339Nano, payloadExpiresAt)
	if err != nil {
		return "", errors.New("certificate payload expiry is invalid")
	}
	if !parsedExpiry.Equal(expiresAt.UTC()) {
		return "", errors.New("certificate payload expiry mismatch")
	}
	machineKeyBytes, err := base64.RawURLEncoding.DecodeString(machinePublicKey)
	if err != nil || len(machineKeyBytes) != ed25519.PublicKeySize {
		return "", errors.New("machine public key is invalid")
	}
	signatureBytes, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize {
		return "", errors.New("certificate signature is invalid")
	}
	if !ed25519.Verify(ed25519.PublicKey(machineKeyBytes), []byte(payload), signatureBytes) {
		return "", errors.New("certificate signature verification failed")
	}
	out, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func containsPrivateShape(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "private") ||
				strings.Contains(lower, "secret") ||
				lower == "d" ||
				lower == "p" ||
				lower == "q" ||
				lower == "dp" ||
				lower == "dq" ||
				lower == "qi" {
				return true
			}
			if containsPrivateShape(item) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range typed {
			if containsPrivateShape(item) {
				return true
			}
		}
		return false
	case string:
		lower := strings.ToLower(typed)
		return strings.Contains(lower, "private key") ||
			strings.Contains(lower, "begin private") ||
			strings.Contains(lower, "app_private_key") ||
			strings.Contains(lower, "machine_private_key")
	default:
		return false
	}
}

func nonEmpty(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
