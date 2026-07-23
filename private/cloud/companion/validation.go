package companion

import (
	"crypto/ed25519"
	"net/url"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion/pathquality"
)

func validateBeginLoginRequest(request *cloudpb.BeginLoginRequest) error {
	if request == nil || request.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_BROWSER && request.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE {
		return protocolError("invalid login method")
	}
	return nil
}

func validateLoginFlow(flow *cloudpb.LoginFlow, now time.Time, allowPublicHTTP bool) error {
	if flow == nil || flow.GetFlowId() == "" || flow.GetVerificationUri() == "" || flow.GetExpiresAtUnix() <= uint64(now.Unix()) || flow.GetPollIntervalMillis() == 0 || flow.GetPollIntervalMillis() > 60_000 {
		return protocolError("Control Plane returned an invalid login flow")
	}
	verificationURL, err := url.Parse(flow.GetVerificationUri())
	if err != nil || verificationURL.Scheme != "https" && !(allowPublicHTTP && verificationURL.Scheme == "http") || verificationURL.Host == "" || verificationURL.User != nil {
		return protocolError("Control Plane returned an untrusted login URL")
	}
	return nil
}

func validateBeginEnrollmentRequest(request *cloudpb.BeginDeviceEnrollmentRequest) error {
	metadata := request.GetMetadata()
	if request == nil || strings.TrimSpace(request.GetOneTimeCode()) == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize || metadata == nil || metadata.GetPlatform() == "" || metadata.GetMuxviaVersion() == "" {
		return protocolError("invalid device enrollment request")
	}
	return nil
}

func validateEnrollmentChallenge(challenge *cloudpb.DeviceEnrollmentChallenge, now time.Time) error {
	if challenge == nil || challenge.GetFlowId() == "" || challenge.GetChallengeId() == "" || len(challenge.GetChallenge()) < 32 || len(challenge.GetChallenge()) > 256 || challenge.GetExpiresAtUnix() <= uint64(now.Unix()) || len(challenge.GetHubCandidates()) == 0 || len(challenge.GetHubCandidates()) > 100 {
		return protocolError("Control Plane returned an invalid enrollment challenge")
	}
	seen := make(map[string]bool, len(challenge.GetHubCandidates()))
	for _, candidate := range challenge.GetHubCandidates() {
		hubURL, hubErr := url.Parse(candidate.GetHubUrl())
		healthURL, healthErr := url.Parse(candidate.GetHealthUrl())
		if candidate == nil || candidate.GetHubId() == "" || candidate.GetRegion() == "" || seen[candidate.GetHubId()] || hubErr != nil || healthErr != nil || hubURL.Host == "" || healthURL.Host == "" || hubURL.User != nil || healthURL.User != nil {
			return protocolError("Control Plane returned an invalid enrollment Hub candidate")
		}
		seen[candidate.GetHubId()] = true
	}
	return nil
}

func validateCompleteEnrollmentRequest(request *cloudpb.CompleteDeviceEnrollmentRequest) error {
	proof := request.GetProof()
	if request == nil || request.GetFlowId() == "" || proof == nil || proof.GetDeviceId() == "" || len(proof.GetDevicePublicKey()) != ed25519.PublicKeySize || proof.GetChallengeId() == "" || len(proof.GetSignature()) != ed25519.SignatureSize || proof.GetSignedAtUnixNano() == 0 {
		return protocolError("invalid device enrollment proof")
	}
	seen := make(map[string]bool, len(request.GetHubObservations()))
	for _, observation := range request.GetHubObservations() {
		if observation == nil || observation.GetHubId() == "" || seen[observation.GetHubId()] || observation.GetReachable() != (observation.GetLatencyMillis() > 0) {
			return protocolError("invalid Hub reachability observation")
		}
		seen[observation.GetHubId()] = true
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

func validateManagedDevices(response *cloudpb.ListManagedDevicesResponse) error {
	if response == nil || response.GetDevices() == nil || len(response.GetDevices()) > 1024 {
		return protocolError("Hub returned an invalid managed device directory")
	}
	seen := make(map[string]struct{}, len(response.GetDevices()))
	for _, device := range response.GetDevices() {
		if device == nil || device.GetDeviceId() == "" || device.GetDisplayName() == "" || device.GetKind() != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT && device.GetKind() != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || device.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_OFFLINE && device.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_ONLINE || device.GetKind() == cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT && device.GetPresence() != cloudpb.PresenceState_PRESENCE_STATE_OFFLINE || device.GetRevoked() && device.GetPresence() == cloudpb.PresenceState_PRESENCE_STATE_ONLINE {
			return protocolError("Hub returned an invalid managed device directory")
		}
		if _, exists := seen[device.GetDeviceId()]; exists {
			return protocolError("Hub returned a duplicate managed device")
		}
		seen[device.GetDeviceId()] = struct{}{}
	}
	return nil
}

func validatePresenceRequest(request *cloudpb.OpenPresenceRequest) error {
	proof := request.GetProof()
	metadata := request.GetMetadata()
	if request == nil || request.GetPresenceSessionId() == "" || proof == nil || metadata == nil || proof.GetDeviceId() == "" || len(proof.GetDevicePublicKey()) != ed25519.PublicKeySize || proof.GetChallengeId() == "" || len(proof.GetSignature()) != ed25519.SignatureSize || proof.GetSignedAtUnixNano() == 0 || metadata.GetPlatform() == "" || metadata.GetMuxviaVersion() == "" {
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
		return protocolError("Hub returned an invalid presence challenge")
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

func validateDaemonRuntimeRequest(request *cloudpb.ReportDaemonRuntimeRequest, daemonDeviceID, hubID string) error {
	if request == nil || request.GetReportId() == "" || request.GetHubId() == "" || request.GetHubId() != hubID || request.GetAssignmentEpoch() == 0 || request.GetPresenceSessionId() == "" || request.GetDaemonRuntimeGeneration() == "" || request.GetPeerSessions() == nil {
		return protocolError("invalid daemon runtime report")
	}
	peer := request.GetPeerSessions()
	if peer.GetReportId() != request.GetReportId() || peer.GetDaemonDeviceId() != daemonDeviceID || peer.GetControlOwnerHubId() != request.GetHubId() || peer.GetAssignmentEpoch() != request.GetAssignmentEpoch() || peer.GetControlPresenceSessionId() != request.GetPresenceSessionId() || peer.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || peer.GetRegistryRevision() != request.GetRegistryRevision() || peer.GetObservedAtUnixMillis() <= 0 {
		return protocolError("daemon runtime session inventory does not match its envelope")
	}
	for _, sessionProjection := range peer.GetSessions() {
		target := sessionProjection.GetTarget()
		if sessionProjection == nil || target == nil || target.GetDaemonDeviceId() != daemonDeviceID || target.GetAssignmentEpoch() != request.GetAssignmentEpoch() || target.GetControlPresenceSessionId() != request.GetPresenceSessionId() || target.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || sessionProjection.GetControlOwnerHubId() != request.GetHubId() || target.GetManagedSessionId() == "" || target.GetSessionIncarnation() == 0 || sessionProjection.GetClientDeviceId() == "" || !validObservedPath(sessionProjection.GetObservedDataPath()) || sessionProjection.GetState() == cloudpb.ManagedPeerSessionState_MANAGED_PEER_SESSION_STATE_UNSPECIFIED {
			return protocolError("daemon runtime contains an invalid managed session")
		}
	}
	if access := request.GetTerminalAccesses(); access != nil {
		if access.GetReportId() != request.GetReportId() || access.GetDaemonDeviceId() != daemonDeviceID || access.GetControlOwnerHubId() != request.GetHubId() || access.GetAssignmentEpoch() != request.GetAssignmentEpoch() || access.GetControlPresenceSessionId() != request.GetPresenceSessionId() || access.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || access.GetRegistryRevision() != request.GetRegistryRevision() {
			return protocolError("daemon runtime access inventory does not match its envelope")
		}
	}
	return nil
}

func validateDaemonRuntimeResponse(request *cloudpb.ReportDaemonRuntimeRequest, response *cloudpb.ReportDaemonRuntimeResponse) error {
	if response == nil || response.GetReportId() != request.GetReportId() || response.GetDaemonRuntimeGeneration() != request.GetDaemonRuntimeGeneration() || response.GetAcceptedRegistryRevision() != request.GetRegistryRevision() {
		return protocolError("Hub returned an invalid daemon runtime acknowledgement")
	}
	return nil
}

func validateDaemonCommandResultRequest(request *cloudpb.ReportDaemonCommandResultRequest, daemonDeviceID string) error {
	result := request.GetResult()
	if request == nil || result == nil || daemonDeviceID == "" || result.GetCommandId() == "" || result.GetDaemonDeviceId() != daemonDeviceID || result.GetAssignmentEpoch() == 0 || result.GetPresenceSessionId() == "" || result.GetDaemonRuntimeGeneration() == "" || result.GetResultCode() == cloudpb.RuntimeCommandResultCode_RUNTIME_COMMAND_RESULT_CODE_UNSPECIFIED || result.GetCompletedAtUnixMillis() <= 0 {
		return protocolError("daemon command result is invalid")
	}
	sessionResult := result.GetManagedSessionId() != "" && result.GetSessionIncarnation() != 0 && result.GetOpaqueAccessReference() == ""
	accessResult := result.GetManagedSessionId() == "" && result.GetSessionIncarnation() == 0 && result.GetOpaqueAccessReference() != "" && result.GetAccessProjectionRevision() != 0
	if sessionResult == accessResult {
		return protocolError("daemon command result target is invalid")
	}
	return nil
}

func validateDaemonCommandResultResponse(request *cloudpb.ReportDaemonCommandResultRequest, response *cloudpb.ReportDaemonCommandResultResponse) error {
	if response == nil || response.GetAcceptedCommandId() == "" || response.GetAcceptedCommandId() != request.GetResult().GetCommandId() {
		return protocolError("Hub daemon command result acknowledgement is invalid")
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
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY,
		cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_AUTHORIZATION_REVOKED:
		return true
	default:
		return false
	}
}
