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
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	envelope, payload, ok := state.readHubRequest(writer, request, &cloudpb.CreateSignalingSessionRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Admission.Ticket)
	defer clear(envelope.Payload)
	createRequest := payload.(*cloudpb.CreateSignalingSessionRequest)
	if envelope.Admission.SessionKind != cloudservice.HubSessionManaged || envelope.Admission.SessionID != createRequest.GetManagedSessionId() || envelope.Admission.TargetDeviceID != createRequest.GetTargetDeviceId() {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub client admission binding was rejected", false)
		return
	}
	signalingSessionID, err := state.randomID("signal")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub signaling session could not be created", true)
		return
	}
	clientSession, err := state.hub.CreateSession(request.Context(), cloudhub.CreateSessionRequest{
		Admission: envelope.Admission.Ticket, AccountID: envelope.Admission.AccountID,
		ClientDeviceID: envelope.Admission.DeviceID, TargetDeviceID: createRequest.GetTargetDeviceId(),
		ManagedSessionID: createRequest.GetManagedSessionId(), SignalingSessionID: signalingSessionID,
		SDP: createRequest.GetOfferSdp(), Candidates: candidatesFromWire(createRequest.GetCandidates()),
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
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	envelope, payload, ok := state.readHubRequest(writer, request, &cloudpb.CompleteSignalingOfferRequest{})
	if !ok {
		return
	}
	defer clear(envelope.Admission.Ticket)
	defer clear(envelope.Payload)
	completeRequest := payload.(*cloudpb.CompleteSignalingOfferRequest)
	if envelope.Admission.SessionKind != cloudservice.HubSessionManaged || envelope.Admission.TargetDeviceID != "" || completeRequest.GetSignalingSessionId() == "" {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub daemon admission binding was rejected", false)
		return
	}
	if answer := completeRequest.GetAnswer(); answer != nil && completeRequest.GetError() == nil {
		if answer.GetSignalingSessionId() != completeRequest.GetSignalingSessionId() || answer.GetSdp() == "" {
			writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling answer is invalid", false)
			return
		}
		if _, err := state.hub.CompleteAnswer(request.Context(), cloudhub.CompleteAnswerRequest{
			Admission: envelope.Admission.Ticket, AccountID: envelope.Admission.AccountID,
			DaemonDeviceID: envelope.Admission.DeviceID, ManagedSessionID: envelope.Admission.SessionID,
			SignalingSessionID: completeRequest.GetSignalingSessionId(), SDP: answer.GetSdp(),
			Candidates: candidatesFromWire(answer.GetCandidates()),
		}); err != nil {
			mapHubError(writer, err)
			return
		}
	} else if failure := completeRequest.GetError(); failure != nil && completeRequest.GetAnswer() == nil && validSignalingFailureCode(failure.GetCode()) {
		if err := state.hub.CompleteFailure(request.Context(), cloudhub.CompleteFailureRequest{
			Admission: envelope.Admission.Ticket, AccountID: envelope.Admission.AccountID,
			DaemonDeviceID: envelope.Admission.DeviceID, ManagedSessionID: envelope.Admission.SessionID,
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
