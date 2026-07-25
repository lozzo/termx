package creem_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/muxvia/muxvia/private/cloud/control-plane/creem"
)

func TestClientUsesOfficialTestOriginAndDoesNotLeakProviderBody(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme+"://"+request.URL.Host != "https://test-api.creem.io" || request.URL.Path != "/v1/checkouts" || request.Header.Get("x-api-key") != "test-key" {
			t.Fatalf("unexpected Creem request = %s headers=%v", request.URL, request.Header)
		}
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"product_id":"prod_test"`) || !strings.Contains(string(body), `"request_id":"order_test"`) || !strings.Contains(string(body), `"discount_code":"SAVE20"`) {
			t.Fatalf("checkout request body = %s", body)
		}
		return jsonResponse(http.StatusOK, `{"id":"ch_test","status":"pending","product":"prod_test","request_id":"order_test","checkout_url":"https://checkout.creem.test/session","order":{"id":"ord_test","product":"prod_test","amount":1000,"sub_total":1000,"discount_amount":200,"currency":"USD","status":"pending"},"discount":{"id":"dis_test","code":"SAVE20"}}`), nil
	})
	client, err := creem.NewClient(creem.ClientConfig{Environment: creem.EnvironmentTest, APIKey: "test-key", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	checkout, err := client.CreateCheckout(context.Background(), "prod_test", "order_test", "buyer@example.com", "SAVE20", "https://muxvia.com/account", map[string]string{"muxvia_order_id": "order_test"})
	if err != nil || checkout.ID != "ch_test" || checkout.Discount.ID != "dis_test" {
		t.Fatalf("checkout = %v, %v", checkout, err)
	}

	client, _ = creem.NewClient(creem.ClientConfig{Environment: creem.EnvironmentTest, APIKey: "test-key", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, `{"trace_id":"trace-safe","private":"must-not-leak"}`), nil
	})}})
	_, err = client.Transaction(context.Background(), "tran_test")
	var apiErr *creem.APIError
	if !errors.As(err, &apiErr) || !apiErr.Retryable || strings.Contains(err.Error(), "must-not-leak") || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("safe provider error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
