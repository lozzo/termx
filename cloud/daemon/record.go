// Package daemon 实现 daemon 进程内唯一 AnyTTY Cloud owner。
package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const recordVersion = 1

// EnrollmentRecord 是 daemon 唯一允许持久化的 Cloud 状态。
// 它不包含 Edge assignment、AgentTicket、Presence、session 或任何私钥。
type EnrollmentRecord struct {
	Version              int       `json:"version"`
	DaemonID             string    `json:"daemon_id"`
	AccountID            string    `json:"account_id"`
	ControllerAddress    string    `json:"controller_address"`
	ControllerServerName string    `json:"controller_server_name"`
	EnrolledAt           time.Time `json:"enrolled_at"`
}

// Validate 校验本地 enrollment record 的稳定发现字段。
func (record EnrollmentRecord) Validate() error {
	if record.Version != recordVersion || strings.TrimSpace(record.DaemonID) == "" || strings.TrimSpace(record.AccountID) == "" ||
		strings.TrimSpace(record.ControllerAddress) == "" || strings.TrimSpace(record.ControllerServerName) == "" || record.EnrolledAt.IsZero() {
		return errors.New("Cloud enrollment record is incomplete or unsupported")
	}
	return nil
}

// LoadRecord 严格加载 owner-only JSON；文件不存在表示 daemon 尚未启用 Cloud。
func LoadRecord(path string) (EnrollmentRecord, error) {
	payload, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return EnrollmentRecord{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var record EnrollmentRecord
	if err := decoder.Decode(&record); err != nil {
		return EnrollmentRecord{}, fmt.Errorf("decode Cloud enrollment record: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return EnrollmentRecord{}, errors.New("Cloud enrollment record has trailing data")
	}
	if err := record.Validate(); err != nil {
		return EnrollmentRecord{}, err
	}
	return record, nil
}

// SaveRecord 原子写入 0600 enrollment record，不触及 DeviceIdentity 私钥文件。
func SaveRecord(path string, record EnrollmentRecord) error {
	record.Version = recordVersion
	if err := record.Validate(); err != nil {
		return err
	}
	directory := filepath.Dir(filepath.Clean(path))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temp, err := os.CreateTemp(directory, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
