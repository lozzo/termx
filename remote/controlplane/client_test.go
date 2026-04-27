package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientLoginAndMeFlow(t *testing.T) {
	t.Helper()

	var loginSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			loginSeen = true
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST /api/auth/login, got %s", r.Method)
			}
			if got := r.Header.Get("X-Client-Type"); got != "app" {
				t.Fatalf("expected X-Client-Type=app, got %q", got)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if body["username"] != "alice" || body["password"] != "secret" {
				t.Fatalf("unexpected login body: %#v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":         "access-1",
				"refresh_token": "refresh-1",
				"user": map[string]string{
					"id":       "user-1",
					"username": "alice",
					"role":     "user",
				},
			})
		case "/api/auth/me":
			if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
				t.Fatalf("unexpected me auth header: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]string{
					"id":       "user-1",
					"username": "alice",
					"role":     "user",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)

	login, err := client.Login(context.Background(), "alice", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if !loginSeen {
		t.Fatal("expected login endpoint to be called")
	}
	if login.Token != "access-1" || login.RefreshToken != "refresh-1" {
		t.Fatalf("unexpected login result: %#v", login)
	}
	if login.User.ID != "user-1" || login.User.Username != "alice" {
		t.Fatalf("unexpected login user: %#v", login.User)
	}

	user, err := client.Me(context.Background(), login.Token)
	if err != nil {
		t.Fatalf("Me returned error: %v", err)
	}
	if user.ID != "user-1" || user.Username != "alice" {
		t.Fatalf("unexpected me user: %#v", user)
	}
}

func TestClientRefreshParsesRotatedRefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/refresh" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST /api/auth/refresh, got %s", r.Method)
		}
		if got := r.Header.Get("X-Client-Type"); got != "app" {
			t.Fatalf("expected X-Client-Type=app, got %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode refresh body: %v", err)
		}
		if body["refresh_token"] != "refresh-1" {
			t.Fatalf("unexpected refresh body: %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":         "access-2",
			"refresh_token": "refresh-2",
			"user": map[string]string{
				"id":       "user-1",
				"username": "alice",
				"role":     "user",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	res, err := client.Refresh(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if res.Token != "access-2" || res.RefreshToken != "refresh-2" {
		t.Fatalf("unexpected refresh result: %#v", res)
	}
	if res.User.Username != "alice" {
		t.Fatalf("unexpected refresh user: %#v", res.User)
	}
}
