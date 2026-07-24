package cloudcompanion

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	enrollmentProofDomain = "TXCLOUD-ENROLLMENT-PROOF-v1\x00"
	presenceProofDomain   = "TXCLOUD-PRESENCE-PROOF-v1\x00"
)

// EnrollmentProofSigningBytes 返回 daemon DeviceIdentity 签名 enrollment challenge 的 canonical bytes。
// 输入绑定 flow、challenge、DeviceID、public key 和签名时间；Companion 只能转发，不能替换后继续通过服务端验签。
func EnrollmentProofSigningBytes(input *cloudpb.DeviceEnrollmentProofInput) ([]byte, error) {
	if input == nil || input.GetFlowId() == "" || input.GetChallengeId() == "" || len(input.GetChallenge()) < 32 || len(input.GetChallenge()) > 256 || input.GetDeviceId() == "" || len(input.GetDevicePublicKey()) != ed25519.PublicKeySize || input.GetSignedAtUnixNano() == 0 || len(input.GetCandidateSetDigest()) != sha256.Size || input.GetPreferredHubId() == "" || len(input.GetHubObservationsDigest()) != sha256.Size || input.GetFlowRevision() == 0 {
		return nil, fmt.Errorf("invalid device enrollment proof input")
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode device enrollment proof input: %w", err)
	}
	result := make([]byte, 0, len(enrollmentProofDomain)+len(payload))
	result = append(result, enrollmentProofDomain...)
	result = append(result, payload...)
	return result, nil
}

// EnrollmentCandidateSetDigest 返回 Controller 候选目录的确定性摘要。
// 摘要按 Hub ID 排序并拒绝重复项，使 daemon 提议的 Hub 只能来自本次 challenge。
func EnrollmentCandidateSetDigest(candidates []*cloudpb.HubEnrollmentCandidate) ([]byte, error) {
	cloned := make([]*cloudpb.HubEnrollmentCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.GetHubId() == "" || candidate.GetHubUrl() == "" || candidate.GetHealthUrl() == "" || candidate.GetRegion() == "" {
			return nil, fmt.Errorf("invalid enrollment Hub candidate")
		}
		cloned = append(cloned, proto.Clone(candidate).(*cloudpb.HubEnrollmentCandidate))
	}
	if len(cloned) == 0 {
		return nil, fmt.Errorf("empty enrollment Hub candidate set")
	}
	sort.Slice(cloned, func(left, right int) bool { return cloned[left].GetHubId() < cloned[right].GetHubId() })
	for index := 1; index < len(cloned); index++ {
		if cloned[index-1].GetHubId() == cloned[index].GetHubId() {
			return nil, fmt.Errorf("duplicate enrollment Hub candidate")
		}
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudpb.DeviceEnrollmentChallenge{HubCandidates: cloned})
	if err != nil {
		return nil, fmt.Errorf("encode enrollment Hub candidates: %w", err)
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

// EnrollmentObservationsDigest 返回 daemon Hub 探测结果的确定性摘要。
// 规范化只允许每个候选一个观测，签名后 Companion 不能替换首选 Hub 或延迟数据。
func EnrollmentObservationsDigest(observations []*cloudpb.HubReachabilityObservation) ([]byte, error) {
	cloned := make([]*cloudpb.HubReachabilityObservation, 0, len(observations))
	for _, observation := range observations {
		if observation == nil || observation.GetHubId() == "" {
			return nil, fmt.Errorf("invalid enrollment Hub observation")
		}
		cloned = append(cloned, proto.Clone(observation).(*cloudpb.HubReachabilityObservation))
	}
	if len(cloned) == 0 {
		return nil, fmt.Errorf("empty enrollment Hub observations")
	}
	sort.Slice(cloned, func(left, right int) bool { return cloned[left].GetHubId() < cloned[right].GetHubId() })
	for index := 1; index < len(cloned); index++ {
		if cloned[index-1].GetHubId() == cloned[index].GetHubId() {
			return nil, fmt.Errorf("duplicate enrollment Hub observation")
		}
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&cloudpb.CompleteDeviceEnrollmentRequest{HubObservations: cloned})
	if err != nil {
		return nil, fmt.Errorf("encode enrollment Hub observations: %w", err)
	}
	digest := sha256.Sum256(payload)
	return digest[:], nil
}

// PresenceProofSigningBytes 返回 daemon DeviceIdentity 签名 fresh presence challenge 的 canonical bytes。
// 输入绑定独立 PresenceSession、一次性 challenge、DeviceID、public key 和签名时间；它不能复用 enrollment flow 或 client ManagedSession。
func PresenceProofSigningBytes(input *cloudpb.PresenceProofInput) ([]byte, error) {
	if input == nil || input.GetPresenceSessionId() == "" || input.GetChallengeId() == "" || len(input.GetChallenge()) < 32 || len(input.GetChallenge()) > 256 || input.GetDeviceId() == "" || len(input.GetDevicePublicKey()) != ed25519.PublicKeySize || input.GetSignedAtUnixNano() == 0 {
		return nil, fmt.Errorf("invalid device presence proof input")
	}
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode device presence proof input: %w", err)
	}
	result := make([]byte, 0, len(presenceProofDomain)+len(payload))
	result = append(result, presenceProofDomain...)
	result = append(result, payload...)
	return result, nil
}
