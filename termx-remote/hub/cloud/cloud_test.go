package cloud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestCloudSignalingOfferAndAnswerFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 44, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:    "mach_1",
		TerminalID:   "term_1",
		SessionToken: "session-token-1",
		SDP:          minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if offer.Path != cloud.PathHub || offer.RelayInUse || offer.AllowRelay {
		t.Fatalf("offer path/relay = %+v", offer)
	}
	if offer.MachineID != "mach_1" || offer.TerminalID != "term_1" || offer.SessionToken != "session-token-1" {
		t.Fatalf("offer identity = %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll agent offer: %v", err)
	}
	if polled.ID != offer.ID || polled.SDP != minimalSDP("offer") || polled.AllowRelay {
		t.Fatalf("polled offer = %+v", polled)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if answer.SDP != minimalSDP("answer") || answer.RelayInUse {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestCloudLocalSessionPathDisablesRelayAndPropagatesToAgentOffer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 16, 10, 15, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{
		Registry:                    reg,
		Clock:                       clock,
		AllowRelayByDefault:         true,
		AllowRelayTransferByDefault: true,
	})
	preflight, err := svc.PreflightSession(ctx, cloud.PreflightSessionInput{
		MachineID:    "mach_1",
		TerminalID:   "term_1",
		Path:         cloud.PathLocal,
		SessionToken: "session-token-1",
	})
	if err != nil {
		t.Fatalf("preflight local: %v", err)
	}
	if preflight.Path != cloud.PathLocal || preflight.AllowRelay || preflight.AllowRelayTransfer {
		t.Fatalf("local preflight policy = %+v", preflight)
	}
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:     "mach_1",
		TerminalID:    "term_1",
		Path:          cloud.PathLocal,
		SessionToken:  "session-token-1",
		SDP:           minimalSDP("offer"),
		ICECandidates: []string{"candidate:local 1 udp 1 127.0.0.1 1 typ host"},
	})
	if err != nil {
		t.Fatalf("submit local offer: %v", err)
	}
	if offer.Path != cloud.PathLocal || offer.AllowRelay || offer.AllowRelayTransfer {
		t.Fatalf("local offer policy = %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll local offer: %v", err)
	}
	if polled.Path != cloud.PathLocal || polled.AllowRelay {
		t.Fatalf("polled local offer = %+v", polled)
	}
	policy, ok := svc.OfferPolicy(offer.ID)
	if !ok || policy.Path != cloud.PathLocal || policy.AllowRelay || policy.AllowRelayTransfer {
		t.Fatalf("local offer policy lookup = %+v ok=%v", policy, ok)
	}
}

func TestCloudServiceDefaultRelayAllowancePropagatesToOfferPolicy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 5, 14, 20, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock, AllowRelayByDefault: true})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if !offer.AllowRelay {
		t.Fatalf("submitted offer should allow relay when cloud config enables it: %+v", offer)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll agent offer: %v", err)
	}
	if !polled.AllowRelay {
		t.Fatalf("polled offer should preserve relay allowance: %+v", polled)
	}
	policy, ok := svc.OfferPolicy(offer.ID)
	if !ok || !policy.AllowRelay {
		t.Fatalf("offer policy should preserve relay allowance: %+v ok=%v", policy, ok)
	}
}

func TestCloudSubmitOfferRequiresOnlineMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 49, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-no-agent"),
	}); !errors.Is(err, registry.ErrAgentNotFound) {
		t.Fatalf("offline offer err = %v", err)
	}
}

func TestCloudAnswerRequiresMachineCorrelation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 54, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{AgentID: "agent_1", MachineID: "mach_1", Timeout: time.Second}); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   offer.ID,
		SDP:       minimalSDP("answer"),
	}); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		MachineID: "wrong_machine",
	}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("wrong machine answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{OfferID: offer.ID}); !errors.Is(err, cloud.ErrWrongMachine) {
		t.Fatalf("missing machine answer err = %v", err)
	}
	if _, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   offer.ID,
		MachineID: "mach_1",
	}); err != nil {
		t.Fatalf("authorized answer: %v", err)
	}
}

func TestCloudAnswerLookupUsesPublicSessionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 10, 43, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_public",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-a"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	polled, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	})
	if err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if polled.ID != offer.ID {
		t.Fatalf("polled offer = %s, want %s", polled.ID, offer.ID)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   "rtc_public",
		SDP:       minimalSDP("answer-a"),
	}); err != nil {
		t.Fatalf("submit answer by public session id: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   "rtc_public",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer by public session id: %v", err)
	}
	if answer.OfferID != offer.ID || answer.SDP != minimalSDP("answer-a") {
		t.Fatalf("answer = %+v, want offer %s", answer, offer.ID)
	}
}

func TestCloudOfferStateLivesInRegistry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 18, 58, 0, 0, time.UTC)}
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{
		Registry:            reg,
		Clock:               clock,
		OfferTTL:            time.Second,
		MaxOffers:           1,
		AllowRelayByDefault: true,
	})
	first, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_a",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-a"),
	})
	if err != nil {
		t.Fatalf("submit first: %v", err)
	}
	stored, ok := reg.Offer(ctx, registry.OfferLookupInput{MachineID: "mach_1", OfferID: "rtc_a"})
	if !ok || stored.ID != first.ID || !stored.AllowRelay {
		t.Fatalf("registry offer lookup by public session = %+v ok=%v", stored, ok)
	}
	policy, ok := svc.OfferPolicyForAnswer(cloud.GetAnswerInput{OfferID: "rtc_a", MachineID: "mach_1"})
	if !ok || !policy.AllowRelay || policy.TerminalID != "term_1" {
		t.Fatalf("policy from registry offer = %+v ok=%v", policy, ok)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_b",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-b"),
	}); err != nil {
		t.Fatalf("submit second: %v", err)
	}
	if _, ok := reg.Offer(ctx, registry.OfferLookupInput{MachineID: "mach_1", OfferID: "rtc_a"}); ok {
		t.Fatal("expected oldest offer to be evicted from registry")
	}
	clock.value = clock.value.Add(2 * time.Second)
	removed := svc.CleanupExpired(ctx)
	if removed == 0 {
		t.Fatal("expected expired registry offer entries to be cleaned")
	}
}

func TestCloudPublicSessionAnswerLookupUsesRegistry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &mutableClock{value: time.Date(2026, 5, 3, 19, 2, 0, 0, time.UTC)}
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	offer, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		SessionID:  "rtc_registry_public",
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-registry-public"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	if _, err := svc.PollAgentOffer(ctx, cloud.PollAgentOfferInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		Timeout:   time.Second,
	}); err != nil {
		t.Fatalf("poll offer: %v", err)
	}
	if err := svc.SubmitAnswer(ctx, cloud.SubmitAnswerInput{
		AgentID:   "agent_1",
		MachineID: "mach_1",
		OfferID:   "rtc_registry_public",
		SDP:       minimalSDP("answer-registry-public"),
	}); err != nil {
		t.Fatalf("submit answer by public session: %v", err)
	}
	answer, err := svc.GetAnswer(ctx, cloud.GetAnswerInput{
		OfferID:   "rtc_registry_public",
		MachineID: "mach_1",
	})
	if err != nil {
		t.Fatalf("get answer by public session: %v", err)
	}
	if answer.OfferID != offer.ID || answer.SDP != minimalSDP("answer-registry-public") {
		t.Fatalf("answer = %+v, want offer %s", answer, offer.ID)
	}
}

func TestCloudOfferPolicyForAnswerWithoutRegistryReturnsFalse(t *testing.T) {
	t.Parallel()

	svc := cloud.NewService(cloud.Config{})
	if policy, ok := svc.OfferPolicyForAnswer(cloud.GetAnswerInput{OfferID: "offer_1", MachineID: "mach_1"}); ok {
		t.Fatalf("policy = %+v ok=true, want false", policy)
	}
}

func TestCloudSignalingRejectsRuntimePayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 45, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        "v=0\r\nterminal_data=must-not-forward",
	}); !errors.Is(err, registry.ErrRuntimePayload) {
		t.Fatalf("runtime payload err = %v", err)
	}
	if _, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-after-runtime-reject"),
	}); err != nil {
		t.Fatalf("valid offer after runtime reject should succeed: %v", err)
	}
}

func TestCloudSignalingAllowsConcurrentOffers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 5, 51, 0, 0, time.UTC))
	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(ctx, registry.RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	svc := cloud.NewService(cloud.Config{Registry: reg, Clock: clock})
	first, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-1"),
	})
	if err != nil {
		t.Fatalf("first offer: %v", err)
	}
	second, err := svc.SubmitOffer(ctx, cloud.SubmitOfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalSDP("offer-2"),
	})
	if err != nil {
		t.Fatalf("second offer: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("offers reused id %q", first.ID)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}

type mutableClock struct {
	value time.Time
}

func (c *mutableClock) Now() time.Time {
	return c.value
}

func minimalSDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}
