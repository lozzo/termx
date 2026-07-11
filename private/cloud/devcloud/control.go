package devcloud

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/admission"
	"github.com/lozzow/termx/private/cloud/control-plane/directory"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/presence"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	"github.com/lozzow/termx/private/cloud/control-plane/usage"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

func (state *serviceState) controlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(httpapi.ControlHealthPath, state.handleControlHealth)
	mux.HandleFunc(httpapi.ControlBeginLoginPath, state.handleBeginLogin)
	mux.HandleFunc(httpapi.ControlCompleteLoginPath, state.handleCompleteLogin)
	mux.HandleFunc(httpapi.ControlBeginEnrollmentPath, state.handleBeginEnrollment)
	mux.HandleFunc(httpapi.ControlCompleteEnrollmentPath, state.handleCompleteEnrollment)
	mux.HandleFunc(httpapi.ControlBeginPresencePath, state.handleBeginPresence)
	mux.HandleFunc(httpapi.ControlResolveEndpointPath, state.handleResolveEndpoint)
	mux.HandleFunc(httpapi.ControlPresenceAdmissionPath, state.handlePresenceAdmission)
	mux.HandleFunc(httpapi.ControlClientAdmissionPath, state.handleClientAdmission)
	mux.HandleFunc(httpapi.ControlAnswerAdmissionPath, state.handleAnswerAdmission)
	mux.HandleFunc(httpapi.ControlAcquireRelayLeasePath, state.handleAcquireRelayLease)
	mux.HandleFunc(controlRelayUsagePath, state.handleRelayUsage)
	return mux
}

func (state *serviceState) handleControlHealth(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodGet) || !requireNoAuthorization(writer, request) {
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (state *serviceState) handleBeginLogin(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	payload := &cloudpb.BeginLoginRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_BROWSER && payload.GetMethod() != cloudpb.LoginMethod_LOGIN_METHOD_DEVICE_CODE {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "dev login method is invalid", false)
		return
	}
	flowID, err := state.randomID("login")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev login could not start", true)
		return
	}
	userCodeID, err := state.randomID("code")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev login could not start", true)
		return
	}
	now := state.now().UTC()
	expiresAt := now.Add(loginTTL)
	state.mu.Lock()
	state.cleanupLocked(now)
	state.loginFlows[flowID] = loginFlow{expiresAt: expiresAt}
	state.mu.Unlock()
	writeProto(writer, http.StatusOK, &cloudpb.LoginFlow{
		FlowId: flowID, VerificationUri: "https://login.dev.invalid/device",
		UserCode: strings.ToUpper(userCodeID[len(userCodeID)-8:]), ExpiresAtUnix: uint64(expiresAt.Unix()), PollIntervalMillis: 100,
	})
}

func (state *serviceState) handleCompleteLogin(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	payload := &cloudpb.CompleteLoginRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetFlowId() == "" {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "dev login completion is invalid", false)
		return
	}
	now := state.now().UTC()
	state.mu.Lock()
	state.cleanupLocked(now)
	flow, ok := state.loginFlows[payload.GetFlowId()]
	if ok {
		delete(state.loginFlows, payload.GetFlowId())
	}
	state.mu.Unlock()
	if !ok || !now.Before(flow.expiresAt) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED, "dev login flow is unavailable", false)
		return
	}
	cloudSession, token, err := state.issueSession(session.KindAccount, devClientDeviceID)
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev account session could not be issued", true)
		return
	}
	defer clear(token)
	writeJSON(writer, http.StatusOK, sessionWire(cloudSession, token))
}

func (state *serviceState) handleBeginEnrollment(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	payload := &cloudpb.BeginDeviceEnrollmentRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	metadata := payload.GetMetadata()
	if subtle.ConstantTimeCompare([]byte(payload.GetOneTimeCode()), []byte(state.enrollmentCode)) != 1 || len(payload.GetDevicePublicKey()) != ed25519.PublicKeySize || metadata == nil || metadata.GetPlatform() == "" || metadata.GetTermxVersion() == "" {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment code or device metadata is invalid", false)
		return
	}
	flowID, err := state.randomID("enrollment")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev enrollment could not start", true)
		return
	}
	challengeID, err := state.randomID("challenge")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev enrollment could not start", true)
		return
	}
	challenge, err := state.randomBytes(32)
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev enrollment could not start", true)
		return
	}
	now := state.now().UTC()
	expiresAt := now.Add(enrollmentTTL)
	state.mu.Lock()
	state.cleanupLocked(now)
	if state.enrollmentClaimed {
		state.mu.Unlock()
		clear(challenge)
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment code was already used", false)
		return
	}
	state.enrollmentClaimed = true
	state.enrollmentFlows[flowID] = enrollmentFlow{
		challengeID: challengeID, challenge: append([]byte(nil), challenge...),
		publicKey: append([]byte(nil), payload.GetDevicePublicKey()...), metadata: proto.Clone(metadata).(*cloudpb.DeviceMetadata), expiresAt: expiresAt,
	}
	state.mu.Unlock()
	writeProto(writer, http.StatusOK, &cloudpb.DeviceEnrollmentChallenge{
		FlowId: flowID, ChallengeId: challengeID, Challenge: challenge, ExpiresAtUnix: uint64(expiresAt.Unix()),
	})
	clear(challenge)
}

func (state *serviceState) handleCompleteEnrollment(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	payload := &cloudpb.CompleteDeviceEnrollmentRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	proof := payload.GetProof()
	if payload.GetFlowId() == "" || proof == nil {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "dev enrollment proof is invalid", false)
		return
	}
	now := state.now().UTC()
	state.mu.Lock()
	state.cleanupLocked(now)
	flow, ok := state.enrollmentFlows[payload.GetFlowId()]
	if ok {
		delete(state.enrollmentFlows, payload.GetFlowId())
	}
	state.mu.Unlock()
	if !ok || !now.Before(flow.expiresAt) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment challenge is unavailable", false)
		return
	}
	defer clear(flow.challenge)
	defer clear(flow.publicKey)
	if proof.GetDeviceId() == "" || proof.GetChallengeId() != flow.challengeID || len(proof.GetDevicePublicKey()) != ed25519.PublicKeySize || subtle.ConstantTimeCompare(proof.GetDevicePublicKey(), flow.publicKey) != 1 || len(proof.GetSignature()) != ed25519.SignatureSize {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment proof was rejected", false)
		return
	}
	proofTime := signedAt(proof.GetSignedAtUnixNano())
	if proofTime.Before(now.Add(-enrollmentTTL)) || proofTime.After(now.Add(30*time.Second)) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment proof was rejected", false)
		return
	}
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{
		FlowId: payload.GetFlowId(), ChallengeId: flow.challengeID, Challenge: append([]byte(nil), flow.challenge...),
		DeviceId: proof.GetDeviceId(), DevicePublicKey: append([]byte(nil), flow.publicKey...), SignedAtUnixNano: proofTime.UnixNano(),
	})
	if err != nil || !ed25519.Verify(ed25519.PublicKey(flow.publicKey), signingBytes, proof.GetSignature()) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment proof was rejected", false)
		return
	}
	if err := state.directory.RegisterDevice(domain.DeviceRegistration{
		ID: proof.GetDeviceId(), AccountID: devAccountID, OwnerUserID: devUserID, Kind: domain.DeviceKindDaemon,
		Label: deviceLabel(flow.metadata, proof.GetDeviceId()), PublicKey: flow.publicKey,
		Fingerprint: fingerprint(flow.publicKey), RegisteredAt: now,
	}); err != nil {
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "dev daemon registration conflicted", false)
		return
	}
	cloudSession, token, err := state.issueSession(session.KindDevice, proof.GetDeviceId())
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev device session could not be issued", true)
		return
	}
	defer clear(token)
	writeJSON(writer, http.StatusOK, sessionWire(cloudSession, token))
}

func (state *serviceState) handleBeginPresence(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticate(writer, request, session.KindDevice)
	if !ok {
		return
	}
	payload := &cloudpb.BeginPresenceRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetDeviceId() != cloudSession.deviceID {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "presence device does not match device session", false)
		return
	}
	challenge, err := state.presence.Begin(request.Context(), cloudSession.accountID, cloudSession.deviceID)
	if err != nil {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "presence challenge was rejected", false)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.PresenceChallenge{
		PresenceSessionId: challenge.PresenceSessionID, ChallengeId: challenge.ChallengeID,
		Challenge: challenge.Value, ExpiresAtUnix: uint64(challenge.ExpiresAt.Unix()),
	})
}

func (state *serviceState) handleResolveEndpoint(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticate(writer, request, session.KindAccount)
	if !ok {
		return
	}
	payload := &cloudpb.ResolveEndpointRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetEndpointId() == "" || payload.GetTargetDeviceId() == "" {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed endpoint request is invalid", false)
		return
	}
	target, err := state.directory.Device(cloudSession.accountID, payload.GetTargetDeviceId())
	if err != nil || target.Kind != domain.DeviceKindDaemon || target.RevokedAt != nil {
		writeCloudError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "managed target device was not found", false)
		return
	}
	managedSessionID, err := state.randomID("managed")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "managed session could not be created", true)
		return
	}
	now := state.now().UTC()
	if err := state.directory.CreateManagedSession(domain.ManagedSession{
		ID: managedSessionID, AccountID: cloudSession.accountID, ClientDeviceID: cloudSession.deviceID,
		TargetDeviceID: target.ID, Hub: domain.HubAssignment{HubID: devHubID, Region: devRegion},
		CreatedAt: now, ExpiresAt: now.Add(managedTTL),
	}, now); err != nil {
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed session ownership was rejected", false)
		return
	}
	presenceState := cloudpb.PresenceState_PRESENCE_STATE_OFFLINE
	if state.hub.HasPresence(target.ID) {
		presenceState = cloudpb.PresenceState_PRESENCE_STATE_ONLINE
	}
	writeProto(writer, http.StatusOK, &cloudpb.ResolvedEndpoint{
		EndpointId: payload.GetEndpointId(), TargetDeviceId: target.ID, Presence: presenceState,
		HubId: devHubID, HubUrl: devPublicHubURL, ManagedSessionId: managedSessionID,
	})
}

func (state *serviceState) handleAcquireRelayLease(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticateKinds(writer, request, session.KindAccount, session.KindDevice)
	if !ok {
		return
	}
	payload := &cloudpb.AcquireRelayLeaseRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetManagedSessionId() == "" || payload.GetTargetDeviceId() == "" ||
		payload.GetRoutePreference() != cloudpb.RoutePreference_ROUTE_PREFERENCE_STANDARD_RELAY ||
		payload.GetPreferredRegion() != "" && payload.GetPreferredRegion() != devRegion {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "single Relay request is invalid", false)
		return
	}
	now := state.now().UTC()
	managedSession, err := state.directory.ManagedSession(cloudSession.accountID, payload.GetManagedSessionId(), now)
	if err != nil || managedSession.TargetDeviceID != payload.GetTargetDeviceId() ||
		cloudSession.kind == session.KindAccount && cloudSession.deviceID != managedSession.ClientDeviceID ||
		cloudSession.kind == session.KindDevice && cloudSession.deviceID != managedSession.TargetDeviceID {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay lease session binding was rejected", false)
		return
	}
	lease, activation, err := state.acquireSingleRelay(managedSession)
	if err != nil {
		state.writeRelayAcquireError(writer, err)
		return
	}
	// 同一 signed lease 的两套 TURN credential 不能同时出现在一个 caller response 中。
	credential := activation.ClientCredential
	if cloudSession.kind == session.KindDevice {
		credential = activation.DaemonCredential
	}
	if credential.Username == "" || credential.Password == "" {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "single Relay credential is unavailable", true)
		return
	}
	writeProto(writer, http.StatusOK, &cloudpb.RelayLease{
		LeaseId: lease.claims.LeaseID, SignedLease: lease.signedLease,
		ExpiresAtUnix: uint64(lease.claims.ExpiresAtUnix), PathKind: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY,
		IceServers: []*cloudpb.IceServer{{Urls: []string{state.relayControl.url}, Username: credential.Username, Credential: credential.Password}},
	})
}

func (state *serviceState) writeRelayAcquireError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, entitlement.ErrNotEntitled), errors.Is(err, entitlement.ErrEntitlementNotFound):
		writeCloudError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED, "Relay entitlement is unavailable", false)
	case errors.Is(err, entitlement.ErrQuotaPolicy):
		writeCloudError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED, "Relay quota policy rejected the request", false)
	case errors.Is(err, directory.ErrNotFound), errors.Is(err, directory.ErrOwnership), errors.Is(err, servicecredential.ErrCredentialBinding):
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay lease session binding was rejected", false)
	case errors.Is(err, cloudrelay.ErrLeaseRejected):
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ROUTE_UNAVAILABLE, "single Relay admission failed", true)
	default:
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "single Relay service is unavailable", true)
	}
}

func (state *serviceState) handleRelayUsage(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	event := usage.Event{}
	if !readJSON(writer, request, &event) {
		return
	}
	claims, ok := state.relayLeaseClaims(event.LeaseID)
	if !ok {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage lease binding was rejected", false)
		return
	}
	if _, err := state.relayControl.usageLedger.Apply(claims, event, state.now().UTC()); err != nil {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage event was rejected", false)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (state *serviceState) handlePresenceAdmission(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticate(writer, request, session.KindDevice)
	if !ok {
		return
	}
	payload := &cloudpb.OpenPresenceRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	proof := payload.GetProof()
	if proof == nil || payload.GetPresenceSessionId() == "" || proof.GetDeviceId() != cloudSession.deviceID {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "presence proof does not match device session", false)
		return
	}
	issued, err := state.presence.Issue(request.Context(), cloudSession.accountID, presence.Proof{
		PresenceSessionID: payload.GetPresenceSessionId(), ChallengeID: proof.GetChallengeId(),
		DeviceID: proof.GetDeviceId(), PublicKey: proof.GetDevicePublicKey(), Signature: proof.GetSignature(),
		SignedAt: signedAt(proof.GetSignedAtUnixNano()),
	})
	if err != nil {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "fresh presence proof was rejected", false)
		return
	}
	reference, err := state.randomID("admission")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "presence admission could not be issued", true)
		return
	}
	wire := httpapi.AdmissionWire{
		Reference: reference, HubID: devHubID, AccountID: cloudSession.accountID, DeviceID: cloudSession.deviceID,
		SessionKind: cloudservice.HubSessionPresence, SessionID: issued.PresenceSessionID,
		ExpiresAt: issued.ExpiresAt.Unix(), Ticket: issued.Ticket.Bytes(),
	}
	defer clear(wire.Ticket)
	writeJSON(writer, http.StatusOK, wire)
}

func (state *serviceState) handleClientAdmission(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticate(writer, request, session.KindAccount)
	if !ok {
		return
	}
	payload := &cloudpb.CreateSignalingSessionRequest{}
	if !readProto(writer, request, payload) {
		return
	}
	if payload.GetManagedSessionId() == "" || payload.GetTargetDeviceId() == "" || payload.GetOfferSdp() == "" {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "managed client admission request is invalid", false)
		return
	}
	managedSession, err := state.directory.ManagedSession(cloudSession.accountID, payload.GetManagedSessionId(), state.now().UTC())
	if err != nil || managedSession.ClientDeviceID != cloudSession.deviceID || managedSession.TargetDeviceID != payload.GetTargetDeviceId() {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "managed client admission binding was rejected", false)
		return
	}
	state.issueManagedAdmission(writer, request.Context(), managedSession, servicecredential.PrincipalClient, []servicecredential.HubOperation{
		servicecredential.HubOperationOffer, servicecredential.HubOperationCandidate,
	})
}

func (state *serviceState) handleAnswerAdmission(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	cloudSession, ok := state.authenticate(writer, request, session.KindDevice)
	if !ok {
		return
	}
	envelope := httpapi.AnswerAdmissionRequest{}
	if !readJSON(writer, request, &envelope) {
		return
	}
	if envelope.ManagedSessionID == "" {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "daemon answer admission request is invalid", false)
		return
	}
	payload := &cloudpb.CompleteSignalingOfferRequest{}
	if err := readProtoBytes(envelope.Payload, payload); err != nil || payload.GetSignalingSessionId() == "" || payload.GetAnswer() == nil && payload.GetError() == nil || payload.GetAnswer() != nil && payload.GetError() != nil {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "daemon answer admission request is invalid", false)
		return
	}
	managedSession, err := state.directory.ManagedSession(cloudSession.accountID, envelope.ManagedSessionID, state.now().UTC())
	if err != nil || managedSession.TargetDeviceID != cloudSession.deviceID {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "managed daemon admission binding was rejected", false)
		return
	}
	state.issueManagedAdmission(writer, request.Context(), managedSession, servicecredential.PrincipalDaemon, []servicecredential.HubOperation{
		servicecredential.HubOperationAnswer, servicecredential.HubOperationCandidate,
	})
}

func (state *serviceState) issueManagedAdmission(writer http.ResponseWriter, ctx context.Context, managedSession domain.ManagedSession, principal servicecredential.PrincipalKind, operations []servicecredential.HubOperation) {
	if err := ctx.Err(); err != nil {
		writeCloudError(writer, http.StatusRequestTimeout, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "managed admission request was canceled", true)
		return
	}
	ticketID, err := state.randomID("ticket")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "managed admission could not be issued", true)
		return
	}
	ticket, err := state.admission.Issue(admission.Command{
		TicketID: ticketID, AccountID: managedSession.AccountID, ManagedSessionID: managedSession.ID,
		PrincipalKind: principal, Operations: operations, TTL: admissionTTL,
	}, state.now().UTC())
	if err != nil {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "managed admission ownership was rejected", false)
		return
	}
	deviceID := managedSession.ClientDeviceID
	targetDeviceID := managedSession.TargetDeviceID
	if principal == servicecredential.PrincipalDaemon {
		deviceID = managedSession.TargetDeviceID
		targetDeviceID = ""
	}
	wire := httpapi.AdmissionWire{
		Reference: ticketID, HubID: managedSession.Hub.HubID, AccountID: managedSession.AccountID,
		DeviceID: deviceID, TargetDeviceID: targetDeviceID,
		SessionKind: cloudservice.HubSessionManaged, SessionID: managedSession.ID,
		ExpiresAt: state.now().UTC().Add(admissionTTL).Unix(), Ticket: ticket.Bytes(),
	}
	defer clear(wire.Ticket)
	writeJSON(writer, http.StatusOK, wire)
}

func sessionWire(cloudSession cloudSession, token []byte) httpapi.SessionWire {
	return httpapi.SessionWire{
		Kind: cloudSession.kind, AccountID: cloudSession.accountID, AccountLabel: cloudSession.accountLabel,
		DeviceID: cloudSession.deviceID, ExpiresAt: cloudSession.expiresAt.Unix(), AccessToken: token,
	}
}

func deviceLabel(metadata *cloudpb.DeviceMetadata, fallback string) string {
	if metadata != nil {
		if label := strings.TrimSpace(metadata.GetDisplayName()); label != "" {
			return label
		}
		if hostname := strings.TrimSpace(metadata.GetHostname()); hostname != "" {
			return hostname
		}
	}
	return fmt.Sprintf("Dev daemon %s", fallback)
}
