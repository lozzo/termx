package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lozzow/termx/web-control/internal/account"
	"github.com/lozzow/termx/web-control/internal/httpapi"
	"github.com/lozzow/termx/web-control/internal/machines"
	"github.com/lozzow/termx/web-control/internal/rendezvous"
	"github.com/lozzow/termx/web-control/internal/store"
)

func TestPublicP2PRendezvousHTTPFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := store.OpenSQLite(ctx, "file:termx-http-rendezvous-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	accounts := account.NewService(account.Config{
		DB:     db,
		Clock:  fixedClock(time.Date(2026, 5, 3, 6, 52, 0, 0, time.UTC)),
		Tokens: account.NewHMACTokenIssuer([]byte("slice-4-http-secret")),
	})
	machineSvc := machines.NewService(machines.Config{DB: db, Clock: fixedClock(time.Date(2026, 5, 3, 6, 52, 0, 0, time.UTC))})
	rv := rendezvous.NewService(rendezvous.Config{
		DB:                    db,
		Clock:                 fixedClock(time.Date(2026, 5, 3, 6, 52, 0, 0, time.UTC)),
		STUNServers:           []string{"stun:stun.termx.test:3478"},
		MaxPayloadBytes:       2048,
		MaxMessagesPerChannel: 4,
	})
	router := httpapi.NewRouter(httpapi.Config{Accounts: accounts, Machines: machineSvc, Rendezvous: rv, MaxPublicP2PBodyBytes: 2048})

	register := postJSON(t, router, "/api/v1/auth/register", map[string]string{
		"email":    "rv-http@example.com",
		"password": "valid password",
	}, "")
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", register.Code, register.Body.String())
	}
	var auth struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, register, &auth)
	boot, err := machineSvc.Bootstrap(ctx, machines.BootstrapInput{MachinePublicKey: "machine-public-key", DisplayName: "HTTP RV"})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	user, err := accounts.Me(ctx, auth.AccessToken)
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if _, err := machineSvc.Claim(ctx, machines.ClaimInput{UserID: user.User.ID, MachineID: boot.Machine.ID, ClaimToken: boot.ClaimToken}); err != nil {
		t.Fatalf("claim: %v", err)
	}

	unauth := postJSON(t, router, "/api/v1/public-p2p/channels", map[string]string{
		"machine_id":  boot.Machine.ID,
		"terminal_id": "term_1",
	}, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth channel status = %d", unauth.Code)
	}

	create := postJSON(t, router, "/api/v1/public-p2p/channels", map[string]string{
		"machine_id":  boot.Machine.ID,
		"terminal_id": "term_1",
	}, auth.AccessToken)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	if strings.Contains(strings.ToLower(create.Body.String()), "turn:") {
		t.Fatalf("create response contains TURN: %s", create.Body.String())
	}
	var channel struct {
		ID            string `json:"id"`
		Secret        string `json:"secret"`
		Path          string `json:"path"`
		ChannelID     string `json:"channel_id"`
		ChannelSecret string `json:"channel_secret"`
	}
	decodeJSON(t, create, &channel)
	if channel.ID == "" || channel.Secret == "" || channel.Path != "public_p2p" {
		t.Fatalf("channel response = %+v", channel)
	}
	if channel.ChannelID != channel.ID || channel.ChannelSecret != channel.Secret {
		t.Fatalf("documented channel aliases missing: %+v", channel)
	}
	oversizedCreate := httptest.NewRecorder()
	oversizedCreateReq := httptest.NewRequest(http.MethodPost, "/api/v1/public-p2p/channels", bytes.NewReader([]byte(`{"machine_id":"`+strings.Repeat("x", 4096)+`","terminal_id":"term_1"}`)))
	oversizedCreateReq.Header.Set("Content-Type", "application/json")
	oversizedCreateReq.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	router.ServeHTTP(oversizedCreate, oversizedCreateReq)
	if oversizedCreate.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized create status = %d body=%s", oversizedCreate.Code, oversizedCreate.Body.String())
	}

	sendOffer := postJSON(t, router, "/api/v1/public-p2p/channels/"+channel.ID+"/offer", map[string]any{
		"channel_secret": channel.Secret,
		"app_certificate": map[string]any{
			"payload":   map[string]any{"machine_id": boot.Machine.ID},
			"signature": "base64-signature",
		},
		"offer": map[string]any{"sdp": "offer", "ice_candidates": []any{
			map[string]any{"candidate": "candidate:1 1 udp 1 192.0.2.1 1 typ host"},
		}},
		"signature": map[string]any{
			"algorithm": "ed25519",
			"nonce":     "nonce",
			"timestamp": 1770000000,
			"value":     "base64-signature",
		},
	}, "")
	if sendOffer.Code != http.StatusAccepted {
		t.Fatalf("offer status = %d body=%s", sendOffer.Code, sendOffer.Body.String())
	}
	sendAnswer := postJSON(t, router, "/api/v1/public-p2p/channels/"+channel.ID+"/answer", map[string]any{
		"channel_secret": channel.Secret,
		"answer":         map[string]any{"sdp": "answer", "ice_candidates": []any{}},
	}, "")
	if sendAnswer.Code != http.StatusAccepted {
		t.Fatalf("answer status = %d body=%s", sendAnswer.Code, sendAnswer.Body.String())
	}
	sendCandidate := postJSON(t, router, "/api/v1/public-p2p/channels/"+channel.ID+"/candidate", map[string]any{
		"channel_secret": channel.Secret,
		"app_public_key": "base64-app-public-key",
		"candidate": map[string]any{
			"candidate":   "candidate:1 1 udp 1 192.0.2.2 1 typ host",
			"mid":         "0",
			"mline_index": 0,
		},
	}, "")
	if sendCandidate.Code != http.StatusAccepted {
		t.Fatalf("candidate status = %d body=%s", sendCandidate.Code, sendCandidate.Body.String())
	}
	oversized := postJSON(t, router, "/api/v1/public-p2p/channels/"+channel.ID+"/candidate", map[string]any{
		"channel_secret": channel.Secret,
		"payload":        map[string]any{"candidate": strings.Repeat("x", 4096)},
	}, "")
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d body=%s", oversized.Code, oversized.Body.String())
	}

	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/public-p2p/channels/"+channel.ID+"/events?channel_secret="+channel.Secret, nil)
	router.ServeHTTP(list, req)
	if list.Code != http.StatusUnauthorized {
		t.Fatalf("query-secret events status = %d body=%s", list.Code, list.Body.String())
	}

	list = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/public-p2p/channels/"+channel.ID+"/events", nil)
	req.Header.Set("Authorization", "Rendezvous "+channel.ID+":"+channel.Secret)
	router.ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("events status = %d body=%s", list.Code, list.Body.String())
	}
	var events struct {
		Messages []struct {
			Type string `json:"type"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(events.Messages) != 3 || events.Messages[0].Type != "offer" || events.Messages[1].Type != "answer" || events.Messages[2].Type != "candidate" {
		t.Fatalf("events = %+v", events)
	}
	if strings.Contains(strings.ToLower(list.Body.String()), "terminal_data") {
		t.Fatalf("events leaked runtime data: %s", list.Body.String())
	}
}
