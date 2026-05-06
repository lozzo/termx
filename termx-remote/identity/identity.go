package identity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const filename = "identity.json"

type DeviceIdentity struct {
	DeviceID    string    `json:"device_id"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func LoadOrCreate(dir, displayName string) (DeviceIdentity, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return DeviceIdentity{}, fmt.Errorf("create remote data dir: %w", err)
	}
	path := filepath.Join(dir, filename)
	if data, err := os.ReadFile(path); err == nil {
		var ident DeviceIdentity
		if err := json.Unmarshal(data, &ident); err != nil {
			return DeviceIdentity{}, fmt.Errorf("decode identity: %w", err)
		}
		if ident.DeviceID == "" {
			return DeviceIdentity{}, fmt.Errorf("identity file %s is missing device_id", path)
		}
		if name := strings.TrimSpace(displayName); name != "" && ident.DisplayName != name {
			ident.DisplayName = name
			ident.UpdatedAt = time.Now().UTC()
			if err := persist(path, ident); err != nil {
				return DeviceIdentity{}, err
			}
		}
		return ident, nil
	} else if !os.IsNotExist(err) {
		return DeviceIdentity{}, fmt.Errorf("read identity: %w", err)
	}

	now := time.Now().UTC()
	ident := DeviceIdentity{
		DeviceID:    newDeviceID(),
		DisplayName: fallbackDisplayName(displayName),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := persist(path, ident); err != nil {
		return DeviceIdentity{}, err
	}
	return ident, nil
}

func fallbackDisplayName(displayName string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "termx-device"
}

func newDeviceID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("device-%d", time.Now().UnixNano())
	}
	return "device-" + hex.EncodeToString(raw[:])
}

func persist(path string, ident DeviceIdentity) error {
	payload, err := json.MarshalIndent(ident, "", "  ")
	if err != nil {
		return fmt.Errorf("encode identity: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(payload, '\n'), 0o600); err != nil {
		return fmt.Errorf("write identity temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace identity file: %w", err)
	}
	return nil
}
