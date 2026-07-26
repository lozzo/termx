// Package configsignature 定义 Edge desired config 的独立签名域。
// 它不持有配置真值，只在 Controller 签名与 Edge 验签之间共享密码学契约。
package configsignature

import (
	"crypto/ed25519"
	"errors"
	"fmt"

	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

const domain = "muxvia.cloud.edge-config.v1\x00"

// Sign 对确定性 Proto payload 加独立 domain 后签名。
func Sign(config *cloudv1.EdgeDesiredConfig, keyID string, privateKey ed25519.PrivateKey) (*cloudv1.SignedEdgeDesiredConfig, error) {
	if config == nil || keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New("desired config, key ID, and Ed25519 private key are required")
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal desired Edge config: %w", err)
	}
	return &cloudv1.SignedEdgeDesiredConfig{KeyId: keyID, Payload: payload, Signature: ed25519.Sign(privateKey, append([]byte(domain), payload...))}, nil
}

// Verify 校验 key ID、签名域和 Proto payload，失败时不返回部分配置。
func Verify(signed *cloudv1.SignedEdgeDesiredConfig, expectedKeyID string, publicKey ed25519.PublicKey) (*cloudv1.EdgeDesiredConfig, error) {
	if signed == nil || signed.GetKeyId() != expectedKeyID || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("desired config key is invalid")
	}
	if !ed25519.Verify(publicKey, append([]byte(domain), signed.GetPayload()...), signed.GetSignature()) {
		return nil, errors.New("desired config signature is invalid")
	}
	config := &cloudv1.EdgeDesiredConfig{}
	if err := proto.Unmarshal(signed.GetPayload(), config); err != nil {
		return nil, fmt.Errorf("decode desired config: %w", err)
	}
	return config, nil
}
