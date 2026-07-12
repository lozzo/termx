package webcontroller_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	webcontroller "github.com/lozzow/termx/private/cloud/web-controller"
)

func TestStatusHandlerReportsOwningServiceReadiness(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	// httptest 在本机 loopback listener 上运行，保持与部署时 SSH-only 管理边界一致。
	if host, _, err := net.SplitHostPort(upstream.Listener.Addr().String()); err != nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("test upstream is not loopback")
	}
	handler, err := webcontroller.StatusHandler(webcontroller.StatusConfig{
		ControlPlaneURL: upstream.URL, HubURL: upstream.URL, RelayURL: "turn:114.66.58.243:41003?transport=udp",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d", response.Code)
	}
}

func TestStatusHandlerPublishesConfiguredCatalog(t *testing.T) {
	catalog, err := webcontroller.LoadCatalog("config/plans.json")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := webcontroller.StatusHandler(webcontroller.StatusConfig{
		ControlPlaneURL: "http://127.0.0.1:41001", HubURL: "http://127.0.0.1:41002",
		RelayURL: "turn:114.66.58.243:41003?transport=udp", Catalog: &catalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/catalog", nil))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") == "" {
		t.Fatalf("catalog response = %d headers=%v", response.Code, response.Header())
	}
	var got webcontroller.Catalog
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || len(got.Plans) != 3 {
		t.Fatalf("catalog = (%#v, %v)", got, err)
	}
}

func TestStatusHandlerRejectsPublicControlPlaneOrigin(t *testing.T) {
	_, err := webcontroller.StatusHandler(webcontroller.StatusConfig{
		ControlPlaneURL: "http://114.66.58.243:41001", HubURL: "http://127.0.0.1:41002", RelayURL: "turn:114.66.58.243:41003?transport=udp",
	})
	if err == nil {
		t.Fatal("public Control Plane origin was accepted")
	}
}
