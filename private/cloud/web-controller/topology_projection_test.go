package webcontroller

import (
	"context"
	"errors"
	"testing"

	"github.com/lozzow/termx/proto/cloudpb"
)

type fakeTopologyProjection struct {
	accountID  string
	projection *cloudpb.PresenceProjection
	err        error
}

func (projection fakeTopologyProjection) Presence(context.Context, string) (string, *cloudpb.PresenceProjection, error) {
	return projection.accountID, projection.projection, projection.err
}

func TestBrowserProjectsOnlineOnlyFromSameAccountFreshTopology(t *testing.T) {
	nodes := []ManagedNode{{ID: "daemon-1", Kind: "daemon", Online: true}}
	projectNodePresence(context.Background(), fakeTopologyProjection{accountID: "account-1", projection: &cloudpb.PresenceProjection{Availability: cloudpb.Availability_AVAILABILITY_ONLINE, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}}, "account-1", nodes)
	if !nodes[0].Online {
		t.Fatal("fresh same-account topology was not projected online")
	}
	for _, fixture := range []fakeTopologyProjection{
		{accountID: "other", projection: &cloudpb.PresenceProjection{Availability: cloudpb.Availability_AVAILABILITY_ONLINE, Freshness: cloudpb.Freshness_FRESHNESS_FRESH}},
		{accountID: "account-1", projection: &cloudpb.PresenceProjection{Availability: cloudpb.Availability_AVAILABILITY_UNKNOWN, Freshness: cloudpb.Freshness_FRESHNESS_STALE}},
		{accountID: "account-1", err: errors.New("missing")},
	} {
		nodes[0].Online = true
		projectNodePresence(context.Background(), fixture, "account-1", nodes)
		if nodes[0].Online {
			t.Fatalf("non-current topology projected online: %#v", fixture)
		}
	}
}
