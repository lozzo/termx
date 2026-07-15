package devcloud

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
)

type authorityRecord struct {
	KeyID      string    `json:"key_id"`
	PrivateKey []byte    `json:"private_key"`
	NotBefore  time.Time `json:"not_before"`
	NotAfter   time.Time `json:"not_after"`
}

// loadOrCreateAuthority 恢复 Control Plane 唯一签名 authority，或以 0600 原子文件首次创建。
// 私钥只返回给 issuer 装配；Hub 仅取得由 Signer 导出的公钥记录。
func loadOrCreateAuthority(path string, random io.Reader, now time.Time) (servicecredential.Signer, error) {
	if path == "" {
		_, privateKey, err := ed25519.GenerateKey(random)
		if err != nil {
			return servicecredential.Signer{}, fmt.Errorf("generate ephemeral Control Plane authority: %w", err)
		}
		defer clear(privateKey)
		return servicecredential.NewSigner("dev-admission-key", privateKey, now.Add(-time.Hour), now.Add(24*time.Hour))
	}
	data, err := os.ReadFile(path)
	if err == nil {
		defer clear(data)
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return servicecredential.Signer{}, fmt.Errorf("Control Plane authority file permissions are invalid")
		}
		var record authorityRecord
		if json.Unmarshal(data, &record) != nil || record.KeyID == "" || len(record.PrivateKey) != ed25519.PrivateKeySize || record.NotBefore.IsZero() || record.NotAfter.IsZero() {
			return servicecredential.Signer{}, fmt.Errorf("Control Plane authority file is invalid")
		}
		defer clear(record.PrivateKey)
		return servicecredential.NewSigner(record.KeyID, ed25519.PrivateKey(record.PrivateKey), record.NotBefore, record.NotAfter)
	}
	if !os.IsNotExist(err) {
		return servicecredential.Signer{}, fmt.Errorf("read Control Plane authority: %w", err)
	}
	_, privateKey, err := ed25519.GenerateKey(random)
	if err != nil {
		return servicecredential.Signer{}, fmt.Errorf("generate Control Plane authority: %w", err)
	}
	defer clear(privateKey)
	record := authorityRecord{KeyID: "dev-admission-key", PrivateKey: append([]byte(nil), privateKey...), NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)}
	defer clear(record.PrivateKey)
	if err := writeAuthority(path, record); err != nil {
		return servicecredential.Signer{}, err
	}
	return servicecredential.NewSigner(record.KeyID, privateKey, record.NotBefore, record.NotAfter)
}

func writeAuthority(path string, record authorityRecord) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Control Plane authority directory: %w", err)
	}
	file, err := os.CreateTemp(directory, ".authority-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	committed = true
	return nil
}
