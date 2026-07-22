package controller

import (
	"bytes"
	"container/list"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudtopology "github.com/muxvia/muxvia/private/cloud/control-plane/topology"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"github.com/muxvia/muxvia/shared/cloudcompanion"
	"google.golang.org/protobuf/proto"
)

const (
	enrollmentFlowTTL    = 10 * time.Minute
	enrollmentSessionTTL = time.Hour
	maxEnrollmentFlows   = 1_000_000
	cloudProtoMediaType  = "application/x-protobuf"
	maxEnrollmentBody    = 4 << 20
)

var (
	errEnrollmentDenied  = errors.New("daemon enrollment denied")
	errEnrollmentPending = errors.New("daemon enrollment is waiting for Web approval")
	errEnrollmentBusy    = errors.New("daemon enrollment capacity is exhausted")
)

type enrollmentFlow struct {
	flowID          string
	userCode        string
	codeDigest      string
	accountID       string
	hubID           string
	state           cloudpb.DaemonEnrollmentState
	challengeID     string
	challenge       []byte
	deviceID        string
	devicePublicKey ed25519.PublicKey
	metadata        *cloudpb.DeviceMetadata
	expiresAt       time.Time
	order           *list.Element
	completing      bool
}

// enrollmentService 是 Controller 对 daemon enrollment 短期 flow 的内存真值。
// 重启会使全部 pending flow 失效；完成后的设备归属、assignment 和 session 仍由持久领域持有。
type enrollmentService struct {
	mu                 sync.Mutex
	defaultHubID       string
	commerce           *cloudcommerce.Service
	topology           *cloudtopology.Service
	registry           *hubregistry.Registry
	edgeIssuer         servicecredential.EdgeAccessIssuer
	controlKeyID       string
	controlPublicKey   ed25519.PublicKey
	controlNotBefore   time.Time
	controlNotAfter    time.Time
	now                func() time.Time
	random             io.Reader
	notifyPolicyChange func(string)
	flows              map[string]*enrollmentFlow
	codes              map[string]string
	expiry             *list.List
}

type enrollmentServiceConfig struct {
	DefaultHubID       string
	Commerce           *cloudcommerce.Service
	Topology           *cloudtopology.Service
	Registry           *hubregistry.Registry
	EdgeIssuer         servicecredential.EdgeAccessIssuer
	ControlKeyID       string
	ControlPublicKey   ed25519.PublicKey
	ControlNotBefore   time.Time
	ControlNotAfter    time.Time
	Now                func() time.Time
	NotifyPolicyChange func(string)
}

func newEnrollmentService(config enrollmentServiceConfig) (*enrollmentService, error) {
	if config.DefaultHubID == "" || config.Commerce == nil || config.Topology == nil || config.Registry == nil || config.ControlKeyID == "" || len(config.ControlPublicKey) != ed25519.PublicKeySize || !config.ControlNotAfter.After(config.ControlNotBefore) {
		return nil, fmt.Errorf("invalid daemon enrollment configuration")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &enrollmentService{
		defaultHubID: config.DefaultHubID,
		commerce:     config.Commerce, topology: config.Topology, registry: config.Registry, edgeIssuer: config.EdgeIssuer,
		controlKeyID: config.ControlKeyID, controlPublicKey: append(ed25519.PublicKey(nil), config.ControlPublicKey...),
		controlNotBefore: config.ControlNotBefore.UTC(), controlNotAfter: config.ControlNotAfter.UTC(),
		now: config.Now, random: rand.Reader, notifyPolicyChange: config.NotifyPolicyChange,
		flows: make(map[string]*enrollmentFlow), codes: make(map[string]string), expiry: list.New(),
	}, nil
}

// CreateDaemonEnrollment 为当前 Web 账号创建 128-bit 单次登录码。
func (service *enrollmentService) CreateDaemonEnrollment(ctx context.Context, accountID, userID string) (*cloudpb.DaemonEnrollmentProjection, error) {
	if accountID == "" || userID == "" {
		return nil, cloudcommerce.ErrUnauthorized
	}
	view, err := service.commerce.AccountCommerce(ctx, accountID)
	if err != nil || view.GetAccount().GetUserId() != userID {
		return nil, cloudcommerce.ErrUnauthorized
	}
	flowID, err := randomEnrollmentID("enroll", 18)
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	if len(service.flows) >= maxEnrollmentFlows {
		return nil, errEnrollmentBusy
	}
	var code string
	var digest string
	for range 16 {
		candidate, generateErr := newOneTimeCode(service.random, "MXD")
		if generateErr != nil {
			return nil, generateErr
		}
		candidateDigest := string(oneTimeCodeDigest(candidate))
		if _, exists := service.codes[candidateDigest]; !exists {
			code, digest = candidate, candidateDigest
			break
		}
	}
	if code == "" {
		return nil, errEnrollmentBusy
	}
	flow := &enrollmentFlow{flowID: flowID, userCode: code, codeDigest: digest, accountID: accountID, hubID: service.defaultHubID, state: cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE, expiresAt: now.Add(enrollmentFlowTTL)}
	flow.order = service.expiry.PushBack(flow)
	service.flows[flowID], service.codes[digest] = flow, flowID
	return daemonEnrollmentProjection(flow), nil
}

// InspectDaemonEnrollment 返回同一账号可见的非秘密 flow 状态。
func (service *enrollmentService) InspectDaemonEnrollment(_ context.Context, accountID, userCode string) (*cloudpb.DaemonEnrollmentProjection, error) {
	flow, err := service.flowByCode(accountID, userCode)
	if err != nil {
		return nil, err
	}
	return daemonEnrollmentProjection(flow), nil
}

// ApproveDaemonEnrollment 只批准已提交 daemon 公开身份且仍属于当前账号的 flow。
func (service *enrollmentService) ApproveDaemonEnrollment(_ context.Context, accountID, userCode string) (*cloudpb.ApproveDaemonEnrollmentResponse, error) {
	code, err := normalizeOneTimeCode(userCode, "MXD")
	if err != nil {
		return nil, cloudcommerce.ErrNotFound
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(service.now().UTC())
	flowID, ok := service.codes[string(oneTimeCodeDigest(code))]
	flow := service.flows[flowID]
	if !ok || flow == nil || flow.accountID != accountID || flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL {
		return nil, cloudcommerce.ErrNotFound
	}
	flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_APPROVED
	return &cloudpb.ApproveDaemonEnrollmentResponse{Approved: true}, nil
}

func (service *enrollmentService) begin(request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	if request == nil || request.GetDeviceId() == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize || request.GetMetadata() == nil || request.GetMetadata().GetDisplayName() == "" || request.GetMetadata().GetPlatform() == "" || request.GetMetadata().GetMuxviaVersion() == "" {
		return nil, errEnrollmentDenied
	}
	code, err := normalizeOneTimeCode(request.GetOneTimeCode(), "MXD")
	if err != nil {
		return nil, errEnrollmentDenied
	}
	challengeID, err := randomEnrollmentID("challenge", 18)
	if err != nil {
		return nil, err
	}
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}
	now := service.now().UTC()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	flowID, ok := service.codes[string(oneTimeCodeDigest(code))]
	flow := service.flows[flowID]
	if !ok || flow == nil || flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE {
		return nil, errEnrollmentDenied
	}
	flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL
	flow.challengeID, flow.challenge = challengeID, append([]byte(nil), challenge...)
	flow.deviceID = request.GetDeviceId()
	flow.devicePublicKey = append(ed25519.PublicKey(nil), request.GetDevicePublicKey()...)
	flow.metadata = proto.Clone(request.GetMetadata()).(*cloudpb.DeviceMetadata)
	return &cloudpb.DeviceEnrollmentChallenge{FlowId: flow.flowID, ChallengeId: challengeID, Challenge: challenge, ExpiresAtUnix: uint64(flow.expiresAt.Unix())}, nil
}

func (service *enrollmentService) complete(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentServiceSession, error) {
	if request == nil || request.GetFlowId() == "" || request.GetProof() == nil {
		return nil, errEnrollmentDenied
	}
	now := service.now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	flow, ok := service.flows[request.GetFlowId()]
	if ok && flow.state == cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL {
		service.mu.Unlock()
		return nil, errEnrollmentPending
	}
	if !ok || flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_APPROVED || flow.completing {
		service.mu.Unlock()
		return nil, errEnrollmentDenied
	}
	flow.completing = true
	flowCopy := cloneEnrollmentFlow(flow)
	service.mu.Unlock()
	completed := false
	defer func() {
		if !completed {
			service.resetCompleting(flowCopy.flowID)
		}
	}()
	proof := request.GetProof()
	signedAt := time.Unix(0, proof.GetSignedAtUnixNano()).UTC()
	if !now.Before(flowCopy.expiresAt) || proof.GetChallengeId() != flowCopy.challengeID || !bytes.Equal(proof.GetDevicePublicKey(), flowCopy.devicePublicKey) || proof.GetDeviceId() != flowCopy.deviceID || signedAt.Before(now.Add(-enrollmentFlowTTL)) || signedAt.After(now.Add(time.Minute)) {
		return nil, errEnrollmentDenied
	}
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{
		FlowId: flowCopy.flowID, ChallengeId: flowCopy.challengeID, Challenge: append([]byte(nil), flowCopy.challenge...),
		DeviceId: proof.GetDeviceId(), DevicePublicKey: append([]byte(nil), flowCopy.devicePublicKey...), SignedAtUnixNano: proof.GetSignedAtUnixNano(),
	})
	if err != nil || !ed25519.Verify(flowCopy.devicePublicKey, signingBytes, proof.GetSignature()) {
		return nil, errEnrollmentDenied
	}
	accountView, err := service.commerce.AccountCommerce(ctx, flowCopy.accountID)
	if err != nil || accountView.GetAccount() == nil || accountView.GetAccount().GetAuthRevision() == 0 {
		return nil, errEnrollmentDenied
	}
	account := accountView.GetAccount()
	if current, loadErr := service.topology.Device(ctx, proof.GetDeviceId()); loadErr == nil {
		if current.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || current.AccountID != flowCopy.accountID || !bytes.Equal(current.PublicKey, flowCopy.devicePublicKey) {
			return nil, errEnrollmentDenied
		}
	} else if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
		return nil, loadErr
	}
	needsAssignment := false
	assignment, assignmentErr := service.registry.Assignment(ctx, proof.GetDeviceId())
	if errors.Is(assignmentErr, hubregistry.ErrAssignmentConflict) {
		needsAssignment = true
		assignmentErr = nil
	}
	if assignmentErr != nil || (!needsAssignment && (assignment.Value.GetAccountId() != flowCopy.accountID || assignment.Value.GetHubId() != flowCopy.hubID || assignment.Value.GetExpiresAtUnixMillis() <= now.UnixMilli())) {
		return nil, errEnrollmentDenied
	}
	policy := &cloudpb.CloudDevicePolicy{AccountId: flowCopy.accountID, DeviceId: proof.GetDeviceId(), DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision(), PublicKey: append([]byte(nil), flowCopy.devicePublicKey...)}
	if err := service.topology.PutDeviceOwnership(ctx, policy); err != nil {
		return nil, err
	}
	if needsAssignment {
		assignment, assignmentErr = service.registry.Assign(ctx, &cloudpb.HubAssignment{DaemonDeviceId: proof.GetDeviceId(), AccountId: flowCopy.accountID, HubId: flowCopy.hubID, AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli()}, now)
		if assignmentErr != nil {
			return nil, assignmentErr
		}
	}
	tokenID, err := randomEnrollmentID("edge", 18)
	if err != nil {
		return nil, err
	}
	sessionExpiresAt := now.Add(enrollmentSessionTTL)
	if service.controlNotAfter.Before(sessionExpiresAt) {
		sessionExpiresAt = service.controlNotAfter
	}
	assignmentExpiresAt := time.UnixMilli(assignment.Value.GetExpiresAtUnixMillis()).UTC()
	if assignmentExpiresAt.Before(sessionExpiresAt) {
		sessionExpiresAt = assignmentExpiresAt
	}
	if sessionExpiresAt.Sub(now) < time.Minute {
		return nil, errEnrollmentDenied
	}
	accessToken, err := service.edgeIssuer.IssueEdgeAccessForPrincipal(tokenID, flowCopy.hubID, flowCopy.accountID, proof.GetDeviceId(), servicecredential.EdgePrincipalDaemon, account.GetAuthRevision(), sessionExpiresAt.Sub(now), now)
	if err != nil {
		return nil, err
	}
	refreshCredential, err := service.commerce.IssueDeviceSession(ctx, flowCopy.accountID, proof.GetDeviceId())
	if err != nil {
		return nil, err
	}
	refreshToken := append([]byte(nil), refreshCredential.GetRefreshToken()...)
	refreshExpiresAt := refreshCredential.GetRefreshExpiresAtUnixMillis()
	clear(refreshCredential.AccessToken)
	clear(refreshCredential.RefreshToken)
	if service.notifyPolicyChange != nil {
		service.notifyPolicyChange(flowCopy.accountID)
	}
	enrollment := &cloudpb.DaemonControlEnrollment{
		AccountId: flowCopy.accountID, DaemonDeviceId: proof.GetDeviceId(), AuthEpoch: account.GetAuthRevision(), EnrolledAtUnixMillis: now.UnixMilli(),
		VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: service.controlKeyID, PublicKey: append([]byte(nil), service.controlPublicKey...), NotBeforeUnixMillis: service.controlNotBefore.UnixMilli(), NotAfterUnixMillis: service.controlNotAfter.UnixMilli()}},
	}
	result := &cloudpb.DeviceEnrollmentServiceSession{
		Session:     &cloudpb.CloudSessionSummary{AccountLabel: account.GetDisplayName(), AccountId: flowCopy.accountID, DeviceId: proof.GetDeviceId(), ExpiresAtUnix: uint64(sessionExpiresAt.Unix())},
		AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAtUnixMillis: refreshExpiresAt,
		HubId: flowCopy.hubID, HubDirectoryVersion: 1, ControlEnrollment: enrollment,
	}
	service.consume(flowCopy.flowID)
	completed = true
	return result, nil
}

func (service *enrollmentService) cleanupLocked(now time.Time) {
	for service.expiry.Len() > 0 {
		front := service.expiry.Front()
		flow := front.Value.(*enrollmentFlow)
		if now.Before(flow.expiresAt) {
			return
		}
		service.removeLocked(flow)
	}
}

func (service *enrollmentService) flowByCode(accountID, raw string) (*enrollmentFlow, error) {
	code, err := normalizeOneTimeCode(raw, "MXD")
	if err != nil {
		return nil, cloudcommerce.ErrNotFound
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(service.now().UTC())
	flowID := service.codes[string(oneTimeCodeDigest(code))]
	flow := service.flows[flowID]
	if flow == nil || flow.accountID != accountID {
		return nil, cloudcommerce.ErrNotFound
	}
	return cloneEnrollmentFlow(flow), nil
}

func (service *enrollmentService) resetCompleting(flowID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if flow := service.flows[flowID]; flow != nil {
		flow.completing = false
	}
}

func (service *enrollmentService) consume(flowID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if flow := service.flows[flowID]; flow != nil {
		service.removeLocked(flow)
	}
}

func (service *enrollmentService) removeLocked(flow *enrollmentFlow) {
	delete(service.flows, flow.flowID)
	delete(service.codes, flow.codeDigest)
	if flow.order != nil {
		service.expiry.Remove(flow.order)
		flow.order = nil
	}
}

func cloneEnrollmentFlow(flow *enrollmentFlow) *enrollmentFlow {
	clone := *flow
	clone.challenge = append([]byte(nil), flow.challenge...)
	clone.devicePublicKey = append(ed25519.PublicKey(nil), flow.devicePublicKey...)
	if flow.metadata != nil {
		clone.metadata = proto.Clone(flow.metadata).(*cloudpb.DeviceMetadata)
	}
	clone.order = nil
	return &clone
}

func daemonEnrollmentProjection(flow *enrollmentFlow) *cloudpb.DaemonEnrollmentProjection {
	projection := &cloudpb.DaemonEnrollmentProjection{UserCode: flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), HubId: flow.hubID, State: flow.state, DaemonDeviceId: flow.deviceID}
	if flow.metadata != nil {
		projection.DaemonMetadata = proto.Clone(flow.metadata).(*cloudpb.DeviceMetadata)
	}
	return projection
}

func randomEnrollmentID(prefix string, size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(value), nil
}

func (service *enrollmentService) registerHTTP(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/enrollment/begin", func(writer http.ResponseWriter, request *http.Request) {
		input := &cloudpb.BeginDeviceEnrollmentRequest{}
		if err := readEnrollmentProto(request, input); err != nil {
			writeEnrollmentError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "device enrollment request is invalid", false)
			return
		}
		response, err := service.begin(input)
		if err != nil {
			writeEnrollmentError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "daemon enrollment was not accepted", false)
			return
		}
		writeEnrollmentProto(writer, response)
	})
	mux.HandleFunc("POST /v1/enrollment/complete", func(writer http.ResponseWriter, request *http.Request) {
		input := &cloudpb.CompleteDeviceEnrollmentRequest{}
		if err := readEnrollmentProto(request, input); err != nil {
			writeEnrollmentError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "device enrollment proof is invalid", false)
			return
		}
		response, err := service.complete(request.Context(), input)
		if err != nil {
			if errors.Is(err, errEnrollmentPending) {
				writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "daemon enrollment is waiting for Web approval", true)
				return
			}
			writeEnrollmentError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "daemon enrollment was not accepted", false)
			return
		}
		writeEnrollmentProto(writer, response)
	})
}

func readEnrollmentProto(request *http.Request, target proto.Message) error {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != cloudProtoMediaType || len(parameters) != 0 {
		return fmt.Errorf("invalid content type")
	}
	payload, err := io.ReadAll(io.LimitReader(request.Body, maxEnrollmentBody+1))
	if err != nil || len(payload) == 0 || len(payload) > maxEnrollmentBody || proto.Unmarshal(payload, target) != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		return fmt.Errorf("invalid protobuf body")
	}
	return nil
}

func writeEnrollmentProto(writer http.ResponseWriter, value proto.Message) {
	payload, err := proto.Marshal(value)
	if err != nil {
		writeEnrollmentError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "device enrollment response failed", true)
		return
	}
	writer.Header().Set("Content-Type", cloudProtoMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(payload)
}

func writeEnrollmentError(writer http.ResponseWriter, status int, code cloudpb.CloudErrorCode, message string, retryable bool) {
	payload, _ := proto.Marshal(&cloudpb.CloudError{Code: code, Message: message, Retryable: retryable})
	writer.Header().Set("Content-Type", cloudProtoMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}
