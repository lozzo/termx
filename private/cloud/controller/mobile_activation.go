package controller

import (
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

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	cloudcommerce "github.com/lozzow/termx/private/cloud/control-plane/commerce"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const (
	mobileActivationTTL = 10 * time.Minute
	mobileAccessTTL     = 30 * time.Minute
	mobileCodeAlphabet  = "23456789ABCDEFGHJKMNPQRSTVWXYZ"
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
	expiresAt      time.Time
}

// mobileActivationService 是 Controller 内 Web 扫码登录的短时真值。
// Web 只持有 user code，App 认领后才得到 flow ID；批准、设备授权和 edge credential 签发均在这里完成。
type mobileActivationService struct {
	mu                 sync.Mutex
	flows              map[string]mobileActivationFlow
	codes              map[string]string
	commerce           *cloudcommerce.Service
	topology           *cloudtopology.Service
	edgeIssuer         servicecredential.EdgeAccessIssuer
	hubID              string
	hubURL             string
	hubRegion          string
	now                func() time.Time
	random             io.Reader
	notifyPolicyChange func(string)
}

func newMobileActivationService(commerce *cloudcommerce.Service, topology *cloudtopology.Service, issuer servicecredential.EdgeAccessIssuer, hubID, hubURL, hubRegion string, now func() time.Time, notify func(string)) (*mobileActivationService, error) {
	if commerce == nil || topology == nil || hubID == "" || now == nil || notify == nil {
		return nil, errMobileActivationUnavailable
	}
	return &mobileActivationService{
		flows: make(map[string]mobileActivationFlow), codes: make(map[string]string),
		commerce: commerce, topology: topology, edgeIssuer: issuer, hubID: hubID, hubURL: hubURL, hubRegion: hubRegion, now: now, random: rand.Reader, notifyPolicyChange: notify,
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
	code, err := service.newCodeLocked()
	if err != nil {
		return nil, err
	}
	flow := mobileActivationFlow{flowID: flowID, userCode: code, ownerAccountID: accountID, expiresAt: now.Add(mobileActivationTTL)}
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
	service.flows[flow.flowID] = flow
	return &cloudpb.MobileActivationApproveResponse{Approved: true}, nil
}

func (service *mobileActivationService) claim(request *cloudpb.ClaimMobileActivationRequest) (*cloudpb.LoginFlow, error) {
	if request == nil || !validMobileMetadata(request.GetClientMetadata()) {
		return nil, errMobileActivationUnavailable
	}
	deviceID, err := service.randomID("client")
	if err != nil {
		return nil, err
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
	flow.clientDeviceID = deviceID
	flow.clientMetadata = proto.Clone(request.GetClientMetadata()).(*cloudpb.DeviceMetadata)
	service.flows[flow.flowID] = flow
	return &cloudpb.LoginFlow{FlowId: flow.flowID, UserCode: flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), PollIntervalMillis: 1000}, nil
}

func (service *mobileActivationService) complete(ctx context.Context, request *cloudpb.CompleteLoginRequest) (httpapi.SessionWire, error) {
	if request == nil || request.GetFlowId() == "" {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	service.mu.Lock()
	service.cleanupLocked(service.now().UTC())
	flow, ok := service.flows[request.GetFlowId()]
	if ok && flow.claimed && flow.approved {
		// flow ID 是 App 兑换 session 的单次 secret；先消费再执行外部写入，禁止并发重复签发。
		delete(service.flows, flow.flowID)
		delete(service.codes, flow.userCode)
	}
	service.mu.Unlock()
	if ok && flow.claimed && !flow.approved {
		return httpapi.SessionWire{}, errMobileActivationPending
	}
	if !ok || !flow.claimed || !flow.approved {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	view, err := service.commerce.AccountCommerce(ctx, flow.ownerAccountID)
	if err != nil || view.GetAccount() == nil {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	account := view.GetAccount()
	policy := &cloudpb.CloudDevicePolicy{AccountId: account.GetAccountId(), DeviceId: flow.clientDeviceID, DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT, AuthEpoch: account.GetAuthRevision()}
	if current, loadErr := service.topology.Device(ctx, flow.clientDeviceID); loadErr == nil {
		if current.AccountID != policy.GetAccountId() || current.Kind != policy.GetDeviceKind() || current.Revoked {
			return httpapi.SessionWire{}, errMobileActivationUnavailable
		}
	} else if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
		return httpapi.SessionWire{}, loadErr
	} else if err := service.topology.PutDeviceOwnership(ctx, policy); err != nil {
		return httpapi.SessionWire{}, err
	}
	service.notifyPolicyChange(account.GetAccountId())
	credential, err := service.commerce.IssueDeviceSession(ctx, account.GetAccountId(), flow.clientDeviceID)
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	wire, err := service.issueSession(account, flow.clientDeviceID, credential)
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	return wire, nil
}

func (service *mobileActivationService) refreshSession(ctx context.Context, input httpapi.RefreshSessionWire) (httpapi.SessionWire, error) {
	if input.Kind != session.KindAccount || len(input.RefreshToken) < 32 {
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
	return service.issueSession(view.GetAccount(), rotated.ClientDeviceID, credential)
}

func (service *mobileActivationService) issueSession(account *cloudpb.AccountProjection, deviceID string, credential *cloudpb.AccountSessionCredential) (httpapi.SessionWire, error) {
	if credential == nil || len(credential.GetRefreshToken()) < 32 {
		return httpapi.SessionWire{}, errMobileActivationUnavailable
	}
	now := service.now().UTC()
	tokenID, err := service.randomID("edge")
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	access, err := service.edgeIssuer.IssueEdgeAccessWithDirectory(tokenID, service.hubID, service.hubURL, service.hubRegion, 1, account.GetAccountId(), deviceID, servicecredential.EdgePrincipalClient, account.GetAuthRevision(), mobileAccessTTL, now)
	if err != nil {
		return httpapi.SessionWire{}, err
	}
	refreshToken := append([]byte(nil), credential.GetRefreshToken()...)
	clear(credential.AccessToken)
	clear(credential.RefreshToken)
	return httpapi.SessionWire{Kind: session.KindAccount, AccountID: account.GetAccountId(), AccountLabel: account.GetDisplayName(), DeviceID: deviceID, ExpiresAt: now.Add(mobileAccessTTL).Unix(), AccessToken: access, RefreshToken: refreshToken, RefreshExpiresAt: credential.GetRefreshExpiresAtUnixMillis() / 1000, HubID: service.hubID, HubURL: service.hubURL, HubRegion: service.hubRegion, HubDirectoryVersion: 1}, nil
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

func mobileProjection(flow mobileActivationFlow) *cloudpb.MobileActivationProjection {
	state := cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_DEVICE
	if flow.claimed {
		state = cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_WAITING_FOR_APPROVAL
	}
	if flow.approved {
		state = cloudpb.MobileActivationState_MOBILE_ACTIVATION_STATE_APPROVED
	}
	projection := &cloudpb.MobileActivationProjection{UserCode: flow.userCode, QrPayload: "termx-cloud-activate:v1:" + flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), State: state}
	if flow.clientMetadata != nil {
		projection.ClientMetadata = proto.Clone(flow.clientMetadata).(*cloudpb.DeviceMetadata)
	}
	return projection
}

func (service *mobileActivationService) flowByCodeLocked(raw string) (mobileActivationFlow, bool) {
	flowID, ok := service.codes[strings.ToUpper(strings.TrimSpace(raw))]
	flow, exists := service.flows[flowID]
	return flow, ok && exists
}

func (service *mobileActivationService) cleanupLocked(now time.Time) {
	for id, flow := range service.flows {
		if !now.Before(flow.expiresAt) {
			delete(service.flows, id)
			delete(service.codes, flow.userCode)
		}
	}
}

func (service *mobileActivationService) newCodeLocked() (string, error) {
	for range 16 {
		data := make([]byte, 10)
		if _, err := io.ReadFull(service.random, data); err != nil {
			return "", err
		}
		codeBytes := make([]byte, len(data))
		for index, value := range data {
			codeBytes[index] = mobileCodeAlphabet[int(value)%len(mobileCodeAlphabet)]
		}
		code := string(codeBytes)
		candidate := code[:5] + "-" + code[5:]
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
	return value != nil && strings.TrimSpace(value.GetDisplayName()) != "" && len(value.GetDisplayName()) <= 128 && strings.TrimSpace(value.GetPlatform()) != "" && len(value.GetPlatform()) <= 64 && strings.TrimSpace(value.GetTermxVersion()) != "" && len(value.GetTermxVersion()) <= 64
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
