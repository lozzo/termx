package companion

import (
	"crypto/ed25519"
	"net/url"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion/pathquality"
)

func validateBeginLoginRequest(request *cloudpb.BeginLoginRequest) error {
	if request == nil || request.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_BROWSER && request.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE {
		return protocolError("invalid login method")
	}
	return nil
}

func validateLoginFlow(flow *cloudpb.LoginFlow, now time.Time) error {
	if flow == nil || flow.GetFlowId() == "" || flow.GetVerificationUri() == "" || flow.GetExpiresAtUnix() <= uint64(now.Unix()) || flow.GetPollIntervalMillis() == 0 || flow.GetPollIntervalMillis() > 60_000 {
		return protocolError("Control Plane returned an invalid login flow")
	}
	verificationURL, err := url.Parse(flow.GetVerificationUri())
	if err != nil || verificationURL.Scheme != "https" || verificationURL.Host == "" || verificationURL.User != nil {
		return protocolError("Control Plane returned an untrusted login URL")
	}
	return nil
}

func validateBeginEnrollmentRequest(request *cloudpb.BeginDeviceEnrollmentRequest) error {
	metadata := request.GetMetadata()
	if request == nil || strings.TrimSpace(request.GetOneTimeCode()) == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize || metadata == nil || metadata.GetPlatform() == "" || metadata.GetTermxVersion() == "" {
		return protocolError("invalid device enrollment request")
	}
	return nil
}

func validateEnrollmentChallenge(challenge *cloudpb.DeviceEnrollmentChallenge, now time.Time) error {
	if challenge == nil || challenge.GetFlowId() == "" || challenge.GetChallengeId() == "" || len(challenge.GetChallenge()) < 32 || len(challenge.GetChallenge()) > 256 || challenge.GetExpiresAtUnix() <= uint64(now.Unix()) {
		return protocolError("Control Plane returned an invalid enrollment challenge")
	}
	return nil
}

func validateCompleteEnrollmentRequest(request *cloudpb.CompleteDeviceEnrollmentRequest) error {
	proof := request.GetProof()
	if request == nil || request.GetFlowId() == "" || proof == nil || proof.GetDeviceId() == "" || len(proof.GetDevicePublicKey()) != ed25519.PublicKeySize || proof.GetChallengeId() == "" || len(proof.GetSignature()) != ed25519.SignatureSize || proof.GetSignedAtUnixNano() == 0 {
		return protocolError("invalid device enrollment proof")
	}
	return nil
}

func sanitizePresenceEvent(event *cloudpb.PresenceEvent, presenceSessionID, targetDeviceID string) (*cloudpb.PresenceEvent, error) {
	if event == nil {
		return nil, protocolError("Hub returned an empty presence event")
	}
	cloned := cloneMessage(event)
	switch payload := cloned.GetPayload().(type) {
	case *cloudpb.PresenceEvent_Ready:
		if payload.Ready == nil || payload.Ready.GetPresenceSessionId() != presenceSessionID || payload.Ready.GetHeartbeatSeconds() == 0 {
			return nil, protocolError("Hub returned an invalid presence ready event")
		}
		if err := validateIceServers(payload.Ready.GetIceServers()); err != nil {
			return nil, err
		}
	case *cloudpb.PresenceEvent_Offer:
		if payload.Offer == nil || payload.Offer.GetSignalingSessionId() == "" || payload.Offer.GetManagedSessionId() == "" || payload.Offer.GetSourceDeviceId() == "" || payload.Offer.GetTargetDeviceId() != targetDeviceID || payload.Offer.GetSdp() == "" {
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
	if request == nil || request.GetPresenceSessionId() == "" || proof == nil || metadata == nil || proof.GetDeviceId() == "" || len(proof.GetDevicePublicKey()) != ed25519.PublicKeySize || proof.GetChallengeId() == "" || len(proof.GetSignature()) != ed25519.SignatureSize || proof.GetSignedAtUnixNano() == 0 || metadata.GetPlatform() == "" || metadata.GetTermxVersion() == "" {
		return protocolError("invalid daemon presence request")
	}
	return nil
}

func validateBeginPresenceRequest(request *cloudpb.BeginPresenceRequest) error {
	if request == nil || request.GetDeviceId() == "" {
		return protocolError("invalid daemon presence challenge request")
	}
	return nil
}

func validatePresenceChallenge(challenge *cloudpb.PresenceChallenge, now time.Time) error {
	if challenge == nil || challenge.GetPresenceSessionId() == "" || challenge.GetChallengeId() == "" || len(challenge.GetChallenge()) < 32 || len(challenge.GetChallenge()) > 256 || challenge.GetExpiresAtUnix() <= uint64(now.Unix()) {
		return protocolError("Control Plane returned an invalid presence challenge")
	}
	return nil
}

func validateSignalingRequest(request *cloudpb.CreateSignalingSessionRequest) error {
	if request == nil || request.GetEndpointId() == "" || request.GetManagedSessionId() == "" || request.GetTargetDeviceId() == "" || request.GetOfferSdp() == "" || !validRoutePreference(request.GetRoutePreference()) ||
		request.GetRelayOnly() && request.GetRoutePreference() == cloudpb.RoutePreference_ROUTE_PREFERENCE_DIRECT_ONLY {
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

func validateManagedRoutePlanRequest(request *cloudpb.PlanManagedRouteRequest) error {
	if request == nil || !canonicalRouteIdentifier(request.GetEndpointId()) ||
		!canonicalRouteIdentifier(request.GetManagedSessionId()) || !canonicalRouteIdentifier(request.GetTargetDeviceId()) ||
		request.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_SMART_ROUTE {
		return protocolError("invalid managed route plan request")
	}
	return nil
}

func validateManagedRoutePlanResponse(request *cloudpb.PlanManagedRouteRequest, response *cloudpb.ManagedRoutePlan, now time.Time) error {
	if response == nil || !canonicalRouteIdentifier(response.GetPlanId()) ||
		response.GetManagedSessionId() != request.GetManagedSessionId() || response.GetTargetDeviceId() != request.GetTargetDeviceId() ||
		!validRouteSelectionReason(response.GetSelectionReason()) || response.GetValidUntilUnix() <= uint64(now.Unix()) ||
		response.GetValidUntilUnix() > uint64(now.Add(10*time.Minute).Unix()) {
		return protocolError("Control Plane returned an invalid managed route plan")
	}
	if err := validateIceServers(response.GetIceServers()); err != nil {
		return err
	}
	hasTURN := false
	for _, server := range response.GetIceServers() {
		for _, rawURL := range server.GetUrls() {
			lowerURL := strings.ToLower(strings.TrimSpace(rawURL))
			if strings.HasPrefix(lowerURL, "turn:") || strings.HasPrefix(lowerURL, "turns:") {
				hasTURN = true
			}
		}
	}
	switch response.GetSelectedPath() {
	case cloudpb.ObservedPath_OBSERVED_PATH_DIRECT:
		if response.GetRelayOnly() || response.GetRelayRegion() != "" || hasTURN {
			return protocolError("direct managed route plan contains Relay material")
		}
	case cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY:
		if !response.GetRelayOnly() || !hasTURN || !validRouteTag(response.GetRelayRegion()) {
			return protocolError("single-relay managed route plan is incomplete")
		}
	default:
		return protocolError("managed route plan path is unavailable in GA002")
	}
	return nil
}

func validatePathQualityRequest(request *cloudpb.ReportPathQualityRequest) error {
	if request == nil {
		return protocolError("invalid path quality summary")
	}
	if _, err := pathquality.Decode(request.GetSummary()); err != nil {
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

func validateAdmission(admission cloudservice.HubAdmission, sessionKind cloudservice.HubSessionKind, sessionID string, now time.Time) error {
	if admission.Reference == "" || admission.HubID == "" || admission.AccountID == "" || admission.DeviceID == "" || admission.SessionKind != sessionKind || admission.SessionID == "" || len(admission.TicketBytes()) == 0 || !now.Before(admission.ExpiresAt) {
		return protocolError("Control Plane returned an invalid Hub admission")
	}
	if sessionID == "" || admission.SessionID != sessionID {
		return protocolError("Hub admission session mismatch")
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

func validRouteSelectionReason(reason cloudpb.RouteSelectionReason) bool {
	return reason >= cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_INITIAL_BEST &&
		reason <= cloudpb.RouteSelectionReason_ROUTE_SELECTION_REASON_CURRENT_BEST
}

func canonicalRouteIdentifier(value string) bool {
	return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t ")
}

func validRouteTag(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	return true
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
