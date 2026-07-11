package cloudcompanion

import (
	"crypto/ed25519"
	"fmt"

	"github.com/lozzow/termx/termx-proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const enrollmentProofDomain = "TXCLOUD-ENROLLMENT-PROOF-v1\x00"

// EnrollmentProofSigningBytes 返回 daemon DeviceIdentity 签名 enrollment challenge 的 canonical bytes。
// 输入绑定 flow、challenge、DeviceID、public key 和签名时间；Companion 只能转发，不能替换后继续通过服务端验签。
func EnrollmentProofSigningBytes(input *cloudpb.DeviceEnrollmentProofInput) ([]byte, error) {
	if input == nil || input.GetFlowId() == "" || input.GetChallengeId() == "" || len(input.GetChallenge()) < 32 || len(input.GetChallenge()) > 256 || input.GetDeviceId() == "" || len(input.GetDevicePublicKey()) != ed25519.PublicKeySize || input.GetSignedAtUnixNano() == 0 {
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
