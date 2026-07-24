package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"github.com/muxvia/muxvia/shared/cloudcompanion/activation"
	"github.com/muxvia/muxvia/shared/cloudcompanion/installer"
)

var v3CloudCompanionRootDir = installer.DefaultRootDir

type v3BundledDevelopmentSource struct {
	installation installer.Installation
	payload      []byte
	rootDir      string
}

// v3BundledDevelopmentCompanionSource 只装配显式 development 单文件构建内嵌的 Companion。
// 默认源码构建没有 artifact 和 linker metadata，因此仍由 signed installer 拥有安装真值。
func v3BundledDevelopmentCompanionSource() (activation.InstallationSource, bool, error) {
	digest := strings.ToLower(strings.TrimSpace(cloudDevelopmentCompanionSHA256))
	version := strings.TrimSpace(cloudDevelopmentCompanionVersion)
	channel := strings.TrimSpace(cloudDevelopmentCompanionChannel)
	payload := cloudDevelopmentCompanionEmbedded
	configured := len(payload) != 0 || digest != "" || version != "" || channel != ""
	if !configured {
		return nil, false, nil
	}
	if muxviaBuildVersion != v3DevelopmentBuildVersion || len(payload) == 0 || digest == "" || version == "" || channel != "development" {
		return nil, false, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "embedded development Cloud Companion metadata is invalid")
	}
	decodedDigest, err := hex.DecodeString(digest)
	if err != nil || len(decodedDigest) != sha256.Size {
		return nil, false, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "embedded development Cloud Companion digest is invalid")
	}
	payloadDigest := sha256.Sum256(payload)
	if !strings.EqualFold(hex.EncodeToString(payloadDigest[:]), digest) {
		return nil, false, companionDevelopmentTrustError("embedded development Cloud Companion does not match build metadata")
	}
	rootDir := strings.TrimSpace(v3CloudCompanionRootDir())
	if !filepath.IsAbs(rootDir) {
		return nil, false, companionDevelopmentTrustError("embedded development Cloud Companion root is invalid")
	}
	path := filepath.Join(rootDir, "bundled", digest, installer.ExecutableName())
	return &v3BundledDevelopmentSource{installation: installer.Installation{
		Version: version, Channel: channel, OS: runtime.GOOS, Arch: runtime.GOARCH,
		BinaryPath: path, BinarySHA256: digest,
	}, payload: payload, rootDir: rootDir}, true, nil
}

// Status 原子释放内嵌 artifact，并在每次 activation 前重新验证路径、owner/mode 和构建期摘要。
// 已存在但被篡改的文件 fail closed，不从 sibling、环境变量或任意 executable path 恢复。
func (source *v3BundledDevelopmentSource) Status() (installer.Installation, error) {
	if source == nil {
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion source is missing")
	}
	if err := source.materialize(); err != nil {
		return installer.Installation{}, err
	}
	if err := source.validateInstalled(); err != nil {
		return installer.Installation{}, err
	}
	return source.installation, nil
}

func (source *v3BundledDevelopmentSource) materialize() error {
	if _, err := os.Lstat(source.installation.BinaryPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be inspected")
	}
	targetDir := filepath.Dir(source.installation.BinaryPath)
	for _, directory := range []string{source.rootDir, filepath.Join(source.rootDir, "bundled"), targetDir} {
		if err := ensureV3DevelopmentCompanionDirectory(directory); err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(targetDir, ".muxvia-cloud-*")
	if err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion temporary file cannot be created")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o700); err != nil {
		_ = temporary.Close()
		return companionDevelopmentTrustError("embedded development Cloud Companion permissions cannot be set")
	}
	if _, err := temporary.Write(source.payload); err != nil {
		_ = temporary.Close()
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be written")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be synced")
	}
	if err := temporary.Close(); err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be closed")
	}
	if err := os.Rename(temporaryPath, source.installation.BinaryPath); err != nil {
		// 另一进程可能已发布相同 artifact；只有完整复验成功才能接受并发结果。
		if validateErr := source.validateInstalled(); validateErr == nil {
			return nil
		}
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be published")
	}
	return nil
}

func (source *v3BundledDevelopmentSource) validateInstalled() error {
	info, err := os.Lstat(source.installation.BinaryPath)
	if err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be inspected")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return companionDevelopmentTrustError("embedded development Cloud Companion is not a regular file")
	}
	if err := validateV3DevelopmentCompanionFile(info); err != nil {
		return err
	}
	file, err := os.Open(source.installation.BinaryPath)
	if err != nil {
		return companionDevelopmentTrustError("embedded development Cloud Companion cannot be read")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), source.installation.BinarySHA256) {
		return companionDevelopmentTrustError("embedded development Cloud Companion does not match the muxvia build")
	}
	return nil
}

func companionDevelopmentTrustError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, message)
}

func invalidDevelopmentCompanionMode(mode os.FileMode) error {
	return companionDevelopmentTrustError(fmt.Sprintf("development Cloud Companion permissions are unsafe (%#o)", mode.Perm()))
}
