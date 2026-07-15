package protocol

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestClientAccessMethodCodecsRoundTripStrictContract(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	scope := ClientAccessScope{AllowDaemon: true, FileReadMetadata: true, ManageClientAccess: true}
	params := ClientAccessTicketCreateParams{Label: "Phone", Scope: scope, TicketTTLSeconds: 600, GrantLifetimeSeconds: 86400}
	payload, err := EncodeMethodParams("remote.access.ticket.create", params)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMethodParams("remote.access.ticket.create", payload)
	if err != nil || decoded != params {
		t.Fatalf("params round trip = %#v err=%v", decoded, err)
	}
	record := ClientAccessRecord{
		GrantID: "grant-1", RevocationID: "grant-1", SubjectKeyFingerprint: "ed25519-sha256:client",
		ClientLabel: "Phone", Scope: scope, IssuedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	resultPayload, err := EncodeMethodResult("remote.access.list", ClientAccessListResult{Records: []ClientAccessRecord{record}})
	if err != nil {
		t.Fatal(err)
	}
	var result ClientAccessListResult
	if err := DecodeMethodResult("remote.access.list", resultPayload, &result); err != nil || len(result.Records) != 1 || result.Records[0] != record {
		t.Fatalf("result round trip = %#v err=%v", result, err)
	}
}

func TestClientAccessMethodCodecRejectsUnknownMissingAndCoercedFields(t *testing.T) {
	tests := []map[string]any{
		{
			"label": "Phone", "scope": clientAccessScopeMap(ClientAccessScope{AllowDaemon: true}),
			"ticket_ttl_seconds": "600", "grant_lifetime_seconds": 86400,
		},
		{
			"label": "Phone", "scope": clientAccessScopeMap(ClientAccessScope{AllowDaemon: true}),
			"ticket_ttl_seconds": 600,
		},
		{
			"label": "Phone", "scope": clientAccessScopeMap(ClientAccessScope{AllowDaemon: true}),
			"ticket_ttl_seconds": 600, "grant_lifetime_seconds": 86400, "cloud_token": "forbidden",
		},
	}
	for _, input := range tests {
		message, err := structpb.NewStruct(input)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := proto.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeMethodParams("remote.access.ticket.create", payload); err == nil {
			t.Fatalf("unsafe client access params were accepted: %#v", input)
		}
	}
}
