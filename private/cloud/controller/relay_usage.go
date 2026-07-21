package controller

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	cloudsqlite "github.com/muxvia/muxvia/private/cloud/control-plane/sqlite"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type relayUsageDeployment struct {
	metadata *cloudpb.EdgeDeploymentMetadata
	keyID    string
}

type relayUsageHTTPHandler struct {
	store       *cloudsqlite.Store
	leaseKeys   *servicecredential.KeyRing
	usageKeys   *servicecredential.KeyRing
	deployments map[string]relayUsageDeployment
	now         func() time.Time
}

func newRelayUsageHTTPHandler(store *cloudsqlite.Store, leaseSigner servicecredential.Signer, deployments []DeploymentConfig, now time.Time) (*relayUsageHTTPHandler, error) {
	if store == nil {
		return nil, errors.New("Relay usage store is required")
	}
	leaseKeys, err := servicecredential.NewKeyRing(leaseSigner.PublicKey())
	if err != nil {
		return nil, err
	}
	verificationKeys := make([]servicecredential.VerificationKey, 0, len(deployments))
	byRelay := make(map[string]relayUsageDeployment, len(deployments))
	for _, deployment := range deployments {
		publicKey, decodeErr := base64.RawStdEncoding.DecodeString(deployment.RelayControlPublicKeyBase64)
		metadata := deployment.Metadata
		if decodeErr != nil || len(publicKey) != ed25519.PublicKeySize || metadata == nil || metadata.GetRelayId() == "" || metadata.GetEdgeDeploymentId() == "" || metadata.GetRegion() == "" || metadata.GetRelayControlIdentityFingerprint() != hubregistry.IdentityFingerprint(ed25519.PublicKey(publicKey)) {
			return nil, errors.New("invalid Relay usage deployment")
		}
		keyID := "relay-control-" + metadata.GetRelayId()
		verificationKeys = append(verificationKeys, servicecredential.VerificationKey{ID: keyID, PublicKey: ed25519.PublicKey(publicKey), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour)})
		byRelay[metadata.GetRelayId()] = relayUsageDeployment{metadata: proto.Clone(metadata).(*cloudpb.EdgeDeploymentMetadata), keyID: keyID}
	}
	usageKeys, err := servicecredential.NewKeyRing(verificationKeys...)
	if err != nil {
		return nil, err
	}
	return &relayUsageHTTPHandler{store: store, leaseKeys: leaseKeys, usageKeys: usageKeys, deployments: byRelay, now: time.Now}, nil
}

func (handler *relayUsageHTTPHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != relayProtoMediaType {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage request is invalid", false)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, (4<<20)+1))
	defer clear(body)
	if err != nil || len(body) == 0 || len(body) > 4<<20 {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage request is invalid", false)
		return
	}
	payload := &cloudpb.ReportRelayUsageRequest{}
	if proto.Unmarshal(body, payload) != nil || len(payload.ProtoReflect().GetUnknown()) != 0 || payload.GetRelayId() == "" || payload.GetEdgeDeploymentId() == "" || len(payload.GetRecords()) == 0 || len(payload.GetRecords()) > 128 {
		writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage payload is invalid", false)
		return
	}
	deployment, ok := handler.deployments[payload.GetRelayId()]
	if !ok || deployment.metadata.GetEdgeDeploymentId() != payload.GetEdgeDeploymentId() {
		writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage deployment was rejected", false)
		return
	}
	now := handler.now().UTC()
	response := &cloudpb.ReportRelayUsageResponse{}
	for _, record := range payload.GetRecords() {
		claims, verifyErr := servicecredential.VerifyRelayLeaseForService(handler.leaseKeys, record.GetSignedLease(), "muxvia-cloud-controller-relay", "pool-"+deployment.metadata.GetRegion(), relayUsageReportGrace, now)
		if verifyErr != nil || claims.Region != deployment.metadata.GetRegion() || claims.CredentialBindingID != "binding-"+payload.GetRelayId() {
			writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage lease was rejected", false)
			return
		}
		event, eventErr := usage.FromProto(record.GetEvent())
		if eventErr != nil || event.RelayID != payload.GetRelayId() {
			writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage event was rejected", false)
			return
		}
		digest, verifyErr := usage.VerifyEvent(handler.usageKeys, deployment.keyID, claims, event, now, relayUsageReportGrace)
		if verifyErr != nil {
			writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage signature was rejected", false)
			return
		}
		ack, _, _, applyErr := handler.store.ApplyRelayUsage(request.Context(), record, claims, event, digest, now)
		if applyErr != nil {
			status, code := http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL
			if errors.Is(applyErr, usage.ErrUsageOutOfRange) {
				status, code = http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_QUOTA_EXHAUSTED
			}
			writeRelayLeaseError(writer, status, code, "Relay usage settlement was rejected", false)
			return
		}
		response.Acknowledgements = append(response.Acknowledgements, ack)
	}
	writeRelayLeaseProto(writer, http.StatusOK, response)
}
