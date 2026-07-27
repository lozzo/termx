// Package directoryapi 实现客户端 Cloud Route 解析与短期 ClientTicket 签发。
// 持久 Store 只提供 daemon identity；在线位置唯一来自 Controller Directory actor。
package directoryapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/anytty/anytty/cloud/controller/directory"
	"github.com/anytty/anytty/cloud/controller/edgeconfig"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	"github.com/anytty/anytty/cloud/ticket"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
	"github.com/anytty/anytty/shared/remoteauth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type challengeState struct {
	challenge          []byte
	expires            time.Time
	grant              *cloudv1.SignedEnvelope
	claims             *cloudv1.CloudRouteGrantClaims
	pairingGrant       *cloudv1.SignedEnvelope
	pairingClaimDigest []byte
	clientPublicKey    ed25519.PublicKey
	product            cloudv1.ClientProduct
	accessMode         cloudv1.CloudClientAccessMode
	daemon             enrollment.Daemon
	location           directory.ObjectLocation
}

// Config 固定客户端解析所需的持久身份、纯内存目录、Edge desired state 与 Controller TicketSigner。
type Config struct {
	Store              enrollment.Store
	Directory          *directory.Directory
	Edges              *edgeconfig.Service
	EdgeCACertificate  []byte
	TicketSigningKey   ed25519.PrivateKey
	TicketSigningKeyID string
	ChallengeTTL       time.Duration
	ClientTicketTTL    time.Duration
	Entitlement        interface {
		EffectiveEntitlement(context.Context, string) (*cloudv1.EffectiveEntitlement, error)
	}
	Now func() time.Time
}

// Service 是 DirectoryService 的 application owner；challenge 只在当前 Controller 内存存活。
type Service struct {
	cloudv1.UnimplementedDirectoryServiceServer
	config     Config
	mu         sync.Mutex
	challenges map[string]challengeState
}

// NewService 拒绝缺失 identity store、runtime Directory 或独立票据签名密钥的装配。
func NewService(config Config) (*Service, error) {
	config.TicketSigningKeyID = strings.TrimSpace(config.TicketSigningKeyID)
	if config.Store == nil || config.Directory == nil || config.Edges == nil || config.Entitlement == nil || len(config.EdgeCACertificate) == 0 || len(config.TicketSigningKey) != ed25519.PrivateKeySize ||
		config.TicketSigningKeyID == "" || config.ChallengeTTL <= 0 || config.ClientTicketTTL <= 0 || config.ClientTicketTTL > 2*time.Minute {
		return nil, errors.New("DirectoryService store, runtime directory, Edge state, signer, CA, and bounded TTLs are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config, challenges: make(map[string]challengeState)}, nil
}

// BeginClientRoute 验证 daemon DeviceIdentity grant 后才创建一次性客户端 proof challenge。
func (service *Service) BeginClientRoute(ctx context.Context, request *cloudv1.BeginClientRouteRequest) (*cloudv1.IdentityChallenge, error) {
	grant := request.GetCloudRouteGrant()
	if grant == nil {
		return nil, status.Error(codes.InvalidArgument, "CloudRouteGrant is required")
	}
	unverified := &cloudv1.CloudRouteGrantClaims{}
	if err := proto.Unmarshal(grant.GetPayload(), unverified); err != nil || strings.TrimSpace(unverified.GetDaemonId()) == "" {
		return nil, status.Error(codes.Unauthenticated, "CloudRouteGrant payload is invalid")
	}
	daemon, err := service.config.Store.GetDaemon(ctx, strings.TrimSpace(unverified.GetDaemonId()))
	if err != nil || daemon.Revoked {
		return nil, status.Error(codes.NotFound, "daemon is unavailable")
	}
	claims, err := ticket.VerifyCloudRouteGrant(grant, daemon.DevicePublicKey, daemon.ID, service.now())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}
	location, found, err := service.config.Directory.LocateDaemon(ctx, daemon.ID)
	if err != nil || !found {
		return nil, status.Error(codes.Unavailable, "daemon is offline")
	}
	challenge := make([]byte, remoteauth.DeviceIdentityChallengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := service.now()
	id := uuid.NewString()
	service.mu.Lock()
	service.compactLocked(now)
	service.challenges[id] = challengeState{challenge: challenge, expires: now.Add(service.config.ChallengeTTL), grant: proto.Clone(grant).(*cloudv1.SignedEnvelope), claims: proto.Clone(claims).(*cloudv1.CloudRouteGrantClaims), clientPublicKey: append(ed25519.PublicKey(nil), claims.GetClientPublicKey()...), product: claims.GetProduct(), accessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY, daemon: daemon, location: location}
	service.mu.Unlock()
	return &cloudv1.IdentityChallenge{ChallengeId: id, Challenge: append([]byte(nil), challenge...), ExpiresAt: timestamppb.New(now.Add(service.config.ChallengeTTL))}, nil
}

// BeginPairingRoute 使用 daemon 签名的短期 bootstrap grant 定位 owning daemon，并把未授权客户端限制在 pairing-only signaling。
func (service *Service) BeginPairingRoute(ctx context.Context, request *cloudv1.BeginPairingRouteRequest) (*cloudv1.IdentityChallenge, error) {
	if request == nil || len(request.GetClientPublicKey()) != ed25519.PublicKeySize || request.GetProduct() < cloudv1.ClientProduct_CLIENT_PRODUCT_TUI || request.GetProduct() > cloudv1.ClientProduct_CLIENT_PRODUCT_DESKTOP_GUI {
		return nil, status.Error(codes.InvalidArgument, "pairing route client identity is invalid")
	}
	grant := request.GetPairingRouteGrant()
	if grant == nil {
		return nil, status.Error(codes.InvalidArgument, "PairingRouteGrant is required")
	}
	unverified := &cloudv1.PairingRouteGrantClaims{}
	if err := proto.Unmarshal(grant.GetPayload(), unverified); err != nil || strings.TrimSpace(unverified.GetDaemonId()) == "" {
		return nil, status.Error(codes.Unauthenticated, "PairingRouteGrant payload is invalid")
	}
	daemon, err := service.config.Store.GetDaemon(ctx, strings.TrimSpace(unverified.GetDaemonId()))
	if err != nil || daemon.Revoked {
		return nil, status.Error(codes.NotFound, "daemon is unavailable")
	}
	claims, err := ticket.VerifyPairingRouteGrant(grant, daemon.DevicePublicKey, daemon.ID, service.now())
	if err != nil || claims.GetDeviceId() != daemon.DeviceID {
		return nil, status.Error(codes.Unauthenticated, "PairingRouteGrant is invalid")
	}
	location, found, err := service.config.Directory.LocateDaemon(ctx, daemon.ID)
	if err != nil || !found {
		return nil, status.Error(codes.Unavailable, "daemon is offline")
	}
	challenge := make([]byte, remoteauth.DeviceIdentityChallengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := service.now()
	id := uuid.NewString()
	service.mu.Lock()
	service.compactLocked(now)
	service.challenges[id] = challengeState{
		challenge: challenge, expires: now.Add(service.config.ChallengeTTL), pairingGrant: proto.Clone(grant).(*cloudv1.SignedEnvelope), pairingClaimDigest: append([]byte(nil), claims.GetPairingClaimSha256()...),
		clientPublicKey: append(ed25519.PublicKey(nil), request.GetClientPublicKey()...), product: request.GetProduct(), accessMode: cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING,
		daemon: daemon, location: location,
	}
	service.mu.Unlock()
	return &cloudv1.IdentityChallenge{ChallengeId: id, Challenge: append([]byte(nil), challenge...), ExpiresAt: timestamppb.New(now.Add(service.config.ChallengeTTL))}, nil
}

// ResolveClientRoute 校验 ClientAccessIdentity proof，并按当前 Presence 签发允许独立 RelayLease 的 ClientTicket。
func (service *Service) ResolveClientRoute(ctx context.Context, request *cloudv1.ResolveClientRouteRequest) (*cloudv1.ResolveClientRouteResponse, error) {
	if request == nil || strings.TrimSpace(request.GetRequestId()) == "" {
		return nil, status.Error(codes.InvalidArgument, "client route request ID is required")
	}
	state, err := service.takeChallenge(request.GetChallengeId())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	var canonical []byte
	switch state.accessMode {
	case cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_CAPABILITY:
		canonical, err = ticket.ClientRouteProofBytes(request.GetChallengeId(), state.challenge, state.grant, request.GetRequestId())
	case cloudv1.CloudClientAccessMode_CLOUD_CLIENT_ACCESS_MODE_PAIRING:
		canonical, err = ticket.PairingRouteProofBytes(request.GetChallengeId(), state.challenge, state.pairingGrant, request.GetRequestId())
	default:
		err = errors.New("client route challenge mode is invalid")
	}
	if err != nil || ticket.VerifyClientRouteProof(state.clientPublicKey, request.GetClientProof(), canonical) != nil {
		return nil, status.Error(codes.Unauthenticated, "client route proof is invalid")
	}
	current, found, locateErr := service.config.Directory.LocateDaemon(ctx, state.daemon.ID)
	if locateErr != nil || !found || current != state.location {
		return nil, status.Error(codes.FailedPrecondition, "daemon Presence changed during route resolution")
	}
	edge, err := service.config.Edges.GetEdge(ctx, current.EdgeID)
	if err != nil || !edge.Enabled {
		return nil, status.Error(codes.FailedPrecondition, "target Edge is unavailable")
	}
	entitlement, entitlementErr := service.config.Entitlement.EffectiveEntitlement(ctx, state.daemon.AccountID)
	if entitlementErr != nil || entitlement.GetState() != cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE || !entitlement.GetCapability().GetManagedP2PEnabled() {
		return nil, status.Error(codes.PermissionDenied, "account Cloud entitlement is unavailable")
	}
	now := service.now()
	clientID := remoteauth.Fingerprint(state.clientPublicKey)
	routePolicy := cloudv1.CloudRoutePolicy_CLOUD_ROUTE_POLICY_P2P_ONLY
	if entitlement.GetCapability().GetRelayEnabled() && entitlement.GetRelayRemainingBytes() > 0 {
		routePolicy = cloudv1.CloudRoutePolicy_CLOUD_ROUTE_POLICY_P2P_OR_RELAY
	}
	claims := &cloudv1.ClientTicketClaims{
		TicketId: uuid.NewString(), AccountId: state.daemon.AccountID, EdgeId: current.EdgeID, DaemonId: state.daemon.ID, ClientId: clientID,
		ClientPublicKey: append([]byte(nil), state.clientPublicKey...), Product: state.product, RoutePolicy: routePolicy, AccessMode: state.accessMode,
		PairingClaimSha256: append([]byte(nil), state.pairingClaimDigest...), IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(service.config.ClientTicketTTL)),
	}
	signed, err := ticket.SignClientTicket(service.config.TicketSigningKeyID, service.config.TicketSigningKey, claims)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	projection, online, err := service.config.Directory.Edge(ctx, edge.ID)
	if err != nil || !online {
		return nil, status.Error(codes.FailedPrecondition, "target Edge is offline")
	}
	host := edge.PublicEndpoint
	if parsed, _, splitErr := net.SplitHostPort(edge.PublicEndpoint); splitErr == nil {
		host = parsed
	}
	candidate := &cloudv1.CandidateEdge{EdgeId: edge.ID, Name: edge.Name, Region: edge.Region, PublicEndpoint: edge.PublicEndpoint, ServerName: strings.Trim(host, "[]"), CaCertificatePem: append([]byte(nil), service.config.EdgeCACertificate...), Capacity: edge.Capacity, CurrentAgents: uint64(projection.AgentCount)}
	return &cloudv1.ResolveClientRouteResponse{ClientTicket: signed, Edge: candidate}, nil
}

func (service *Service) takeChallenge(id string) (challengeState, error) {
	now := service.now()
	service.mu.Lock()
	defer service.mu.Unlock()
	service.compactLocked(now)
	state, ok := service.challenges[strings.TrimSpace(id)]
	if !ok {
		return challengeState{}, errors.New("client route challenge is invalid")
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
