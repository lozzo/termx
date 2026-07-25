// Command muxvia-release-metadata 为 CLI/daemon 或 Android artifact 生成签名 Proto JSON。
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("muxvia-release-metadata", flag.ContinueOnError)
	var file, keyFile, keyID, releaseID, product, channel, version, targetOS, arch, downloadURL, changelog string
	var versionCode, minCompatible uint64
	var forceAfter int64
	var rollout uint
	flags.StringVar(&file, "file", "", "artifact file")
	flags.StringVar(&keyFile, "signing-key", "", "external Ed25519 PKCS#8 PEM")
	flags.StringVar(&keyID, "key-id", "", "trusted public key id")
	flags.StringVar(&releaseID, "release-id", "", "immutable release id")
	flags.StringVar(&product, "product", "", "cli-daemon or android")
	flags.StringVar(&channel, "channel", "stable", "stable or beta")
	flags.StringVar(&version, "version", "", "semantic version")
	flags.Uint64Var(&versionCode, "version-code", 0, "monotonic version code")
	flags.StringVar(&targetOS, "os", "", "target OS")
	flags.StringVar(&arch, "arch", "", "target architecture")
	flags.StringVar(&downloadURL, "download-url", "", "official HTTPS URL")
	flags.Uint64Var(&minCompatible, "min-compatible-code", 1, "minimum compatible current version code")
	flags.Int64Var(&forceAfter, "force-after-millis", 0, "optional absolute force deadline")
	flags.UintVar(&rollout, "rollout-basis-points", 10000, "0..10000 rollout")
	flags.StringVar(&changelog, "changelog", "", "release notes")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || file == "" || keyFile == "" || keyID == "" || releaseID == "" || version == "" || versionCode == 0 || targetOS == "" || arch == "" || downloadURL == "" {
		return fmt.Errorf("all artifact identity, target, version, URL and signing key flags are required")
	}
	payload, err := os.ReadFile(file)
	if err != nil || len(payload) == 0 {
		return fmt.Errorf("read artifact")
	}
	defer clear(payload)
	digest := sha256.Sum256(payload)
	key, err := readKey(keyFile)
	if err != nil {
		return err
	}
	defer clear(key)
	parsedProduct, parsedChannel := parseProduct(product), parseChannel(channel)
	if parsedProduct == cloudpb.ReleaseProduct_RELEASE_PRODUCT_UNSPECIFIED || parsedChannel == cloudpb.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED || rollout > 10000 || forceAfter < 0 {
		return fmt.Errorf("product, channel, rollout, or force deadline is invalid")
	}
	value := &cloudpb.ReleaseArtifactProjection{ReleaseId: releaseID, Product: parsedProduct, Channel: parsedChannel, Version: version, VersionCode: versionCode, Os: targetOS, Arch: arch, DownloadUrl: downloadURL, ArtifactSize: uint64(len(payload)), Sha256: digest[:], SigningKeyId: keyID, MinCompatibleVersionCode: minCompatible, ForceAfterUnixMillis: forceAfter, RolloutBasisPoints: uint32(rollout), Changelog: changelog}
	signingPayload, err := releasecatalog.SigningPayload(value)
	if err != nil {
		return err
	}
	value.Signature = ed25519.Sign(key, signingPayload)
	encoded, err := protojson.MarshalOptions{UseProtoNames: true, Multiline: true, Indent: "  "}.Marshal(value)
	if err != nil {
		return err
	}
	fmt.Fprintln(output, string(encoded))
	return nil
}

func parseProduct(value string) cloudpb.ReleaseProduct {
	if value == "android" {
		return cloudpb.ReleaseProduct_RELEASE_PRODUCT_ANDROID
	}
	if value == "cli-daemon" {
		return cloudpb.ReleaseProduct_RELEASE_PRODUCT_CLI_DAEMON
	}
	return cloudpb.ReleaseProduct_RELEASE_PRODUCT_UNSPECIFIED
}

func parseChannel(value string) cloudpb.ReleaseChannel {
	if value == "stable" {
		return cloudpb.ReleaseChannel_RELEASE_CHANNEL_STABLE
	}
	if value == "beta" {
		return cloudpb.ReleaseChannel_RELEASE_CHANNEL_BETA
	}
	return cloudpb.ReleaseChannel_RELEASE_CHANNEL_UNSPECIFIED
}

func readKey(path string) (ed25519.PrivateKey, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(body)
	block, rest := pem.Decode(body)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("signing key must be PKCS#8 PRIVATE KEY PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok || len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("signing key must be Ed25519")
	}
	return key, nil
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
