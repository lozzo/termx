package edge

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	"github.com/muxvia/muxvia/private/cloud/control-plane/relaylease"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/muxvia/muxvia/private/cloud/hub"
	cloudrelay "github.com/muxvia/muxvia/private/cloud/relay"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/remoteauth"
	"google.golang.org/protobuf/proto"
)

const maxHubRequestBytes = 4 << 20

type hubHTTPConfig struct {
	Hub           *cloudhub.Service
	Authorizer    *cloudhub.EdgeAuthorizer
	Projection    hubProjection
	HubID         string
	HubURL        string
	ControllerURL string
	Relay         *cloudrelay.Server
	RelayID       string
	Region        string
	HTTPClient    *http.Client
}

type hubHTTPHandler struct {
	hub           *cloudhub.Service
	authorizer    *cloudhub.EdgeAuthorizer
	projection    hubProjection
	hubID         string
	hubURL        string
	controllerURL string
	relay         *cloudrelay.Server
	relayID       string
	region        string
	httpClient    *http.Client
}

type hubProjection interface {
	Ready() bool
	ActiveAssignment(deviceID string) (uint64, bool)
}

// newHubHTTPHandler 建立 Edge 公网 Hub adapter。它只消费纯内存 projection 和 Hub Service，
// 不访问 Controller store，也不拥有 account、assignment、Presence 或 signaling 真值。
func newHubHTTPHandler(config hubHTTPConfig) http.Handler {
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	handler := &hubHTTPHandler{hub: config.Hub, authorizer: config.Authorizer, projection: config.Projection, hubID: config.HubID, hubURL: config.HubURL, controllerURL: strings.TrimSuffix(config.ControllerURL, "/"), relay: config.Relay, relayID: config.RelayID, region: config.Region, httpClient: client}
	mux := http.NewServeMux()
	mux.HandleFunc(httpapi.HubHealthPath, handler.handleHealth)
	mux.HandleFunc(httpapi.HubBeginPresencePath, handler.handleBeginPresence)
	mux.HandleFunc(httpapi.HubOpenPresencePath, handler.handleOpenPresence)
	mux.HandleFunc(httpapi.HubCreateSignalingPath, handler.handleCreateSignaling)
	mux.HandleFunc(httpapi.HubCompleteSignalingPath, handler.handleCompleteSignaling)
	mux.HandleFunc(httpapi.HubReportDaemonRuntimePath, handler.handleReportDaemonRuntime)
	mux.HandleFunc(httpapi.HubReportDaemonCommandResultPath, handler.handleReportDaemonCommandResult)
	mux.HandleFunc(httpapi.HubAcquireRelayLeasePath, handler.handleAcquireRelayLease)
	mux.HandleFunc(httpapi.HubResolveEndpointPath, handler.handleResolveEndpoint)
	mux.HandleFunc(httpapi.HubListManagedDevicesPath, handler.handleListManagedDevices)
	return mux
}

func (handler *hubHTTPHandler) handleReportDaemonCommandResult(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.ReportDaemonCommandResultRequest](writer, request, &cloudpb.ReportDaemonCommandResultRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	claims, err := handler.authorizer.AuthorizeDaemon(token, envelope.AccountID, envelope.DeviceID)
	result := payload.GetResult()
	if err != nil || claims.ClientDeviceID != envelope.DeviceID || result == nil || result.GetDaemonDeviceId() != envelope.DeviceID {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub daemon command result binding was rejected", false)
		return
	}
	response, err := handler.hub.ReportDaemonCommandResult(envelope.DeviceID, result)
	if err != nil {
		mapHubError(writer, err)
		return
	}
	writeHubProto(writer, http.StatusOK, response)
}

func (handler *hubHTTPHandler) handleReportDaemonRuntime(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.ReportDaemonRuntimeRequest](writer, request, &cloudpb.ReportDaemonRuntimeRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	claims, err := handler.authorizer.AuthorizeDaemon(token, envelope.AccountID, envelope.DeviceID)
	if err != nil || claims.ClientDeviceID != envelope.DeviceID || payload.GetHubId() != handler.hubID {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub daemon runtime binding was rejected", false)
		return
	}
	assignmentEpoch, assigned := handler.projection.ActiveAssignment(envelope.DeviceID)
	if !assigned || assignmentEpoch != payload.GetAssignmentEpoch() {
		writeHubError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "Hub daemon runtime assignment is stale", false)
		return
	}
	response, err := handler.hub.ReportDaemonRuntime(envelope.DeviceID, payload)
	if err != nil {
		mapHubError(writer, err)
		return
	}
	writeHubProto(writer, http.StatusOK, response)
}

func (handler *hubHTTPHandler) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if !requireHubMethod(writer, request, http.MethodGet) {
		return
	}
	if request.Header.Get("Authorization") != "" {
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "authorization is not accepted for this operation", false)
		return
	}
	if handler.projection == nil || !handler.projection.Ready() {
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub projection is unavailable", true)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (handler *hubHTTPHandler) handleBeginPresence(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.BeginPresenceRequest](writer, request, &cloudpb.BeginPresenceRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	if payload.GetDeviceId() == "" || payload.GetDeviceId() != envelope.DeviceID {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub presence identity was rejected", false)
		return
	}
	challenge, err := handler.hub.BeginEdgePresence(request.Context(), token, envelope.AccountID, envelope.DeviceID)
	if err != nil {
		mapHubError(writer, err)
		return
	}
	defer clear(challenge.Value)
	writeHubProto(writer, http.StatusOK, &cloudpb.PresenceChallenge{PresenceSessionId: challenge.PresenceSessionID, ChallengeId: challenge.ChallengeID, Challenge: append([]byte(nil), challenge.Value...), ExpiresAtUnix: uint64(challenge.ExpiresAt.Unix())})
}

func (handler *hubHTTPHandler) handleListManagedDevices(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.ListManagedDevicesRequest](writer, request, &cloudpb.ListManagedDevicesRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	if payload.GetSchemaVersion() != 1 {
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed device directory version is invalid", false)
		return
	}
	if _, err := handler.authorizer.AuthorizeClient(token, envelope.AccountID, envelope.DeviceID); err != nil {
		mapHubError(writer, err)
		return
	}
	devices := handler.authorizer.AccountDevices(envelope.AccountID)
	sort.Slice(devices, func(left, right int) bool { return devices[left].DeviceID < devices[right].DeviceID })
	response := &cloudpb.ListManagedDevicesResponse{}
	for _, device := range devices {
		kind := cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT
		presence := cloudpb.PresenceState_PRESENCE_STATE_OFFLINE
		fingerprint := ""
		if device.Kind == "daemon" {
			kind = cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON
			if len(device.PublicKey) != ed25519.PublicKeySize {
				writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed daemon identity projection is invalid", true)
				return
			}
			fingerprint = remoteauth.Fingerprint(ed25519.PublicKey(device.PublicKey))
			if !device.Revoked && handler.hub.HasPresence(device.DeviceID) {
				presence = cloudpb.PresenceState_PRESENCE_STATE_ONLINE
			}
		}
		response.Devices = append(response.Devices, &cloudpb.ManagedDevice{DeviceId: device.DeviceID, DisplayName: device.DisplayName, Platform: device.Platform, Kind: kind, Presence: presence, Revoked: device.Revoked, DeviceFingerprint: fingerprint})
	}
	writeHubProto(writer, http.StatusOK, response)
}

func (handler *hubHTTPHandler) handleOpenPresence(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.OpenPresenceRequest](writer, request, &cloudpb.OpenPresenceRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	proof := payload.GetProof()
	if proof == nil || envelope.DeviceID != proof.GetDeviceId() {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub presence proof binding was rejected", false)
		return
	}
	presence, err := handler.hub.OpenEdgePresence(request.Context(), token, envelope.AccountID, cloudhub.EdgePresenceProof{PresenceSessionID: payload.GetPresenceSessionId(), ChallengeID: proof.GetChallengeId(), DeviceID: proof.GetDeviceId(), PublicKey: append([]byte(nil), proof.GetDevicePublicKey()...), Signature: append([]byte(nil), proof.GetSignature()...), SignedAt: time.Unix(0, proof.GetSignedAtUnixNano()).UTC()})
	if err != nil {
		mapHubError(writer, err)
		return
	}
	defer presence.Close()
	assignmentEpoch, assigned := handler.projection.ActiveAssignment(envelope.DeviceID)
	if !assigned {
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub assignment is unavailable", true)
		return
	}
	writer.Header().Set("Content-Type", httpapi.StreamMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	if err := httpapi.WriteFrame(writer, &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Ready{Ready: &cloudpb.PresenceReady{PresenceSessionId: payload.GetPresenceSessionId(), HeartbeatSeconds: 30, HubId: handler.hubID, AssignmentEpoch: assignmentEpoch}}}); err != nil {
		return
	}
	flushHub(writer)
	for {
		event, err := presence.Receive(request.Context())
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
				_ = httpapi.WriteFrame(writer, hubPresenceError(cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub presence stream failed", true))
				flushHub(writer)
			}
			return
		}
		wire, valid := presenceEventToWire(event)
		if !valid || httpapi.WriteFrame(writer, wire) != nil {
			return
		}
		flushHub(writer)
	}
}

func (handler *hubHTTPHandler) handleCreateSignaling(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.CreateSignalingSessionRequest](writer, request, &cloudpb.CreateSignalingSessionRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	claims, err := handler.authorizer.AuthorizeDirect(token, envelope.AccountID, envelope.DeviceID, payload.GetTargetDeviceId())
	if err != nil {
		mapHubError(writer, err)
		return
	}
	signalingSessionID, err := randomHubID("signal")
	if err != nil {
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub signaling session could not be created", true)
		return
	}
	session, err := handler.hub.CreateEdgeSession(request.Context(), cloudhub.CreateEdgeSessionRequest{EdgeToken: token, AccountID: claims.AccountID, ClientDeviceID: claims.ClientDeviceID, ClientConnectionID: claims.TokenID, TargetDeviceID: payload.GetTargetDeviceId(), SignalingSessionID: signalingSessionID, RelayCorrelationID: payload.GetManagedSessionId(), SDP: payload.GetOfferSdp(), Candidates: candidatesFromWire(payload.GetCandidates()), RoutePreference: cloudhub.RoutePreference(payload.GetRoutePreference()), RelayOnly: payload.GetRelayOnly(), RelayTransport: cloudhub.RelayTransport(payload.GetRelayTransport())})
	if err != nil {
		mapHubError(writer, err)
		return
	}
	defer session.Close()
	writer.Header().Set("Content-Type", httpapi.StreamMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	flushHub(writer)
	for {
		event, err := session.Receive(request.Context())
		if err != nil {
			return
		}
		wire, valid := clientEventToWire(event)
		if !valid || httpapi.WriteFrame(writer, wire) != nil {
			return
		}
		flushHub(writer)
	}
}

func (handler *hubHTTPHandler) handleCompleteSignaling(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.CompleteSignalingOfferRequest](writer, request, &cloudpb.CompleteSignalingOfferRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	claims, err := handler.authorizer.AuthorizeDaemon(token, envelope.AccountID, envelope.DeviceID)
	if err != nil || payload.GetSignalingSessionId() == "" {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub daemon edge binding was rejected", false)
		return
	}
	if answer := payload.GetAnswer(); answer != nil && payload.GetError() == nil {
		if answer.GetSignalingSessionId() != payload.GetSignalingSessionId() || answer.GetSdp() == "" {
			writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling answer is invalid", false)
			return
		}
		_, err = handler.hub.CompleteEdgeAnswer(request.Context(), cloudhub.CompleteEdgeAnswerRequest{EdgeToken: token, AccountID: claims.AccountID, DaemonDeviceID: claims.ClientDeviceID, SignalingSessionID: payload.GetSignalingSessionId(), SDP: answer.GetSdp(), Candidates: candidatesFromWire(answer.GetCandidates())})
	} else if failure := payload.GetError(); failure != nil && payload.GetAnswer() == nil && validSignalingFailureCode(failure.GetCode()) {
		err = handler.hub.CompleteEdgeFailure(request.Context(), cloudhub.CompleteEdgeFailureRequest{EdgeToken: token, AccountID: claims.AccountID, DaemonDeviceID: claims.ClientDeviceID, SignalingSessionID: payload.GetSignalingSessionId(), Code: int32(failure.GetCode()), Retryable: failure.GetRetryable()})
	} else {
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling result is invalid", false)
		return
	}
	if err != nil {
		mapHubError(writer, err)
		return
	}
	writeHubProto(writer, http.StatusOK, &cloudpb.CompleteSignalingOfferResponse{})
}

func (handler *hubHTTPHandler) handleAcquireRelayLease(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.AcquireRelayLeaseRequest](writer, request, &cloudpb.AcquireRelayLeaseRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	if handler.relay == nil || handler.controllerURL == "" || handler.relayID == "" || handler.region == "" || payload.GetManagedSessionId() == "" || payload.GetTargetDeviceId() == "" || payload.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY || payload.GetPreferredRegion() != "" && payload.GetPreferredRegion() != handler.region {
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay lease request is invalid", false)
		return
	}
	intent, err := handler.hub.RelayIntent(payload.GetManagedSessionId(), envelope.DeviceID)
	if err != nil || intent.AccountID != envelope.AccountID || intent.TargetDeviceID != payload.GetTargetDeviceId() {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay intent binding was rejected", false)
		return
	}
	principalClient := envelope.DeviceID == intent.ClientDeviceID
	if principalClient {
		if _, err := handler.authorizer.AuthorizeClient(token, envelope.AccountID, envelope.DeviceID); err != nil {
			mapHubError(writer, err)
			return
		}
	} else if envelope.DeviceID == intent.TargetDeviceID {
		if _, err := handler.authorizer.AuthorizeDaemon(token, envelope.AccountID, envelope.DeviceID); err != nil {
			mapHubError(writer, err)
			return
		}
	} else {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay principal binding was rejected", false)
		return
	}
	budget, err := handler.authorizer.RelayBudget(envelope.AccountID)
	if err != nil {
		mapHubError(writer, err)
		return
	}
	reserve := &cloudpb.ReserveRelayLeaseRequest{AccountId: envelope.AccountID, ManagedSessionId: payload.GetManagedSessionId(), ClientDeviceId: intent.ClientDeviceID, TargetDeviceId: intent.TargetDeviceID, HubId: handler.hubID, RelayId: handler.relayID, Region: handler.region, LeaseId: intent.LeaseID}
	signed, status, cloudErr := handler.reserveRelayLease(request.Context(), token, reserve)
	if cloudErr != nil {
		writeHubProto(writer, status, cloudErr)
		return
	}
	if err := handler.hub.BindRelayIntentLease(payload.GetManagedSessionId(), signed.GetLeaseId(), time.Unix(int64(signed.GetExpiresAtUnix()), 0).UTC()); err != nil {
		writeHubError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "Relay lease intent became stale", true)
		return
	}
	activation, err := handler.relay.ActivateLease(cloudrelay.ActivationRequest{SignedLease: signed.GetSignedLease(), AccountID: envelope.AccountID, ManagedSessionID: payload.GetManagedSessionId(), ClientDeviceID: intent.ClientDeviceID, TargetDeviceID: intent.TargetDeviceID, PathKind: servicecredential.RelayPathSingle})
	if err != nil || activation.Claims.MaxBytes > budget.MaxBytes || activation.Claims.MaxBitrateKbps > budget.MaxBitrateKbps || activation.Claims.MaxConcurrency > budget.MaxConcurrency || time.Unix(activation.Claims.ExpiresAtUnix, 0).Sub(time.Unix(activation.Claims.NotBeforeUnix, 0)) > budget.MaxLeaseDuration {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay lease verification was rejected", false)
		return
	}
	credential := activation.DaemonCredential
	if principalClient {
		credential = activation.ClientCredential
	}
	if credential.Username == "" || credential.Password == "" || !time.Now().UTC().Before(credential.ExpiresAt) {
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Relay credential is unavailable", true)
		return
	}
	response := proto.Clone(signed).(*cloudpb.RelayLease)
	// Edge Relay 是 transport truth：同一 caller-specific credential 同时下发 UDP 首选和 TCP fallback。
	response.IceServers = []*cloudpb.IceServer{{Urls: handler.relay.URLs(), Username: credential.Username, Credential: credential.Password}}
	writeHubProto(writer, http.StatusOK, response)
}

func (handler *hubHTTPHandler) reserveRelayLease(ctx context.Context, token []byte, payload *cloudpb.ReserveRelayLeaseRequest) (*cloudpb.RelayLease, int, *cloudpb.CloudError) {
	wire, err := proto.Marshal(payload)
	if err != nil {
		return nil, http.StatusInternalServerError, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, Message: "Relay reservation encoding failed", Retryable: true}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, handler.controllerURL+relaylease.InternalReservePath, bytes.NewReader(wire))
	if err != nil {
		return nil, http.StatusServiceUnavailable, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, Message: "Relay reservation service is unavailable", Retryable: true}
	}
	request.Header.Set("Content-Type", httpapi.ProtobufMediaType)
	request.Header.Set("Authorization", "Bearer "+base64.RawURLEncoding.EncodeToString(token))
	response, err := handler.httpClient.Do(request)
	if err != nil {
		return nil, http.StatusServiceUnavailable, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, Message: "Relay reservation service is unavailable", Retryable: true}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxHubRequestBytes))
	if err != nil || response.Header.Get("Content-Type") != httpapi.ProtobufMediaType {
		return nil, http.StatusServiceUnavailable, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, Message: "Relay reservation response is invalid"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		cloudErr := &cloudpb.CloudError{}
		if proto.Unmarshal(responseBody, cloudErr) != nil || cloudErr.GetCode() == cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNSPECIFIED {
			return nil, http.StatusServiceUnavailable, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, Message: "Relay reservation response is invalid"}
		}
		return nil, response.StatusCode, cloudErr
	}
	lease := &cloudpb.RelayLease{}
	if proto.Unmarshal(responseBody, lease) != nil || lease.GetLeaseId() == "" || len(lease.GetSignedLease()) == 0 || lease.GetPathKind() != cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY || len(lease.GetIceServers()) != 0 {
		return nil, http.StatusServiceUnavailable, &cloudpb.CloudError{Code: cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, Message: "Relay reservation response is invalid"}
	}
	return lease, http.StatusOK, nil
}

func (handler *hubHTTPHandler) handleResolveEndpoint(writer http.ResponseWriter, request *http.Request) {
	token, envelope, payload, ok := readHubRequest[*cloudpb.ResolveEndpointRequest](writer, request, &cloudpb.ResolveEndpointRequest{})
	if !ok {
		return
	}
	defer clear(token)
	defer clear(envelope.Payload)
	if payload.GetEndpointId() == "" || payload.GetTargetDeviceId() == "" {
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed endpoint request is invalid", false)
		return
	}
	if _, err := handler.authorizer.AuthorizeDirect(token, envelope.AccountID, envelope.DeviceID, payload.GetTargetDeviceId()); err != nil {
		mapHubError(writer, err)
		return
	}
	if !handler.hub.HasPresence(payload.GetTargetDeviceId()) {
		writeHubError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "target device is offline", true)
		return
	}
	correlationID, err := randomHubID("managed")
	if err != nil {
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub session correlation could not be created", true)
		return
	}
	if err := handler.hub.CreateRelayIntent(correlationID, envelope.AccountID, envelope.DeviceID, payload.GetTargetDeviceId()); err != nil {
		mapHubError(writer, err)
		return
	}
	writeHubProto(writer, http.StatusOK, &cloudpb.ResolvedEndpoint{EndpointId: payload.GetEndpointId(), TargetDeviceId: payload.GetTargetDeviceId(), Presence: cloudpb.PresenceState_PRESENCE_STATE_ONLINE, HubId: handler.hubID, HubUrl: handler.hubURL, ManagedSessionId: correlationID})
}

func readHubRequest[T proto.Message](writer http.ResponseWriter, request *http.Request, payload T) ([]byte, httpapi.EdgeHubRequest, T, bool) {
	var zero T
	if !requireHubMethod(writer, request, http.MethodPost) {
		return nil, httpapi.EdgeHubRequest{}, zero, false
	}
	token, ok := readHubBearer(writer, request)
	if !ok {
		return nil, httpapi.EdgeHubRequest{}, zero, false
	}
	if !contentTypeIs(request, httpapi.JSONMediaType) {
		clear(token)
		writeHubError(writer, http.StatusUnsupportedMediaType, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub request content type is invalid", false)
		return nil, httpapi.EdgeHubRequest{}, zero, false
	}
	reader := http.MaxBytesReader(writer, request.Body, maxHubRequestBytes)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		clear(token)
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub request is invalid", false)
		return nil, httpapi.EdgeHubRequest{}, zero, false
	}
	defer clear(body)
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var envelope httpapi.EdgeHubRequest
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF || envelope.AccountID == "" || envelope.DeviceID == "" || len(envelope.Payload) == 0 || len(envelope.Payload) > maxHubRequestBytes || proto.Unmarshal(envelope.Payload, payload) != nil || len(payload.ProtoReflect().GetUnknown()) != 0 {
		clear(token)
		clear(envelope.Payload)
		writeHubError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub request is invalid", false)
		return nil, httpapi.EdgeHubRequest{}, zero, false
	}
	return token, envelope, payload, true
}

func readHubBearer(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	header := request.Header.Get("Authorization")
	if strings.Count(header, " ") != 1 {
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub edge authorization is required", false)
		return nil, false
	}
	scheme, encoded, _ := strings.Cut(header, " ")
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if scheme != "Bearer" || err != nil || len(token) == 0 || len(token) > 4096 {
		clear(token)
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub edge authorization is invalid", false)
		return nil, false
	}
	return token, true
}

func requireHubMethod(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	writeHubError(writer, http.StatusMethodNotAllowed, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub method is not allowed", false)
	return false
}

func contentTypeIs(request *http.Request, expected string) bool {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == expected && len(parameters) == 0
}

func writeHubProto(writer http.ResponseWriter, status int, message proto.Message) {
	payload, err := proto.Marshal(message)
	if err != nil {
		writeHubError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub response encoding failed", true)
		return
	}
	writer.Header().Set("Content-Type", httpapi.ProtobufMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeHubError(writer http.ResponseWriter, status int, code cloudpb.CloudErrorCode, message string, retryable bool) {
	payload, _ := proto.Marshal(&cloudpb.CloudError{Code: code, Message: message, Retryable: retryable})
	writer.Header().Set("Content-Type", httpapi.ProtobufMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func mapHubError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudhub.ErrAdmission), errors.Is(err, cloudhub.ErrEdgeAuthentication), errors.Is(err, cloudhub.ErrEdgeAuthorization):
		writeHubError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub authorization was rejected", false)
	case errors.Is(err, cloudhub.ErrPolicySnapshot):
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub authorization projection is unavailable", true)
	case errors.Is(err, cloudhub.ErrPrincipalRevoked):
		writeHubError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_AUTHORIZATION_REVOKED, "Hub principal authorization was revoked", false)
	case errors.Is(err, cloudhub.ErrP2PNotEntitled), errors.Is(err, cloudhub.ErrRelayNotEntitled):
		writeHubError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED, "managed Cloud capability is not enabled for this account", false)
	case errors.Is(err, cloudhub.ErrP2PConcurrency):
		writeHubError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED, "managed P2P concurrency is exhausted", true)
	case errors.Is(err, cloudhub.ErrTargetUnavailable):
		writeHubError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "target managed device is unavailable", false)
	case errors.Is(err, cloudhub.ErrPresenceNotFound):
		writeHubError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "target device is offline", true)
	case errors.Is(err, cloudhub.ErrBackpressure), errors.Is(err, cloudhub.ErrCapacity):
		writeHubError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE, "Hub signaling capacity is unavailable", true)
	case errors.Is(err, cloudhub.ErrPresenceConflict), errors.Is(err, cloudhub.ErrSessionConflict), errors.Is(err, cloudhub.ErrSessionNotFound), errors.Is(err, cloudhub.ErrInvalidSignal):
		writeHubError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling binding was rejected", false)
	case errors.Is(err, cloudhub.ErrRuntimeReport):
		writeHubError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub daemon runtime report was rejected", false)
	default:
		writeHubError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub operation failed", true)
	}
}

func randomHubID(prefix string) (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Hub ID: %w", err)
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func presenceEventToWire(event cloudhub.PresenceEvent) (*cloudpb.PresenceEvent, bool) {
	switch {
	case event.Offer != nil:
		return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Offer{Offer: &cloudpb.SignalingOffer{SignalingSessionId: event.Offer.SignalingSessionID, ManagedSessionId: event.Offer.ManagedSessionID, SourceDeviceId: event.Offer.SourceDeviceID, TargetDeviceId: event.Offer.TargetDeviceID, Sdp: event.Offer.SDP, Candidates: candidatesToWire(event.Offer.Candidates), RoutePreference: cloudpb.RoutePreference(event.Offer.RoutePreference), RelayOnly: event.Offer.RelayOnly, SessionIncarnation: event.Offer.SessionIncarnation, PresenceSessionId: event.Offer.PresenceSessionID, AssignmentEpoch: event.Offer.AssignmentEpoch, RelayTransport: cloudpb.RelayTransport(event.Offer.RelayTransport)}}}, true
	case event.DaemonCommand != nil:
		return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_DaemonCommand{DaemonCommand: proto.Clone(event.DaemonCommand).(*cloudpb.DaemonControlCommand)}}, true
	case event.Closed != nil:
		return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Closed{Closed: &cloudpb.PresenceClosed{Reason: event.Closed.Reason}}}, true
	default:
		return nil, false
	}
}

func clientEventToWire(event cloudhub.ClientEvent) (*cloudpb.SignalingEvent, bool) {
	switch {
	case event.Answer != nil:
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Answer{Answer: &cloudpb.SignalingAnswer{SignalingSessionId: event.Answer.SignalingSessionID, Sdp: event.Answer.SDP, Candidates: candidatesToWire(event.Answer.Candidates)}}}, true
	case event.Candidate != nil:
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Candidate{Candidate: candidateToWire(event.Candidate.Candidate)}}, true
	case event.Failure != nil && validSignalingFailureCode(cloudpb.CloudErrorCode(event.Failure.Code)):
		return &cloudpb.SignalingEvent{Payload: &cloudpb.SignalingEvent_Error{Error: &cloudpb.CloudError{Code: cloudpb.CloudErrorCode(event.Failure.Code), Message: "daemon could not establish managed transport", Retryable: event.Failure.Retryable}}}, true
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
		result = append(result, cloudhub.Candidate{Candidate: candidate.GetCandidate(), SDPMid: candidate.GetSdpMid(), SDPMLineIndex: candidate.GetSdpMlineIndex(), UsernameFragment: candidate.GetUsernameFragment()})
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
	return &cloudpb.IceCandidate{Candidate: candidate.Candidate, SdpMid: candidate.SDPMid, SdpMlineIndex: candidate.SDPMLineIndex, UsernameFragment: candidate.UsernameFragment}
}

func hubPresenceError(code cloudpb.CloudErrorCode, message string, retryable bool) *cloudpb.PresenceEvent {
	return &cloudpb.PresenceEvent{Payload: &cloudpb.PresenceEvent_Error{Error: &cloudpb.CloudError{Code: code, Message: message, Retryable: retryable}}}
}

func validSignalingFailureCode(code cloudpb.CloudErrorCode) bool {
	return code >= cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_COMPANION_MISSING && code <= cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY
}

func flushHub(writer http.ResponseWriter) {
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}
