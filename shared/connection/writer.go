package connection

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Encode 把规范化 connection registry 编码为稳定 YAML 子集。
// 输出只包含客户端 endpoint 期望状态；raw grant、账号 token、Hub assignment 和运行时连接状态都不得进入该文件。
func Encode(registry Registry) ([]byte, error) {
	normalized, err := registry.Normalize()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, "version: %d\n", normalized.Version)
	fmt.Fprintf(&out, "default: %s\n", quoteRegistryScalar(string(normalized.Default)))
	out.WriteString("connections:\n")
	for _, cfg := range normalized.List() {
		fmt.Fprintf(&out, "  %s:\n", cfg.ID)
		writeRegistryString(&out, "label", cfg.Label)
		fmt.Fprintf(&out, "    enabled: %t\n", cfg.Enabled)
		writeRegistryString(&out, "transport", string(cfg.Transport))
		writeRegistryString(&out, "connect_mode", string(cfg.ConnectMode))
		switch cfg.Transport {
		case TransportLocal:
			writeRegistryString(&out, "socket", cfg.Socket)
		case TransportSSH:
			writeRegistryString(&out, "address", cfg.Address)
			writeRegistryOptionalString(&out, "auth_ref", cfg.AuthRef)
			writeRegistryString(&out, "remote_socket", cfg.RemoteSocket)
		case TransportHubP2P:
			writeRegistryString(&out, "hub_device_id", cfg.HubDeviceID)
			writeRegistryString(&out, "device_fingerprint", cfg.DeviceFingerprint)
			writeRegistryString(&out, "grant_ref", cfg.GrantRef)
			writeRegistryString(&out, "relay_mode", string(cfg.RelayMode))
		}
	}
	return out.Bytes(), nil
}

// Save 原子写入 connection registry；空 path 使用 DefaultPath。
// 文件固定为 0600，写入失败保留旧 registry；该操作不触碰 credential store，因此调用方必须先完成 raw grant 的安全落盘。
func Save(path string, registry Registry) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultPath()
	}
	payload, err := Encode(registry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create connection registry directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".connections-*.yaml")
	if err != nil {
		return fmt.Errorf("create connection registry temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("secure connection registry temp file: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		return fmt.Errorf("write connection registry: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync connection registry: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close connection registry: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish connection registry: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure connection registry: %w", err)
	}
	committed = true
	return nil
}

func writeRegistryString(out *bytes.Buffer, key, value string) {
	fmt.Fprintf(out, "    %s: %s\n", key, quoteRegistryScalar(value))
}

func writeRegistryOptionalString(out *bytes.Buffer, key, value string) {
	if strings.TrimSpace(value) != "" {
		writeRegistryString(out, key, value)
	}
}

func quoteRegistryScalar(value string) string {
	return strconv.Quote(strings.TrimSpace(value))
}
