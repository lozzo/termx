package rendezvous

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultTTLSeconds      = 600
	defaultMaxPayloadBytes = 64 * 1024
	defaultMaxMessages     = 64
)

const (
	MessageOffer     MessageType = "offer"
	MessageAnswer    MessageType = "answer"
	MessageCandidate MessageType = "candidate"
)

type MessageType string

type Config struct {
	Now                   func() time.Time
	MaxPayloadBytes       int
	MaxTTLSeconds         int
	MaxMessagesPerChannel int
	PublicSTUNServers     []string
}

type Store struct {
	cfg Config

	mu       sync.Mutex
	channels map[string]*channelState
}

type CreateChannelRequest struct {
	MachineID                   string `json:"machine_id"`
	MachinePublicKeyFingerprint string `json:"machine_public_key_fingerprint"`
	TTLSeconds                  int    `json:"ttl_seconds"`
}

type Channel struct {
	ChannelID                   string    `json:"channel_id"`
	ChannelSecret               string    `json:"channel_secret"`
	MachineID                   string    `json:"machine_id"`
	MachinePublicKeyFingerprint string    `json:"machine_public_key_fingerprint"`
	ExpiresAt                   time.Time `json:"expires_at"`
	PublicSTUNServers           []string  `json:"public_stun_servers"`
}

type Message struct {
	Type         MessageType `json:"type"`
	From         string      `json:"from,omitempty"`
	AppPublicKey string      `json:"app_public_key,omitempty"`
	Payload      []byte      `json:"payload"`
}

type channelState struct {
	channel      Channel
	secretHash   string
	appPublicKey string
	messages     []Message
}

func NewMemoryStore(cfg Config) *Store {
	if cfg.MaxPayloadBytes <= 0 {
		cfg.MaxPayloadBytes = defaultMaxPayloadBytes
	}
	if cfg.MaxTTLSeconds <= 0 {
		cfg.MaxTTLSeconds = defaultTTLSeconds
	}
	if cfg.MaxMessagesPerChannel <= 0 {
		cfg.MaxMessagesPerChannel = defaultMaxMessages
	}
	if len(cfg.PublicSTUNServers) == 0 {
		cfg.PublicSTUNServers = []string{
			"stun:stun.l.google.com:19302",
			"stun:stun.cloudflare.com:3478",
		}
	} else {
		cfg.PublicSTUNServers = append([]string(nil), cfg.PublicSTUNServers...)
	}
	return &Store{
		cfg:      cfg,
		channels: make(map[string]*channelState),
	}
}

func (s *Store) CreateChannel(req CreateChannelRequest) (Channel, error) {
	if s == nil {
		return Channel{}, errors.New("rendezvous store is nil")
	}
	if err := validatePublicSTUNServers(s.cfg.PublicSTUNServers); err != nil {
		return Channel{}, err
	}
	if strings.TrimSpace(req.MachineID) == "" {
		return Channel{}, errors.New("machine_id is required")
	}
	if strings.TrimSpace(req.MachinePublicKeyFingerprint) == "" {
		return Channel{}, errors.New("machine_public_key_fingerprint is required")
	}
	ttl := req.TTLSeconds
	if ttl <= 0 {
		ttl = defaultTTLSeconds
	}
	if ttl > s.cfg.MaxTTLSeconds {
		return Channel{}, fmt.Errorf("ttl_seconds %d exceeds max %d", ttl, s.cfg.MaxTTLSeconds)
	}
	channelID, err := randomToken("rv_", 16)
	if err != nil {
		return Channel{}, err
	}
	secret, err := randomToken("", 24)
	if err != nil {
		return Channel{}, err
	}
	now := s.now()
	channel := Channel{
		ChannelID:                   channelID,
		ChannelSecret:               secret,
		MachineID:                   strings.TrimSpace(req.MachineID),
		MachinePublicKeyFingerprint: strings.TrimSpace(req.MachinePublicKeyFingerprint),
		ExpiresAt:                   now.Add(time.Duration(ttl) * time.Second).UTC(),
		PublicSTUNServers:           append([]string(nil), s.cfg.PublicSTUNServers...),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.channels[channel.ChannelID] = &channelState{
		channel:    channel,
		secretHash: channel.ChannelSecret,
	}
	return channel, nil
}

func (s *Store) PostMessage(channelID, channelSecret string, msg Message) error {
	if s == nil {
		return errors.New("rendezvous store is nil")
	}
	if len(msg.Payload) > s.cfg.MaxPayloadBytes {
		return fmt.Errorf("payload size %d exceeds limit %d", len(msg.Payload), s.cfg.MaxPayloadBytes)
	}
	if !validMessageType(msg.Type) {
		return fmt.Errorf("unsupported message type %q", msg.Type)
	}
	if err := validateSignalingPayload(msg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.authorizedStateLocked(channelID, channelSecret)
	if err != nil {
		return err
	}
	appPublicKey := strings.TrimSpace(msg.AppPublicKey)
	if appPublicKey != "" {
		if state.appPublicKey == "" {
			state.appPublicKey = appPublicKey
		} else if subtle.ConstantTimeCompare([]byte(state.appPublicKey), []byte(appPublicKey)) != 1 {
			return errors.New("channel already claimed by a different app public key")
		}
	}
	if len(state.messages) >= s.cfg.MaxMessagesPerChannel {
		return fmt.Errorf("channel message count exceeds limit %d", s.cfg.MaxMessagesPerChannel)
	}
	msg.Payload = append([]byte(nil), msg.Payload...)
	state.messages = append(state.messages, msg)
	return nil
}

func (s *Store) Events(channelID, channelSecret string) ([]Message, error) {
	if s == nil {
		return nil, errors.New("rendezvous store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.authorizedStateLocked(channelID, channelSecret)
	if err != nil {
		return nil, err
	}
	out := make([]Message, len(state.messages))
	for i, msg := range state.messages {
		out[i] = msg
		out[i].Payload = append([]byte(nil), msg.Payload...)
	}
	return out, nil
}

func (s *Store) authorizedStateLocked(channelID, channelSecret string) (*channelState, error) {
	channelID = strings.TrimSpace(channelID)
	channelSecret = strings.TrimSpace(channelSecret)
	if channelID == "" {
		return nil, errors.New("channel_id is required")
	}
	if channelSecret == "" {
		return nil, errors.New("channel_secret is required")
	}
	state, ok := s.channels[channelID]
	if !ok {
		return nil, errors.New("channel not found")
	}
	if !s.now().Before(state.channel.ExpiresAt) {
		delete(s.channels, channelID)
		return nil, errors.New("channel expired")
	}
	if subtle.ConstantTimeCompare([]byte(state.secretHash), []byte(channelSecret)) != 1 {
		return nil, errors.New("invalid channel secret")
	}
	return state, nil
}

func validMessageType(typ MessageType) bool {
	switch typ {
	case MessageOffer, MessageAnswer, MessageCandidate:
		return true
	default:
		return false
	}
}

func validateSignalingPayload(msg Message) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(msg.Payload, &raw); err != nil {
		return fmt.Errorf("signaling payload must be JSON object: %w", err)
	}
	if len(raw) == 0 {
		return errors.New("signaling payload must not be empty")
	}
	switch msg.Type {
	case MessageOffer, MessageAnswer:
		if _, ok := raw["sdp"]; !ok {
			return fmt.Errorf("%s payload requires sdp", msg.Type)
		}
	case MessageCandidate:
		if _, ok := raw["candidate"]; !ok {
			return errors.New("candidate payload requires candidate")
		}
	}
	for key := range raw {
		switch key {
		case "sdp", "ice_candidates", "candidate", "sdp_type", "mid", "mline_index":
		default:
			return fmt.Errorf("unsupported signaling payload field %q", key)
		}
	}
	return nil
}

func (s *Store) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now().UTC()
	}
	return time.Now().UTC()
}

func validatePublicSTUNServers(servers []string) error {
	if len(servers) == 0 {
		return errors.New("public STUN servers are required")
	}
	for _, server := range servers {
		server = strings.TrimSpace(strings.ToLower(server))
		if server == "" {
			return errors.New("public STUN servers must not contain empty values")
		}
		if strings.HasPrefix(server, "turn:") || strings.HasPrefix(server, "turns:") {
			return fmt.Errorf("anonymous rendezvous must not include TURN server %q", server)
		}
		if !strings.HasPrefix(server, "stun:") && !strings.HasPrefix(server, "stuns:") {
			return fmt.Errorf("anonymous rendezvous server must be STUN, got %q", server)
		}
	}
	return nil
}

func randomToken(prefix string, byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
