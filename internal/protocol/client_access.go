package protocol

import (
	"fmt"
	"time"

	"github.com/lozzow/termx/proto/remoteauthpb"
	"github.com/lozzow/termx/shared/remoteauth"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ClientAccessScope 是 remoteauth.Scope 的 protocol API 别名。
// scope 真值只由 remoteauth 持有，protocol 不再维护第二套字段模型。
type ClientAccessScope = remoteauth.Scope

// ClientAccessIdentityResult 是 remoteauth 公开身份投影的 protocol API 别名。
type ClientAccessIdentityResult = remoteauth.ClientAccessIdentityResult

// ClientAccessTicketCreateParams 是 remoteauth ticket 创建输入的 protocol API 别名。
type ClientAccessTicketCreateParams = remoteauth.ClientAccessTicketCreateParams

// ClientAccessTicketCreateResult 是 remoteauth ticket 创建结果的 protocol API 别名。
type ClientAccessTicketCreateResult = remoteauth.ClientAccessTicketCreateResult

// ClientAccessRevokeParams 是 remoteauth 撤销输入的 protocol API 别名。
type ClientAccessRevokeParams = remoteauth.ClientAccessRevokeParams

// ClientAccessRecord 是 remoteauth 脱敏授权记录的 protocol API 别名。
type ClientAccessRecord = remoteauth.ClientAccessRecord

// ClientAccessListResult 是 remoteauth 授权列表结果的 protocol API 别名。
type ClientAccessListResult = remoteauth.ClientAccessListResult

func encodeClientAccessMethodParams(method string, params any) ([]byte, bool, error) {
	var message proto.Message
	switch method {
	case "remote.access.identity", "remote.access.list":
		message = &emptypb.Empty{}
	case "remote.access.ticket.create":
		value, ok := clientAccessTicketCreateParams(params)
		if !ok {
			return nil, true, methodParamsTypeError(method, "remoteauth.ClientAccessTicketCreateParams", params)
		}
		message = &remoteauthpb.ClientAccessTicketCreateRequest{
			Label: value.Label, Scope: clientAccessScopeToProto(value.Scope),
			TicketTtlSeconds: value.TicketTTLSeconds, GrantLifetimeSeconds: value.GrantLifetimeSeconds,
		}
	case "remote.access.revoke":
		value, ok := clientAccessRevokeParams(params)
		if !ok {
			return nil, true, methodParamsTypeError(method, "remoteauth.ClientAccessRevokeParams", params)
		}
		message = &remoteauthpb.ClientAccessRevokeRequest{GrantId: value.GrantID}
	default:
		return nil, false, nil
	}
	payload, err := proto.Marshal(message)
	return payload, true, err
}

func decodeClientAccessMethodParams(method string, payload []byte) (any, bool, error) {
	switch method {
	case "remote.access.identity", "remote.access.list":
		message := &emptypb.Empty{}
		return nil, true, unmarshalClientAccess(payload, message)
	case "remote.access.ticket.create":
		message := &remoteauthpb.ClientAccessTicketCreateRequest{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return nil, true, err
		}
		return remoteauth.ClientAccessTicketCreateParams{
			Label: message.GetLabel(), Scope: clientAccessScopeFromProto(message.GetScope()),
			TicketTTLSeconds: message.GetTicketTtlSeconds(), GrantLifetimeSeconds: message.GetGrantLifetimeSeconds(),
		}, true, nil
	case "remote.access.revoke":
		message := &remoteauthpb.ClientAccessRevokeRequest{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return nil, true, err
		}
		return remoteauth.ClientAccessRevokeParams{GrantID: message.GetGrantId()}, true, nil
	default:
		return nil, false, nil
	}
}

func encodeClientAccessMethodResult(method string, result any) ([]byte, bool, error) {
	var message proto.Message
	switch method {
	case "remote.access.identity":
		value, ok := clientAccessIdentityResult(result)
		if !ok {
			return nil, true, methodResultTypeError(method, "remoteauth.ClientAccessIdentityResult", result)
		}
		message = &remoteauthpb.ClientAccessIdentityResult{
			DeviceId: value.DeviceID, DeviceFingerprint: value.DeviceFingerprint,
			DevicePublicKey: append([]byte(nil), value.DevicePublicKey...),
		}
	case "remote.access.ticket.create":
		value, ok := clientAccessTicketCreateResult(result)
		if !ok {
			return nil, true, methodResultTypeError(method, "remoteauth.ClientAccessTicketCreateResult", result)
		}
		message = &remoteauthpb.ClientAccessTicketCreateResult{
			Bundle: append([]byte(nil), value.Bundle...), TicketId: value.TicketID,
			ExpiresAtUnixNano: optionalUnixNano(value.ExpiresAt),
		}
	case "remote.access.list":
		value, ok := clientAccessListResult(result)
		if !ok {
			return nil, true, methodResultTypeError(method, "remoteauth.ClientAccessListResult", result)
		}
		list := &remoteauthpb.ClientAccessListResult{Records: make([]*remoteauthpb.ClientAccessRecord, 0, len(value.Records))}
		for _, record := range value.Records {
			list.Records = append(list.Records, clientAccessRecordToProto(record))
		}
		message = list
	case "remote.access.revoke":
		value, ok := clientAccessRecord(result)
		if !ok {
			return nil, true, methodResultTypeError(method, "remoteauth.ClientAccessRecord", result)
		}
		message = clientAccessRecordToProto(value)
	default:
		return nil, false, nil
	}
	payload, err := proto.Marshal(message)
	return payload, true, err
}

func decodeClientAccessMethodResult(method string, payload []byte, out any) (bool, error) {
	switch method {
	case "remote.access.identity":
		ptr, ok := out.(*remoteauth.ClientAccessIdentityResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*remoteauth.ClientAccessIdentityResult", out)
		}
		message := &remoteauthpb.ClientAccessIdentityResult{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return true, err
		}
		*ptr = remoteauth.ClientAccessIdentityResult{
			DeviceID: message.GetDeviceId(), DeviceFingerprint: message.GetDeviceFingerprint(),
			DevicePublicKey: append([]byte(nil), message.GetDevicePublicKey()...),
		}
	case "remote.access.ticket.create":
		ptr, ok := out.(*remoteauth.ClientAccessTicketCreateResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*remoteauth.ClientAccessTicketCreateResult", out)
		}
		message := &remoteauthpb.ClientAccessTicketCreateResult{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return true, err
		}
		*ptr = remoteauth.ClientAccessTicketCreateResult{
			Bundle: append([]byte(nil), message.GetBundle()...), TicketID: message.GetTicketId(),
			ExpiresAt: timeFromUnixNano(message.GetExpiresAtUnixNano()),
		}
	case "remote.access.list":
		ptr, ok := out.(*remoteauth.ClientAccessListResult)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*remoteauth.ClientAccessListResult", out)
		}
		message := &remoteauthpb.ClientAccessListResult{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return true, err
		}
		ptr.Records = make([]remoteauth.ClientAccessRecord, 0, len(message.GetRecords()))
		for _, record := range message.GetRecords() {
			ptr.Records = append(ptr.Records, clientAccessRecordFromProto(record))
		}
	case "remote.access.revoke":
		ptr, ok := out.(*remoteauth.ClientAccessRecord)
		if !ok || ptr == nil {
			return true, methodOutTypeError(method, "*remoteauth.ClientAccessRecord", out)
		}
		message := &remoteauthpb.ClientAccessRecord{}
		if err := unmarshalClientAccess(payload, message); err != nil {
			return true, err
		}
		*ptr = clientAccessRecordFromProto(message)
	default:
		return false, nil
	}
	return true, nil
}

func clientAccessScopeToProto(scope remoteauth.Scope) *remoteauthpb.ClientAccessScope {
	return &remoteauthpb.ClientAccessScope{
		AllowDaemon: scope.AllowDaemon, TerminalId: scope.TerminalID, MachineEventsOnly: scope.MachineEventsOnly,
		FileReadMetadata: scope.FileReadMetadata, FileReadContent: scope.FileReadContent,
		FileWriteContent: scope.FileWriteContent, FileMutate: scope.FileMutate, ManageClientAccess: scope.ManageClientAccess,
	}
}

func clientAccessScopeFromProto(scope *remoteauthpb.ClientAccessScope) remoteauth.Scope {
	if scope == nil {
		return remoteauth.Scope{}
	}
	return remoteauth.Scope{
		AllowDaemon: scope.GetAllowDaemon(), TerminalID: scope.GetTerminalId(), MachineEventsOnly: scope.GetMachineEventsOnly(),
		FileReadMetadata: scope.GetFileReadMetadata(), FileReadContent: scope.GetFileReadContent(),
		FileWriteContent: scope.GetFileWriteContent(), FileMutate: scope.GetFileMutate(), ManageClientAccess: scope.GetManageClientAccess(),
	}
}

func clientAccessRecordToProto(record remoteauth.ClientAccessRecord) *remoteauthpb.ClientAccessRecord {
	return &remoteauthpb.ClientAccessRecord{
		GrantId: record.GrantID, RevocationId: record.RevocationID, SubjectKeyFingerprint: record.SubjectKeyFingerprint,
		ClientLabel: record.ClientLabel, Scope: clientAccessScopeToProto(record.Scope),
		IssuedAtUnixNano: optionalUnixNano(record.IssuedAt), ExpiresAtUnixNano: optionalUnixNano(record.ExpiresAt),
		RevokedAtUnixNano: optionalUnixNano(record.RevokedAt),
	}
}

func clientAccessRecordFromProto(record *remoteauthpb.ClientAccessRecord) remoteauth.ClientAccessRecord {
	if record == nil {
		return remoteauth.ClientAccessRecord{}
	}
	return remoteauth.ClientAccessRecord{
		GrantID: record.GetGrantId(), RevocationID: record.GetRevocationId(), SubjectKeyFingerprint: record.GetSubjectKeyFingerprint(),
		ClientLabel: record.GetClientLabel(), Scope: clientAccessScopeFromProto(record.GetScope()),
		IssuedAt: timeFromUnixNano(record.GetIssuedAtUnixNano()), ExpiresAt: timeFromUnixNano(record.GetExpiresAtUnixNano()),
		RevokedAt: timeFromUnixNano(record.GetRevokedAtUnixNano()),
	}
}

func unmarshalClientAccess(payload []byte, message proto.Message) error {
	if err := (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(payload, message); err != nil {
		return err
	}
	if len(message.ProtoReflect().GetUnknown()) != 0 {
		return fmt.Errorf("protocol: client access payload contains unknown protobuf fields")
	}
	return nil
}

func optionalUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}

func clientAccessTicketCreateParams(value any) (remoteauth.ClientAccessTicketCreateParams, bool) {
	result, ok := value.(remoteauth.ClientAccessTicketCreateParams)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessTicketCreateParams); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}

func clientAccessRevokeParams(value any) (remoteauth.ClientAccessRevokeParams, bool) {
	result, ok := value.(remoteauth.ClientAccessRevokeParams)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessRevokeParams); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}

func clientAccessIdentityResult(value any) (remoteauth.ClientAccessIdentityResult, bool) {
	result, ok := value.(remoteauth.ClientAccessIdentityResult)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessIdentityResult); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}

func clientAccessTicketCreateResult(value any) (remoteauth.ClientAccessTicketCreateResult, bool) {
	result, ok := value.(remoteauth.ClientAccessTicketCreateResult)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessTicketCreateResult); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}

func clientAccessListResult(value any) (remoteauth.ClientAccessListResult, bool) {
	result, ok := value.(remoteauth.ClientAccessListResult)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessListResult); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}

func clientAccessRecord(value any) (remoteauth.ClientAccessRecord, bool) {
	result, ok := value.(remoteauth.ClientAccessRecord)
	if !ok {
		if ptr, ptrOK := value.(*remoteauth.ClientAccessRecord); ptrOK && ptr != nil {
			return *ptr, true
		}
	}
	return result, ok
}
