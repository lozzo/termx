package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/activation"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
)

var v3ExecutablePath = os.Executable

type v3BundledDevelopmentSource struct {
	installation installer.Installation
}

// v3BundledDevelopmentCompanionSource 只装配显式 development 构建固化的同目录 Companion 身份。
// 默认/正式构建没有这些 linker metadata，因此仍由 signed installer 拥有安装真值。
func v3BundledDevelopmentCompanionSource() (activation.InstallationSource, bool, error) {
	name := strings.TrimSpace(cloudDevelopmentCompanionName)
	digest := strings.ToLower(strings.TrimSpace(cloudDevelopmentCompanionSHA256))
	version := strings.TrimSpace(cloudDevelopmentCompanionVersion)
	channel := strings.TrimSpace(cloudDevelopmentCompanionChannel)
	configured := name != "" || digest != "" || version != "" || channel != ""
	if !configured {
		return nil, false, nil
	}
	if termxBuildVersion != v3DevelopmentBuildVersion || name == "" || filepath.Base(name) != name || name == "." || digest == "" || version == "" || channel != "development" {
		return nil, false, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "embedded development Cloud Companion metadata is invalid")
	}
	decodedDigest, err := hex.DecodeString(digest)
	if err != nil || len(decodedDigest) != sha256.Size {
		return nil, false, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, "embedded development Cloud Companion digest is invalid")
	}
	executable, err := v3ExecutablePath()
	if err != nil {
		return nil, false, companionDevelopmentTrustError("termx executable path cannot be resolved")
	}
	realExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, false, companionDevelopmentTrustError("termx executable path cannot be verified")
	}
	path := filepath.Join(filepath.Dir(realExecutable), name)
	return &v3BundledDevelopmentSource{installation: installer.Installation{
		Version: version, Channel: channel, OS: runtime.GOOS, Arch: runtime.GOARCH,
		BinaryPath: path, BinarySHA256: digest,
	}}, true, nil
}

// Status 在每次 activation 前重新验证路径、owner/mode 和构建期摘要；任何变化都 fail closed。
// 这里不把 sibling existence 当成信任来源，也不读取环境变量、runtime manifest 或任意 executable path。
func (source *v3BundledDevelopmentSource) Status() (installer.Installation, error) {
	if source == nil {
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion source is missing")
	}
	info, err := os.Lstat(source.installation.BinaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return installer.Installation{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "development Cloud Companion is missing beside termx; rebuild with `make build-cloud-test`")
		}
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion cannot be inspected")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion is not a regular file")
	}
	if err := validateV3DevelopmentCompanionFile(info); err != nil {
		return installer.Installation{}, err
	}
	file, err := os.Open(source.installation.BinaryPath)
	if err != nil {
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion cannot be read")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), source.installation.BinarySHA256) {
		return installer.Installation{}, companionDevelopmentTrustError("development Cloud Companion does not match the termx build")
	}
	return source.installation, nil
}

func companionDevelopmentTrustError(message string) error {
	return cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED, message)
}

func invalidDevelopmentCompanionMode(mode os.FileMode) error {
	return companionDevelopmentTrustError(fmt.Sprintf("development Cloud Companion permissions are unsafe (%#o)", mode.Perm()))
}
