package cloudcompanion

import (
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

const maxSingleRelayLeaseTTL = 10 * time.Minute
const maxSignedRelayLeaseBytes = 1 << 20

// RelayLeaseUnavailableForAuto 判断 AUTO 是否可以在 Relay 能力不可用时继续尝试 P2P。
// 错误码来自 Cloud product contract；只有明确的 entitlement/quota 拒绝允许降级，鉴权、协议或临时服务错误仍须终止当前尝试。
func RelayLeaseUnavailableForAuto(err error) bool {
	return IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED) ||
		IsCode(err, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED)
}

// ValidateSingleRelayLease 在公开 client/daemon 边界校验 single-Relay 执行 material。
// 该校验接受 STANDARD_RELAY 的 AUTO 或 relay-only 请求、短期 signed lease 和纯 TURN caller-specific credential；校验失败不能回退 direct、共享 TURN secret 或旧 Hub 路径。
func ValidateSingleRelayLease(request *cloudpb.AcquireRelayLeaseRequest, response *cloudpb.RelayLease, now time.Time) error {
	if request == nil || response == nil || strings.TrimSpace(request.GetManagedSessionId()) == "" || strings.TrimSpace(request.GetTargetDeviceId()) == "" ||
		request.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || !canonicalRelayValue(response.GetLeaseId()) ||
		len(response.GetSignedLease()) == 0 || len(response.GetSignedLease()) > maxSignedRelayLeaseBytes {
		return relayProtocolError("Cloud Companion returned an invalid single Relay lease identity")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if response.GetExpiresAtUnix() > uint64(^uint64(0)>>1) {
		return relayProtocolError("Cloud Companion returned an invalid single Relay expiry")
	}
	expiresAt := time.Unix(int64(response.GetExpiresAtUnix()), 0).UTC()
	if !now.Before(expiresAt) || expiresAt.After(now.Add(maxSingleRelayLeaseTTL)) {
		return relayProtocolError("Cloud Companion returned an expired or overlong single Relay lease")
	}
	if response.GetPathKind() != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY || response.GetRouteId() != "" || response.GetRouteVersion() != 0 ||
		response.GetClientEdgeRelayId() != "" || response.GetDaemonEdgeRelayId() != "" || response.GetMaxInternalTransit() != 0 {
		return relayProtocolError("Cloud Companion returned Relay material outside the single Relay contract")
	}
	if len(response.GetIceServers()) == 0 {
		return relayProtocolError("Cloud Companion returned a single Relay lease without TURN material")
	}
	for _, server := range response.GetIceServers() {
		if server == nil || len(server.GetUrls()) == 0 || strings.TrimSpace(server.GetUsername()) == "" || strings.TrimSpace(server.GetCredential()) == "" {
			return relayProtocolError("Cloud Companion returned incomplete single Relay TURN material")
		}
		for _, rawURL := range server.GetUrls() {
			if rawURL == "" || rawURL != strings.TrimSpace(rawURL) {
				return relayProtocolError("Cloud Companion returned a non-canonical single Relay URL")
			}
			lowerURL := strings.ToLower(rawURL)
			if !strings.HasPrefix(lowerURL, "turn:") && !strings.HasPrefix(lowerURL, "turns:") {
				return relayProtocolError("Cloud Companion returned non-TURN material for relay-only ICE")
			}
		}
	}
	return nil
}

func canonicalRelayValue(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, " \t\r\n")
}

func relayProtocolError(message string) error {
	err := NewError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, message)
	err.Retryable = false
	return err
}
