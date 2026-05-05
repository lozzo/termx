package ice_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/ice"
	hubturn "github.com/lozzow/termx/termx-remote/hub/turn"
)

func TestCloudPaidLeaseGetsTemporaryTurnCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 33, 0, 0, time.UTC))
	svc := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp", "turn:hub.termx.test:3478?transport=tcp"},
	})
	cfg, err := svc.ConfigForLease(ctx, ice.Lease{
		ID:         "lease_1",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ice config: %v", err)
	}
	if cfg.Path != ice.PathCloud {
		t.Fatalf("path = %q", cfg.Path)
	}
	if len(cfg.ICEServers) != 2 {
		t.Fatalf("ice servers = %+v", cfg.ICEServers)
	}
	if strings.HasPrefix(cfg.Path, "relay") {
		t.Fatalf("relay became client path: %+v", cfg)
	}
	turn := cfg.ICEServers[1]
	if len(turn.URLs) != 2 || turn.Username == "" || turn.Credential == "" {
		t.Fatalf("turn server = %+v", turn)
	}
	if turn.ExpiresAt == nil || !turn.ExpiresAt.Equal(clock.Now().Add(ice.MaxCredentialTTL)) {
		t.Fatalf("turn expires_at = %v", turn.ExpiresAt)
	}
	if !strings.Contains(turn.Username, "lease_1:") {
		t.Fatalf("turn username is not lease-scoped: %q", turn.Username)
	}
	if !svc.VerifyCredential(turn.Username, turn.Credential, clock.Now(), "lease_1") {
		t.Fatal("generated turn credential did not verify")
	}
	if svc.VerifyCredential(turn.Username, turn.Credential, clock.Now(), "other_lease") {
		t.Fatal("turn credential verified for wrong lease")
	}
	if svc.VerifyCredential(turn.Username, turn.Credential, clock.Now().Add(ice.MaxCredentialTTL+time.Second), "lease_1") {
		t.Fatal("expired turn credential still verified")
	}
}

func TestCloudPaidLeaseGetsEmbeddedTurnCredentials(t *testing.T) {
	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 5, 7, 0, 0, 0, time.UTC))
	turnServer, err := hubturn.NewServer(hubturn.Config{
		ListenAddr: "127.0.0.1:0",
		PublicIP:   "127.0.0.1",
		Secret:     "embedded-secret",
		Realm:      "termx",
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("turn server: %v", err)
	}
	defer turnServer.Close()
	svc := ice.NewService(ice.Config{
		Clock:      clock,
		STUNURLs:   []string{"stun:hub.termx.test:3478"},
		TURNServer: turnServer,
	})

	cfg, err := svc.ConfigForLease(ctx, ice.Lease{
		ID:         "embedded",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ice config: %v", err)
	}
	if len(cfg.ICEServers) != 2 {
		t.Fatalf("ice servers = %+v", cfg.ICEServers)
	}
	turn := cfg.ICEServers[1]
	if len(turn.URLs) != 2 || !strings.HasPrefix(turn.URLs[0], "turn:127.0.0.1:") || !strings.Contains(turn.URLs[0], "transport=udp") || !strings.Contains(turn.URLs[1], "transport=tcp") {
		t.Fatalf("embedded turn urls = %+v", turn.URLs)
	}
	if turn.Username == "" || turn.Credential == "" {
		t.Fatalf("embedded turn credentials missing: %+v", turn)
	}
	if turn.ExpiresAt == nil || !turn.ExpiresAt.Equal(clock.Now().Add(24*time.Hour)) {
		t.Fatalf("embedded turn expires_at = %v", turn.ExpiresAt)
	}
	if key, ok := turnServer.AuthHandler()(turn.Username, "termx", nil); !ok || len(key) == 0 {
		t.Fatal("embedded turn credential did not authenticate")
	}
}

func TestTurnCredentialDoesNotOutliveLeaseAndRequiresSecret(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 36, 0, 0, time.UTC))
	svc := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	cfg, err := svc.ConfigForLease(ctx, ice.Lease{
		ID:         "lease_short",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("short lease ice config: %v", err)
	}
	if len(cfg.ICEServers) != 2 || cfg.ICEServers[1].ExpiresAt == nil || !cfg.ICEServers[1].ExpiresAt.Equal(clock.Now().Add(time.Minute)) {
		t.Fatalf("short lease turn server = %+v", cfg.ICEServers)
	}

	missingSecret := ice.NewService(ice.Config{
		Clock:    clock,
		Realm:    "termx.test",
		STUNURLs: []string{"stun:hub.termx.test:3478"},
		TURNURLs: []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	if _, err := missingSecret.ConfigForLease(ctx, ice.Lease{
		ID:         "lease_no_secret",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Minute),
	}); err == nil {
		t.Fatal("turn config without shared secret succeeded")
	}
	if _, err := svc.ConfigForLease(ctx, ice.Lease{
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Minute),
	}); !errors.Is(err, ice.ErrLeaseRequired) {
		t.Fatalf("blank lease id err = %v", err)
	}
}

func TestCloudWithoutRelayAndPublicP2PDoNotReceiveTurnCredentials(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 34, 0, 0, time.UTC))
	svc := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	for _, tc := range []ice.Lease{
		{ID: "free_cloud", Path: ice.PathCloud, AllowRelay: false, ExpiresAt: clock.Now().Add(time.Minute)},
		{ID: "public_p2p", Path: ice.PathPublicP2P, AllowRelay: true, ExpiresAt: clock.Now().Add(time.Minute)},
	} {
		cfg, err := svc.ConfigForLease(ctx, tc)
		if err != nil {
			t.Fatalf("ice config for %+v: %v", tc, err)
		}
		if cfg.Path != tc.Path {
			t.Fatalf("path = %q, want %q", cfg.Path, tc.Path)
		}
		if len(cfg.ICEServers) != 1 || cfg.ICEServers[0].Username != "" || cfg.ICEServers[0].Credential != "" {
			t.Fatalf("non-paid/non-cloud got turn credentials: %+v", cfg)
		}
		if strings.Contains(strings.ToLower(cfg.String()), "turn:") {
			t.Fatalf("non-paid/non-cloud response contains TURN URL: %+v", cfg)
		}
	}
}

func TestExpiredAndInvalidPathLeasesDoNotReceiveTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 40, 0, 0, time.UTC))
	svc := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	if _, err := svc.ConfigForLease(ctx, ice.Lease{
		ID:         "expired",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(-time.Second),
	}); !errors.Is(err, ice.ErrLeaseExpired) {
		t.Fatalf("expired lease err = %v", err)
	}
	if _, err := svc.ConfigForLease(ctx, ice.Lease{
		ID:         "bad_path",
		Path:       "relay",
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Minute),
	}); !errors.Is(err, ice.ErrInvalidPath) {
		t.Fatalf("invalid path err = %v", err)
	}
}

func TestICEConfigRejectsMisconfiguredURLSchemes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := fixedClock(time.Date(2026, 5, 3, 6, 42, 0, 0, time.UTC))
	badSTUN := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
		TURNURLs:     []string{"turn:hub.termx.test:3478?transport=udp"},
	})
	if _, err := badSTUN.ConfigForLease(ctx, ice.Lease{
		ID:         "free",
		Path:       ice.PathCloud,
		AllowRelay: false,
		ExpiresAt:  clock.Now().Add(time.Minute),
	}); !errors.Is(err, ice.ErrInvalidICEURL) {
		t.Fatalf("bad STUN url err = %v", err)
	}
	badTURN := ice.NewService(ice.Config{
		Clock:        clock,
		Realm:        "termx.test",
		SharedSecret: "turn-secret",
		STUNURLs:     []string{"stun:hub.termx.test:3478"},
		TURNURLs:     []string{"stun:hub.termx.test:3478"},
	})
	if _, err := badTURN.ConfigForLease(ctx, ice.Lease{
		ID:         "paid",
		Path:       ice.PathCloud,
		AllowRelay: true,
		ExpiresAt:  clock.Now().Add(time.Minute),
	}); !errors.Is(err, ice.ErrInvalidICEURL) {
		t.Fatalf("bad TURN url err = %v", err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time {
	return time.Time(c)
}
