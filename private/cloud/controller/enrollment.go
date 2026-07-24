package controller

import (
	"bytes"
	"container/list"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"

	cloudcommerce "github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/persistence"
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
	errEnrollmentDenied           = errors.New("daemon enrollment denied")
	errEnrollmentPending          = errors.New("daemon enrollment is waiting for Web approval")
	errEnrollmentBusy             = errors.New("daemon enrollment capacity is exhausted")
	errEnrollmentExpired          = errors.New("daemon enrollment code expired")
	errEnrollmentActive           = errors.New("daemon identity is active in another account")
	errEnrollmentIdentityMismatch = errors.New("daemon identity does not match the registered public key")
	errEnrollmentNoReachableHub   = errors.New("daemon did not find a reachable Hub")
	errEnrollmentCandidateStale   = errors.New("daemon enrollment Hub candidate is stale")
	errEnrollmentCommitConflict   = errors.New("daemon enrollment commit conflicted")
	// errEnrollmentUnavailable 表示注册码本身尚未被消费，但 Controller 暂时无法提供 Hub 候选。
	// HTTP 边界必须把它投影为可重试服务故障，不能伪装成注册码无效。
	errEnrollmentUnavailable = errors.New("daemon enrollment is temporarily unavailable")
)

type enrollmentFlow struct {
	flowID            string
	userCode          string
	codeDigest        string
	accountID         string
	state             cloudpb.DaemonEnrollmentState
	challengeID       string
	challenge         []byte
	deviceID          string
	devicePublicKey   ed25519.PublicKey
	metadata          *cloudpb.DeviceMetadata
	hubCandidates     []*cloudpb.HubEnrollmentCandidate
	candidateDigest   []byte
	challengeRevision uint64
	revision          uint64
	changed           chan struct{}
	action            cloudpb.DaemonEnrollmentAction
	expiresAt         time.Time
	order             *list.Element
	completing        bool
	completionDigest  []byte
	completedResult   *cloudpb.DeviceEnrollmentServiceSession
}

// enrollmentHubCandidate 把客户端可探测的公开目录与 Controller 私有容量真值绑定。
// assignment 数和容量不得进入 enrollment Proto；它们只用于校验 daemon 提议的 Hub 仍可分配。
type enrollmentHubCandidate struct {
	value           *cloudpb.HubEnrollmentCandidate
	assignmentCount uint64
	maxAssignments  uint64
}

// enrollmentService 是 Controller 对 daemon enrollment 短期 flow 的内存真值。
// 重启会使全部 pending flow 失效；完成后的设备归属、assignment 和 session 仍由持久领域持有。
type enrollmentService struct {
	mu                 sync.Mutex
	commerce           *cloudcommerce.Service
	topology           *cloudtopology.Service
	registry           *hubregistry.Registry
	enrollmentStore    persistence.DaemonEnrollmentStore
	candidateProvider  func(context.Context, time.Time, string) ([]enrollmentHubCandidate, error)
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
	Commerce           *cloudcommerce.Service
	Topology           *cloudtopology.Service
	Registry           *hubregistry.Registry
	EnrollmentStore    persistence.DaemonEnrollmentStore
	CandidateProvider  func(context.Context, time.Time, string) ([]enrollmentHubCandidate, error)
	EdgeIssuer         servicecredential.EdgeAccessIssuer
	ControlKeyID       string
	ControlPublicKey   ed25519.PublicKey
	ControlNotBefore   time.Time
	ControlNotAfter    time.Time
	Now                func() time.Time
	NotifyPolicyChange func(string)
}

func newEnrollmentService(config enrollmentServiceConfig) (*enrollmentService, error) {
	if config.Commerce == nil || config.Topology == nil || config.Registry == nil || config.EnrollmentStore == nil || config.CandidateProvider == nil || config.ControlKeyID == "" || len(config.ControlPublicKey) != ed25519.PublicKeySize || !config.ControlNotAfter.After(config.ControlNotBefore) {
		return nil, fmt.Errorf("invalid daemon enrollment configuration")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &enrollmentService{
		commerce: config.Commerce, topology: config.Topology, registry: config.Registry, enrollmentStore: config.EnrollmentStore, edgeIssuer: config.EdgeIssuer,
		candidateProvider: config.CandidateProvider,
		controlKeyID:      config.ControlKeyID, controlPublicKey: append(ed25519.PublicKey(nil), config.ControlPublicKey...),
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
	flow := &enrollmentFlow{flowID: flowID, userCode: code, codeDigest: digest, accountID: accountID, state: cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE, expiresAt: now.Add(enrollmentFlowTTL), revision: 1, changed: make(chan struct{})}
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
	service.notifyFlowLocked(flow)
	return &cloudpb.ApproveDaemonEnrollmentResponse{Approved: true}, nil
}

func (service *enrollmentService) begin(ctx context.Context, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	if request == nil || request.GetDeviceId() == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize || request.GetMetadata() == nil || request.GetMetadata().GetDisplayName() == "" || request.GetMetadata().GetPlatform() == "" || request.GetMetadata().GetMuxviaVersion() == "" {
		return nil, fmt.Errorf("%w: request metadata is invalid", errEnrollmentDenied)
	}
	code, err := normalizeOneTimeCode(request.GetOneTimeCode(), "MXD")
	if err != nil {
		return nil, fmt.Errorf("%w: code format is invalid", errEnrollmentDenied)
	}
	now := service.now().UTC()
	digest := string(oneTimeCodeDigest(code))
	service.mu.Lock()
	service.cleanupLocked(now)
	flowID, ok := service.codes[digest]
	flow := service.flows[flowID]
	if !ok || flow == nil {
		service.mu.Unlock()
		return nil, errEnrollmentExpired
	}
	if flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE {
		challenge, resumeErr := resumableEnrollmentChallenge(flow, request)
		service.mu.Unlock()
		if resumeErr != nil {
			return nil, resumeErr
		}
		return challenge, nil
	}
	service.mu.Unlock()
	challengeID, err := randomEnrollmentID("challenge", 18)
	if err != nil {
		return nil, fmt.Errorf("%w: create challenge ID: %v", errEnrollmentUnavailable, err)
	}
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("%w: create challenge: %v", errEnrollmentUnavailable, err)
	}
	existingHubID := ""
	if assignment, assignmentErr := service.registry.Assignment(ctx, request.GetDeviceId()); assignmentErr == nil {
		existingHubID = assignment.Value.GetHubId()
	} else if !errors.Is(assignmentErr, hubregistry.ErrAssignmentConflict) {
		return nil, fmt.Errorf("%w: inspect daemon assignment: %v", errEnrollmentUnavailable, assignmentErr)
	}
	candidates, err := service.candidateProvider(ctx, now, existingHubID)
	if err != nil {
		return nil, fmt.Errorf("%w: load Hub candidates: %v", errEnrollmentUnavailable, err)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no active Hub candidates", errEnrollmentUnavailable)
	}
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}
	publicCandidates := publicEnrollmentCandidates(candidates)
	candidateDigest, err := cloudcompanion.EnrollmentCandidateSetDigest(publicCandidates)
	if err != nil {
		return nil, fmt.Errorf("%w: digest Hub candidates: %v", errEnrollmentUnavailable, err)
	}
	action := cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_APPROVE
	if ownership, loadErr := service.topology.Device(ctx, request.GetDeviceId()); loadErr == nil {
		switch {
		case ownership.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || !bytes.Equal(ownership.PublicKey, request.GetDevicePublicKey()):
			action = cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_IDENTITY_MISMATCH
		case ownership.AccountID == flow.accountID && !ownership.Revoked:
			action = cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_ALREADY_ENROLLED
		case ownership.AccountID != flow.accountID && !ownership.Revoked:
			action = cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_REMOVE_FROM_CURRENT_ACCOUNT
		case ownership.AccountID != flow.accountID && ownership.Revoked:
			action = cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_CONFIRM_TRANSFER
		}
	} else if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
		return nil, fmt.Errorf("%w: inspect daemon ownership: %v", errEnrollmentUnavailable, loadErr)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.cleanupLocked(now)
	flowID, ok = service.codes[digest]
	flow = service.flows[flowID]
	if !ok || flow == nil {
		return nil, errEnrollmentExpired
	}
	if flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_DEVICE {
		return resumableEnrollmentChallenge(flow, request)
	}
	flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL
	flow.challengeID, flow.challenge = challengeID, append([]byte(nil), challenge...)
	flow.deviceID = request.GetDeviceId()
	flow.devicePublicKey = append(ed25519.PublicKey(nil), request.GetDevicePublicKey()...)
	flow.metadata = proto.Clone(request.GetMetadata()).(*cloudpb.DeviceMetadata)
	flow.hubCandidates = cloneEnrollmentCandidates(publicCandidates)
	flow.candidateDigest = append([]byte(nil), candidateDigest...)
	flow.action = action
	service.notifyFlowLocked(flow)
	flow.challengeRevision = flow.revision
	return enrollmentChallenge(flow), nil
}

// resumableEnrollmentChallenge 只允许同一个 daemon 请求恢复已经绑定的 challenge。
// 这覆盖 HTTP 响应丢失、CLI 中断和同命令重跑；不同 DeviceIdentity 仍不能接管已进入批准阶段的 flow。
func resumableEnrollmentChallenge(flow *enrollmentFlow, request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	if flow == nil || request == nil || flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL && flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_APPROVED && flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_COMPLETING && flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_COMPLETED ||
		flow.deviceID != request.GetDeviceId() || !bytes.Equal(flow.devicePublicKey, request.GetDevicePublicKey()) || !proto.Equal(flow.metadata, request.GetMetadata()) {
		return nil, fmt.Errorf("%w: flow is already bound to another daemon request", errEnrollmentDenied)
	}
	return enrollmentChallenge(flow), nil
}

func enrollmentChallenge(flow *enrollmentFlow) *cloudpb.DeviceEnrollmentChallenge {
	return &cloudpb.DeviceEnrollmentChallenge{
		FlowId: flow.flowID, ChallengeId: flow.challengeID, Challenge: append([]byte(nil), flow.challenge...),
		ExpiresAtUnix: uint64(flow.expiresAt.Unix()), HubCandidates: cloneEnrollmentCandidates(flow.hubCandidates),
		CandidateSetDigest: append([]byte(nil), flow.candidateDigest...), FlowRevision: flow.challengeRevision,
	}
}

func (service *enrollmentService) complete(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentServiceSession, error) {
	if request == nil || request.GetFlowId() == "" || request.GetProof() == nil || request.GetPreferredHubId() == "" || len(request.GetCandidateSetDigest()) != sha256.Size || request.GetFlowRevision() == 0 {
		return nil, errEnrollmentDenied
	}
	observationsDigest, err := cloudcompanion.EnrollmentObservationsDigest(request.GetHubObservations())
	if err != nil {
		return nil, errEnrollmentDenied
	}
	requestBody, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil {
		return nil, errEnrollmentDenied
	}
	requestDigest := sha256.Sum256(requestBody)
	now := service.now().UTC()
	service.mu.Lock()
	service.cleanupLocked(now)
	flow, ok := service.flows[request.GetFlowId()]
	if ok && flow.completedResult != nil {
		if !bytes.Equal(flow.completionDigest, requestDigest[:]) {
			service.mu.Unlock()
			return nil, errEnrollmentDenied
		}
		result := proto.Clone(flow.completedResult).(*cloudpb.DeviceEnrollmentServiceSession)
		service.mu.Unlock()
		return result, nil
	}
	if ok && flow.state == cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_WAITING_FOR_APPROVAL {
		if flow.action == cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_REMOVE_FROM_CURRENT_ACCOUNT {
			service.mu.Unlock()
			return nil, errEnrollmentActive
		}
		if flow.action == cloudpb.DaemonEnrollmentAction_DAEMON_ENROLLMENT_ACTION_IDENTITY_MISMATCH {
			service.mu.Unlock()
			return nil, errEnrollmentIdentityMismatch
		}
		changed := flow.changed
		wait := 25 * time.Second
		if remaining := flow.expiresAt.Sub(now); remaining < wait {
			wait = remaining
		}
		service.mu.Unlock()
		if wait <= 0 {
			return nil, errEnrollmentExpired
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errEnrollmentPending
		case <-changed:
			return service.complete(ctx, request)
		}
	}
	if !ok || flow.state != cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_APPROVED || flow.completing {
		service.mu.Unlock()
		return nil, errEnrollmentDenied
	}
	if !bytes.Equal(request.GetCandidateSetDigest(), flow.candidateDigest) || request.GetFlowRevision() != flow.challengeRevision {
		service.mu.Unlock()
		return nil, errEnrollmentCandidateStale
	}
	flow.completing = true
	flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_COMPLETING
	service.notifyFlowLocked(flow)
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
		CandidateSetDigest: append([]byte(nil), request.GetCandidateSetDigest()...), PreferredHubId: request.GetPreferredHubId(),
		HubObservationsDigest: observationsDigest, FlowRevision: request.GetFlowRevision(),
	})
	if err != nil || !ed25519.Verify(flowCopy.devicePublicKey, signingBytes, proof.GetSignature()) {
		return nil, errEnrollmentDenied
	}
	accountView, err := service.commerce.AccountCommerce(ctx, flowCopy.accountID)
	if err != nil {
		return nil, fmt.Errorf("%w: load enrollment account: %v", errEnrollmentUnavailable, err)
	}
	if accountView.GetAccount() == nil || accountView.GetAccount().GetAuthRevision() == 0 {
		return nil, errEnrollmentDenied
	}
	account := accountView.GetAccount()
	var currentOwnership cloudtopology.DeviceOwnership
	hasCurrentOwnership := false
	migratingRevokedOwnership := false
	if current, loadErr := service.topology.Device(ctx, proof.GetDeviceId()); loadErr == nil {
		if current.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || !bytes.Equal(current.PublicKey, flowCopy.devicePublicKey) {
			return nil, errEnrollmentIdentityMismatch
		}
		currentOwnership, hasCurrentOwnership = current, true
		if current.AccountID != flowCopy.accountID {
			if !current.Revoked {
				return nil, errEnrollmentActive
			}
			migratingRevokedOwnership = true
		}
	} else if !errors.Is(loadErr, cloudtopology.ErrOwnershipNotFound) {
		return nil, fmt.Errorf("%w: load daemon ownership: %v", errEnrollmentUnavailable, loadErr)
	}
	needsAssignment := false
	assignment, assignmentErr := service.registry.Assignment(ctx, proof.GetDeviceId())
	if errors.Is(assignmentErr, hubregistry.ErrAssignmentConflict) {
		needsAssignment = true
		assignmentErr = nil
	}
	if assignmentErr != nil {
		return nil, fmt.Errorf("%w: load daemon assignment: %v", errEnrollmentUnavailable, assignmentErr)
	}
	renewingSameAccountAssignment := false
	// 同账号显式替换 cloud session 和已撤销跨账号转移都可以续签原 Hub；
	// assignment 变化仍由最终事务提升 epoch，不能在 enrollment 中静默换 Hub。
	if !needsAssignment && assignment.Value.GetExpiresAtUnixMillis() <= now.UnixMilli() && !migratingRevokedOwnership {
		if !hasCurrentOwnership || currentOwnership.AccountID != flowCopy.accountID || currentOwnership.Revoked {
			return nil, errEnrollmentDenied
		}
		renewingSameAccountAssignment = true
	}
	existingHubID := ""
	if !needsAssignment {
		expectedAccountID := flowCopy.accountID
		if migratingRevokedOwnership {
			expectedAccountID = currentOwnership.AccountID
		}
		if assignment.Value.GetAccountId() != expectedAccountID {
			return nil, errEnrollmentDenied
		}
		existingHubID = assignment.Value.GetHubId()
	}
	currentCandidates, err := service.candidateProvider(ctx, now, existingHubID)
	if err != nil {
		return nil, fmt.Errorf("%w: reload Hub candidates: %v", errEnrollmentUnavailable, err)
	}
	if len(currentCandidates) == 0 {
		return nil, fmt.Errorf("%w: no active Hub candidates during completion", errEnrollmentUnavailable)
	}
	selected, err := validateEnrollmentHubProposal(flowCopy.hubCandidates, currentCandidates, request.GetHubObservations(), request.GetPreferredHubId(), existingHubID)
	if err != nil {
		return nil, err
	}
	policy := &cloudpb.CloudDevicePolicy{AccountId: flowCopy.accountID, DeviceId: proof.GetDeviceId(), DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision(), PublicKey: append([]byte(nil), flowCopy.devicePublicKey...)}
	nextOwnership := cloudtopology.DeviceOwnership{DeviceID: policy.GetDeviceId(), AccountID: policy.GetAccountId(), Kind: policy.GetDeviceKind(), AuthEpoch: policy.GetAuthEpoch(), PublicKey: append([]byte(nil), policy.GetPublicKey()...)}
	var expectedOwnership *cloudtopology.DeviceOwnership
	var expectedAssignment *cloudpb.HubAssignment
	var nextAssignment *cloudpb.HubAssignment
	if hasCurrentOwnership {
		current := currentOwnership
		expectedOwnership = &current
	}
	if !needsAssignment {
		expectedAssignment = proto.Clone(assignment.Value).(*cloudpb.HubAssignment)
	}
	if migratingRevokedOwnership {
		if !hasCurrentOwnership {
			return nil, errEnrollmentDenied
		}
		if needsAssignment {
			nextAssignment = &cloudpb.HubAssignment{DaemonDeviceId: proof.GetDeviceId(), AccountId: flowCopy.accountID, HubId: selected.GetHubId(), AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli()}
		} else {
			nextAssignment = proto.Clone(assignment.Value).(*cloudpb.HubAssignment)
			nextAssignment.AccountId = flowCopy.accountID
			nextAssignment.AssignmentEpoch++
			nextAssignment.NotBeforeUnixMillis = now.Add(-time.Minute).UnixMilli()
			nextAssignment.ExpiresAtUnixMillis = now.Add(24 * time.Hour).UnixMilli()
		}
	} else if needsAssignment {
		nextAssignment = &cloudpb.HubAssignment{DaemonDeviceId: proof.GetDeviceId(), AccountId: flowCopy.accountID, HubId: selected.GetHubId(), AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli()}
	} else if renewingSameAccountAssignment {
		nextAssignment = proto.Clone(assignment.Value).(*cloudpb.HubAssignment)
		nextAssignment.AssignmentEpoch++
		nextAssignment.NotBeforeUnixMillis = now.Add(-time.Minute).UnixMilli()
		nextAssignment.ExpiresAtUnixMillis = now.Add(24 * time.Hour).UnixMilli()
	} else {
		nextAssignment = proto.Clone(assignment.Value).(*cloudpb.HubAssignment)
	}
	tokenID, err := randomEnrollmentID("edge", 18)
	if err != nil {
		return nil, fmt.Errorf("%w: create edge token ID: %v", errEnrollmentUnavailable, err)
	}
	sessionExpiresAt := now.Add(enrollmentSessionTTL)
	if service.controlNotAfter.Before(sessionExpiresAt) {
		sessionExpiresAt = service.controlNotAfter
	}
	assignmentExpiresAt := time.UnixMilli(nextAssignment.GetExpiresAtUnixMillis()).UTC()
	if assignmentExpiresAt.Before(sessionExpiresAt) {
		sessionExpiresAt = assignmentExpiresAt
	}
	if sessionExpiresAt.Sub(now) < time.Minute {
		return nil, fmt.Errorf("%w: enrollment credential window is unavailable", errEnrollmentUnavailable)
	}
	accessToken, err := service.edgeIssuer.IssueEdgeAccessForPrincipal(tokenID, selected.GetHubId(), flowCopy.accountID, proof.GetDeviceId(), servicecredential.EdgePrincipalDaemon, account.GetAuthRevision(), sessionExpiresAt.Sub(now), now)
	if err != nil {
		return nil, fmt.Errorf("%w: issue daemon edge credential: %v", errEnrollmentUnavailable, err)
	}
	preparedSession, err := service.commerce.PrepareDeviceSession(account, proof.GetDeviceId(), now)
	if err != nil {
		return nil, fmt.Errorf("%w: prepare daemon refresh session: %v", errEnrollmentUnavailable, err)
	}
	assignment, err = service.enrollmentStore.CommitDaemonEnrollment(ctx, persistence.DaemonEnrollmentCommit{
		ExpectedOwnership: expectedOwnership, ExpectedAssignment: expectedAssignment,
		NextOwnership: nextOwnership, NextAssignment: nextAssignment,
		Session: preparedSession.Record, Audit: preparedSession.Audit,
	}, now)
	if err != nil {
		clear(preparedSession.Credential.AccessToken)
		clear(preparedSession.Credential.RefreshToken)
		if errors.Is(err, cloudtopology.ErrTopologyRejected) || errors.Is(err, hubregistry.ErrAssignmentConflict) || errors.Is(err, cloudcommerce.ErrConflict) {
			return nil, errEnrollmentCommitConflict
		}
		return nil, fmt.Errorf("%w: commit daemon enrollment: %v", errEnrollmentUnavailable, err)
	}
	refreshCredential := preparedSession.Credential
	refreshToken := append([]byte(nil), refreshCredential.GetRefreshToken()...)
	refreshExpiresAt := refreshCredential.GetRefreshExpiresAtUnixMillis()
	clear(refreshCredential.AccessToken)
	clear(refreshCredential.RefreshToken)
	if service.notifyPolicyChange != nil {
		if migratingRevokedOwnership {
			service.notifyPolicyChange(currentOwnership.AccountID)
		}
		service.notifyPolicyChange(flowCopy.accountID)
	}
	enrollment := &cloudpb.DaemonControlEnrollment{
		AccountId: flowCopy.accountID, DaemonDeviceId: proof.GetDeviceId(), AuthEpoch: account.GetAuthRevision(), EnrolledAtUnixMillis: now.UnixMilli(),
		VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: service.controlKeyID, PublicKey: append([]byte(nil), service.controlPublicKey...), NotBeforeUnixMillis: service.controlNotBefore.UnixMilli(), NotAfterUnixMillis: service.controlNotAfter.UnixMilli()}},
	}
	result := &cloudpb.DeviceEnrollmentServiceSession{
		Session:     &cloudpb.CloudSessionSummary{AccountLabel: account.GetDisplayName(), AccountId: flowCopy.accountID, DeviceId: proof.GetDeviceId(), ExpiresAtUnix: uint64(sessionExpiresAt.Unix())},
		AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAtUnixMillis: refreshExpiresAt,
		HubId: selected.GetHubId(), HubUrl: selected.GetHubUrl(), HubRegion: selected.GetRegion(), HubDirectoryVersion: 1, ControlEnrollment: enrollment,
	}
	service.completeDelivery(flowCopy.flowID, requestDigest[:], result)
	completed = true
	return proto.Clone(result).(*cloudpb.DeviceEnrollmentServiceSession), nil
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
		flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_APPROVED
		service.notifyFlowLocked(flow)
	}
}

func (service *enrollmentService) completeDelivery(flowID string, digest []byte, result *cloudpb.DeviceEnrollmentServiceSession) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if flow := service.flows[flowID]; flow != nil {
		flow.completing = false
		flow.completionDigest = append([]byte(nil), digest...)
		flow.completedResult = proto.Clone(result).(*cloudpb.DeviceEnrollmentServiceSession)
		flow.state = cloudpb.DaemonEnrollmentState_DAEMON_ENROLLMENT_STATE_COMPLETED
		service.notifyFlowLocked(flow)
	}
}

func (service *enrollmentService) notifyFlowLocked(flow *enrollmentFlow) {
	if flow.changed != nil {
		close(flow.changed)
	}
	flow.changed = make(chan struct{})
	flow.revision++
}

func (service *enrollmentService) removeLocked(flow *enrollmentFlow) {
	if flow.completedResult != nil {
		clear(flow.completedResult.AccessToken)
		clear(flow.completedResult.RefreshToken)
	}
	clear(flow.completionDigest)
	if flow.changed != nil {
		close(flow.changed)
		flow.changed = nil
	}
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
	clone.hubCandidates = cloneEnrollmentCandidates(flow.hubCandidates)
	clone.candidateDigest = append([]byte(nil), flow.candidateDigest...)
	clone.completionDigest = append([]byte(nil), flow.completionDigest...)
	if flow.completedResult != nil {
		clone.completedResult = proto.Clone(flow.completedResult).(*cloudpb.DeviceEnrollmentServiceSession)
	}
	clone.order = nil
	clone.changed = nil
	return &clone
}

func daemonEnrollmentProjection(flow *enrollmentFlow) *cloudpb.DaemonEnrollmentProjection {
	projection := &cloudpb.DaemonEnrollmentProjection{UserCode: flow.userCode, ExpiresAtUnix: uint64(flow.expiresAt.Unix()), State: flow.state, DaemonDeviceId: flow.deviceID, Revision: flow.revision, Action: flow.action}
	if flow.metadata != nil {
		projection.DaemonMetadata = proto.Clone(flow.metadata).(*cloudpb.DeviceMetadata)
	}
	return projection
}

func cloneEnrollmentCandidates(source []*cloudpb.HubEnrollmentCandidate) []*cloudpb.HubEnrollmentCandidate {
	result := make([]*cloudpb.HubEnrollmentCandidate, 0, len(source))
	for _, candidate := range source {
		if candidate != nil {
			result = append(result, proto.Clone(candidate).(*cloudpb.HubEnrollmentCandidate))
		}
	}
	return result
}

func publicEnrollmentCandidates(source []enrollmentHubCandidate) []*cloudpb.HubEnrollmentCandidate {
	result := make([]*cloudpb.HubEnrollmentCandidate, 0, len(source))
	for _, candidate := range source {
		if candidate.value != nil {
			result = append(result, proto.Clone(candidate.value).(*cloudpb.HubEnrollmentCandidate))
		}
	}
	return result
}

func validateEnrollmentHubProposal(offered []*cloudpb.HubEnrollmentCandidate, current []enrollmentHubCandidate, observations []*cloudpb.HubReachabilityObservation, preferredHubID, existingHubID string) (*cloudpb.HubEnrollmentCandidate, error) {
	if preferredHubID == "" || len(offered) == 0 || len(current) == 0 {
		return nil, errEnrollmentDenied
	}
	offeredIDs := make(map[string]bool, len(offered))
	for _, candidate := range offered {
		if candidate == nil || candidate.GetHubId() == "" || offeredIDs[candidate.GetHubId()] {
			return nil, errEnrollmentDenied
		}
		offeredIDs[candidate.GetHubId()] = true
	}
	observed := make(map[string]*cloudpb.HubReachabilityObservation, len(observations))
	for _, observation := range observations {
		if observation == nil || !offeredIDs[observation.GetHubId()] || observed[observation.GetHubId()] != nil || observation.GetReachable() != (observation.GetLatencyMillis() > 0) {
			return nil, errEnrollmentDenied
		}
		observed[observation.GetHubId()] = observation
	}
	preferredObservation := observed[preferredHubID]
	if preferredObservation == nil || !preferredObservation.GetReachable() {
		return nil, errEnrollmentNoReachableHub
	}
	if existingHubID != "" && preferredHubID != existingHubID {
		return nil, errEnrollmentCandidateStale
	}
	for _, candidate := range current {
		if candidate.value == nil || candidate.value.GetHubId() != preferredHubID {
			continue
		}
		if !offeredIDs[preferredHubID] || candidate.value.GetHubUrl() == "" || candidate.value.GetHealthUrl() == "" || candidate.value.GetRegion() == "" || candidate.maxAssignments == 0 || existingHubID == "" && candidate.assignmentCount >= candidate.maxAssignments {
			return nil, errEnrollmentCandidateStale
		}
		return proto.Clone(candidate.value).(*cloudpb.HubEnrollmentCandidate), nil
	}
	return nil, errEnrollmentCandidateStale
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
		response, err := service.begin(request.Context(), input)
		if err != nil {
			if writeKnownEnrollmentError(writer, err) {
				return
			}
			if errors.Is(err, errEnrollmentUnavailable) {
				// 日志只记录内部失败分类，不记录 MXD、账号、DeviceID 或 public key。
				slog.Error("daemon enrollment begin unavailable", "error", err)
				writeEnrollmentError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "daemon enrollment is temporarily unavailable", true)
				return
			}
			slog.Warn("daemon enrollment begin rejected", "reason", err.Error())
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
			if writeKnownEnrollmentError(writer, err) {
				return
			}
			if errors.Is(err, errEnrollmentUnavailable) {
				// complete 已经验证私钥 proof，但内部错误仍不得记录账号、DeviceID、token 或注册码。
				slog.Error("daemon enrollment completion unavailable", "error", err)
				writeEnrollmentError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "daemon enrollment is temporarily unavailable", true)
				return
			}
			slog.Warn("daemon enrollment completion rejected", "reason", err.Error())
			writeEnrollmentError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ENROLLMENT_REQUIRED, "daemon enrollment was not accepted", false)
			return
		}
		writeEnrollmentProto(writer, response)
	})
}

func writeKnownEnrollmentError(writer http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errEnrollmentExpired):
		writeEnrollmentError(writer, http.StatusGone, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENROLLMENT_CODE_EXPIRED, "daemon enrollment code expired", false)
	case errors.Is(err, errEnrollmentPending):
		writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENROLLMENT_APPROVAL_PENDING, "daemon enrollment is waiting for Web approval", true)
	case errors.Is(err, errEnrollmentActive):
		writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_ACTIVE_IN_ANOTHER_ACCOUNT, "daemon identity is active in another account", false)
	case errors.Is(err, errEnrollmentIdentityMismatch):
		writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_IDENTITY_MISMATCH, "daemon identity public key does not match", false)
	case errors.Is(err, errEnrollmentNoReachableHub):
		writeEnrollmentError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_NO_REACHABLE_HUB, "no reachable Cloud Hub was reported", true)
	case errors.Is(err, errEnrollmentCandidateStale):
		writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_HUB_CANDIDATE_STALE, "Cloud Hub candidate is no longer available", true)
	case errors.Is(err, errEnrollmentCommitConflict):
		writeEnrollmentError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENROLLMENT_COMMIT_CONFLICT, "daemon enrollment changed during commit", true)
	default:
		return false
	}
	return true
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
