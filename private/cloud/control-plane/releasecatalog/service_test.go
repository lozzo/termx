package releasecatalog_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	"github.com/muxvia/muxvia/private/cloud/control-plane/releasecatalog"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

func TestReleaseCatalogVerifiesSignatureMonotonicActivationRolloutAndRollback(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "release-catalog-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := releasecatalog.New(store, map[string]ed25519.PublicKey{"release-key-1": publicKey}, []string{"https://releases.muxvia.test"}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	v100 := signedArtifact(t, privateKey, "android-100", "v1.0.0", 100, 10000, 1)
	tampered := proto.Clone(v100).(*cloudpb.ReleaseArtifactProjection)
	tampered.Sha256[0] ^= 0xff
	if _, err := service.Publish(ctx, tampered, "operator-1", "tampered", "publish-tampered"); !errors.Is(err, releasecatalog.ErrInvalid) {
		t.Fatalf("tampered signature = %v", err)
	}
	untrusted := proto.Clone(v100).(*cloudpb.ReleaseArtifactProjection)
	untrusted.ReleaseId = "android-untrusted"
	untrusted.DownloadUrl = "https://attacker.example/app.apk"
	untrusted.Signature = nil
	untrustedPayload, _ := releasecatalog.SigningPayload(untrusted)
	untrusted.Signature = ed25519.Sign(privateKey, untrustedPayload)
	if _, err := service.Publish(ctx, untrusted, "operator-1", "untrusted origin", "publish-untrusted"); !errors.Is(err, releasecatalog.ErrInvalid) {
		t.Fatalf("untrusted release origin = %v", err)
	}
	if _, err := service.Publish(ctx, v100, "operator-1", "initial Android", "publish-100"); err != nil {
		t.Fatal(err)
	}
	head, err := service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: v100.GetReleaseId(), Reason: "initial activation", RequestId: "activate-100"}, "operator-1")
	if err != nil || head.GetRevision() != 1 {
		t.Fatalf("initial activation = (%v, %v)", head, err)
	}
	v90 := signedArtifact(t, privateKey, "android-90", "v0.9.0", 90, 10000, 1)
	if _, err := service.Publish(ctx, v90, "operator-1", "historical candidate", "publish-90"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: v90.GetReleaseId(), ExpectedRevision: 1, Reason: "implicit downgrade", RequestId: "activate-90"}, "operator-1"); !errors.Is(err, releasecatalog.ErrConflict) {
		t.Fatalf("implicit downgrade = %v", err)
	}
	v200 := signedArtifact(t, privateKey, "android-200", "v2.0.0", 200, 1, 1)
	if _, err := service.Publish(ctx, v200, "operator-1", "canary", "publish-200"); err != nil {
		t.Fatal(err)
	}
	head, err = service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: v200.GetReleaseId(), ExpectedRevision: 1, Reason: "start canary", RequestId: "activate-200"}, "operator-1")
	if err != nil || head.GetRevision() != 2 {
		t.Fatalf("forward activation = (%v, %v)", head, err)
	}
	request := &cloudpb.ResolveClientReleaseRequest{Product: cloudpb.ReleaseProduct_RELEASE_PRODUCT_ANDROID, Channel: cloudpb.ReleaseChannel_RELEASE_CHANNEL_STABLE, Os: "android", Arch: "arm64", CurrentVersion: "v1.0.0", CurrentVersionCode: 100, StableClientId: "device-stable-1"}
	first, err := service.Resolve(ctx, request)
	second, secondErr := service.Resolve(ctx, request)
	if err != nil || secondErr != nil || first.GetRolloutBucket() != second.GetRolloutBucket() || first.GetDecision() != second.GetDecision() {
		t.Fatalf("stable rollout = (%v, %v, %v, %v)", first, err, second, secondErr)
	}
	forcedArtifact := proto.Clone(v200).(*cloudpb.ReleaseArtifactProjection)
	forcedArtifact.ReleaseId = "android-201"
	forcedArtifact.Version = "v2.0.1"
	forcedArtifact.VersionCode = 201
	forcedArtifact.MinCompatibleVersionCode = 150
	forcedArtifact.Signature = nil
	payload, _ := releasecatalog.SigningPayload(forcedArtifact)
	forcedArtifact.Signature = ed25519.Sign(privateKey, payload)
	if _, err := service.Publish(ctx, forcedArtifact, "operator-1", "compatibility floor", "publish-201"); err != nil {
		t.Fatal(err)
	}
	head, err = service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: forcedArtifact.GetReleaseId(), ExpectedRevision: 2, Reason: "enforce compatibility", RequestId: "activate-201"}, "operator-1")
	if err != nil {
		t.Fatal(err)
	}
	forced, err := service.Resolve(ctx, request)
	if err != nil || !forced.GetForced() || !forced.GetUpdateAvailable() {
		t.Fatalf("compatibility force = (%v, %v)", forced, err)
	}
	head, err = service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: forcedArtifact.GetReleaseId(), ExpectedRevision: 3, Paused: true, Reason: "incident pause", RequestId: "pause-201"}, "operator-1")
	if err != nil {
		t.Fatal(err)
	}
	paused, err := service.Resolve(ctx, request)
	if err != nil || paused.GetUpdateAvailable() || paused.GetDecision() != "paused" {
		t.Fatalf("paused channel = (%v, %v)", paused, err)
	}
	head, err = service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: forcedArtifact.GetReleaseId(), ExpectedRevision: head.GetRevision(), Reason: "incident resolved", RequestId: "resume-201"}, "operator-1")
	if err != nil || head.GetPaused() || head.GetRevision() != 5 {
		t.Fatalf("resumed channel = (%v, %v)", head, err)
	}
	head, err = service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: v100.GetReleaseId(), ExpectedRevision: head.GetRevision(), AllowRollback: true, Reason: "rollback incident", RequestId: "rollback-100"}, "operator-1")
	if err != nil || head.GetActiveReleaseId() != v100.GetReleaseId() || head.GetRevision() != 6 {
		t.Fatalf("explicit rollback = (%v, %v)", head, err)
	}
	if _, err := service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: v200.GetReleaseId(), ExpectedRevision: 3, Reason: "stale CAS", RequestId: "stale-cas"}, "operator-1"); !errors.Is(err, releasecatalog.ErrConflict) {
		t.Fatalf("stale channel CAS = %v", err)
	}
	cli := signedArtifact(t, privateKey, "cli-10", "v1.0.0", 10, 0, 1)
	cli.Product = cloudpb.ReleaseProduct_RELEASE_PRODUCT_CLI_DAEMON
	cli.Os = "linux"
	cli.Arch = "amd64"
	cli.DownloadUrl = "https://releases.muxvia.test/cli-10.tar.gz"
	cli.ForceAfterUnixMillis = now.Add(-time.Minute).UnixMilli()
	cli.Signature = nil
	cliPayload, _ := releasecatalog.SigningPayload(cli)
	cli.Signature = ed25519.Sign(privateKey, cliPayload)
	if _, err := service.Publish(ctx, cli, "operator-1", "CLI force deadline", "publish-cli-10"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetChannel(ctx, &cloudpb.SetReleaseChannelRequest{ReleaseId: cli.GetReleaseId(), Reason: "activate CLI", RequestId: "activate-cli-10"}, "operator-1"); err != nil {
		t.Fatal(err)
	}
	cliDecision, err := service.Resolve(ctx, &cloudpb.ResolveClientReleaseRequest{Product: cli.GetProduct(), Channel: cli.GetChannel(), Os: cli.GetOs(), Arch: cli.GetArch(), CurrentVersion: "v0.9.0", CurrentVersionCode: 9, StableClientId: "daemon-machine-1"})
	if err != nil || !cliDecision.GetForced() || !cliDecision.GetUpdateAvailable() || cliDecision.GetDecision() != "forced" {
		t.Fatalf("CLI force deadline = (%v, %v)", cliDecision, err)
	}
	history, err := service.List(ctx, &cloudpb.ListReleaseArtifactsRequest{Page: &cloudpb.PageRequest{PageSize: 20}})
	if err != nil || len(history.GetOperatorAudit()) < 8 {
		t.Fatalf("release audit history = (%v, %v)", history, err)
	}
	actions := map[string]bool{}
	for _, audit := range history.GetOperatorAudit() {
		actions[audit.GetAction()] = true
	}
	for _, action := range []string{"release.publish", "release.activate", "release.pause", "release.resume", "release.rollback"} {
		if !actions[action] {
			t.Fatalf("release audit action %q missing: %v", action, actions)
		}
	}
}

func signedArtifact(t *testing.T, key ed25519.PrivateKey, id, version string, versionCode uint64, rollout uint32, min uint64) *cloudpb.ReleaseArtifactProjection {
	t.Helper()
	digest := sha256.Sum256([]byte(id))
	value := &cloudpb.ReleaseArtifactProjection{ReleaseId: id, Product: cloudpb.ReleaseProduct_RELEASE_PRODUCT_ANDROID, Channel: cloudpb.ReleaseChannel_RELEASE_CHANNEL_STABLE, Version: version, VersionCode: versionCode, Os: "android", Arch: "arm64", DownloadUrl: "https://releases.muxvia.test/" + id + ".apk", ArtifactSize: 4096, Sha256: digest[:], SigningKeyId: "release-key-1", MinCompatibleVersionCode: min, RolloutBasisPoints: rollout, Changelog: version}
	payload, err := releasecatalog.SigningPayload(value)
	if err != nil {
		t.Fatal(err)
	}
	value.Signature = ed25519.Sign(key, payload)
	return value
}
