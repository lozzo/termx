package installer

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"golang.org/x/mod/semver"
)

const activeSchemaVersion = 1

const maxBinaryBytes int64 = 256 << 20

// SmokeFunc 在 staging binary 上验证 executable 可启动并完成 Companion Hello。
// 失败必须阻止 active version 切换；函数不得把任意 executable path 保存为运行时配置。
type SmokeFunc func(context.Context, string, Manifest) error

// Config 固定 installer root、官方 HTTPS origin、平台、release roots 与 handshake smoke。
// RootDir、Origin 和 TrustedKeys 都由 official/public build 装配，不能来自 Hub、endpoint 或 manifest。
type Config struct {
	RootDir     string
	Origin      string
	OS          string
	Arch        string
	TrustedKeys map[string]ed25519.PublicKey
	HTTPClient  *http.Client
	Now         func() time.Time
	Smoke       SmokeFunc
}

// Request 选择 stable/beta channel 和可选精确版本。
// Version 为空表示请求官方 latest manifest；downgrade 仍必须由目标签名 manifest 明确授权。
type Request struct {
	Channel string
	Version string
}

// Installation 是 installer 验证后的 active Companion 状态。
// BinaryPath 始终位于固定 RootDir/version namespace，SHA256 是提取后 executable bytes 的摘要。
type Installation struct {
	Version      string    `json:"version"`
	Channel      string    `json:"channel"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	BinaryPath   string    `json:"binary_path"`
	BinarySHA256 string    `json:"binary_sha256"`
	InstalledAt  time.Time `json:"installed_at"`
}

// Installer 验证、提取、smoke 并原子激活官方 Cloud Companion artifact。
// 它不管理 account/device session；uninstall 只删除 artifact，purge 必须先通过 lifecycle Logout 显式执行。
type Installer struct {
	root        string
	origin      *url.URL
	os          string
	arch        string
	trustedKeys map[string]ed25519.PublicKey
	httpClient  *http.Client
	now         func() time.Time
	smoke       SmokeFunc
}

type activeRecord struct {
	SchemaVersion uint32 `json:"schema_version"`
	Installation
}

// New 创建 fail-closed Companion installer。
// Origin 必须是无 userinfo/query/fragment 的 HTTPS base URL；Smoke 和至少一个 Ed25519 root 必须存在。
func New(config Config) (*Installer, error) {
	root := strings.TrimSpace(config.RootDir)
	if root == "" {
		root = DefaultRootDir()
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return nil, fmt.Errorf("Cloud Companion install root must be an absolute non-root path")
	}
	origin, err := url.Parse(strings.TrimSpace(config.Origin))
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, fmt.Errorf("Cloud Companion release origin must be a fixed HTTPS URL")
	}
	if config.OS == "" {
		config.OS = runtime.GOOS
	}
	if config.Arch == "" {
		config.Arch = runtime.GOARCH
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Smoke == nil || len(config.TrustedKeys) == 0 {
		return nil, fmt.Errorf("Cloud Companion release roots and handshake smoke are required")
	}
	keys := make(map[string]ed25519.PublicKey, len(config.TrustedKeys))
	for keyID, publicKey := range config.TrustedKeys {
		if keyID == "" || len(publicKey) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("invalid Cloud Companion release root")
		}
		keys[keyID] = append(ed25519.PublicKey(nil), publicKey...)
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	httpClient := *config.HTTPClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Installer{root: filepath.Clean(root), origin: origin, os: config.OS, arch: config.Arch, trustedKeys: keys, httpClient: &httpClient, now: config.Now, smoke: config.Smoke}, nil
}

// InstallRelease 从固定 official origin 获取 manifest 和 archive，再进入同一签名/原子安装链路。
// redirect 后的最终 URL 仍必须位于固定 origin；非 200、超限响应和 Content-Length mismatch 全部 fail closed。
func (installer *Installer) InstallRelease(ctx context.Context, request Request) (Installation, error) {
	manifestURL, err := installer.manifestURL(request)
	if err != nil {
		return Installation{}, err
	}
	manifestPayload, err := installer.fetch(ctx, manifestURL, 1<<20)
	if err != nil {
		return Installation{}, err
	}
	manifest, err := ParseAndVerifyManifest(manifestPayload, installer.verification(request.Channel))
	if err != nil {
		return Installation{}, err
	}
	downloadURL, err := url.Parse(manifest.DownloadURL)
	if err != nil || !installer.allowedURL(downloadURL) {
		return Installation{}, untrustedError("Cloud Companion archive URL is outside the official origin")
	}
	response, err := installer.get(ctx, downloadURL)
	if err != nil {
		return Installation{}, err
	}
	defer response.Body.Close()
	if response.ContentLength >= 0 && response.ContentLength != manifest.ArchiveSize {
		return Installation{}, untrustedError("Cloud Companion archive Content-Length does not match its manifest")
	}
	return installer.installVerified(ctx, manifest, response.Body)
}

// InstallArchive 验证调用方提供的 signed manifest 与 archive bytes，并执行同一原子安装链路。
// 该入口供 package manager、offline enterprise installer 和 harness 使用，不绕过签名、平台、hash、size 或 smoke。
func (installer *Installer) InstallArchive(ctx context.Context, manifestPayload []byte, archive io.Reader, channel string) (Installation, error) {
	manifest, err := ParseAndVerifyManifest(manifestPayload, installer.verification(channel))
	if err != nil {
		return Installation{}, err
	}
	return installer.installVerified(ctx, manifest, archive)
}

// Status 读取并重新验证 active record、固定 binary path、owner/mode 和 executable SHA-256。
// active 缺失返回 COMPANION_MISSING；任何 pointer/path/hash 异常返回 COMPANION_UNTRUSTED。
func (installer *Installer) Status() (Installation, error) {
	if err := validateInstallRoot(installer.root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Installation{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not installed")
		}
		return Installation{}, err
	}
	activeInfo, err := os.Lstat(installer.activePath())
	if errors.Is(err, os.ErrNotExist) {
		return Installation{}, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING, "Cloud Companion is not installed")
	}
	if err != nil || !activeInfo.Mode().IsRegular() || !trustedFileOwner(installer.activePath(), activeInfo) || untrustedPrivateMode(activeInfo.Mode()) {
		return Installation{}, untrustedError("Cloud Companion active record owner or mode is untrusted")
	}
	payload, err := os.ReadFile(installer.activePath())
	if err != nil {
		return Installation{}, untrustedError("Cloud Companion active record cannot be read")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var record activeRecord
	if err := decoder.Decode(&record); err != nil {
		return Installation{}, untrustedError("Cloud Companion active record is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF || record.SchemaVersion != activeSchemaVersion {
		return Installation{}, untrustedError("Cloud Companion active record version is invalid")
	}
	expectedPath := filepath.Join(installer.root, "versions", record.Version, ExecutableName())
	if record.BinaryPath != expectedPath || record.OS != installer.os || record.Arch != installer.arch || canonicalVersion(record.Version) == "" || record.Channel != "stable" && record.Channel != "beta" || record.InstalledAt.IsZero() {
		return Installation{}, untrustedError("Cloud Companion active record escapes the fixed install namespace")
	}
	if err := validatePrivateInstallerDirectory(filepath.Join(installer.root, "versions")); err != nil {
		return Installation{}, err
	}
	if err := validatePrivateInstallerDirectory(filepath.Dir(expectedPath)); err != nil {
		return Installation{}, err
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(installer.root)
	resolvedPath, pathErr := filepath.EvalSymlinks(expectedPath)
	trustedResolvedPath := filepath.Join(resolvedRoot, "versions", record.Version, ExecutableName())
	if rootErr != nil || pathErr != nil || resolvedPath != trustedResolvedPath {
		return Installation{}, untrustedError("Cloud Companion active executable path contains an untrusted symlink")
	}
	info, err := os.Lstat(expectedPath)
	if err != nil || !info.Mode().IsRegular() || !trustedFileOwner(expectedPath, info) || untrustedExecutableMode(info.Mode()) {
		return Installation{}, untrustedError("Cloud Companion active executable owner or mode is untrusted")
	}
	digest, err := hashFile(expectedPath)
	if err != nil || digest != record.BinarySHA256 {
		return Installation{}, untrustedError("Cloud Companion active executable hash changed")
	}
	return record.Installation, nil
}

// Uninstall 删除固定 installer root 中的 binary、versions 和 active record。
// account/device OS credentials、DeviceIdentity、CapabilityGrant、connections.yaml 与 SSH 配置全部位于其他 owner domain，不会被删除。
func (installer *Installer) Uninstall() error {
	if err := validateInstallRoot(installer.root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(installer.root); err != nil {
		return fmt.Errorf("remove Cloud Companion installation: %w", err)
	}
	return nil
}

func (installer *Installer) installVerified(ctx context.Context, manifest Manifest, archive io.Reader) (Installation, error) {
	if archive == nil {
		return Installation{}, untrustedError("Cloud Companion archive is missing")
	}
	downloadURL, err := url.Parse(manifest.DownloadURL)
	if err != nil || !installer.allowedURL(downloadURL) {
		return Installation{}, untrustedError("Cloud Companion archive URL is outside the official origin")
	}
	if err := ensureInstallRoot(installer.root); err != nil {
		return Installation{}, err
	}
	if active, err := installer.Status(); err == nil && semver.Compare(manifest.Version, active.Version) < 0 && !manifest.AllowDowngrade {
		return Installation{}, untrustedError("Cloud Companion downgrade is not authorized by the signed manifest")
	} else if err != nil && !cloudcompanion.IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING) {
		return Installation{}, err
	}
	archivePath, archiveErr := installer.writeArchive(archive, manifest)
	if archiveErr != nil {
		return Installation{}, archiveErr
	}
	defer os.Remove(archivePath)
	stagingDir, err := os.MkdirTemp(installer.root, ".staging-")
	if err != nil {
		return Installation{}, fmt.Errorf("create Cloud Companion staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return Installation{}, fmt.Errorf("secure Cloud Companion staging directory: %w", err)
	}
	binaryPath := filepath.Join(stagingDir, ExecutableName())
	binaryDigest, err := extractBinary(archivePath, binaryPath)
	if err != nil {
		return Installation{}, err
	}
	if err := installer.smoke(ctx, binaryPath, manifest); err != nil {
		return Installation{}, fmt.Errorf("Cloud Companion handshake smoke failed: %w", err)
	}
	versionsDir := filepath.Join(installer.root, "versions")
	if err := ensurePrivateInstallerDirectory(versionsDir); err != nil {
		return Installation{}, fmt.Errorf("create Cloud Companion versions directory: %w", err)
	}
	targetDir := filepath.Join(versionsDir, manifest.Version)
	if info, err := os.Lstat(targetDir); err == nil {
		if !info.IsDir() || !trustedFileOwner(targetDir, info) || untrustedPrivateMode(info.Mode()) {
			return Installation{}, untrustedError("installed Cloud Companion version directory is untrusted")
		}
		existingPath := filepath.Join(targetDir, ExecutableName())
		existingInfo, infoErr := os.Lstat(existingPath)
		existingDigest, hashErr := hashFile(existingPath)
		if infoErr != nil || !existingInfo.Mode().IsRegular() || !trustedFileOwner(existingPath, existingInfo) || untrustedExecutableMode(existingInfo.Mode()) || hashErr != nil || existingDigest != binaryDigest {
			return Installation{}, untrustedError("installed Cloud Companion version has different executable bytes")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installation{}, fmt.Errorf("inspect Cloud Companion version directory: %w", err)
	} else if err := os.Rename(stagingDir, targetDir); err != nil {
		return Installation{}, fmt.Errorf("install Cloud Companion version atomically: %w", err)
	}
	if err := syncDirectory(versionsDir); err != nil {
		return Installation{}, fmt.Errorf("sync Cloud Companion versions directory: %w", err)
	}
	installation := Installation{
		Version: manifest.Version, Channel: manifest.Channel, OS: manifest.OS, Arch: manifest.Arch,
		BinaryPath: filepath.Join(targetDir, ExecutableName()), BinarySHA256: binaryDigest, InstalledAt: installer.now().UTC(),
	}
	if err := installer.writeActive(installation); err != nil {
		return Installation{}, err
	}
	return installation, nil
}

func (installer *Installer) writeArchive(reader io.Reader, manifest Manifest) (string, error) {
	file, err := os.CreateTemp(installer.root, ".archive-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("create Cloud Companion archive staging file: %w", err)
	}
	path := file.Name()
	defer func() {
		_ = file.Close()
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(reader, manifest.ArchiveSize+1))
	if copyErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("read Cloud Companion archive: %w", copyErr)
	}
	if written != manifest.ArchiveSize || hex.EncodeToString(hash.Sum(nil)) != manifest.ArchiveSHA256 {
		_ = os.Remove(path)
		return "", untrustedError("Cloud Companion archive size or SHA-256 does not match its signed manifest")
	}
	if err := file.Sync(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("sync Cloud Companion archive: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close Cloud Companion archive: %w", err)
	}
	return path, nil
}

func extractBinary(archivePath, binaryPath string) (string, error) {
	archive, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open verified Cloud Companion archive: %w", err)
	}
	defer archive.Close()
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return "", untrustedError("Cloud Companion archive is not valid gzip")
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	header, err := tarReader.Next()
	if err != nil || header.Name != ExecutableName() || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxBinaryBytes {
		return "", untrustedError("Cloud Companion archive must contain exactly one regular executable")
	}
	binary, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, executableMode())
	if err != nil {
		return "", fmt.Errorf("create staged Cloud Companion executable: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(binary, hash), io.LimitReader(tarReader, header.Size+1))
	if copyErr != nil || written != header.Size {
		_ = binary.Close()
		return "", untrustedError("Cloud Companion executable is truncated")
	}
	if err := binary.Sync(); err != nil {
		_ = binary.Close()
		return "", fmt.Errorf("sync staged Cloud Companion executable: %w", err)
	}
	if err := binary.Close(); err != nil {
		return "", fmt.Errorf("close staged Cloud Companion executable: %w", err)
	}
	if _, err := tarReader.Next(); !errors.Is(err, io.EOF) {
		return "", untrustedError("Cloud Companion archive contains unexpected files or trailing data")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (installer *Installer) writeActive(installation Installation) error {
	record := activeRecord{SchemaVersion: activeSchemaVersion, Installation: installation}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode Cloud Companion active record: %w", err)
	}
	temp, err := os.CreateTemp(installer.root, ".active-*.json")
	if err != nil {
		return fmt.Errorf("create Cloud Companion active record: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure Cloud Companion active record: %w", err)
	}
	if _, err := temp.Write(append(payload, '\n')); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write Cloud Companion active record: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync Cloud Companion active record: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close Cloud Companion active record: %w", err)
	}
	if err := replaceFile(tempPath, installer.activePath()); err != nil {
		return fmt.Errorf("activate Cloud Companion version atomically: %w", err)
	}
	return syncDirectory(installer.root)
}

func (installer *Installer) manifestURL(request Request) (*url.URL, error) {
	channel := strings.TrimSpace(request.Channel)
	if channel != "stable" && channel != "beta" {
		return nil, fmt.Errorf("Cloud Companion channel must be stable or beta")
	}
	version := "latest"
	if request.Version != "" {
		version = canonicalVersion(request.Version)
		if version == "" || version != request.Version {
			return nil, fmt.Errorf("Cloud Companion version must be canonical semver with v prefix")
		}
	}
	manifestURL := *installer.origin
	manifestURL.Path = strings.TrimRight(installer.origin.Path, "/") + "/" + channel + "/" + installer.os + "/" + installer.arch + "/" + url.PathEscape(version) + ".json"
	return &manifestURL, nil
}

func (installer *Installer) fetch(ctx context.Context, target *url.URL, limit int64) ([]byte, error) {
	response, err := installer.get(ctx, target)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read Cloud Companion release metadata: %w", err)
	}
	if int64(len(payload)) > limit {
		return nil, untrustedError("Cloud Companion release metadata exceeds its size limit")
	}
	return payload, nil
}

func (installer *Installer) get(ctx context.Context, target *url.URL) (*http.Response, error) {
	if !installer.allowedURL(target) {
		return nil, untrustedError("Cloud Companion release URL is outside the official origin")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := installer.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Cloud Companion release: %w", err)
	}
	if response.StatusCode != http.StatusOK || response.Request == nil || response.Request.URL == nil || !installer.allowedURL(response.Request.URL) {
		response.Body.Close()
		return nil, untrustedError("Cloud Companion release server returned an untrusted response")
	}
	return response, nil
}

func (installer *Installer) allowedURL(target *url.URL) bool {
	if target == nil || target.Scheme != "https" || target.Host != installer.origin.Host || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return false
	}
	basePath := strings.TrimRight(installer.origin.EscapedPath(), "/") + "/"
	return strings.HasPrefix(target.EscapedPath(), basePath)
}

func (installer *Installer) verification(channel string) Verification {
	return Verification{Channel: channel, OS: installer.os, Arch: installer.arch, ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax, Now: installer.now().UTC(), TrustedKeys: installer.trustedKeys}
}

func (installer *Installer) activePath() string { return filepath.Join(installer.root, "active.json") }

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maxBinaryBytes+1))
	if err != nil {
		return "", err
	}
	if written > maxBinaryBytes {
		return "", fmt.Errorf("Cloud Companion executable exceeds size limit")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func ensureInstallRoot(root string) error {
	if info, err := os.Lstat(root); err == nil {
		if !info.IsDir() || !trustedFileOwner(root, info) || untrustedPrivateMode(info.Mode()) {
			return untrustedError("Cloud Companion install root owner or mode is untrusted")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create Cloud Companion install root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return err
	}
	return validateInstallRoot(root)
}

func validateInstallRoot(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || !trustedFileOwner(root, info) || untrustedPrivateMode(info.Mode()) {
		return untrustedError("Cloud Companion install root owner or mode is untrusted")
	}
	return nil
}

func ensurePrivateInstallerDirectory(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return validatePrivateInstallerDirectory(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return validatePrivateInstallerDirectory(path)
		}
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return validatePrivateInstallerDirectory(path)
}

func validatePrivateInstallerDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !trustedFileOwner(path, info) || untrustedPrivateMode(info.Mode()) {
		return untrustedError("Cloud Companion version directory owner or mode is untrusted")
	}
	return nil
}
