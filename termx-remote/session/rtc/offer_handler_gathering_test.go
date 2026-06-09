package rtc

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestGatheringEarlyStopOnHostCandidate(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()
	candidateAt := make(chan time.Time, 1)

	start := time.Now()
	wait := newICEGatheringWaiterEvents(mock.onCandidate, complete, 500*time.Millisecond, 5*time.Second)
	done := runGatheringWait(wait)

	time.AfterFunc(100*time.Millisecond, func() {
		candidateAt <- time.Now()
		mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost})
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForICEGatheringEvents did not early-stop within 1s after host candidate")
	}
	emittedAt := <-candidateAt
	if elapsed := time.Since(emittedAt); elapsed < 450*time.Millisecond {
		t.Fatalf("early stop returned before the 500ms candidate grace period elapsed: %s since candidate", elapsed)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("early stop should return within 1s, got %s", elapsed)
	}
}

func TestGatheringEarlyStopOnRelayCandidate(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()

	wait := newICEGatheringWaiterEvents(mock.onCandidate, complete, 25*time.Millisecond, time.Second)
	done := runGatheringWait(wait)
	mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeRelay})

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waitForICEGatheringEvents did not early-stop after relay candidate")
	}
}

func TestGatheringRequiresRelayCandidateWhenTURNConfigured(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()

	wait := newICEGatheringWaiterEventsWithPolicy(mock.onCandidate, complete, 25*time.Millisecond, time.Second, true)
	done := runGatheringWait(wait)
	mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost})

	select {
	case <-done:
		t.Fatal("relay-required gathering returned after host candidate")
	case <-time.After(100 * time.Millisecond):
	}

	mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeRelay})
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("relay-required gathering did not early-stop after relay candidate")
	}
}

func TestHasTURNServer(t *testing.T) {
	if !hasTURNServer([]webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302", " TURN:114.66.58.243:3478?transport=udp "}}}) {
		t.Fatal("TURN URL was not detected")
	}
	if !hasTURNServer([]webrtc.ICEServer{{URLs: []string{"turns:turn.example.com:5349"}}}) {
		t.Fatal("TURNS URL was not detected")
	}
	if hasTURNServer([]webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}) {
		t.Fatal("STUN-only config should not require relay gathering")
	}
}

func TestGatheringHardTimeoutWhenNoCandidate(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()

	start := time.Now()
	newICEGatheringWaiterEvents(mock.onCandidate, complete, 500*time.Millisecond, 5*time.Second)()
	elapsed := time.Since(start)

	if elapsed < 4500*time.Millisecond {
		t.Fatalf("hard timeout returned too early: %s", elapsed)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("hard timeout should return near 5s, got %s", elapsed)
	}
}

func TestGatheringCompleteBeforeEarlyStop(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()

	start := time.Now()
	wait := newICEGatheringWaiterEvents(mock.onCandidate, complete, 500*time.Millisecond, 5*time.Second)
	done := runGatheringWait(wait)

	time.AfterFunc(100*time.Millisecond, func() {
		mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost})
	})
	time.AfterFunc(200*time.Millisecond, func() {
		close(complete)
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waitForICEGatheringEvents did not return on gathering completion")
	}
	if elapsed := time.Since(start); elapsed > 450*time.Millisecond {
		t.Fatalf("gathering complete should win over the 500ms early stop, got %s", elapsed)
	}
}

func TestGatheringWaiterRegistersCandidateCallbackBeforeWaiting(t *testing.T) {
	complete := make(chan struct{})
	mock := newMockICEGatheringPeer()

	wait := newICEGatheringWaiterEvents(mock.onCandidate, complete, 500*time.Millisecond, 5*time.Second)
	if mock.onCandidateFn == nil {
		t.Fatal("candidate callback must be registered before SetLocalDescription starts gathering")
	}
	done := runGatheringWait(wait)
	mock.emitCandidate(&webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("registered waiter did not early-stop after pre-wait candidate")
	}
}

func runGatheringWait(wait func()) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		wait()
		close(done)
	}()
	return done
}

type mockICEGatheringPeer struct {
	onCandidateFn func(*webrtc.ICECandidate)
}

func newMockICEGatheringPeer() *mockICEGatheringPeer {
	return &mockICEGatheringPeer{}
}

func (m *mockICEGatheringPeer) onCandidate(fn func(*webrtc.ICECandidate)) {
	m.onCandidateFn = fn
}

func (m *mockICEGatheringPeer) emitCandidate(candidate *webrtc.ICECandidate) {
	if m.onCandidateFn != nil {
		m.onCandidateFn(candidate)
	}
}
