package webcontroller_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

func TestBrowserHandlerOwnsCookieOriginAndCSRF(t *testing.T) {
	center := webcontroller.NewUserCenterStore(time.Now)
	defer center.Close()
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	commerce, err := webcontroller.NewCommerceService([]byte("0123456789abcdef0123456789abcdef"), &entitlementPublisher{}, catalog, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	commerce.AttachUserCenter(center)
	handler, err := webcontroller.BrowserHandler(webcontroller.BrowserConfig{Catalog: &catalog, Commerce: commerce, UserCenter: center, RelayURL: "turn:127.0.0.1:41003", StagingLogin: true})
	if err != nil {
		t.Fatal(err)
	}

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "http://web.test/api/auth/password/register", strings.NewReader(`{"email":"blocked@example.com","password":"secure-password"}`)))
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-origin register = %d", blocked.Code)
	}

	register := httptest.NewRequest(http.MethodPost, "http://web.test/api/auth/password/register", strings.NewReader(`{"email":"owner@example.com","password":"secure-password","aff":"TERMXDEV"}`))
	register.Header.Set("Origin", "http://web.test")
	register.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	handler.ServeHTTP(registered, register)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", registered.Code, registered.Body.String())
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range registered.Result().Cookies() {
		if cookie.Name == "termx_web_session" {
			sessionCookie = cookie
		}
		if cookie.Name == "termx_csrf" {
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil || !sessionCookie.HttpOnly || csrfCookie.HttpOnly {
		t.Fatalf("cookies = %#v", registered.Result().Cookies())
	}

	centerRequest := httptest.NewRequest(http.MethodGet, "http://web.test/api/center", nil)
	centerRequest.AddCookie(sessionCookie)
	centerResponse := httptest.NewRecorder()
	handler.ServeHTTP(centerResponse, centerRequest)
	if centerResponse.Code != http.StatusOK {
		t.Fatalf("center = %d", centerResponse.Code)
	}

	profile := httptest.NewRequest(http.MethodPatch, "http://web.test/api/profile", strings.NewReader(`{"display_name":"Owner"}`))
	profile.Header.Set("Origin", "http://web.test")
	profile.Header.Set("X-TermX-CSRF", csrfCookie.Value)
	profile.AddCookie(sessionCookie)
	profile.AddCookie(csrfCookie)
	profileResponse := httptest.NewRecorder()
	handler.ServeHTTP(profileResponse, profile)
	if profileResponse.Code != http.StatusOK {
		t.Fatalf("profile = %d: %s", profileResponse.Code, profileResponse.Body.String())
	}
	logout := httptest.NewRequest(http.MethodPost, "http://web.test/api/auth/logout", nil)
	logout.Header.Set("Origin", "http://web.test")
	logout.Header.Set("X-TermX-CSRF", csrfCookie.Value)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout = %d", logoutResponse.Code)
	}
	reuse := httptest.NewRequest(http.MethodGet, "http://web.test/api/center", nil)
	reuse.AddCookie(sessionCookie)
	reuseResponse := httptest.NewRecorder()
	handler.ServeHTTP(reuseResponse, reuse)
	if reuseResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session reuse = %d", reuseResponse.Code)
	}
}
