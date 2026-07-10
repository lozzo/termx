package remoteauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const identityPrivateKeyFile = "remote_device_identity.json"

// Identity 是 remote daemon 的长期 Ed25519 设备身份。
// DeviceID 只用于 Hub 发现，Fingerprint 才是客户端配置中的安全身份，PrivateKey 只能留在签发 daemon 本地。
type Identity struct {
	DeviceID    string
	Fingerprint string
	PublicKey   ed25519.PublicKey
	PrivateKey  ed25519.PrivateKey
}

type storedIdentity struct {
	Version    int       `json:"version"`
	DeviceID   string    `json:"device_id"`
	PrivateKey string    `json:"private_key"`
	CreatedAt  time.Time `json:"created_at"`
}

// LoadOrCreateIdentity 加载或创建 daemon-local 设备身份。
// 已有 identity 的 DeviceID 不允许被调用方静默替换；私钥文件固定为 0600，避免展示名或 Hub 路由变化升级成信任迁移。
func LoadOrCreateIdentity(dir string, deviceID string) (Identity, error) {
	dir = strings.TrimSpace(dir)
	deviceID = strings.TrimSpace(deviceID)
	if dir == "" {
		return Identity{}, fmt.Errorf("remote identity requires storage directory")
	}
	if deviceID == "" {
		return Identity{}, fmt.Errorf("remote identity requires device_id")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, fmt.Errorf("create remote identity directory: %w", err)
	}
	path := filepath.Join(dir, identityPrivateKeyFile)
	data, err := os.ReadFile(path)
	if err == nil {
		var stored storedIdentity
		if err := json.Unmarshal(data, &stored); err != nil {
			return Identity{}, fmt.Errorf("decode remote identity: %w", err)
		}
		if strings.TrimSpace(stored.DeviceID) != deviceID {
			return Identity{}, fmt.Errorf("remote identity device_id mismatch: stored %q requested %q", stored.DeviceID, deviceID)
		}
		privateKey, err := base64.RawURLEncoding.DecodeString(stored.PrivateKey)
		if err != nil || len(privateKey) != ed25519.PrivateKeySize {
			return Identity{}, fmt.Errorf("remote identity has invalid private key")
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return Identity{}, fmt.Errorf("secure remote identity permissions: %w", err)
		}
		return identityFromPrivateKey(deviceID, ed25519.PrivateKey(privateKey)), nil
	}
	if !os.IsNotExist(err) {
		return Identity{}, fmt.Errorf("read remote identity: %w", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generate remote identity: %w", err)
	}
	stored := storedIdentity{Version: 1, DeviceID: deviceID, PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey), CreatedAt: time.Now().UTC()}
	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return Identity{}, fmt.Errorf("encode remote identity: %w", err)
	}
	if err := writePrivateFile(path, append(payload, '\n')); err != nil {
		return Identity{}, err
	}
	return identityFromPrivateKey(deviceID, privateKey), nil
}

func identityFromPrivateKey(deviceID string, privateKey ed25519.PrivateKey) Identity {
	publicKey := append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
	return Identity{
		DeviceID:    deviceID,
		Fingerprint: Fingerprint(publicKey),
		PublicKey:   publicKey,
		PrivateKey:  append(ed25519.PrivateKey(nil), privateKey...),
	}
}

func writePrivateFile(path string, payload []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create remote identity temp file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure remote identity temp file: %w", err)
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write remote identity: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close remote identity: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("install remote identity: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure remote identity: %w", err)
	}
	return nil
}
