package devcloud

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/lozzow/termx/private/cloud/companion/cloudservice/httpapi"
	"github.com/lozzow/termx/private/cloud/companion/session"
	"github.com/lozzow/termx/private/cloud/control-plane/servicecredential"
	cloudhub "github.com/lozzow/termx/private/cloud/hub"
	"github.com/lozzow/termx/proto/cloudpb"
	"google.golang.org/protobuf/proto"
)

const maxRequestBytes = 4 << 20

func requireMethod(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	writeCloudError(writer, http.StatusMethodNotAllowed, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud method is not allowed", false)
	return false
}

func requireNoAuthorization(writer http.ResponseWriter, request *http.Request) bool {
	if request.Header.Get("Authorization") == "" {
		return true
	}
	writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "authorization is not accepted for this operation", false)
	return false
}

func readProto(writer http.ResponseWriter, request *http.Request, target proto.Message) bool {
	if !contentTypeIs(request, httpapi.ProtobufMediaType) {
		writeCloudError(writer, http.StatusUnsupportedMediaType, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud request content type is invalid", false)
		return false
	}
	payload, err := readBody(writer, request)
	if err != nil || len(payload) == 0 {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud protobuf request is invalid", false)
		return false
	}
	defer clear(payload)
	if err := proto.Unmarshal(payload, target); err != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud protobuf request is invalid", false)
		return false
	}
	return true
}

func readJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	if !contentTypeIs(request, httpapi.JSONMediaType) {
		writeCloudError(writer, http.StatusUnsupportedMediaType, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud request content type is invalid", false)
		return false
	}
	payload, err := readBody(writer, request)
	if err != nil || len(payload) == 0 {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud JSON request is invalid", false)
		return false
	}
	defer clear(payload)
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud JSON request is invalid", false)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeCloudError(writer, http.StatusBadRequest, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "cloud JSON request has trailing data", false)
		return false
	}
	return true
}

func readProtoBytes(payload []byte, target proto.Message) error {
	if len(payload) == 0 || len(payload) > maxRequestBytes {
		return fmt.Errorf("invalid protobuf payload size")
	}
	if err := proto.Unmarshal(payload, target); err != nil || len(target.ProtoReflect().GetUnknown()) != 0 {
		return fmt.Errorf("invalid protobuf payload")
	}
	return nil
}

func readBody(writer http.ResponseWriter, request *http.Request) ([]byte, error) {
	reader := http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	defer reader.Close()
	return io.ReadAll(reader)
}

func contentTypeIs(request *http.Request, expected string) bool {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == expected && len(parameters) == 0
}

func writeProto(writer http.ResponseWriter, status int, message proto.Message) {
	payload, err := proto.Marshal(message)
	if err != nil {
		writeCloudError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "cloud response encoding failed", true)
		return
	}
	writer.Header().Set("Content-Type", httpapi.ProtobufMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		writeCloudError(writer, http.StatusInternalServerError, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "cloud response encoding failed", true)
		return
	}
	defer clear(payload)
	writer.Header().Set("Content-Type", httpapi.JSONMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func writeCloudError(writer http.ResponseWriter, status int, code cloudpb.CloudErrorCode, message string, retryable bool) {
	payload, _ := proto.Marshal(&cloudpb.CloudError{Code: code, Message: message, Retryable: retryable})
	writer.Header().Set("Content-Type", httpapi.ProtobufMediaType)
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}

func (state *serviceState) authenticate(writer http.ResponseWriter, request *http.Request, expected session.Kind) (cloudSession, bool) {
	return state.authenticateKinds(writer, request, expected)
}

func (state *serviceState) authenticateKinds(writer http.ResponseWriter, request *http.Request, allowed ...session.Kind) (cloudSession, bool) {
	header := request.Header.Get("Authorization")
	if header == "" || strings.Count(header, " ") != 1 {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud authorization is required", false)
		return cloudSession{}, false
	}
	scheme, encoded, _ := strings.Cut(header, " ")
	if scheme != "Bearer" || encoded == "" {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud authorization is invalid", false)
		return cloudSession{}, false
	}
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(token) == 0 || len(token) > 4096 {
		clear(token)
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud authorization is invalid", false)
		return cloudSession{}, false
	}
	key := sha256.Sum256(token)
	clear(token)
	now := state.now().UTC()
	state.mu.Lock()
	state.cleanupLocked(now)
	storedSession, ok := state.sessions[key]
	state.mu.Unlock()
	if !ok || !containsSessionKind(allowed, storedSession.kind) || !now.Before(storedSession.expiresAt) {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "cloud authorization is invalid", false)
		return cloudSession{}, false
	}
	return storedSession, true
}

func readBearerToken(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	header := request.Header.Get("Authorization")
	if header == "" || strings.Count(header, " ") != 1 {
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub edge authorization is required", false)
		return nil, false
	}
	scheme, encoded, _ := strings.Cut(header, " ")
	token, err := base64.RawURLEncoding.DecodeString(encoded)
	if scheme != "Bearer" || err != nil || len(token) == 0 || len(token) > 4096 {
		clear(token)
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub edge authorization is invalid", false)
		return nil, false
	}
	return token, true
}

func containsSessionKind(allowed []session.Kind, current session.Kind) bool {
	for _, candidate := range allowed {
		if candidate == current {
			return true
		}
	}
	return false
}

func (state *serviceState) issueSession(kind session.Kind, accountID, accountLabel, deviceID string) (cloudSession, []byte, []byte, time.Time, error) {
	cloudSession, token, err := state.issueAccessSession(kind, accountID, accountLabel, deviceID)
	if err != nil {
		return cloudSession, nil, nil, time.Time{}, err
	}
	refreshToken, refreshExpiresAt, err := state.refreshSessions.Issue(refreshRecord{Kind: kind, AccountID: accountID, AccountLabel: accountLabel, DeviceID: deviceID}, state.now().UTC())
	if err != nil {
		state.mu.Lock()
		delete(state.sessions, sha256.Sum256(token))
		state.mu.Unlock()
		clear(token)
		return cloudSession, nil, nil, time.Time{}, err
	}
	return cloudSession, token, refreshToken, refreshExpiresAt, nil
}

func (state *serviceState) issueAccessSession(kind session.Kind, accountID, accountLabel, deviceID string) (cloudSession, []byte, error) {
	tokenID, err := state.randomID("edge-token")
	if err != nil {
		return cloudSession{}, nil, err
	}
	now := state.now().UTC()
	principal := servicecredential.EdgePrincipalClient
	if kind == session.KindDevice {
		principal = servicecredential.EdgePrincipalDaemon
	}
	token, err := state.edgeIssuer.IssueEdgeAccessWithDirectory(tokenID, devHubID, devPublicHubURL, devRegion, 1, accountID, deviceID, principal, 1, cloudSessionTTL, now)
	if err != nil {
		return cloudSession{}, nil, err
	}
	defer clear(token)
	cloudSession := cloudSession{
		kind: kind, accountID: accountID, accountLabel: accountLabel,
		deviceID: deviceID, expiresAt: now.Add(cloudSessionTTL),
	}
	state.mu.Lock()
	state.cleanupLocked(now)
	state.sessions[sha256.Sum256(token)] = cloudSession
	state.mu.Unlock()
	return cloudSession, append([]byte(nil), token...), nil
}

func (state *serviceState) publishEdgeSnapshot(now time.Time) error {
	devices := make([]servicecredential.EdgePolicyDevice, 0, len(state.edgeDevices))
	for _, device := range state.edgeDevices {
		devices = append(devices, servicecredential.EdgePolicyDevice{DeviceID: device.DeviceID, AccountID: device.AccountID, Kind: device.Kind, DisplayName: device.DisplayName, Platform: device.Platform, PublicKey: append([]byte(nil), device.PublicKey...), Revoked: device.Revoked})
	}
	accounts := make([]servicecredential.EdgePolicyAccount, 0, len(state.directoryAccounts)+len(state.webAccounts)+1)
	accountIDs := map[string]struct{}{devAccountID: {}}
	for accountID := range state.directoryAccounts {
		accountIDs[accountID] = struct{}{}
	}
	for accountID := range state.webAccounts {
		accountIDs[accountID] = struct{}{}
	}
	for accountID := range accountIDs {
		maxBytes, maxConcurrency := uint64(64<<20), uint32(2)
		if validUntil, ok := state.webEntitlements[accountID]; ok && now.Before(validUntil) {
			maxBytes, maxConcurrency = 256<<20, 4
		}
		accounts = append(accounts, servicecredential.EdgePolicyAccount{AccountID: accountID, AuthEpoch: 1, ManagedDirectEnabled: true, StandardRelayEnabled: true, RelayMaxLeaseSeconds: uint32(relayLeaseTTL / time.Second), RelayMaxBytes: maxBytes, RelayMaxBitrateKbps: 100_000, RelayMaxConcurrency: maxConcurrency})
	}
	encoded, err := state.edgePolicyIssuer.Issue(devHubID, state.edgeRevision, accounts, devices, 30*time.Minute, now.UTC())
	if err != nil {
		return err
	}
	defer clear(encoded)
	return state.edgeAuth.ApplySignedSnapshot(encoded)
}

// refreshEdgeSnapshot 在不改变账号或设备业务真值时发布新 revision，刷新 Hub 的离线授权窗口。
// 调用方不持有 state.mu；签发或应用失败会恢复 revision，旧快照随后按原 max staleness fail closed。
func (state *serviceState) refreshEdgeSnapshot(now time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	previousRevision := state.edgeRevision
	state.edgeRevision++
	if err := state.publishEdgeSnapshot(now.UTC()); err != nil {
		state.edgeRevision = previousRevision
		return err
	}
	return nil
}

func (state *serviceState) cleanupLocked(now time.Time) {
	for id, flow := range state.loginFlows {
		if !now.Before(flow.expiresAt) {
			delete(state.loginCodes, flow.userCode)
			delete(state.loginFlows, id)
		}
	}
	for code, claim := range state.enrollmentClaims {
		if !now.Before(claim.expiresAt) {
			delete(state.enrollmentClaims, code)
		}
	}
	for id, flow := range state.enrollmentFlows {
		if !now.Before(flow.expiresAt) {
			clear(flow.challenge)
			clear(flow.publicKey)
			delete(state.enrollmentFlows, id)
		}
	}
	for token, cloudSession := range state.sessions {
		if !now.Before(cloudSession.expiresAt) {
			delete(state.sessions, token)
		}
	}
}

func mapHubError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudhub.ErrAdmission), errors.Is(err, cloudhub.ErrEdgeAuthorization):
		writeCloudError(writer, http.StatusUnauthorized, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_UNAUTHENTICATED, "Hub authorization was rejected", false)
	case errors.Is(err, cloudhub.ErrPolicySnapshot):
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub authorization snapshot is unavailable", true)
	case errors.Is(err, cloudhub.ErrTargetUnavailable):
		writeCloudError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "target managed device was removed or is unavailable", false)
	case errors.Is(err, cloudhub.ErrPresenceNotFound):
		writeCloudError(writer, http.StatusNotFound, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_DEVICE_NOT_FOUND, "target device is offline", true)
	case errors.Is(err, cloudhub.ErrBackpressure), errors.Is(err, cloudhub.ErrCapacity):
		writeCloudError(writer, http.StatusTooManyRequests, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_BACKPRESSURE, "Hub signaling capacity is unavailable", true)
	case errors.Is(err, cloudhub.ErrPresenceConflict), errors.Is(err, cloudhub.ErrSessionConflict), errors.Is(err, cloudhub.ErrSessionNotFound), errors.Is(err, cloudhub.ErrInvalidSignal):
		writeCloudError(writer, http.StatusConflict, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_PROTOCOL, "Hub signaling binding was rejected", false)
	default:
		writeCloudError(writer, http.StatusServiceUnavailable, cloudpb.CloudErrorCode_CLOUD_ERROR_CODE_TEMPORARY, "Hub operation failed", true)
	}
}

func signedAt(nanoseconds int64) time.Time {
	return time.Unix(0, nanoseconds).UTC()
}
