package rendezvous

import (
	"context"
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

const (
	publicP2PPath              = "public_p2p"
	defaultChannelTTL          = 10 * time.Minute
	maxChannelTTL              = 15 * time.Minute
	defaultMaxPayloadBytes     = 64 * 1024
	defaultMaxMessagesPerTopic = 64
)

type Config struct {
	DB                    *sql.DB
	Clock                 Clock
	STUNServers           []string
	MaxPayloadBytes       int
	MaxMessagesPerChannel int
}

type Service struct {
	db                    *sql.DB
	clock                 Clock
	iceServers            []ICEServer
	maxPayloadBytes       int
	maxMessagesPerChannel int
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = realClock{}
	}
	maxPayloadBytes := cfg.MaxPayloadBytes
	if maxPayloadBytes <= 0 {
		maxPayloadBytes = defaultMaxPayloadBytes
	}
	maxMessagesPerChannel := cfg.MaxMessagesPerChannel
	if maxMessagesPerChannel <= 0 {
		maxMessagesPerChannel = defaultMaxMessagesPerTopic
	}
	return &Service{
		db:                    cfg.DB,
		clock:                 clock,
		iceServers:            stunOnlyServers(cfg.STUNServers),
		maxPayloadBytes:       maxPayloadBytes,
		maxMessagesPerChannel: maxMessagesPerChannel,
	}
}

func (s *Service) CreateChannel(ctx context.Context, in CreateChannelInput) (Channel, error) {
	if s == nil || s.db == nil {
		return Channel{}, errors.New("rendezvous service is not configured")
	}
	userID := strings.TrimSpace(in.UserID)
	machineID := strings.TrimSpace(in.MachineID)
	terminalID := strings.TrimSpace(in.TerminalID)
	if userID == "" || machineID == "" || terminalID == "" {
		return Channel{}, errors.New("user id, machine id, and terminal id are required")
	}
	if err := s.requireOwnedMachine(ctx, userID, machineID); err != nil {
		return Channel{}, err
	}
	if len(s.iceServers) == 0 {
		return Channel{}, errors.New("at least one STUN server is required for public_p2p rendezvous")
	}
	ttl := in.TTL
	if ttl <= 0 {
		ttl = defaultChannelTTL
	}
	if ttl > maxChannelTTL {
		ttl = maxChannelTTL
	}
	now := s.clock.Now().UTC()
	channel := Channel{
		ID:         randomID("rv"),
		Secret:     randomID("rvsec"),
		Path:       publicP2PPath,
		MachineID:  machineID,
		TerminalID: terminalID,
		ExpiresAt:  now.Add(ttl),
		ICEServers: cloneICEServers(s.iceServers),
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO rendezvous_channels(id, user_id, machine_id, terminal_id, secret_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, channel.ID, userID, machineID, terminalID, hashSecret(channel.Secret), formatTime(channel.ExpiresAt), formatTime(now)); err != nil {
		return Channel{}, fmt.Errorf("create rendezvous channel: %w", err)
	}
	return channel, nil
}

func (s *Service) Send(ctx context.Context, in SendMessageInput) error {
	if s == nil || s.db == nil {
		return errors.New("rendezvous service is not configured")
	}
	channelID := strings.TrimSpace(in.ChannelID)
	secret := strings.TrimSpace(in.Secret)
	messageType := strings.TrimSpace(in.Type)
	payload := strings.TrimSpace(in.Payload)
	if channelID == "" || secret == "" || payload == "" {
		return errors.New("channel id, secret, and payload are required")
	}
	if !validMessageType(messageType) {
		return errors.New("unsupported rendezvous message type")
	}
	if len([]byte(payload)) > s.maxPayloadBytes {
		return errors.New("rendezvous payload is too large")
	}
	if !json.Valid([]byte(payload)) {
		return errors.New("rendezvous payload must be JSON")
	}
	if err := validateSignalingPayload(messageType, []byte(payload)); err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err := s.loadAuthorizedChannelTx(ctx, tx, channelID, secret, now); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM rendezvous_messages WHERE channel_id = ?`, channelID).Scan(&count); err != nil {
		return err
	}
	if count >= s.maxMessagesPerChannel {
		return errors.New("rendezvous message rate limit exceeded")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rendezvous_messages(id, channel_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, randomID("rvmsg"), channelID, messageType, payload, formatTime(now)); err != nil {
		return fmt.Errorf("store rendezvous message: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (s *Service) ListMessages(ctx context.Context, in ListMessagesInput) ([]Message, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("rendezvous service is not configured")
	}
	channelID := strings.TrimSpace(in.ChannelID)
	secret := strings.TrimSpace(in.Secret)
	if channelID == "" || secret == "" {
		return nil, errors.New("channel id and secret are required")
	}
	if _, err := s.loadAuthorizedChannel(ctx, channelID, secret, s.clock.Now().UTC()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, payload, created_at
		FROM rendezvous_messages
		WHERE channel_id = ?
		ORDER BY created_at ASC, rowid ASC
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var msg Message
		var createdAt string
		if err := rows.Scan(&msg.ID, &msg.Type, &msg.Payload, &createdAt); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse rendezvous message time: %w", err)
		}
		msg.CreatedAt = parsed
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *Service) CleanupExpired(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("rendezvous service is not configured")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM rendezvous_channels WHERE expires_at <= ?`, formatTime(s.clock.Now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("cleanup expired rendezvous channels: %w", err)
	}
	return result.RowsAffected()
}

func (s *Service) requireOwnedMachine(ctx context.Context, userID string, machineID string) error {
	var owner sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT owner_user_id FROM machines WHERE id = ?`, machineID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("machine not found")
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return errors.New("machine is not owned by user")
	}
	return nil
}

func (s *Service) loadAuthorizedChannel(ctx context.Context, channelID string, secret string, now time.Time) (Channel, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, machine_id, terminal_id, secret_hash, expires_at
		FROM rendezvous_channels
		WHERE id = ?
	`, channelID)
	return scanAuthorizedChannel(row, secret, now)
}

func (s *Service) loadAuthorizedChannelTx(ctx context.Context, tx *sql.Tx, channelID string, secret string, now time.Time) (Channel, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, machine_id, terminal_id, secret_hash, expires_at
		FROM rendezvous_channels
		WHERE id = ?
	`, channelID)
	return scanAuthorizedChannel(row, secret, now)
}

func scanAuthorizedChannel(row *sql.Row, secret string, now time.Time) (Channel, error) {
	var channel Channel
	var secretHash string
	var expiresAt string
	if err := row.Scan(&channel.ID, &channel.MachineID, &channel.TerminalID, &secretHash, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Channel{}, errors.New("rendezvous channel not found")
		}
		return Channel{}, err
	}
	if secretHash != hashSecret(secret) {
		return Channel{}, errors.New("invalid rendezvous channel secret")
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return Channel{}, fmt.Errorf("parse rendezvous channel expiry: %w", err)
	}
	if !now.Before(parsed) {
		return Channel{}, errors.New("rendezvous channel expired")
	}
	channel.Path = publicP2PPath
	channel.ExpiresAt = parsed
	return channel, nil
}

func validMessageType(messageType string) bool {
	switch messageType {
	case MessageOffer, MessageAnswer, MessageCandidate:
		return true
	default:
		return false
	}
}

func stunOnlyServers(values []string) []ICEServer {
	var servers []ICEServer
	for _, value := range values {
		value = strings.TrimSpace(value)
		lower := strings.ToLower(value)
		if value == "" || (!strings.HasPrefix(lower, "stun:") && !strings.HasPrefix(lower, "stuns:")) {
			continue
		}
		servers = append(servers, ICEServer{URL: value})
	}
	return servers
}

func cloneICEServers(in []ICEServer) []ICEServer {
	out := make([]ICEServer, len(in))
	copy(out, in)
	return out
}

func validateSignalingPayload(messageType string, payload []byte) error {
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		return errors.New("rendezvous payload must be a JSON object")
	}
	if len(object) == 0 {
		return errors.New("rendezvous payload is empty")
	}
	switch messageType {
	case MessageOffer, MessageAnswer:
		return validateDescriptionPayload(messageType, object)
	case MessageCandidate:
		return validateCandidatePayload(object)
	default:
		return errors.New("unsupported rendezvous message type")
	}
}

func validateDescriptionPayload(messageType string, object map[string]any) error {
	envelopeKey := messageType
	if nested, ok := object[envelopeKey]; ok {
		allowed := map[string]struct{}{
			envelopeKey:       {},
			"app_certificate": {},
			"signature":       {},
		}
		if err := requireAllowedKeys(object, allowed); err != nil {
			return err
		}
		if cert, ok := object["app_certificate"]; ok {
			if err := validateAppCertificateEnvelope(cert); err != nil {
				return err
			}
		}
		if sig, ok := object["signature"]; ok {
			if err := validateSignatureEnvelope(sig); err != nil {
				return err
			}
		}
		nestedObject, ok := nested.(map[string]any)
		if !ok {
			return fmt.Errorf("rendezvous %s payload must be an object", messageType)
		}
		return validateDescriptionObject(nestedObject)
	}
	return validateDescriptionObject(object)
}

func validateDescriptionObject(object map[string]any) error {
	if err := requireAllowedKeys(object, map[string]struct{}{
		"sdp":            {},
		"type":           {},
		"session_id":     {},
		"ice_candidates": {},
	}); err != nil {
		return err
	}
	sdp, ok := object["sdp"].(string)
	if !ok || strings.TrimSpace(sdp) == "" {
		return errors.New("rendezvous description payload requires string sdp")
	}
	if forbiddenSignalingText(sdp) || sdpContainsRelayCandidate(sdp) {
		return errors.New("rendezvous payload may contain only WebRTC signaling data")
	}
	if value, ok := object["type"]; ok {
		typed, ok := value.(string)
		if !ok || forbiddenSignalingText(typed) {
			return errors.New("rendezvous description type must be a safe string")
		}
	}
	if value, ok := object["session_id"]; ok {
		typed, ok := value.(string)
		if !ok || strings.TrimSpace(typed) == "" || forbiddenSignalingText(typed) {
			return errors.New("rendezvous description session_id must be a safe string")
		}
	}
	if value, ok := object["ice_candidates"]; ok {
		items, ok := value.([]any)
		if !ok {
			return errors.New("rendezvous ice_candidates must be an array")
		}
		for _, item := range items {
			candidate, ok := item.(map[string]any)
			if !ok {
				return errors.New("rendezvous ice_candidates entries must be objects")
			}
			if err := validateCandidateObject(candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCandidatePayload(object map[string]any) error {
	if nested, ok := object["candidate"]; ok {
		switch typed := nested.(type) {
		case string:
			return validateCandidateObject(object)
		case map[string]any:
			if err := requireAllowedKeys(object, map[string]struct{}{
				"candidate":      {},
				"app_public_key": {},
			}); err != nil {
				return err
			}
			if value, ok := object["app_public_key"]; ok {
				if err := requireSafeString("app_public_key", value); err != nil {
					return err
				}
			}
			return validateCandidateObject(typed)
		default:
			return errors.New("rendezvous candidate payload requires string or object candidate")
		}
	}
	return errors.New("rendezvous candidate payload requires candidate")
}

func validateCandidateObject(object map[string]any) error {
	if err := requireAllowedKeys(object, map[string]struct{}{
		"candidate":        {},
		"sdpMid":           {},
		"sdpMLineIndex":    {},
		"usernameFragment": {},
		"mid":              {},
		"mline_index":      {},
	}); err != nil {
		return err
	}
	candidate, ok := object["candidate"].(string)
	if !ok || strings.TrimSpace(candidate) == "" {
		return errors.New("rendezvous candidate payload requires string candidate")
	}
	if forbiddenSignalingText(candidate) || candidateIsRelay(candidate) {
		return errors.New("rendezvous payload may contain only non-relay ICE signaling data")
	}
	for _, key := range []string{"sdpMid", "usernameFragment", "mid"} {
		if value, ok := object[key]; ok {
			if err := requireSafeString(key, value); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"sdpMLineIndex", "mline_index"} {
		if value, ok := object[key]; ok && !isJSONInteger(value) {
			return fmt.Errorf("rendezvous %s must be a number", key)
		}
	}
	return nil
}

func validateAppCertificateEnvelope(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("app_certificate must be an object")
	}
	if err := requireAllowedKeys(object, map[string]struct{}{
		"payload":   {},
		"signature": {},
	}); err != nil {
		return err
	}
	if payload, ok := object["payload"]; ok && forbiddenCertificatePayload("payload", payload) {
		return errors.New("app_certificate payload contains forbidden material")
	}
	if signature, ok := object["signature"]; ok {
		if err := requireSafeString("signature", signature); err != nil {
			return err
		}
	}
	return nil
}

func validateSignatureEnvelope(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return errors.New("signature must be an object")
	}
	if err := requireAllowedKeys(object, map[string]struct{}{
		"algorithm": {},
		"nonce":     {},
		"timestamp": {},
		"value":     {},
	}); err != nil {
		return err
	}
	for _, key := range []string{"algorithm", "nonce", "value"} {
		if value, ok := object[key]; ok {
			if err := requireSafeString(key, value); err != nil {
				return err
			}
		}
	}
	if value, ok := object["timestamp"]; ok && !isJSONInteger(value) {
		return errors.New("signature timestamp must be a number")
	}
	return nil
}

func requireAllowedKeys(object map[string]any, allowed map[string]struct{}) error {
	for key, value := range object {
		if _, ok := allowed[key]; !ok {
			return errors.New("rendezvous payload contains non-signaling fields")
		}
		if forbiddenSignalingText(key) || forbiddenSignalingValue("", value) {
			return errors.New("rendezvous payload may contain only WebRTC signaling data")
		}
	}
	return nil
}

func requireSafeString(key string, value any) error {
	typed, ok := value.(string)
	if !ok || forbiddenSignalingText(typed) {
		return fmt.Errorf("rendezvous %s must be a safe string", key)
	}
	return nil
}

func forbiddenSignalingValue(key string, value any) bool {
	if forbiddenSignalingText(key) {
		return true
	}
	switch typed := value.(type) {
	case string:
		return forbiddenSignalingText(typed)
	case []any:
		for _, item := range typed {
			if forbiddenSignalingValue("", item) {
				return true
			}
		}
	case map[string]any:
		for childKey, item := range typed {
			if forbiddenSignalingValue(childKey, item) {
				return true
			}
		}
	}
	return false
}

func forbiddenCertificatePayload(key string, value any) bool {
	if forbiddenSignalingText(key) || privateKeyField(key) {
		return true
	}
	switch typed := value.(type) {
	case string:
		return forbiddenSignalingText(typed)
	case []any:
		for _, item := range typed {
			if forbiddenCertificatePayload("", item) {
				return true
			}
		}
	case map[string]any:
		for childKey, item := range typed {
			if forbiddenCertificatePayload(childKey, item) {
				return true
			}
		}
	}
	return false
}

func isJSONInteger(value any) bool {
	typed, ok := value.(float64)
	if !ok {
		return false
	}
	return typed == float64(int64(typed))
}

func sdpContainsRelayCandidate(sdp string) bool {
	for line := range strings.Lines(sdp) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "a=candidate:") && candidateIsRelay(strings.TrimPrefix(line, "a=")) {
			return true
		}
	}
	return false
}

func candidateIsRelay(candidate string) bool {
	fields := strings.Fields(strings.ToLower(candidate))
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "typ" && fields[i+1] == "relay" {
			return true
		}
	}
	return false
}

func privateKeyField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "d", "p", "q", "dp", "dq", "qi", "oth", "x5c_private", "private", "privatekey", "private_key":
		return true
	default:
		return false
	}
}

func forbiddenSignalingText(value string) bool {
	lower := strings.ToLower(value)
	for _, forbidden := range []string{
		"terminal_data",
		"file_data",
		"api_data",
		"events_data",
		"terminal:",
		"file:",
		"turn:",
		"turns:",
		"credential",
		"private_key",
		"machine_private",
		"app_private",
		"begin private",
		"private key",
	} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}

func randomID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b[:])
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
