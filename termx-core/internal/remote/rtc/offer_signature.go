package rtc

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/lozzow/termx/termx-core/internal/remote/cert"
)

type OfferSignature struct {
	Algorithm string `json:"algorithm"`
	Nonce     string `json:"nonce"`
	Timestamp int64  `json:"timestamp"`
	Value     string `json:"value"`
}

type OfferSignatureFields struct {
	TicketID   string
	MachineID  string
	TerminalID string
	SDP        string
	Nonce      string
	Timestamp  time.Time
}

func CanonicalOfferSignatureMessage(fields OfferSignatureFields) []byte {
	timestamp := fields.Timestamp.UTC().Unix()
	sdpHash := sha256.Sum256([]byte(fields.SDP))
	message := strings.Join([]string{
		"termx-webrtc-offer-v1:",
		"ticket_id:" + strings.TrimSpace(fields.TicketID),
		"machine_id:" + strings.TrimSpace(fields.MachineID),
		"terminal_id:" + strings.TrimSpace(fields.TerminalID),
		"sha256(sdp):" + hex.EncodeToString(sdpHash[:]),
		"nonce:" + strings.TrimSpace(fields.Nonce),
		fmt.Sprintf("timestamp:%d", timestamp),
	}, "\n")
	return []byte(message)
}

func VerifyOfferSignature(
	signature OfferSignature,
	fields OfferSignatureFields,
	appPublicKey ed25519.PublicKey,
	replay *cert.ReplayWindow,
	now time.Time,
) error {
	if strings.ToLower(strings.TrimSpace(signature.Algorithm)) != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", signature.Algorithm)
	}
	if len(appPublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("app public key has size %d, want %d", len(appPublicKey), ed25519.PublicKeySize)
	}
	fields.Nonce = strings.TrimSpace(signature.Nonce)
	fields.Timestamp = time.Unix(signature.Timestamp, 0).UTC()
	if fields.Nonce == "" {
		return fmt.Errorf("signature nonce is required")
	}
	if signature.Timestamp == 0 {
		return fmt.Errorf("signature timestamp is required")
	}
	rawSignature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature.Value))
	if err != nil {
		return fmt.Errorf("decode offer signature: %w", err)
	}
	if len(rawSignature) != ed25519.SignatureSize {
		return fmt.Errorf("offer signature has size %d, want %d", len(rawSignature), ed25519.SignatureSize)
	}
	message := CanonicalOfferSignatureMessage(fields)
	if !ed25519.Verify(appPublicKey, message, rawSignature) {
		return fmt.Errorf("offer signature verification failed")
	}
	if replay != nil {
		if err := replay.Accept(fields.Nonce, fields.Timestamp, now); err != nil {
			return err
		}
	}
	return nil
}
