package remoteauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

// ClientAccessIdentity 是客户端针对单个 Endpoint 持有的长期 Ed25519 访问身份。
// PrivateKey 的 truth 只存在于当前平台 secure credential store；Fingerprint 被 owning daemon 写入 CapabilityGrant v2，
// 同一客户端访问不同 Endpoint 时必须使用不同 key，不能复用 Cloud installation identity。
type ClientAccessIdentity struct {
	EndpointID  string
	Fingerprint string
	PublicKey   ed25519.PublicKey
	PrivateKey  ed25519.PrivateKey
}

// Validate 校验 Endpoint 绑定、Ed25519 key pair 与 fingerprint 是否一致。
// 失败表示客户端 secure store 损坏或调用方把其他 Endpoint 的 key 混入当前连接，调用方必须停止授权且不能生成临时替代 key。
func (identity ClientAccessIdentity) Validate() error {
	if strings.TrimSpace(identity.EndpointID) == "" || len(identity.PublicKey) != ed25519.PublicKeySize || len(identity.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("incomplete ClientAccessIdentity")
	}
	if Fingerprint(identity.PublicKey) != strings.TrimSpace(identity.Fingerprint) || !identity.PublicKey.Equal(identity.PrivateKey.Public()) {
		return fmt.Errorf("ClientAccessIdentity key and fingerprint mismatch")
	}
	return nil
}

// NewClientAccessIdentity 从平台 secure store 已持有的私钥构造一个 Endpoint 专用访问身份。
// 该函数复制 key material；返回值只能留在公开客户端进程并用于 PairingExchange 或 capability channel-bound signature。
func NewClientAccessIdentity(endpointID string, privateKey ed25519.PrivateKey) (ClientAccessIdentity, error) {
	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		return ClientAccessIdentity{}, fmt.Errorf("ClientAccessIdentity requires endpoint_id")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return ClientAccessIdentity{}, fmt.Errorf("ClientAccessIdentity requires ed25519 private key")
	}
	publicKey := append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
	identity := ClientAccessIdentity{
		EndpointID:  endpointID,
		Fingerprint: Fingerprint(publicKey),
		PublicKey:   publicKey,
		PrivateKey:  append(ed25519.PrivateKey(nil), privateKey...),
	}
	return identity, identity.Validate()
}

// GenerateClientAccessIdentity 使用调用方提供的 CSPRNG 生成 Endpoint 专用访问身份。
// reader 仅允许测试注入；生产 nil 使用 crypto/rand，生成失败时不得回退到 daemon key、Cloud identity 或其他 Endpoint 的 key。
func GenerateClientAccessIdentity(endpointID string, reader io.Reader) (ClientAccessIdentity, error) {
	if reader == nil {
		reader = rand.Reader
	}
	_, privateKey, err := ed25519.GenerateKey(reader)
	if err != nil {
		return ClientAccessIdentity{}, fmt.Errorf("generate ClientAccessIdentity: %w", err)
	}
	return NewClientAccessIdentity(endpointID, privateKey)
}
