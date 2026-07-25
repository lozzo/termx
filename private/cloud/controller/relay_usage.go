package controller

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/muxvia/muxvia/private/cloud/control-plane/hubregistry"
	"github.com/muxvia/muxvia/private/cloud/control-plane/servicecredential"
	"github.com/muxvia/muxvia/private/cloud/control-plane/usage"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

type relayUsageHTTPHandler struct {
	store               usage.Store
	leaseKeys           *servicecredential.KeyRing
	registry            *hubregistry.Registry
	credentialNotBefore time.Time
	credentialNotAfter  time.Time
	now                 func() time.Time
}

func newRelayUsageHTTPHandler(store usage.Store, leaseSigner servicecredential.Signer, registry *hubregistry.Registry, credentialNotBefore, credentialNotAfter time.Time) (*relayUsageHTTPHandler, error) {
	if store == nil || registry == nil {
		return nil, errors.New("Relay usage store is required")
	}
	if credentialNotBefore.IsZero() || !credentialNotAfter.After(credentialNotBefore) {
		return nil, errors.New("Relay usage credential window is invalid")
	}
	leaseKeys, err := servicecredential.NewKeyRing(leaseSigner.PublicKey())
	if err != nil {
		return nil, err
	}
	return &relayUsageHTTPHandler{store: store, leaseKeys: leaseKeys, registry: registry, credentialNotBefore: credentialNotBefore, credentialNotAfter: credentialNotAfter, now: time.Now}, nil
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
	deployment, deploymentErr := handler.registry.DeploymentByRelay(request.Context(), payload.GetRelayId())
	if deploymentErr != nil || !deployment.IdentityApproved || !deployment.Enabled || deployment.Archived || deployment.Metadata.GetEdgeDeploymentId() != payload.GetEdgeDeploymentId() {
		writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage deployment was rejected", false)
		return
	}
	now := handler.now().UTC()
	keyID := "relay-control-" + deployment.Metadata.GetRelayId()
	// Relay 身份密钥的有效期由 Controller 部署配置持有；目录标签或 URL 更新不能改变
	// 已签发 usage event 的验签窗口，否则一次无关编辑会丢失尚未结算的用量。
	usageKeys, keyErr := servicecredential.NewKeyRing(servicecredential.VerificationKey{ID: keyID, PublicKey: deployment.RelayControlPublicKey, NotBefore: handler.credentialNotBefore, NotAfter: handler.credentialNotAfter})
	if keyErr != nil {
		writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage deployment was rejected", false)
		return
	}
	response := &cloudpb.ReportRelayUsageResponse{}
	for _, record := range payload.GetRecords() {
		claims, verifyErr := servicecredential.VerifyRelayLeaseForService(handler.leaseKeys, record.GetSignedLease(), "muxvia-cloud-controller-relay", "pool-"+deployment.Metadata.GetRegion(), relayUsageReportGrace, now)
		if verifyErr != nil || claims.Region != deployment.Metadata.GetRegion() || claims.CredentialBindingID != "binding-"+payload.GetRelayId() {
			writeRelayLeaseError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Relay usage lease was rejected", false)
			return
		}
		event, eventErr := usage.FromProto(record.GetEvent())
		if eventErr != nil || event.RelayID != payload.GetRelayId() {
			writeRelayLeaseError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Relay usage event was rejected", false)
			return
		}
		digest, verifyErr := usage.VerifyEvent(usageKeys, keyID, claims, event, now, relayUsageReportGrace)
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
