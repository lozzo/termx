package devcloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
)

func (state *serviceState) hubHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(httpapi.HubHealthPath, state.handleHubHealth)
	mux.HandleFunc(httpapi.HubOpenPresencePath, state.handleHubOpenPresence)
	mux.HandleFunc(httpapi.HubCreateSignalingPath, state.handleHubCreateSignaling)
	mux.HandleFunc(httpapi.HubCompleteSignalingPath, state.handleHubCompleteSignaling)
	mux.HandleFunc(httpapi.HubAcquireRelayLeasePath, state.handleHubAcquireRelayLease)
	return mux
}

func (state *serviceState) handleHubHealth(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodGet) || !requireNoAuthorization(writer, request) {
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (state *serviceState) handleHubOpenPresence(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	envelope, payload, ok := state.readHubRequest(writer, request, &cloudpb.OpenPresenceRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Admission.Ticket)
	defer clear(envelope.Payload)
	openRequest := payload.(*cloudpb.OpenPresenceRequest)
	if envelope.Admission.SessionKind != cloudservice.HubSessionPresence || envelope.Admission.SessionID != openRequest.GetPresenceSessionId() || envelope.Admission.TargetDeviceID != "" || openRequest.GetProof() == nil || envelope.Admission.DeviceID != openRequest.GetProof().GetDeviceId() {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub presence admission binding was rejected", false)
		return
	}
	presence, err := state.hub.OpenPresence(request.Context(), cloudhub.OpenPresenceRequest{
		Admission: envelope.Admission.Ticket, AccountID: envelope.Admission.AccountID,
		DeviceID: envelope.Admission.DeviceID, PresenceSession: envelope.Admission.SessionID,
	})
	if err != nil {
		mapHubError(writer, err)
		return
	}
	defer presence.Close()
	writer.Header().Set("Content-Type", httpapi.StreamMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	if err := httpapi.WriteFrame(writer, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{
		PresenceSessionId: envelope.Admission.SessionID, HeartbeatSeconds: 30,
	}}}); err != nil {
		return
	}
	flush(writer)
	for {
		event, err := presence.Receive(request.Context())
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				_ = httpapi.WriteFrame(writer, presenceErrorEvent(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub presence stream failed", true))
				flush(writer)
			}
			return
		}
		wire, ok := presenceEventToWire(event)
		if !ok {
			_ = httpapi.WriteFrame(writer, presenceErrorEvent(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub presence event is unsupported", false))
			flush(writer)
			return
		}
		if err := httpapi.WriteFrame(writer, wire); err != nil {
			return
		}
		flush(writer)
	}
}

func (state *serviceState) handleHubCreateSignaling(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	token, ok := readBearerToken(writer, request)
	if !ok {
		return
	}
	defer clear(token)
	envelope, payload, ok := state.readEdgeHubRequest(writer, request, &cloudpb.CreateSignalingSessionRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Payload)
	createRequest := payload.(*cloudpb.CreateSignalingSessionRequest)
	claims, err := state.edgeAuth.AuthorizeDirect(token, envelope.AccountID, envelope.DeviceID, createRequest.GetTargetDeviceId())
	if err != nil {
		mapHubError(writer, err)
		return
	}
	signalingSessionID, err := state.randomID("signal")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub signaling session could not be created", true)
		return
	}
	clientSession, err := state.hub.CreateEdgeSession(request.Context(), cloudhub.CreateEdgeSessionRequest{
		EdgeToken: token, AccountID: claims.AccountID,
		ClientDeviceID: claims.ClientDeviceID, ClientConnectionID: claims.TokenID, TargetDeviceID: createRequest.GetTargetDeviceId(), SignalingSessionID: signalingSessionID,
		RelayCorrelationID: createRequest.GetManagedSessionId(),
		SDP:                createRequest.GetOfferSdp(), Candidates: candidatesFromWire(createRequest.GetCandidates()),
		RoutePreference: cloudhub.RoutePreference(createRequest.GetRoutePreference()), RelayOnly: createRequest.GetRelayOnly(),
	})
	if err != nil {
		mapHubError(writer, err)
		return
	}
	defer clientSession.Close()
	writer.Header().Set("Content-Type", httpapi.StreamMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	flush(writer)
	for {
		event, err := clientSession.Receive(request.Context())
		if err != nil {
			return
		}
		wire, ok := clientEventToWire(event)
		if !ok {
			_ = httpapi.WriteFrame(writer, signalingErrorEvent(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling event is unsupported", false))
			flush(writer)
			return
		}
		if err := httpapi.WriteFrame(writer, wire); err != nil {
			return
		}
		flush(writer)
	}
}

func (state *serviceState) handleHubCompleteSignaling(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	token, ok := readBearerToken(writer, request)
	if !ok {
		return
	}
	defer clear(token)
	envelope, payload, ok := state.readEdgeHubRequest(writer, request, &cloudpb.CompleteSignalingOfferRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Payload)
	completeRequest := payload.(*cloudpb.CompleteSignalingOfferRequest)
	claims, err := state.edgeAuth.AuthorizeDaemon(token, envelope.AccountID, envelope.DeviceID)
	if err != nil || completeRequest.GetSignalingSessionId() == "" {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub daemon edge binding was rejected", false)
		return
	}
	if answer := completeRequest.GetAnswer(); answer != nil && completeRequest.GetError() == nil {
		if answer.GetSignalingSessionId() != completeRequest.GetSignalingSessionId() || answer.GetSdp() == "" {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling answer is invalid", false)
			return
		}
		if _, err := state.hub.CompleteEdgeAnswer(request.Context(), cloudhub.CompleteEdgeAnswerRequest{
			EdgeToken: token, AccountID: claims.AccountID,
			DaemonDeviceID:     claims.ClientDeviceID,
			SignalingSessionID: completeRequest.GetSignalingSessionId(), SDP: answer.GetSdp(),
			Candidates: candidatesFromWire(answer.GetCandidates()),
		}); err != nil {
			mapHubError(writer, err)
			return
		}
	} else if failure := completeRequest.GetError(); failure != nil && completeRequest.GetAnswer() == nil && validSignalingFailureCode(failure.GetCode()) {
		if err := state.hub.CompleteEdgeFailure(request.Context(), cloudhub.CompleteEdgeFailureRequest{
			EdgeToken: token, AccountID: claims.AccountID,
			DaemonDeviceID:     claims.ClientDeviceID,
			SignalingSessionID: completeRequest.GetSignalingSessionId(), Code: int32(failure.GetCode()), Retryable: failure.GetRetryable(),
		}); err != nil {
			mapHubError(writer, err)
			return
		}
	} else {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling result is invalid", false)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.CompleteSignalingOfferResponse{})
}

func (state *serviceState) handleHubAcquireRelayLease(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	token, ok := readBearerToken(writer, request)
	if !ok {
		return
	}
	defer clear(token)
	envelope, payload, ok := state.readEdgeHubRequest(writer, request, &cloudpb.AcquireRelayLeaseRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Payload)
	relayRequest := payload.(*cloudpb.AcquireRelayLeaseRequest)
	if relayRequest.GetManagedSessionId() == "" || relayRequest.GetTargetDeviceId() == "" || relayRequest.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || relayRequest.GetPreferredRegion() != "" && relayRequest.GetPreferredRegion() != devRegion {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "single Relay request is invalid", false)
		return
	}
	accountID, clientDeviceID, targetDeviceID := envelope.AccountID, envelope.DeviceID, relayRequest.GetTargetDeviceId()
	daemonPrincipal := false
	if _, err := state.edgeAuth.AuthorizeDirect(token, accountID, clientDeviceID, targetDeviceID); err != nil {
		if _, daemonErr := state.edgeAuth.AuthorizeDaemon(token, accountID, envelope.DeviceID); daemonErr != nil || envelope.DeviceID != targetDeviceID {
			writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay edge identity was rejected", false)
			return
		}
		daemonPrincipal = true
		state.relayControl.mu.Lock()
		current, exists := state.relayControl.sessions[relayRequest.GetManagedSessionId()]
		state.relayControl.mu.Unlock()
		if !exists || current.claims.AccountID != accountID || current.claims.TargetDeviceID != targetDeviceID {
			writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay session binding was rejected", false)
			return
		}
		clientDeviceID = current.claims.ClientDeviceID
	}
	lease, activation, err := state.acquireSingleRelay(accountID, relayRequest.GetManagedSessionId(), clientDeviceID, targetDeviceID)
	if err != nil {
		writeCloudError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED, "regional Relay budget rejected the request", false)
		return
	}
	credential := activation.ClientCredential
	if daemonPrincipal {
		credential = activation.DaemonCredential
	}
	if credential.Username == "" || credential.Password == "" {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "single Relay credential is unavailable", true)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.RelayLease{LeaseId: lease.claims.LeaseID, SignedLease: lease.signedLease, ExpiresAtUnix: uint64(lease.claims.ExpiresAtUnix), PathKind: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY, IceServers: []*cloudpb.IceServer{{Urls: []string{state.relayControl.url}, Username: credential.Username, Credential: credential.Password}}})
}

func (state *serviceState) readEdgeHubRequest(writer http.ResponseWriter, request *http.Request, payload any) (httpapi.EdgeHubRequest, any, bool) {
	envelope := httpapi.EdgeHubRequest{}
	if !readJSON(writer, request, &envelope) || envelope.AccountID == "" || envelope.DeviceID == "" || len(envelope.Payload) == 0 || request.Context().Err() != nil {
		return httpapi.EdgeHubRequest{}, nil, false
	}
	switch target := payload.(type) {
	case *cloudpb.CreateSignalingSessionRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling payload is invalid", false)
			return httpapi.EdgeHubRequest{}, nil, false
		}
	case *cloudpb.CompleteSignalingOfferRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub answer payload is invalid", false)
			return httpapi.EdgeHubRequest{}, nil, false
		}
	case *cloudpb.AcquireRelayLeaseRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub Relay payload is invalid", false)
			return httpapi.EdgeHubRequest{}, nil, false
		}
	default:
		writeCloudError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub edge payload type is invalid", false)
		return httpapi.EdgeHubRequest{}, nil, false
	}
	return envelope, payload, true
}

func (state *serviceState) readHubRequest(writer http.ResponseWriter, request *http.Request, payload any) (httpapi.HubRequest, any, bool) {
	envelope := httpapi.HubRequest{}
	if !readJSON(writer, request, &envelope) {
		return httpapi.HubRequest{}, nil, false
	}
	if envelope.Admission.Reference == "" || envelope.Admission.HubID != devHubID || envelope.Admission.AccountID == "" || envelope.Admission.DeviceID == "" || envelope.Admission.SessionID == "" || len(envelope.Admission.Ticket) == 0 || request.Context().Err() != nil || !state.now().UTC().Before(time.Unix(envelope.Admission.ExpiresAt, 0).UTC()) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub admission envelope is invalid", false)
		return httpapi.HubRequest{}, nil, false
	}
	switch target := payload.(type) {
	case *cloudpb.OpenPresenceRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub presence payload is invalid", false)
			return httpapi.HubRequest{}, nil, false
		}
	case *cloudpb.CreateSignalingSessionRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling payload is invalid", false)
			return httpapi.HubRequest{}, nil, false
		}
	case *cloudpb.CompleteSignalingOfferRequest:
		if err := readProtoBytes(envelope.Payload, target); err != nil {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub answer payload is invalid", false)
			return httpapi.HubRequest{}, nil, false
		}
	default:
		writeCloudError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub handler payload type is invalid", false)
		return httpapi.HubRequest{}, nil, false
	}
	return envelope, payload, true
}

func presenceEventToWire(event cloudhub.PresenceEvent) (*cloudpb.PresenceEvent, bool) {
	switch {
	case event.Offer != nil:
		return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{
			SignalingSessionId: event.Offer.SignalingSessionID, ManagedSessionId: event.Offer.ManagedSessionID,
			SourceDeviceId: event.Offer.SourceDeviceID, TargetDeviceId: event.Offer.TargetDeviceID,
			Sdp: event.Offer.SDP, Candidates: candidatesToWire(event.Offer.Candidates),
			RoutePreference: cloudpb.RoutePreference(event.Offer.RoutePreference), RelayOnly: event.Offer.RelayOnly,
		}}}, true
	case event.Closed != nil:
		return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: event.Closed.Reason}}}, true
	default:
		return nil, false
	}
}

func clientEventToWire(event cloudhub.ClientEvent) (*cloudpb.SignalingEvent, bool) {
	switch {
	case event.Answer != nil:
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{
			SignalingSessionId: event.Answer.SignalingSessionID, Sdp: event.Answer.SDP,
			Candidates: candidatesToWire(event.Answer.Candidates),
		}}}, true
	case event.Candidate != nil:
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Candidate{Candidate: candidateToWire(event.Candidate.Candidate)}}, true
	case event.Failure != nil && validSignalingFailureCode(cloudpb.CloudErrorCode(event.Failure.Code)):
		return signalingErrorEvent(cloudpb.CloudErrorCode(event.Failure.Code), "daemon could not establish managed transport", event.Failure.Retryable), true
	case event.Closed != nil:
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Closed{Closed: &cloudpb.SignalingClosed{Reason: event.Closed.Reason}}}, true
	default:
		return nil, false
	}
}

func candidatesFromWire(candidates []*cloudpb.IceCandidate) []cloudhub.Candidate {
	result := make([]cloudhub.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil {
			result = append(result, cloudhub.Candidate{})
			continue
		}
		result = append(result, cloudhub.Candidate{
			Candidate: candidate.GetCandidate(), SDPMid: candidate.GetSdpMid(),
			SDPMLineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment(),
		})
	}
	return result
}

func candidatesToWire(candidates []cloudhub.Candidate) []*cloudpb.IceCandidate {
	result := make([]*cloudpb.IceCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidateToWire(candidate))
	}
	return result
}

func candidateToWire(candidate cloudhub.Candidate) *cloudpb.IceCandidate {
	return &cloudpb.IceCandidate{
		Candidate: candidate.Candidate, SdpMid: candidate.SDPMid,
		SdpMlineIndex: candidate.SDPMLineIndex, UsernameFragment: candidate.UsernameFragment,
	}
}

func presenceErrorEvent(code cloudpb.CloudErrorCode, message string, retryable bool) *cloudpb.PresenceEvent {
	return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Error{Error: &cloudpb.CloudError{Code: code, Message: message, Retryable: retryable}}}
}

func signalingErrorEvent(code cloudpb.CloudErrorCode, message string, retryable bool) *cloudpb.SignalingEvent {
	return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Error{Error: &cloudpb.CloudError{Code: code, Message: message, Retryable: retryable}}}
}

func validSignalingFailureCode(code cloudpb.CloudErrorCode) bool {
	return code >= cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING && code <= cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY
}

func flush(writer http.ResponseWriter) {
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
