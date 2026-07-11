// Package main builds signed Cloud Companion release artifacts for private CI.
// Release private keys are read from an external PKCS#8 PEM file and are never embedded in this repository or output metadata.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lozzow/termx/shared/cloudcompanion"
	"github.com/lozzow/termx/shared/cloudcompanion/installer"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, time.Now); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, now func() time.Time) error {
	flags := flag.NewFlagSet("termx-cloud-release", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var binaryPath, keyPath, keyID, channel, version, targetOS, targetArch, downloadURL, outputDir string
	var allowDowngrade bool
	flags.StringVar(&binaryPath, "binary", "", "built termx-cloud executable")
	flags.StringVar(&keyPath, "signing-key", "", "external Ed25519 PKCS#8 PEM key")
	flags.StringVar(&keyID, "key-id", "", "public release key id")
	flags.StringVar(&channel, "channel", "stable", "stable or beta")
	flags.StringVar(&version, "version", "", "canonical v-prefixed semver")
	flags.StringVar(&targetOS, "os", "", "target GOOS")
	flags.StringVar(&targetArch, "arch", "", "target GOARCH")
	flags.StringVar(&downloadURL, "download-url", "", "final official HTTPS archive URL")
	flags.StringVar(&outputDir, "out", "", "output directory")
	flags.BoolVar(&allowDowngrade, "allow-downgrade", false, "authorize an explicit signed rollback")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || binaryPath == "" || keyPath == "" || keyID == "" || version == "" || targetOS == "" || targetArch == "" || downloadURL == "" || outputDir == "" {
		return fmt.Errorf("binary, signing-key, key-id, version, os, arch, download-url and out are required")
	}
	privateKey, err := loadSigningKey(keyPath)
	if err != nil {
		return err
	}
	defer clear(privateKey)
	binary, err := os.ReadFile(binaryPath)
	if err != nil || len(binary) == 0 {
		return fmt.Errorf("read Cloud Companion release binary")
	}
	defer clear(binary)
	archive, err := buildArchive(binary, executableName(targetOS))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(archive)
	manifest := installer.Manifest{
		SchemaVersion: 1, Channel: channel, Version: version, OS: targetOS, Arch: targetArch,
		DownloadURL: downloadURL, ArchiveSHA256: hex.EncodeToString(digest[:]), ArchiveSize: int64(len(archive)),
		SigningKeyID: keyID, MinCompanionProtocol: cloudcompanion.ProtocolVersionMin, MaxCompanionProtocol: cloudcompanion.ProtocolVersionMax,
		PublishedAt: now().UTC().Format(time.RFC3339), AllowDowngrade: allowDowngrade,
	}
	manifestPayload, err := installer.MarshalSignedManifest(manifest, privateKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return fmt.Errorf("create release output directory: %w", err)
	}
	baseName := fmt.Sprintf("termx-cloud_%s_%s_%s", strings.TrimPrefix(version, "v"), targetOS, targetArch)
	archivePath := filepath.Join(outputDir, baseName+".tar.gz")
	manifestPath := filepath.Join(outputDir, baseName+".json")
	if err := writeAtomic(archivePath, archive, 0o644); err != nil {
		return err
	}
	if err := writeAtomic(manifestPath, append(manifestPayload, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "archive=%s\nmanifest=%s\n", archivePath, manifestPath)
	return nil
}

func loadSigningKey(path string) (ed25519.PrivateKey, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read release signing key: %w", err)
	}
	defer clear(payload)
	block, rest := pem.Decode(payload)
	if block == nil || len(rest) != 0 || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("release signing key must be one PKCS#8 PRIVATE KEY PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	clear(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse release signing key: %w", err)
	}
	privateKey, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("release signing key is not Ed25519")
	}
	return append(ed25519.PrivateKey(nil), privateKey...), nil
}

func buildArchive(binary []byte, name string) ([]byte, error) {
	var buffer strings.Builder
	gzipWriter := gzip.NewWriter(&builderWriter{builder: &buffer})
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: name, Mode: 0o700, Size: int64(len(binary)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0).UTC()}
	if err := tarWriter.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := tarWriter.Write(binary); err != nil {
		return nil, err
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return []byte(buffer.String()), nil
}

type builderWriter struct{ builder *strings.Builder }

func (writer *builderWriter) Write(payload []byte) (int, error) { return writer.builder.Write(payload) }

func executableName(targetOS string) string {
	if targetOS == "windows" {
		return "termx-cloud.exe"
	}
	return "termx-cloud"
}

func writeAtomic(path string, payload []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
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
	return os.Rename(tempPath, path)
}
