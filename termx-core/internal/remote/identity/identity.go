package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const filename = "identity.json"

const MachineKeyFilename = "machine_key"

type MachineKey struct {
	PublicKey   ed25519.PublicKey `json:"public_key"`
	privateKey  ed25519.PrivateKey
	privateSeed []byte
}

type machineKeyFile struct {
	Version        int    `json:"version"`
	Algorithm      string `json:"algorithm"`
	PrivateKeySeed string `json:"private_key_seed"`
}

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

func LoadOrCreateMachineKey(dir string) (MachineKey, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return MachineKey{}, fmt.Errorf("create remote data dir: %w", err)
	}
	path := filepath.Join(dir, MachineKeyFilename)
	if data, err := os.ReadFile(path); err == nil {
		key, decodeErr := decodeMachineKey(data)
		if decodeErr != nil {
			return MachineKey{}, fmt.Errorf("decode machine key: %w", decodeErr)
		}
		if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
			return MachineKey{}, fmt.Errorf("set machine key permissions: %w", chmodErr)
		}
		return key, nil
	} else if !os.IsNotExist(err) {
		return MachineKey{}, fmt.Errorf("read machine key: %w", err)
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return MachineKey{}, fmt.Errorf("generate machine key: %w", err)
	}
	key := MachineKey{
		PublicKey:   append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...),
		privateKey:  append(ed25519.PrivateKey(nil), privateKey...),
		privateSeed: append([]byte(nil), privateKey.Seed()...),
	}
	if err := persistMachineKey(path, key); err != nil {
		if errors.Is(err, os.ErrExist) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return MachineKey{}, fmt.Errorf("read concurrently created machine key: %w", readErr)
			}
			key, decodeErr := decodeMachineKey(data)
			if decodeErr != nil {
				return MachineKey{}, fmt.Errorf("decode concurrently created machine key: %w", decodeErr)
			}
			if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
				return MachineKey{}, fmt.Errorf("set machine key permissions: %w", chmodErr)
			}
			return key, nil
		}
		return MachineKey{}, err
	}
	return key, nil
}

func MachinePublicKeyFingerprint(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (k MachineKey) Sign(message []byte) []byte {
	if len(k.privateKey) != ed25519.PrivateKeySize {
		return nil
	}
	return ed25519.Sign(k.privateKey, message)
}

func (k MachineKey) String() string {
	return fmt.Sprintf("MachineKey{%s}", MachinePublicKeyFingerprint(k.PublicKey))
}

func (k MachineKey) GoString() string {
	return k.String()
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

func decodeMachineKey(data []byte) (MachineKey, error) {
	var stored machineKeyFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return MachineKey{}, err
	}
	if stored.Version != 1 {
		return MachineKey{}, fmt.Errorf("unsupported machine key version %d", stored.Version)
	}
	if stored.Algorithm != "ed25519" {
		return MachineKey{}, fmt.Errorf("unsupported machine key algorithm %q", stored.Algorithm)
	}
	seed, err := base64.StdEncoding.DecodeString(stored.PrivateKeySeed)
	if err != nil {
		return MachineKey{}, fmt.Errorf("decode private key seed: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return MachineKey{}, fmt.Errorf("private key seed has size %d, want %d", len(seed), ed25519.SeedSize)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	return MachineKey{
		PublicKey:   append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...),
		privateKey:  append(ed25519.PrivateKey(nil), privateKey...),
		privateSeed: append([]byte(nil), seed...),
	}, nil
}

func persistMachineKey(path string, key MachineKey) error {
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("private key has size %d, want %d", len(key.privateKey), ed25519.PrivateKeySize)
	}
	seed := key.privateSeed
	if len(seed) == 0 {
		seed = key.privateKey.Seed()
	}
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("private key seed has size %d, want %d", len(seed), ed25519.SeedSize)
	}
	stored := machineKeyFile{
		Version:        1,
		Algorithm:      "ed25519",
		PrivateKeySeed: base64.StdEncoding.EncodeToString(seed),
	}
	payload, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine key: %w", err)
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create machine key temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	_, writeErr := tmpFile.Write(append(payload, '\n'))
	chmodErr := tmpFile.Chmod(0o600)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write machine key file: %w", writeErr)
	}
	if chmodErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("set machine key temp permissions: %w", chmodErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close machine key file: %w", closeErr)
	}
	if err := os.Link(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("install machine key file: %w", err)
	}
	_ = os.Remove(tmpPath)
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set machine key permissions: %w", err)
	}
	return nil
}
