package controller

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/muxvia/muxvia/private/cloud/companion/cloudservice/httpapi"
	"github.com/muxvia/muxvia/private/cloud/companion/session"
	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	mobileActivationTTL  = 10 * time.Minute
	mobileAccessTTL      = 30 * time.Minute
	maxMobileActivations = 1_000_000
)

var (
	errMobileActivationUnavailable = errors.New("mobile activation is unavailable")
	errMobileActivationPending     = errors.New("mobile activation is waiting for Web approval")
)

type mobileActivationFlow struct {
	flowID         string
	userCode       string
	ownerAccountID string
	clientDeviceID string
	clientMetadata *cloudpb.DeviceMetadata
	claimed        bool
	approved       bool
	completing     bool
	completed      *httpapi.SessionWire
	expiresAt      time.Time
	order          *list.Element
}

// mobileActivationService 是 Controller 内 Web 扫码登录的短时真值。
// Web 只持有 user code，App 认领后才得到 flow ID；批准、设备授权和 edge credential 签发均在这里完成。
type mobileActivationService struct {
	mu                 sync.Mutex
	flows              map[string]*mobileActivationFlow
	codes              map[string]string
	expiry             *list.List
	commerce           *cloudcommerce.Service
	activationStore    persistence.MobileActivationStore
	topology           *cloudtopology.Service
	registry           *hubregistry.Registry
	edgeIssuer         servicecredential.EdgeAccessIssuer
	preferredHubID     string
	daemonNotAfter     time.Time
	now                func() time.Time
	random             io.Reader
	notifyPolicyChange func(string)
}

func newMobileActivationService(commerce *cloudcommerce.Service, activationStore persistence.MobileActivationStore, topology *cloudtopology.Service, registry *hubregistry.Registry, issuer servicecredential.EdgeAccessIssuer, preferredHubID string, daemonNotAfter time.Time, now func() time.Time, notify func(string)) (*mobileActivationService, error) {
	if commerce == nil || activationStore == nil || topology == nil || registry == nil || now == nil || notify == nil {
		return nil, errMobileActivationUnavailable
	}
	if !daemonNotAfter.After(now().UTC()) {
		return nil, errMobileActivationUnavailable
	}
	return &mobileActivationService{
		flows: make(map[string]*mobileActivationFlow), codes: make(map[string]string), expiry: list.New(),
		commerce: commerce, activationStore: activationStore, topology: topology, registry: registry, edgeIssuer: issuer, preferredHubID: preferredHubID,
		daemonNotAfter: daemonNotAfter.UTC(), now: now, random: rand.Reader, notifyPolicyChange: notify,
	}, nil
}

// CreateMobileActivation 为当前已认证 Web 账号创建一次性二维码 locator。
func (service *mobileActivationService) CreateMobileActivation(ctx context.Context, accountID, userID string) (*cloudpb.MobileActivationProjection, error) {
	if accountID == "" || userID == "" {
		return nil, cloudcommerce.ErrUnauthorized
	}
	view, err := service.commerce.AccountCommerce(ctx, accountID)
	if err != nil || view.GetAccount().GetUserId() != userID {
		return nil, cloudcommerce.ErrUnauthorized
	}
	flowID, err := service.randomID("mobile")
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if len(service.flows) >= maxMobileActivations {
		return nil, errMobileActivationUnavailable
	}
	code, err := service.newCodeLocked()
	if err != nil {
		return nil, err
	}
	flow := &mobileActivationFlow{flowID: flowID, userCode: code, ownerAccountID: accountID, expiresAt: now.Add(mobileActivationTTL)}
	flow.order = service.expiry.PushBack(flow)
	service.flows[flowID], service.codes[code] = flow, flowID
	return mobileProjection(flow), nil
}

// InspectMobileActivation 返回同一账号可见的扫码状态，不暴露 flow ID 或任何 credential。
func (service *mobileActivationService) InspectMobileActivation(_ context.Context, accountID, userCode string) (*cloudpb.MobileActivationProjection, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(service.now().UTC())
	flow, ok := service.flowByCodeLocked(userCode)
	if !ok || flow.ownerAccountID != accountID {
		return nil, cloudcommerce.ErrNotFound
	}
	return mobileProjection(flow), nil
}

// ApproveMobileActivation 只批准已被手机认领且仍属于当前 Web 账号的 flow。
func (service *mobileActivationService) ApproveMobileActivation(_ context.Context, accountID, userCode string) (*cloudpb.MobileActivationApproveResponse, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(service.now().UTC())
	flow, ok := service.flowByCodeLocked(userCode)
	if !ok || flow.ownerAccountID != accountID || !flow.claimed || flow.approved {
		return nil, cloudcommerce.ErrNotFound
	}
	flow.approved = true
	return &cloudpb.MobileActivationApproveResponse{Approved: true}, nil
}

func (service *mobileActivationService) claim(request *cloudpb.ClaimMobileActivationRequest) (*cloudpb.LoginFlow, error) {
	if request == nil || !validMobileMetadata(request.GetClientMetadata()) || !validMobileClientDeviceID(request.GetClientDeviceId()) {
		return nil, errMobileActivationUnavailable
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	now := service.now().UTC()
	service.cleanupLocked(now)
	flow, ok := service.flowByCodeLocked(request.GetUserCode())
	if !ok || flow.claimed || flow.approved {
		return nil, errMobileActivationUnavailable
	}
	flow.claimed = true
	// 注册码只授权一次登录事务；安装级 client_device_id 才是 Web、Hub 和 App 目录中的机器真值。
	flow.clientDeviceID = request.GetClientDeviceId()
	flow.clientMetadata = proto.Clone(request.GetClientMetadata()).(*cloudpb.DeviceMetadata)
	return &cloudpb.LoginFlow{FlowId: flow.flowID, UserCode: flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), PollIntervalMillis: 1000}, nil
}

func (service *mobileActivationService) complete(ctx context.Context, request *cloudpb.CompleteLoginRequest) (httpapi.SessionWire, error) {
	if request == nil || request.GetFlowId() == "" {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	service.mu.Lock()
	service.cleanupLocked(service.now().UTC())
	flow, ok := service.flows[request.GetFlowId()]
	if ok && flow.completed != nil {
		wire := cloneSessionWire(*flow.completed)
		service.mu.Unlock()
		return wire, nil
	}
	if ok && flow.completing {
		service.mu.Unlock()
		return httpapi.SessionWire{}, errMobileActivationPending
	}
	flowCopy := cloneMobileActivationFlow(flow)
	if ok && flow.claimed && flow.approved {
		flow.completing = true
	}
	service.mu.Unlock()
	flow = flowCopy
	if ok && flow.claimed && !flow.approved {
		return httpapi.SessionWire{}, errMobileActivationPending
	}
	if !ok || !flow.claimed || !flow.approved {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	view, err := service.commerce.AccountCommerce(ctx, flow.ownerAccountID)
	if err != nil || view.GetAccount() == nil {
		return service.failCompletion(flow.flowID, errMobileActivationUnavailable)
	}
	account := view.GetAccount()
	nextOwnership := cloudtopology.DeviceOwnership{AccountID: account.GetAccountId(), DeviceID: flow.clientDeviceID, Kind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: account.GetAuthRevision()}
	var expectedOwnership *cloudtopology.DeviceOwnership
	if current, loadErr := service.topology.Device(ctx, flow.clientDeviceID); loadErr == nil {
		if current.AccountID != nextOwnership.AccountID || current.Kind != nextOwnership.Kind || len(current.PublicKey) != 0 {
			return service.failCompletion(flow.flowID, errMobileActivationUnavailable)
		}
		expectedOwnership = &current
	} else if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
		return service.failCompletion(flow.flowID, loadErr)
	}
	prepared, err := service.commerce.PrepareDeviceSession(account, flow.clientDeviceID, service.now().UTC())
	if err != nil {
		return service.failCompletion(flow.flowID, err)
	}
	wire, err := service.issueSession(account, view.GetSubscription(), view.GetPlan(), flow.clientDeviceID, prepared.Credential)
	if err != nil {
		return service.failCompletion(flow.flowID, err)
	}
	if err := service.activationStore.CommitMobileActivation(ctx, persistence.MobileActivationCommit{ExpectedOwnership: expectedOwnership, NextOwnership: nextOwnership, Session: prepared.Record, Audit: prepared.Audit}, service.now().UTC()); err != nil {
		return service.failCompletion(flow.flowID, err)
	}
	service.notifyPolicyChange(account.GetAccountId())
	service.mu.Lock()
	if current := service.flows[flow.flowID]; current != nil {
		current.completing = false
		stored := cloneSessionWire(wire)
		current.completed = &stored
	}
	service.mu.Unlock()
	return cloneSessionWire(wire), nil
}

func (service *mobileActivationService) failCompletion(flowID string, err error) (httpapi.SessionWire, error) {
	service.mu.Lock()
	if flow := service.flows[flowID]; flow != nil {
		flow.completing = false
	}
	service.mu.Unlock()
	return httpapi.SessionWire{}, err
}

func cloneSessionWire(wire httpapi.SessionWire) httpapi.SessionWire {
	clone := wire
	clone.AccessToken = append([]byte(nil), wire.AccessToken...)
	clone.RefreshToken = append([]byte(nil), wire.RefreshToken...)
	return clone
}

func (service *mobileActivationService) refreshSession(ctx context.Context, input httpapi.RefreshSessionWire) (httpapi.SessionWire, error) {
	if input.Kind != session.KindAccount && input.Kind != session.KindDevice || len(input.RefreshToken) < 32 {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	rotated, err := service.commerce.RefreshDeviceSession(ctx, input.RefreshToken)
	if err != nil || rotated.Credential == nil {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	credential := rotated.Credential
	device, err := service.topology.Device(ctx, rotated.ClientDeviceID)
	if err != nil || device.Revoked || device.AccountID != credential.GetAccount().GetAccountId() {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	view, err := service.commerce.AccountCommerce(ctx, device.AccountID)
	if err != nil || view.GetAccount().GetAuthRevision() != device.AuthEpoch {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	if input.Kind == session.KindAccount {
		if device.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT {
			return httpapi.SessionWire{}, errMobileActivationUnavailable
		}
		return service.issueSession(view.GetAccount(), view.GetSubscription(), view.GetPlan(), rotated.ClientDeviceID, credential)
	}
	if device.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	assignment, err := service.registry.Assignment(ctx, rotated.ClientDeviceID)
	if err != nil || assignment.Value.GetAccountId() != device.AccountID {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	return service.issueDaemonSession(view.GetAccount(), rotated.ClientDeviceID, assignment.Value, credential)
}

// issueDaemonSession 在 refresh 单次轮换后重新验证 assignment，并签发同一 Hub 的 daemon edge credential。
// 它不修改 DeviceIdentity、control enrollment 或 terminal grant；这些真值仍由 daemon 本地状态持有。
func (service *mobileActivationService) issueDaemonSession(account *cloudpb.AccountProjection, deviceID string, assignment *cloudpb.HubAssignment, credential *cloudpb.AccountSessionCredential) (httpapi.SessionWire, error) {
	if account == nil || assignment == nil || credential == nil || len(credential.GetRefreshToken()) < 32 {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	deployment, err := service.registry.Deployment(context.Background(), assignment.GetHubId())
	if err != nil || !deployment.IdentityApproved || !deployment.Enabled || deployment.Archived || deployment.PublicHubURL == "" {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	now := service.now().UTC()
	expiresAt := now.Add(enrollmentSessionTTL)
	assignmentExpiresAt := time.UnixMilli(assignment.GetExpiresAtUnixMillis()).UTC()
	if assignmentExpiresAt.Before(expiresAt) {
		expiresAt = assignmentExpiresAt
	}
	if service.daemonNotAfter.Before(expiresAt) {
		expiresAt = service.daemonNotAfter
	}
	if expiresAt.Sub(now) < time.Minute {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	tokenID, err := service.randomID("edge")
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	access, err := service.edgeIssuer.IssueEdgeAccessForPrincipal(tokenID, assignment.GetHubId(), account.GetAccountId(), deviceID, servicecredential.EdgePrincipalDaemon, account.GetAuthRevision(), expiresAt.Sub(now), now)
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	refreshToken := append([]byte(nil), credential.GetRefreshToken()...)
	clear(credential.AccessToken)
	clear(credential.RefreshToken)
	return httpapi.SessionWire{
		Kind: session.KindDevice, AccountID: account.GetAccountId(), AccountLabel: account.GetDisplayName(), DeviceID: deviceID,
		ExpiresAt: expiresAt.Unix(), AccessToken: access, RefreshToken: refreshToken, RefreshExpiresAt: credential.GetRefreshExpiresAtUnixMillis() / 1000,
		HubID: assignment.GetHubId(), HubURL: deployment.PublicHubURL, HubRegion: deployment.Metadata.GetRegion(), HubDirectoryVersion: deployment.DirectoryRevision,
	}, nil
}

func (service *mobileActivationService) issueSession(account *cloudpb.AccountProjection, subscription *cloudpb.SubscriptionProjection, plan *cloudpb.PlanDefinition, deviceID string, credential *cloudpb.AccountSessionCredential) (httpapi.SessionWire, error) {
	if account == nil || subscription == nil || plan == nil || subscription.GetAccountId() != account.GetAccountId() || subscription.GetPlanId() != plan.GetPlanId() || subscription.GetPlanVersion() != plan.GetPlanVersion() || plan.GetPresentation().GetName() == "" || subscription.GetRevision() == 0 || credential == nil || len(credential.GetRefreshToken()) < 32 {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	now := service.now().UTC()
	deployment, err := service.clientDeployment(context.Background())
	if err != nil {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	tokenID, err := service.randomID("edge")
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	access, err := service.edgeIssuer.IssueEdgeAccessWithDirectory(tokenID, deployment.Metadata.GetHubId(), deployment.PublicHubURL, deployment.Metadata.GetRegion(), deployment.DirectoryRevision, account.GetAccountId(), deviceID, servicecredential.EdgePrincipalClient, account.GetAuthRevision(), mobileAccessTTL, now)
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	refreshToken := append([]byte(nil), credential.GetRefreshToken()...)
	clear(credential.AccessToken)
	clear(credential.RefreshToken)
	return httpapi.SessionWire{
		Kind: session.KindAccount, AccountID: account.GetAccountId(), AccountLabel: account.GetDisplayName(), DeviceID: deviceID,
		ExpiresAt: now.Add(mobileAccessTTL).Unix(), AccessToken: access, RefreshToken: refreshToken,
		RefreshExpiresAt: credential.GetRefreshExpiresAtUnixMillis() / 1000, HubID: deployment.Metadata.GetHubId(), HubURL: deployment.PublicHubURL,
		HubRegion: deployment.Metadata.GetRegion(), HubDirectoryVersion: deployment.DirectoryRevision, PlanID: subscription.GetPlanId(), PlanName: plan.GetPresentation().GetName(),
		SubscriptionStatus: subscription.GetStatus().String(), SubscriptionRevision: subscription.GetRevision(),
	}, nil
}

func (service *mobileActivationService) clientDeployment(ctx context.Context) (hubregistry.Deployment, error) {
	deployments, err := service.registry.Deployments(ctx)
	if err != nil {
		return hubregistry.Deployment{}, err
	}
	var fallback *hubregistry.Deployment
	for index := range deployments {
		deployment := deployments[index]
		if !deployment.IdentityApproved || !deployment.Enabled || deployment.Draining || deployment.Archived || deployment.PublicHubURL == "" || deployment.DirectoryRevision == 0 {
			continue
		}
		if deployment.Metadata.GetHubId() == service.preferredHubID {
			return deployment, nil
		}
		if fallback == nil {
			fallback = &deployment
		}
	}
	if fallback == nil {
		return hubregistry.Deployment{}, errMobileActivationUnavailable
	}
	return *fallback, nil
}

func (service *mobileActivationService) registerHTTP(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/login/mobile/claim", func(w http.ResponseWriter, r *http.Request) {
		request := &cloudpb.ClaimMobileActivationRequest{}
		if !readMobileProto(w, r, request) {
			return
		}
		response, err := service.claim(request)
		if err == nil {
			scheme := r.Header.Get("X-Forwarded-Proto")
			if scheme == "" {
				scheme = "http"
			}
			host := r.Header.Get("X-Forwarded-Host")
			if host == "" {
				host = r.Host
			}
			response.VerificationUri = scheme + "://" + host + "/account"
		}
		writeMobileProto(w, response, err)
	})
	mux.HandleFunc("POST /v1/login/complete", func(w http.ResponseWriter, r *http.Request) {
		request := &cloudpb.CompleteLoginRequest{}
		if !readMobileProto(w, r, request) {
			return
		}
		wire, err := service.complete(r.Context(), request)
		writeMobileSession(w, wire, err)
	})
	mux.HandleFunc("POST /v1/sessions/refresh", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var input httpapi.RefreshSessionWire
		if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input) != nil {
			writeMobileError(w, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "invalid refresh request", false)
			return
		}
		wire, err := service.refreshSession(r.Context(), input)
		writeMobileSession(w, wire, err)
	})
}

func mobileProjection(flow *mobileActivationFlow) *cloudpb.MobileActivationProjection {
	state := cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE
	if flow.claimed {
		state = cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_APPROVAL
	}
	if flow.approved {
		state = cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_APPROVED
	}
	projection := &cloudpb.MobileActivationProjection{UserCode: flow.userCode, QrPayload: "muxvia-cloud-activate:v1:" + flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), State: state}
	if flow.clientMetadata != nil {
		projection.ClientMetadata = proto.Clone(flow.clientMetadata).(*cloudpb.DeviceMetadata)
	}
	return projection
}

func (service *mobileActivationService) flowByCodeLocked(raw string) (*mobileActivationFlow, bool) {
	code, err := normalizeOneTimeCode(raw, "MXA")
	if err != nil {
		return nil, false
	}
	flowID, ok := service.codes[code]
	flow, exists := service.flows[flowID]
	return flow, ok && exists
}

func (service *mobileActivationService) cleanupLocked(now time.Time) {
	for service.expiry.Len() > 0 {
		flow := service.expiry.Front().Value.(*mobileActivationFlow)
		if now.Before(flow.expiresAt) {
			return
		}
		service.removeMobileFlowLocked(flow)
	}
}

func (service *mobileActivationService) removeMobileFlowLocked(flow *mobileActivationFlow) {
	delete(service.flows, flow.flowID)
	delete(service.codes, flow.userCode)
	if flow.order != nil {
		service.expiry.Remove(flow.order)
		flow.order = nil
	}
}

func cloneMobileActivationFlow(flow *mobileActivationFlow) *mobileActivationFlow {
	if flow == nil {
		return nil
	}
	clone := *flow
	clone.order = nil
	clone.completed = nil
	if flow.clientMetadata != nil {
		clone.clientMetadata = proto.Clone(flow.clientMetadata).(*cloudpb.DeviceMetadata)
	}
	return &clone
}

func (service *mobileActivationService) newCodeLocked() (string, error) {
	for range 16 {
		candidate, err := newOneTimeCode(service.random, "MXA")
		if err != nil {
			return "", err
		}
		if _, exists := service.codes[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", errMobileActivationUnavailable
}

func (service *mobileActivationService) randomID(prefix string) (string, error) {
	data := make([]byte, 18)
	if _, err := io.ReadFull(service.random, data); err != nil {
		return "", err
	}
	return prefix + "-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(data), nil
}

func validMobileMetadata(value *cloudpb.DeviceMetadata) bool {
	return value != nil && strings.TrimSpace(value.GetDisplayName()) != "" && len(value.GetDisplayName()) <= 128 && strings.TrimSpace(value.GetPlatform()) != "" && len(value.GetPlatform()) <= 64 && strings.TrimSpace(value.GetMuxviaVersion()) != "" && len(value.GetMuxviaVersion()) <= 64
}

func validMobileClientDeviceID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 16 || len(value) > 96 || !strings.HasPrefix(value, "client-") {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func readMobileProto(w http.ResponseWriter, r *http.Request, target proto.Message) bool {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || proto.Unmarshal(body, target) != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		writeMobileError(w, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "invalid mobile activation request", false)
		return false
	}
	return true
}

func writeMobileProto(w http.ResponseWriter, value proto.Message, err error) {
	if err != nil {
		writeMobileError(w, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_LOGIN_REQUIRED, "mobile activation is unavailable", false)
		return
	}
	body, _ := proto.Marshal(value)
	w.Header().Set("Content-Type", cloudProtoMediaType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func writeMobileSession(w http.ResponseWriter, wire httpapi.SessionWire, err error) {
	if err != nil {
		if errors.Is(err, errMobileActivationPending) {
			writeMobileError(w, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "mobile activation is waiting for Web approval", true)
			return
		}
		writeMobileError(w, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "mobile cloud session is unavailable", false)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(wire)
}

func writeMobileError(w http.ResponseWriter, status int, code cloudpb.CloudErrorCode, message string, retryable bool) {
	body, _ := proto.Marshal(&cloudpb.CloudError{Code: code, Message: message, Retryable: retryable})
	w.Header().Set("Content-Type", cloudProtoMediaType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

var _ webcontroller.MobileActivationService = (*mobileActivationService)(nil)
