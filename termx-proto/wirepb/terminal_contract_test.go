package wirepb

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCoreV2RemoteWireContractFields(t *testing.T) {
	assertFields(t, (&CreateParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"command": 1,
		"id":      2,
		"name":    3,
		"tags":    4,
		"size":    5,
		"dir":     6,
		"env":     7,
	})

	assertFields(t, (&TerminalInfo{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"id":                            1,
		"name":                          2,
		"command":                       3,
		"tags":                          4,
		"size":                          5,
		"state":                         6,
		"cwd":                           7,
		"live_cwd":                      8,
		"created_at_unix_nano":          9,
		"exit_code":                     10,
		"resize_ownership":              11,
		"resize_owner_attachment_count": 12,
		"exited_at_unix_nano":           13,
	})

	assertFields(t, (&RemoteStatus{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"state":                1,
		"detail":               2,
		"device_id":            3,
		"device_name":          4,
		"control_url":          5,
		"hub_url":              6,
		"hub_urls":             7,
		"data_dir":             8,
		"mode":                 9,
		"allow_lan":            10,
		"terminal_count":       11,
		"updated_at_unix_nano": 12,
	})

	assertFields(t, (&RemotePairStartParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"local_pair_url":   1,
		"ttl_seconds":      2,
		"auth_ttl_seconds": 3,
	})

	assertFields(t, (&RemotePairStartResult{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"type":                 1,
		"machine_id":           2,
		"machine_name":         3,
		"local_pair_url":       4,
		"pair_session_id":      5,
		"pair_secret":          6,
		"answer_proof_secret":  7,
		"expires_at_unix_nano": 8,
	})

	assertFields(t, (&RemoteLocalEnableParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"local_web_addr": 1,
		"ice_tcp_addr":   2,
		"hub_urls":       3,
		"control_url":    4,
		"access_token":   5,
		"region":         6,
	})

	assertFields(t, (&RemoteLocalStatus{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"enabled":              1,
		"http_url":             2,
		"local_web_addr":       3,
		"local_pair_url":       4,
		"ice_tcp_enabled":      5,
		"ice_tcp_addr":         6,
		"ice_tcp_port":         7,
		"updated_at_unix_nano": 8,
	})

	assertFields(t, (&StorageEntry{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"app_id":               1,
		"scope":                2,
		"owner_id":             3,
		"key":                  4,
		"value":                5,
		"version":              6,
		"updated_at_unix_nano": 7,
	})
}

func assertFields(t *testing.T, message protoreflect.MessageDescriptor, fields map[protoreflect.Name]protoreflect.FieldNumber) {
	t.Helper()
	for name, number := range fields {
		field := message.Fields().ByName(name)
		if field == nil {
			t.Fatalf("%s missing field %s", message.FullName(), name)
		}
		if field.Number() != number {
			t.Fatalf("%s.%s field number = %d, want %d", message.FullName(), name, field.Number(), number)
		}
	}
}
