package registry

import (
	"context"
	"testing"
	"time"
)

func TestPollWaiterReceivesSubmittedOffer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := New(Config{
		Clock:    fixedRegistryClock(time.Date(2026, 5, 3, 10, 3, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
	})
	if _, err := store.Register(ctx, RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	resultCh := make(chan offerPollResult, 1)
	go func() {
		offer, err := store.Poll(ctx, PollInput{
			AgentID:   "agent_1",
			MachineID: "mach_1",
			Timeout:   time.Second,
		})
		resultCh <- offerPollResult{Offer: offer, Err: err}
	}()
	waitForPollWaiter(t, store, "mach_1")

	submitted, err := store.SubmitOffer(ctx, OfferInput{
		MachineID:  "mach_1",
		TerminalID: "term_1",
		SDP:        minimalRegistrySDP("offer"),
	})
	if err != nil {
		t.Fatalf("submit offer: %v", err)
	}
	select {
	case result := <-resultCh:
		if result.Err != nil {
			t.Fatalf("poll waiter err = %v", result.Err)
		}
		if result.Offer.ID != submitted.ID {
			t.Fatalf("poll waiter offer = %q, want %q", result.Offer.ID, submitted.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("poll waiter was not notified within 100ms")
	}
}

func TestOfferWaiterCancelDoesNotRemovePairingWaiter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := New(Config{
		Clock:    fixedRegistryClock(time.Date(2026, 5, 3, 10, 16, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
	})
	if _, err := store.Register(ctx, RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	offerCtx, cancelOffer := context.WithCancel(ctx)
	offerCh := make(chan offerPollResult, 1)
	go func() {
		offer, err := store.Poll(offerCtx, PollInput{
			AgentID:   "agent_1",
			MachineID: "mach_1",
			Timeout:   time.Second,
		})
		offerCh <- offerPollResult{Offer: offer, Err: err}
	}()
	waitForOfferPollWaiter(t, store, "mach_1")

	pairingCh := make(chan pairingPollResult, 1)
	go func() {
		claim, err := store.PollPairingClaim(ctx, PairingPollInput{
			AgentID:   "agent_1",
			MachineID: "mach_1",
			Timeout:   time.Second,
		})
		pairingCh <- pairingPollResult{Claim: claim, Err: err}
	}()
	waitForPairingPollWaiter(t, store, "mach_1")

	cancelOffer()
	select {
	case result := <-offerCh:
		if result.Err == nil {
			t.Fatalf("offer poll err = nil, offer = %+v", result.Offer)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("offer poll did not return after cancellation")
	}
	if store.pairingQueue.waiterCount("mach_1") == 0 {
		t.Fatal("pairing waiter was removed when offer waiter was canceled")
	}

	submitted, err := store.SubmitPairingClaim(ctx, PairingClaimInput{
		MachineID:             "mach_1",
		PairSessionID:         "pair-session-1",
		PairSecret:            "pair-secret-1",
		AppDeviceID:           "app-device-1",
		AppName:               "TermX App",
		RequestedCapabilities: []string{"terminal"},
	})
	if err != nil {
		t.Fatalf("submit pairing claim: %v", err)
	}
	select {
	case result := <-pairingCh:
		if result.Err != nil {
			t.Fatalf("pairing poll err = %v", result.Err)
		}
		if result.Claim.ID != submitted.ID {
			t.Fatalf("pairing claim = %q, want %q", result.Claim.ID, submitted.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("pairing waiter was not notified after offer waiter cancellation")
	}
}

func TestOfferLookupPrefersMachineScopedPublicSessionID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := New(Config{
		Clock:    fixedRegistryClock(time.Date(2026, 5, 3, 9, 46, 0, 0, time.UTC)),
		AgentTTL: time.Minute,
	})
	if _, err := store.Register(ctx, RegisterInput{MachineID: "mach_1", AgentID: "agent_1"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	first, err := store.SubmitOffer(ctx, OfferInput{
		MachineID: "mach_1",
		SDP:       minimalRegistrySDP("offer-first"),
	})
	if err != nil {
		t.Fatalf("submit first offer: %v", err)
	}
	second, err := store.SubmitOffer(ctx, OfferInput{
		SessionID: "rtc_second",
		MachineID: "mach_1",
		SDP:       minimalRegistrySDP("offer-second"),
	})
	if err != nil {
		t.Fatalf("submit second offer: %v", err)
	}
	second.SessionID = first.ID
	store.offerQueue.set(second)

	found, ok := store.Offer(ctx, OfferLookupInput{MachineID: "mach_1", OfferID: first.ID})
	if !ok || found.ID != second.ID {
		t.Fatalf("lookup should prefer public session match, got %+v ok=%v; first=%s second=%s", found, ok, first.ID, second.ID)
	}
}

func waitForPollWaiter(t *testing.T, store *Registry, machineID string) {
	t.Helper()
	waitForOfferPollWaiter(t, store, machineID)
}

func waitForOfferPollWaiter(t *testing.T, store *Registry, machineID string) {
	t.Helper()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.offerQueue.waiterCount(machineID) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("poll waiter was not registered")
}

func waitForPairingPollWaiter(t *testing.T, store *Registry, machineID string) {
	t.Helper()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if store.pairingQueue.waiterCount(machineID) > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pairing poll waiter was not registered")
}

type offerPollResult struct {
	Offer Offer
	Err   error
}

type pairingPollResult struct {
	Claim PairingClaim
	Err   error
}

type fixedRegistryClock time.Time

func (c fixedRegistryClock) Now() time.Time {
	return time.Time(c)
}

func minimalRegistrySDP(sessionID string) string {
	return "v=0\r\no=- " + sessionID + " 1 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel"
}
