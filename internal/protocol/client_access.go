package protocol

import (
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// ClientAccessScope 是 daemon client-access service 在认证后 protocol 边界使用的显式 scope。
// 它只承载 ticket scope ceiling 和脱敏 grant projection，不拥有 core terminal truth；ManageClientAccess 不从其他字段推导。
type ClientAccessScope struct {
	AllowDaemon        bool   `json:"allow_daemon,omitempty"`
	TerminalID         string `json:"terminal_id,omitempty"`
	MachineEventsOnly  bool   `json:"machine_events_only,omitempty"`
	FileReadMetadata   bool   `json:"file_read_metadata,omitempty"`
	FileReadContent    bool   `json:"file_read_content,omitempty"`
	FileWriteContent   bool   `json:"file_write_content,omitempty"`
	FileMutate         bool   `json:"file_mutate,omitempty"`
	ManageClientAccess bool   `json:"manage_client_access,omitempty"`
}

// ClientAccessIdentityResult 是 local owner 查询 daemon 全局 DeviceIdentity 的公开投影。
// PublicKey 用于验证 ticket/bootstrap signature；结果不包含 daemon private key、Cloud enrollment 或 route 地址。
type ClientAccessIdentityResult struct {
	DeviceID          string `json:"device_id"`
	DeviceFingerprint string `json:"device_fingerprint"`
	DevicePublicKey   []byte `json:"device_public_key"`
}

// ClientAccessTicketCreateParams 是 local owner 或 ManageClientAccess session 创建一次性 ticket 的输入。
// TicketTTLSeconds 与 GrantLifetimeSeconds 都由 daemon 重新校验；客户端不能通过 Cloud 或 bearer metadata 扩大 Scope。
type ClientAccessTicketCreateParams struct {
	Label                string            `json:"label,omitempty"`
	Scope                ClientAccessScope `json:"scope"`
	TicketTTLSeconds     int64             `json:"ticket_ttl_seconds"`
	GrantLifetimeSeconds int64             `json:"grant_lifetime_seconds"`
}

// ClientAccessTicketCreateResult 返回可编码进静态 QR 的 PairingBundle bytes 及脱敏索引。
// Bundle 含短期 ticket 但不含长期 grant；调用方不得写日志，TicketID/ExpiresAt 可用于用户提示。
type ClientAccessTicketCreateResult struct {
	Bundle    []byte    `json:"-"`
	TicketID  string    `json:"ticket_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ClientAccessRevokeParams 指定 owning daemon 要撤销的 client-bound GrantID。
// GrantID 是管理索引而非 bearer secret；撤销必须由 daemon AccessStore 持久化，删除客户端本地 ref 不能替代该操作。
type ClientAccessRevokeParams struct {
	GrantID string `json:"grant_id"`
}

// ClientAccessRecord 是认证后 access list/revoke 返回的脱敏授权投影。
// 它不包含 grant、ticket、client public key bytes 或任何 private material。
type ClientAccessRecord struct {
	GrantID               string            `json:"grant_id"`
	RevocationID          string            `json:"revocation_id"`
	SubjectKeyFingerprint string            `json:"subject_key_fingerprint"`
	ClientLabel           string            `json:"client_label,omitempty"`
	Scope                 ClientAccessScope `json:"scope"`
	IssuedAt              time.Time         `json:"issued_at"`
	ExpiresAt             time.Time         `json:"expires_at"`
	RevokedAt             time.Time         `json:"revoked_at,omitempty"`
}

// ClientAccessListResult 是 daemon-local client access truth 的稳定有序投影。
type ClientAccessListResult struct {
	Records []ClientAccessRecord `json:"records"`
}

func encodeClientAccessMethodParams(method string, params any) ([]byte, bool, error) {
	switch method {
	case "remote.access.identity", "remote.access.list":
		payload, err := proto.Marshal(&structpb.Struct{})
		return payload, true, err
	case "remote.access.ticket.create":
		value, ok := params.(ClientAccessTicketCreateParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientAccessTicketCreateParams); ptrOK && ptr != nil {
				value, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.ClientAccessTicketCreateParams", params)
		}
		message, err := structpb.NewStruct(map[string]any{
			"label":                  value.Label,
			"scope":                  clientAccessScopeMap(value.Scope),
			"ticket_ttl_seconds":     value.TicketTTLSeconds,
			"grant_lifetime_seconds": value.GrantLifetimeSeconds,
		})
		if err != nil {
			return nil, true, err
		}
		payload, err := proto.Marshal(message)
		return payload, true, err
	case "remote.access.revoke":
		value, ok := params.(ClientAccessRevokeParams)
		if !ok {
			if ptr, ptrOK := params.(*ClientAccessRevokeParams); ptrOK && ptr != nil {
				value, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodParamsTypeError(method, "protocol.ClientAccessRevokeParams", params)
		}
		message, err := structpb.NewStruct(map[string]any{"grant_id": value.GrantID})
		if err != nil {
			return nil, true, err
		}
		payload, err := proto.Marshal(message)
		return payload, true, err
	default:
		return nil, false, nil
	}
}

func decodeClientAccessMethodParams(method string, payload []byte) (any, bool, error) {
	switch method {
	case "remote.access.identity", "remote.access.list":
		message, err := decodeClientAccessStruct(payload, nil)
		if err != nil {
			return nil, true, err
		}
		if len(message.Fields) != 0 {
			return nil, true, fmt.Errorf("protocol: %s requires empty params", method)
		}
		return struct{}{}, true, nil
	case "remote.access.ticket.create":
		message, err := decodeClientAccessStruct(payload, []string{"label", "scope", "ticket_ttl_seconds", "grant_lifetime_seconds"})
		if err != nil {
			return nil, true, err
		}
		scopeValue, ok := message.Fields["scope"]
		if !ok || scopeValue.GetStructValue() == nil {
			return nil, true, fmt.Errorf("protocol: remote.access.ticket.create requires scope")
		}
		scope, err := clientAccessScopeFromStruct(scopeValue.GetStructValue())
		if err != nil {
			return nil, true, err
		}
		return ClientAccessTicketCreateParams{
			Label: stringField(message, "label"), Scope: scope,
			TicketTTLSeconds:     int64Field(message, "ticket_ttl_seconds"),
			GrantLifetimeSeconds: int64Field(message, "grant_lifetime_seconds"),
		}, true, nil
	case "remote.access.revoke":
		message, err := decodeClientAccessStruct(payload, []string{"grant_id"})
		if err != nil {
			return nil, true, err
		}
		return ClientAccessRevokeParams{GrantID: stringField(message, "grant_id")}, true, nil
	default:
		return nil, false, nil
	}
}

func encodeClientAccessMethodResult(method string, result any) ([]byte, bool, error) {
	var value map[string]any
	switch method {
	case "remote.access.identity":
		identity, ok := result.(ClientAccessIdentityResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientAccessIdentityResult); ptrOK && ptr != nil {
				identity, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.ClientAccessIdentityResult", result)
		}
		value = map[string]any{
			"device_id": identity.DeviceID, "device_fingerprint": identity.DeviceFingerprint,
			"device_public_key": base64.RawURLEncoding.EncodeToString(identity.DevicePublicKey),
		}
	case "remote.access.ticket.create":
		ticket, ok := result.(ClientAccessTicketCreateResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientAccessTicketCreateResult); ptrOK && ptr != nil {
				ticket, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.ClientAccessTicketCreateResult", result)
		}
		value = map[string]any{
			"bundle": base64.RawURLEncoding.EncodeToString(ticket.Bundle), "ticket_id": ticket.TicketID,
			"expires_at": ticket.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
	case "remote.access.list":
		list, ok := result.(ClientAccessListResult)
		if !ok {
			if ptr, ptrOK := result.(*ClientAccessListResult); ptrOK && ptr != nil {
				list, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.ClientAccessListResult", result)
		}
		records := make([]any, 0, len(list.Records))
		for _, record := range list.Records {
			records = append(records, clientAccessRecordMap(record))
		}
		value = map[string]any{"records": records}
	case "remote.access.revoke":
		record, ok := result.(ClientAccessRecord)
		if !ok {
			if ptr, ptrOK := result.(*ClientAccessRecord); ptrOK && ptr != nil {
				record, ok = *ptr, true
			}
		}
		if !ok {
			return nil, true, methodResultTypeError(method, "protocol.ClientAccessRecord", result)
		}
		value = clientAccessRecordMap(record)
	default:
		return nil, false, nil
	}
	message, err := structpb.NewStruct(value)
	if err != nil {
		return nil, true, err
	}
	payload, err := proto.Marshal(message)
	return payload, true, err
}

func decodeClientAccessMethodResult(method string, payload []byte, out any) (bool, error) {
	switch method {
	case "remote.access.identity":
		message, err := decodeClientAccessStruct(payload, []string{"device_id", "device_fingerprint", "device_public_key"})
		if err != nil {
			return true, err
		}
		ptr, ok := out.(*ClientAccessIdentityResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.ClientAccessIdentityResult", out)
		}
		publicKey, err := base64.RawURLEncoding.DecodeString(stringField(message, "device_public_key"))
		if err != nil {
			return true, fmt.Errorf("protocol: decode daemon public key: %w", err)
		}
		*ptr = ClientAccessIdentityResult{DeviceID: stringField(message, "device_id"), DeviceFingerprint: stringField(message, "device_fingerprint"), DevicePublicKey: publicKey}
		return true, nil
	case "remote.access.ticket.create":
		message, err := decodeClientAccessStruct(payload, []string{"bundle", "ticket_id", "expires_at"})
		if err != nil {
			return true, err
		}
		ptr, ok := out.(*ClientAccessTicketCreateResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.ClientAccessTicketCreateResult", out)
		}
		bundle, err := base64.RawURLEncoding.DecodeString(stringField(message, "bundle"))
		if err != nil {
			return true, fmt.Errorf("protocol: decode pairing bundle: %w", err)
		}
		expiresAt, err := time.Parse(time.RFC3339Nano, stringField(message, "expires_at"))
		if err != nil {
			return true, fmt.Errorf("protocol: decode pairing expiry: %w", err)
		}
		*ptr = ClientAccessTicketCreateResult{Bundle: bundle, TicketID: stringField(message, "ticket_id"), ExpiresAt: expiresAt.UTC()}
		return true, nil
	case "remote.access.list":
		message, err := decodeClientAccessStruct(payload, []string{"records"})
		if err != nil {
			return true, err
		}
		ptr, ok := out.(*ClientAccessListResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.ClientAccessListResult", out)
		}
		list := message.Fields["records"].GetListValue()
		if list == nil {
			return true, fmt.Errorf("protocol: client access records are invalid")
		}
		records := make([]ClientAccessRecord, 0, len(list.Values))
		for _, item := range list.Values {
			record, err := clientAccessRecordFromStruct(item.GetStructValue())
			if err != nil {
				return true, err
			}
			records = append(records, record)
		}
		ptr.Records = records
		return true, nil
	case "remote.access.revoke":
		message, err := decodeClientAccessStruct(payload, clientAccessRecordKeys())
		if err != nil {
			return true, err
		}
		ptr, ok := out.(*ClientAccessRecord)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*protocol.ClientAccessRecord", out)
		}
		record, err := clientAccessRecordFromStruct(message)
		if err != nil {
			return true, err
		}
		*ptr = record
		return true, nil
	default:
		return false, nil
	}
}

func clientAccessScopeMap(scope ClientAccessScope) map[string]any {
	return map[string]any{
		"allow_daemon": scope.AllowDaemon, "terminal_id": scope.TerminalID, "machine_events_only": scope.MachineEventsOnly,
		"file_read_metadata": scope.FileReadMetadata, "file_read_content": scope.FileReadContent,
		"file_write_content": scope.FileWriteContent, "file_mutate": scope.FileMutate,
		"manage_client_access": scope.ManageClientAccess,
	}
}

func clientAccessScopeFromStruct(message *structpb.Struct) (ClientAccessScope, error) {
	allowed := []string{"allow_daemon", "terminal_id", "machine_events_only", "file_read_metadata", "file_read_content", "file_write_content", "file_mutate", "manage_client_access"}
	if err := requireClientAccessKeys(message, allowed); err != nil {
		return ClientAccessScope{}, err
	}
	return ClientAccessScope{
		AllowDaemon: boolField(message, "allow_daemon"), TerminalID: stringField(message, "terminal_id"),
		MachineEventsOnly: boolField(message, "machine_events_only"), FileReadMetadata: boolField(message, "file_read_metadata"),
		FileReadContent: boolField(message, "file_read_content"), FileWriteContent: boolField(message, "file_write_content"),
		FileMutate: boolField(message, "file_mutate"), ManageClientAccess: boolField(message, "manage_client_access"),
	}, nil
}

func clientAccessRecordMap(record ClientAccessRecord) map[string]any {
	return map[string]any{
		"grant_id": record.GrantID, "revocation_id": record.RevocationID,
		"subject_key_fingerprint": record.SubjectKeyFingerprint, "client_label": record.ClientLabel,
		"scope": clientAccessScopeMap(record.Scope), "issued_at": record.IssuedAt.UTC().Format(time.RFC3339Nano),
		"expires_at": record.ExpiresAt.UTC().Format(time.RFC3339Nano), "revoked_at": formatOptionalTime(record.RevokedAt),
	}
}

func clientAccessRecordFromStruct(message *structpb.Struct) (ClientAccessRecord, error) {
	if message == nil {
		return ClientAccessRecord{}, fmt.Errorf("protocol: client access record is invalid")
	}
	if err := requireClientAccessKeys(message, clientAccessRecordKeys()); err != nil {
		return ClientAccessRecord{}, err
	}
	scopeValue := message.Fields["scope"].GetStructValue()
	scope, err := clientAccessScopeFromStruct(scopeValue)
	if err != nil {
		return ClientAccessRecord{}, err
	}
	issuedAt, err := time.Parse(time.RFC3339Nano, stringField(message, "issued_at"))
	if err != nil {
		return ClientAccessRecord{}, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, stringField(message, "expires_at"))
	if err != nil {
		return ClientAccessRecord{}, err
	}
	revokedAt, err := parseOptionalTime(stringField(message, "revoked_at"))
	if err != nil {
		return ClientAccessRecord{}, err
	}
	return ClientAccessRecord{
		GrantID: stringField(message, "grant_id"), RevocationID: stringField(message, "revocation_id"),
		SubjectKeyFingerprint: stringField(message, "subject_key_fingerprint"), ClientLabel: stringField(message, "client_label"),
		Scope: scope, IssuedAt: issuedAt.UTC(), ExpiresAt: expiresAt.UTC(), RevokedAt: revokedAt,
	}, nil
}

func clientAccessRecordKeys() []string {
	return []string{"grant_id", "revocation_id", "subject_key_fingerprint", "client_label", "scope", "issued_at", "expires_at", "revoked_at"}
}

func decodeClientAccessStruct(payload []byte, allowed []string) (*structpb.Struct, error) {
	message := &structpb.Struct{}
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message); err != nil {
		return nil, err
	}
	if len(message.ProtoReflect().GetUnknown()) != 0 {
		return nil, fmt.Errorf("protocol: client access payload contains unknown protobuf fields")
	}
	if allowed != nil {
		if err := requireClientAccessKeys(message, allowed); err != nil {
			return nil, err
		}
	}
	return message, nil
}

func requireClientAccessKeys(message *structpb.Struct, allowed []string) error {
	if message == nil {
		return fmt.Errorf("protocol: client access object is nil")
	}
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range message.Fields {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("protocol: client access object contains unknown field %q", key)
		}
	}
	if len(message.Fields) != len(set) {
		for key := range set {
			if _, ok := message.Fields[key]; !ok {
				return fmt.Errorf("protocol: client access object is missing field %q", key)
			}
		}
	}
	if err := validateClientAccessFieldTypes(message); err != nil {
		return err
	}
	return nil
}

func validateClientAccessFieldTypes(message *structpb.Struct) error {
	for key, value := range message.Fields {
		switch key {
		case "label", "grant_id", "revocation_id", "subject_key_fingerprint", "client_label", "terminal_id",
			"issued_at", "expires_at", "revoked_at", "device_id", "device_fingerprint", "device_public_key",
			"bundle", "ticket_id":
			if _, ok := value.Kind.(*structpb.Value_StringValue); !ok {
				return fmt.Errorf("protocol: client access field %q must be a string", key)
			}
		case "allow_daemon", "machine_events_only", "file_read_metadata", "file_read_content", "file_write_content",
			"file_mutate", "manage_client_access":
			if _, ok := value.Kind.(*structpb.Value_BoolValue); !ok {
				return fmt.Errorf("protocol: client access field %q must be a boolean", key)
			}
		case "ticket_ttl_seconds", "grant_lifetime_seconds":
			number, ok := value.Kind.(*structpb.Value_NumberValue)
			if !ok || math.Trunc(number.NumberValue) != number.NumberValue || number.NumberValue < math.MinInt64 || number.NumberValue > math.MaxInt64 {
				return fmt.Errorf("protocol: client access field %q must be an int64", key)
			}
		case "scope":
			if value.GetStructValue() == nil {
				return fmt.Errorf("protocol: client access field %q must be an object", key)
			}
		case "records":
			if value.GetListValue() == nil {
				return fmt.Errorf("protocol: client access field %q must be a list", key)
			}
		}
	}
	return nil
}

func stringField(message *structpb.Struct, key string) string {
	if value, ok := message.Fields[key]; ok {
		return strings.TrimSpace(value.GetStringValue())
	}
	return ""
}

func boolField(message *structpb.Struct, key string) bool {
	if value, ok := message.Fields[key]; ok {
		return value.GetBoolValue()
	}
	return false
}

func int64Field(message *structpb.Struct, key string) int64 {
	if value, ok := message.Fields[key]; ok {
		return int64(value.GetNumberValue())
	}
	return 0
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}
