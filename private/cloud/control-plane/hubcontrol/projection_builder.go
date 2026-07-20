package hubcontrol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const projectionSignatureDomain = "termx-hub-projection-v1\x00"

// FullProjectionInput 是 Controller 持久真值生成单个 Hub 完整 projection 的输入。
type FullProjectionInput struct {
	HubID        string
	Revision     uint64
	GeneratedAt  time.Time
	TTL          time.Duration
	Accounts     []*cloudpb.HubAccountPolicy
	Devices      []*cloudpb.CloudDevicePolicy
	Assignments  []*cloudpb.HubAssignment
	SigningKeyID string
	SigningKey   ed25519.PrivateKey
}

// DeltaProjectionInput 使用最终完整 candidate 计算 resulting digest，再签名当前操作集合。
type DeltaProjectionInput struct {
	HubID                string
	Revision             uint64
	PreviousRevision     uint64
	GeneratedAt          time.Time
	TTL                  time.Duration
	AccountOperations    []*cloudpb.HubAccountPolicyDelta
	DeviceOperations     []*cloudpb.DevicePolicyDelta
	AssignmentOperations []*cloudpb.HubAssignmentDelta
	ResultingAccounts    []*cloudpb.HubAccountPolicy
	ResultingDevices     []*cloudpb.CloudDevicePolicy
	ResultingAssignments []*cloudpb.HubAssignment
	SigningKeyID         string
	SigningKey           ed25519.PrivateKey
}

// BuildSignedFullProjection 生成与 Hub verifier 一致的 canonical digest 和 Ed25519 signature。
func BuildSignedFullProjection(input FullProjectionInput) (*cloudpb.FullProjectionSnapshot, error) {
	if input.HubID == "" || input.Revision == 0 || input.GeneratedAt.IsZero() || input.TTL <= 0 || input.SigningKeyID == "" || len(input.SigningKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid full Hub projection input")
	}
	full := &cloudpb.FullProjectionSnapshot{HubId: input.HubID, ProjectionRevision: input.Revision, GeneratedAtUnixMillis: input.GeneratedAt.UTC().UnixMilli(), ExpiresAtUnixMillis: input.GeneratedAt.UTC().Add(input.TTL).UnixMilli(), SigningKeyId: input.SigningKeyID}
	for _, value := range input.Accounts {
		full.Accounts = append(full.Accounts, proto.Clone(value).(*cloudpb.HubAccountPolicy))
	}
	for _, value := range input.Devices {
		full.Devices = append(full.Devices, proto.Clone(value).(*cloudpb.CloudDevicePolicy))
	}
	for _, value := range input.Assignments {
		full.Assignments = append(full.Assignments, proto.Clone(value).(*cloudpb.HubAssignment))
	}
	sortProjection(full.Accounts, full.Devices, full.Assignments)
	digest, err := projectionDigest(input.Revision, input.GeneratedAt.UTC(), input.GeneratedAt.UTC().Add(input.TTL), full.Accounts, full.Devices, full.Assignments)
	if err != nil {
		return nil, err
	}
	full.SnapshotDigest = digest
	signing := proto.Clone(full).(*cloudpb.FullProjectionSnapshot)
	signing.Signature = nil
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(signing)
	if err != nil {
		return nil, err
	}
	full.Signature = ed25519.Sign(input.SigningKey, append([]byte(projectionSignatureDomain+"full\x00"), payload...))
	return full, nil
}

// BuildSignedDelta 生成 Controller-owned signed delta；resulting digest 来自最终完整 candidate，而不是操作字节本身。
func BuildSignedDelta(input DeltaProjectionInput) (*cloudpb.PolicyDelta, error) {
	if input.HubID == "" || input.Revision == 0 || input.PreviousRevision == 0 || input.Revision != input.PreviousRevision+1 || input.GeneratedAt.IsZero() || input.TTL <= 0 || input.SigningKeyID == "" || len(input.SigningKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Hub policy delta input")
	}
	digest, err := projectionDigest(input.Revision, input.GeneratedAt.UTC(), input.GeneratedAt.UTC().Add(input.TTL), input.ResultingAccounts, input.ResultingDevices, input.ResultingAssignments)
	if err != nil {
		return nil, err
	}
	delta := &cloudpb.PolicyDelta{HubId: input.HubID, ProjectionRevision: input.Revision, PreviousProjectionRevision: input.PreviousRevision, ResultingDigest: digest, GeneratedAtUnixMillis: input.GeneratedAt.UTC().UnixMilli(), ExpiresAtUnixMillis: input.GeneratedAt.UTC().Add(input.TTL).UnixMilli(), SigningKeyId: input.SigningKeyID}
	for _, value := range input.AccountOperations {
		delta.AccountOperations = append(delta.AccountOperations, proto.Clone(value).(*cloudpb.HubAccountPolicyDelta))
	}
	for _, value := range input.DeviceOperations {
		delta.DeviceOperations = append(delta.DeviceOperations, proto.Clone(value).(*cloudpb.DevicePolicyDelta))
	}
	for _, value := range input.AssignmentOperations {
		delta.AssignmentOperations = append(delta.AssignmentOperations, proto.Clone(value).(*cloudpb.HubAssignmentDelta))
	}
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(delta)
	if err != nil {
		return nil, err
	}
	delta.Signature = ed25519.Sign(input.SigningKey, append([]byte(projectionSignatureDomain+"delta\x00"), payload...))
	return delta, nil
}

func projectionDigest(revision uint64, generatedAt, expiresAt time.Time, accounts []*cloudpb.HubAccountPolicy, devices []*cloudpb.CloudDevicePolicy, assignments []*cloudpb.HubAssignment) ([]byte, error) {
	digestMessage := &cloudpb.FullProjectionSnapshot{HubId: "digest", ProjectionRevision: revision, GeneratedAtUnixMillis: generatedAt.UnixMilli(), ExpiresAtUnixMillis: expiresAt.UnixMilli()}
	for _, value := range accounts {
		digestMessage.Accounts = append(digestMessage.Accounts, proto.Clone(value).(*cloudpb.HubAccountPolicy))
	}
	for _, value := range devices {
		digestMessage.Devices = append(digestMessage.Devices, proto.Clone(value).(*cloudpb.CloudDevicePolicy))
	}
	for _, value := range assignments {
		digestMessage.Assignments = append(digestMessage.Assignments, proto.Clone(value).(*cloudpb.HubAssignment))
	}
	sortProjection(digestMessage.Accounts, digestMessage.Devices, digestMessage.Assignments)
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(digestMessage)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(append([]byte(projectionSignatureDomain+"digest\x00"), payload...))
	return digest[:], nil
}

func sortProjection(accounts []*cloudpb.HubAccountPolicy, devices []*cloudpb.CloudDevicePolicy, assignments []*cloudpb.HubAssignment) {
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].GetAccountId() < accounts[j].GetAccountId() })
	sort.Slice(devices, func(i, j int) bool { return devices[i].GetDeviceId() < devices[j].GetDeviceId() })
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].GetDaemonDeviceId() < assignments[j].GetDaemonDeviceId() })
}
