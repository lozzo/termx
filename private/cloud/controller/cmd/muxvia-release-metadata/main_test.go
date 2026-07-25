package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestRunSignsRealArtifactDigestWithoutPrivateKeyInOutput(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	encoded, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	keyPath := filepath.Join(t.TempDir(), "release.pem")
	artifactPath := filepath.Join(t.TempDir(), "app.apk")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, []byte("signed Android artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := run([]string{"--file", artifactPath, "--signing-key", keyPath, "--key-id", "release-1", "--release-id", "android-1", "--product", "android", "--version", "v1.0.0", "--version-code", "100", "--os", "android", "--arch", "arm64", "--download-url", "https://releases.muxvia.test/app.apk"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	value := &cloudpb.ReleaseArtifactProjection{}
	if err := protojson.Unmarshal(output.Bytes(), value); err != nil {
		t.Fatal(err)
	}
	payload, _ := releasecatalog.SigningPayload(value)
	if !ed25519.Verify(publicKey, payload, value.GetSignature()) || bytes.Contains(output.Bytes(), privateKey) {
		t.Fatalf("release metadata is not safely signed: %s", output.String())
	}
}
