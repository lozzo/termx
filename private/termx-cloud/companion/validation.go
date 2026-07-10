package companion

import (
	"net/url"
	"strings"
	"time"

	"github.com/lozzow/termx/private/termx-cloud/companion/cloudservice"
	"github.com/lozzow/termx/termx-proto/cloudpb"
)

func sanitizePresenceEvent(event *cloudpb.PresenceEvent, managedSessionID, targetDeviceID string) (*cloudpb.PresenceEvent, error) {
	if event == nil {
		return nil, protocolError("Hub returned an empty presence event")
	}
	cloned := cloneMessage(event)
	switch payload := cloned.GetPayload().(type) {
	case *cloudpb.PresenceEvent_Ready:
		if payload.Ready == nil || payload.Ready.GetManagedSessionId() != managedSessionID || payload.Ready.GetHeartbeatSeconds() == 0 {
			return nil, protocolError("Hub returned an invalid presence ready event")
		}
		if err := validateIceServers(payload.Ready.GetIceServers()); err != nil {
			return nil, err
		}
	case *cloudpb.PresenceEvent_Offer:
		if payload.Offer == nil || payload.Offer.GetSignalingSessionId() == "" || payload.Offer.GetManagedSessionId() != managedSessionID || payload.Offer.GetSourceDeviceId() == "" || payload.Offer.GetTargetDeviceId() != targetDeviceID || payload.Offer.GetSdp() == "" {
			return nil, protocolError("Hub returned an invalid signaling offer")
		}
		if err := validateCandidates(payload.Offer.GetCandidates()); err != nil {
			return nil, err
		}
	case *cloudpb.PresenceEvent_Candidate:
		return nil, protocolError("daemon trickle ICE candidate lacks signaling session binding")
	case *cloudpb.PresenceEvent_Error:
		if payload.Error == nil || !validCloudErrorCode(payload.Error.GetCode(), false) {
			return nil, protocolError("Hub returned an invalid presence error")
		}
		payload.Error.Message = "managed cloud presence failed"
	case *cloudpb.PresenceEvent_Closed:
		if payload.Closed == nil || payload.Closed.GetReason() == "" {
			return nil, protocolError("Hub returned an invalid presence close event")
		}
		payload.Closed.Reason = "managed cloud presence closed"
	default:
		return nil, protocolError("Hub returned an unknown presence event")
	}
	return cloned, nil
}

func sanitizeSignalingEvent(event *cloudpb.SignalingEvent) (*cloudpb.SignalingEvent, error) {
	if event == nil {
		return nil, protocolError("Hub returned an empty signaling event")
	}
	cloned := cloneMessage(event)
	switch payload := cloned.GetPayload().(type) {
	case *cloudpb.SignalingEvent_Answer:
		if payload.Answer == nil || payload.Answer.GetSignalingSessionId() == "" || payload.Answer.GetSdp() == "" {
			return nil, protocolError("Hub returned an invalid signaling answer")
		}
		if err := validateCandidates(payload.Answer.GetCandidates()); err != nil {
			return nil, err
		}
	case *cloudpb.SignalingEvent_Candidate:
		if payload.Candidate == nil || payload.Candidate.GetCandidate() == "" {
			return nil, protocolError("Hub returned an invalid client ICE candidate")
		}
	case *cloudpb.SignalingEvent_Error:
		if payload.Error == nil || !validCloudErrorCode(payload.Error.GetCode(), false) {
			return nil, protocolError("Hub returned an invalid signaling error")
		}
		payload.Error.Message = "managed cloud signaling failed"
	case *cloudpb.SignalingEvent_Closed:
		if payload.Closed == nil || payload.Closed.GetReason() == "" {
			return nil, protocolError("Hub returned an invalid signaling close event")
		}
		payload.Closed.Reason = "managed cloud signaling closed"
	default:
		return nil, protocolError("Hub returned an unknown signaling event")
	}
	return cloned, nil
}

func validateResolveRequest(request *cloudpb.ResolveEndpointRequest) error {
	if request == nil || request.GetEndpointId() == "" || request.GetTargetDeviceId() == "" {
		return protocolError("invalid endpoint resolution request")
	}
	return nil
}

func validateResolvedEndpoint(request *cloudpb.ResolveEndpointRequest, response *cloudpb.ResolvedEndpoint) error {
	if response == nil || response.GetEndpointId() != request.GetEndpointId() || response.GetTargetDeviceId() != request.GetTargetDeviceId() || response.GetManagedSessionId() == "" || response.GetHubId() == "" || response.GetHubUrl() == "" {
		return protocolError("Control Plane returned an invalid endpoint resolution")
	}
	if response.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_ONLINE && response.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_OFFLINE {
		return protocolError("Control Plane returned an invalid presence state")
	}
	hubURL, err := url.Parse(response.GetHubUrl())
	if err != nil || hubURL.Scheme != "https" || hubURL.Host == "" || hubURL.User != nil {
		return protocolError("Control Plane returned an untrusted Hub URL")
	}
	return validateIceServers(response.GetIceServers())
}

func validatePresenceRequest(request *cloudpb.OpenPresenceRequest) error {
	proof := request.GetProof()
	metadata := request.GetMetadata()
	if request == nil || proof == nil || metadata == nil || proof.GetDeviceId() == "" || len(proof.GetDevicePublicKey()) == 0 || proof.GetChallengeId() == "" || len(proof.GetSignature()) == 0 || proof.GetSignedAtUnixNano() == 0 || metadata.GetPlatform() == "" || metadata.GetTermxVersion() == "" {
		return protocolError("invalid daemon presence request")
	}
	return nil
}

func validateSignalingRequest(request *cloudpb.CreateSignalingSessionRequest) error {
	if request == nil || request.GetEndpointId() == "" || request.GetManagedSessionId() == "" || request.GetTargetDeviceId() == "" || request.GetOfferSdp() == "" || !validRoutePreference(request.GetRoutePreference()) {
		return protocolError("invalid signaling session request")
	}
	return validateCandidates(request.GetCandidates())
}

func validateCompleteOfferRequest(request *cloudpb.CompleteSignalingOfferRequest) error {
	if request == nil || request.GetSignalingSessionId() == "" {
		return protocolError("invalid signaling offer result")
	}
	if request.GetAnswer() == nil && request.GetError() == nil || request.GetAnswer() != nil && request.GetError() != nil {
		return protocolError("signaling offer result must contain exactly one result")
	}
	if answer := request.GetAnswer(); answer != nil {
		if answer.GetSignalingSessionId() != request.GetSignalingSessionId() || answer.GetSdp() == "" {
			return protocolError("invalid signaling answer")
		}
		return validateCandidates(answer.GetCandidates())
	}
	if !validCloudErrorCode(request.GetError().GetCode(), false) {
		return protocolError("invalid signaling error code")
	}
	return nil
}

func validateRelayLeaseRequest(request *cloudpb.AcquireRelayLeaseRequest) error {
	if request == nil || request.GetManagedSessionId() == "" || request.GetTargetDeviceId() == "" || !validRoutePreference(request.GetRoutePreference()) || request.GetRoutePreference() == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
		return protocolError("invalid Relay lease request")
	}
	return nil
}

func validateRelayLeaseResponse(request *cloudpb.AcquireRelayLeaseRequest, response *cloudpb.RelayLease, now time.Time) error {
	if response == nil || response.GetLeaseId() == "" || len(response.GetSignedLease()) == 0 || response.GetExpiresAtUnix() <= uint64(now.Unix()) {
		return protocolError("Control Plane returned an invalid Relay lease")
	}
	switch response.GetPathKind() {
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		if response.GetRouteId() != "" || response.GetRouteVersion() != 0 || response.GetClientEdgeRelayId() != "" || response.GetDaemonEdgeRelayId() != "" || response.GetMaxInternalTransit() != 0 {
			return protocolError("single Relay lease contains mesh fields")
		}
	case cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH:
		if request.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR || response.GetRouteId() == "" || response.GetRouteVersion() == 0 || response.GetClientEdgeRelayId() == "" || response.GetDaemonEdgeRelayId() == "" || response.GetClientEdgeRelayId() == response.GetDaemonEdgeRelayId() || response.GetMaxInternalTransit() > 1 {
			return protocolError("Relay Mesh lease binding is invalid")
		}
	default:
		return protocolError("Relay lease path kind is invalid")
	}
	return validateIceServers(response.GetIceServers())
}

func validatePathQualityRequest(request *cloudpb.ReportPathQualityRequest) error {
	summary := request.GetSummary()
	if request == nil || summary == nil || summary.GetManagedSessionId() == "" || !validObservedPath(summary.GetObservedPath()) || summary.GetLossBasisPoints() > 10_000 || summary.GetNetworkClass() == "" {
		return protocolError("invalid path quality summary")
	}
	return nil
}

func validateConnectionOutcomeRequest(request *cloudpb.ReportConnectionOutcomeRequest) error {
	outcome := request.GetOutcome()
	if request == nil || outcome == nil || outcome.GetManagedSessionId() == "" || !validObservedPath(outcome.GetObservedPath()) || !validCloudErrorCode(outcome.GetErrorCode(), true) {
		return protocolError("invalid connection outcome")
	}
	return nil
}

func validateAdmission(admission cloudservice.HubAdmission, managedSessionID string, now time.Time) error {
	if admission.Reference == "" || admission.HubID == "" || admission.ManagedSessionID == "" || len(admission.TicketBytes()) == 0 || !now.Before(admission.ExpiresAt) {
		return protocolError("Control Plane returned an invalid Hub admission")
	}
	if managedSessionID != "" && admission.ManagedSessionID != managedSessionID {
		return protocolError("Hub admission managed session mismatch")
	}
	return nil
}

func validateIceServers(servers []*cloudpb.IceServer) error {
	for _, server := range servers {
		if server == nil || len(server.GetUrls()) == 0 {
			return protocolError("cloud response contains an invalid ICE server")
		}
		for _, url := range server.GetUrls() {
			lowerURL := strings.ToLower(url)
			if url == "" || !strings.HasPrefix(lowerURL, "stun:") && !strings.HasPrefix(lowerURL, "stuns:") && !strings.HasPrefix(lowerURL, "turn:") && !strings.HasPrefix(lowerURL, "turns:") {
				return protocolError("cloud response contains an empty ICE URL")
			}
			if (strings.HasPrefix(lowerURL, "turn:") || strings.HasPrefix(lowerURL, "turns:")) && (server.GetUsername() == "" || server.GetCredential() == "") {
				return protocolError("cloud response contains a TURN server without short credential")
			}
		}
	}
	return nil
}

func validateCandidates(candidates []*cloudpb.IceCandidate) error {
	for _, candidate := range candidates {
		if candidate == nil || candidate.GetCandidate() == "" {
			return protocolError("signaling request contains an invalid ICE candidate")
		}
	}
	return nil
}

func validObservedPath(path cloudpb.ObservedPath) bool {
	return path == cloudpb.ObservedPath_OBSERVED_PATH_DIRECT || path == cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY || path == cloudpb.ObservedPath_OBSERVED_PATH_RELAY_MESH
}

func validRoutePreference(preference cloudpb.RoutePreference) bool {
	return preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY || preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE || preference == cloudpb.RoutePreference_ROUTE_PREFERENCE_GLOBAL_ACCELERATOR
}

func validCloudErrorCode(code cloudpb.CloudErrorCode, allowUnspecified bool) bool {
	if allowUnspecified && code == cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
		return true
	}
	switch code {
	case cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_NOT_RUNNING,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_INCOMPATIBLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_UNTRUSTED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_REGION_UNAVAILABLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY:
		return true
	default:
		return false
	}
}
