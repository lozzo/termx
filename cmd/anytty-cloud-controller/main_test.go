package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/anytty/anytty/cloud/controller/commerce"
	"github.com/anytty/anytty/cloud/controller/enrollment"
	cloudv1 "github.com/anytty/anytty/proto/cloud/v1"
)

type controllerShutdownFunc func(context.Context) error

type daemonConnectionStoreFunc func(context.Context, string) (enrollment.Daemon, error)

func (function daemonConnectionStoreFunc) GetDaemon(ctx context.Context, daemonID string) (enrollment.Daemon, error) {
	return function(ctx, daemonID)
}

type daemonConnectionEntitlementFunc func(context.Context, string) (*cloudv1.EffectiveEntitlement, error)

func (function daemonConnectionEntitlementFunc) EffectiveEntitlement(ctx context.Context, accountID string) (*cloudv1.EffectiveEntitlement, error) {
	return function(ctx, accountID)
}

func (shutdown controllerShutdownFunc) Shutdown(ctx context.Context) error {
	return shutdown(ctx)
}

func TestShutdownControllerStartsBothComponentsBeforeDeadline(t *testing.T) {
	for _, blockingComponent := range []string{"HTTPS", "runtime"} {
		t.Run(blockingComponent, func(t *testing.T) {
			httpsStarted := make(chan struct{})
			runtimeStarted := make(chan struct{})
			persistentErr := errors.New("persistent shutdown failure")
			block := func(started chan struct{}) controllerShutdownFunc {
				return func(ctx context.Context) error {
					close(started)
					<-ctx.Done()
					return ctx.Err()
				}
			}
			fail := func(started chan struct{}) controllerShutdownFunc {
				return func(context.Context) error {
					close(started)
					return persistentErr
				}
			}
			httpsShutdown, runtimeShutdown := fail(httpsStarted), block(runtimeStarted)
			if blockingComponent == "HTTPS" {
				httpsShutdown, runtimeShutdown = block(httpsStarted), fail(runtimeStarted)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			err := shutdownController(ctx, httpsShutdown, runtimeShutdown)
			if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, persistentErr) {
				t.Fatalf("shutdown error = %v", err)
			}
			select {
			case <-httpsStarted:
			default:
				t.Fatal("HTTPS shutdown was not started")
			}
			select {
			case <-runtimeStarted:
			default:
				t.Fatal("runtime shutdown was not started")
			}
		})
	}
}

func TestResolveDaemonConnectionLimitValidatesIdentityAndEntitlement(t *testing.T) {
	activeStore := daemonConnectionStoreFunc(func(_ context.Context, daemonID string) (enrollment.Daemon, error) {
		return enrollment.Daemon{ID: daemonID, AccountID: "account-a", State: cloudv1.DaemonState_DAEMON_STATE_ACTIVE}, nil
	})
	activeEntitlement := daemonConnectionEntitlementFunc(func(_ context.Context, accountID string) (*cloudv1.EffectiveEntitlement, error) {
		return &cloudv1.EffectiveEntitlement{AccountId: accountID, State: cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE, Capability: &cloudv1.CloudCapability{CloudDaemonLimit: 3}}, nil
	})
	if limit, err := resolveDaemonConnectionLimit(context.Background(), activeStore, activeEntitlement, "daemon-a", "account-a"); err != nil || limit != 3 {
		t.Fatalf("active daemon limit=%d err=%v", limit, err)
	}
	if _, err := resolveDaemonConnectionLimit(context.Background(), activeStore, activeEntitlement, "daemon-a", "account-b"); err == nil {
		t.Fatal("cross-account daemon identity was admitted")
	}
	blockedStore := daemonConnectionStoreFunc(func(_ context.Context, daemonID string) (enrollment.Daemon, error) {
		return enrollment.Daemon{ID: daemonID, AccountID: "account-a", State: cloudv1.DaemonState_DAEMON_STATE_BLOCKED}, nil
	})
	if _, err := resolveDaemonConnectionLimit(context.Background(), blockedStore, activeEntitlement, "daemon-a", "account-a"); err == nil {
		t.Fatal("blocked daemon was admitted")
	}
	expiredEntitlement := daemonConnectionEntitlementFunc(func(_ context.Context, accountID string) (*cloudv1.EffectiveEntitlement, error) {
		return &cloudv1.EffectiveEntitlement{AccountId: accountID, State: cloudv1.EntitlementState_ENTITLEMENT_STATE_EXPIRED, Capability: &cloudv1.CloudCapability{CloudDaemonLimit: 3}}, nil
	})
	if _, err := resolveDaemonConnectionLimit(context.Background(), activeStore, expiredEntitlement, "daemon-a", "account-a"); !errors.Is(err, commerce.ErrEntitlementUnavailable) {
		t.Fatalf("expired entitlement error=%v", err)
	}
	zeroEntitlement := daemonConnectionEntitlementFunc(func(_ context.Context, accountID string) (*cloudv1.EffectiveEntitlement, error) {
		return &cloudv1.EffectiveEntitlement{AccountId: accountID, State: cloudv1.EntitlementState_ENTITLEMENT_STATE_ACTIVE, Capability: &cloudv1.CloudCapability{}}, nil
	})
	if _, err := resolveDaemonConnectionLimit(context.Background(), activeStore, zeroEntitlement, "daemon-a", "account-a"); !errors.Is(err, commerce.ErrEntitlementUnavailable) {
		t.Fatalf("zero daemon limit error=%v", err)
	}
}

func TestShutdownControllerJoinsLabeledComponentErrors(t *testing.T) {
	httpsErr := errors.New("HTTPS sentinel")
	runtimeErr := errors.New("runtime sentinel")
	err := shutdownController(
		context.Background(),
		controllerShutdownFunc(func(context.Context) error { return httpsErr }),
		controllerShutdownFunc(func(context.Context) error { return runtimeErr }),
	)
	if !errors.Is(err, httpsErr) || !errors.Is(err, runtimeErr) {
		t.Fatalf("joined shutdown error = %v", err)
	}
	if !strings.Contains(err.Error(), "Controller HTTPS") || !strings.Contains(err.Error(), "Controller runtime") {
		t.Fatalf("shutdown error lacks component labels: %v", err)
	}
}

func TestParseOptionsSupportsHelpAndRejectsPositionalArguments(t *testing.T) {
	if _, err := parseOptions([]string{"--help"}, io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help error = %v, want flag.ErrHelp", err)
	}
	if _, err := parseOptions([]string{"unexpected"}, io.Discard); err == nil {
		t.Fatal("positional Controller argument was accepted")
	}
}

func TestParseOptionsTrustedProxyCIDRsAreExplicitAndRepeatable(t *testing.T) {
	config, err := parseOptions([]string{"--trusted-proxy-cidr=127.0.0.0/8", "--trusted-proxy-cidr=2001:db8::/32"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("2001:db8::/32")}
	if len(config.trustedProxyCIDRs) != len(want) || config.trustedProxyCIDRs[0] != want[0] || config.trustedProxyCIDRs[1] != want[1] {
		t.Fatalf("trusted proxy CIDRs = %v", config.trustedProxyCIDRs)
	}
	config, err = parseOptions(nil, io.Discard)
	if err != nil || len(config.trustedProxyCIDRs) != 0 {
		t.Fatalf("default trusted proxy CIDRs = %v, err=%v", config.trustedProxyCIDRs, err)
	}
	if _, err := parseOptions([]string{"--trusted-proxy-cidr=not-a-cidr"}, io.Discard); err == nil {
		t.Fatal("invalid trusted proxy CIDR was accepted")
	}
}
