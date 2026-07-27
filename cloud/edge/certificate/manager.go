// Package certificate 拥有 Edge 本机 applied 证书状态和 TLS 热加载。
// 单一状态文件是重启真值，Controller desired state 通过 EdgeControl 对账。
package certificate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/cloud/securetransport"
	cloudv1 "github.com/muxvia/muxvia/proto/cloud/v1"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrStaleRevision 表示同一档案的旧 revision 晚于新 revision 到达；本机证书真值不能降序。
	ErrStaleRevision = errors.New("certificate profile revision is stale")
)

// Config 是 Edge 本地证书 owner 的身份和单文件持久化路径。
type Config struct {
	EdgeID    string
	StateFile string
	Now       func() time.Time
}

// Manager 校验并原子提交证书包；它不拥有 Controller desired state。
type Manager struct {
	config  Config
	loader  *securetransport.ReloadableCertificate
	mu      sync.RWMutex
	current *cloudv1.EdgeCertificateBundle
}

// New 创建 manager，并在存在 managed state 时先校验再恢复内存 TLS 证书。
func New(config Config, loader *securetransport.ReloadableCertificate) (*Manager, error) {
	config.EdgeID = strings.TrimSpace(config.EdgeID)
	config.StateFile = filepath.Clean(strings.TrimSpace(config.StateFile))
	if config.StateFile != "." && !filepath.IsAbs(config.StateFile) {
		absolute, err := filepath.Abs(config.StateFile)
		if err != nil {
			return nil, err
		}
		config.StateFile = absolute
	}
	if config.EdgeID == "" || config.StateFile == "." || loader == nil {
		return nil, errors.New("Edge ID, absolute certificate state file, and TLS loader are required")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	manager := &Manager{config: config, loader: loader}
	payload, err := os.ReadFile(config.StateFile)
	if errors.Is(err, os.ErrNotExist) {
		return manager, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read managed Edge certificate: %w", err)
	}
	bundle := &cloudv1.EdgeCertificateBundle{}
	if err := proto.Unmarshal(payload, bundle); err != nil {
		return nil, fmt.Errorf("decode managed Edge certificate: %w", err)
	}
	validated, err := manager.validate(bundle, false)
	if err != nil {
		return nil, fmt.Errorf("validate managed Edge certificate: %w", err)
	}
	manager.loader.Replace(validated.Certificate)
	manager.current = proto.Clone(bundle).(*cloudv1.EdgeCertificateBundle)
	return manager, nil
}

// Current 返回 EdgeHello 使用的 applied profile/revision，不暴露证书材料。
func (manager *Manager) Current() (string, uint64) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.current == nil {
		return "", 0
	}
	return manager.current.GetCertificateProfileId(), manager.current.GetRevision()
}

// Apply 先完整校验，再原子持久化单文件 bundle，最后切换内存 TLS loader。
// 任一步失败都不会修改当前 loader 或 applied revision。
func (manager *Manager) Apply(ctx context.Context, bundle *cloudv1.EdgeCertificateBundle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	validated, err := manager.validate(bundle, true)
	if err != nil {
		return err
	}
	if manager.current != nil && manager.current.GetCertificateProfileId() == bundle.GetCertificateProfileId() && manager.current.GetRevision() > bundle.GetRevision() {
		return ErrStaleRevision
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(bundle)
	if err != nil {
		return fmt.Errorf("encode managed Edge certificate: %w", err)
	}
	if err := atomicWrite(manager.config.StateFile, payload); err != nil {
		return fmt.Errorf("persist managed Edge certificate: %w", err)
	}
	manager.loader.Replace(validated.Certificate)
	manager.current = proto.Clone(bundle).(*cloudv1.EdgeCertificateBundle)
	return nil
}

func (manager *Manager) validate(bundle *cloudv1.EdgeCertificateBundle, requireCurrent bool) (*securetransport.ValidatedServerPair, error) {
	if bundle == nil || bundle.GetTargetEdgeId() != manager.config.EdgeID || bundle.GetCertificateProfileId() == "" || bundle.GetRevision() == 0 || strings.TrimSpace(bundle.GetPublicEndpoint()) == "" {
		return nil, errors.New("certificate bundle identity, profile, revision, and endpoint are required")
	}
	now := time.Time{}
	if requireCurrent {
		now = manager.config.Now()
	}
	return securetransport.ValidateServerPair(bundle.GetCertificateChainPem(), bundle.GetPrivateKeyPem(), bundle.GetPublicEndpoint(), now)
}

func atomicWrite(path string, payload []byte) error {
	directoryPath := filepath.Dir(path)
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directoryPath, ".managed-certificate-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directory, err := os.Open(directoryPath)
	if err != nil {
		return err
	}
	err = directory.Sync()
	if closeErr := directory.Close(); err == nil {
		err = closeErr
	}
	return err
}
