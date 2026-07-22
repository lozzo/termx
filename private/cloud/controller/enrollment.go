package controller

import (
	"bytes"
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
	enrollmentChallengeTTL = 5 * time.Minute
	enrollmentSessionTTL   = time.Hour
	cloudProtoMediaType    = "application/x-protobuf"
	maxEnrollmentBody      = 4 << 20
)

var errEnrollmentDenied = errors.New("development daemon enrollment denied")

type enrollmentFlow struct {
	flowID          string
	challengeID     string
	challenge       []byte
	devicePublicKey ed25519.PublicKey
	metadata        *cloudpb.DeviceMetadata
	expiresAt       time.Time
}

// enrollmentService 是 Controller 对 development daemon enrollment 的短期 flow owner。
// 持久账号、设备和 assignment 仍分别由 commerce、topology 与 Hub registry 持有；本服务
// 只验证一次性 code 与 DeviceIdentity proof，并签发不包含 terminal capability 的 edge session。
type enrollmentService struct {
	mu                 sync.Mutex
	code               string
	claimed            bool
	accountID          string
	hubID              string
	commerce           *cloudcommerce.Service
	topology           *cloudtopology.Service
	registry           *hubregistry.Registry
	edgeIssuer         servicecredential.EdgeAccessIssuer
	controlKeyID       string
	controlPublicKey   ed25519.PublicKey
	controlNotBefore   time.Time
	controlNotAfter    time.Time
	now                func() time.Time
	notifyPolicyChange func(string)
	flows              map[string]enrollmentFlow
}

type enrollmentServiceConfig struct {
	Code               string
	AccountID          string
	HubID              string
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
	if config.Code == "" || config.AccountID == "" || config.HubID == "" || config.Commerce == nil || config.Topology == nil || config.Registry == nil || config.ControlKeyID == "" || len(config.ControlPublicKey) != ed25519.PublicKeySize || !config.ControlNotAfter.After(config.ControlNotBefore) {
		return nil, fmt.Errorf("invalid development enrollment configuration")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &enrollmentService{
		code: config.Code, accountID: config.AccountID, hubID: config.HubID,
		commerce: config.Commerce, topology: config.Topology, registry: config.Registry, edgeIssuer: config.EdgeIssuer,
		controlKeyID: config.ControlKeyID, controlPublicKey: append(ed25519.PublicKey(nil), config.ControlPublicKey...),
		controlNotBefore: config.ControlNotBefore.UTC(), controlNotAfter: config.ControlNotAfter.UTC(),
		now: config.Now, notifyPolicyChange: config.NotifyPolicyChange, flows: make(map[string]enrollmentFlow),
	}, nil
}

func (service *enrollmentService) begin(request *cloudpb.BeginDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentChallenge, error) {
	if request == nil || request.GetOneTimeCode() == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize || request.GetMetadata() == nil || request.GetMetadata().GetDisplayName() == "" || request.GetMetadata().GetPlatform() == "" || request.GetMetadata().GetMuxviaVersion() == "" {
		return nil, errEnrollmentDenied
	}
	flowID, err := randomEnrollmentID("enroll", 18)
	if err != nil {
		return nil, err
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
	if service.claimed || request.GetOneTimeCode() != service.code {
		return nil, errEnrollmentDenied
	}
	service.claimed = true
	service.flows[flowID] = enrollmentFlow{
		flowID: flowID, challengeID: challengeID, challenge: append([]byte(nil), challenge...),
		devicePublicKey: append(ed25519.PublicKey(nil), request.GetDevicePublicKey()...),
		metadata:        proto.Clone(request.GetMetadata()).(*cloudpb.DeviceMetadata), expiresAt: now.Add(enrollmentChallengeTTL),
	}
	return &cloudpb.DeviceEnrollmentChallenge{FlowId: flowID, ChallengeId: challengeID, Challenge: challenge, ExpiresAtUnix: uint64(now.Add(enrollmentChallengeTTL).Unix())}, nil
}

func (service *enrollmentService) complete(ctx context.Context, request *cloudpb.CompleteDeviceEnrollmentRequest) (*cloudpb.DeviceEnrollmentServiceSession, error) {
	if request == nil || request.GetFlowId() == "" || request.GetProof() == nil {
		return nil, errEnrollmentDenied
	}
	now := service.now().UTC()
	service.mu.Lock()
	flow, ok := service.flows[request.GetFlowId()]
	delete(service.flows, request.GetFlowId())
	service.cleanupLocked(now)
	service.mu.Unlock()
	proof := request.GetProof()
	signedAt := time.Unix(0, proof.GetSignedAtUnixNano()).UTC()
	if !ok || !now.Before(flow.expiresAt) || proof.GetChallengeId() != flow.challengeID || !bytes.Equal(proof.GetDevicePublicKey(), flow.devicePublicKey) || proof.GetDeviceId() == "" || signedAt.Before(now.Add(-enrollmentChallengeTTL)) || signedAt.After(now.Add(time.Minute)) {
		return nil, errEnrollmentDenied
	}
	signingBytes, err := cloudcompanion.EnrollmentProofSigningBytes(&cloudpb.DeviceEnrollmentProofInput{
		FlowId: flow.flowID, ChallengeId: flow.challengeID, Challenge: append([]byte(nil), flow.challenge...),
		DeviceId: proof.GetDeviceId(), DevicePublicKey: append([]byte(nil), flow.devicePublicKey...), SignedAtUnixNano: proof.GetSignedAtUnixNano(),
	})
	if err != nil || !ed25519.Verify(flow.devicePublicKey, signingBytes, proof.GetSignature()) {
		return nil, errEnrollmentDenied
	}
	accountView, err := service.commerce.AccountCommerce(ctx, service.accountID)
	if err != nil || accountView.GetAccount() == nil || accountView.GetAccount().GetAuthRevision() == 0 {
		return nil, errEnrollmentDenied
	}
	account := accountView.GetAccount()
	if current, loadErr := service.topology.Device(ctx, proof.GetDeviceId()); loadErr == nil {
		if current.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || current.AccountID != service.accountID || !bytes.Equal(current.PublicKey, flow.devicePublicKey) {
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
	if assignmentErr != nil || (!needsAssignment && (assignment.Value.GetAccountId() != service.accountID || assignment.Value.GetHubId() != service.hubID || assignment.Value.GetExpiresAtUnixMillis() <= now.UnixMilli())) {
		return nil, errEnrollmentDenied
	}
	policy := &cloudpb.CloudDevicePolicy{AccountId: service.accountID, DeviceId: proof.GetDeviceId(), DeviceKind: cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON, AuthEpoch: account.GetAuthRevision(), PublicKey: append([]byte(nil), flow.devicePublicKey...)}
	if err := service.topology.PutDeviceOwnership(ctx, policy); err != nil {
		return nil, err
	}
	if needsAssignment {
		assignment, assignmentErr = service.registry.Assign(ctx, &cloudpb.HubAssignment{DaemonDeviceId: proof.GetDeviceId(), AccountId: service.accountID, HubId: service.hubID, AssignmentEpoch: 1, NotBeforeUnixMillis: now.Add(-time.Minute).UnixMilli(), ExpiresAtUnixMillis: now.Add(24 * time.Hour).UnixMilli()}, now)
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
	accessToken, err := service.edgeIssuer.IssueEdgeAccessForPrincipal(tokenID, service.hubID, service.accountID, proof.GetDeviceId(), servicecredential.EdgePrincipalDaemon, account.GetAuthRevision(), sessionExpiresAt.Sub(now), now)
	if err != nil {
		return nil, err
	}
	refreshCredential, err := service.commerce.IssueDeviceSession(ctx, service.accountID, proof.GetDeviceId())
	if err != nil {
		return nil, err
	}
	refreshToken := append([]byte(nil), refreshCredential.GetRefreshToken()...)
	refreshExpiresAt := refreshCredential.GetRefreshExpiresAtUnixMillis()
	clear(refreshCredential.AccessToken)
	clear(refreshCredential.RefreshToken)
	if service.notifyPolicyChange != nil {
		service.notifyPolicyChange(service.accountID)
	}
	enrollment := &cloudpb.DaemonControlEnrollment{
		AccountId: service.accountID, DaemonDeviceId: proof.GetDeviceId(), AuthEpoch: account.GetAuthRevision(), EnrolledAtUnixMillis: now.UnixMilli(),
		VerificationKeys: []*cloudpb.DaemonControlVerificationKey{{KeyId: service.controlKeyID, PublicKey: append([]byte(nil), service.controlPublicKey...), NotBeforeUnixMillis: service.controlNotBefore.UnixMilli(), NotAfterUnixMillis: service.controlNotAfter.UnixMilli()}},
	}
	return &cloudpb.DeviceEnrollmentServiceSession{
		Session:     &cloudpb.CloudSessionSummary{AccountLabel: account.GetDisplayName(), AccountId: service.accountID, DeviceId: proof.GetDeviceId(), ExpiresAtUnix: uint64(sessionExpiresAt.Unix())},
		AccessToken: accessToken, RefreshToken: refreshToken, RefreshExpiresAtUnixMillis: refreshExpiresAt,
		HubId: service.hubID, HubDirectoryVersion: 1, ControlEnrollment: enrollment,
	}, nil
}

func (service *enrollmentService) cleanupLocked(now time.Time) {
	for flowID, flow := range service.flows {
		if !now.Before(flow.expiresAt) {
			delete(service.flows, flowID)
		}
	}
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
