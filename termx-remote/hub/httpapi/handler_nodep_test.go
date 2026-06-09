package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lozzow/termx/termx-remote/hub/cloud"
	"github.com/lozzow/termx/termx-remote/hub/httpapi"
	"github.com/lozzow/termx/termx-remote/hub/registry"
)

func TestHandlerConfigHasNoControlVerifierFields(t *testing.T) {
	t.Parallel()

	configType := reflect.TypeOf(httpapi.Config{})
	for _, field := range []string{
		"Agent" + "Policy",
		"Agent" + "PolicyProvider",
		"ControlVerifier",
		"ConnectionTicket" + "Verifier",
	} {
		if _, ok := configType.FieldByName(field); ok {
			t.Fatalf("httpapi.Config still exposes control verifier field %q", field)
		}
	}
}

func TestHubSessionDoesNotCallExternalHTTP(t *testing.T) {
	clock := fixedClock(time.Date(2026, 5, 5, 9, 10, 0, 0, time.UTC))
	var outboundHTTP atomic.Int32
	originalTransport := http.DefaultTransport
	http.DefaultTransport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		outboundHTTP.Add(1)
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusTeapot)
		return rec.Result(), nil
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	reg := registry.New(registry.Config{Clock: clock})
	if _, err := reg.Register(context.Background(), registry.RegisterInput{
		MachineID: "mach_nodep",
		AgentID:   "agent_nodep",
	}); err != nil {
		t.Fatalf("register agent without control verifier: %v", err)
	}
	router := httpapi.NewHandler(httpapi.Config{
		Cloud:         cloud.NewService(cloud.Config{Registry: reg, Clock: clock}),
		Registry:      reg,
		AnswerTimeout: time.Millisecond,
		PollInterval:  time.Millisecond,
		Clock:         clock,
	})

	resp := postJSON(t, router, "/api/v1/sessions", validCloudSessionRequestWithIDs("mach_nodep", "term_nodep", "rtc_nodep"))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("session status = %d body=%s", resp.Code, resp.Body.String())
	}
	if got := outboundHTTP.Load(); got != 0 {
		t.Fatalf("hub made %d outbound HTTP request(s) while handling /api/v1/sessions", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
