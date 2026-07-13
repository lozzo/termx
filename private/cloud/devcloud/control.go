package devcloud

import (
	"crypto/ed25519"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice"
	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/presence"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	cloudrelay "github.com/lozzow/termx/private/cloud/relay"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"github.com/lozzow/termx/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

func (state *serviceState) controlHandler() http.Handler {
	mux := http.NewServeMux()
	if state.webHandler != nil {
		mux.Handle("/api/", state.webHandler)
	}
	mux.HandleFunc(httpapi.ControlHealthPath, state.handleControlHealth)
	mux.HandleFunc(httpapi.ControlBeginLoginPath, state.handleBeginLogin)
	mux.HandleFunc(httpapi.ControlCompleteLoginPath, state.handleCompleteLogin)
	mux.HandleFunc(httpapi.ControlBeginEnrollmentPath, state.handleBeginEnrollment)
	mux.HandleFunc(httpapi.ControlCompleteEnrollmentPath, state.handleCompleteEnrollment)
	mux.HandleFunc(httpapi.ControlBeginPresencePath, state.handleBeginPresence)
	mux.HandleFunc(httpapi.ControlPresenceAdmissionPath, state.handlePresenceAdmission)
	mux.HandleFunc(controlRelayUsagePath, state.handleRelayUsage)
	mux.HandleFunc("/v1/internal/web/entitlements", state.handleWebEntitlement)
	return mux
}

type webEntitlementPublisher struct{ state *serviceState }

func (publisher webEntitlementPublisher) Activate(accountID, planID, orderID string, validUntil time.Time) error {
	return publisher.state.activateWebEntitlement(accountID, planID, orderID, validUntil)
}

// InspectDeviceLogin 实现浏览器对设备码的只读确认；flow ID 与客户端凭据不会进入 Web surface。
func (state *serviceState) InspectDeviceLogin(userCode string) (webcontroller.DeviceLoginRequest, error) {
	now := state.now().UTC()
	code := strings.ToUpper(strings.TrimSpace(userCode))
	state.mu.Lock()
	defer state.mu.Unlock()
	state.cleanupLocked(now)
	flowID, ok := state.loginCodes[code]
	flow, exists := state.loginFlows[flowID]
	if !ok || !exists || flow.accountID != "" || !now.Before(flow.expiresAt) {
		return webcontroller.DeviceLoginRequest{}, webcontroller.ErrUserCenterNotFound
	}
	return webcontroller.DeviceLoginRequest{UserCode: flow.userCode, ExpiresAt: flow.expiresAt}, nil
}

// ApproveDeviceLogin 把已认证浏览器账号绑定到一个待处理设备码，并发布账号授权投影。
func (state *serviceState) ApproveDeviceLogin(userCode, accountID string) error {
	if state.webCenter == nil {
		return webcontroller.ErrUserCenterNotFound
	}
	profile, err := state.webCenter.Profile(accountID)
	if err != nil {
		return err
	}
	now := state.now().UTC()
	code := strings.ToUpper(strings.TrimSpace(userCode))
	state.mu.Lock()
	defer state.mu.Unlock()
	state.cleanupLocked(now)
	flowID, ok := state.loginCodes[code]
	flow, exists := state.loginFlows[flowID]
	if !ok || !exists || flow.accountID != "" || !now.Before(flow.expiresAt) {
		return webcontroller.ErrUserCenterNotFound
	}
	oldRevision := state.edgeRevision
	_, accountExisted := state.webAccounts[profile.AccountID]
	flow.accountID = profile.AccountID
	flow.accountLabel = profile.DisplayName
	state.loginFlows[flowID] = flow
	state.webAccounts[profile.AccountID] = struct{}{}
	state.edgeRevision++
	if err := state.publishEdgeSnapshot(now); err != nil {
		flow.accountID, flow.accountLabel = "", ""
		state.loginFlows[flowID] = flow
		state.edgeRevision = oldRevision
		if !accountExisted {
			delete(state.webAccounts, profile.AccountID)
		}
		return err
	}
	return nil
}

// CreateEnrollmentCode 为当前浏览器账号创建单次、短期 daemon enrollment claim。
func (state *serviceState) CreateEnrollmentCode(accountID, userID string) (webcontroller.EnrollmentCode, error) {
	if state.webCenter == nil {
		return webcontroller.EnrollmentCode{}, webcontroller.ErrUserCenterNotFound
	}
	profile, err := state.webCenter.Profile(accountID)
	if err != nil || profile.UserID != userID {
		return webcontroller.EnrollmentCode{}, webcontroller.ErrUserCenterNotFound
	}
	if err := state.ensureDirectoryAccount(profile); err != nil {
		return webcontroller.EnrollmentCode{}, err
	}
	codeID, err := state.randomID("enroll")
	if err != nil {
		return webcontroller.EnrollmentCode{}, err
	}
	code := strings.ToUpper(codeID)
	expiresAt := state.now().UTC().Add(enrollmentTTL)
	state.mu.Lock()
	state.cleanupLocked(state.now().UTC())
	state.enrollmentClaims[code] = enrollmentClaim{accountID: accountID, userID: userID, expiresAt: expiresAt}
	state.mu.Unlock()
	return webcontroller.EnrollmentCode{Code: code, ExpiresAt: expiresAt}, nil
}

func (state *serviceState) ensureDirectoryAccount(profile webcontroller.UserProfile) error {
	state.mu.Lock()
	if _, ok := state.directoryAccounts[profile.AccountID]; ok {
		state.mu.Unlock()
		return nil
	}
	state.mu.Unlock()
	now := state.now().UTC()
	if err := state.directory.PutAccount(domain.Account{ID: profile.AccountID, DisplayName: profile.DisplayName, CreatedAt: now}); err != nil {
		return err
	}
	if err := state.directory.PutUser(domain.User{ID: profile.UserID, AccountID: profile.AccountID, Email: profile.Email, CreatedAt: now}); err != nil {
		return err
	}
	state.mu.Lock()
	state.directoryAccounts[profile.AccountID] = struct{}{}
	state.mu.Unlock()
	return nil
}

func (state *serviceState) initializeWeb(config Config) error {
	if config.WebAccountDBPath == "" && config.WebCatalogPath == "" {
		return nil
	}
	if config.WebAccountDBPath == "" || config.WebCatalogPath == "" {
		return fmt.Errorf("web account database and catalog must be configured together")
	}
	catalog, err := webcontroller.LoadCatalog(config.WebCatalogPath)
	if err != nil {
		return err
	}
	center, err := webcontroller.OpenUserCenterStore(config.WebAccountDBPath, state.now)
	if err != nil {
		return err
	}
	commerce, err := webcontroller.NewCommerceService([]byte("termx-staging-payment-secret-v1-32-bytes"), webEntitlementPublisher{state}, state.now)
	if err != nil {
		_ = center.Close()
		return err
	}
	commerce.AttachUserCenter(center)
	for _, accountID := range center.AccountIDs() {
		state.webAccounts[accountID] = struct{}{}
	}
	state.edgeRevision++
	if err := state.publishEdgeSnapshot(state.now().UTC()); err != nil {
		_ = center.Close()
		return err
	}
	for _, entitlement := range center.ActiveEntitlements(state.now().UTC()) {
		if err := state.activateWebEntitlement(entitlement.AccountID, entitlement.PlanID, entitlement.OrderID, entitlement.ValidUntil); err != nil {
			_ = center.Close()
			return err
		}
	}
	providers := []webcontroller.IdentityProvider{{ID: "github", Name: "GitHub", Configured: false}, {ID: "google", Name: "Google", Configured: false}}
	handler, err := webcontroller.BrowserHandler(webcontroller.BrowserConfig{Catalog: &catalog, Commerce: commerce, UserCenter: center, IdentityProviders: providers, RelayURL: state.relayControl.url, StagingLogin: config.WebStaging, SecureCookie: config.WebSecureCookie, DeviceAccess: state})
	if err != nil {
		_ = center.Close()
		return err
	}
	state.webCenter, state.webCommerce, state.webHandler = center, commerce, handler
	return nil
}

func (state *serviceState) handleWebEntitlement(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || request.Header.Get("X-TermX-Internal-Service") != "web-controller-staging-v1" {
		if request.Header.Get("X-TermX-Internal-Service") != "web-controller-staging-v1" {
			writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "internal Web Controller identity is invalid", false)
		}
		return
	}
	var input struct {
		AccountID  string    `json:"account_id"`
		PlanID     string    `json:"plan_id"`
		OrderID    string    `json:"order_id"`
		ValidUntil time.Time `json:"valid_until"`
	}
	if !readJSON(writer, request, &input) {
		return
	}
	if err := state.activateWebEntitlement(input.AccountID, input.PlanID, input.OrderID, input.ValidUntil); err != nil {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "web entitlement update is invalid", false)
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusNoContent)
}

func (state *serviceState) activateWebEntitlement(accountID, planID, orderID string, validUntil time.Time) error {
	now := state.now().UTC()
	if accountID == "" || planID != "pro" || orderID == "" || !validUntil.After(now) {
		return fmt.Errorf("invalid web entitlement")
	}
	state.mu.Lock()
	oldValid, existed, oldRevision := state.webEntitlements[accountID], false, state.edgeRevision
	_, existed = state.webEntitlements[accountID]
	state.webEntitlements[accountID] = validUntil.UTC()
	state.webAccounts[accountID] = struct{}{}
	state.edgeRevision++
	err := state.publishEdgeSnapshot(now)
	if err != nil {
		if existed {
			state.webEntitlements[accountID] = oldValid
		} else {
			delete(state.webEntitlements, accountID)
		}
		state.edgeRevision = oldRevision
	}
	state.mu.Unlock()
	return err
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
	userCode := strings.ToUpper(userCodeID[len(userCodeID)-8:])
	clientDeviceID, err := state.randomID("client")
	if err != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev login could not start", true)
		return
	}
	flow := loginFlow{userCode: userCode, clientDeviceID: clientDeviceID, expiresAt: expiresAt}
	if state.webCenter == nil {
		flow.accountID, flow.accountLabel, flow.clientDeviceID = devAccountID, devAccountLabel, devClientDeviceID
	}
	state.mu.Lock()
	state.cleanupLocked(now)
	state.loginFlows[flowID] = flow
	state.loginCodes[userCode] = flowID
	state.mu.Unlock()
	writeProto(writer, http.StatusOK, &cloudpb.LoginFlow{
		FlowId: flowID, VerificationUri: state.webPublicURL + "/device?code=" + url.QueryEscape(userCode),
		UserCode: userCode, ExpiresAtUnix: uint64(expiresAt.Unix()), PollIntervalMillis: 1000,
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
	if ok && flow.accountID != "" {
		delete(state.loginFlows, payload.GetFlowId())
		delete(state.loginCodes, flow.userCode)
	}
	state.mu.Unlock()
	if !ok || !now.Before(flow.expiresAt) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED, "dev login flow is unavailable", false)
		return
	}
	if flow.accountID == "" {
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "device login is waiting for browser approval", true)
		return
	}
	cloudSession, token, err := state.issueSession(session.KindAccount, flow.accountID, flow.accountLabel, flow.clientDeviceID)
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
	if len(payload.GetDevicePublicKey()) != ed25519.PublicKeySize || metadata == nil || metadata.GetPlatform() == "" || metadata.GetTermxVersion() == "" {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment code or device metadata is invalid", false)
		return
	}
	now := state.now().UTC()
	state.mu.Lock()
	state.cleanupLocked(now)
	claimCode, claim, claimOK := "", enrollmentClaim{}, false
	for candidate, current := range state.enrollmentClaims {
		if subtle.ConstantTimeCompare([]byte(payload.GetOneTimeCode()), []byte(candidate)) == 1 {
			claimCode, claim, claimOK = candidate, current, true
			break
		}
	}
	if !claimOK || claim.claimed || !now.Before(claim.expiresAt) {
		state.mu.Unlock()
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "dev enrollment code or device metadata is invalid", false)
		return
	}
	claim.claimed = true
	state.enrollmentClaims[claimCode] = claim
	state.mu.Unlock()
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
	expiresAt := now.Add(enrollmentTTL)
	state.mu.Lock()
	state.cleanupLocked(now)
	state.enrollmentFlows[flowID] = enrollmentFlow{
		challengeID: challengeID, challenge: append([]byte(nil), challenge...),
		publicKey: append([]byte(nil), payload.GetDevicePublicKey()...), metadata: proto.Clone(metadata).(*cloudpb.DeviceMetadata), accountID: claim.accountID, userID: claim.userID, expiresAt: expiresAt,
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
		ID: proof.GetDeviceId(), AccountID: flow.accountID, OwnerUserID: flow.userID, Kind: domain.DeviceKindDaemon,
		Label: deviceLabel(flow.metadata, proof.GetDeviceId()), PublicKey: flow.publicKey,
		Fingerprint: fingerprint(flow.publicKey), RegisteredAt: now,
	}); err != nil {
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "dev daemon registration conflicted", false)
		return
	}
	state.mu.Lock()
	state.edgeRevision++
	state.edgeDevices[proof.GetDeviceId()] = cloudhub.DeviceAuthorization{DeviceID: proof.GetDeviceId(), AccountID: flow.accountID, PublicKey: append([]byte(nil), flow.publicKey...)}
	edgeErr := state.publishEdgeSnapshot(now)
	state.mu.Unlock()
	if edgeErr != nil {
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "dev Hub authorization projection could not be published", true)
		return
	}
	if state.webCenter != nil {
		_ = state.webCenter.UpsertManagedNode(flow.accountID, proof.GetDeviceId(), deviceLabel(flow.metadata, proof.GetDeviceId()), true)
	}
	accountLabel := devAccountLabel
	if state.webCenter != nil {
		if profile, profileErr := state.webCenter.Profile(flow.accountID); profileErr == nil {
			accountLabel = profile.DisplayName
		}
	}
	cloudSession, token, err := state.issueSession(session.KindDevice, flow.accountID, accountLabel, proof.GetDeviceId())
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

func (state *serviceState) handleRelayUsage(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) || !requireNoAuthorization(writer, request) {
		return
	}
	record := cloudrelay.UsageRecord{}
	if !readJSON(writer, request, &record) {
		return
	}
	claims, err := servicecredential.VerifyRelayLeaseForService(state.relayControl.leaseKeyRing, record.SignedLease, devRelayLeaseIssuer, devRelayPool, time.Minute, state.now().UTC())
	if err != nil || claims.LeaseID != record.Event.LeaseID {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage lease binding was rejected", false)
		return
	}
	if _, err := state.relayControl.usageLedger.Apply(claims, record.Event, state.now().UTC()); err != nil {
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

func sessionWire(cloudSession cloudSession, token []byte) httpapi.SessionWire {
	return httpapi.SessionWire{
		Kind: cloudSession.kind, AccountID: cloudSession.accountID, AccountLabel: cloudSession.accountLabel,
		DeviceID: cloudSession.deviceID, ExpiresAt: cloudSession.expiresAt.Unix(), AccessToken: token,
		HubID: devHubID, HubURL: devPublicHubURL, HubRegion: devRegion, HubDirectoryVersion: 1,
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
