package client

import (
	"errors"
	"strings"

	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// EncodeEdgeLocator 持久化 Controller 已认证返回的公开 Edge locator；它不包含客户端授权。
func EncodeEdgeLocator(edge *cloudv1.CandidateEdge) ([]byte, error) {
	if err := validateEdgeLocator(edge); err != nil {
		return nil, err
	}
	return proto.MarshalOptions{Deterministic: true}.Marshal(edge)
}

// DecodeEdgeLocator 解析本机缓存；最终 TLS 和 CloudRouteGrant 仍分别验证 Edge 与客户端身份。
func DecodeEdgeLocator(payload []byte) (*cloudv1.CandidateEdge, error) {
	if len(payload) == 0 {
		return nil, errors.New("cached Edge locator is empty")
	}
	edge := &cloudv1.CandidateEdge{}
	if err := proto.Unmarshal(payload, edge); err != nil {
		return nil, errors.New("cached Edge locator is invalid")
	}
	if err := validateEdgeLocator(edge); err != nil {
		return nil, err
	}
	return edge, nil
}

// NewCachedCapabilityRoute 从 secure credential 中的 locator 和 daemon grant 重建直连请求。
func NewCachedCapabilityRoute(locatorPayload, grantPayload []byte) (*RouteResolution, error) {
	edge, err := DecodeEdgeLocator(locatorPayload)
	if err != nil {
		return nil, err
	}
	grant := &cloudv1.SignedEnvelope{}
	if len(grantPayload) == 0 || proto.Unmarshal(grantPayload, grant) != nil {
		return nil, errors.New("cached Cloud Route grant is invalid")
	}
	return NewCachedRoute(edge, grant, cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY)
}

// ShouldRefreshEdgeLocator 只把位置失效或旧 Edge 不可达解释为目录缓存失效。
// 授权、配额和 daemon 拒绝不能通过回源 Controller 掩盖。
func ShouldRefreshEdgeLocator(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.NotFound, codes.Unavailable:
		return true
	default:
		return false
	}
}

func validateEdgeLocator(edge *cloudv1.CandidateEdge) error {
	if edge == nil || strings.TrimSpace(edge.GetEdgeId()) == "" || strings.TrimSpace(edge.GetPublicEndpoint()) == "" || strings.TrimSpace(edge.GetServerName()) == "" {
		return errors.New("cached Edge locator is incomplete")
	}
	return nil
}
