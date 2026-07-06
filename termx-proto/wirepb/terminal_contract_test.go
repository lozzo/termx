package wirepb

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCoreV2RemoteWireContractFields(t *testing.T) {
	assertFields(t, (&CreateParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"command":                    1,
		"id":                         2,
		"name":                       3,
		"tags":                       4,
		"size":                       5,
		"dir":                        6,
		"env":                        7,
		"scrollback_size":            8,
		"scrollback_max_bytes":       9,
		"scrollback_max_age_seconds": 10,
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

	assertFields(t, (&HistoryWindowParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"terminal_id":            1,
		"before_offset":          2,
		"limit":                  3,
		"cols":                   4,
		"token":                  5,
		"history_generation":     6,
		"cursor_valid":           7,
		"before_line_id":         8,
		"before_row_in_line":     9,
		"boundary_first_line_id": 10,
		"boundary_last_line_id":  11,
		"mode":                   12,
		"after_cursor_valid":     13,
		"after_line_id":          14,
		"after_row_in_line":      15,
		"range_valid":            16,
		"range_start_line_id":    17,
		"range_start_col":        18,
		"range_end_line_id":      19,
		"range_end_col":          20,
	})

	assertFields(t, (&HistoryWindow{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"terminal_id":                    1,
		"token":                          2,
		"op":                             3,
		"size":                           4,
		"rows":                           5,
		"line_start_rows":                6,
		"line_end_rows":                  7,
		"line_row_kinds":                 8,
		"before_offset":                  9,
		"loaded_rows":                    10,
		"total_rows":                     11,
		"logical_total":                  12,
		"has_more":                       13,
		"history_generation":             14,
		"first_row_id":                   15,
		"last_row_id":                    16,
		"timestamp_unix_nano":            17,
		"line_clipped_before":            18,
		"line_clipped_after":             19,
		"line_logical_line_ids":          20,
		"loaded_lines":                   21,
		"first_line_id":                  22,
		"last_line_id":                   23,
		"line_timestamp_start_unix_nano": 24,
		"line_timestamp_end_unix_nano":   25,
		"cursor_valid":                   26,
		"cursor_before_line_id":          27,
		"cursor_before_row_in_line":      28,
		"row_logical_line_ids":           29,
		"row_in_line":                    30,
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

	assertFields(t, (&PathListDirsParams{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"prefix": 1,
		"limit":  2,
	})
	assertFields(t, (&PathDirEntry{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"name": 1,
		"path": 2,
	})
	assertFields(t, (&PathListDirsResult{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"base_path": 1,
		"entries":   2,
		"missing":   3,
		"truncated": 4,
	})
	assertFields(t, (&PathDefaultsResult{}).ProtoReflect().Descriptor(), map[protoreflect.Name]protoreflect.FieldNumber{
		"default_command": 1,
		"default_cwd":     2,
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
