package protocol

import (
	"testing"
	"time"

	"github.com/lozzow/termx/proto/remoteauthpb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
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

func TestClientAccessMethodCodecRejectsUnknownFields(t *testing.T) {
	payload, err := proto.Marshal(&remoteauthpb.ClientAccessTicketCreateRequest{
		Label: "Phone", Scope: &remoteauthpb.ClientAccessScope{AllowDaemon: true},
		TicketTtlSeconds: 600, GrantLifetimeSeconds: 86400,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload = protowire.AppendTag(payload, 99, protowire.VarintType)
	payload = protowire.AppendVarint(payload, 1)
	if _, err := DecodeMethodParams("remote.access.ticket.create", payload); err == nil {
		t.Fatal("client access params with unknown protobuf fields were accepted")
	}
}
