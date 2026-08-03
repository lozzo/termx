// Package enrollment 实现 daemon 持久注册以及一次性 Edge binding 签发。
package enrollment

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrEnrollmentInvalid 表示一次性注册 code 已失效、过期或被消费。
	ErrEnrollmentInvalid = errors.New("daemon enrollment code is invalid")
	// ErrDaemonIdentityConflict 表示 device ID 或 fingerprint 已属于另一把 DeviceIdentity 公钥。
	ErrDaemonIdentityConflict = errors.New("daemon DeviceIdentity conflicts with an existing device")
	// ErrDaemonUnavailable 表示 daemon 不存在、已撤销或当前没有可用 Edge。
	ErrDaemonUnavailable = errors.New("daemon is unavailable")
	// ErrDaemonLimitExhausted 表示账号的未删除 daemon 已达到当前套餐上限。
	ErrDaemonLimitExhausted = errors.New("cloud daemon limit is exhausted")
)

// Daemon 是 PostgreSQL 保存的持久 daemon identity，不包含当前 Edge 归属。
type Daemon struct {
	ID, AccountID, AccountName, DisplayName, DeviceID, DeviceFingerprint string
	DevicePublicKey                                                      ed25519.PublicKey
	State                                                                cloudv1.DaemonState
	StateRevision                                                        uint64
	PreferredEdgeID                                                      string
	EdgePreferenceRevision                                               uint64
	EdgePreferenceUpdatedAt                                              time.Time
	CreatedAt, UpdatedAt                                                 time.Time
}

// EdgeMeasurement 是 daemon 从自身网络位置测得的 TCP/TLS/gRPC 连接质量。
type EdgeMeasurement struct {
	EdgeID                string
	Reachable             bool
	ConnectLatencyMS      uint32
	ConnectionFailureRate float64
	SampleCount           uint32
	MeasuredAt            time.Time
}

// EdgeSelectionStore 持久化 Edge 偏好和短期测量；它与 enrollment token 事务相互独立。
type EdgeSelectionStore interface {
	ListDaemonEdgeMeasurements(context.Context, string) ([]EdgeMeasurement, error)
	UpsertDaemonEdgeMeasurements(context.Context, string, []EdgeMeasurement) error
	ChangeDaemonEdgePreference(context.Context, string, string, string, uint64, time.Time) (Daemon, error)
}

// Store 是 daemon enrollment 的持久事务边界；Presence 不得实现该接口或写入数据库。
type Store interface {
	CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error)
	GetDaemonEnrollmentAccount(context.Context, []byte, time.Time) (string, error)
	ConsumeDaemonEnrollment(context.Context, []byte, string, string, ed25519.PublicKey, time.Time) (Daemon, error)
	GetDaemon(context.Context, string) (Daemon, error)
	ListDaemons(context.Context) ([]Daemon, error)
	ListDaemonsByAccount(context.Context, string) ([]Daemon, error)
}

// Config 组合持久 Store、纯内存 Directory、Edge desired state 和 Controller binding signer。
type Config struct {
	Store       Store
	Edges       *edgeconfig.Service
	Directory   *directory.Directory
	Entitlement interface {
		EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error)
	}
	BindingSigningKey   ed25519.PrivateKey
	BindingSigningKeyID string
	EdgeCACertificate   []byte
	EnrollmentTTL       time.Duration
	ChallengeTTL        time.Duration
	BindingTTL          time.Duration
	StateChanged        func(*cloudv1.DaemonStateRecord)
	Now                 func() time.Time
}

type challengeKind uint8

const (
	challengeEnrollment challengeKind = iota + 1
	challengeBindingRefresh
)

type challengeState struct {
	kind                  challengeKind
	value                 []byte
	expires               time.Time
	tokenDigest           []byte
	daemonID              string
	deviceID, fingerprint string
	publicKey             ed25519.PublicKey
}

// Service 是 EnrollmentService gRPC 实现，也是运营管理页创建 code/列 daemon 的 application owner。
type Service struct {
	cloudv1.UnimplementedEnrollmentServiceServer
	config     Config
	mu         sync.Mutex
	challenges map[string]challengeState
}

// NewService 验证所有 owner 和期限，避免启动部分可用的收费准入路径。
func NewService(config Config) (*Service, error) {
	config.BindingSigningKeyID = strings.TrimSpace(config.BindingSigningKeyID)
	if config.Store == nil || config.Edges == nil || config.Directory == nil || config.Entitlement == nil || len(config.BindingSigningKey) != ed25519.PrivateKeySize ||
		config.BindingSigningKeyID == "" || len(config.EdgeCACertificate) == 0 || config.EnrollmentTTL <= 0 || config.ChallengeTTL <= 0 || config.BindingTTL <= 0 {
		return nil, errors.New("enrollment store, directory, Edge state, signer, CA, and positive TTLs are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config, challenges: make(map[string]challengeState)}, nil
}

// CreateEnrollment 为已存在账号创建至少 192 bit 随机 code，数据库只接收摘要。
func (service *Service) CreateEnrollment(ctx context.Context, request *cloudv1.CreateDaemonEnrollmentRequest, commandPrefix string) (*cloudv1.CreateDaemonEnrollmentResponse, error) {
	if request == nil || strings.TrimSpace(request.GetDaemonName()) == "" {
		return nil, errors.New("account ID and daemon name are required")
	}
	accountID := strings.TrimSpace(request.GetAccountId())
	if _, err := uuid.Parse(accountID); err != nil {
		return nil, errors.New("account ID must be UUID")
	}
	daemonCount, daemonLimit, err := service.daemonCapacity(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if daemonCount >= daemonLimit {
		return nil, ErrDaemonLimitExhausted
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	code := "mxe_" + fmt.Sprintf("%x", raw)
	digest := sha256.Sum256([]byte(code))
	now := service.now()
	expires := now.Add(service.config.EnrollmentTTL)
	resolved, err := service.config.Store.CreateDaemonEnrollment(ctx, accountID, strings.TrimSpace(request.GetAccountName()), strings.TrimSpace(request.GetDaemonName()), digest[:], expires, now)
	if err != nil {
		return nil, err
	}
	return &cloudv1.CreateDaemonEnrollmentResponse{AccountId: resolved, EnrollmentCode: code, ExpiresAt: timestamppb.New(expires), EnrollCommand: strings.TrimSpace(commandPrefix) + " " + code, DaemonCount: daemonCount, DaemonLimit: daemonLimit}, nil
}

func (service *Service) daemonCapacity(ctx context.Context, accountID string) (uint32, uint32, error) {
	entitlement, err := service.config.Entitlement.EffectiveEntitlement(ctx, accountID)
	if err != nil || entitlement.GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE || !entitlement.GetCapability().GetManagedP2PEnabled() || entitlement.GetCapability().GetCloudDaemonLimit() == 0 {
		return 0, 0, ErrDaemonUnavailable
	}
	daemons, err := service.config.Store.ListDaemonsByAccount(ctx, accountID)
	if err != nil {
		return 0, 0, err
	}
	return uint32(len(daemons)), entitlement.GetCapability().GetCloudDaemonLimit(), nil
}

// ListManagedDaemons 合并持久身份与 Directory 实时归属，不把投影写回 PostgreSQL。
func (service *Service) ListManagedDaemons(ctx context.Context) (*cloudv1.ListDaemonsResponse, error) {
	daemons, err := service.config.Store.ListDaemons(ctx)
	if err != nil {
		return nil, err
	}
	managed, err := service.projectManagedDaemons(ctx, daemons)
	if err != nil {
		return nil, err
	}
	return &cloudv1.ListDaemonsResponse{Daemons: managed}, nil
}

// projectManagedDaemons 只把调用方已完成账号过滤的 identity 与当前 Directory 投影合并。
func (service *Service) projectManagedDaemons(ctx context.Context, daemons []Daemon) ([]*cloudv1.ManagedDaemon, error) {
	edges, err := service.config.Edges.ListEdges(ctx)
	if err != nil {
		return nil, err
	}
	edgeByID := make(map[string]edgeconfig.Edge, len(edges))
	for _, edge := range edges {
		edgeByID[edge.ID] = edge
	}
	response := make([]*cloudv1.ManagedDaemon, 0, len(daemons))
	for _, daemon := range daemons {
		managed := &cloudv1.ManagedDaemon{Daemon: projectDaemon(daemon), Runtime: &cloudv1.DaemonRuntimeProjection{}}
		if location, found, locateErr := service.config.Directory.LocateDaemon(ctx, daemon.ID); locateErr != nil {
			return nil, locateErr
		} else if found {
			edge := edgeByID[location.EdgeID]
			managed.Runtime = &cloudv1.DaemonRuntimeProjection{Online: true, EdgeId: location.EdgeID, EdgeName: edge.Name, EdgeRegion: edge.Region, EdgePublicEndpoint: edge.PublicEndpoint, BootId: location.BootID, ConnectionId: location.ConnectionID, Generation: location.Generation}
		}
		response = append(response, managed)
	}
	return response, nil
}

// BeginDaemonEnrollment 创建一次性内存 challenge；此时不消费持久 enrollment code。
func (service *Service) BeginDaemonEnrollment(_ context.Context, request *cloudv1.BeginDaemonEnrollmentRequest) (*cloudv1.IdentityChallenge, error) {
	if request == nil || strings.TrimSpace(request.GetEnrollmentCode()) == "" || strings.TrimSpace(request.GetDeviceId()) == "" || strings.TrimSpace(request.GetDeviceFingerprint()) == "" || len(request.GetDevicePublicKey()) != ed25519.PublicKeySize {
		return nil, status.Error(codes.InvalidArgument, "enrollment identity is incomplete")
	}
	if remoteauth.Fingerprint(ed25519.PublicKey(request.GetDevicePublicKey())) != strings.TrimSpace(request.GetDeviceFingerprint()) {
		return nil, status.Error(codes.InvalidArgument, "device fingerprint does not match public key")
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(request.GetEnrollmentCode())))
	state := challengeState{kind: challengeEnrollment, tokenDigest: digest[:], deviceID: strings.TrimSpace(request.GetDeviceId()), fingerprint: strings.TrimSpace(request.GetDeviceFingerprint()), publicKey: append(ed25519.PublicKey(nil), request.GetDevicePublicKey()...)}
	return service.newChallenge(state)
}

// CompleteDaemonEnrollment 验证 DeviceIdentity proof 后在一个数据库事务中消费 code 并创建 identity。
func (service *Service) CompleteDaemonEnrollment(ctx context.Context, request *cloudv1.CompleteDaemonEnrollmentRequest) (*cloudv1.CompleteDaemonEnrollmentResponse, error) {
	state, err := service.takeChallenge(request.GetChallengeId(), challengeEnrollment)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	if err := remoteauth.VerifyDeviceIdentityProof(state.value, state.deviceID, state.fingerprint, state.publicKey, request.GetDeviceProof()); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	now := service.now()
	accountID, err := service.config.Store.GetDaemonEnrollmentAccount(ctx, state.tokenDigest, now)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	entitlement, entitlementErr := service.config.Entitlement.EffectiveEntitlement(ctx, accountID)
	if entitlementErr != nil || entitlement.GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE || !entitlement.GetCapability().GetManagedP2PEnabled() {
		return nil, status.Error(codes.PermissionDenied, "account Cloud entitlement is unavailable")
	}
	edge, _, err := service.selectEdge(ctx, Daemon{}, "")
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	locator, err := service.projectLocator(ctx, edge)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	locatorPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(locator)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	locatorDigest := sha256.Sum256(locatorPayload)
	daemon, err := service.config.Store.ConsumeDaemonEnrollment(ctx, state.tokenDigest, state.deviceID, state.fingerprint, state.publicKey, now)
	if err != nil {
		if errors.Is(err, ErrDaemonLimitExhausted) {
			return nil, status.Error(codes.ResourceExhausted, "cloud_daemon_limit_exhausted")
		}
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	signed, err := service.signDaemonBinding(daemon, edge.ID, locatorDigest, now)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if service.config.StateChanged != nil {
		service.config.StateChanged(&cloudv1.DaemonStateRecord{DaemonId: daemon.ID, State: daemon.State, StateRevision: daemon.StateRevision})
	}
	response := &cloudv1.CompleteDaemonEnrollmentResponse{
		Daemon: projectDaemon(daemon), DaemonBinding: signed, EdgeLocator: locator,
		DaemonLimit: entitlement.GetCapability().GetCloudDaemonLimit(),
	}
	if daemons, listErr := service.config.Store.ListDaemonsByAccount(ctx, daemon.AccountID); listErr == nil {
		response.DaemonCount = uint32(len(daemons))
	}
	return response, nil
}

// BeginDaemonBindingRefresh authenticates an existing daemon from the persistent identity,
// rather than accepting identity or account material supplied by the caller.
func (service *Service) BeginDaemonBindingRefresh(ctx context.Context, request *cloudv1.BeginDaemonBindingRefreshRequest) (*cloudv1.IdentityChallenge, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "daemon binding refresh is incomplete")
	}
	daemonID := strings.TrimSpace(request.GetDaemonId())
	if _, err := uuid.Parse(daemonID); err != nil {
		return nil, status.Error(codes.InvalidArgument, "daemon ID must be UUID")
	}
	daemon, err := service.config.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return nil, status.Error(codes.NotFound, ErrDaemonUnavailable.Error())
	}
	return service.newChallenge(challengeState{
		kind: challengeBindingRefresh, daemonID: daemon.ID,
		deviceID: daemon.DeviceID, fingerprint: daemon.DeviceFingerprint, publicKey: append(ed25519.PublicKey(nil), daemon.DevicePublicKey...),
	})
}

// CompleteDaemonBindingRefresh re-reads lifecycle and identity after proof verification.
// ACTIVE and BLOCKED keep a control connection; DELETED receives no new route material.
func (service *Service) CompleteDaemonBindingRefresh(ctx context.Context, request *cloudv1.CompleteDaemonBindingRefreshRequest) (*cloudv1.RefreshDaemonBindingResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "daemon binding refresh is incomplete")
	}
	state, err := service.takeChallenge(request.GetChallengeId(), challengeBindingRefresh)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	if err := remoteauth.VerifyDeviceIdentityProof(state.value, state.deviceID, state.fingerprint, state.publicKey, request.GetDeviceProof()); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	daemon, err := service.config.Store.GetDaemon(ctx, state.daemonID)
	if err != nil || daemon.DeviceID != state.deviceID || daemon.DeviceFingerprint != state.fingerprint || !bytes.Equal(daemon.DevicePublicKey, state.publicKey) {
		return nil, status.Error(codes.NotFound, ErrDaemonUnavailable.Error())
	}
	response := &cloudv1.RefreshDaemonBindingResponse{Daemon: projectDaemon(daemon)}
	if daemon.State == cloudv1.DaemonState_DAEMON_STATE_DELETED {
		return response, nil
	}
	if daemon.State != cloudv1.DaemonState_DAEMON_STATE_ACTIVE && daemon.State != cloudv1.DaemonState_DAEMON_STATE_BLOCKED {
		return nil, status.Error(codes.FailedPrecondition, ErrDaemonUnavailable.Error())
	}
	selectionStore, hasSelectionStore := service.config.Store.(EdgeSelectionStore)
	if request.GetChangePreference() {
		if !hasSelectionStore || request.GetExpectedPreferenceRevision() == 0 {
			return nil, status.Error(codes.FailedPrecondition, "daemon Edge preference revision is required")
		}
		preferredEdgeID := strings.TrimSpace(request.GetPreferredEdgeId())
		if preferredEdgeID != "" {
			edges, listErr := service.config.Edges.ListEdges(ctx)
			if listErr != nil {
				return nil, status.Error(codes.Internal, listErr.Error())
			}
			found := false
			for _, edge := range edges {
				found = found || edge.ID == preferredEdgeID
			}
			if !found {
				return nil, status.Error(codes.InvalidArgument, "preferred Edge does not exist")
			}
		}
		daemon, err = selectionStore.ChangeDaemonEdgePreference(ctx, daemon.AccountID, daemon.ID, preferredEdgeID, request.GetExpectedPreferenceRevision(), service.now())
		if err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		response.Daemon = projectDaemon(daemon)
	}
	if len(request.GetEdgeMeasurements()) > 0 {
		if !hasSelectionStore {
			return nil, status.Error(codes.Unimplemented, "daemon Edge measurements are unavailable")
		}
		measurements, validateErr := service.acceptMeasurements(ctx, request.GetEdgeMeasurements())
		if validateErr != nil {
			return nil, status.Error(codes.InvalidArgument, validateErr.Error())
		}
		if err := selectionStore.UpsertDaemonEdgeMeasurements(ctx, daemon.ID, measurements); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	currentEdgeID := ""
	if location, found, locateErr := service.config.Directory.LocateDaemon(ctx, daemon.ID); locateErr != nil {
		return nil, status.Error(codes.Internal, locateErr.Error())
	} else if found {
		currentEdgeID = location.EdgeID
	}
	edge, selection, err := service.selectEdge(ctx, daemon, currentEdgeID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	locator, err := service.projectLocator(ctx, edge)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	locatorPayload, err := proto.MarshalOptions{Deterministic: true}.Marshal(locator)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	locatorDigest := sha256.Sum256(locatorPayload)
	signed, err := service.signDaemonBinding(daemon, edge.ID, locatorDigest, service.now())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	response.DaemonBinding = signed
	response.EdgeLocator = locator
	response.EdgeSelection = selection
	return response, nil
}

func (service *Service) signDaemonBinding(daemon Daemon, edgeID string, locatorDigest [sha256.Size]byte, now time.Time) (*cloudv1.SignedEnvelope, error) {
	claims := &cloudv1.DaemonBindingClaims{
		BindingId: uuid.NewString(), DaemonId: daemon.ID, AccountId: daemon.AccountID, EdgeId: edgeID,
		DeviceId: daemon.DeviceID, DevicePublicKey: append([]byte(nil), daemon.DevicePublicKey...),
		Capabilities: []cloudv1.DaemonCapability{cloudv1.DaemonCapability_DAEMON_CAPABILITY_SIGNALING},
		IssuedAt:     timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(service.config.BindingTTL)), EdgeLocatorSha256: locatorDigest[:],
	}
	return ticket.SignDaemonBinding(service.config.BindingSigningKeyID, service.config.BindingSigningKey, claims)
}

func (service *Service) selectEdge(ctx context.Context, daemon Daemon, currentEdgeID string) (edgeconfig.Edge, *cloudv1.DaemonEdgeSelection, error) {
	edges, err := service.config.Edges.ListEdges(ctx)
	if err != nil {
		return edgeconfig.Edge{}, nil, err
	}
	measurements := make(map[string]EdgeMeasurement)
	if daemon.ID != "" {
		if store, ok := service.config.Store.(EdgeSelectionStore); ok {
			values, listErr := store.ListDaemonEdgeMeasurements(ctx, daemon.ID)
			if listErr != nil {
				return edgeconfig.Edge{}, nil, listErr
			}
			for _, value := range values {
				measurements[value.EdgeID] = value
			}
		}
	}
	now := service.now()
	type scored struct {
		edge       edgeconfig.Edge
		projection directory.EdgeProjection
		score      float64
		candidate  *cloudv1.DaemonEdgeCandidate
	}
	values := make([]scored, 0, len(edges))
	candidates := make([]*cloudv1.DaemonEdgeCandidate, 0, len(edges))
	for _, edge := range edges {
		projection, found, locateErr := service.config.Directory.Edge(ctx, edge.ID)
		if locateErr != nil {
			return edgeconfig.Edge{}, nil, locateErr
		}
		load := 1.0
		if edge.Capacity > 0 && found {
			load = float64(projection.AgentCount) / float64(edge.Capacity)
		}
		measurement, measured := measurements[edge.ID]
		fresh := measured && now.Sub(measurement.MeasuredAt) <= 10*time.Minute && measurement.MeasuredAt.Sub(now) <= time.Minute
		eligible := edge.Enabled && found && uint64(projection.AgentCount) < edge.Capacity && (!fresh || measurement.Reachable)
		score := load * 200
		statusText := "可用"
		if fresh {
			score += float64(measurement.ConnectLatencyMS) + measurement.ConnectionFailureRate*1000
		}
		if edge.ID == daemon.PreferredEdgeID {
			score -= 75
		}
		if edge.ID == currentEdgeID {
			score -= 15
		}
		switch {
		case !edge.Enabled:
			statusText = "已停用"
		case !found:
			statusText = "离线"
		case uint64(projection.AgentCount) >= edge.Capacity:
			statusText = "容量已满"
		case fresh && !measurement.Reachable:
			statusText = "当前网络不可达"
		case !fresh:
			statusText = "等待测速"
		}
		candidate := &cloudv1.DaemonEdgeCandidate{Locator: service.configuredLocator(edge), Online: found, Eligible: eligible, AgentCount: uint64(projection.AgentCount), Capacity: edge.Capacity, Preferred: edge.ID == daemon.PreferredEdgeID, Current: edge.ID == currentEdgeID, Score: score, Status: statusText}
		if measured {
			candidate.Measurement = projectEdgeMeasurement(measurement)
		}
		candidates = append(candidates, candidate)
		if eligible {
			values = append(values, scored{edge: edge, projection: projection, score: score, candidate: candidate})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].score != values[j].score {
			return values[i].score < values[j].score
		}
		return values[i].edge.ID < values[j].edge.ID
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].GetEligible() != candidates[j].GetEligible() {
			return candidates[i].GetEligible()
		}
		if candidates[i].GetScore() != candidates[j].GetScore() {
			return candidates[i].GetScore() < candidates[j].GetScore()
		}
		return candidates[i].GetLocator().GetEdgeId() < candidates[j].GetLocator().GetEdgeId()
	})
	selection := &cloudv1.DaemonEdgeSelection{DaemonId: daemon.ID, PreferredEdgeId: daemon.PreferredEdgeID, PreferenceRevision: daemon.EdgePreferenceRevision, CurrentEdgeId: currentEdgeID, Candidates: candidates, EvaluatedAt: timestamppb.New(now)}
	if len(values) == 0 {
		return edgeconfig.Edge{}, selection, ErrDaemonUnavailable
	}
	selection.SelectedEdgeId = values[0].edge.ID
	return values[0].edge, selection, nil
}

func (service *Service) acceptMeasurements(ctx context.Context, values []*cloudv1.DaemonEdgeMeasurement) ([]EdgeMeasurement, error) {
	edges, err := service.config.Edges.ListEdges(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		known[edge.ID] = struct{}{}
	}
	now := service.now()
	result := make([]EdgeMeasurement, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		edgeID := strings.TrimSpace(value.GetEdgeId())
		_, exists := known[edgeID]
		_, duplicate := seen[edgeID]
		failureRate := value.GetConnectionFailureRate()
		if !exists || duplicate || value.GetSampleCount() == 0 || value.GetSampleCount() > 20 || value.GetConnectLatencyMs() > 60_000 || math.IsNaN(failureRate) || math.IsInf(failureRate, 0) || failureRate < 0 || failureRate > 1 {
			return nil, errors.New("Edge measurement is invalid")
		}
		seen[edgeID] = struct{}{}
		result = append(result, EdgeMeasurement{EdgeID: edgeID, Reachable: value.GetReachable(), ConnectLatencyMS: value.GetConnectLatencyMs(), ConnectionFailureRate: value.GetConnectionFailureRate(), SampleCount: value.GetSampleCount(), MeasuredAt: now})
	}
	return result, nil
}

func projectEdgeMeasurement(value EdgeMeasurement) *cloudv1.DaemonEdgeMeasurement {
	return &cloudv1.DaemonEdgeMeasurement{EdgeId: value.EdgeID, Reachable: value.Reachable, ConnectLatencyMs: value.ConnectLatencyMS, ConnectionFailureRate: value.ConnectionFailureRate, SampleCount: value.SampleCount, MeasuredAt: timestamppb.New(value.MeasuredAt)}
}

func (service *Service) projectLocator(ctx context.Context, edge edgeconfig.Edge) (*cloudv1.EdgeLocator, error) {
	_, found, err := service.config.Directory.Edge(ctx, edge.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrDaemonUnavailable
	}
	return service.configuredLocator(edge), nil
}

func (service *Service) configuredLocator(edge edgeconfig.Edge) *cloudv1.EdgeLocator {
	host := edge.PublicEndpoint
	if parsed, _, splitErr := net.SplitHostPort(edge.PublicEndpoint); splitErr == nil {
		host = parsed
	}
	return &cloudv1.EdgeLocator{EdgeId: edge.ID, Name: edge.Name, Region: edge.Region, PublicEndpoint: edge.PublicEndpoint, ServerName: strings.Trim(host, "[]"), CaCertificatePem: append([]byte(nil), service.config.EdgeCACertificate...), Revision: edge.Revision}
}

func (service *Service) newChallenge(state challengeState) (*cloudv1.IdentityChallenge, error) {
	value := make([]byte, remoteauth.DeviceIdentityChallengeBytes)
	if _, err := rand.Read(value); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := service.now()
	state.value = value
	state.expires = now.Add(service.config.ChallengeTTL)
	id := uuid.NewString()
	service.mu.Lock()
	service.compactLocked(now)
	service.challenges[id] = state
	service.mu.Unlock()
	return &cloudv1.IdentityChallenge{ChallengeId: id, Challenge: append([]byte(nil), value...), ExpiresAt: timestamppb.New(state.expires)}, nil
}

func (service *Service) takeChallenge(id string, kind challengeKind) (challengeState, error) {
	now := service.now()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.compactLocked(now)
	state, ok := service.challenges[strings.TrimSpace(id)]
	if !ok || state.kind != kind {
		return challengeState{}, errors.New("identity challenge is invalid")
	}
	delete(service.challenges, strings.TrimSpace(id))
	return state, nil
}

func (service *Service) compactLocked(now time.Time) {
	for id, state := range service.challenges {
		if !state.expires.After(now) {
			delete(service.challenges, id)
		}
	}
}
func (service *Service) now() time.Time { return service.config.Now().UTC() }

func projectDaemon(daemon Daemon) *cloudv1.DaemonRecord {
	record := &cloudv1.DaemonRecord{DaemonId: daemon.ID, AccountId: daemon.AccountID, AccountName: daemon.AccountName, DisplayName: daemon.DisplayName, DeviceId: daemon.DeviceID, DeviceFingerprint: daemon.DeviceFingerprint, State: daemon.State, StateRevision: daemon.StateRevision, CreatedAt: timestamppb.New(daemon.CreatedAt), UpdatedAt: timestamppb.New(daemon.UpdatedAt), PreferredEdgeId: daemon.PreferredEdgeID, EdgePreferenceRevision: daemon.EdgePreferenceRevision}
	if !daemon.EdgePreferenceUpdatedAt.IsZero() {
		record.EdgePreferenceUpdatedAt = timestamppb.New(daemon.EdgePreferenceUpdatedAt)
	}
	return record
}
