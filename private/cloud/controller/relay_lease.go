package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/control-plane/domain"
	"github.com/lozzow/termx/private/cloud/control-plane/entitlement"
	"github.com/lozzow/termx/private/cloud/control-plane/hubregistry"
	"github.com/lozzow/termx/private/cloud/control-plane/relaylease"
	"github.com/lozzow/termx/private/cloud/control-plane/relayquota"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/lozzow/termx/private/cloud/control-plane/sqlite"
	cloudtopology "github.com/lozzow/termx/private/cloud/control-plane/topology"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const relayUsageReportGrace = 2 * time.Minute
const relayProtoMediaType = "application/x-protobuf"

type relayEntitlementSource struct{ store *cloudsqlite.Store }

func (source relayEntitlementSource) Entitlement(ctx context.Context, accountID string) (entitlement.Entitlement, error) {
	projection, err := source.store.Entitlement(ctx, accountID)
	if err != nil {
		return entitlement.Entitlement{}, err
	}
	return entitlement.FromProjection(projection)
}

type relaySessionSource struct {
	topology *cloudtopology.Service
	registry *hubregistry.Registry
}

// ManagedSession 重新验证 edge-authenticated client、target ownership 与当前 assignment。
// ManagedSessionID 是 Hub 生成的连接相关 ID，不是 terminal session，也不授予额外权限。
func (source relaySessionSource) ManagedSession(ctx context.Context, accountID, sessionID, clientDeviceID, targetDeviceID, hubID, region string, now time.Time) (domain.ManagedSession, error) {
	client, clientErr := source.topology.Device(ctx, clientDeviceID)
	target, targetErr := source.topology.Device(ctx, targetDeviceID)
	assignment, assignmentErr := source.registry.Assignment(ctx, targetDeviceID)
	deployment, deploymentErr := source.registry.Deployment(ctx, hubID)
	if sessionID == "" || clientErr != nil || targetErr != nil || assignmentErr != nil || deploymentErr != nil ||
		client.AccountID != accountID || client.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_CLIENT || client.Revoked ||
		target.AccountID != accountID || target.Kind != cloudpb.ManagedDeviceKind_MANAGED_DEVICE_KIND_DAEMON || target.Revoked ||
		assignment.Value.GetAccountId() != accountID || assignment.Value.GetHubId() != hubID || assignment.Value.GetExpiresAtUnixMillis() <= now.UnixMilli() ||
		deployment.Metadata.GetRegion() != region {
		return domain.ManagedSession{}, servicecredential.ErrCredentialBinding
	}
	return domain.ManagedSession{ID: sessionID, AccountID: accountID, ClientDeviceID: clientDeviceID, TargetDeviceID: targetDeviceID, Hub: domain.HubAssignment{HubID: hubID, Region: region}, CreatedAt: now.UTC(), ExpiresAt: time.UnixMilli(assignment.Value.GetExpiresAtUnixMillis()).UTC()}, nil
}

type relayLeaseHTTPHandler struct {
	service     *relaylease.Service
	keyRing     *servicecredential.KeyRing
	deployments map[string]*cloudpb.EdgeDeploymentMetadata
	now         func() time.Time
}

func newRelayLeaseHTTPHandler(store *cloudsqlite.Store, topology *cloudtopology.Service, registry *hubregistry.Registry, signer servicecredential.Signer, deployments []DeploymentConfig) (*relayLeaseHTTPHandler, error) {
	issuer, err := servicecredential.NewRelayLeaseIssuer("termx-cloud-controller-relay", signer)
	if err != nil {
		return nil, err
	}
	service, err := relaylease.NewService(relaySessionSource{topology: topology, registry: registry}, relayEntitlementSource{store: store}, store, issuer, relayUsageReportGrace)
	if err != nil {
		return nil, err
	}
	keyRing, err := servicecredential.NewKeyRing(signer.PublicKey())
	if err != nil {
		return nil, err
	}
	byHub := make(map[string]*cloudpb.EdgeDeploymentMetadata, len(deployments))
	for _, deployment := range deployments {
		if deployment.Metadata == nil || deployment.Metadata.GetHubId() == "" || deployment.Metadata.GetRelayId() == "" || deployment.Metadata.GetRegion() == "" {
			return nil, errors.New("invalid Relay deployment metadata")
		}
		byHub[deployment.Metadata.GetHubId()] = proto.Clone(deployment.Metadata).(*cloudpb.EdgeDeploymentMetadata)
	}
	return &relayLeaseHTTPHandler{service: service, keyRing: keyRing, deployments: byHub, now: time.Now}, nil
}

func (handler *relayLeaseHTTPHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != relayProtoMediaType {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay reservation request is invalid", false)
		return
	}
	token, ok := relayBearer(request.Header.Get("Authorization"))
	if !ok {
		writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay reservation authorization is invalid", false)
		return
	}
	defer clear(token)
	body, err := io.ReadAll(io.LimitReader(request.Body, 4<<20))
	if err != nil {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay reservation request is invalid", false)
		return
	}
	defer clear(body)
	payload := &cloudpb.ReserveRelayLeaseRequest{}
	if proto.Unmarshal(body, payload) != nil || len(payload.ProtoReflect().GetUnknown()) != 0 || payload.GetAccountId() == "" || payload.GetManagedSessionId() == "" || payload.GetClientDeviceId() == "" || payload.GetTargetDeviceId() == "" || payload.GetHubId() == "" || payload.GetRelayId() == "" || payload.GetRegion() == "" || payload.GetLeaseId() == "" {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay reservation payload is invalid", false)
		return
	}
	now := handler.now().UTC()
	metadata := handler.deployments[payload.GetHubId()]
	claims, verifyErr := servicecredential.VerifyEdgeAccess(handler.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: "termx-cloud-controller", AudienceHubID: payload.GetHubId(), AccountID: payload.GetAccountId(), ClientDeviceID: payload.GetClientDeviceId(), PrincipalKind: servicecredential.EdgePrincipalClient}, now)
	if verifyErr != nil {
		claims, verifyErr = servicecredential.VerifyEdgeAccess(handler.keyRing, token, servicecredential.EdgeAccessExpectation{Issuer: "termx-cloud-controller", AudienceHubID: payload.GetHubId(), AccountID: payload.GetAccountId(), ClientDeviceID: payload.GetTargetDeviceId(), PrincipalKind: servicecredential.EdgePrincipalDaemon}, now)
	}
	if verifyErr != nil || metadata == nil || metadata.GetRelayId() != payload.GetRelayId() || metadata.GetRegion() != payload.GetRegion() {
		writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay reservation authorization was rejected", false)
		return
	}
	lease, leaseClaims, err := handler.service.Issue(request.Context(), relaylease.Command{
		LeaseID: payload.GetLeaseId(), AccountID: claims.AccountID, ManagedSessionID: payload.GetManagedSessionId(), ClientDeviceID: payload.GetClientDeviceId(), TargetDeviceID: payload.GetTargetDeviceId(),
		HubID: payload.GetHubId(), RelayID: payload.GetRelayId(), AudienceRelayPool: "pool-" + metadata.GetRegion(), Region: metadata.GetRegion(), PathKind: servicecredential.RelayPathSingle,
		RequestedTTL: 5 * time.Minute, CredentialBindingID: "binding-" + metadata.GetRelayId(),
	}, now)
	if err != nil {
		switch {
		case errors.Is(err, relayquota.ErrQuotaExhausted):
			writeRelayLeaseError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED, "Relay quota is exhausted", true)
		case errors.Is(err, entitlement.ErrNotEntitled), errors.Is(err, entitlement.ErrQuotaPolicy):
			writeRelayLeaseError(writer, http.StatusForbidden, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_ENTITLEMENT_DENIED, "Relay entitlement was denied", false)
		default:
			writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay reservation binding was rejected", false)
		}
		return
	}
	writeRelayLeaseProto(writer, http.StatusOK, &cloudpb.RelayLease{LeaseId: leaseClaims.LeaseID, SignedLease: lease.Bytes(), ExpiresAtUnix: uint64(leaseClaims.ExpiresAtUnix), PathKind: cloudpb.ObservedPath_OBSERVED_PATH_SINGLE_RELAY})
}

func relayBearer(header string) ([]byte, bool) {
	if strings.Count(header, " ") != 1 {
		return nil, false
	}
	scheme, encoded, _ := strings.Cut(header, " ")
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	return value, scheme == "Bearer" && err == nil && len(value) > 0 && len(value) <= 4096
}

func writeRelayLeaseProto(writer http.ResponseWriter, status int, message proto.Message) {
	body, _ := proto.Marshal(message)
	writer.Header().Set("Content-Type", relayProtoMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeRelayLeaseError(writer http.ResponseWriter, status int, code cloudpb.CloudErrorCode, message string, retryable bool) {
	writeRelayLeaseProto(writer, status, &cloudpb.CloudError{Code: code, Message: message, Retryable: retryable})
}
