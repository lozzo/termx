// Package enrollment 实现 daemon 持久注册、Edge 候选选择和短期 AgentTicket 签发。
package enrollment

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrEnrollmentInvalid 表示一次性注册 code 已失效、过期或被消费。
	ErrEnrollmentInvalid = errors.New("daemon enrollment code is invalid")
	// ErrDaemonIdentityConflict 表示 device ID 或 fingerprint 已属于另一把 DeviceIdentity 公钥。
	ErrDaemonIdentityConflict = errors.New("daemon DeviceIdentity conflicts with an existing device")
	// ErrDaemonUnavailable 表示 daemon 不存在、已撤销或当前没有可用 Edge。
	ErrDaemonUnavailable = errors.New("daemon is unavailable")
)

// Daemon 是 PostgreSQL 保存的持久 daemon identity，不包含当前 Edge 归属。
type Daemon struct {
	ID, AccountID, AccountName, DisplayName, DeviceID, DeviceFingerprint string
	DevicePublicKey                                                      ed25519.PublicKey
	Revoked                                                              bool
	Revision                                                             uint64
	CreatedAt, UpdatedAt                                                 time.Time
}

// Store 是 daemon enrollment 的持久事务边界；Presence 不得实现该接口或写入数据库。
type Store interface {
	CreateDaemonEnrollment(context.Context, string, string, string, []byte, time.Time, time.Time) (string, error)
	ConsumeDaemonEnrollment(context.Context, []byte, string, string, ed25519.PublicKey, time.Time) (Daemon, error)
	GetDaemon(context.Context, string) (Daemon, error)
	ListDaemons(context.Context) ([]Daemon, error)
}

// Config 组合持久 Store、纯内存 Directory、Edge desired state 和 Controller TicketSigner。
type Config struct {
	Store       Store
	Edges       *edgeconfig.Service
	Directory   *directory.Directory
	Entitlement interface {
		EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error)
	}
	TicketSigningKey   ed25519.PrivateKey
	TicketSigningKeyID string
	EdgeCACertificate  []byte
	EnrollmentTTL      time.Duration
	ChallengeTTL       time.Duration
	AgentTicketTTL     time.Duration
	Now                func() time.Time
}

type challengeKind uint8

const (
	challengeEnrollment challengeKind = iota + 1
	challengeAgentTicket
)

type challengeState struct {
	kind                  challengeKind
	value                 []byte
	expires               time.Time
	tokenDigest           []byte
	deviceID, fingerprint string
	publicKey             ed25519.PublicKey
	daemon                Daemon
	edge                  edgeconfig.Edge
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
	config.TicketSigningKeyID = strings.TrimSpace(config.TicketSigningKeyID)
	if config.Store == nil || config.Edges == nil || config.Directory == nil || config.Entitlement == nil || len(config.TicketSigningKey) != ed25519.PrivateKeySize ||
		config.TicketSigningKeyID == "" || len(config.EdgeCACertificate) == 0 || config.EnrollmentTTL <= 0 || config.ChallengeTTL <= 0 || config.AgentTicketTTL <= 0 {
		return nil, errors.New("enrollment store, directory, Edge state, signer, CA, and positive TTLs are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config, challenges: make(map[string]challengeState)}, nil
}

// TicketVerificationKey 返回只能下发给 Edge 的 Controller Ed25519 公钥。
func (service *Service) TicketVerificationKey() *cloudv1.VerificationKey {
	publicKey := service.config.TicketSigningKey.Public().(ed25519.PublicKey)
	return &cloudv1.VerificationKey{KeyId: service.config.TicketSigningKeyID, Algorithm: "Ed25519", PublicKey: append([]byte(nil), publicKey...)}
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
	return &cloudv1.CreateDaemonEnrollmentResponse{AccountId: resolved, EnrollmentCode: code, ExpiresAt: timestamppb.New(expires), EnrollCommand: strings.TrimSpace(commandPrefix) + " " + code}, nil
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
	daemon, err := service.config.Store.ConsumeDaemonEnrollment(ctx, state.tokenDigest, state.deviceID, state.fingerprint, state.publicKey, service.now())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &cloudv1.CompleteDaemonEnrollmentResponse{Daemon: projectDaemon(daemon)}, nil
}

// ListAgentCandidates 返回最多三个当前 ready 且 enabled 的 Edge；选择结果不持久化。
func (service *Service) ListAgentCandidates(ctx context.Context, request *cloudv1.ListAgentCandidatesRequest) (*cloudv1.ListAgentCandidatesResponse, error) {
	daemon, err := service.config.Store.GetDaemon(ctx, strings.TrimSpace(request.GetDaemonId()))
	if err != nil || daemon.Revoked {
		return nil, status.Error(codes.NotFound, ErrDaemonUnavailable.Error())
	}
	candidates, err := service.candidates(ctx, strings.TrimSpace(request.GetPreferredRegion()))
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &cloudv1.ListAgentCandidatesResponse{Candidates: candidates}, nil
}

// BeginAgentTicket 把 daemon 选择的当前在线 Edge 固定到一次性 proof challenge。
func (service *Service) BeginAgentTicket(ctx context.Context, request *cloudv1.BeginAgentTicketRequest) (*cloudv1.IdentityChallenge, error) {
	daemon, err := service.config.Store.GetDaemon(ctx, strings.TrimSpace(request.GetDaemonId()))
	if err != nil || daemon.Revoked {
		return nil, status.Error(codes.NotFound, ErrDaemonUnavailable.Error())
	}
	entitlement, entitlementErr := service.config.Entitlement.EffectiveEntitlement(ctx, daemon.AccountID)
	if entitlementErr != nil || entitlement.GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE || !entitlement.GetCapability().GetManagedP2PEnabled() {
		return nil, status.Error(codes.PermissionDenied, "account Cloud entitlement is unavailable")
	}
	edge, err := service.config.Edges.GetEdge(ctx, strings.TrimSpace(request.GetEdgeId()))
	if err != nil || !edge.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "selected Edge is unavailable")
	}
	if _, found, locateErr := service.config.Directory.Edge(ctx, edge.ID); locateErr != nil || !found {
		return nil, status.Error(codes.FailedPrecondition, "selected Edge is offline")
	}
	return service.newChallenge(challengeState{kind: challengeAgentTicket, daemon: daemon, edge: edge, deviceID: daemon.DeviceID, fingerprint: daemon.DeviceFingerprint, publicKey: daemon.DevicePublicKey})
}

// IssueAgentTicket 验证 daemon proof 后签发默认十分钟短票据，不保存票据或 Edge assignment。
func (service *Service) IssueAgentTicket(ctx context.Context, request *cloudv1.IssueAgentTicketRequest) (*cloudv1.IssueAgentTicketResponse, error) {
	state, err := service.takeChallenge(request.GetChallengeId(), challengeAgentTicket)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	if err := remoteauth.VerifyDeviceIdentityProof(state.value, state.deviceID, state.fingerprint, state.publicKey, request.GetDeviceProof()); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	if _, found, locateErr := service.config.Directory.Edge(ctx, state.edge.ID); locateErr != nil || !found {
		return nil, status.Error(codes.FailedPrecondition, "selected Edge is offline")
	}
	now := service.now()
	claims := &cloudv1.AgentTicketClaims{TicketId: uuid.NewString(), DaemonId: state.daemon.ID, AccountId: state.daemon.AccountID, EdgeId: state.edge.ID, DeviceId: state.daemon.DeviceID, DevicePublicKey: append([]byte(nil), state.daemon.DevicePublicKey...), Capabilities: []cloudv1.AgentCapability{cloudv1.AgentCapability_AGENT_CAPABILITY_SIGNALING}, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(service.config.AgentTicketTTL))}
	signed, err := ticket.SignAgentTicket(service.config.TicketSigningKeyID, service.config.TicketSigningKey, claims)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	candidate, err := service.projectCandidate(ctx, state.edge)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &cloudv1.IssueAgentTicketResponse{AgentTicket: signed, Edge: candidate}, nil
}

func (service *Service) candidates(ctx context.Context, region string) ([]*cloudv1.CandidateEdge, error) {
	edges, err := service.config.Edges.ListEdges(ctx)
	if err != nil {
		return nil, err
	}
	type scored struct {
		edge      edgeconfig.Edge
		preferred bool
		load      float64
	}
	values := make([]scored, 0, len(edges))
	for _, edge := range edges {
		projection, found, locateErr := service.config.Directory.Edge(ctx, edge.ID)
		if locateErr != nil {
			return nil, locateErr
		}
		if !edge.Enabled || !found {
			continue
		}
		values = append(values, scored{edge: edge, preferred: region != "" && edge.Region == region, load: float64(projection.AgentCount) / float64(edge.Capacity)})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].preferred != values[j].preferred {
			return values[i].preferred
		}
		if values[i].load != values[j].load {
			return values[i].load < values[j].load
		}
		return values[i].edge.ID < values[j].edge.ID
	})
	if len(values) > 3 {
		values = values[:3]
	}
	result := make([]*cloudv1.CandidateEdge, 0, len(values))
	for _, value := range values {
		candidate, err := service.projectCandidate(ctx, value.edge)
		if err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (service *Service) projectCandidate(ctx context.Context, edge edgeconfig.Edge) (*cloudv1.CandidateEdge, error) {
	projection, found, err := service.config.Directory.Edge(ctx, edge.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrDaemonUnavailable
	}
	host := edge.PublicEndpoint
	if parsed, _, splitErr := net.SplitHostPort(edge.PublicEndpoint); splitErr == nil {
		host = parsed
	}
	return &cloudv1.CandidateEdge{EdgeId: edge.ID, Name: edge.Name, Region: edge.Region, PublicEndpoint: edge.PublicEndpoint, ServerName: strings.Trim(host, "[]"), CaCertificatePem: append([]byte(nil), service.config.EdgeCACertificate...), Capacity: edge.Capacity, CurrentAgents: uint64(projection.AgentCount)}, nil
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
	return &cloudv1.DaemonRecord{DaemonId: daemon.ID, AccountId: daemon.AccountID, AccountName: daemon.AccountName, DisplayName: daemon.DisplayName, DeviceId: daemon.DeviceID, DeviceFingerprint: daemon.DeviceFingerprint, Revoked: daemon.Revoked, Revision: daemon.Revision, CreatedAt: timestamppb.New(daemon.CreatedAt), UpdatedAt: timestamppb.New(daemon.UpdatedAt)}
}
