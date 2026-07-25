package webcontroller_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cloudcatalog "github.com/muxvia/muxvia/private/cloud/control-plane/catalog"
	"github.com/muxvia/muxvia/private/cloud/control-plane/commerce"
	postgrestest "github.com/muxvia/muxvia/private/cloud/control-plane/postgrestest"
	webcontroller "github.com/muxvia/muxvia/private/cloud/web-controller"
	"github.com/muxvia/muxvia/proto/cloudpb"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestProductAPIUsesProtoCookieCSRFAndPersistentCommerce(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	catalogSource, _ := cloudcatalog.NewSnapshotSource(catalog.Contract())
	service, err := commerce.New(commerce.Config{Store: store, Catalog: catalogSource, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: service, EnableTestPaymentProvider: true})
	if err != nil {
		t.Fatal(err)
	}

	register := productRequest(http.MethodPost, "/api/v1/account/register", `{"email":"owner@example.com","password":"secure-password"}`, nil)
	registerResponse := httptest.NewRecorder()
	handler.ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusCreated || strings.Contains(registerResponse.Body.String(), "access_token") || strings.Contains(registerResponse.Body.String(), "refresh_token") {
		t.Fatalf("register = %d: %s", registerResponse.Code, registerResponse.Body.String())
	}
	cookies := cookieMap(registerResponse.Result().Cookies())
	if cookies["muxvia_cloud_access"] == nil || cookies["muxvia_cloud_refresh"] == nil || cookies["muxvia_cloud_csrf"] == nil {
		t.Fatalf("register cookies = %#v", cookies)
	}
	duplicate := productRequest(http.MethodPost, "/api/v1/account/register", `{"email":"owner@example.com","password":"secure-password"}`, nil)
	duplicateResponse := httptest.NewRecorder()
	handler.ServeHTTP(duplicateResponse, duplicate)
	if duplicateResponse.Code != http.StatusConflict || strings.Contains(strings.ToLower(duplicateResponse.Body.String()), "sqlite") || strings.Contains(strings.ToLower(duplicateResponse.Body.String()), "unique") {
		t.Fatalf("duplicate register error = %d: %s", duplicateResponse.Code, duplicateResponse.Body.String())
	}

	commerceRequest := productRequest(http.MethodGet, "/api/v1/account/commerce", "", cookies)
	commerceResponse := httptest.NewRecorder()
	handler.ServeHTTP(commerceResponse, commerceRequest)
	if commerceResponse.Code != http.StatusOK || !strings.Contains(commerceResponse.Body.String(), `"plan_id":"managed-free"`) {
		t.Fatalf("commerce = %d: %s", commerceResponse.Code, commerceResponse.Body.String())
	}

	withoutCSRF := productRequest(http.MethodPost, "/api/v1/checkout", `{"plan_id":"pro","requested_transition":"SUBSCRIPTION_TRANSITION_KIND_UPGRADE"}`, cookies)
	withoutCSRF.Header.Del("X-Muxvia-CSRF")
	withoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusUnauthorized {
		t.Fatalf("checkout without CSRF = %d: %s", withoutCSRFResponse.Code, withoutCSRFResponse.Body.String())
	}

	checkout := productRequest(http.MethodPost, "/api/v1/checkout", `{"plan_id":"pro","requested_transition":"SUBSCRIPTION_TRANSITION_KIND_UPGRADE"}`, cookies)
	checkoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(checkoutResponse, checkout)
	if checkoutResponse.Code != http.StatusCreated {
		t.Fatalf("checkout = %d: %s", checkoutResponse.Code, checkoutResponse.Body.String())
	}
	checkoutContract := &cloudpb.CreateCheckoutResponse{}
	if err := protojson.Unmarshal(checkoutResponse.Body.Bytes(), checkoutContract); err != nil {
		t.Fatal(err)
	}
	orderID := checkoutContract.GetOrder().GetOrderId()
	confirm := productRequest(http.MethodPost, "/api/v1/checkout/test-payment", `{"order_id":"`+orderID+`","event_type":"PAYMENT_EVENT_TYPE_SUCCEEDED"}`, cookies)
	confirmResponse := httptest.NewRecorder()
	handler.ServeHTTP(confirmResponse, confirm)
	if confirmResponse.Code != http.StatusOK || !strings.Contains(confirmResponse.Body.String(), `"status":"SUBSCRIPTION_STATUS_ACTIVE"`) || !strings.Contains(confirmResponse.Body.String(), `"plan_id":"pro"`) {
		t.Fatalf("confirm = %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	restore := productRequest(http.MethodPost, "/api/v1/subscription/transition", `{"transition":"SUBSCRIPTION_TRANSITION_KIND_RESTORE"}`, cookies)
	restoreResponse := httptest.NewRecorder()
	handler.ServeHTTP(restoreResponse, restore)
	if restoreResponse.Code != http.StatusUnauthorized {
		t.Fatalf("public restore transition = %d: %s", restoreResponse.Code, restoreResponse.Body.String())
	}
	refund := productRequest(http.MethodPost, "/api/v1/checkout/test-payment", `{"order_id":"`+orderID+`","event_type":"PAYMENT_EVENT_TYPE_REFUNDED"}`, cookies)
	refundResponse := httptest.NewRecorder()
	handler.ServeHTTP(refundResponse, refund)
	if refundResponse.Code != http.StatusOK || !strings.Contains(refundResponse.Body.String(), `"status":"SUBSCRIPTION_STATUS_CANCELED"`) {
		t.Fatalf("refund = %d: %s", refundResponse.Code, refundResponse.Body.String())
	}

	refresh := productRequest(http.MethodPost, "/api/v1/account/refresh", "", cookies)
	refreshResponse := httptest.NewRecorder()
	handler.ServeHTTP(refreshResponse, refresh)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh = %d: %s", refreshResponse.Code, refreshResponse.Body.String())
	}
	nextCookies := cookieMap(refreshResponse.Result().Cookies())
	if nextCookies["muxvia_cloud_access"] == nil || nextCookies["muxvia_cloud_refresh"] == nil {
		t.Fatalf("rotated cookies = %#v", nextCookies)
	}
	nextCookies["muxvia_cloud_csrf"] = cookies["muxvia_cloud_csrf"]
	logout := productRequest(http.MethodPost, "/api/v1/account/logout", "", nextCookies)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusOK {
		t.Fatalf("logout = %d: %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	reuse := productRequest(http.MethodGet, "/api/v1/account/commerce", "", nextCookies)
	reuseResponse := httptest.NewRecorder()
	handler.ServeHTTP(reuseResponse, reuse)
	if reuseResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie reuse = %d: %s", reuseResponse.Code, reuseResponse.Body.String())
	}
}

func TestProductAPIDoesNotExposeTestProviderUnlessExplicitlyEnabled(t *testing.T) {
	now := time.Now().UTC()
	store, err := postgrestest.Open(t, filepath.Join(t.TempDir(), "controller-postgres"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog, _ := webcontroller.LoadCatalog("config/plans.json")
	catalogSource, _ := cloudcatalog.NewSnapshotSource(catalog.Contract())
	service, _ := commerce.New(commerce.Config{Store: store, Catalog: catalogSource, Now: func() time.Time { return now }})
	handler, err := webcontroller.ProductAPIHandler(webcontroller.ProductAPIConfig{Commerce: service})
	if err != nil {
		t.Fatal(err)
	}
	request := productRequest(http.MethodPost, "/api/v1/checkout/test-payment", `{}`, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled test provider = %d: %s", response.Code, response.Body.String())
	}
}

func productRequest(method, path, body string, cookies map[string]*http.Cookie) *http.Request {
	request := httptest.NewRequest(method, "http://controller.test"+path, strings.NewReader(body))
	request.Header.Set("Origin", "http://controller.test")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	if csrf := cookies["muxvia_cloud_csrf"]; csrf != nil {
		request.Header.Set("X-Muxvia-CSRF", csrf.Value)
	}
	return request
}

func cookieMap(values []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(values))
	for _, value := range values {
		result[value.Name] = value
	}
	return result
}
