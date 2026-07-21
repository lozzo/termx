package ipc

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
)

// HelloOptions 固定一条公开进程到本机 Companion IPC connection 的 caller role 与能力请求。
// Endpoint 由平台配置或显式 dev profile 决定；该结构不能携带账号 token、DeviceIdentity private key、CapabilityGrant 或 terminal 数据。
type HelloOptions struct {
	TermxVersion string
	CallerRole   cloudpb.CallerRole
	Capabilities []cloudpb.CompanionCapability
	Random       io.Reader
}

// DialAndHello 验证本机 IPC peer，建立 Client，并完成该 connection 唯一一次 Hello 协商。
// 协商失败会关闭连接；返回的能力只能是请求集合的子集，后续缺失能力由对应 operation fail closed，不能回退旧 Hub API。
func DialAndHello(ctx context.Context, endpoint string, options HelloOptions) (*Client, *cloudpb.CompanionHelloResponse, error) {
	if options.TermxVersion == "" || options.CallerRole == cloudpb.CallerRole_CALLER_ROLE_UNSPECIFIED {
		return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "invalid Companion Hello configuration")
	}
	requested := make(map[cloudpb.CompanionCapability]struct{}, len(options.Capabilities))
	for _, capability := range options.Capabilities {
		if capability == cloudpb.CompanionCapability_COMPANION_CAPABILITY_UNSPECIFIED {
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Companion Hello contains an unspecified capability")
		}
		if _, duplicate := requested[capability]; duplicate {
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Companion Hello contains a duplicate capability")
		}
		requested[capability] = struct{}{}
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	nonce := make([]byte, 32)
	if _, err := io.ReadFull(randomSource, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate Companion Hello nonce: %w", err)
	}
	client, err := Dial(ctx, endpoint)
	if err != nil {
		return nil, nil, err
	}
	response, err := client.Hello(ctx, &cloudpb.CompanionHelloRequest{
		ProtocolMin: cloudcompanion.ProtocolVersionMin, ProtocolMax: cloudcompanion.ProtocolVersionMax,
		TermxVersion: options.TermxVersion, CallerRole: options.CallerRole,
		RequestedCapabilities: append([]cloudpb.CompanionCapability(nil), options.Capabilities...), RequestNonce: nonce,
	})
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	if response == nil || response.GetSelectedProtocol() < cloudcompanion.ProtocolVersionMin || response.GetSelectedProtocol() > cloudcompanion.ProtocolVersionMax ||
		response.GetCompanionVersion() == "" || response.GetBuildChannel() == "" || len(response.GetResponseNonce()) < 16 || len(response.GetResponseNonce()) > 64 || bytes.Equal(response.GetResponseNonce(), nonce) {
		_ = client.Close()
		return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE, "Cloud Companion returned an invalid Hello response")
	}
	seen := make(map[cloudpb.CompanionCapability]struct{}, len(response.GetSupportedCapabilities()))
	for _, capability := range response.GetSupportedCapabilities() {
		if _, ok := requested[capability]; !ok {
			_ = client.Close()
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion expanded the requested capability set")
		}
		if _, duplicate := seen[capability]; duplicate {
			_ = client.Close()
			return nil, nil, cloudcompanion.NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Cloud Companion returned a duplicate capability")
		}
		seen[capability] = struct{}{}
	}
	return client, response, nil
}
