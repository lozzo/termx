// Package keymaterial 加载权限受控的 Cloud 签名密钥文件。
// 它不生成生产密钥，也不把私钥写入日志或响应。
package keymaterial

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

// LoadEd25519PrivateKey 读取 PKCS#8 PEM 并要求严格的 Ed25519 private key。
func LoadEd25519PrivateKey(path string) (ed25519.PrivateKey, error) {
	payload, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("read Ed25519 private key: %w", err)
	}
	block, _ := pem.Decode(payload)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, errors.New("Ed25519 private key must be PKCS#8 PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("private key is not Ed25519")
	}
	return append(ed25519.PrivateKey(nil), key...), nil
}
