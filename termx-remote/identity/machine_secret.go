package identity

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MachineSecretFilename = "machine_secret"

func LoadOrCreateMachineSecret(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dir, MachineSecretFilename)
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("machine secret wrong length %d", len(data))
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("set machine secret permissions: %w", err)
		}
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read machine secret: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	if err := persistSecret(path, secret); err != nil {
		if errors.Is(err, os.ErrExist) {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil, fmt.Errorf("read concurrently created machine secret: %w", readErr)
			}
			if len(data) != 32 {
				return nil, fmt.Errorf("machine secret wrong length %d", len(data))
			}
			if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
				return nil, fmt.Errorf("set machine secret permissions: %w", chmodErr)
			}
			return data, nil
		}
		return nil, err
	}
	return secret, nil
}

func persistSecret(path string, secret []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	_, writeErr := f.Write(secret)
	chmodErr := f.Chmod(0o600)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write: %w", writeErr)
	}
	if chmodErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("chmod: %w", chmodErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close: %w", closeErr)
	}
	if err := os.Link(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if os.IsExist(err) {
			return fmt.Errorf("%w", os.ErrExist)
		}
		return fmt.Errorf("link: %w", err)
	}
	_ = os.Remove(tmp)
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod installed secret: %w", err)
	}
	return nil
}
